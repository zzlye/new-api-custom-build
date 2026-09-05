package controller

import (
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
)

const asyncRelayMaxBatch = 4

// asyncRelayMetadata 仅保存重建请求所需的非密钥信息，不把令牌或会话写入任务文件。
type asyncRelayMetadata struct {
	Params          gin.Params `json:"params"`
	ClientIP        string     `json:"client_ip"`
	SpecificChannel string     `json:"specific_channel,omitempty"`
	Action          string     `json:"action,omitempty"`
	RelayMode       int        `json:"relay_mode,omitempty"`
}

// ShouldQueueAsyncRelay 统一接管媒体生成；文字对话与纯识图请求保持原有行为。
func ShouldQueueAsyncRelay(c *gin.Context, format relaytypes.RelayFormat) (bool, error) {
	if c == nil || c.Request == nil || c.GetString(model.AsyncRelayContextKey) != "" || c.Request.Method != http.MethodPost {
		return false, nil
	}
	switch format {
	case relaytypes.RelayFormatOpenAIImage:
		return true, nil
	case relaytypes.RelayFormatTask:
		return !strings.HasPrefix(c.Request.URL.Path, "/suno/") && c.GetInt("relay_mode") != relayconstant.RelayModeVideoFetchByID, nil
	case relaytypes.RelayFormatMjProxy:
		path := c.Request.URL.Path
		if strings.HasSuffix(path, "/describe") || strings.HasSuffix(path, "/shorten") || strings.HasSuffix(path, "/upload-discord-images") {
			return false, nil
		}
		return strings.Contains(path, "/submit/") || strings.HasSuffix(path, "/insight-face/swap"), nil
	case relaytypes.RelayFormatGemini, relaytypes.RelayFormatOpenAI, relaytypes.RelayFormatOpenAIResponses:
		body, err := asyncRelayRequestBody(c)
		if err != nil {
			return false, err
		}
		return isMediaGenerationRequest(c, body), nil
	default:
		return false, nil
	}
}

// isMediaGenerationRequest 使用模型名和输出类型识别生成请求，不把提示词里的图片字样当成开关。
func isMediaGenerationRequest(c *gin.Context, body []byte) bool {
	var request struct {
		Model            string `json:"model"`
		GenerationConfig struct {
			ResponseModalities []string `json:"responseModalities"`
		} `json:"generationConfig"`
		Tools []struct {
			Type string `json:"type"`
		} `json:"tools"`
	}
	if common.Unmarshal(body, &request) != nil {
		return false
	}
	for _, modality := range request.GenerationConfig.ResponseModalities {
		if strings.EqualFold(modality, "image") || strings.EqualFold(modality, "video") {
			return true
		}
	}
	for _, tool := range request.Tools {
		if tool.Type == "image_generation" {
			return true
		}
	}
	for _, name := range []string{request.Model, c.GetString("original_model"), c.Request.URL.Path} {
		name = strings.ToLower(name)
		if strings.Contains(name, "imagen") || strings.Contains(name, "image-generation") || strings.Contains(name, "-image") || strings.Contains(name, "image-preview") || strings.Contains(name, "banana") || strings.Contains(name, "sora") || strings.Contains(name, "veo-") {
			return true
		}
	}
	return false
}

