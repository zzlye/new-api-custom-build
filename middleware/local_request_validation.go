package middleware

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"strconv"
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

// upstreamErrorCooldownEntry 记录已触发上游确定性错误（如 400、524）或 429 熔断的冷却状态与过期时间。
type upstreamErrorCooldownEntry struct {
	expiresAt  time.Time
	statusCode int
	message    string
}

var (
	upstreamErrorCooldownMu    sync.RWMutex
	upstreamErrorCooldownCache = make(map[string]upstreamErrorCooldownEntry)
	upstreamRateLimitCache     = make(map[string]upstreamErrorCooldownEntry)
	retrySecondsPattern        = regexp.MustCompile(`(?:请在\s*(\d+)\s*秒后重试|retry in\s*(\d+)\s*s|after\s*(\d+)\s*seconds)`)
)

// ClearUpstreamErrorCooldownCache 清空上游错误冷却缓存与 429 自动熔断缓存，供策略更新或测试重置时调用。
func ClearUpstreamErrorCooldownCache() {
	upstreamErrorCooldownMu.Lock()
	defer upstreamErrorCooldownMu.Unlock()
	clear(upstreamErrorCooldownCache)
	clear(upstreamRateLimitCache)
	helper.ClearGeminiThoughtSignatureCache()
}

// resolveValidationGroup 解析当前请求实际命中的分组名称（兼容 auto 自动分组）。
func resolveValidationGroup(c *gin.Context) string {
	group := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	if group == "auto" && common.GetContextKeyString(c, constant.ContextKeyAutoGroup) != "" {
		group = common.GetContextKeyString(c, constant.ContextKeyAutoGroup)
	}
	return group
}

// resolveValidationModelName 解析当前请求针对的模型名称。
func resolveValidationModelName(c *gin.Context) string {
	modelName := common.GetContextKeyString(c, constant.ContextKeyOriginalModel)
	if modelName == "" && c != nil && c.Request != nil && strings.Contains(c.Request.URL.Path, "/models/") {
		modelName = extractModelNameFromGeminiPath(c.Request.URL.Path)
	}
	if modelName == "" && c != nil {
		if storage, readErr := common.GetBodyStorage(c); readErr == nil {
			if body, readErr := storage.Bytes(); readErr == nil {
				name := gjson.GetBytes(body, "model")
				if name.Type == gjson.String && len(name.Str) <= 255 {
					modelName = name.Str
				}
			}
		}
	}
	return modelName
}

// computeUserModelRateLimitKey 计算用户、令牌、分组与模型维度的 429 临时熔断键。
func computeUserModelRateLimitKey(c *gin.Context, group, modelName string) string {
	if c == nil || group == "" {
		return ""
	}
	userID := c.GetInt("id")
	if userID == 0 {
		userID = common.GetContextKeyInt(c, constant.ContextKeyUserId)
	}
	common.OptionMapRWMutex.RLock()
	policyRaw := common.OptionMap[operation_setting.LocalRequestValidationOption]
	common.OptionMapRWMutex.RUnlock()
	return fmt.Sprintf("%s|%d|%d|%s|%s", policyRaw, userID, c.GetInt("token_id"), group, strings.ToLower(strings.TrimSpace(modelName)))
}

// computeLocalRequestFingerprint 按用户、分组、请求路径与请求体计算 SHA-256 指纹，确保不同用户的冷却拦截完全隔离。
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
	userID := c.GetInt("id")
	if userID == 0 {
		userID = common.GetContextKeyInt(c, constant.ContextKeyUserId)
	}
	common.OptionMapRWMutex.RLock()
	policyRaw := common.OptionMap[operation_setting.LocalRequestValidationOption]
	common.OptionMapRWMutex.RUnlock()
	hasher := sha256.New()
	_, _ = hasher.Write([]byte(policyRaw))
	_, _ = hasher.Write([]byte{0})
	_, _ = hasher.Write([]byte(strconv.Itoa(userID)))
	_, _ = hasher.Write([]byte{0})
	_, _ = hasher.Write([]byte(strconv.Itoa(c.GetInt("token_id"))))
	_, _ = hasher.Write([]byte{0})
	_, _ = hasher.Write([]byte(group))
	_, _ = hasher.Write([]byte{0})
	_, _ = hasher.Write([]byte(c.Request.URL.Path))
	_, _ = hasher.Write([]byte{0})
	_, _ = hasher.Write(body)
	return hex.EncodeToString(hasher.Sum(nil))
}

