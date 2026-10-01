package relay

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// 使用本地上游捕获真正发出的请求，避免只检查转换函数而遗漏路由和鉴权。
func TestGeminiHelperNativeImageRequest(t *testing.T) {
	for _, model := range []string{"gemini-3-pro-image-preview", "gemini-3.1-flash-image-preview"} {
		for _, size := range []string{"2K", "4K"} {
			t.Run(model+"/"+size, func(t *testing.T) {
				type receivedRequest struct {
					path, body, authorization, clientKey string
				}
				received := make(chan receivedRequest, 2)
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					body, _ := io.ReadAll(r.Body)
					received <- receivedRequest{r.URL.RequestURI(), string(body), r.Header.Get("Authorization"), r.Header.Get("x-goog-api-key")}
					// 停在上游错误边界，本测试不依赖数据库，也不产生生成和计费。
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusBadRequest)
					fmt.Fprint(w, `{"error":{"message":"fixture stop","type":"invalid_request_error","code":"fixture"}}`)
				}))
				defer upstream.Close()
				body := fmt.Sprintf(`{"contents":[{"role":"user","parts":[{"text":"画一张竖版苹果图"},{"inlineData":{"mimeType":"image/png","data":"aW1hZ2U="}}]}],"generationConfig":{"responseModalities":["IMAGE"],"imageConfig":{"aspectRatio":"9:16","imageSize":"%s"}}}`, size)
				var request dto.GeminiChatRequest
				require.NoError(t, common.UnmarshalJsonStr(body, &request))
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/nano-banana-pro:generateContent", strings.NewReader(body))
				c.Request.Header.Set("Content-Type", "application/json")
				c.Request.Header.Set("Authorization", "Bearer client-token-must-not-forward")
				c.Request.Header.Set("x-goog-api-key", "client-token-must-not-forward")
				common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeOpenAI)
				common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, upstream.URL)
				common.SetContextKey(c, constant.ContextKeyChannelKey, "upstream-test-key")
				common.SetContextKey(c, constant.ContextKeyOriginalModel, "nano-banana-pro")
				c.Set("model_mapping", fmt.Sprintf(`{"nano-banana-pro":%q}`, model))
				info := &relaycommon.RelayInfo{
					Request: &request, OriginModelName: "nano-banana-pro",
					RelayFormat: types.RelayFormatGemini, RelayMode: relayconstant.RelayModeGemini,
					RequestURLPath: c.Request.URL.Path, StartTime: time.Now(), DisablePing: true,
				}
				apiErr := GeminiHelper(c, info)
				require.NotNil(t, apiErr)
				require.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
				select {
				case got := <-received:
					assert.Equal(t, "/v1beta/models/"+model+":generateContent", got.path)
					assert.Equal(t, "9:16", gjson.Get(got.body, "generationConfig.imageConfig.aspectRatio").String())
					assert.Equal(t, size, gjson.Get(got.body, "generationConfig.imageConfig.imageSize").String())
					assert.Equal(t, "IMAGE", gjson.Get(got.body, "generationConfig.responseModalities.0").String())
					assert.Equal(t, "aW1hZ2U=", gjson.Get(got.body, "contents.0.parts.1.inlineData.data").String())
					assert.Equal(t, "Bearer upstream-test-key", got.authorization)
					assert.NotEqual(t, "client-token-must-not-forward", got.clientKey)
					assert.False(t, gjson.Get(got.body, "messages").Exists())
				case <-time.After(time.Second):
					t.Fatal("上游没有收到请求")
				}
				assert.Empty(t, received, "原生请求不得更换协议再次提交")
				assert.Equal(t, types.RelayFormat(types.RelayFormatGemini), info.GetFinalRequestRelayFormat())
				assert.Equal(t, constant.ChannelTypeOpenAI, info.ChannelType, "保留实际渠道身份")
			})
		}
	}
}

