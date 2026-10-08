package service

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

const AsyncMediaRoutingPersistKey = "async_media_routing_persist"
const asyncMediaRetryPolicyKey = "async_media_retry_policy"

type asyncMediaResponseKey struct{}
type asyncMediaResponse struct {
	status   int
	rejected bool
}

// AsyncMediaRetryConfig 固定一次后台执行的策略快照，运行中修改设置只影响新执行的任务。
func AsyncMediaRetryConfig(c *gin.Context) operation_setting.AsyncMediaRetryPolicy {
	if value, ok := c.Get(asyncMediaRetryPolicyKey); ok {
		return value.(operation_setting.AsyncMediaRetryPolicy)
	}
	policy := operation_setting.GetAsyncMediaRetryPolicy()
	c.Set(asyncMediaRetryPolicyKey, policy)
	return policy
}

func RelayRetryLimit(c *gin.Context) int {
	if c.GetString(model.AsyncRelayContextKey) == "" {
		return common.RetryTimes
	}
	policy := AsyncMediaRetryConfig(c)
	if !policy.Enabled {
		return 0
	}
	return policy.MaxRetries
}

// BeginAsyncMediaAttempt 清除上一渠道的响应证据，并在发送前持久化路由记录。
func BeginAsyncMediaAttempt(c *gin.Context) error {
	if c.GetString(model.AsyncRelayContextKey) == "" {
		return nil
	}
	c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), asyncMediaResponseKey{}, &asyncMediaResponse{}))
	if callback, exists := c.Get(AsyncMediaRoutingPersistKey); exists {
		return callback.(func() error)()
	}
	return nil
}

// ObserveAsyncMediaHTTPFailure 只信任完整读到的上游错误响应，不把本机500或断网当成上游拒单。
func ObserveAsyncMediaHTTPFailure(ctx context.Context, response *http.Response, body []byte, readErr error, taskIDPaths ...string) {
	evidence, ok := ctx.Value(asyncMediaResponseKey{}).(*asyncMediaResponse)
	if !ok || response == nil {
		return
	}
	evidence.status = response.StatusCode
	evidence.rejected = readErr == nil && response.StatusCode >= 400 && response.StatusCode <= 599 && response.Header.Get("Location") == ""
	var payload any
	if common.Unmarshal(body, &payload) == nil && asyncMediaHasAcceptedResult(payload) {
		evidence.rejected = false
	}
	// 自定义视频协议的任务编号可以使用任意字段名，同样必须阻止重复付费提交。
	for _, path := range taskIDPaths {
		if path != "" && gjson.GetBytes(body, path).String() != "" {
			evidence.rejected = false
		}
	}
}

// 路由日志展示用于决策的上游原始状态，不被客户端错误码映射混淆。
func asyncMediaUpstreamStatus(c *gin.Context, fallback int) int {
	if c != nil && c.Request != nil {
		if response, ok := c.Request.Context().Value(asyncMediaResponseKey{}).(*asyncMediaResponse); ok && response.status != 0 {
			return response.status
		}
	}
	return fallback
}

// 错误响应仍携带任务编号、生成结果或运行状态时，保守保留原任务，绝不换渠道再付费。
func asyncMediaHasAcceptedResult(value any) bool {
	switch item := value.(type) {
	case map[string]any:
		for key, nested := range item {
			key = strings.ToLower(strings.ReplaceAll(key, "_", ""))
			switch key {
			case "id", "taskid", "job", "jobid", "predictionid", "b64json", "url", "output", "result":
				if nested != nil && nested != "" {
					return true
				}
			case "status", "state":
				if status, ok := nested.(string); ok {
					switch strings.ToLower(status) {
					case "queued", "pending", "running", "processing", "accepted", "succeeded", "success", "completed":
						return true
					}
				}
			}
			if asyncMediaHasAcceptedResult(nested) {
				return true
			}
		}
	case []any:
		for _, nested := range item {
			if asyncMediaHasAcceptedResult(nested) {
				return true
			}
		}
	}
	return false
}

func DecideAsyncMediaRetry(c *gin.Context, remaining int, explicitlyStopped bool) PolicyDecision {
	stop := PolicyDecision{Action: "stop", Source: "async_media", Reason: "media_retry_disabled"}
	policy := AsyncMediaRetryConfig(c)
	if !policy.Enabled {
		return stop
	}
	switch {
	case explicitlyStopped:
		stop.Reason = "non_retryable_error"
	case c.Request.Context().Err() != nil || c.Writer.Written():
		stop.Reason = "response_started_or_cancelled"
	case remaining <= 0:
		stop.Reason = "attempt_budget_exhausted"
	case len(GetChannelConstraints(c).Pins) > 0 || c.GetString("specific_channel_id") != "":
		stop.Reason = "pinned_channel"
	case ShouldSkipRetryAfterChannelAffinityFailure(c):
		stop.Reason = "strict_session"
	case !policy.IncludesChannel(c.GetInt("channel_id")):
		stop.Reason = "channel_not_selected"
	default:
		evidence, ok := c.Request.Context().Value(asyncMediaResponseKey{}).(*asyncMediaResponse)
		if !ok || !evidence.rejected {
			stop.Reason = "submission_not_rejected"
		} else if !policy.MatchesStatus(c.GetInt("channel_id"), evidence.status) {
			stop.Reason = "status_not_retryable"
		} else {
			return PolicyDecision{Action: "retry", Source: "async_media", Reason: "retry_status_matched"}
		}
	}
	return stop
}

// SelectAsyncMediaRetryChannel 只从未尝试过的候选里按优先级和权重选择，不增加分组外的权限。
func SelectAsyncMediaRetryChannel(param *RetryParam) (*model.Channel, string, error) {
	c := param.Ctx
	if err := PrepareVideoChannelSelection(c, param.ModelName); err != nil {
		return nil, param.TokenGroup, err
	}
	filters := append([]dto.ChannelFilter(nil), GetChannelConstraints(c).Filters...)
	excluded := make(map[int]bool)
	for _, value := range c.GetStringSlice("use_channel") {
		id, _ := strconv.Atoi(value)
		excluded[id] = true
	}
	filters = append(filters, dto.ChannelFilter{Kind: dto.FilterExcludedChannels, ExcludedChannelIDs: excluded})
	policy := AsyncMediaRetryConfig(c)
	if len(policy.ChannelIDs) > 0 {
		allowed := make(map[int]bool)
		for _, id := range policy.ChannelIDs {
			allowed[id] = true
		}
		filters = append(filters, dto.ChannelFilter{Kind: dto.FilterAllowedChannels, AllowedChannelIDs: allowed})
	}
	groups := []string{param.TokenGroup}
	if param.TokenGroup == "auto" {
		current := common.GetContextKeyString(c, constant.ContextKeyAutoGroup)
		groups = []string{current}
		if common.GetContextKeyBool(c, constant.ContextKeyTokenCrossGroupRetry) {
			groups = GetRequestAutoGroups(c, common.GetContextKeyString(c, constant.ContextKeyUserGroup))
		}
	}
	for _, group := range groups {
		channel, err := model.GetRandomSatisfiedChannel(group, param.ModelName, 0, filters)
		if err != nil {
			return nil, group, err
		}
		if channel != nil {
			if param.TokenGroup == "auto" {
				common.SetContextKey(c, constant.ContextKeyAutoGroup, group)
			}
			return channel, group, nil
		}
	}
	return nil, param.TokenGroup, nil
}
