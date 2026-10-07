package controller

import (
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// GetAsyncRelayTaskDetails 把请求、参考素材、结果和时间放在同一条记录中，列表本身不携带大段输入。
func GetAsyncRelayTaskDetails(c *gin.Context) {
	task := loadAsyncRelayTask(c)
	if task == nil {
		return
	}
	c.Header("Cache-Control", "private, no-store")
	// 详情只返回本站文件入口，不再为展示上游地址读取或改写历史结果。
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
	fullReferences := make([]dto.AsyncTaskReferenceSnapshot, 0, len(input.References))
	for index, reference := range input.References {
		item := dto.TaskMedia{Name: reference.Name, Role: reference.Role, Kind: reference.Kind, ContentType: reference.ContentType, Error: reference.Error}
		if !expired && (reference.Path != "" || reference.Source != "") {
			item.URL = "/api/task/" + task.TaskID + "/reference/" + strconv.Itoa(index)
			item.PreviewURL = asyncRelayMediaPreviewURL(task, "reference", index)
		}
		references = append(references, item)
		fullReferences = append(fullReferences, dto.AsyncTaskReferenceSnapshot{Role: reference.Role, ContentType: reference.ContentType, Kind: reference.Kind})
	}
	var taskLog model.Task
	if task.LogID > 0 {
		_ = model.DB.First(&taskLog, task.LogID).Error
	}
	upstreamModelName := taskLog.Properties.UpstreamModelName
	channelID := taskLog.ChannelId
	if upstreamModelName == "" || channelID == 0 {
		var child model.Task
		if err := model.DB.Where("async_parent_id = ?", task.TaskID).Order("id asc").First(&child).Error; err == nil {
			if upstreamModelName == "" {
				upstreamModelName = child.Properties.UpstreamModelName
			}
			if channelID == 0 {
				channelID = child.ChannelId
			}
		}
	}
	createdBeijing := ""
	if task.CreatedAt > 0 {
		createdBeijing = time.Unix(task.CreatedAt, 0).In(time.FixedZone("Asia/Shanghai", 8*60*60)).Format(time.RFC3339)
	}
	fullParameters := dto.AsyncTaskParameterSnapshot{
		TaskID: task.TaskID, ModelName: task.ModelName, UpstreamModelName: upstreamModelName, ChannelID: channelID,
		RequestMethod: task.RequestMethod, RequestPath: task.RequestPath, RequestQuery: task.RequestQuery,
		RequestContentType: task.RequestContentType, RequestFormat: task.RequestFormat,
		RequestConversion: []string{asyncRequestConversionLabel(task.RequestFormat)},
		RequestDetails:    dto.AsyncTaskRequestSnapshot{Prompt: input.Prompt, PromptSource: input.PromptSource, Parameters: input.Parameters, References: fullReferences, CaptureError: input.CaptureError},
		CreatedAt:         task.CreatedAt, StartedAt: task.StartedAt, FinishedAt: task.FinishedAt, CreatedBeijing: createdBeijing,
		Status: string(task.Status), ResponseStatusCode: task.ResponseStatusCode, ResultExpiredAt: task.ResultExpiredAt,
	}
	response := dto.AsyncTaskDetails{TaskID: task.TaskID, ModelName: task.ModelName, RequestMethod: task.RequestMethod, RequestPath: task.RequestPath, RequestFormat: task.RequestFormat,
		Status: string(task.Status), Error: task.Error, Prompt: input.Prompt, PromptSource: input.PromptSource, InputAvailable: available, InputError: input.CaptureError,
		Parameters: input.Parameters, References: references, Media: asyncRelayMediaLinks(task, "/api/task/"), MediaExpired: expired,
		SubmitTime: task.CreatedAt, StartTime: task.StartedAt, ResponseTime: responseTime, FinishTime: task.FinishedAt, ResponseStatusCode: task.ResponseStatusCode}
	response.FullParameters = fullParameters
	if task.FinishedAt > 0 {
		response.ExpiresAt = task.FinishedAt + common.AsyncMediaRetentionSeconds()
	}
	// 渠道身份和路由过程只向管理员展示，用户仍仅看到自己的生成结果。
	if c.GetInt("role") >= common.RoleAdminUser {
		response.RoutingEvents = input.RoutingEvents
	}
	common.ApiSuccess(c, response)
}

// asyncRequestConversionLabel 将内部协议名转换成任务日志中可读的转换名称。
func asyncRequestConversionLabel(format string) string {
	switch format {
	case "gemini":
		return "Google Gemini"
	case "openai_image":
		return "OpenAI Image"
	case "openai_responses":
		return "OpenAI Responses"
	case "task":
		return "Task"
	case "mj_proxy":
		return "Midjourney"
	case "openai":
		return "OpenAI Compatible"
	case "claude":
		return "Claude Messages"
	default:
		return format
	}
}

// GetAsyncRelayReference 与生成文件使用相同的归属和到期规则，读取远程参考素材时仍需通过地址校验。
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
		// 仅在用户查看时读取远程素材；沿用地址校验和大小上限，不影响生成流程。
		media, err := saveAsyncMediaSource(c.Request.Context(), asyncMediaSource{Value: reference.Source, AllowAudio: true})
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
