package operation_setting

import (
	"fmt"
	"slices"

	"github.com/QuantumNous/new-api/common"
)

const AsyncMediaRetryOption = "AsyncMediaRetryPolicy"
const DefaultAsyncMediaRetryJSON = `{"enabled":false,"max_retries":2,"status_codes":"429,500,502,503","channel_ids":[]}`

// AsyncMediaRetryPolicy 一次保存完整策略，避免开关、状态码和渠道范围出现半更新。
type AsyncMediaRetryPolicy struct {
	Enabled     bool   `json:"enabled"`
	MaxRetries  int    `json:"max_retries"`
	StatusCodes string `json:"status_codes"`
	ChannelIDs  []int  `json:"channel_ids"`
	ranges      []StatusCodeRange
}

func ParseAsyncMediaRetryPolicy(value string) (AsyncMediaRetryPolicy, error) {
	var policy AsyncMediaRetryPolicy
	if err := common.UnmarshalJsonStr(value, &policy); err != nil {
		return policy, fmt.Errorf("媒体重试设置格式无效")
	}
	if policy.MaxRetries < 0 || policy.MaxRetries > 20 {
		return policy, fmt.Errorf("换渠道重试次数必须是 0 到 20 的整数")
	}
	ranges, err := ParseHTTPStatusCodeRanges(policy.StatusCodes)
	if err != nil || len(ranges) == 0 {
		return policy, fmt.Errorf("请填写触发重试的 HTTP 错误码，例如 500 或 500-503")
	}
	for _, r := range ranges {
		if r.Start < 400 || r.End > 599 {
			return policy, fmt.Errorf("媒体重试仅接受 400 到 599 的错误码")
		}
	}
	if len(policy.ChannelIDs) > 10000 {
		return policy, fmt.Errorf("所选渠道过多")
	}
	seen := make(map[int]bool)
	for _, id := range policy.ChannelIDs {
		if id <= 0 || seen[id] {
			return policy, fmt.Errorf("渠道编号必须是互不重复的正整数")
		}
		seen[id] = true
	}
	policy.ranges = ranges
	return policy, nil
}

// GetAsyncMediaRetryPolicy 兼容旧安装；缺失或损坏的策略保持关闭，不隐式开启付费重发。
func GetAsyncMediaRetryPolicy() AsyncMediaRetryPolicy {
	common.OptionMapRWMutex.RLock()
	value := common.OptionMap[AsyncMediaRetryOption]
	common.OptionMapRWMutex.RUnlock()
	if value == "" {
		value = DefaultAsyncMediaRetryJSON
	}
	policy, err := ParseAsyncMediaRetryPolicy(value)
	if err != nil {
		return AsyncMediaRetryPolicy{}
	}
	return policy
}

func (policy AsyncMediaRetryPolicy) IncludesChannel(id int) bool {
	return len(policy.ChannelIDs) == 0 || slices.Contains(policy.ChannelIDs, id)
}

func (policy AsyncMediaRetryPolicy) MatchesStatus(code int) bool {
	// 超时表示上游是否接单不明，即使范围包含这些状态也不重新生成。
	return code != 408 && code != 504 && code != 524 && shouldMatchStatusCodeRanges(policy.ranges, code)
}
