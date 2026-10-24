package gemini

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// 只改变上游接口与接收格式，不改变客户端的流式标记，避免向非流式客户端发送 SSE 或保活包。
type forcedStreamAdaptor struct{ *Adaptor }

func (a forcedStreamAdaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	upstreamInfo := *info
	upstreamInfo.IsStream = true
	return a.Adaptor.GetRequestURL(&upstreamInfo)
}

func (a forcedStreamAdaptor) SetupRequestHeader(c *gin.Context, headers *http.Header, info *relaycommon.RelayInfo) error {
	if err := a.Adaptor.SetupRequestHeader(c, headers, info); err != nil {
		return err
	}
	headers.Set("Accept", "text/event-stream")
	return nil
}

func (a *Adaptor) doGeminiRequest(c *gin.Context, info *relaycommon.RelayInfo, body io.Reader) (*http.Response, error) {
	// 渠道参数覆盖后的最终请求体决定是否强制流式；默认渠道和非生成接口沿用原行为。
	model := info.UpstreamModelName
	if len(info.ParamOverride) == 0 || strings.HasPrefix(model, "imagen") || strings.HasPrefix(model, "embedding") ||
		strings.HasPrefix(model, "text-embedding") || strings.HasPrefix(model, "gemini-embedding") {
		return channel.DoApiRequest(a, c, info, body)
	}
	payload, err := io.ReadAll(body)
	if err != nil {
		return nil, err
	}
	forceStream := gjson.GetBytes(payload, "stream")
	if forceStream.Type != gjson.True {
		return channel.DoApiRequest(a, c, info, bytes.NewReader(payload))
	}
	// stream 是本站的渠道开关，Gemini 原生协议以接口路径区分流式，请勿将该字段发送给 Google。
	payload, err = sjson.DeleteBytes(payload, "stream")
	if err != nil {
		return nil, err
	}
	if !info.IsStream && gjson.GetBytes(payload, "generationConfig.streamFunctionCallArguments").Bool() {
		// 完整 JSON 客户端需要完整函数参数，不向上游启用参数增量实验能力。
		payload, err = sjson.SetBytes(payload, "generationConfig.streamFunctionCallArguments", false)
		if err != nil {
			return nil, err
		}
	}
	outbound, closer, err := relaycommon.NewOutboundJSONBody(payload)
	if err != nil {
		return nil, err
	}
	defer closer.Close()
	resp, err := channel.DoApiRequest(forcedStreamAdaptor{a}, c, info, outbound)
	if err != nil || resp.StatusCode != http.StatusOK || info.IsStream {
		return resp, err
	}
	if !strings.HasPrefix(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream") {
		// 兼容忽略流式接口而直接返回完整 JSON 的上游，原有处理器仍负责协议转换和错误检查。
		return resp, nil
	}
	return bufferGeminiStream(c, info, resp)
}

// 聚合时保留原生未知字段、候选序号、思考签名及工具调用；累计用量采用覆盖而不是逐帧相加。
type geminiStreamBuffer struct {
	response   map[string]common.RawMessage
	candidates []map[string]common.RawMessage
	indexes    map[int]int
	usage      *dto.GeminiUsageMetadata
	size       int
}

