package operation_setting

import (
	"testing"

	"github.com/stretchr/testify/require"
)

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
	for _, code := range []int{408, 504, 524} {
		policy, err = ParseAsyncMediaRetryPolicy(`{"channel_status_codes":{"12":"400-599"}}`)
		require.NoError(t, err)
		require.False(t, policy.MatchesStatus(12, code))
	}
	policy, err = ParseAsyncMediaRetryPolicy(`{"status_codes":"500","channel_status_codes":{}}`)
	require.NoError(t, err)
	require.False(t, policy.MatchesStatus(12, 500))
}

// 旧配置仍可读取，但新保存的数据不会回退到旧通用规则。
func TestAsyncMediaRetryLegacyAndValidation(t *testing.T) {
	legacy, err := ParseAsyncMediaRetryPolicy(`{"max_retries":2,"status_codes":"500","channel_ids":[12]}`)
	require.NoError(t, err)
	require.True(t, legacy.MatchesStatus(12, 500))
	for _, value := range []string{
		`{"channel_status_codes":{"0":"500"}}`,
		`{"channel_status_codes":{"12":""}}`,
		`{"channel_status_codes":{"12":"200"}}`,
		`{"channel_status_codes":{"12":"600"}}`,
		`{"channel_status_codes":{"12":"503-500"}}`,
		`{"channel_status_codes":{"abc":"500"}}`,
	} {
		_, err := ParseAsyncMediaRetryPolicy(value)
		require.Error(t, err, value)
	}
}
