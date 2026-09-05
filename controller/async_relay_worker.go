package controller

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

var asyncRelaySlots = make(chan struct{}, asyncRelayMaxBatch)
var asyncRelayWakeup = make(chan struct{}, 1)
var asyncRelayStart sync.Once

// WakeAsyncRelayWorkers 合并并发提交的唤醒信号，避免每个请求都创建后台执行线程。
func WakeAsyncRelayWorkers() {
	select {
	case asyncRelayWakeup <- struct{}{}:
	default:
	}
}

// StartAsyncRelayWorkers 每个节点独立恢复本地队列，主从节点都能继续持有文件的任务。
func StartAsyncRelayWorkers() {
	asyncRelayStart.Do(func() {
		gopool.Go(func() {
			ticker := time.NewTicker(5 * time.Second)
			defer ticker.Stop()
			var lastOrphanCleanup time.Time
			for {
				if time.Since(lastOrphanCleanup) >= 5*time.Minute {
					if err := model.CleanupOrphanAsyncMediaFiles(); err != nil {
						common.SysError("清理未关联媒体文件失败: " + err.Error())
					}
					lastOrphanCleanup = time.Now()
				}
				if _, err := model.RecoverStaleAsyncRelayTasks(); err != nil {
					common.SysError("恢复后台媒体任务失败: " + err.Error())
				}
				if err := model.CleanupExpiredAsyncRelayTasks(common.NodeName); err != nil {
					common.SysError("清理到期媒体文件失败: " + err.Error())
				}
				for i := 0; i < asyncRelayMaxBatch; i++ {
					gopool.Go(func() {
						if _, err := ProcessAsyncRelayTasks(context.Background(), 1); err != nil {
							common.SysError("执行后台媒体任务失败: " + err.Error())
						}
					})
				}
				select {
				case <-ticker.C:
				case <-asyncRelayWakeup:
				}
			}
		})
	})
}

// ProcessAsyncRelayTasks 限制全进程同时执行的任务数，排队压力不会变成无限并发上游请求。
func ProcessAsyncRelayTasks(ctx context.Context, limit int) (int, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case asyncRelaySlots <- struct{}{}:
		defer func() { <-asyncRelaySlots }()
	default:
		return 0, nil
	}
	if limit <= 0 || limit > asyncRelayMaxBatch {
		limit = asyncRelayMaxBatch
	}
	processed := 0
	for i := 0; i < limit; i++ {
		if ctx.Err() != nil {
			return processed, ctx.Err()
		}
		workerID := fmt.Sprintf("%s-%d", common.NodeName, time.Now().UnixNano())
		task, err := model.ClaimAsyncRelayTaskForNode(common.NodeName, workerID)
		if err != nil || task == nil {
			return processed, err
		}
		processed++
		processAsyncRelayTask(ctx, task)
	}
	return processed, nil
}

// asyncRelayFileWriter 将上游响应直接落盘，并在超过单文件上限时停止写入。
type asyncRelayFileWriter struct {
	file   *os.File
	header http.Header
	status int
	size   int64
	err    error
}

func (writer *asyncRelayFileWriter) Header() http.Header { return writer.header }
func (writer *asyncRelayFileWriter) WriteHeader(status int) {
	if writer.status == 0 {
		writer.status = status
	}
}
func (writer *asyncRelayFileWriter) Write(data []byte) (int, error) {
	if writer.status == 0 {
		writer.status = http.StatusOK
	}
	if writer.err != nil {
		return 0, writer.err
	}
	if int64(len(data)) > common.AsyncMediaMaxFileBytes-writer.size {
		writer.err = fmt.Errorf("生成结果超过单文件保存上限")
		return 0, writer.err
	}
	n, err := writer.file.Write(data)
	writer.size += int64(n)
	writer.err = err
	return n, err
}
func (writer *asyncRelayFileWriter) Flush() {}

