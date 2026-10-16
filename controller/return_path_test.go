package controller

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type publicAddressTestTransport func(*http.Request) (*http.Response, error)

func (transport publicAddressTestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func TestCreemCheckoutUsesCurrentSiteReturnURL(t *testing.T) {
	oldAddress, oldKey, oldTransport := system_setting.ServerAddress, setting.CreemApiKey, http.DefaultTransport
	t.Cleanup(func() {
		system_setting.ServerAddress, setting.CreemApiKey, http.DefaultTransport = oldAddress, oldKey, oldTransport
	})
	setting.CreemApiKey = "fixture-key"
	// 截获真实下单请求，不向支付平台创建订单。
	http.DefaultTransport = publicAddressTestTransport(func(request *http.Request) (*http.Response, error) {
		assert.Equal(t, "/v1/checkouts", request.URL.Path)
		var body map[string]any
		require.NoError(t, common.DecodeJson(request.Body, &body))
		assert.Equal(t, strings.TrimRight(system_setting.ServerAddress, "/")+"/wallet?show_history=true", body["success_url"])
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"checkout_url":"https://checkout.example/session","id":"fixture"}`))}, nil
	})
	for _, address := range []string{"https://site.example/prefix/", "https://changed.example"} {
		system_setting.ServerAddress = address
		_, err := genCreemLink(context.Background(), "fixture-order", &CreemProduct{ProductId: "fixture-product"}, "fixture@example.com", "fixture")
		require.NoError(t, err)
	}
}

func TestPaymentReturnPathUsesDefaultDashboardRoutes(t *testing.T) {
	previousAddress := system_setting.ServerAddress
	system_setting.ServerAddress = "https://dashboard.example.com/"
	t.Cleanup(func() { system_setting.ServerAddress = previousAddress })

	assert.Equal(
		t,
		"https://dashboard.example.com/wallet?pay=success",
		paymentReturnPath("/wallet?pay=success"),
	)
	assert.Equal(
		t,
		"https://dashboard.example.com/usage-logs",
		paymentReturnPath("/usage-logs"),
	)
}

func TestPancakeCheckoutUsesCurrentSiteReturnURL(t *testing.T) {
	oldAddress, oldReturn := system_setting.ServerAddress, setting.WaffoPancakeReturnURL
	oldMerchant, oldKey, oldTransport := setting.WaffoPancakeMerchantID, setting.WaffoPancakePrivateKey, http.DefaultTransport
	t.Cleanup(func() {
		system_setting.ServerAddress, setting.WaffoPancakeReturnURL = oldAddress, oldReturn
		setting.WaffoPancakeMerchantID, setting.WaffoPancakePrivateKey, http.DefaultTransport = oldMerchant, oldKey, oldTransport
	})
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	setting.WaffoPancakePrivateKey = string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
	setting.WaffoPancakeMerchantID = "MER_" + strings.Repeat("a", 22)
	requests := make(chan map[string]any, 1)
	// SDK 并发请求只截获测试数据，不真实创建订单；断言留在测试主协程。
	http.DefaultTransport = publicAddressTestTransport(func(request *http.Request) (*http.Response, error) {
		body := `{"data":{"token":"fixture","expiresAt":"2099-01-01T00:00:00Z"}}`
		switch request.URL.Path {
		case "/v1/actions/checkout/create-session":
			var params map[string]any
			if err := common.DecodeJson(request.Body, &params); err != nil {
				return nil, err
			}
			requests <- params
			body = `{"data":{"sessionId":"fixture","checkoutUrl":"https://checkout.example/session","expiresAt":"2099-01-01T00:00:00Z"}}`
		case "/v1/actions/auth/issue-session-token":
		default:
			return nil, fmt.Errorf("unexpected checkout path: %s", request.URL.Path)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	for _, tc := range []struct{ server, override, want string }{
		{"https://site.example/prefix/", "", "https://site.example/prefix/wallet?show_history=true"},
		{"https://changed.example", "", "https://changed.example/wallet?show_history=true"},
		{"https://changed.example", "https://payments.example/finished", "https://payments.example/finished"},
	} {
		system_setting.ServerAddress, setting.WaffoPancakeReturnURL = tc.server, tc.override
		_, err := service.CreateWaffoPancakeCheckoutSession(context.Background(), &service.WaffoPancakeCreateSessionParams{
			ProductID: "PROD_" + strings.Repeat("a", 22), BuyerIdentity: "fixture-user", OrderMerchantExternalID: "fixture-order",
		})
		require.NoError(t, err)
		assert.Equal(t, tc.want, (<-requests)["successUrl"])
	}
}
