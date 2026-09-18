package controller

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/require"
)

func TestAsyncMediaValidationCategories(t *testing.T) {
	for _, tc := range []struct{ detail, want string }{
		{"DNS resolution failed for signed-secret.example", "DNS"},
		{"unsupported protocol: ftp", "协议"},
		{"port 9000 is not allowed", "端口"},
		{"domain not in whitelist: signed-secret.example", "域名"},
		{"ip in blacklist: 192.0.2.1", "目标 IP"},
	} {
		reason := asyncMediaURLValidationReason(errors.New(tc.detail))
		require.Contains(t, reason, tc.want)
		require.NotContains(t, reason, "signed-secret")
	}
}

// 检查真实结果读取入口，让任务日志区分无图片、上游拦截和媒体地址策略错误。
func TestAsyncMediaMissingResultDiagnostics(t *testing.T) {
	cases := []struct{ body, want string }{
		{`{"candidates":[{"content":{"parts":[{"text":"description only"}]},"finishReason":"STOP"}]}`, "上游仅返回文字"},
		{`{"promptFeedback":{"blockReason":"SAFETY"}}`, "上游拦截"},
		{`{"candidates":[{"finishReason":"MAX_TOKENS"}]}`, "输出长度上限"},
		{`{"candidates":[{"finishReason":"NO_IMAGE"}]}`, "上游未生成图片"},
	}
	for _, tc := range cases {
		for _, stream := range []bool{false, true} {
			body, contentType := tc.body, "application/json"
			if stream {
				body, contentType = "data: "+body+"\n\ndata: [DONE]\n\n", "text/event-stream"
			}
			_, err := readAsyncRelayMediaSources(strings.NewReader(body), contentType)
			require.ErrorContains(t, err, tc.want)
		}
	}
}

func TestAsyncMediaDoesNotTreatTextBeforeImageAsFailure(t *testing.T) {
	body := "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"working\"}]}}]}\n\ndata: {\"candidates\":[{\"content\":{\"parts\":[{\"inlineData\":{\"mimeType\":\"image/png\",\"data\":\"" + asyncFixturePNG + "\"}}]}}]}\n\n"
	sources, err := readAsyncRelayMediaSources(strings.NewReader(body), "text/event-stream")
	require.NoError(t, err)
	require.Len(t, sources, 1)
}

func TestAsyncMediaURLValidationReasonHidesSignedURL(t *testing.T) {
	setting := system_setting.GetFetchSetting()
	old := *setting
	t.Cleanup(func() { *setting = old })
	setting.EnableSSRFProtection = true
	setting.AllowPrivateIp = false
	setting.DomainFilterMode = false
	setting.IpFilterMode = false
	setting.DomainList = nil
	setting.IpList = nil
	setting.AllowedPorts = []string{"443"}
	_, err := saveAsyncMediaSource(context.Background(), asyncMediaSource{Value: "https://127.0.0.1/image?token=private-secret"})
	require.ErrorContains(t, err, "私有或保留 IP")
	require.NotContains(t, err.Error(), "private-secret")
	require.NotContains(t, err.Error(), "127.0.0.1")
}