func (b *geminiStreamBuffer) add(data string) (bool, error) {
	if strings.TrimSpace(data) == "[DONE]" {
		return true, nil
	}
	b.size += len(data)
	if b.size > helper.DefaultMaxScannerBufferSize {
		return false, errors.New("Gemini stream exceeds buffer limit")
	}
	var chunk map[string]common.RawMessage
	if err := common.UnmarshalJsonStr(data, &chunk); err != nil || chunk == nil {
		return false, errors.New("invalid Gemini stream event")
	}
	if b.response == nil {
		b.response = make(map[string]common.RawMessage)
		b.indexes = make(map[int]int)
	}
	if raw, ok := chunk["error"]; ok && string(raw) != "null" {
		b.response = map[string]common.RawMessage{"error": raw}
		return true, nil
	}
	for key, value := range chunk {
		if key != "candidates" && key != "usageMetadata" {
			b.response[key] = value
		}
	}
	if raw, ok := chunk["usageMetadata"]; ok {
		var usage dto.GeminiUsageMetadata
		if err := common.Unmarshal(raw, &usage); err != nil {
			return false, err
		}
		b.usage = dto.MergeGeminiUsageMetadataNonZero(b.usage, &usage)
		// 模态明细同样是累计快照，不能复用旧流式实现中的明细累加逻辑。
		for _, key := range []string{"promptTokensDetails", "toolUsePromptTokensDetails", "candidatesTokensDetails"} {
			previous := gjson.GetBytes(b.response["usageMetadata"], key)
			current := gjson.GetBytes(raw, key)
			if current.Exists() {
				previous = current
			}
			if previous.Exists() {
				var details []dto.GeminiPromptTokensDetails
				if err := common.UnmarshalJsonStr(previous.Raw, &details); err != nil {
					return false, err
				}
				switch key {
				case "promptTokensDetails":
					b.usage.PromptTokensDetails = details
				case "toolUsePromptTokensDetails":
					b.usage.ToolUsePromptTokensDetails = details
				case "candidatesTokensDetails":
					b.usage.CandidatesTokensDetails = details
				}
			}
		}
		merged, err := common.Marshal(b.usage)
		if err != nil {
			return false, err
		}
		// 保留上游新增的用量字段，已知计费字段使用统一合并后的结果。
		var metadata map[string]common.RawMessage
		if old := b.response["usageMetadata"]; len(old) > 0 {
			_ = common.Unmarshal(old, &metadata)
		}
		if metadata == nil {
			metadata = make(map[string]common.RawMessage)
		}
		var incoming, known map[string]common.RawMessage
		_ = common.Unmarshal(raw, &incoming)
		_ = common.Unmarshal(merged, &known)
		maps.Copy(metadata, incoming)
		maps.Copy(metadata, known)
		b.response["usageMetadata"], err = common.Marshal(metadata)
		if err != nil {
			return false, err
		}
	}
	var candidates []map[string]common.RawMessage
	if raw := chunk["candidates"]; len(raw) > 0 {
		if err := common.Unmarshal(raw, &candidates); err != nil {
			return false, err
		}
	}
	for _, incoming := range candidates {
		index := 0
		if raw := incoming["index"]; len(raw) > 0 {
			if err := common.Unmarshal(raw, &index); err != nil {
				return false, err
			}
		}
		pos, ok := b.indexes[index]
		if !ok {
			pos = len(b.candidates)
			b.indexes[index] = pos
			b.candidates = append(b.candidates, make(map[string]common.RawMessage))
		}
		target := b.candidates[pos]
		if raw := incoming["content"]; len(raw) > 0 {
			var content, previous map[string]common.RawMessage
			if err := common.Unmarshal(raw, &content); err != nil {
				return false, err
			}
			if old := target["content"]; len(old) > 0 {
				if err := common.Unmarshal(old, &previous); err != nil {
					return false, err
				}
			}
			if previous == nil {
				previous = make(map[string]common.RawMessage)
			}
			var parts, additions []map[string]common.RawMessage
			if old := previous["parts"]; len(old) > 0 {
				if err := common.Unmarshal(old, &parts); err != nil {
					return false, err
				}
			}
			if next := content["parts"]; len(next) > 0 {
				if err := common.Unmarshal(next, &additions); err != nil {
					return false, err
				}
			}
			for _, part := range additions {
				if gjson.GetBytes(part["functionCall"], "partialArgs").Exists() || gjson.GetBytes(part["functionCall"], "willContinue").Bool() {
					return false, errors.New("Gemini returned incomplete function arguments for a buffered response")
				}
				// 仅合并同类文本增量，保留签名与非文本 part 的原始边界。
				if len(parts) > 0 && geminiTextPart(parts[len(parts)-1]) && geminiTextPart(part) &&
					(string(parts[len(parts)-1]["thought"]) == "true") == (string(part["thought"]) == "true") {
					last := parts[len(parts)-1]
					var a, text string
					_ = common.Unmarshal(last["text"], &a)
					_ = common.Unmarshal(part["text"], &text)
					last["text"], _ = common.Marshal(a + text)
				} else {
					parts = append(parts, part)
				}
			}
			maps.Copy(previous, content)
			var err error
			previous["parts"], err = common.Marshal(parts)
			if err != nil {
				return false, err
			}
			target["content"], err = common.Marshal(previous)
			if err != nil {
				return false, err
			}
		}
		for key, value := range incoming {
			if key != "content" {
				target[key] = value
			}
		}
	}
	return false, nil
}

func geminiTextPart(part map[string]common.RawMessage) bool {
	if _, ok := part["text"]; !ok {
		return false
	}
	for key := range part {
		if key != "text" && key != "thought" {
			return false
		}
	}
	return true
}

