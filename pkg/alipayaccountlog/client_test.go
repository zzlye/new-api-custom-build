package alipayaccountlog

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type receiptQueryTransport func(*http.Request) (*http.Response, error)

func (f receiptQueryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestQueryFailureAndVerification(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"应用未上线", 400, `{"code":"not-online-app","message":"secret"}`, "支付宝账务查询失败：应用未上线（not-online-app，HTTP 400）"},
		{"未知错误不泄露原文", 403, `{"code":"secret","message":"secret"}`, "支付宝账务查询返回 HTTP 403"},
		{"非JSON错误", 502, "<html>secret</html>", "支付宝账务查询返回 HTTP 502"},
		{"超大错误", 400, strings.Repeat(" ", 16*1024) + `{"code":"not-online-app"}`, "支付宝账务查询返回 HTTP 400"},
		{"成功响应仍须验签", 200, `{"total_size":0,"detail_list":[]}`, "支付宝账务响应签名头缺失或无效"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &Client{appID: "2026000000000001", privateKey: key, publicKey: &key.PublicKey, now: time.Now,
				httpClient: &http.Client{Transport: receiptQueryTransport(func(req *http.Request) (*http.Response, error) {
					// 只替换测试传输，不放开正式网关或发送真实账务请求。
					assert.Equal(t, "openapi.alipay.com", req.URL.Host)
					assert.Equal(t, http.MethodGet, req.Method)
					return &http.Response{StatusCode: tc.status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(tc.body))}, nil
				})},
			}
			page, err := client.Query(context.Background(), time.Now().Add(-time.Minute), time.Now(), 1, 100)
			require.EqualError(t, err, tc.want)
			assert.Nil(t, page)
		})
	}
}
