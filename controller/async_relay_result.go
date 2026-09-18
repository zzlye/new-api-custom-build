package controller

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
)

// asyncMediaSource 在归档时同时携带文件内容和可选来源，来源地址本身不触发额外下载。
type asyncMediaSource struct {
	SourceURL   string
	Value       string
	ContentType string
	Base64      bool
}

// 只识别结果文本中的 Markdown 图片，不把普通超链接当成生成文件。
// 支持尖括号地址、可选标题和地址中的一层括号，保留签名查询参数原文。
var asyncMarkdownImagePattern = regexp.MustCompile(`!\[[^\]\r\n]*\]\(\s*(?:<([^<>\r\n]+)>|([^\s()<>]+(?:\([^\s()<>]*\)[^\s()<>]*)*))(?:\s+["'][^\r\n]*?["'])?\s*\)`)

func collectAsyncMarkdownImages(text string, sources *[]asyncMediaSource) {
	// 超过归档上限时交由现有数量校验拒绝，限制超长回复的解析开销。
	for _, match := range asyncMarkdownImagePattern.FindAllStringSubmatch(text, 129) {
		value := match[1]
		if value == "" {
			value = match[2]
		}
		if strings.HasPrefix(value, "https://") || strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "data:image/") {
			*sources = append(*sources, asyncMediaSource{Value: value, SourceURL: asyncMediaSourceURL(value)})
		}
	}
}

// collectAsyncMediaSources 识别图片接口、Gemini、Responses 及聊天生图的结果结构。
func collectAsyncMediaSources(value any, sources *[]asyncMediaSource) {
	switch item := value.(type) {
	case []any:
		for _, child := range item {
			collectAsyncMediaSources(child, sources)
		}
	case string:
		if strings.HasPrefix(item, "https://") || strings.HasPrefix(item, "http://") || strings.HasPrefix(item, "data:") {
			*sources = append(*sources, asyncMediaSource{Value: item, SourceURL: asyncMediaSourceURL(item)})
		} else {
			collectAsyncMarkdownImages(item, sources)
		}
	case map[string]any:
		inlineStart := len(*sources)
		hasInline := false
		if data, ok := item["b64_json"].(string); ok && data != "" {
			*sources = append(*sources, asyncMediaSource{Value: data, Base64: true})
			hasInline = true
		}
		if item["type"] == "image_generation_call" {
			if data, ok := item["result"].(string); ok && data != "" {
				*sources = append(*sources, asyncMediaSource{Value: data, Base64: true})
				hasInline = true
			}
		}
		for _, key := range []string{"inlineData", "inline_data"} {
			if inline, ok := item[key].(map[string]any); ok {
				contentType, _ := inline["mimeType"].(string)
				if contentType == "" {
					contentType, _ = inline["mime_type"].(string)
				}
				if data, ok := inline["data"].(string); ok && (strings.HasPrefix(contentType, "image/") || strings.HasPrefix(contentType, "video/")) {
					*sources = append(*sources, asyncMediaSource{Value: data, ContentType: contentType, Base64: true})
					hasInline = true
				}
			}
		}
		// 内嵌数据优先保存，备用地址只作为这份媒体的来源记录，不重复下载。
		var aliases []asyncMediaSource
		// Gemini 将图片链接放在 parts.text；仍走相同的地址校验、下载和文件头检查。
		if text, ok := item["text"].(string); ok {
			collectAsyncMarkdownImages(text, &aliases)
		}
		for _, key := range []string{"url", "image_url", "imageUrl", "video_url", "videoUrl"} {
			if child, ok := item[key]; ok {
				collectAsyncMediaSources(child, &aliases)
			}
		}
		if hasInline {
			for _, alias := range aliases {
				if alias.SourceURL == "" {
					continue
				}
				for index := inlineStart; index < len(*sources); index++ {
					(*sources)[index].SourceURL = alias.SourceURL
				}
				break
			}
		} else {
			*sources = append(*sources, aliases...)
		}
		// 仅遍历结果容器，不下载提示词、错误描述或用量字段里出现的地址。
		for _, key := range []string{"data", "candidates", "content", "parts", "output", "choices", "message", "delta", "images", "results", "videoUrls"} {
			if child, ok := item[key]; ok {
				collectAsyncMediaSources(child, sources)
			}
		}
	}
}

