package controller

import (
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// GetAsyncMediaRetryChannels 仅提供 root 勾选所需的摘要，不返回渠道凭证。
func GetAsyncMediaRetryChannels(c *gin.Context) {
	if c.GetInt("role") != common.RoleRootUser {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "仅根用户可以查看媒体重试渠道设置"})
		return
	}
	var channels []struct {
		ID     int    `json:"id"`
		Name   string `json:"name"`
		Status int    `json:"status"`
	}
	if err := model.DB.Model(&model.Channel{}).Select("id", "name", "status").Order("id desc").Scan(&channels).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, channels)
}

// persistAsyncRelayRouting 使用原有详情字段保存非敏感路由摘要，不引入新表或改变文件到期规则。
func persistAsyncRelayRouting(task *model.AsyncRelayTask, c *gin.Context) error {
	var details model.AsyncRelayRequestDetails
	if task.RequestDetails != "" {
		if err := common.UnmarshalJsonStr(task.RequestDetails, &details); err != nil {
			return fmt.Errorf("读取任务路由详情失败")
		}
	}
	encoded, err := common.Marshal(service.RequestPolicy(c).Events())
	if err != nil {
		return err
	}
	if err := common.Unmarshal(encoded, &details.RoutingEvents); err != nil {
		return err
	}
	encoded, err = common.Marshal(details)
	if err != nil {
		return err
	}
	result := model.DB.Model(&model.AsyncRelayTask{}).Where("id = ? AND status = ? AND worker_id = ?", task.ID, model.AsyncRelayTaskStatusProcessing, task.WorkerID).Update("request_details", string(encoded))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		// MySQL 的相同内容更新可能返回零，需区分无变化和执行租约已经失效。
		var count int64
		if err := model.DB.Model(&model.AsyncRelayTask{}).Where("id = ? AND status = ? AND worker_id = ?", task.ID, model.AsyncRelayTaskStatusProcessing, task.WorkerID).Count(&count).Error; err != nil {
			return err
		}
		if count != 1 {
			return fmt.Errorf("任务执行归属已变化，停止换渠道")
		}
	}
	task.RequestDetails = string(encoded)
	return nil
}