// EnqueueAsyncRelayRequest 持久化请求后立刻返回编号，不等待上游连接或生成完成。
func EnqueueAsyncRelayRequest(c *gin.Context, format relaytypes.RelayFormat) error {
	contentType := c.GetHeader("Content-Type")
	requestPath, file, err := common.CreateAsyncMediaFile()
	if err != nil {
		return err
	}
	saved := false
	defer func() {
		_ = file.Close()
		if !saved {
			_ = common.RemoveAsyncMediaFile(requestPath)
		}
	}()
	var body []byte
	if strings.Contains(strings.ToLower(contentType), "multipart/form-data") {
		// 重写表单时保留全部附件及自定义字段，只移除异步开关并关闭上游流式输出。
		form, err := common.ParseMultipartFormReusable(c)
		if err != nil {
			return err
		}
		defer form.RemoveAll()
		writer := multipart.NewWriter(file)
		for key, values := range form.Value {
			if key == "async" || key == "background" || key == "stream" {
				continue
			}
			for _, value := range values {
				if err := writer.WriteField(key, value); err != nil {
					return err
				}
			}
		}
		if err := writer.WriteField("stream", "false"); err != nil {
			return err
		}
		for key, files := range form.File {
			for _, header := range files {
				input, err := header.Open()
				if err != nil {
					return err
				}
				output, createErr := writer.CreatePart(header.Header)
				if createErr != nil {
					_ = input.Close()
					return createErr
				}
				_, copyErr := io.Copy(output, input)
				_ = input.Close()
				if copyErr != nil {
					return fmt.Errorf("保存附件 %s 失败: %w", key, copyErr)
				}
			}
		}
		if err := writer.Close(); err != nil {
			return err
		}
		contentType = writer.FormDataContentType()
	} else {
		body, err = asyncRelayRequestBody(c)
		if err != nil {
			return err
		}
		body, err = normalizeAsyncRelayJSON(body)
		if err != nil {
			return err
		}
		if _, err := file.Write(body); err != nil {
			return err
		}
		contentType = "application/json"
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	headers, err := asyncRequestHeaders(c.Request.Header)
	if err != nil {
		return err
	}
	metadata, err := common.Marshal(asyncRelayMetadata{Params: c.Params, ClientIP: c.ClientIP(), SpecificChannel: c.GetString("specific_channel_id"), Action: c.GetString("action"), RelayMode: c.GetInt("relay_mode")})
	if err != nil {
		return err
	}
	modelName := c.GetString("original_model")
	if modelName == "" {
		var value struct {
			Model string `json:"model"`
		}
		_ = common.Unmarshal(body, &value)
		modelName = value.Model
	}
	task := &model.AsyncRelayTask{UserID: c.GetInt("id"), TokenID: c.GetInt("token_id"), ModelName: modelName,
		NodeID: common.NodeName, RequestMethod: http.MethodPost,
		RequestPath:  strings.ReplaceAll(c.Request.URL.Path, ":streamGenerateContent", ":generateContent"),
		RequestQuery: removeAsyncQuery(c.Request.URL.RawQuery), RequestContentType: contentType,
		RequestFormat: string(format), RequestFilePath: requestPath, RequestFiles: headers, RequestMetadata: string(metadata),
	}
	action := "IMAGE"
	if format == relaytypes.RelayFormatTask || strings.HasSuffix(task.RequestPath, "/video") {
		action = "VIDEO"
	}
	if err := task.InsertWithLog(common.GetContextKeyString(c, constant.ContextKeyUsingGroup), action); err != nil {
		return err
	}
	saved = true
	c.Header("Location", "/v1/tasks/"+task.TaskID)
	c.Header("Retry-After", "3")
	c.JSON(http.StatusAccepted, gin.H{"id": task.TaskID, "task_id": task.TaskID, "object": asyncRelayObjectForFormat(format), "status": task.Status, "created": task.CreatedAt, "model": task.ModelName, "poll_url": "/v1/tasks/" + task.TaskID, "request_path": task.RequestPath})
	WakeAsyncRelayWorkers()
	return nil
}

func asyncRelayRequestBody(c *gin.Context) ([]byte, error) {
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, err
	}
	return storage.Bytes()
}

// normalizeAsyncRelayJSON 保留原始数字精度，避免大整数在入队时被浮点转换改写。
func normalizeAsyncRelayJSON(body []byte) ([]byte, error) {
	var payload map[string]json.RawMessage
	if err := common.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("请求应为有效的对象: %w", err)
	}
	if payload == nil {
		return nil, fmt.Errorf("请求应为有效的对象")
	}
	delete(payload, "async")
	delete(payload, "background")
	if _, exists := payload["stream"]; exists {
		payload["stream"] = json.RawMessage("false")
	}
	return common.Marshal(payload)
}

func asyncRequestHeaders(headers http.Header) (string, error) {
	values := make(map[string][]string)
	// 只持久化影响协议的白名单头，避免自定义鉴权信息进入数据库。
	for _, key := range []string{"OpenAI-Beta", "OpenAI-Organization", "OpenAI-Project", "Accept-Language"} {
		if items := headers.Values(key); len(items) > 0 {
			values[key] = items
		}
	}
	data, err := common.Marshal(values)
	return string(data), err
}

func removeAsyncQuery(raw string) string {
	query, err := url.ParseQuery(raw)
	if err != nil {
		return ""
	}
	for _, key := range []string{"async", "background", "alt", "key", "api_key", "access_token"} {
		query.Del(key)
	}
	return query.Encode()
}

func asyncRelayObjectForFormat(format relaytypes.RelayFormat) string {
	if format == relaytypes.RelayFormatTask {
		return "video_generation"
	}
	if format == relaytypes.RelayFormatGemini {
		return "gemini_image_generation"
	}
	return "image_generation"
}