// readAsyncRelayMediaSources 同时解析普通响应和原样保存的事件流，不要求调用方关闭流式功能。
func readAsyncRelayMediaSources(reader io.Reader, contentType string) ([]asyncMediaSource, error) {
	var sources []asyncMediaSource
	if !strings.Contains(strings.ToLower(contentType), "text/event-stream") {
		var value any
		if err := common.DecodeJson(reader, &value); err != nil {
			return nil, fmt.Errorf("上游生成结果格式有误")
		}
		collectAsyncMediaSources(value, &sources)
		if len(sources) == 0 {
			if reason := asyncMediaResponseDiagnostic(value); reason != "" {
				return nil, fmt.Errorf("%s", reason)
			}
		}
		return sources, nil
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), int(common.AsyncMediaMaxFileBytes))
	var event strings.Builder
	var diagnostic string
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if err := collectAsyncStreamMediaEvent(event.String(), &sources, &diagnostic); err != nil {
				return nil, err
			}
			event.Reset()
			continue
		}
		if strings.HasPrefix(line, "data:") {
			if event.Len() > 0 {
				event.WriteByte('\n')
			}
			event.WriteString(strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("读取上游生成事件失败")
	}
	if err := collectAsyncStreamMediaEvent(event.String(), &sources, &diagnostic); err != nil {
		return nil, err
	}
	// 流式响应先输出文字再输出图片属于正常情况，只在流结束且无媒体时报告原因。
	if len(sources) == 0 && diagnostic != "" {
		return nil, fmt.Errorf("%s", diagnostic)
	}
	return sources, nil
}

// collectAsyncStreamMediaEvent 只收集最终图片，Responses 的完整结果与完成事件随后统一去重。
func collectAsyncStreamMediaEvent(data string, sources *[]asyncMediaSource, diagnostic *string) error {
	data = strings.TrimSpace(data)
	if data == "" || data == "[DONE]" {
		return nil
	}
	var value any
	if common.Unmarshal([]byte(data), &value) != nil {
		return fmt.Errorf("上游流式生成结果格式有误")
	}
	if event, ok := value.(map[string]any); ok {
		eventType, _ := event["type"].(string)
		if strings.Contains(eventType, "partial_image") {
			return nil
		}
		switch eventType {
		case "response.output_item.done":
			value = event["item"]
		case "response.completed":
			value = event["response"]
		case "error", "response.failed":
			detail := "上游流式生成失败"
			failure, _ := event["error"].(map[string]any)
			if response, ok := event["response"].(map[string]any); ok {
				failure, _ = response["error"].(map[string]any)
			}
			if message, ok := failure["message"].(string); ok && message != "" {
				detail += "：" + message
			}
			return fmt.Errorf("%s", detail)
		}
	}
	collectAsyncMediaSources(value, sources)
	if reason := asyncMediaResponseDiagnostic(value); reason != "" {
		*diagnostic = reason
	}
	return nil
}

// inspectAsyncMedia 使用文件头识别可展示的媒体，不把上游返回的网页或脚本当作图片。
func inspectAsyncMedia(path string) (string, string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", "", err
	}
	defer file.Close()
	prefix := make([]byte, 512)
	n, err := file.Read(prefix)
	if err != nil && err != io.EOF {
		return "", "", err
	}
	if n == 0 {
		return "", "", fmt.Errorf("生成媒体文件为空")
	}
	contentType := http.DetectContentType(prefix[:n])
	switch contentType {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return contentType, "image", nil
	case "video/mp4", "video/webm", "video/avi", "video/mpeg":
		return contentType, "video", nil
	}
	if n >= 12 && string(prefix[4:8]) == "ftyp" {
		if string(prefix[8:12]) == "avif" || string(prefix[8:12]) == "avis" {
			return "image/avif", "image", nil
		}
		return "video/mp4", "video", nil
	}
	return "", "", fmt.Errorf("上游返回的文件不是可预览的图片或视频")
}

