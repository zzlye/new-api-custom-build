package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 真实决策链必须匹配失败渠道的原始错误码，后续渠道不能沿用首个渠道的规则。
func TestAsyncMediaRoutingPerChannelCodes(t *testing.T) {
	prepareAsyncCompatRelay(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }))
	require.NoError(t, model.UpdateOption(operation_setting.AsyncMediaRetryOption, `{"enabled":true,"max_retries":2,"channel_ids":[],"channel_status_codes":{"1":"500","2":"429,503"}}`))
	for _, test := range []struct {
		channel, status int
		action          string
	}{
		{1, 500, "retry"}, {2, 500, "stop"}, {1, 429, "stop"}, {2, 429, "retry"}, {3, 500, "stop"},
	} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
		c.Set(model.AsyncRelayContextKey, "fixture")
		c.Set("channel_id", test.channel)
		require.NoError(t, service.BeginAsyncMediaAttempt(c))
		service.ObserveAsyncMediaHTTPFailure(c.Request.Context(), &http.Response{StatusCode: test.status, Header: http.Header{}}, []byte(`{"error":"rejected"}`), nil)
		require.Equal(t, test.action, service.DecideAsyncMediaRetry(c, 2, false).Action)
	}
}
