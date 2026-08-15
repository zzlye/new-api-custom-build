package controller

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
)

const (
	asyncRelayWorkerHeader = "X-New-Api-Async-Worker"
	asyncRelayHeader       = "X-New-Api-Async"
	asyncRelayObject       = "image_generation"
	asyncRelayMaxBatch     = 4
)

// ShouldQueueAsyncRelay 判断图片或 Gemini 生图请求是否要求后台执行。
// 默认保持原有同步行为，调用方可使用 async/background 查询参数或请求头开启异步。
func ShouldQueueAsyncRelay(c *gin.Context, relayFormat relaytypes.RelayFormat) (bool, error) {
	if c == nil || c.Request == nil || c.GetHeader(asyncRelayWorkerHeader) == "true" {
		return false, nil
	}
	if relayFormat != relaytypes.RelayFormatOpenAIImage && relayFormat != relaytypes.RelayFormatGemini {
		return false, nil
	}

	requested := isAsyncFlag(c.Query("async")) ||
		isAsyncFlag(c.Query("background")) ||
		isAsyncFlag(c.GetHeader(asyncRelayHeader))
	var body []byte
	contentType := strings.ToLower(c.GetHeader("Content-Type"))
	// 只有 JSON 请求可能在请求体中携带开关；multipart 图片编辑通过查询参数或请求头开启，避免复制大文件。
	if relayFormat == relaytypes.RelayFormatGemini || strings.HasPrefix(contentType, "application/json") {
		var err error
		body, err = asyncRelayRequestBody(c)
		if err != nil {
			return false, err
		}
	}
	if !requested {
		requested = asyncFlagFromJSON(body)
	}
	if !requested {
		return false, nil
	}
	if relayFormat == relaytypes.RelayFormatGemini && !isGeminiImageRequest(c, body) {
		return false, nil
	}
	if isStreamingAsyncRequest(c, body) {
		return false, fmt.Errorf("异步图片请求不支持流式响应")
	}
	return true, nil
}

// EnqueueAsyncRelayRequest 将请求体写入持久化缓存并创建用户可查询的任务。
func EnqueueAsyncRelayRequest(c *gin.Context, relayFormat relaytypes.RelayFormat) error {
	contentType := c.GetHeader("Content-Type")
	var body []byte
	var requestFile string
	var err error
	if strings.HasPrefix(strings.ToLower(contentType), "application/json") {
		body, err = asyncRelayRequestBody(c)
		if err != nil {
			return err
		}
		body = stripAsyncJSONFlags(body, contentType)
		requestFile, err = common.WriteDiskCacheFile(common.DiskCacheTypeFile, body)
	} else {
		requestFile, err = copyAsyncRelayBodyToFile(c)
	}
	if err != nil {
		return fmt.Errorf("保存异步请求失败: %w", err)
	}

	headers, err := asyncRequestHeaders(c.Request.Header)
	if err != nil {
		_ = common.RemoveDiskCacheFile(requestFile)
		return fmt.Errorf("保存异步请求头失败: %w", err)
	}
	modelName := c.GetString("original_model")
	if modelName == "" {
		modelName = asyncModelName(body)
	}
	task := &model.AsyncRelayTask{
		UserID:             c.GetInt("id"),
		TokenID:            c.GetInt("token_id"),
		ModelName:          modelName,
		RequestMethod:      c.Request.Method,
		RequestPath:        c.Request.URL.Path,
		RequestQuery:       removeAsyncQuery(c.Request.URL.RawQuery),
		RequestContentType: contentType,
		RequestFormat:      string(relayFormat),
		RequestFilePath:    requestFile,
		RequestFiles:       headers,
	}
	if err := task.Insert(); err != nil {
		_ = common.RemoveDiskCacheFile(requestFile)
		return fmt.Errorf("创建异步任务失败: %w", err)
	}

	c.Header("Location", "/v1/tasks/"+task.TaskID)
	c.JSON(http.StatusAccepted, gin.H{
		"id":           task.TaskID,
		"task_id":      task.TaskID,
		"object":       asyncRelayObjectForFormat(relayFormat),
		"status":       string(task.Status),
		"created":      task.CreatedAt,
		"model":        task.ModelName,
		"poll_url":     "/v1/tasks/" + task.TaskID,
		"request_path": task.RequestPath,
	})

	// 立即尝试执行，系统任务调度器同时负责跨进程恢复待处理任务。
	gopool.Go(func() {
		_, _ = ProcessAsyncRelayTasks(context.Background(), 1)
	})
	return nil
}

