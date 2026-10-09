package operation_setting

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 保存和重读都逐个保留错误码，区间输入仅作为兼容格式，不影响实际匹配集合。
func TestAsyncMediaRetryIndividualStatusCodes(t *testing.T) {
	for _, tc := range []struct {
		input    string
		expected string
		matches  []int
		excludes []int
	}{
		{"502,503", "502,503", []int{502, 503}, []int{501, 504, 505}},
		{"429,502,503,504", "429,502,503,504", []int{429, 502, 503, 504}, []int{501, 505}},
		{"429,502-504,503", "429,502,503,504", []int{429, 502, 503, 504}, []int{501, 505}},
		{" 503，502，503，504，429 ", "429,502,503,504", []int{429, 502, 503, 504}, []int{501, 505}},
		{"500-503", "500,501,502,503", []int{500, 501, 502, 503}, []int{499, 504}},
	} {
		t.Run(tc.input, func(t *testing.T) {
			value, err := common.Marshal(AsyncMediaRetryPolicy{
				Enabled:              true,
				MaxRetries:           2,
				SameChannelRetries:   1,
				ChannelStatusCodes:   map[int]string{113: tc.input},
				ChannelIDs:           []int{113},
				SelectedChannelsOnly: true,
			})
			require.NoError(t, err)
			policy, err := ParseAsyncMediaRetryPolicy(string(value))
			require.NoError(t, err)
			assert.Equal(t, tc.expected, policy.ChannelStatusCodes[113])
			for _, code := range tc.matches {
				assert.True(t, policy.MatchesStatus(113, code), "错误码 %d 应参与匹配", code)
			}
			for _, code := range tc.excludes {
				assert.False(t, policy.MatchesStatus(113, code), "未配置的错误码 %d 不应匹配", code)
			}
			value, err = common.Marshal(policy)
			require.NoError(t, err)
			reloaded, err := ParseAsyncMediaRetryPolicy(string(value))
			require.NoError(t, err)
			assert.Equal(t, tc.expected, reloaded.ChannelStatusCodes[113])
		})
	}
}

// 不同渠道的相同响应必须使用各自规则，未配置渠道不能继承通用状态码。
func TestAsyncMediaRetryChannelCodes(t *testing.T) {
	policy, err := ParseAsyncMediaRetryPolicy(`{"enabled":true,"max_retries":2,"status_codes":"500-599","channel_ids":[],"channel_status_codes":{"12":"500","38":"429,502-503"}}`)
	require.NoError(t, err)
	require.True(t, policy.MatchesStatus(12, 500))
	require.False(t, policy.MatchesStatus(38, 500))
	require.False(t, policy.MatchesStatus(12, 429))
	require.True(t, policy.MatchesStatus(38, 429))
	require.False(t, policy.MatchesStatus(99, 500))
	require.Empty(t, policy.StatusCodes)
	for _, code := range []int{408, 524} {
		policy, err = ParseAsyncMediaRetryPolicy(`{"channel_status_codes":{"12":"400-599"}}`)
		require.NoError(t, err)
		require.False(t, policy.MatchesStatus(12, code))
	}
	policy, err = ParseAsyncMediaRetryPolicy(`{"channel_status_codes":{"12":"429,503,504","38":"429,503"}}`)
	require.NoError(t, err)
	require.True(t, policy.MatchesStatus(12, 504), "完整上游504响应必须遵循该渠道的显式配置")
	require.False(t, policy.MatchesStatus(38, 504))
	policy, err = ParseAsyncMediaRetryPolicy(`{"status_codes":"500","channel_status_codes":{}}`)
	require.NoError(t, err)
	require.False(t, policy.MatchesStatus(12, 500))
}

// 旧配置仍可读取，但新保存的数据不会回退到旧通用规则。
func TestAsyncMediaRetryLegacyAndValidation(t *testing.T) {
	legacy, err := ParseAsyncMediaRetryPolicy(`{"max_retries":2,"status_codes":"500","channel_ids":[12]}`)
	require.NoError(t, err)
	require.True(t, legacy.MatchesStatus(12, 500))
	require.Zero(t, legacy.SameChannelRetries, "旧配置不隐式启用原渠道付费重发")
	for _, value := range []string{
		`{"channel_status_codes":{"0":"500"}}`,
		`{"channel_status_codes":{"12":""}}`,
		`{"channel_status_codes":{"12":"200"}}`,
		`{"channel_status_codes":{"12":"600"}}`,
		`{"channel_status_codes":{"12":"503-500"}}`,
		`{"channel_status_codes":{"abc":"500"}}`,
		`{"channel_status_codes":{},"same_channel_retries":-1}`,
		`{"channel_status_codes":{},"same_channel_retries":6}`,
		`{"channel_status_codes":{},"same_channel_retries":1.5}`,
	} {
		_, err := ParseAsyncMediaRetryPolicy(value)
		require.Error(t, err, value)
	}
}

// 一个总开关配合显式名单；清空名单不能意外变成全部渠道。
func TestAsyncMediaRetrySelectedChannelsOnly(t *testing.T) {
	policy, err := ParseAsyncMediaRetryPolicy(`{"enabled":true,"max_retries":2,"channel_status_codes":{"12":"500","38":"429"},"channel_ids":[12],"selected_channels_only":true}`)
	require.NoError(t, err)
	require.True(t, policy.IncludesChannel(12))
	require.False(t, policy.IncludesChannel(38))
	policy, err = ParseAsyncMediaRetryPolicy(`{"enabled":false,"channel_status_codes":{"12":"500"},"channel_ids":[],"selected_channels_only":true}`)
	require.NoError(t, err)
	require.False(t, policy.IncludesChannel(12))
	legacy, err := ParseAsyncMediaRetryPolicy(`{"channel_status_codes":{"12":"500"},"channel_ids":[]}`)
	require.NoError(t, err)
	require.True(t, legacy.IncludesChannel(12))
}