// extractUpstreamRetrySeconds 从上游 429 错误消息中提取需要等待的秒数；未包含明确秒数时使用默认短期保护秒数。
func extractUpstreamRetrySeconds(message string, fallbackSeconds int) int {
	matches := retrySecondsPattern.FindStringSubmatch(strings.ToLower(message))
	for i := 1; i < len(matches); i++ {
		if matches[i] == "" {
			continue
		}
		if secs, err := strconv.Atoi(matches[i]); err == nil && secs > 0 {
			return min(secs, 600)
		}
	}
	if fallbackSeconds > 0 {
		return min(fallbackSeconds, 30)
	}
	return 15
}

// lookupUpstreamRateLimit 查询当前用户的目标模型是否处于 429 自动开启的冷却保护期内，到期自动失效关闭。
func lookupUpstreamRateLimit(key string) (upstreamErrorCooldownEntry, int, bool) {
	if key == "" {
		return upstreamErrorCooldownEntry{}, 0, false
	}
	now := time.Now()
	upstreamErrorCooldownMu.RLock()
	entry, exists := upstreamRateLimitCache[key]
	upstreamErrorCooldownMu.RUnlock()
	if !exists {
		return upstreamErrorCooldownEntry{}, 0, false
	}
	if !now.Before(entry.expiresAt) {
		upstreamErrorCooldownMu.Lock()
		if current, ok := upstreamRateLimitCache[key]; ok && !now.Before(current.expiresAt) {
			delete(upstreamRateLimitCache, key)
		}
		upstreamErrorCooldownMu.Unlock()
		return upstreamErrorCooldownEntry{}, 0, false
	}
	remaining := max(1, int(math.Ceil(entry.expiresAt.Sub(now).Seconds())))
	return entry, remaining, true
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

// ObserveGroupUpstreamError 在上游返回 429 熔断或匹配的 400/524 错误时自动开启定时拦截，到期后自动恢复。
func ObserveGroupUpstreamError(c *gin.Context, apiErr *types.NewAPIError) {
	if c == nil || apiErr == nil || apiErr.GetErrorType() == types.ErrorTypeNewAPIError {
		return
	}
	group := resolveValidationGroup(c)
	policy, ok := operation_setting.GetLocalRequestValidationPolicy()
	if !ok || policy.GroupStatus(group) == 0 {
		return
	}
	now := time.Now()
	// 当上游返回 429 时，自动检测错误中的重试等待秒数并开启本地定时拦截，到点后自动关闭恢复。
	if apiErr.StatusCode == http.StatusTooManyRequests && policy.CooldownSeconds() > 0 {
		types.ErrOptionWithSkipRetry()(apiErr)
		waitSeconds := extractUpstreamRetrySeconds(apiErr.Error(), policy.CooldownSeconds())
		rlKey := computeUserModelRateLimitKey(c, group, resolveValidationModelName(c))
		if rlKey != "" && waitSeconds > 0 {
			upstreamErrorCooldownMu.Lock()
			if len(upstreamRateLimitCache) >= 10000 {
				for k, v := range upstreamRateLimitCache {
					if !now.Before(v.expiresAt) {
						delete(upstreamRateLimitCache, k)
					}
				}
			}
			if len(upstreamRateLimitCache) < 10000 {
				upstreamRateLimitCache[rlKey] = upstreamErrorCooldownEntry{
					expiresAt:  now.Add(time.Duration(waitSeconds) * time.Second),
					statusCode: http.StatusTooManyRequests,
					message:    apiErr.MaskSensitiveError(),
				}
			}
			upstreamErrorCooldownMu.Unlock()
		}
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
			expiresAt:  expiresAt,
			statusCode: apiErr.StatusCode,
			message:    apiErr.MaskSensitiveErrorWithStatusCode(),
		}
	}
	upstreamErrorCooldownMu.Unlock()
}