// GetAsyncRelayTask 返回异步任务状态和已完成的原始接口结果。
func GetAsyncRelayTask(c *gin.Context) {
	task, err := model.GetAsyncRelayTaskByUserAndTaskID(c.GetInt("id"), c.Param("task_id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": "查询异步任务失败"}})
		return
	}
	if task == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"message": "异步任务不存在"}})
		return
	}

	response := gin.H{
		"id":       task.TaskID,
		"task_id":  task.TaskID,
		"object":   asyncRelayObjectForFormat(relaytypes.RelayFormat(task.RequestFormat)),
		"status":   string(task.Status),
		"created":  task.CreatedAt,
		"updated":  task.UpdatedAt,
		"model":    task.ModelName,
		"poll_url": "/v1/tasks/" + task.TaskID,
	}
	if task.Status == model.AsyncRelayTaskStatusSucceeded && task.ResultFilePath != "" {
		result, readErr := os.ReadFile(task.ResultFilePath)
		if readErr != nil {
			response["status"] = string(model.AsyncRelayTaskStatusFailed)
			response["error"] = gin.H{"message": "异步结果文件已失效"}
		} else {
			var resultValue any
			if common.Unmarshal(result, &resultValue) == nil {
				response["result"] = resultValue
			} else {
				response["result"] = string(result)
			}
			response["response_status_code"] = task.ResponseStatusCode
			response["content_type"] = task.ResultContentType
		}
	}
	if task.Status == model.AsyncRelayTaskStatusFailed || task.Status == model.AsyncRelayTaskStatusCancelled {
		response["error"] = gin.H{"message": task.Error}
	}
	c.JSON(http.StatusOK, response)
}

// ProcessAsyncRelayTasks 领取并处理一批图片或 Gemini 生图任务。
func ProcessAsyncRelayTasks(ctx context.Context, limit int) (int, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if limit <= 0 || limit > asyncRelayMaxBatch {
		limit = asyncRelayMaxBatch
	}
	// 进程异常退出后，超过租约时间的任务需要重新进入队列。
	if _, err := model.RecoverStaleAsyncRelayTasks(); err != nil {
		return 0, err
	}
	pending, err := model.FindPendingAsyncRelayTasks(limit)
	if err != nil {
		return 0, err
	}
	processed := 0
	workerID := fmt.Sprintf("%s-async-%d", common.NodeName, time.Now().UnixNano())
	for _, candidate := range pending {
		if ctx.Err() != nil {
			return processed, ctx.Err()
		}
		task, claimed, claimErr := model.ClaimAsyncRelayTask(candidate.ID, workerID)
		if claimErr != nil {
			return processed, claimErr
		}
		if !claimed || task == nil {
			continue
		}
		processed++
		processAsyncRelayTask(ctx, task)
	}
	return processed, nil
}