// 覆盖普通响应和流式响应，图片数据及上游计费明细都必须保持 Gemini 原生格式。
func TestGeminiImageNativeRoundTrip(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%t", stream), func(t *testing.T) {
			response := `{"modelVersion":"gemini-3-pro-image-preview","candidates":[{"content":{"role":"model","parts":[{"inlineData":{"mimeType":"image/png","data":"aW1hZ2U="},"thoughtSignature":"retained-signature"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":20,"totalTokenCount":30,"promptTokensDetails":[{"modality":"TEXT","tokenCount":4},{"modality":"IMAGE","tokenCount":6}],"candidatesTokensDetails":[{"modality":"IMAGE","tokenCount":20}]},"providerExtension":"preserved"}`
			received := make(chan string, 1)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				received <- r.URL.RequestURI()
				if stream {
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprintf(w, "data: %s\n\n", response)
				} else {
					w.Header().Set("Content-Type", "application/json")
					fmt.Fprint(w, response)
				}
			}))
			defer upstream.Close()
			var request dto.GeminiChatRequest
			require.NoError(t, common.UnmarshalJsonStr(`{"contents":[{"role":"user","parts":[{"text":"画图"}]}],"generationConfig":{"responseModalities":["IMAGE"],"imageConfig":{"aspectRatio":"9:16","imageSize":"4K","futureOption":false}}}`, &request))
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/alias:generateContent", nil)
			c.Request.Header.Set("Content-Type", "application/json")
			info := &relaycommon.RelayInfo{
				OriginModelName: "alias", Request: &request, RelayFormat: types.RelayFormatGemini,
				RelayMode: relayconstant.RelayModeGemini, IsStream: stream, StartTime: time.Now(), DisablePing: true,
				ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenAI, ApiType: constant.APITypeOpenAI,
					ChannelBaseUrl: upstream.URL, ApiKey: "upstream-test-key", UpstreamModelName: "gemini-3-pro-image-preview"},
			}
			a := getGeminiRequestAdaptor(info, &request)
			a.Init(info)
			converted, err := a.ConvertGeminiRequest(c, info, &request)
			require.NoError(t, err)
			relaycommon.AppendRequestConversionFromRequest(info, converted)
			body, err := common.Marshal(converted)
			require.NoError(t, err)
			assert.Equal(t, "false", gjson.GetBytes(body, "generationConfig.imageConfig.futureOption").Raw)
			rawResp, err := a.DoRequest(c, info, strings.NewReader(string(body)))
			require.NoError(t, err)
			usageValue, apiErr := a.DoResponse(c, rawResp.(*http.Response), info)
			require.Nil(t, apiErr)
			usage, ok := usageValue.(*dto.Usage)
			require.True(t, ok)
			require.NotNil(t, usage.BillingUsage)
			assert.Equal(t, dto.BillingUsageSourceGeminiChat, usage.BillingUsage.Source)
			require.NotNil(t, usage.BillingUsage.GeminiUsageMetadata)
			metadata, err := common.Marshal(usage.BillingUsage.GeminiUsageMetadata)
			require.NoError(t, err)
			assert.Equal(t, int64(6), gjson.GetBytes(metadata, "promptTokensDetails.1.tokenCount").Int())
			assert.Equal(t, int64(20), gjson.GetBytes(metadata, "candidatesTokensDetails.0.tokenCount").Int())
			assert.Equal(t, 10, usage.PromptTokens)
			assert.Equal(t, 20, usage.CompletionTokens)
			assert.Equal(t, 30, usage.TotalTokens)
			assert.Equal(t, types.RelayFormat(types.RelayFormatGemini), info.GetFinalRequestRelayFormat())
			wantPath := "/v1beta/models/gemini-3-pro-image-preview:generateContent"
			if stream {
				wantPath = "/v1beta/models/gemini-3-pro-image-preview:streamGenerateContent?alt=sse"
				assert.Contains(t, recorder.Body.String(), response)
			} else {
				assert.JSONEq(t, response, recorder.Body.String())
			}
			assert.Equal(t, wantPath, <-received)
			assert.NotContains(t, recorder.Body.String(), "chat.completion")
		})
	}
}