func saveAsyncMediaSource(ctx context.Context, source asyncMediaSource) (model.AsyncRelayMedia, error) {
	sourceURL := asyncMediaSourceURL(source.SourceURL)
	if sourceURL == "" && !source.Base64 {
		sourceURL = asyncMediaSourceURL(source.Value)
	}
	var reader io.Reader
	var closer io.Closer
	if strings.HasPrefix(source.Value, "data:") {
		parts := strings.SplitN(source.Value, ",", 2)
		if len(parts) != 2 || !strings.HasSuffix(parts[0], ";base64") {
			return model.AsyncRelayMedia{}, fmt.Errorf("媒体内嵌数据格式有误")
		}
		source.Value, source.Base64 = parts[1], true
	}
	if source.Base64 {
		encoding := base64.StdEncoding
		if len(source.Value)%4 != 0 {
			encoding = base64.RawStdEncoding
		}
		reader = base64.NewDecoder(encoding, strings.NewReader(source.Value))
	} else {
		if err := service.ValidateSSRFProtectedFetchURL(source.Value); err != nil {
			return model.AsyncRelayMedia{}, fmt.Errorf("%s", asyncMediaURLValidationReason(err))
		}
		downloadCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
		request, err := http.NewRequestWithContext(downloadCtx, http.MethodGet, source.Value, nil)
		if err != nil {
			return model.AsyncRelayMedia{}, err
		}
		client := service.GetSSRFProtectedHTTPClient()
		if client == nil {
			return model.AsyncRelayMedia{}, fmt.Errorf("媒体下载服务尚未初始化")
		}
		// 只进行一次媒体下载，失败直接交由任务状态记录，禁止自动重复抓取。
		response, err := client.Do(request)
		if err != nil {
			return model.AsyncRelayMedia{}, fmt.Errorf("下载生成文件失败")
		}
		reader, closer = response.Body, response.Body
		defer closer.Close()
		if response.StatusCode != http.StatusOK {
			return model.AsyncRelayMedia{}, fmt.Errorf("下载生成文件失败（HTTP %d）", response.StatusCode)
		}
	}
	path, file, err := common.CreateAsyncMediaFile()
	if err != nil {
		return model.AsyncRelayMedia{}, err
	}
	keep := false
	defer func() {
		_ = file.Close()
		if !keep {
			_ = common.RemoveAsyncMediaFile(path)
		}
	}()
	size, err := io.Copy(file, io.LimitReader(reader, common.AsyncMediaMaxFileBytes+1))
	if err != nil {
		return model.AsyncRelayMedia{}, err
	}
	if size > common.AsyncMediaMaxFileBytes {
		return model.AsyncRelayMedia{}, fmt.Errorf("生成文件超过保存上限")
	}
	if err := file.Sync(); err != nil {
		return model.AsyncRelayMedia{}, err
	}
	if err := file.Close(); err != nil {
		return model.AsyncRelayMedia{}, err
	}
	contentType, kind, err := inspectAsyncMedia(path)
	if err != nil {
		return model.AsyncRelayMedia{}, err
	}
	keep = true
	return model.AsyncRelayMedia{Path: path, ContentType: contentType, Kind: kind, SourceURL: sourceURL, SourceChecked: true}, nil
}

// completeAsyncRelayResult 生成结果和预览文件全部就绪后才提交成功状态。
func completeAsyncRelayResult(ctx context.Context, task *model.AsyncRelayTask, path, contentType string) bool {
	if !beginAsyncRelayMediaSave(task) {
		return false
	}
	return finalizeAsyncRelayResult(ctx, task, path, contentType)
}