func processAsyncRelayTask(parent context.Context, task *model.AsyncRelayTask) {
	ctx, cancel := context.WithTimeout(parent, 30*time.Minute)
	defer cancel()
	// 心跳续租只更新当前执行者的任务，慢生成不会被另一个执行者重复领取。
	heartbeatDone := make(chan struct{})
	defer close(heartbeatDone)
	gopool.Go(func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeatDone:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				result := model.DB.Model(&model.AsyncRelayTask{}).Where("id = ? AND status = ? AND worker_id = ?", task.ID, model.AsyncRelayTaskStatusProcessing, task.WorkerID).Update("updated_at", common.GetTimestamp())
				if result.Error != nil || result.RowsAffected == 0 {
					cancel()
					return
				}
			}
		}
	})
	defer func() {
		if recovered := recover(); recovered != nil {
			failAsyncRelayTask(task, fmt.Sprintf("后台任务执行异常: %v", recovered))
		}
		if task.Status != model.AsyncRelayTaskStatusProcessing && task.RequestFilePath != "" {
			if err := common.RemoveAsyncMediaFile(task.RequestFilePath); err == nil {
				_ = model.DB.Model(&model.AsyncRelayTask{}).Where("id = ?", task.ID).Update("request_file_path", "").Error
			}
		}
	}()
	if task.ResultFilePath != "" {
		completeAsyncRelayResult(ctx, task, task.ResultFilePath, task.ResultContentType)
		return
	}
	if task.LinkedTaskID != "" {
		pollLinkedAsyncRelayTask(ctx, task)
		return
	}
	requestFile, err := os.Open(task.RequestFilePath)
	if err != nil {
		failAsyncRelayTask(task, "读取已保存请求失败")
		return
	}
	defer requestFile.Close()
	info, err := requestFile.Stat()
	if err != nil {
		failAsyncRelayTask(task, "读取请求文件信息失败")
		return
	}
	responsePath, responseFile, err := common.CreateAsyncMediaFile()
	if err != nil {
		failAsyncRelayTask(task, "创建结果文件失败")
		return
	}
	keepResponse := false
	defer func() {
		_ = responseFile.Close()
		if !keepResponse {
			_ = common.RemoveAsyncMediaFile(responsePath)
		}
	}()
	writer := &asyncRelayFileWriter{file: responseFile, header: make(http.Header)}
	worker, _ := gin.CreateTestContext(writer)
	defer common.CleanupBodyStorage(worker)
	var metadata asyncRelayMetadata
	if task.RequestMetadata != "" && common.Unmarshal([]byte(task.RequestMetadata), &metadata) != nil {
		failAsyncRelayTask(task, "恢复请求参数失败")
		return
	}
	worker.Params = metadata.Params
	// Gemini 的路由参数也要同步切换，避免处理器再次选中流式接口。
	for i := range worker.Params {
		worker.Params[i].Value = strings.ReplaceAll(worker.Params[i].Value, ":streamGenerateContent", ":generateContent")
	}
	requestURL := task.RequestPath
	if task.RequestQuery != "" {
		requestURL += "?" + task.RequestQuery
	}
	request, err := http.NewRequestWithContext(ctx, task.RequestMethod, requestURL, requestFile)
	if err != nil {
		failAsyncRelayTask(task, "恢复请求地址失败")
		return
	}
	request.ContentLength, request.Host = info.Size(), "async-worker"
	request.RemoteAddr = net.JoinHostPort(metadata.ClientIP, "0")
	if task.RequestFiles != "" {
		_ = common.Unmarshal([]byte(task.RequestFiles), &request.Header)
	}
	request.Header.Set("Content-Type", task.RequestContentType)
	request.Header.Set("Accept", "application/json")
	token, err := model.GetTokenById(task.TokenID)
	if err != nil || token == nil || token.UserId != task.UserID {
		failAsyncRelayTask(task, "任务令牌已失效")
		return
	}
	credential := "Bearer sk-" + token.Key
	if metadata.SpecificChannel != "" {
		credential += "-" + metadata.SpecificChannel
	}
	request.Header.Set("Authorization", credential)
	worker.Request = request
	worker.Set(model.AsyncRelayContextKey, task.TaskID)
	worker.Set(common.RequestIdKey, task.TaskID)
	common.SetContextKey(worker, constant.ContextKeyRequestStartTime, time.Now())
	if metadata.Action != "" {
		worker.Set("action", metadata.Action)
	}
	if metadata.RelayMode != 0 {
		worker.Set("relay_mode", metadata.RelayMode)
	}
	// 在执行时重新校验令牌状态、额度、模型范围和客户端地址，不沿用提交时的过期权限。
	middleware.TokenAuth()(worker)
	if worker.IsAborted() {
		failAsyncRelayTask(task, "执行时用户或令牌状态已变更")
		return
	}
	if err := resolveAsyncRelayReferences(worker, task); err != nil {
		failAsyncRelayTask(task, err.Error())
		return
	}
	middleware.Distribute()(worker)
	if worker.IsAborted() {
		failAsyncRelayTask(task, "执行时模型或分组渠道不可用")
		return
	}
	task.DispatchStartedAt = common.GetTimestamp()
	// 发往上游前持久化标记，进程意外退出时对结果未知的请求不做自动重发。
	marked := model.DB.Model(&model.AsyncRelayTask{}).Where("id = ? AND status = ? AND worker_id = ?", task.ID, model.AsyncRelayTaskStatusProcessing, task.WorkerID).Update("dispatch_started_at", task.DispatchStartedAt)
	if marked.Error != nil || marked.RowsAffected != 1 {
		failAsyncRelayTask(task, "保存执行状态失败")
		return
	}
	switch relaytypes.RelayFormat(task.RequestFormat) {
	case relaytypes.RelayFormatTask:
		RelayTask(worker)
	case relaytypes.RelayFormatMjProxy:
		RelayMidjourney(worker)
	default:
		Relay(worker, relaytypes.RelayFormat(task.RequestFormat))
	}
	logFields := map[string]any{"channel_id": worker.GetInt("channel_id")}
	if quota, exists := worker.Get("async_relay_settled_quota"); exists {
		logFields["quota"] = quota
	}
	_ = model.DB.Model(&model.Task{}).Where("id = ?", task.LogID).Updates(logFields).Error
	task.ResponseStatusCode, task.ResponseContentType = worker.Writer.Status(), writer.header.Get("Content-Type")
	if writer.err != nil {
		failAsyncRelayTask(task, writer.err.Error())
		return
	}
	if persistError := worker.GetString("async_relay_persist_error"); persistError != "" {
		failAsyncRelayTask(task, persistError)
		return
	}
	if task.ResponseStatusCode >= 400 {
		failAsyncRelayTask(task, fmt.Sprintf("生成请求失败（HTTP %d）", task.ResponseStatusCode))
		return
	}
	if err := responseFile.Sync(); err != nil {
		failAsyncRelayTask(task, "保存生成结果失败")
		return
	}
	if err := responseFile.Close(); err != nil {
		failAsyncRelayTask(task, "关闭生成结果文件失败")
		return
	}
	linked, err := model.GetAsyncRelayTaskByTaskID(task.TaskID)
	if err != nil {
		failAsyncRelayTask(task, "读取上游任务关联失败")
		return
	}
	if linked != nil {
		task.LinkedTaskID = linked.LinkedTaskID
	}
	if task.LinkedTaskID != "" {
		task.Status = model.AsyncRelayTaskStatusWaiting
		if _, err := task.UpdateWithStatus(model.AsyncRelayTaskStatusProcessing); err != nil {
			common.SysError("保存后台任务等待状态失败: " + err.Error())
		}
		return
	}
	// 同步媒体响应已完整落盘，随后提取可预览的本地文件。
	keepResponse = completeAsyncRelayResult(ctx, task, responsePath, task.ResponseContentType)
}

