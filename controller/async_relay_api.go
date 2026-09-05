package controller

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// loadAsyncRelayTask 同时校验登录用户和记录归属，管理查看权限只来自后台认证上下文。
func loadAsyncRelayTask(c *gin.Context) *model.AsyncRelayTask {
	taskID := c.Param("async_task_id")
	if taskID == "" {
		taskID = c.Param("task_id")
	}
	task, err := model.GetAsyncRelayTaskByTaskID(taskID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": "读取任务失败"}})
		return nil
	}
	if task == nil || (task.UserID != c.GetInt("id") && c.GetInt("role") < common.RoleAdminUser) {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"message": "任务不存在"}})
		return nil
	}
	return task
}

// asyncRelayMediaLinks 不暴露文件路径，所有预览均经过带归属校验的内容接口。
func asyncRelayMediaLinks(task *model.AsyncRelayTask, prefix string) []dto.TaskMedia {
	if model.AsyncRelayTaskExpired(task, common.GetTimestamp()) {
		return nil
	}
	var files []model.AsyncRelayMedia
	if common.Unmarshal([]byte(task.ResultFiles), &files) != nil {
		return nil
	}
	media := make([]dto.TaskMedia, 0, len(files))
	for index, file := range files {
		media = append(media, dto.TaskMedia{URL: prefix + task.TaskID + "/media/" + strconv.Itoa(index), Kind: file.Kind, ContentType: file.ContentType})
	}
	return media
}

// GetAsyncRelayTask 返回状态和完整生成结果；文件到期后保留成功状态并明确标识已过期。
func GetAsyncRelayTask(c *gin.Context) {
	task := loadAsyncRelayTask(c)
	if task == nil {
		return
	}
	c.Header("Cache-Control", "private, no-store")
	c.Header("Retry-After", "3")
	expired := model.AsyncRelayTaskExpired(task, common.GetTimestamp())
	response := gin.H{"id": task.TaskID, "task_id": task.TaskID, "object": asyncRelayObjectForFormat(relaytypes.RelayFormat(task.RequestFormat)),
		"status": task.Status, "created": task.CreatedAt, "updated": task.UpdatedAt, "model": task.ModelName,
		"poll_url": "/v1/tasks/" + task.TaskID, "result_expired": expired,
	}
	if task.FinishedAt > 0 {
		response["expires_at"] = task.FinishedAt + common.AsyncMediaRetentionSeconds()
	}
	if !expired && (task.Status == model.AsyncRelayTaskStatusSucceeded || (task.Status == model.AsyncRelayTaskStatusFailed && task.ResultFilePath != "")) {
		media := asyncRelayMediaLinks(task, "/v1/tasks/")
		response["media"] = media
		response["response_status_code"] = task.ResponseStatusCode
		response["content_type"] = task.ResultContentType
		if strings.Contains(task.ResultContentType, "json") && task.ResultFilePath != "" {
			result, err := os.ReadFile(task.ResultFilePath)
			if err != nil {
				c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"message": "当前节点尚未读取到生成结果，请稍后重试"}})
				return
			}
			if common.ValidJson(result) {
				response["result"] = json.RawMessage(result)
			}
		} else {
			response["result"] = gin.H{"media": media}
		}
	}
	if task.Status == model.AsyncRelayTaskStatusFailed || task.Status == model.AsyncRelayTaskStatusCancelled {
		response["error"] = gin.H{"message": task.Error}
	}
	c.JSON(http.StatusOK, response)
}

// GetAsyncRelayMedia 支持图片展示和视频范围读取，过期结果统一返回 410。
func GetAsyncRelayMedia(c *gin.Context) {
	task := loadAsyncRelayTask(c)
	if task == nil {
		return
	}
	serveAsyncRelayMedia(c, task, c.Param("index"))
}