// finalizeAsyncRelayResult 只在本次保存执行中完成归档，调用前必须持久化唯一保存机会。
func finalizeAsyncRelayResult(ctx context.Context, task *model.AsyncRelayTask, path, contentType string) bool {
	// 先持久化完整上游响应；下载失败后保留核对依据，不重新发起生成或保存。
	if task.ResultFilePath != path {
		result := model.DB.Model(&model.AsyncRelayTask{}).Where("id = ? AND status = ? AND worker_id = ?", task.ID, model.AsyncRelayTaskStatusProcessing, task.WorkerID).
			Updates(map[string]any{"result_file_path": path, "result_content_type": contentType, "updated_at": common.GetTimestamp()})
		if result.Error != nil || result.RowsAffected == 0 {
			failAsyncRelayTask(task, "保存完整生成结果失败")
			return false
		}
		task.ResultFilePath, task.ResultContentType = path, contentType
	}
	var media []model.AsyncRelayMedia
	keep := false
	defer func() {
		if !keep {
			for _, item := range media {
				if item.Path != path {
					_ = common.RemoveAsyncMediaFile(item.Path)
				}
			}
		}
	}()
	if strings.HasPrefix(contentType, "image/") || strings.HasPrefix(contentType, "video/") {
		detected, kind, err := inspectAsyncMedia(path)
		if err != nil {
			failAsyncRelayTask(task, err.Error())
			return true
		}
		// 原生视频通过内容接口归档，真实来源从已保存的子任务补入，不额外发起下载。
		sourceURL := ""
		if task.RequestFormat == string(relaytypes.RelayFormatTask) && task.LinkedTaskID != "" {
			if child, exists, err := model.GetByTaskId(task.UserID, task.LinkedTaskID); err == nil && exists {
				sourceURL = asyncMediaSourceURL(child.PrivateData.ResultURL)
			}
		}
		media = append(media, model.AsyncRelayMedia{Path: path, ContentType: detected, Kind: kind, SourceURL: sourceURL, SourceChecked: true})
		contentType = detected
	} else {
		file, err := os.Open(path)
		if err != nil {
			failAsyncRelayTask(task, "读取生成结果失败")
			return true
		}
		sources, err := readAsyncRelayMediaSources(file, contentType)
		_ = file.Close()
		if err != nil {
			failAsyncRelayTask(task, err.Error())
			return true
		}
		sources = uniqueAsyncMediaSources(sources)
		if len(sources) == 0 {
			failAsyncRelayTask(task, "上游未返回可保存的图片或视频")
			return true
		}
		if len(sources) > 128 {
			failAsyncRelayTask(task, "生成文件数量超过保存上限")
			return true
		}
		var totalBytes int64
		for _, source := range sources {
			result, err := saveAsyncMediaSource(ctx, source)
			if err != nil {
				failAsyncRelayTask(task, "生成已完成，保存媒体失败："+err.Error()+"，自动重试已关闭")
				return true
			}
			media = append(media, result)
			info, err := os.Stat(result.Path)
			if err != nil {
				failAsyncRelayTask(task, "读取生成文件大小失败")
				return true
			}
			totalBytes += info.Size()
			if totalBytes > common.AsyncMediaMaxFileBytes {
				failAsyncRelayTask(task, "生成文件总大小超过保存上限")
				return true
			}
		}
		if !strings.Contains(strings.ToLower(contentType), "text/event-stream") {
			contentType = "application/json"
		}
	}
	encoded, err := common.Marshal(media)
	if err != nil {
		failAsyncRelayTask(task, "保存媒体清单失败")
		return true
	}
	task.ResultFiles, task.ResultFilePath, task.ResultContentType = string(encoded), path, contentType
	task.Status, task.Error, task.NextAttemptAt = model.AsyncRelayTaskStatusSucceeded, "", 0
	if task.ResponseStatusCode == 0 {
		task.ResponseStatusCode = http.StatusOK
	}
	won, err := task.UpdateWithStatus(model.AsyncRelayTaskStatusProcessing)
	if err != nil {
		common.SysError("保存媒体任务结果失败: " + err.Error())
		return true
	}
	keep = won
	return true
}

// beginAsyncRelayMediaSave 在下载或写入前占用唯一保存机会，进程中断也不会重复执行。
func beginAsyncRelayMediaSave(task *model.AsyncRelayTask) bool {
	result := model.DB.Model(&model.AsyncRelayTask{}).
		Where("id = ? AND status = ? AND worker_id = ?", task.ID, model.AsyncRelayTaskStatusProcessing, task.WorkerID).
		Where("media_attempts = ? OR media_attempts IS NULL", 0).
		Where("next_attempt_at = ? OR next_attempt_at IS NULL", 0).
		Updates(map[string]any{"media_attempts": 1, "updated_at": common.GetTimestamp()})
	if result.Error != nil || result.RowsAffected != 1 {
		failAsyncRelayTask(task, "保存文件未执行或已中断，自动重试已关闭")
		return false
	}
	task.MediaAttempts = 1
	return true
}