// CheckLocalRequestParameters 在选定分组内阻止非法参数、冷却期内的重复错误请求或 429 熔断期请求进入上游。
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
	modelName := resolveValidationModelName(c)
	if policy.CooldownSeconds() > 0 {
		rlKey := computeUserModelRateLimitKey(c, group, modelName)
		if entry, remaining, active := lookupUpstreamRateLimit(rlKey); active {
			const rateLimitCode = "local_rate_limit_cooldown"
			msg := retrySecondsPattern.ReplaceAllString(entry.message, fmt.Sprintf("请在 %d 秒后重试", remaining))
			if msg == entry.message {
				msg = fmt.Sprintf("上游触发限流熔断（%s），本地保护已开启，请在 %d 秒后重试", entry.message, remaining)
			}
			abortLocalValidationRequest(c, group, modelName, http.StatusTooManyRequests, rateLimitCode, msg, true)
			return true
		}
		fingerprint := computeLocalRequestFingerprint(c, group)
		if entry, cached := lookupUpstreamErrorCooldown(fingerprint); cached {
			const cooldownCode = "local_error_cooldown_intercepted"
			message := fmt.Sprintf("本地错误拦截：相同请求近期已触发上游错误（%s），冷却期内不再转发上游", entry.message)
			abortLocalValidationRequest(c, group, modelName, status, cooldownCode, message, true)
			return true
		}
	}
	err := helper.ValidateLocalRequestWithPolicy(c, policy)
	if err == nil {
		c.Set("local_request_parameters_validated", true)
		return false
	}
	// 思考签名被篡改时直接按上游原样返回 400 Invalid thought signature.，保证下游测活脚本通过且不触发上游熔断。
	if errors.Is(err, helper.ErrInvalidGeminiThoughtSignature) {
		state := service.RequestPolicy(c)
		state.AddEvent(service.PolicyEvent{Status: http.StatusBadRequest, ErrorCode: "400", Decision: service.PolicyDecision{Action: "stop", Reason: "invalid_thought_signature", Source: "local_validation"}, Health: "unchanged"})
		state.FinalLogged = true
		other := model.NewLogOther()
		other.SetPublic("status_code", http.StatusBadRequest)
		other.SetPublic("error_code", "local_parameter_error")
		other.SetPublic("local_validation", true)
		model.RecordErrorLog(c, c.GetInt("id"), 0, modelName, c.GetString("token_name"), "本地参数校验失败：Invalid thought signature.", c.GetInt("token_id"), 0, false, group, other)
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"message": "Invalid thought signature.",
				"type":    "upstream_error",
				"param":   "",
				"code":    http.StatusBadRequest,
			},
		})
		return true
	}
	// 请求体大小限制仍保留 413，不能被分组自定义状态码覆盖。
	if common.IsRequestBodyTooLargeError(err) || errors.Is(err, common.ErrRequestBodyTooLarge) {
		status = http.StatusRequestEntityTooLarge
	}
	const code = "local_parameter_error"
	message := "本地参数校验失败：" + err.Error()
	abortLocalValidationRequest(c, group, modelName, status, code, message, false)
	return true
}

// abortLocalValidationRequest 统一记录本地拦截决策与错误日志，并按请求协议格式返回客户端错误。
func abortLocalValidationRequest(c *gin.Context, group string, modelName string, status int, code string, message string, cooldown bool) {
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
	// 拒绝请求留在现有错误日志中；不保存请求体、素材或密钥，也不生成空任务。
	model.RecordErrorLog(c, c.GetInt("id"), 0, modelName, c.GetString("token_name"), message, c.GetInt("token_id"), 0, false, group, other)
	if c.Request.URL.Path == "/v1/messages" {
		c.AbortWithStatusJSON(status, gin.H{"type": "error", "error": gin.H{"type": "invalid_request_error", "message": common.MessageWithRequestId(message, c.GetString(common.RequestIdKey))}})
	} else {
		abortWithOpenAiMessage(c, status, message, types.ErrorCode(code))
	}
}
