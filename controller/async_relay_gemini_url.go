package controller

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
)

// isGeminiImageURLTask 读取提交时的选择，不依赖查询时的全局开关或请求参数。
func isGeminiImageURLTask(task *model.AsyncRelayTask) bool {
	if task.RequestFormat != string(relaytypes.RelayFormatGemini) {
		return false
	}
	var metadata asyncRelayMetadata
	return common.UnmarshalJsonStr(task.RequestMetadata, &metadata) == nil && metadata.GeminiImageURL
}

// geminiImageURLBaseAddress 只使用管理员设置的公开地址，避免把请求头中的任意主机写入图片链接。
func geminiImageURLBaseAddress() string {
	if address := strings.TrimSpace(system_setting.TaskPublicAddress); address != "" {
		return address
	}
	return strings.TrimSpace(system_setting.ServerAddress)
}

// geminiImageURLResponse 保留 Gemini 图片结果结构，以文件链接代替内嵌图片和大签名。
func geminiImageURLResponse(task *model.AsyncRelayTask) (gin.H, error) {
	if task.Status != model.AsyncRelayTaskStatusSucceeded || model.AsyncRelayTaskExpired(task, common.GetTimestamp()) {
		return nil, fmt.Errorf("生成图片尚未就绪或已过期")
	}
	base := geminiImageURLBaseAddress()
	if err := service.ValidateTaskArtifactBaseURL(base); err != nil {
		return nil, fmt.Errorf("任务公开地址或服务器地址配置无效")
	}
	var files []model.AsyncRelayMedia
	if err := common.UnmarshalJsonStr(task.ResultFiles, &files); err != nil {
		return nil, fmt.Errorf("读取生成图片清单失败")
	}
	parts := make([]gin.H, 0, len(files))
	for index, file := range files {
		if file.Kind != "image" {
			continue
		}
		parts = append(parts, gin.H{"fileData": gin.H{"mimeType": file.ContentType,
			"fileUri": strings.TrimRight(base, "/") + asyncRelayMediaPreviewURL(task, "media", index)}})
	}
	if len(parts) == 0 {
		return nil, fmt.Errorf("生成结果中没有可返回的图片")
	}
	return gin.H{"candidates": []gin.H{{"content": gin.H{"role": "model", "parts": parts}, "finishReason": "STOP"}}}, nil
}

// serveGeminiImageURLTask 等归档完成后返回链接，客户端断开只结束等待，不取消后台生成。
func serveGeminiImageURLTask(c *gin.Context, taskID string) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		task, err := model.GetAsyncRelayTaskByTaskID(taskID)
		if err != nil || task == nil {
			return fmt.Errorf("读取生成任务失败")
		}
		if task.Status == model.AsyncRelayTaskStatusSucceeded {
			result, err := geminiImageURLResponse(task)
			if err != nil {
				return err
			}
			c.Header("Cache-Control", "private, no-store")
			c.JSON(http.StatusOK, result)
			return nil
		}
		if task.Status.IsTerminal() {
			status := task.ResponseStatusCode
			if status < 400 || status > 599 {
				status = http.StatusBadGateway
			}
			c.JSON(status, gin.H{"error": gin.H{"message": task.Error, "type": "generation_error"}})
			return nil
		}
		select {
		case <-c.Request.Context().Done():
			return nil
		case <-ticker.C:
		}
	}
}