func bufferGeminiStream(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*http.Response, error) {
	// 独立读取上游，不调用会写下游 SSE 头和 ping 的公共转发器；超时和断开时关闭连接并回收读取协程。
	ctx, cancel := context.WithCancel(c.Request.Context())
	lines := make(chan string)
	readDone := make(chan struct{})
	var readErr error
	go func() {
		defer close(readDone)
		scanner := helper.NewStreamScanner(resp.Body)
		for scanner.Scan() {
			select {
			case lines <- scanner.Text():
			case <-ctx.Done():
				readErr = ctx.Err()
				return
			}
		}
		readErr = scanner.Err()
	}()
	defer func() { cancel(); _ = resp.Body.Close(); <-readDone }()
	timeout := time.Duration(constant.StreamingTimeout) * time.Second
	if timeout <= 0 {
		timeout = 300 * time.Second
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	var buffer geminiStreamBuffer
	var event strings.Builder
	finished := false
	for !finished {
		var data string
		select {
		case <-ctx.Done():
			return nil, types.NewErrorWithStatusCode(ctx.Err(), types.ErrorCodeBadResponseBody, http.StatusBadGateway, types.ErrOptionWithSkipRetry())
		case <-timer.C:
			return nil, types.NewErrorWithStatusCode(errors.New("Gemini stream idle timeout"), types.ErrorCodeBadResponseBody, http.StatusGatewayTimeout, types.ErrOptionWithSkipRetry())
		case <-readDone:
			if readErr != nil {
				return nil, types.NewErrorWithStatusCode(readErr, types.ErrorCodeBadResponseBody, http.StatusBadGateway, types.ErrOptionWithSkipRetry())
			}
			data = event.String()
			finished = true
		case line := <-lines:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(timeout)
			if payload, ok := strings.CutPrefix(line, "data:"); ok {
				if event.Len() > 0 {
					event.WriteByte('\n')
				}
				event.WriteString(strings.TrimPrefix(payload, " "))
				if event.Len() > helper.DefaultMaxScannerBufferSize {
					return nil, types.NewErrorWithStatusCode(errors.New("Gemini stream event exceeds buffer limit"), types.ErrorCodeBadResponseBody, http.StatusBadGateway, types.ErrOptionWithSkipRetry())
				}
				continue
			}
			if line != "" {
				continue
			}
			data = event.String()
			event.Reset()
		}
		if data == "" {
			continue
		}
		info.SetFirstResponseTime()
		info.ReceivedResponseCount++
		done, err := buffer.add(data)
		if err != nil {
			return nil, types.NewErrorWithStatusCode(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway, types.ErrOptionWithSkipRetry())
		}
		finished = finished || done
	}
	status := http.StatusOK
	if raw := buffer.response["error"]; len(raw) > 0 {
		status = int(gjson.GetBytes(raw, "code").Int())
		if status < 400 || status > 599 {
			status = http.StatusBadGateway
		}
	} else {
		// [DONE] 或 EOF 不能代替每个候选的结束原因，否则半截答案会被误当成功。
		if len(buffer.candidates) == 0 && !gjson.GetBytes(buffer.response["promptFeedback"], "blockReason").Exists() {
			return nil, types.NewErrorWithStatusCode(errors.New("empty Gemini stream"), types.ErrorCodeBadResponseBody, http.StatusBadGateway, types.ErrOptionWithSkipRetry())
		}
		for _, candidate := range buffer.candidates {
			var reason string
			_ = common.Unmarshal(candidate["finishReason"], &reason)
			if reason == "" {
				return nil, types.NewErrorWithStatusCode(errors.New("incomplete Gemini stream"), types.ErrorCodeBadResponseBody, http.StatusBadGateway, types.ErrOptionWithSkipRetry())
			}
		}
		if len(buffer.candidates) > 0 {
			data, err := common.Marshal(buffer.candidates)
			if err != nil {
				return nil, err
			}
			buffer.response["candidates"] = data
		}
	}
	payload, err := common.Marshal(buffer.response)
	if err != nil {
		return nil, err
	}
	result := *resp
	result.Header = resp.Header.Clone()
	result.Header.Set("Content-Type", "application/json")
	result.Header.Set("Content-Length", strconv.Itoa(len(payload)))
	for _, key := range []string{"Content-Encoding", "Transfer-Encoding", "ETag", "Content-MD5"} {
		result.Header.Del(key)
	}
	result.StatusCode = status
	result.Status = fmt.Sprintf("%d %s", status, http.StatusText(status))
	result.ContentLength = int64(len(payload))
	result.TransferEncoding = nil
	result.Body = io.NopCloser(bytes.NewReader(payload))
	return &result, nil
}
