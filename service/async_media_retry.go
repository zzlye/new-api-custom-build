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
const asyncMediaRetrySameChannelKey = "async_media_retry_same_channel"

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
	// 每条渠道都有首次提交及自己的重试预算，换渠道预算仍表示备用渠道数量。
	return (policy.MaxRetries+1)*(policy.SameChannelRetries+1) - 1
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
	c.Set(asyncMediaRetrySameChannelKey, false)
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
			used := c.GetStringSlice("use_channel")
			attempts := 0
			for i := len(used) - 1; i >= 0 && used[i] == strconv.Itoa(c.GetInt("channel_id")); i-- {
				attempts++
			}
			if attempts > 0 && attempts <= policy.SameChannelRetries {
				c.Set(asyncMediaRetrySameChannelKey, true)
				return PolicyDecision{Action: "retry", Source: "async_media", Reason: "retry_same_channel"}
			}
			visited := make(map[string]bool)
			for _, id := range used {
				visited[id] = true
			}
			if len(visited) > policy.MaxRetries {
				stop.Reason = "attempt_budget_exhausted"
				return stop
			}
			return PolicyDecision{Action: "retry", Source: "async_media", Reason: "retry_status_matched"}
		}
	}
	return stop
}

// SelectAsyncMediaRetryChannel 先重试原渠道，再从同模型、分组内未尝试渠道选备用；勾选名单不限制备用渠道。
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
	groups := []string{param.TokenGroup}
	if param.TokenGroup == "auto" {
		current := common.GetContextKeyString(c, constant.ContextKeyAutoGroup)
		groups = []string{current}
		if common.GetContextKeyBool(c, constant.ContextKeyTokenCrossGroupRetry) {
			groups = GetRequestAutoGroups(c, common.GetContextKeyString(c, constant.ContextKeyUserGroup))
		}
	}
	if c.GetBool(asyncMediaRetrySameChannelKey) {
		c.Set(asyncMediaRetrySameChannelKey, false)
		// 原渠道仍须通过实时可用性和协议筛选，不能重试已被禁用的渠道。
		sameFilters := append([]dto.ChannelFilter(nil), GetChannelConstraints(c).Filters...)
		sameFilters = append(sameFilters,
			dto.ChannelFilter{Kind: dto.FilterExcludedChannels, ExcludedChannelIDs: map[int]bool{}},
			dto.ChannelFilter{Kind: dto.FilterAllowedChannels, AllowedChannelIDs: map[int]bool{c.GetInt("channel_id"): true}},
		)
		group := param.TokenGroup
		if group == "auto" {
			group = common.GetContextKeyString(c, constant.ContextKeyAutoGroup)
		}
		channel, err := model.GetRandomSatisfiedChannel(group, param.ModelName, 0, sameFilters)
		if err != nil || channel != nil {
			return channel, group, err
		}
		// 原渠道不可用时允许直接切换，但不能突破配置的备用渠道数量。
		if len(excluded) > AsyncMediaRetryConfig(c).MaxRetries {
			return nil, group, nil
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
