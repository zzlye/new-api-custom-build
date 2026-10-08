package operation_setting

import (
	"fmt"
	"slices"

	"github.com/QuantumNous/new-api/common"
)

const AsyncMediaRetryOption = "AsyncMediaRetryPolicy"
const DefaultAsyncMediaRetryJSON = `{"enabled":false,"max_retries":2,"channel_status_codes":{},"channel_ids":[],"selected_channels_only":true}`

// AsyncMediaRetryPolicy 一次保存完整策略，避免开关、状态码和渠道范围出现半更新。
type AsyncMediaRetryPolicy struct {
	Enabled    bool `json:"enabled"`
	MaxRetries int  `json:"max_retries"`
	// 旧字段仅用于读取已有配置；新配置按渠道保存，空映射不会回退到通用错误码。
	StatusCodes        string         `json:"status_codes,omitempty"`
	ChannelStatusCodes map[int]string `json:"channel_status_codes"`
	ChannelIDs         []int          `json:"channel_ids"`
	// 新界面始终使用勾选名单；保留旧配置缺失该字段时的兼容行为。
	SelectedChannelsOnly bool `json:"selected_channels_only,omitempty"`
	ranges               []StatusCodeRange
	channelRanges        map[int][]StatusCodeRange
}

func ParseAsyncMediaRetryPolicy(value string) (AsyncMediaRetryPolicy, error) {
	var policy AsyncMediaRetryPolicy
	if err := common.UnmarshalJsonStr(value, &policy); err != nil {
		return policy, fmt.Errorf("媒体重试设置格式无效")
	}
	if policy.MaxRetries < 0 || policy.MaxRetries > 20 {
		return policy, fmt.Errorf("换渠道重试次数必须是 0 到 20 的整数")
	}
	if policy.ChannelStatusCodes == nil {
		// 尚未保存新版界面的旧配置保留原行为，避免升级时突然丢失路由策略。
		ranges, err := parseAsyncMediaRetryCodes(policy.StatusCodes)
		if err != nil {
			return policy, err
		}
		policy.ranges = ranges
	} else {
		if len(policy.ChannelStatusCodes) > 10000 {
			return policy, fmt.Errorf("渠道错误码配置过多")
		}
		policy.channelRanges = make(map[int][]StatusCodeRange, len(policy.ChannelStatusCodes))
		for id, codes := range policy.ChannelStatusCodes {
			if id <= 0 {
				return policy, fmt.Errorf("渠道编号必须是正整数")
			}
			ranges, err := parseAsyncMediaRetryCodes(codes)
			if err != nil {
				return policy, fmt.Errorf("渠道 #%d：%w", id, err)
			}
			policy.channelRanges[id] = ranges
			policy.ChannelStatusCodes[id] = statusCodeRangesToString(ranges)
		}
		// 明确进入按渠道模式后，旧错误码字段不再参与任何决策。
		policy.StatusCodes = ""
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
	return policy, nil
}

// parseAsyncMediaRetryCodes 统一验证渠道规则，拒绝空值、成功状态和非法区间。
func parseAsyncMediaRetryCodes(codes string) ([]StatusCodeRange, error) {
	ranges, err := ParseHTTPStatusCodeRanges(codes)
	if err != nil || len(ranges) == 0 {
		return nil, fmt.Errorf("请填写触发重试的 HTTP 错误码，例如 500 或 500-503")
	}
	for _, r := range ranges {
		if r.Start < 400 || r.End > 599 {
			return nil, fmt.Errorf("媒体重试仅接受 400 到 599 的错误码")
		}
	}
	return ranges, nil
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
	return (!policy.SelectedChannelsOnly && len(policy.ChannelIDs) == 0) || slices.Contains(policy.ChannelIDs, id)
}

func (policy AsyncMediaRetryPolicy) MatchesStatus(channelID, code int) bool {
	// 超时表示上游是否接单不明，即使范围包含这些状态也不重新生成。
	ranges := policy.ranges
	if policy.ChannelStatusCodes != nil {
		ranges = policy.channelRanges[channelID]
	}
	return code != 408 && code != 504 && code != 524 && shouldMatchStatusCodeRanges(ranges, code)
}