func failAsyncRelayTask(task *model.AsyncRelayTask, message string) {
	if message == "" {
		message = "生成任务失败，请查看调用日志"
	}
	task.Status, task.Error = model.AsyncRelayTaskStatusFailed, message
	task.FinishedAt, task.UpdatedAt = common.GetTimestamp(), common.GetTimestamp()
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		// 仅修改终态字段，避免把刚落库的上游关联或结果检查点覆盖为空。
		result := tx.Model(&model.AsyncRelayTask{}).Where("id = ? AND status = ? AND worker_id = ?", task.ID, model.AsyncRelayTaskStatusProcessing, task.WorkerID).
			Updates(map[string]any{"status": task.Status, "error": message, "finished_at": task.FinishedAt, "updated_at": task.UpdatedAt, "media_attempts": task.MediaAttempts})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			task.Status = model.AsyncRelayTaskStatusProcessing
			return nil
		}
		return task.SyncLog(tx)
	})
	if err != nil {
		common.SysError("保存任务失败状态失败: " + err.Error())
	}
}

// resolveAsyncRelayReferences 将对外返回的任务编号解析为本用户的上游任务编号。
func resolveAsyncRelayReferences(c *gin.Context, task *model.AsyncRelayTask) error {
	for i := range c.Params {
		if c.Params[i].Key != "video_id" || !strings.HasPrefix(c.Params[i].Value, "async_") {
			continue
		}
		origin, err := model.GetAsyncRelayTaskByUserAndTaskID(task.UserID, c.Params[i].Value)
		if err != nil {
			return err
		}
		if origin == nil || origin.LinkedTaskID == "" || origin.Status != model.AsyncRelayTaskStatusSucceeded {
			return fmt.Errorf("引用的生成任务尚未完成或不属于当前用户")
		}
		c.Request.URL.Path = strings.Replace(c.Request.URL.Path, c.Params[i].Value, origin.LinkedTaskID, 1)
		c.Params[i].Value = origin.LinkedTaskID
	}
	return nil
}

