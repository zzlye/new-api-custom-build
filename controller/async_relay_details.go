package controller

import (
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// GetAsyncRelayTaskDetails 把请求、参考图、结果和时间放在同一条记录中，列表本身不携带大段输入。
func GetAsyncRelayTaskDetails(c *gin.Context) {
	task := loadAsyncRelayTask(c)
	if task == nil {
		return
	}
	c.Header("Cache-Control", "private, no-store")
	// 旧记录只补来源信息，不重发生成请求，也不修改完成时间和保存期限。
	restoreAsyncRelayMediaURLs(task)
	expired := model.AsyncRelayTaskExpired(task, common.GetTimestamp())
	var input model.AsyncRelayRequestDetails
	available := task.RequestDetails != "" && common.UnmarshalJsonStr(task.RequestDetails, &input) == nil
	responseTime := task.ResponseCompletedAt
	// 旧任务未保存原始提示词时，只把上游返回的修订提示词作为补充展示，不冒充原输入。
	if !available && !expired && task.ResponseFilePath != "" && strings.Contains(task.ResponseContentType, "json") {
		if file, err := os.Open(task.ResponseFilePath); err == nil {
			var result struct {
				Data []struct {
					RevisedPrompt string `json:"revised_prompt"`
				} `json:"data"`
			}
			if common.DecodeJson(file, &result) == nil {
				var prompts []string
				for _, image := range result.Data {
					if image.RevisedPrompt != "" {
						prompts = append(prompts, image.RevisedPrompt)
					}
				}
				input.Prompt = strings.Join(prompts, "\n\n")
				if input.Prompt != "" {
					input.PromptSource = "upstream_revised"
				}
			}
			if info, err := file.Stat(); err == nil && responseTime == 0 {
				responseTime = info.ModTime().Unix()
			}
			_ = file.Close()
		}
	}
	references := make([]dto.TaskMedia, 0, len(input.References))
	for index, reference := range input.References {
		item := dto.TaskMedia{Name: reference.Name, Role: reference.Role, Kind: reference.Kind, ContentType: reference.ContentType, Error: reference.Error}
		if !expired && (reference.Path != "" || reference.Source != "") {
			item.URL = "/api/task/" + task.TaskID + "/reference/" + strconv.Itoa(index)
			item.PreviewURL = "/task-media/" + task.TaskID + "/reference/" + strconv.Itoa(index)
		}
		references = append(references, item)
	}
	response := dto.AsyncTaskDetails{TaskID: task.TaskID, ModelName: task.ModelName, RequestMethod: task.RequestMethod, RequestPath: task.RequestPath, RequestFormat: task.RequestFormat,
		Status: string(task.Status), Error: task.Error, Prompt: input.Prompt, PromptSource: input.PromptSource, InputAvailable: available, InputError: input.CaptureError,
		Parameters: input.Parameters, References: references, Media: asyncRelayMediaLinks(task, "/api/task/"), MediaExpired: expired,
		SubmitTime: task.CreatedAt, StartTime: task.StartedAt, ResponseTime: responseTime, FinishTime: task.FinishedAt, ResponseStatusCode: task.ResponseStatusCode}
	if task.FinishedAt > 0 {
		response.ExpiresAt = task.FinishedAt + common.AsyncMediaRetentionSeconds()
	}
	common.ApiSuccess(c, response)
}

// GetAsyncRelayReference 与生成文件使用相同的归属和到期规则，读取远程参考图时仍需通过地址校验。
func GetAsyncRelayReference(c *gin.Context) {
	task := loadAsyncRelayTask(c)
	if task == nil {
		return
	}
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	if model.AsyncRelayTaskExpired(task, common.GetTimestamp()) {
		c.JSON(http.StatusGone, gin.H{"error": gin.H{"message": "参考媒体已过期，任务文字记录仍保留"}})
		return
	}
	var input model.AsyncRelayRequestDetails
	index, err := strconv.Atoi(c.Param("index"))
	if err != nil || index < 0 || common.UnmarshalJsonStr(task.RequestDetails, &input) != nil || index >= len(input.References) {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"message": "参考媒体不存在"}})
		return
	}
	reference := input.References[index]
	if reference.Path == "" && reference.Source != "" {
		// 仅在用户查看时读取远程参考图；沿用地址校验和大小上限，不影响生图流程。
		media, err := saveAsyncMediaSource(c.Request.Context(), asyncMediaSource{Value: reference.Source})
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"message": "参考媒体读取失败"}})
			return
		}
		defer common.RemoveAsyncMediaFile(media.Path)
		reference.Path, reference.ContentType = media.Path, media.ContentType
	}
	file, err := os.Open(reference.Path)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"message": "参考媒体未保存或已清理"}})
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": "读取参考媒体失败"}})
		return
	}
	c.Header("Content-Type", reference.ContentType)
	http.ServeContent(c.Writer, c.Request, "reference-media", info.ModTime(), file)
}