func serveAsyncRelayMedia(c *gin.Context, task *model.AsyncRelayTask, indexValue string) {
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	if model.AsyncRelayTaskExpired(task, common.GetTimestamp()) {
		c.JSON(http.StatusGone, gin.H{"error": gin.H{"message": "生成文件已过期，任务日志仍保留"}})
		return
	}
	if task.Status != model.AsyncRelayTaskStatusSucceeded {
		c.JSON(http.StatusConflict, gin.H{"error": gin.H{"message": "任务尚未完成"}})
		return
	}
	index, err := strconv.Atoi(indexValue)
	var files []model.AsyncRelayMedia
	if err != nil || index < 0 || common.Unmarshal([]byte(task.ResultFiles), &files) != nil || index >= len(files) {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"message": "生成文件不存在"}})
		return
	}
	media := files[index]
	file, err := os.Open(media.Path)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"message": "生成文件不存在"}})
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": "读取生成文件失败"}})
		return
	}
	c.Header("Content-Type", media.ContentType)
	http.ServeContent(c.Writer, c.Request, "generated-media", info.ModTime(), file)
}

// DeleteTaskLog 仅根用户能删除日志及其生成文件，运行中的任务先保留以保障扣费与退款一致。
func DeleteTaskLog(c *gin.Context) {
	if c.GetInt("role") != common.RoleRootUser {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "仅根用户可以删除任务日志"})
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "任务日志编号有误"})
		return
	}
	var log model.Task
	err = model.DB.First(&log, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "任务日志不存在"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "读取任务日志失败"})
		return
	}
	if log.AsyncParentID != "" {
		c.JSON(http.StatusConflict, gin.H{"success": false, "message": "请删除对应的主任务日志"})
		return
	}
	if log.Status != model.TaskStatusSuccess && log.Status != model.TaskStatusFailure {
		c.JSON(http.StatusConflict, gin.H{"success": false, "message": "请等待任务结束后再删除日志"})
		return
	}
	task, err := model.GetAsyncRelayTaskByTaskID(log.TaskID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "读取后台任务失败"})
		return
	}
	if task != nil {
		if !task.Status.IsTerminal() {
			c.JSON(http.StatusConflict, gin.H{"success": false, "message": "请等待任务结束后再删除日志"})
			return
		}
		if err := model.ExpireAsyncRelayTaskFiles(task); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "清理生成文件失败，请在文件所属节点重试"})
			return
		}
	}
	err = model.DB.Transaction(func(tx *gorm.DB) error {
		if task != nil {
			if err := tx.Where("async_parent_id = ?", task.TaskID).Delete(&model.Task{}).Error; err != nil {
				return err
			}
			if task.RequestFormat == string(relaytypes.RelayFormatMjProxy) && task.LinkedTaskID != "" {
				if err := tx.Where("user_id = ? AND async_parent_id = ?", task.UserID, task.TaskID).Delete(&model.Midjourney{}).Error; err != nil {
					return err
				}
			}
			if err := tx.Delete(&model.AsyncRelayTask{}, task.ID).Error; err != nil {
				return err
			}
		}
		return tx.Where("id = ? AND status IN ?", log.ID, []model.TaskStatus{model.TaskStatusSuccess, model.TaskStatusFailure}).Delete(&model.Task{}).Error
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "删除任务日志失败"})
		return
	}
	common.ApiSuccess(c, nil)
}

// MidjourneyImageProxy 保留历史图片入口，新后台任务改用归属校验和统一到期规则。
func MidjourneyImageProxy(c *gin.Context) {
	child := model.GetByOnlyMJId(c.Param("id"))
	if child == nil || child.AsyncParentID == "" {
		relay.RelayMidjourneyImage(c)
		return
	}
	middleware.TokenOrUserAuth()(c)
	if c.IsAborted() {
		return
	}
	c.Params = append(c.Params, gin.Param{Key: "async_task_id", Value: child.AsyncParentID})
	task := loadAsyncRelayTask(c)
	if task == nil {
		return
	}
	if model.AsyncRelayTaskExpired(task, common.GetTimestamp()) || task.Status == model.AsyncRelayTaskStatusSucceeded {
		serveAsyncRelayMedia(c, task, "0")
		return
	}
	// 后台文件尚在保存时保持原生预览可用，归属校验已在上面完成。
	relay.RelayMidjourneyImage(c)
}