func processAsyncRelayTask(ctx context.Context, task *model.AsyncRelayTask) {
	defer func() {
		if recovered := recover(); recovered != nil {
			failAsyncRelayTask(task, fmt.Sprintf("异步任务执行异常: %v", recovered))
		}
		if task.RequestFilePath != "" {
			_ = common.RemoveDiskCacheFile(task.RequestFilePath)
		}
	}()

	requestFile, err := os.Open(task.RequestFilePath)
	if err != nil {
		failAsyncRelayTask(task, "读取异步请求失败")
		return
	}
	defer requestFile.Close()
	requestStat, err := requestFile.Stat()
	if err != nil {
		failAsyncRelayTask(task, "读取异步请求大小失败")
		return
	}
	requestHeaders := make(http.Header)
	if task.RequestFiles != "" {
		var values map[string][]string
		if err := common.Unmarshal([]byte(task.RequestFiles), &values); err == nil {
			for key, items := range values {
				for _, value := range items {
					requestHeaders.Add(key, value)
				}
			}
		}
	}
	if task.RequestContentType != "" {
		requestHeaders.Set("Content-Type", task.RequestContentType)
	}
	requestHeaders.Set(asyncRelayWorkerHeader, "true")
	recording := httptest.NewRecorder()
	workerContext, _ := gin.CreateTestContext(recording)
	defer common.CleanupBodyStorage(workerContext)
	requestURL := &url.URL{Path: task.RequestPath, RawQuery: removeAsyncQuery(task.RequestQuery)}
	request := &http.Request{
		Method:        task.RequestMethod,
		URL:           requestURL,
		Header:        requestHeaders,
		Body:          requestFile,
		ContentLength: requestStat.Size(),
		Host:          "async-worker",
	}
	workerContext.Request = request.WithContext(ctx)

	userCache, err := model.GetUserCache(task.UserID)
	if err != nil || userCache == nil || userCache.Status != common.UserStatusEnabled {
		failAsyncRelayTask(task, "用户状态不可用")
		return
	}
	userCache.WriteContext(workerContext)
	token, err := model.GetTokenById(task.TokenID)
	if err != nil || token == nil || token.UserId != task.UserID {
		failAsyncRelayTask(task, "异步任务令牌已失效")
		return
	}
	if err := middleware.SetupContextForToken(workerContext, token); err != nil {
		failAsyncRelayTask(task, "恢复异步任务授权失败")
		return
	}
	// SetupContextForToken 只恢复令牌字段，分组需要沿用提交时的选择规则。
	usingGroup := userCache.Group
	if token.Group != "" {
		usingGroup = token.Group
	}
	common.SetContextKey(workerContext, constant.ContextKeyUsingGroup, usingGroup)

	// 复用正式分发逻辑重新选择渠道，确保任务执行时仍遵守模型和分组权限。
	middleware.Distribute()(workerContext)
	if workerContext.IsAborted() || recording.Code >= http.StatusBadRequest {
		failAsyncRelayTask(task, "异步任务渠道分发失败")
		return
	}
	Relay(workerContext, relaytypes.RelayFormat(task.RequestFormat))
	statusCode := workerContext.Writer.Status()
	if statusCode <= 0 {
		statusCode = recording.Code
	}
	task.ResponseStatusCode = statusCode
	task.ResponseContentType = recording.Header().Get("Content-Type")
	task.ResultContentType = task.ResponseContentType
	if statusCode >= http.StatusBadRequest {
		failAsyncRelayTask(task, fmt.Sprintf("上游请求失败（HTTP %d）", statusCode))
		return
	}
	resultPath, err := common.WriteDiskCacheFile(common.DiskCacheTypeFile, recording.Body.Bytes())
	if err != nil {
		failAsyncRelayTask(task, "保存异步结果失败")
		return
	}
	task.ResultFilePath = resultPath
	task.Status = model.AsyncRelayTaskStatusSucceeded
	if _, err := task.UpdateWithStatus(model.AsyncRelayTaskStatusProcessing); err != nil {
		_ = common.RemoveDiskCacheFile(resultPath)
	}
}

func failAsyncRelayTask(task *model.AsyncRelayTask, reason string) {
	if task == nil {
		return
	}
	task.Error = reason
	task.Status = model.AsyncRelayTaskStatusFailed
	_, _ = task.UpdateWithStatus(model.AsyncRelayTaskStatusProcessing)
}

func asyncRelayRequestBody(c *gin.Context) ([]byte, error) {
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, err
	}
	body, err := storage.Bytes()
	if err != nil {
		return nil, err
	}
	return append([]byte(nil), body...), nil
}

// copyAsyncRelayBodyToFile 将 multipart 请求体直接复制到任务文件，避免把上传图片完整读入内存。
func copyAsyncRelayBodyToFile(c *gin.Context) (string, error) {
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return "", err
	}
	reader, err := storage.NewReader()
	if err != nil {
		return "", err
	}
	defer reader.Close()

	filePath, file, err := common.CreateDiskCacheFile(common.DiskCacheTypeFile)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(file, reader); err != nil {
		_ = file.Close()
		_ = common.RemoveDiskCacheFile(filePath)
		return "", err
	}
	if err := file.Close(); err != nil {
		_ = common.RemoveDiskCacheFile(filePath)
		return "", err
	}
	return filePath, nil
}

