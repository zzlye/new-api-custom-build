package oauth

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type publicOAuthTestTransport func(*http.Request) (*http.Response, error)

func (transport publicOAuthTestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func TestOAuthTokenExchangeUsesConfiguredPublicCallback(t *testing.T) {
	previousAddress, previousTransport := system_setting.ServerAddress, http.DefaultTransport
	previousEndpoint := system_setting.GetOIDCSettings().TokenEndpoint
	t.Cleanup(func() {
		system_setting.ServerAddress, http.DefaultTransport = previousAddress, previousTransport
		system_setting.GetOIDCSettings().TokenEndpoint = previousEndpoint
	})
	system_setting.GetOIDCSettings().TokenEndpoint = "https://identity.example/token"
	for _, tc := range []struct {
		name     string
		provider Provider
	}{
		{"discord", &DiscordProvider{}},
		{"oidc", &OIDCProvider{}},
		{"linuxdo", &LinuxDOProvider{}},
		{"custom", NewGenericOAuthProvider(&model.CustomOAuthProvider{Slug: "custom", TokenEndpoint: "https://identity.example/token"})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, base := range []string{"https://public.example/root/", "https://changed.example/"} {
				system_setting.ServerAddress = base
				http.DefaultTransport = publicOAuthTestTransport(func(request *http.Request) (*http.Response, error) {
					require.NoError(t, request.ParseForm())
					assert.Equal(t, strings.TrimRight(base, "/")+"/oauth/"+tc.name, request.Form.Get("redirect_uri"))
					return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"access_token":"fixture"}`))}, nil
				})
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				// 代理内网协议、旧域名与伪造转发头均不得改变回调地址。
				c.Request = httptest.NewRequest("GET", "http://old.example/api/oauth/"+tc.name, nil)
				c.Request.Header.Set("X-Forwarded-Host", "untrusted.example")
				_, err := tc.provider.ExchangeToken(context.Background(), "fixture-code", c)
				require.NoError(t, err)
			}
		})
	}
}

func TestOIDCProvider_GetName(t *testing.T) {
	settings := system_setting.GetOIDCSettings()
	originalDisplayName := settings.DisplayName
	defer func() { settings.DisplayName = originalDisplayName }()

	p := &OIDCProvider{}

	settings.DisplayName = ""
	assert.Equal(t, "OIDC", p.GetName())

	settings.DisplayName = "  Acme SSO  "
	assert.Equal(t, "Acme SSO", p.GetName())
}
