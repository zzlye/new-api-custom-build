package service

import (
	"slices"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

const requestPolicyContextKey = "request_policy_state"

type PolicyDecision struct {
	Action string `json:"action"`
	Reason string `json:"reason"`
	Source string `json:"source"`
}

type PolicyEvent struct {
	Attempt     int            `json:"attempt"`
	ChannelID   int            `json:"channel_id,omitempty"`
	Group       string         `json:"group,omitempty"`
	Rule        string         `json:"rule,omitempty"`
	Status      int            `json:"status,omitempty"`
	ErrorCode   string         `json:"error_code,omitempty"`
	ErrorSource string         `json:"error_source,omitempty"`
	ElapsedMS   int64          `json:"elapsed_ms"`
	Decision    PolicyDecision `json:"decision"`
	Health      string         `json:"health,omitempty"`
}

// RequestPolicyState records how one request was routed so administrators can
// read the decision flow in the log details. Channel selection and the retry
// decision stay in the relay flows; this state only records them.
type RequestPolicyState struct {
	FinalLogged       bool
	StartedAt         time.Time
	Attempts          int
	SelectedGroup     string
	SessionMode       string
	SessionModeSource string
	RuleName          string
	Successful        bool
	OutcomeRecorded   bool
	mu                sync.Mutex
	events            []PolicyEvent
}

func RequestPolicy(c *gin.Context) *RequestPolicyState {
	if c != nil {
		if value, ok := c.Get(requestPolicyContextKey); ok {
			return value.(*RequestPolicyState)
		}
	}
	state := &RequestPolicyState{StartedAt: time.Now()}
	if c != nil {
		c.Set(requestPolicyContextKey, state)
	}
	return state
}

func (s *RequestPolicyState) AddEvent(event PolicyEvent) {
	event.Attempt, event.ElapsedMS = s.Attempts, time.Since(s.StartedAt).Milliseconds()
	if event.Group == "" {
		event.Group = s.SelectedGroup
	}
	if event.Rule == "" {
		event.Rule = s.RuleName
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.events) < 512 {
		s.events = append(s.events, event)
	}
}

func (s *RequestPolicyState) Events() []PolicyEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.events)
}

func (s *RequestPolicyState) BeginAttempt(channel *model.Channel, group string) {
	s.Attempts++
	s.Successful = false
	s.OutcomeRecorded = false
	s.SelectedGroup = group
	s.AddEvent(PolicyEvent{ChannelID: channel.Id, Decision: PolicyDecision{Action: "attempt", Reason: "channel_selected", Source: "routing"}})
}

// RecordPolicyFailure appends the failed attempt and the retry decision made
// for it. The health entry mirrors the check in ProcessChannelError, which
// performs the actual disable.
func RecordPolicyFailure(c *gin.Context, channelID int, err *types.NewAPIError, decision PolicyDecision) {
	if c == nil || err == nil {
		return
	}
	source := "upstream"
	switch {
	case types.IsChannelError(err):
		source = "channel"
	case err.GetErrorCode() == types.ErrorCodeDoRequestFailed:
		source = "transport"
	case err.GetErrorType() == types.ErrorTypeNewAPIError:
		source = "local"
	}
	state := RequestPolicy(c)
	event := PolicyEvent{ChannelID: channelID, Status: asyncMediaUpstreamStatus(c, err.StatusCode), ErrorCode: string(err.GetErrorCode()), ErrorSource: source, Decision: PolicyDecision{Action: "failure", Reason: "upstream_failure", Source: source}}
	if source == "local" {
		event.Decision.Reason = "local_rejection"
	}
	state.AddEvent(event)
	event.Decision, event.Health = decision, "unchanged"
	if source != "local" && c.GetBool("auto_ban") && ShouldDisableChannel(err) {
		event.Health = "channel_disable_requested"
		if common.GetContextKeyBool(c, constant.ContextKeyChannelIsMultiKey) {
			event.Health = "key_disable_requested"
		}
	}
	state.AddEvent(event)
}

func MarkRequestPolicySuccess(c *gin.Context, stream *relaycommon.StreamStatus) {
	state := RequestPolicy(c)
	if state.OutcomeRecorded {
		return
	}
	state.OutcomeRecorded = true
	state.Successful = stream == nil || stream.IsNormalEnd() && !stream.HasErrors() && (stream.ResponseOutcome() == "" || stream.ResponseOutcome() == "completed")
	decision := PolicyDecision{Action: "success", Reason: "request_completed", Source: "upstream"}
	if !state.Successful {
		decision = PolicyDecision{Action: "stop", Reason: "stream_not_successful", Source: "system"}
	}
	channelID := 0
	if c != nil {
		channelID = c.GetInt("channel_id")
	}
	state.AddEvent(PolicyEvent{ChannelID: channelID, Decision: decision})
}

// Rules explicitly inherit the global default or override it. Rules without a
// mode retain their legacy retry behavior until an administrator changes them.
func EffectiveSessionMode(setting *operation_setting.ChannelAffinitySetting, rule operation_setting.ChannelAffinityRule) (mode, source string) {
	if rule.SessionMode == "inherit" {
		if setting.SessionMode != "" {
			return setting.SessionMode, "global"
		}
		return "prefer", "global"
	}
	if rule.SessionMode != "" {
		return rule.SessionMode, "session_rule"
	}
	if rule.SkipRetryOnFailure {
		return "strict", "session_rule"
	}
	return "prefer", "session_rule"
}

// RecordRequestPolicyTermination 记录最终失败决策，内部异步仅在此写入一条最终错误日志。
// 普通同步请求仍由 ProcessChannelError 逐次记录，此处不再重复写入。
func RecordRequestPolicyTermination(c *gin.Context, apiErr *types.NewAPIError) {
	if c == nil || apiErr == nil {
		return
	}
	state := RequestPolicy(c)
	if state.FinalLogged {
		return
	}
	state.FinalLogged = true
	events := state.Events()
	if len(events) == 0 || events[len(events)-1].Decision.Action != "stop" {
		state.AddEvent(PolicyEvent{ChannelID: c.GetInt("channel_id"), Status: asyncMediaUpstreamStatus(c, apiErr.StatusCode), ErrorCode: string(apiErr.GetErrorCode()), Decision: PolicyDecision{Action: "stop", Reason: "request_failed", Source: "system"}, Health: "unchanged"})
	}
	if c.GetString(model.AsyncRelayContextKey) != "" {
		if pending, exists := c.Get(asyncMediaPendingErrorLogKey); exists {
			// 无备用渠道或后续本地失败也在此收口，避免略过中间日志后丢失最终错误。
			failure := pending.(asyncMediaPendingErrorLog)
			recordChannelErrorLog(c, failure.channel, apiErr, failure.relayInfo)
		}
	}
}
