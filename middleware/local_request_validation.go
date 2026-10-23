package middleware

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

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

// upstreamErrorCooldownEntry 记录已触发上游确定性错误（如 400、524）的请求摘要与过期时间。
type upstreamErrorCooldownEntry struct {
	expiresAt time.Time
	message   string
}

var (
	upstreamErrorCooldownMu    sync.RWMutex
	upstreamErrorCooldownCache = make(map[string]upstreamErrorCooldownEntry)
)

// ClearUpstreamErrorCooldownCache 清空上游错误冷却缓存，供策略更新或测试重置时调用。
func ClearUpstreamErrorCooldownCache() {
	upstreamErrorCooldownMu.Lock()
	defer upstreamErrorCooldownMu.Unlock()
	clear(upstreamErrorCooldownCache)
}

// resolveValidationGroup 解析当前请求实际命中的分组名称（兼容 auto 自动分组）。
func resolveValidationGroup(c *gin.Context) string {
	group := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	if group == "auto" && common.GetContextKeyString(c, constant.ContextKeyAutoGroup) != "" {
		group = common.GetContextKeyString(c, constant.ContextKeyAutoGroup)
	}
	return group
}

// computeLocalRequestFingerprint 计算分组、请求路径与请求体内容的 SHA-256 指纹，用于短期拦截重复错误请求。
func computeLocalRequestFingerprint(c *gin.Context, group string) string {
	if c == nil || c.Request == nil || c.Request.Method != http.MethodPost || group == "" {
		return ""
	}
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return ""
	}
	body, err := storage.Bytes()
	if err != nil || len(body) == 0 {
		return ""
	}
	common.OptionMapRWMutex.RLock()
	policyRaw := common.OptionMap[operation_setting.LocalRequestValidationOption]
	common.OptionMapRWMutex.RUnlock()
	hasher := sha256.New()
	_, _ = hasher.Write([]byte(policyRaw))
	_, _ = hasher.Write([]byte{0})
	_, _ = hasher.Write([]byte(group))
	_, _ = hasher.Write([]byte{0})
	_, _ = hasher.Write([]byte(c.Request.URL.Path))
	_, _ = hasher.Write([]byte{0})
	_, _ = hasher.Write(body)
	return hex.EncodeToString(hasher.Sum(nil))
}

// lookupUpstreamErrorCooldown 查询当前请求指纹是否处于上游错误冷却拦截期内。
func lookupUpstreamErrorCooldown(fingerprint string) (upstreamErrorCooldownEntry, bool) {
	if fingerprint == "" {
		return upstreamErrorCooldownEntry{}, false
	}
	now := time.Now()
	upstreamErrorCooldownMu.RLock()
	entry, exists := upstreamErrorCooldownCache[fingerprint]
	upstreamErrorCooldownMu.RUnlock()
	if !exists {
		return upstreamErrorCooldownEntry{}, false
	}
	if !now.Before(entry.expiresAt) {
		upstreamErrorCooldownMu.Lock()
		if current, ok := upstreamErrorCooldownCache[fingerprint]; ok && !now.Before(current.expiresAt) {
			delete(upstreamErrorCooldownCache, fingerprint)
		}
		upstreamErrorCooldownMu.Unlock()
		return upstreamErrorCooldownEntry{}, false
	}
	return entry, true
}

// ObserveGroupUpstreamError 在上游返回匹配的错误（如 400、524）时停止重试并记录短期冷却缓存，防止高频重发触发 429 熔断。
func ObserveGroupUpstreamError(c *gin.Context, apiErr *types.NewAPIError) {
	if c == nil || apiErr == nil || apiErr.GetErrorType() == types.ErrorTypeNewAPIError {
		return
	}
	group := resolveValidationGroup(c)
	policy, ok := operation_setting.GetLocalRequestValidationPolicy()
	if !ok || policy.GroupStatus(group) == 0 {
		return
	}
	if !policy.MatchesUpstreamError(apiErr.StatusCode, string(apiErr.GetErrorType()), string(apiErr.GetErrorCode()), apiErr.Error()) {
		return
	}
	// 命中分组错误拦截条件后立即终止内部重试，避免同一坏请求继续打向其他 Gemini 渠道。
	types.ErrOptionWithSkipRetry()(apiErr)
	fingerprint := computeLocalRequestFingerprint(c, group)
	if fingerprint == "" {
		return
	}
	now := time.Now()
	expiresAt := now.Add(time.Duration(policy.CooldownSeconds()) * time.Second)
	upstreamErrorCooldownMu.Lock()
	if len(upstreamErrorCooldownCache) >= 10000 {
		for k, v := range upstreamErrorCooldownCache {
			if !now.Before(v.expiresAt) {
				delete(upstreamErrorCooldownCache, k)
			}
		}
	}
	if len(upstreamErrorCooldownCache) < 10000 {
		upstreamErrorCooldownCache[fingerprint] = upstreamErrorCooldownEntry{
			expiresAt: expiresAt,
			message:   apiErr.MaskSensitiveErrorWithStatusCode(),
		}
	}
	upstreamErrorCooldownMu.Unlock()
}

// CheckLocalRequestParameters 在选定分组内阻止非法参数或冷却期内的重复错误请求进入入队、扣费和上游重试流程。
func CheckLocalRequestParameters(c *gin.Context) bool {
	group := resolveValidationGroup(c)
	policy, ok := operation_setting.GetLocalRequestValidationPolicy()
	if !ok {
		return false
	}
	status := policy.GroupStatus(group)
	if status == 0 || c.GetBool("local_request_parameters_validated") {
		return false
	}
	if policy.CooldownSeconds() > 0 {
		fingerprint := computeLocalRequestFingerprint(c, group)
		if entry, cached := lookupUpstreamErrorCooldown(fingerprint); cached {
			const cooldownCode = "local_error_cooldown_intercepted"
			message := fmt.Sprintf("本地错误拦截：相同请求近期已触发上游错误（%s），冷却期内不再转发上游", entry.message)
			abortLocalValidationRequest(c, group, status, cooldownCode, message, true)
			return true
		}
	}
	err := helper.ValidateLocalRequestWithPolicy(c, policy)
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
	abortLocalValidationRequest(c, group, status, code, message, false)
	return true
}

// abortLocalValidationRequest 统一记录本地拦截决策与错误日志，并按请求协议格式返回客户端错误。
func abortLocalValidationRequest(c *gin.Context, group string, status int, code string, message string, cooldown bool) {
	state := service.RequestPolicy(c)
	state.AddEvent(service.PolicyEvent{Status: status, ErrorCode: code, Decision: service.PolicyDecision{Action: "stop", Reason: code, Source: "local_validation"}, Health: "unchanged"})
	state.FinalLogged = true
	other := model.NewLogOther()
	other.SetPublic("status_code", status)
	other.SetPublic("error_code", code)
	other.SetPublic("local_validation", true)
	if cooldown {
		other.SetPublic("cooldown_intercepted", true)
	}
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
}