// pollLinkedAsyncRelayTask 原生视频和绘图继续复用现有轮询、退款和差额结算流程。
func pollLinkedAsyncRelayTask(ctx context.Context, task *model.AsyncRelayTask) {
	var urls []string
	if task.RequestFormat == string(relaytypes.RelayFormatTask) {
		child, exists, err := model.GetByTaskId(task.UserID, task.LinkedTaskID)
		if err != nil {
			failAsyncRelayTask(task, "查询原生视频任务失败")
			return
		}
		if !exists {
			failAsyncRelayTask(task, "原生视频任务记录缺失")
			return
		}
		_ = model.DB.Model(&model.Task{}).Where("id = ?", task.LogID).Updates(map[string]any{"quota": child.Quota, "channel_id": child.ChannelId}).Error
		if child.Status == model.TaskStatusFailure {
			failAsyncRelayTask(task, child.FailReason)
			return
		}
		if child.Status != model.TaskStatusSuccess {
			task.Status = model.AsyncRelayTaskStatusWaiting
			_, _ = task.UpdateWithStatus(model.AsyncRelayTaskStatusProcessing)
			return
		}
		path, file, err := common.CreateAsyncMediaFile()
		if err != nil {
			failAsyncRelayTask(task, "创建视频结果文件失败")
			return
		}
		writer := &asyncRelayFileWriter{file: file, header: make(http.Header)}
		c, _ := gin.CreateTestContext(writer)
		c.Request, _ = http.NewRequestWithContext(ctx, http.MethodGet, "/v1/videos/"+child.TaskID+"/content", nil)
		c.Params = gin.Params{{Key: "task_id", Value: child.TaskID}}
		c.Set("id", task.UserID)
		c.Set(model.AsyncRelayContextKey, task.TaskID)
		VideoProxy(c)
		syncErr := file.Sync()
		closeErr := file.Close()
		if writer.err != nil || syncErr != nil || closeErr != nil || writer.status != http.StatusOK || writer.size == 0 || c.GetBool("video_proxy_stream_error") {
			_ = common.RemoveAsyncMediaFile(path)
			retryAsyncRelayMedia(task, "视频已生成，保存视频文件失败")
			return
		}
		contentType := writer.header.Get("Content-Type")
		if contentType == "" || strings.HasPrefix(contentType, "application/octet-stream") {
			contentType = "video/mp4"
		}
		if !completeAsyncRelayResult(ctx, task, path, contentType) {
			_ = common.RemoveAsyncMediaFile(path)
		}
		return
	}
	child := model.GetByMJId(task.UserID, task.TaskID)
	if child == nil {
		failAsyncRelayTask(task, "绘图任务记录缺失")
		return
	}
	_ = model.DB.Model(&model.Task{}).Where("id = ?", task.LogID).Updates(map[string]any{"quota": child.Quota, "channel_id": child.ChannelId}).Error
	if child.Status == "FAILURE" {
		failAsyncRelayTask(task, child.FailReason)
		return
	}
	if child.Status != "SUCCESS" {
		task.Status = model.AsyncRelayTaskStatusWaiting
		_, _ = task.UpdateWithStatus(model.AsyncRelayTaskStatusProcessing)
		return
	}
	if child.VideoUrls != "" {
		var values any
		if common.Unmarshal([]byte(child.VideoUrls), &values) == nil {
			var sources []asyncMediaSource
			collectAsyncMediaSources(values, &sources)
			for _, source := range sources {
				urls = append(urls, source.Value)
			}
		}
	}
	if child.VideoUrl != "" {
		urls = append(urls, child.VideoUrl)
	} else if len(urls) == 0 && child.ImageUrl != "" {
		urls = append(urls, child.ImageUrl)
	}
	result, _ := common.Marshal(gin.H{"data": urls})
	path, file, err := common.CreateAsyncMediaFile()
	if err != nil {
		failAsyncRelayTask(task, "创建绘图结果文件失败")
		return
	}
	_, err = file.Write(result)
	syncErr := file.Sync()
	closeErr := file.Close()
	if err != nil || syncErr != nil || closeErr != nil {
		_ = common.RemoveAsyncMediaFile(path)
		retryAsyncRelayMedia(task, "绘图已生成，保存结果文件失败")
		return
	}
	if !completeAsyncRelayResult(ctx, task, path, "application/json") {
		_ = common.RemoveAsyncMediaFile(path)
	}
}

// 确保文件响应写入器满足流式复制时使用的接口。
var _ http.ResponseWriter = (*asyncRelayFileWriter)(nil)
var _ io.Writer = (*asyncRelayFileWriter)(nil)
