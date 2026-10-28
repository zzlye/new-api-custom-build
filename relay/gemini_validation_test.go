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
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// 从实际处理器走模型映射、转换和参数覆盖，只有合法的最终请求才能到达测试上游。
func TestGeminiOutboundValidation(t *testing.T) {
	common.OptionMapRWMutex.Lock()
	oldPolicy, existed := common.OptionMap[operation_setting.LocalRequestValidationOption]
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string)
	}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		defer common.OptionMapRWMutex.Unlock()
		if existed {
			common.OptionMap[operation_setting.LocalRequestValidationOption] = oldPolicy
		} else {
			delete(common.OptionMap, operation_setting.LocalRequestValidationOption)
		}
	})
	for _, tc := range []struct {
		name, model, config, override, wantLevel         string
		passThrough, disabled, generationOff, otherGroup bool
		format                                           types.RelayFormat
		wantError                                        string
	}{
		{name: "映射后Flash原样允许MINIMAL", model: "gemini-3-flash-preview", config: `{"thinkingConfig":{"thinkingLevel":"MINIMAL"}}`, wantLevel: "MINIMAL"},
		{name: "原生Pro不支持minimal", model: "gemini-3.1-pro-preview", config: `{"thinkingConfig":{"thinkingLevel":"MINIMAL"}}`, wantError: "thinkingLevel"},
		{name: "Flash默认枚举", model: "gemini-3-flash-preview", config: `{"thinkingConfig":{"thinkingLevel":"THINKING_LEVEL_UNSPECIFIED"}}`, wantLevel: "THINKING_LEVEL_UNSPECIFIED"},
		{name: "2.5默认枚举和预算", model: "gemini-2.5-flash", config: `{"thinkingConfig":{"thinkingLevel":"THINKING_LEVEL_UNSPECIFIED","thinkingBudget":1024}}`, wantLevel: "THINKING_LEVEL_UNSPECIFIED"},
		{name: "渠道覆盖Pro非法等级", model: "gemini-3.1-pro-preview", override: "minimal", wantError: "thinkingLevel"},
		{name: "渠道覆盖Flash合法等级", model: "gemini-3-flash-preview", override: "minimal", wantLevel: "minimal"},
		{name: "透传Pro非法等级", model: "gemini-3.1-pro-preview", config: `{"thinkingConfig":{"thinkingLevel":"MINIMAL"}}`, passThrough: true, wantError: "thinkingLevel"},
		{name: "透传Flash合法等级", model: "gemini-3-flash-preview", config: `{"thinkingConfig":{"thinkingLevel":"MINIMAL"}}`, passThrough: true, wantLevel: "MINIMAL"},
		{name: "未知型号不套用Pro", model: "gemini-future-pro", config: `{"thinkingConfig":{"thinkingLevel":"minimal"}}`, passThrough: true, wantLevel: "minimal"},
		{name: "不按系列限制输出及采样", model: "gemini-3-future", config: `{"maxOutputTokens":128000,"topK":1000}`, passThrough: true},
		{name: "关闭策略不拦截", model: "gemini-3.1-pro-preview", override: "minimal", disabled: true, wantLevel: "minimal"},
		{name: "关闭生成参数检查不拦截", model: "gemini-3.1-pro-preview", override: "minimal", generationOff: true, wantLevel: "minimal"},
		{name: "其他分组不拦截", model: "gemini-3.1-pro-preview", override: "minimal", otherGroup: true, wantLevel: "minimal"},
		{name: "OpenAI转Flash保留minimal", model: "gemini-3-flash-preview", format: types.RelayFormatOpenAI, wantLevel: "minimal"},
		{name: "OpenAI转Pro保持兼容降级", model: "gemini-3.1-pro-preview", format: types.RelayFormatOpenAI, wantLevel: "low"},
		{name: "OpenAI转换后覆盖非法等级", model: "gemini-3.1-pro-preview", format: types.RelayFormatOpenAI, override: "minimal", wantError: "thinkingLevel"},
		{name: "Claude转换后覆盖非法等级", model: "gemini-3.1-pro-preview", format: types.RelayFormatClaude, override: "minimal", wantError: "thinkingLevel"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			common.OptionMapRWMutex.Lock()
			common.OptionMap[operation_setting.LocalRequestValidationOption] = fmt.Sprintf(`{"enabled":%t,"check_generation_config":%t,"rules":[{"group":"gemini","enabled":true,"status_code":422}]}`, !tc.disabled, !tc.generationOff)
			common.OptionMapRWMutex.Unlock()
			received := make(chan string, 1)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				received <- string(body)
				// 停在上游响应边界，不依赖数据库，也不触发生成及计费。
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTeapot)
				fmt.Fprint(w, `{"error":{"message":"fixture stop"}}`)
			}))
			defer upstream.Close()
			config := tc.config
			if config == "" {
				config = `{}`
			}
			body := `{"contents":[{"role":"user","parts":[{"text":"hi"}]}],"generationConfig":` + config + `}`
			path := "/v1beta/models/client-alias:generateContent"
			format := tc.format
			if format == "" {
				format = types.RelayFormatGemini
			}
			var request dto.Request
			switch format {
			case types.RelayFormatOpenAI:
				body = `{"model":"client-alias","messages":[{"role":"user","content":"hi"}],"reasoning_effort":"minimal"}`
				path = "/v1/chat/completions"
				request = &dto.GeneralOpenAIRequest{}
			case types.RelayFormatClaude:
				body = `{"model":"client-alias","messages":[{"role":"user","content":"hi"}],"max_tokens":100}`
				path = "/v1/messages"
				request = &dto.ClaudeRequest{}
			default:
				request = &dto.GeminiChatRequest{}
			}
			require.NoError(t, common.UnmarshalJsonStr(body, request))
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")
			defer common.CleanupBodyStorage(c)
			common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeGemini)
			common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, upstream.URL)
			common.SetContextKey(c, constant.ContextKeyChannelKey, "fixture-key")
			common.SetContextKey(c, constant.ContextKeyOriginalModel, "client-alias")
			group := "gemini"
			if tc.otherGroup {
				group = "default"
			}
			common.SetContextKey(c, constant.ContextKeyUsingGroup, group)
			common.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{PassThroughBodyEnabled: tc.passThrough})
			c.Set("model_mapping", fmt.Sprintf(`{"client-alias":%q}`, tc.model))
			if tc.override != "" {
				common.SetContextKey(c, constant.ContextKeyChannelParamOverride, map[string]any{
					"generationConfig": map[string]any{"thinkingConfig": map[string]any{"thinkingLevel": tc.override}},
				})
			}
			info := &relaycommon.RelayInfo{Request: request, OriginModelName: "client-alias", UsingGroup: group, RelayFormat: format, RelayMode: relayconstant.RelayModeGemini, RequestURLPath: path, StartTime: time.Now(), DisablePing: true}
			var apiErr *types.NewAPIError
			switch format {
			case types.RelayFormatOpenAI:
				apiErr = TextHelper(c, info)
			case types.RelayFormatClaude:
				apiErr = ClaudeHelper(c, info)
			default:
				apiErr = GeminiHelper(c, info)
			}
			require.NotNil(t, apiErr)
			if tc.wantError != "" {
				assert.Equal(t, 422, apiErr.StatusCode)
				assert.Equal(t, types.ErrorCode("local_parameter_error"), apiErr.GetErrorCode())
				assert.Equal(t, types.ErrorTypeNewAPIError, apiErr.GetErrorType())
				assert.Contains(t, apiErr.Error(), tc.wantError)
				assert.True(t, types.IsSkipRetryError(apiErr))
				assert.False(t, service.ShouldDisableChannel(apiErr))
				assert.Equal(t, "stop", service.DecideRelayRetry(c, apiErr, 5).Action)
				assert.Empty(t, received, "拒绝时不能发送任何上游请求")
			} else {
				require.Equal(t, http.StatusTeapot, apiErr.StatusCode, apiErr.Error())
				require.Len(t, received, 1)
				assert.Equal(t, tc.wantLevel, gjson.Get(<-received, "generationConfig.thinkingConfig.thinkingLevel").String())
			}
		})
	}
}
