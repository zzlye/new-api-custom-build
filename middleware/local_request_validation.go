package middleware

import (
	"errors"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// CheckLocalRequestParameters 在选定分组内阻止非法参数进入入队、扣费和上游重试流程。
func CheckLocalRequestParameters(c *gin.Context) bool {
	group := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	if group == "auto" && common.GetContextKeyString(c, constant.ContextKeyAutoGroup) != "" {
		group = common.GetContextKeyString(c, constant.ContextKeyAutoGroup)
	}
	status := operation_setting.LocalRequestValidationStatus(group)
	if status == 0 || c.GetBool("local_request_parameters_validated") {
		return false
	}
	err := helper.ValidateLocalRequest(c)
	if err == nil {
		c.Set("local_request_parameters_validated", true)
		return false
	}
	// 请求体大小限制仍保留 413，不能被分组自定义状态码覆盖。
	if common.IsRequestBodyTooLargeError(err) || errors.Is(err, common.ErrRequestBodyTooLarge) {
		status = http.StatusRequestEntityTooLarge
	}
	const code = "local_parameter_error"
	message := "本地参数校验失败：" + err.Error()
	state := service.RequestPolicy(c)
	state.AddEvent(service.PolicyEvent{Status: status, ErrorCode: code, Decision: service.PolicyDecision{Action: "stop", Reason: code, Source: "local_validation"}, Health: "unchanged"})
	state.FinalLogged = true
	other := model.NewLogOther()
	other.SetPublic("status_code", status)
	other.SetPublic("error_code", code)
	other.SetPublic("local_validation", true)
	modelName := common.GetContextKeyString(c, constant.ContextKeyOriginalModel)
	if modelName == "" && strings.Contains(c.Request.URL.Path, "/models/") {
		modelName = extractModelNameFromGeminiPath(c.Request.URL.Path)
	}
	if modelName == "" {
		if storage, readErr := common.GetBodyStorage(c); readErr == nil {
			if body, readErr := storage.Bytes(); readErr == nil {
				name := gjson.GetBytes(body, "model")
				if name.Type == gjson.String && len(name.Str) <= 255 {
					modelName = name.Str
				}
			}
		}
	}
	// 拒绝请求留在现有错误日志中；不保存请求体、素材或密钥，也不生成空任务。
	model.RecordErrorLog(c, c.GetInt("id"), 0, modelName, c.GetString("token_name"), message, c.GetInt("token_id"), 0, false, group, other)
	if c.Request.URL.Path == "/v1/messages" {
		c.AbortWithStatusJSON(status, gin.H{"type": "error", "error": gin.H{"type": "invalid_request_error", "message": common.MessageWithRequestId(message, c.GetString(common.RequestIdKey))}})
	} else {
		abortWithOpenAiMessage(c, status, message, types.ErrorCode(code))
	}
	return true
}