func isAsyncFlag(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func asyncFlagFromJSON(body []byte) bool {
	var payload map[string]any
	if len(body) == 0 || common.Unmarshal(body, &payload) != nil {
		return false
	}
	for _, key := range []string{"async", "background"} {
		if value, ok := payload[key]; ok {
			if flag, ok := value.(bool); ok && flag {
				return true
			}
			if text, ok := value.(string); ok && isAsyncFlag(text) {
				return true
			}
		}
	}
	return false
}

func stripAsyncJSONFlags(body []byte, contentType string) []byte {
	if !strings.HasPrefix(strings.ToLower(contentType), "application/json") {
		return body
	}
	var payload map[string]any
	if len(body) == 0 || common.Unmarshal(body, &payload) != nil {
		return body
	}
	delete(payload, "async")
	delete(payload, "background")
	cleaned, err := common.Marshal(payload)
	if err != nil {
		return body
	}
	return cleaned
}

func isGeminiImageRequest(c *gin.Context, body []byte) bool {
	values := []string{c.Request.URL.Path, c.GetString("original_model")}
	var payload map[string]any
	if common.Unmarshal(body, &payload) == nil {
		if modelValue, ok := payload["model"].(string); ok {
			values = append(values, modelValue)
		}
		values = append(values, strings.ToLower(string(body)))
	}
	for _, value := range values {
		value = strings.ToLower(value)
		if strings.Contains(value, "imagen") ||
			strings.Contains(value, "image-generation") ||
			strings.Contains(value, "-image") ||
			strings.Contains(value, "image-preview") ||
			strings.Contains(value, "banana") ||
			(strings.Contains(value, "responsemodalities") && strings.Contains(value, "image")) {
			return true
		}
	}
	return false
}

func isStreamingAsyncRequest(c *gin.Context, body []byte) bool {
	path := strings.ToLower(c.Request.URL.Path)
	if strings.Contains(path, "stream") {
		return true
	}
	if strings.Contains(strings.ToLower(c.GetHeader("Content-Type")), "multipart/form-data") {
		if form, err := common.ParseMultipartFormReusable(c); err == nil {
			defer form.RemoveAll()
			if values := form.Value["stream"]; len(values) > 0 && isAsyncFlag(values[0]) {
				return true
			}
		}
	}
	var payload map[string]any
	if common.Unmarshal(body, &payload) != nil {
		return false
	}
	if stream, ok := payload["stream"].(bool); ok {
		if stream {
			return true
		}
	}
	if stream, ok := payload["stream"].(string); ok && isAsyncFlag(stream) {
		return true
	}
	// Gemini 使用 alt=sse 返回事件流，异步任务统一保存完整 JSON 结果。
	if strings.EqualFold(c.Query("alt"), "sse") {
		return true
	}
	if strings.Contains(strings.ToLower(c.GetHeader("Accept")), "text/event-stream") {
		return true
	}
	return false
}

func asyncRequestHeaders(headers http.Header) (string, error) {
	values := make(map[string][]string)
	for key, items := range headers {
		switch strings.ToLower(key) {
		case "authorization", "cookie", "x-api-key", "x-goog-api-key", "mj-api-secret", "content-length":
			continue
		}
		values[key] = append([]string(nil), items...)
	}
	data, err := common.Marshal(values)
	return string(data), err
}

func asyncModelName(body []byte) string {
	var payload map[string]any
	if common.Unmarshal(body, &payload) != nil {
		return ""
	}
	modelName, _ := payload["model"].(string)
	return modelName
}

func removeAsyncQuery(rawQuery string) string {
	query, err := url.ParseQuery(rawQuery)
	if err != nil {
		return rawQuery
	}
	query.Del("async")
	query.Del("background")
	return query.Encode()
}

func asyncRelayObjectForFormat(format relaytypes.RelayFormat) string {
	if format == relaytypes.RelayFormatGemini {
		return "gemini_image_generation"
	}
	return asyncRelayObject
}