// 只改明确的原生图片请求，普通文本、识图、Azure、OpenRouter 和原生 Gemini 渠道继续走原逻辑。
func TestGeminiImageNativeSelectionBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, config string
		channelType  int
		native       bool
	}{
		{"显式图片模式", `{"responseModalities":["IMAGE"]}`, constant.ChannelTypeOpenAI, true},
		{"显式尺寸配置", `{"imageConfig":{"aspectRatio":"9:16"}}`, constant.ChannelTypeOpenAI, true},
		{"下划线原生字段", `{"response_modalities":["IMAGE"],"image_config":{"aspectRatio":"9:16"}}`, constant.ChannelTypeOpenAI, true},
		{"普通文本", `{}`, constant.ChannelTypeOpenAI, false},
		{"只做识图", `{"responseModalities":["TEXT"]}`, constant.ChannelTypeOpenAI, false},
		{"空图片配置", `{"imageConfig":null}`, constant.ChannelTypeOpenAI, false},
		{"Azure渠道", `{"responseModalities":["IMAGE"]}`, constant.ChannelTypeAzure, false},
		{"OpenRouter渠道", `{"responseModalities":["IMAGE"]}`, constant.ChannelTypeOpenRouter, false},
		{"原生Gemini渠道", `{"responseModalities":["IMAGE"]}`, constant.ChannelTypeGemini, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var request dto.GeminiChatRequest
			require.NoError(t, common.UnmarshalJsonStr(`{"contents":[{"role":"user","parts":[{"text":"描述图片"},{"inlineData":{"mimeType":"image/png","data":"aW1hZ2U="}}]}],"generationConfig":`+tc.config+`}`, &request))
			apiType, _ := common.ChannelType2APIType(tc.channelType)
			info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatGemini, RelayMode: relayconstant.RelayModeGemini,
				RequestURLPath: "/v1beta/models/alias:generateContent",
				ChannelMeta: &relaycommon.ChannelMeta{ChannelType: tc.channelType, ApiType: apiType,
					ChannelBaseUrl: "https://upstream.example", UpstreamModelName: "gemini-fixture", ApiKey: "channel-key"}}
			a := getGeminiRequestAdaptor(info, &request)
			a.Init(info)
			gotURL, err := a.GetRequestURL(info)
			require.NoError(t, err)
			if tc.native {
				assert.Equal(t, "https://upstream.example/v1beta/models/gemini-fixture:generateContent", gotURL)
			} else {
				original := GetAdaptor(apiType)
				original.Init(info)
				wantURL, err := original.GetRequestURL(info)
				require.NoError(t, err)
				assert.Equal(t, wantURL, gotURL, "非图片原生请求维持原有上游路径")
			}
		})
	}
}

// 已启用正文透传的渠道也必须同时使用原生地址，并继续尊重管理员的鉴权头覆盖。
func TestGeminiHelperNativeImagePassthrough(t *testing.T) {
	body := `{"contents":[{"role":"user","parts":[{"text":"原样转发"}]}],"generationConfig":{"responseModalities":["IMAGE"],"imageConfig":{"aspectRatio":"16:9","imageSize":"2K"}},"providerExtension":false}`
	type receivedRequest struct{ path, body, auth string }
	received := make(chan receivedRequest, 2)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		received <- receivedRequest{r.URL.Path, string(b), r.Header.Get("Authorization")}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":{"message":"fixture stop","type":"invalid_request_error"}}`)
	}))
	defer upstream.Close()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	defer common.CleanupBodyStorage(c)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/alias:generateContent", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	var request dto.GeminiChatRequest
	require.NoError(t, common.UnmarshalJsonStr(body, &request))
	common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeOpenAI)
	common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, upstream.URL)
	common.SetContextKey(c, constant.ContextKeyChannelKey, "upstream-test-key")
	common.SetContextKey(c, constant.ContextKeyOriginalModel, "alias")
	common.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{PassThroughBodyEnabled: true})
	common.SetContextKey(c, constant.ContextKeyChannelHeaderOverride, map[string]any{"Authorization": "Bearer admin-override-key"})
	c.Set("model_mapping", `{"alias":"gemini-3.1-flash-image-preview"}`)
	info := &relaycommon.RelayInfo{Request: &request, OriginModelName: "alias", RequestURLPath: c.Request.URL.Path,
		RelayFormat: types.RelayFormatGemini, RelayMode: relayconstant.RelayModeGemini, StartTime: time.Now(), DisablePing: true}
	apiErr := GeminiHelper(c, info)
	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	got := <-received
	assert.Equal(t, "/v1beta/models/gemini-3.1-flash-image-preview:generateContent", got.path)
	assert.Equal(t, body, got.body)
	assert.Equal(t, "Bearer admin-override-key", got.auth)
	assert.Empty(t, received)
	assert.Equal(t, types.RelayFormat(types.RelayFormatGemini), info.GetFinalRequestRelayFormat())
}
