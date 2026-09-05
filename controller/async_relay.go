package controller

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
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
	Host            string     `json:"host,omitempty"`
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

// EnqueueAsyncRelayRequest 始终持久化后台任务，默认由服务器转回原响应，仅显式任务模式返回编号。
func EnqueueAsyncRelayRequest(c *gin.Context, format relaytypes.RelayFormat) error {
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return err
	}
	if storage.Size() > common.AsyncMediaMaxFileBytes {
		return fmt.Errorf("生成请求超过保存上限")
	}
	input, err := storage.NewReader()
	if err != nil {
		return err
	}
	defer input.Close()
	requestPath, file, err := common.CreateAsyncMediaFile()
	if err != nil {
		return err
	}
	saved := false
	var inputDetails model.AsyncRelayRequestDetails
	defer func() {
		_ = file.Close()
		if !saved {
			_ = common.RemoveAsyncMediaFile(requestPath)
			for _, reference := range inputDetails.References {
				_ = common.RemoveAsyncMediaFile(reference.Path)
			}
		}
	}()
	// 完整保留原始请求及表单附件，透明背景、流式开关、大整数和原生任务参数都由原接口处理。
	size, err := io.Copy(file, input)
	_ = input.Close()
	if err != nil {
		return err
	}
	modelName := c.GetString("original_model")
	contentType := c.GetHeader("Content-Type")
	if contentType == "" {
		contentType = "application/json"
	}
	if modelName == "" && !strings.Contains(strings.ToLower(contentType), "multipart/form-data") {
		var value struct {
			Model string `json:"model"`
		}
		_ = common.DecodeJson(io.NewSectionReader(file, 0, size), &value)
		modelName = value.Model
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
	metadata, err := common.Marshal(asyncRelayMetadata{Host: c.Request.Host, Params: c.Params, ClientIP: c.ClientIP(), SpecificChannel: c.GetString("specific_channel_id"), Action: c.GetString("action"), RelayMode: c.GetInt("relay_mode")})
	if err != nil {
		return err
	}
	task := &model.AsyncRelayTask{UserID: c.GetInt("id"), TokenID: c.GetInt("token_id"), ModelName: modelName,
		NodeID: common.NodeName, RequestMethod: http.MethodPost,
		RequestPath: c.Request.URL.Path, RequestQuery: removeAsyncQuery(c.Request.URL.RawQuery), RequestContentType: contentType,
		RequestFormat: string(format), RequestFilePath: requestPath, RequestFiles: headers, RequestMetadata: string(metadata),
	}
	// 在原请求文件清理前保存提示词及参考图，采集过程不发起任何远程请求。
	inputDetails = captureAsyncRequestDetails(task)
	encodedDetails, err := common.Marshal(inputDetails)
	if err != nil {
		return err
	}
	task.RequestDetails = string(encodedDetails)
	if task.ModelName == "" {
		task.ModelName = inputDetails.Parameters["model"]
	}
	task.TaskID, err = model.GenerateAsyncRelayTaskID()
	if err != nil {
		return err
	}
	taskMode := wantsAsyncRelayTask(c)
	var delivery *asyncRelayDelivery
	if !taskMode {
		// 在任务可被领取前登记响应接收方，避免快速任务先执行完而错过响应。
		delivery = &asyncRelayDelivery{changed: make(chan struct{}, 1)}
		asyncRelayDeliveries.Store(task.TaskID, delivery)
		defer asyncRelayDeliveries.Delete(task.TaskID)
	}
	action := "IMAGE"
	if format == relaytypes.RelayFormatTask || strings.HasSuffix(task.RequestPath, "/video") {
		action = "VIDEO"
	}
	if err := task.InsertWithLog(common.GetContextKeyString(c, constant.ContextKeyUsingGroup), action); err != nil {
		return err
	}
	saved = true
	c.Header("X-New-Api-Task-Id", task.TaskID)
	WakeAsyncRelayWorkers()
	if !taskMode {
		return delivery.serve(c, task.TaskID)
	}
	c.Header("Location", "/v1/tasks/"+task.TaskID)
	c.Header("Retry-After", "3")
	c.Header("Preference-Applied", "respond-async")
	c.JSON(http.StatusAccepted, gin.H{"id": task.TaskID, "task_id": task.TaskID, "object": asyncRelayObjectForFormat(format), "status": task.Status, "created": task.CreatedAt, "model": task.ModelName, "poll_url": "/v1/tasks/" + task.TaskID, "request_path": task.RequestPath})
	return nil
}

// wantsAsyncRelayTask 只由明确的接入选择改变返回方式，普通客户端不需要识别任务协议。
func wantsAsyncRelayTask(c *gin.Context) bool {
	if value, exists := c.GetQuery("async"); exists {
		enabled, _ := strconv.ParseBool(value)
		return enabled
	}
	for _, value := range strings.Split(c.GetHeader("Prefer"), ",") {
		if strings.EqualFold(strings.TrimSpace(strings.SplitN(value, ";", 2)[0]), "respond-async") {
			return true
		}
	}
	return false
}

func asyncRelayRequestBody(c *gin.Context) ([]byte, error) {
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, err
	}
	return storage.Bytes()
}

func asyncRequestHeaders(headers http.Header) (string, error) {
	values := make(map[string][]string)
	// 只持久化影响协议的白名单头，避免自定义鉴权信息进入数据库。
	for _, key := range []string{"Accept", "X-Forwarded-Proto", "X-Forwarded-Host", "OpenAI-Beta", "OpenAI-Organization", "OpenAI-Project", "Accept-Language"} {
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
	for _, key := range []string{"async", "key", "api_key", "access_token"} {
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
