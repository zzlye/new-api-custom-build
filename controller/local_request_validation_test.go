package controller

import (
	"bytes"
	"fmt"
	"io"
	"maps"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLocalParameterValidationPayloads(t *testing.T) {
	for _, tc := range []struct{ name, path, body, want string }{
		{"缺少内容", "/v1beta/models/gemini:generateContent", `{}`, "contents"},
		{"JSON格式错误", "/v1beta/models/gemini:generateContent", `{"contents":`, "JSON"},
		{"JSON空值", "/v1beta/models/gemini:generateContent", `null`, "对象"},
		{"空parts", "/v1beta/models/gemini:generateContent", `{"contents":[{"parts":[]}]}`, "contents[0].parts"},
		{"无效角色", "/v1beta/models/gemini:generateContent", `{"contents":[{"role":"assistant","parts":[{"text":"hi"}]}]}`, "role"},
		{"具体模型兼容角色别名", "/v1beta/models/gemini-3.1-pro-preview:generateContent", `{"contents":[{"role":"assistant","parts":[{"text":"hi"}]},{"role":"user","parts":[{"text":"ok"}]}]}`, ""},
		{"具体模型未知角色拦截", "/v1beta/models/gemini-3.8-flash:generateContent", `{"contents":[{"role":"unknown_role","parts":[{"text":"hi"}]}]}`, "role"},
		{"仅含空文本Part拦截", "/v1beta/models/gemini-4-argon:generateContent", `{"contents":[{"role":"user","parts":[{"text":""}]}]}`, "parts[0].data"},
		{"仅含签名无内容Part拦截", "/v1beta/models/gemini-4-argon:generateContent", `{"contents":[{"role":"model","parts":[{"thoughtSignature":"skip_thought_signature_validator"}]}]}`, "parts[0].data"},
		{"文本类型错误", "/v1beta/models/gemini:streamGenerateContent", `{"contents":[{"parts":[{"text":123}]}]}`, "text"},
		{"工具调用名称为空", "/v1beta/models/gemini:generateContent", `{"contents":[{"parts":[{"functionCall":{"name":"","args":{}}}]}]}`, "functionCall.name"},
		{"工具声明名称为空", "/v1beta/models/gemini:generateContent", `{"contents":[{"parts":[{"text":"hi"}]}],"tools":[{"functionDeclarations":[{"name":""}]}]}`, "functionDeclarations[0].name"},
		{"工具Schema数组缺items拦截", "/v1beta/models/gemini-3.8-flash:streamGenerateContent", `{"contents":[{"role":"user","parts":[{"text":"hi"}]}],"tools":[{"functionDeclarations":[{"name":"search","parameters":{"type":"OBJECT","properties":{"questions":{"type":"ARRAY"},"issues":{"type":"array","items":{}}}}}]}]}`, "properties[questions].items: missing field."},
		{"响应Schema数组缺items拦截", "/v1beta/models/gemini-3.1-pro-preview:generateContent", `{"contents":[{"role":"user","parts":[{"text":"hi"}]}],"generationConfig":{"responseSchema":{"type":"OBJECT","properties":{"json_patch":{"type":"ARRAY"}}}}}`, "response_schema.properties[json_patch].items: missing field."},
		{"工具Schema数组含items合法", "/v1beta/models/gemini-3.8-flash:streamGenerateContent", `{"contents":[{"role":"user","parts":[{"text":"hi"}]}],"tools":[{"functionDeclarations":[{"name":"search","parameters":{"type":"OBJECT","properties":{"questions":{"type":"ARRAY","items":{"type":"STRING"}}}}}]}]}`, ""},
		{"损坏素材", "/v1beta/models/gemini:generateContent", `{"contents":[{"parts":[{"inlineData":{"mimeType":"image/png","data":"secret-not-base64!"}}]}]}`, "Base64"},
		{"素材误传链接", "/v1beta/models/gemini:generateContent", `{"contents":[{"parts":[{"inlineData":{"mimeType":"image/png","data":"https://example.com/a.png"}}]}]}`, "Base64"},
		{"素材伪装HTML错误页", "/v1beta/models/gemini:generateContent", `{"contents":[{"parts":[{"inlineData":{"mimeType":"image/png","data":"PCFET0NUWVBFaHRtbD48aHRtbD40MDMgRm9yYmlkZGVuPC9odG1sPg=="}}]}]}`, "损坏"},
		{"素材类型错误", "/v1beta/models/gemini:generateContent", `{"contents":[{"parts":[{"inlineData":{"data":"YWJj"}}]}]}`, "mimeType"},
		{"素材URI为空", "/v1beta/models/gemini:generateContent", `{"contents":[{"parts":[{"fileData":{"fileUri":""}}]}]}`, "fileUri"},
		{"参数超出范围", "/v1beta/models/gemini:generateContent", `{"contents":[{"parts":[{"text":"hi"}]}],"generationConfig":{"temperature":3}}`, "temperature"},
		{"入口不按3.1别名限制输出", "/v1beta/models/gemini-3.1-pro-preview:generateContent", `{"contents":[{"parts":[{"text":"hi"}]}],"generationConfig":{"maxOutputTokens":128000}}`, ""},
		{"入口不按3.8别名限制输出", "/v1beta/models/gemini-3.8-flash:streamGenerateContent", `{"contents":[{"parts":[{"text":"hi"}]}],"generationConfig":{"maxOutputTokens":128000}}`, ""},
		{"Gemini4支持128000输出Token", "/v1beta/models/gemini-4-argon:generateContent", `{"contents":[{"parts":[{"text":"hi"}]}],"generationConfig":{"maxOutputTokens":128000}}`, ""},
		{"不臆造TopK模型上限", "/v1beta/models/gemini-3.1-pro-preview:generateContent", `{"contents":[{"parts":[{"text":"hi"}]}],"generationConfig":{"topK":1000}}`, ""},
		{"TopK必须为整数", "/v1beta/models/gemini:generateContent", `{"contents":[{"parts":[{"text":"hi"}]}],"generationConfig":{"topK":1.5}}`, "topK"},
		{"思考等级类型错误", "/v1beta/models/gemini:generateContent", `{"contents":[{"parts":[{"text":"hi"}]}],"generationConfig":{"thinkingConfig":{"thinkingLevel":true}}}`, "thinkingLevel"},
		{"思考摘要类型错误", "/v1beta/models/gemini:generateContent", `{"contents":[{"parts":[{"text":"hi"}]}],"generationConfig":{"thinkingConfig":{"includeThoughts":"true"}}}`, "includeThoughts"},
		{"思考预算不能为小数", "/v1beta/models/gemini:generateContent", `{"contents":[{"parts":[{"text":"hi"}]}],"generationConfig":{"thinkingConfig":{"thinkingBudget":2.5}}}`, "thinkingBudget"},
		{"不硬编码候选数量为一", "/v1beta/models/gemini:generateContent", `{"contents":[{"parts":[{"text":"hi"}]}],"generationConfig":{"candidateCount":2}}`, ""},
		{"参数类型错误", "/v1beta/models/gemini:generateContent", `{"contents":[{"parts":[{"text":"hi"}]}],"generationConfig":{"maxOutputTokens":"100"}}`, "maxOutputTokens"},
		{"思考预算越界", "/v1beta/models/gemini:generateContent", `{"contents":[{"parts":[{"text":"hi"}]}],"generationConfig":{"thinkingConfig":{"thinkingBudget":-5}}}`, "thinkingBudget"},
		{"原生生图合法", "/v1beta/models/gemini:generateContent", `{"contents":[{"parts":[{"text":"hi"},{"inlineData":{"mimeType":"image/png","data":"YWJj"}}]}],"generationConfig":{"responseModalities":["IMAGE"],"temperature":0,"imageConfig":{"imageSize":"4K"}}}`, ""},
		{"兼容别名和未知扩展", "/v1/models/gemini:generateContent", `{"contents":[{"parts":[{"file_data":{"file_uri":"gs://bucket/item"}},{"futurePart":{"id":1}}]}],"generation_config":{"temperature":0}}`, ""},
		{"URL安全Base64", "/v1beta/models/gemini:generateContent", `{"contents":[{"parts":[{"inlineData":{"mimeType":"image/png","data":"-_8"}}]}]}`, ""},
		{"工具响应无文本", "/v1beta/models/gemini:generateContent", `{"contents":[{"role":"model","parts":[{"functionCall":{"name":"get_weather"}}]},{"role":"user","parts":[{"functionResponse":{"name":"get_weather","response":{"ok":true}}}]}]}`, ""},
		{"空对话", "/v1/chat/completions", `{"model":"gemini","messages":[]}`, "messages"},
		{"仅有system消息无user消息", "/v1/chat/completions", `{"model":"gemini-4-argon","messages":[{"role":"system","content":"only system"}]}`, "非 system"},
		{"对话内容错误", "/v1/chat/completions", `{"model":"gemini","messages":[{"role":"user","content":123}]}`, "content"},
		{"对话内联图片损坏", "/v1/chat/completions", `{"model":"gemini","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,not-valid-base64!"}}]}]}`, "Base64"},
		{"工具调用合法", "/v1/chat/completions", `{"model":"gemini","messages":[{"role":"assistant","content":null,"tool_calls":[{"id":"x","type":"function","function":{"name":"f","arguments":"{}"}}]},{"role":"tool","tool_call_id":"x","content":"ok"}]}`, ""},
		{"音频回复上下文", "/v1/chat/completions", `{"model":"audio-model","messages":[{"role":"assistant","content":null,"audio":{"id":"audio_reply"}}]}`, ""},
		{"生图缺提示词", "/v1/images/generations", `{"model":"gemini-image"}`, "prompt"},
		{"生图数量越界", "/v1/images/generations", `{"model":"gemini-image","prompt":"hi","n":2147483647}`, "n"},
		{"不支持的接口保持原样", "/v1/videos", `{"provider_specific":true}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
			c.Request.Header.Set("Content-Type", "application/json")
			defer common.CleanupBodyStorage(c)
			err := helper.ValidateLocalRequest(c)
			if tc.want == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.want)
				assert.NotContains(t, err.Error(), "secret-not-base64")
			}
			storage, err := common.GetBodyStorage(c)
			require.NoError(t, err)
			body, err := storage.Bytes()
			require.NoError(t, err)
			assert.Equal(t, tc.body, string(body), "校验不得改写原始请求")
		})
	}
}

func TestLocalParameterValidationRuleTogglesAndKeywords(t *testing.T) {
	// 验证可按子项开关（结构、媒体、生成参数、自定义拦截词）精确控制拦截行为。
	disabled := false
	enabled := true
	bodyWithBadMedia := `{"contents":[{"parts":[{"inlineData":{"mimeType":"image/png","data":"invalid-b64!"}}]}]}`
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini:generateContent", strings.NewReader(bodyWithBadMedia))
	c.Request.Header.Set("Content-Type", "application/json")
	defer common.CleanupBodyStorage(c)

	require.NoError(t, helper.ValidateLocalRequestWithPolicy(c, operation_setting.LocalRequestValidationPolicy{
		CheckStructure:        &enabled,
		CheckMedia:            &disabled,
		CheckGenerationConfig: &enabled,
	}))
	require.ErrorContains(t, helper.ValidateLocalRequestWithPolicy(c, operation_setting.LocalRequestValidationPolicy{
		CheckStructure:        &enabled,
		CheckMedia:            &enabled,
		CheckGenerationConfig: &enabled,
	}), "Base64")
	require.ErrorContains(t, helper.ValidateLocalRequestWithPolicy(c, operation_setting.LocalRequestValidationPolicy{
		CheckStructure:  &disabled,
		CheckMedia:      &disabled,
		BlockedKeywords: "invalid-b64!",
	}), "invalid-b64!")
}

func TestLocalParameterValidationThoughtSignature(t *testing.T) {
	// 覆盖签名格式、原样透传和开关边界；编码合法不等于签名经上游验证有效。
	for _, tc := range []struct {
		name, part        string
		disabled, blocked bool
	}{
		{"签名类型错误", `{"text":"hi","thoughtSignature":123}`, false, true},
		{"签名对象错误", `{"text":"hi","thoughtSignature":{"value":"YWJj"}}`, false, true},
		{"签名编码损坏", `{"text":"hi","thoughtSignature":"secret-invalid-signature!"}`, false, true},
		{"下划线签名编码损坏", `{"text":"hi","thought_signature":"secret-invalid-signature!"}`, false, true},
		{"签名中间截断", `{"text":"hi","thoughtSignature":"Y"}`, false, true},
		{"标准Base64签名", `{"text":"hi","thoughtSignature":"YWJjZA=="}`, false, false},
		{"无填充签名", `{"text":"hi","thoughtSignature":"YWJjZA"}`, false, false},
		{"URL安全签名", `{"text":"hi","thought_signature":"-_8="}`, false, false},
		{"现有兼容签名", `{"functionCall":{"name":"f","args":{}},"thoughtSignature":"context_engineering_is_the_way_to_go"}`, false, false},
		{"现有跳过标记", `{"text":"hi","thoughtSignature":"skip_thought_signature_validator"}`, false, false},
		{"未携带可选签名", `{"text":"hi"}`, false, false},
		{"空签名视为未设置", `{"text":"hi","thoughtSignature":""}`, false, false},
		{"空值签名视为未设置", `{"text":"hi","thoughtSignature":null}`, false, false},
		{"提示词提到签名不拦截", `{"text":"Invalid thought signature."}`, false, false},
		{"流式尾块空文本携带签名", `{"text":"","thoughtSignature":"YWJjZA=="}`, false, false},
		{"关闭生成校验保持透传", `{"text":"hi","thoughtSignature":"secret-invalid-signature!"}`, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, action := range []string{"generateContent", "streamGenerateContent"} {
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				body := `{"contents":[{"role":"model","parts":[` + tc.part + `]}]}`
				if strings.Contains(tc.part, "functionCall") {
					body = `{"contents":[{"role":"model","parts":[` + tc.part + `]},{"role":"user","parts":[{"functionResponse":{"name":"f","response":{}}}]}]}`
				}
				c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-3.1-pro-preview:"+action, strings.NewReader(body))
				c.Request.Header.Set("Content-Type", "application/json")
				defer common.CleanupBodyStorage(c)
				enabled := !tc.disabled
				err := helper.ValidateLocalRequestWithPolicy(c, operation_setting.LocalRequestValidationPolicy{CheckGenerationConfig: &enabled})
				if tc.blocked {
					require.ErrorContains(t, err, "Invalid thought signature.")
					assert.Contains(t, err.Error(), "contents[0].parts[0]")
					assert.NotContains(t, err.Error(), "secret-invalid-signature")
				} else {
					require.NoError(t, err)
				}
				storage, err := common.GetBodyStorage(c)
				require.NoError(t, err)
				actual, err := storage.Bytes()
				require.NoError(t, err)
				assert.Equal(t, body, string(actual), "签名校验不得清洗或改写请求")
			}
		})
	}
	// 单独开启生成参数校验时仍检查签名，不依赖结构和媒体两个开关。
	disabled, enabled := false, true
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/models/gemini:generateContent", strings.NewReader(`{"contents":[{"parts":[{"text":"hi","thoughtSignature":false}]}]}`))
	defer common.CleanupBodyStorage(c)
	require.ErrorContains(t, helper.ValidateLocalRequestWithPolicy(c, operation_setting.LocalRequestValidationPolicy{
		CheckStructure: &disabled, CheckMedia: &disabled, CheckGenerationConfig: &enabled,
	}), "Invalid thought signature.")
}

func TestLocalParameterValidationToolPairing(t *testing.T) {
	// 使用真实消息形状覆盖并行、跨轮及兼容协议，不把合法乱序结果误判为缺失。
	for _, tc := range []struct {
		name, path, body string
		blocked          bool
	}{
		{"原生缺结果", "/v1beta/models/gemini:generateContent", `{"contents":[{"role":"model","parts":[{"functionCall":{"name":"f"}}]}]}`, true},
		{"原生孤立结果", "/v1beta/models/gemini:generateContent", `{"contents":[{"role":"user","parts":[{"functionResponse":{"name":"f","response":{}}}]}]}`, true},
		{"原生并行少一个", "/v1beta/models/gemini:generateContent", `{"contents":[{"role":"model","parts":[{"functionCall":{"name":"f"}},{"functionCall":{"name":"g"}}]},{"role":"user","parts":[{"functionResponse":{"name":"f","response":{}}}]}]}`, true},
		{"原生普通消息打断", "/v1beta/models/gemini:generateContent", `{"contents":[{"role":"model","parts":[{"functionCall":{"name":"f"}}]},{"role":"user","parts":[{"text":"secret-prompt"}]}]}`, true},
		{"原生同名无ID并行", "/v1beta/models/gemini:generateContent", `{"contents":[{"role":"model","parts":[{"functionCall":{"name":"f"}},{"functionCall":{"name":"f"}}]},{"role":"user","parts":[{"functionResponse":{"name":"f","response":{}}},{"functionResponse":{"name":"f","response":{}}}]}]}`, false},
		{"原生按ID乱序", "/v1beta/models/gemini:streamGenerateContent", `{"contents":[{"role":"model","parts":[{"functionCall":{"id":"a","name":"f"}},{"functionCall":{"id":"b","name":"f"}}]},{"role":"user","parts":[{"functionResponse":{"id":"b","name":"f","response":{}}},{"functionResponse":{"id":"a","name":"f","response":{}}}]}]}`, false},
		{"原生重复调用ID", "/v1beta/models/gemini:generateContent", `{"contents":[{"role":"model","parts":[{"functionCall":{"id":"a","name":"f"}},{"functionCall":{"id":"a","name":"f"}}]}]}`, true},
		{"原生同数量重复结果", "/v1beta/models/gemini:generateContent", `{"contents":[{"role":"model","parts":[{"functionCall":{"id":"a","name":"f"}},{"functionCall":{"id":"b","name":"f"}}]},{"role":"user","parts":[{"functionResponse":{"id":"a","name":"f","response":{}}},{"functionResponse":{"id":"a","name":"f","response":{}}}]}]}`, true},
		{"原生工具名不符", "/v1beta/models/gemini:generateContent", `{"contents":[{"role":"model","parts":[{"functionCall":{"id":"a","name":"f"}}]},{"role":"user","parts":[{"functionResponse":{"id":"a","name":"g","response":{}}}]}]}`, true},
		{"原生调用ID未回传", "/v1beta/models/gemini:generateContent", `{"contents":[{"role":"model","parts":[{"functionCall":{"id":"a","name":"f"}}]},{"role":"user","parts":[{"functionResponse":{"name":"f","response":{}}}]}]}`, true},
		{"原生蛇形字段多轮", "/v1/models/gemini:generateContent", `{"contents":[{"role":"model","parts":[{"function_call":{"name":"f"}}]},{"role":"user","parts":[{"function_response":{"name":"f","response":{}}}]},{"role":"model","parts":[{"functionCall":{"name":"g"}}]},{"role":"user","parts":[{"functionResponse":{"name":"g","response":{}}}]}]}`, false},
		{"聊天连续tool乱序", "/v1/chat/completions", `{"model":"gemini","messages":[{"role":"assistant","tool_calls":[{"id":"a","type":"function","function":{"name":"f","arguments":"{}"}},{"id":"b","type":"function","function":{"name":"g","arguments":"{}"}}]},{"role":"tool","tool_call_id":"b","content":"secret-result"},{"role":"tool","tool_call_id":"a","content":"ok"}]}`, false},
		{"聊天漏结果", "/v1/chat/completions", `{"model":"gemini","messages":[{"role":"assistant","tool_calls":[{"id":"a","type":"function","function":{"name":"f","arguments":"{}"}}]},{"role":"user","content":"next"}]}`, true},
		{"聊天孤立tool", "/v1/chat/completions", `{"model":"gemini","messages":[{"role":"tool","tool_call_id":"a","content":"secret-result"}]}`, true},
		{"聊天未知ID", "/v1/chat/completions", `{"model":"gemini","messages":[{"role":"assistant","tool_calls":[{"id":"a","type":"function","function":{"name":"f","arguments":"{}"}}]},{"role":"tool","tool_call_id":"b","content":"ok"}]}`, true},
		{"聊天重复结果", "/v1/chat/completions", `{"model":"gemini","messages":[{"role":"assistant","tool_calls":[{"id":"a","type":"function","function":{"name":"f","arguments":"{}"}}]},{"role":"tool","tool_call_id":"a","content":"ok"},{"role":"tool","tool_call_id":"a","content":"ok"}]}`, true},
		{"聊天旧function格式", "/v1/chat/completions", `{"model":"gemini","messages":[{"role":"assistant","function_call":{"name":"f","arguments":"{}"}},{"role":"function","name":"f","content":"ok"}]}`, false},
		{"仅声明工具", "/v1/chat/completions", `{"model":"gemini","messages":[{"role":"user","content":"hello"}],"tools":[{"type":"function","function":{"name":"f"}}]}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, enabled := range []bool{true, false} {
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
				c.Request.Header.Set("Content-Type", "application/json")
				defer common.CleanupBodyStorage(c)
				err := helper.ValidateLocalRequestWithPolicy(c, operation_setting.LocalRequestValidationPolicy{CheckGenerationConfig: &enabled})
				if tc.blocked && enabled {
					require.ErrorContains(t, err, "function response parts")
					assert.NotContains(t, err.Error(), "secret-")
				} else {
					require.NoError(t, err)
				}
				storage, err := common.GetBodyStorage(c)
				require.NoError(t, err)
				actual, err := storage.Bytes()
				require.NoError(t, err)
				assert.Equal(t, tc.body, string(actual))
			}
		})
	}
}

func TestLocalParameterValidationRules(t *testing.T) {
	disabled := false
	for _, tc := range []struct {
		name, path, body, rules, want string
	}{
		{
			name:  "Gemini 思考级别精确匹配",
			path:  "/v1beta/models/gemini:generateContent",
			body:  `{"generationConfig":{"thinkingConfig":{"thinkingLevel":"MINIMAL"}}}`,
			rules: "generationConfig.thinkingConfig.thinkingLevel=MINIMAL",
			want:  "generationConfig.thinkingConfig.thinkingLevel",
		},
		{
			name:  "不匹配正文中的相同词语",
			path:  "/v1beta/models/gemini:generateContent",
			body:  `{"contents":[{"parts":[{"text":"MINIMAL"}]}]}`,
			rules: "generationConfig.thinkingConfig.thinkingLevel=MINIMAL",
		},
		{
			name:  "兼容接口精确匹配",
			path:  "/v1/chat/completions",
			body:  `{"model":"gemini","reasoning_effort":"minimal"}`,
			rules: "reasoning_effort=minimal",
			want:  "reasoning_effort",
		},
		{
			name:  "无值规则匹配字段存在",
			path:  "/v1/chat/completions",
			body:  `{"logprobs":false}`,
			rules: "logprobs",
			want:  "logprobs",
		},
		{
			name: "多行规则匹配数量参数", path: "/v1beta/models/gemini:generateContent",
			body:  `{"generationConfig":{"candidateCount":2}}`,
			rules: "\nlogprobs=true\r\ngenerationConfig.candidateCount=2\n", want: "generationConfig.candidateCount",
		},
		{
			name: "合法参数值不拦截", path: "/v1beta/models/gemini:generateContent",
			body:  `{"generationConfig":{"thinkingConfig":{"thinkingLevel":"HIGH"},"candidateCount":1}}`,
			rules: "generationConfig.thinkingConfig.thinkingLevel=MINIMAL\ngenerationConfig.candidateCount=2",
		},
		{
			name: "清空规则不拦截", path: "/v1/chat/completions", body: `{"reasoning_effort":"minimal"}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
			c.Request.Header.Set("Content-Type", "application/json")
			defer common.CleanupBodyStorage(c)
			err := helper.ValidateLocalRequestWithPolicy(c, operation_setting.LocalRequestValidationPolicy{
				CheckStructure:        &disabled,
				CheckMedia:            &disabled,
				CheckGenerationConfig: &disabled,
				LocalParameterRules:   tc.rules,
			})
			if tc.want == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.want)
		})
	}
}

// 相同分组内只拦截规则指定模型，原生接口模型以路径为准，正文不能改写规则范围。
func TestLocalParameterValidationModelScope(t *testing.T) {
	rules := "model=gemini-3-pro*,gemini-3.1-pro* && generationConfig.thinkingConfig.thinkingLevel=MINIMAL\n" +
		"model=gemini-3-pro*,gemini-3.1-pro* && reasoning_effort=minimal"
	encoded, err := common.Marshal(map[string]string{"local_parameter_rules": rules})
	require.NoError(t, err)
	policy, err := operation_setting.ParseLocalRequestValidationPolicy(string(encoded))
	require.NoError(t, err)
	for _, tc := range []struct {
		name, path, body string
		blocked          bool
	}{
		{"原生Flash合法minimal", "/v1beta/models/gemini-3-flash:generateContent", `{"contents":[{"parts":[{"text":"hi"}]}],"generationConfig":{"thinkingConfig":{"thinkingLevel":"MINIMAL"}}}`, false},
		{"原生Flash预览合法minimal", "/v1beta/models/gemini-3-flash-preview:streamGenerateContent", `{"contents":[{"parts":[{"text":"hi"}]}],"generationConfig":{"thinkingConfig":{"thinkingLevel":"minimal"}}}`, false},
		{"原生Flash不被正文Pro模型误伤", "/v1/models/gemini-3-flash:generateContent", `{"model":"gemini-3.1-pro-preview","contents":[{"parts":[{"text":"hi"}]}],"generationConfig":{"thinkingConfig":{"thinkingLevel":"minimal"}}}`, false},
		{"原生Pro命中minimal", "/v1beta/models/gemini-3.1-pro-preview:generateContent", `{"contents":[{"parts":[{"text":"hi"}]}],"generationConfig":{"thinkingConfig":{"thinkingLevel":"minimal"}}}`, true},
		{"原生Pro不被正文Flash绕过", "/v1beta/models/gemini-3-pro-preview:generateContent", `{"model":"gemini-3-flash","contents":[{"parts":[{"text":"hi"}]}],"generationConfig":{"thinkingConfig":{"thinkingLevel":"minimal"}}}`, true},
		{"原生Pro合法high", "/v1beta/models/gemini-3.1-pro-preview:generateContent", `{"contents":[{"parts":[{"text":"hi"}]}],"generationConfig":{"thinkingConfig":{"thinkingLevel":"high"}}}`, false},
		{"兼容Flash合法minimal", "/v1/chat/completions", `{"model":"gemini-3-flash","messages":[{"role":"user","content":"hi"}],"reasoning_effort":"minimal"}`, false},
		{"兼容Pro命中minimal", "/v1/chat/completions", `{"model":"gemini-3.1-pro-preview","messages":[{"role":"user","content":"hi"}],"reasoning_effort":"minimal"}`, true},
		{"未知模型不套用Pro限制", "/v1/chat/completions", `{"model":"custom-flash","messages":[{"role":"user","content":"hi"}],"reasoning_effort":"minimal"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
			c.Request.Header.Set("Content-Type", "application/json")
			defer common.CleanupBodyStorage(c)
			err := helper.ValidateLocalRequestWithPolicy(c, policy)
			if tc.blocked {
				require.ErrorContains(t, err, "本地参数规则命中")
			} else {
				require.NoError(t, err)
			}
			storage, err := common.GetBodyStorage(c)
			require.NoError(t, err)
			body, err := storage.Bytes()
			require.NoError(t, err)
			assert.Equal(t, tc.body, string(body))
		})
	}
}

func TestLocalParameterValidationMultipart(t *testing.T) {
	// 校验后继续读取同一上传素材，防止提前解析耗尽编辑图片请求的文件流。
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", "gemini-image"))
	require.NoError(t, writer.WriteField("prompt", "修改参考图"))
	file, err := writer.CreateFormFile("image", "reference.png")
	require.NoError(t, err)
	_, err = file.Write([]byte("test-image-content"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", bytes.NewReader(body.Bytes()))
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	defer common.CleanupBodyStorage(c)
	require.NoError(t, helper.ValidateLocalRequest(c))
	request, err := helper.GetAndValidOpenAIImageRequest(c, relayconstant.RelayModeImagesEdits)
	require.NoError(t, err)
	assert.Equal(t, "修改参考图", request.Prompt)
	storage, err := common.GetBodyStorage(c)
	require.NoError(t, err)
	raw, err := storage.Bytes()
	require.NoError(t, err)
	assert.Equal(t, body.Bytes(), raw)
}

func TestLocalParameterValidationPolicy(t *testing.T) {
	for _, value := range []string{
		`null`, `{"enabled":true}`, `{"rules":[{"group":"","status_code":400}]}`,
		`{"rules":[{"group":"gemini","status_code":200}]}`,
		`{"rules":[{"group":"gemini","status_code":600}]}`,
		`{"rules":[{"group":"gemini","status_code":422.5}]}`,
		`{"local_parameter_rules":"=MINIMAL"}`,
		`{"local_parameter_rules":"Thinking level is unsupported: THINKING_LEVEL_MINIMAL"}`,
		`{"local_parameter_rules":"generationConfig..candidateCount=2"}`,
		`{"local_parameter_rules":"candidateCount>1"}`,
		`{"local_parameter_rules":"model= && reasoning_effort=minimal"}`,
		`{"local_parameter_rules":"model=gemini*pro && reasoning_effort=minimal"}`,
		`{"local_parameter_rules":"model=gemini-3-pro*,,gemini-3.1-pro* && reasoning_effort=minimal"}`,
		`{"local_parameter_rules":"model=gemini-3-pro* && "}`,
		`{"local_parameter_rules":"model=gemini-3-pro* && model=gemini-3.1-pro* && reasoning_effort=minimal"}`,
		`{"local_parameter_rules":"` + strings.Repeat("x\\n", 1001) + `"}`,
		`{"local_parameter_rules":"` + strings.Repeat("x", 65537) + `"}`,
		`{"rules":[{"group":"gemini","status_code":400},{"group":"gemini","status_code":422}]}`,
		`{"enabled":true,"rules":[{"group":"gemini","enabled":true,"status_code":400}],"upstream_cooldown_seconds":-1}`,
		`{"enabled":true,"rules":[{"group":"gemini","enabled":true,"status_code":400}],"upstream_status_codes":"200"}`,
		`{"enabled":true,"rules":[{"group":"gemini","enabled":true,"status_code":400}],"check_structure":false,"check_media":false,"check_generation_config":false,"upstream_cooldown_seconds":0}`,
	} {
		_, err := operation_setting.ParseLocalRequestValidationPolicy(value)
		assert.Error(t, err, value)
	}

	// 验证上游 400 与 status_code=524,openai_error 的匹配逻辑不会误伤普通 503 重试。
	parsed, err := operation_setting.ParseLocalRequestValidationPolicy(
		`{"enabled":true,"rules":[{"group":"gemini","enabled":true,"status_code":400}],"upstream_cooldown_seconds":60,"upstream_status_codes":"400,524","upstream_error_keywords":"status_code=524,openai_error\nINVALID_ARGUMENT"}`,
	)
	require.NoError(t, err)
	assert.True(t, parsed.MatchesUpstreamError(400, "gemini_error", "invalid_request", "bad param"))
	assert.True(t, parsed.MatchesUpstreamError(524, "openai_error", "bad_response_status_code", "bad response status code 524"))
	assert.True(t, parsed.MatchesUpstreamError(422, "gemini_error", "bad_response_status_code", "INVALID_ARGUMENT: bad value"))
	assert.False(t, parsed.MatchesUpstreamError(503, "openai_error", "bad_response_status_code", "service unavailable"))
	// 单独启用参数规则不依赖结构校验或上游冷却。
	_, err = operation_setting.ParseLocalRequestValidationPolicy(`{"enabled":true,"rules":[{"group":"gemini","enabled":true,"status_code":400}],"check_structure":false,"check_media":false,"check_generation_config":false,"upstream_cooldown_seconds":0,"local_parameter_rules":"reasoning_effort=minimal"}`)
	require.NoError(t, err)
}

func TestLocalParameterValidationDatabase(t *testing.T) {
	for _, kind := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(kind, func(t *testing.T) {
			dsn := os.Getenv("TEST_" + strings.ToUpper(kind) + "_DSN")
			if kind != "sqlite" && dsn == "" {
				t.Skip("未配置独立测试库")
			}
			db, _ := newAuditTestDatabase(t, kind, dsn)
			oldDB, oldLog := model.DB, model.LOG_DB
			oldMainType, oldLogType := common.MainDatabaseType(), common.LogDatabaseType()
			oldRedis := common.RedisEnabled
			model.DB, model.LOG_DB, common.RedisEnabled = db, db, false
			common.SetDatabaseTypes(common.DatabaseType(kind), common.DatabaseType(kind))
			common.OptionMapRWMutex.Lock()
			oldOptions := common.OptionMap
			common.OptionMap = maps.Clone(oldOptions)
			if common.OptionMap == nil {
				common.OptionMap = map[string]string{}
			}
			common.OptionMapRWMutex.Unlock()
			middleware.ClearUpstreamErrorCooldownCache()
			t.Cleanup(func() {
				middleware.ClearUpstreamErrorCooldownCache()
				model.DB, model.LOG_DB, common.RedisEnabled = oldDB, oldLog, oldRedis
				common.SetDatabaseTypes(oldMainType, oldLogType)
				common.OptionMapRWMutex.Lock()
				common.OptionMap = oldOptions
				common.OptionMapRWMutex.Unlock()
			})
			require.NoError(t, db.AutoMigrate(&model.Option{}, &model.User{}, &model.Log{}, &model.AsyncRelayTask{}, &model.AuditLog{}))
			user := &model.User{Username: "local-validator", Quota: 123456, Group: "gemini", Status: common.UserStatusEnabled}
			require.NoError(t, db.Create(user).Error)
			policy := `{"enabled":true,"rules":[{"group":"gemini","enabled":true,"status_code":422}],"local_parameter_rules":"generationConfig.thinkingConfig.thinkingLevel=MINIMAL"}`
			for _, role := range []int{common.RoleAdminUser, common.RoleRootUser} {
				response := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(response)
				c.Set("role", role)
				payload, err := common.Marshal(map[string]string{"key": operation_setting.LocalRequestValidationOption, "value": policy})
				require.NoError(t, err)
				c.Request = httptest.NewRequest(http.MethodPut, "/api/option/", strings.NewReader(string(payload)))
				UpdateOption(c)
				if role != common.RoleRootUser {
					assert.Equal(t, 403, response.Code)
				} else {
					require.Contains(t, response.Body.String(), `"success":true`)
				}
			}
			var saved model.Option
			require.NoError(t, db.Where(&model.Option{Key: operation_setting.LocalRequestValidationOption}).First(&saved).Error)
			assert.Equal(t, policy, saved.Value)
			assert.Equal(t, 422, operation_setting.LocalRequestValidationStatus("gemini"))
			require.Error(t, model.UpdateOption(operation_setting.LocalRequestValidationOption, `{"enabled":true,"rules":[]}`))
			assert.Equal(t, 422, operation_setting.LocalRequestValidationStatus("gemini"))
			var upstreamCalls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				upstreamCalls.Add(1)
				w.WriteHeader(503)
				_, _ = w.Write([]byte(`{"error":{"message":"upstream failure"}}`))
			}))
			defer upstream.Close()
			for _, tc := range []struct {
				group, auto, body string
				blocked           bool
			}{
				{"gemini", "", `{"contents":[{"parts":[]}]}`, true},
				{"auto", "gemini", `{"contents":[{"parts":[]}]}`, true},
				{"default", "", `{"contents":[{"parts":[]}]}`, false},
				{"gemini", "", `{"contents":[{"parts":[{"text":"hi"}]}]}`, false},
				{"gemini", "", `{"contents":[{"parts":[{"text":"hi"}]}],"generationConfig":{"thinkingConfig":{"thinkingLevel":"MINIMAL"}}}`, true},
				{"default", "", `{"contents":[{"parts":[{"text":"hi"}]}],"generationConfig":{"thinkingConfig":{"thinkingLevel":"MINIMAL"}}}`, false},
				{"gemini", "", `{"contents":[{"parts":[{"text":"hi","thoughtSignature":"secret-invalid-signature!"}]}]}`, true},
				{"auto", "gemini", `{"contents":[{"parts":[{"text":"hi","thought_signature":123}]}]}`, true},
				{"default", "", `{"contents":[{"parts":[{"text":"hi","thoughtSignature":"secret-invalid-signature!"}]}]}`, false},
				{"gemini", "", `{"contents":[{"parts":[{"text":"hi","thoughtSignature":"YWJjZA=="}]}]}`, false},
				{"gemini", "", `{"contents":[{"parts":[{"functionCall":{"name":"f"}}]}]}`, true},
				{"auto", "gemini", `{"contents":[{"parts":[{"functionResponse":{"name":"f","response":{}}}]}]}`, true},
				{"default", "", `{"contents":[{"parts":[{"functionCall":{"name":"f"}}]}]}`, false},
				{"gemini", "", `{"contents":[{"parts":[{"functionCall":{"name":"f"}}]},{"parts":[{"functionResponse":{"name":"f","response":{}}}]}]}`, false},
				{"gemini", "", `{"model":"gemini","messages":[{"role":"assistant","tool_calls":[{"id":"a","type":"function","function":{"name":"f","arguments":"{}"}}]}]}`, true},
				{"auto", "gemini", `{"model":"gemini","messages":[{"role":"tool","tool_call_id":"a","content":"secret-result"}]}`, true},
				{"default", "", `{"model":"gemini","messages":[{"role":"assistant","tool_calls":[{"id":"a","type":"function","function":{"name":"f","arguments":"{}"}}]}]}`, false},
				{"gemini", "", `{"model":"gemini","messages":[{"role":"assistant","tool_calls":[{"id":"a","type":"function","function":{"name":"f","arguments":"{}"}}]},{"role":"tool","tool_call_id":"a","content":"ok"}]}`, false},
			} {
				before := upstreamCalls.Load()
				router := gin.New()
				router.Use(middleware.BodyStorageCleanup(), func(c *gin.Context) {
					c.Set("id", user.Id)
					c.Set("username", user.Username)
					common.SetContextKey(c, constant.ContextKeyUsingGroup, tc.group)
					common.SetContextKey(c, constant.ContextKeyAutoGroup, tc.auto)
					if middleware.CheckLocalRequestParameters(c) {
						return
					}
					c.Next()
				})
				forward := func(c *gin.Context) {
					res, err := http.Post(upstream.URL, "application/json", strings.NewReader(tc.body))
					require.NoError(t, err)
					defer res.Body.Close()
					body, err := io.ReadAll(res.Body)
					require.NoError(t, err)
					c.Data(res.StatusCode, "application/json", body)
				}
				router.POST("/v1beta/models/*path", forward)
				router.POST("/v1/chat/completions", forward)
				response := httptest.NewRecorder()
				requestPath := "/v1beta/models/gemini:generateContent"
				if strings.Contains(tc.body, `"messages"`) {
					requestPath = "/v1/chat/completions"
				}
				request := httptest.NewRequest(http.MethodPost, requestPath, strings.NewReader(tc.body))
				request.Header.Set("Content-Type", "application/json")
				router.ServeHTTP(response, request)
				if tc.blocked {
					assert.Equal(t, 422, response.Code)
					if strings.Contains(tc.body, "MINIMAL") {
						assert.Contains(t, response.Body.String(), "generationConfig.thinkingConfig.thinkingLevel")
					} else if requestPath == "/v1/chat/completions" {
						assert.Contains(t, response.Body.String(), "messages[0]")
					} else {
						assert.Contains(t, response.Body.String(), "contents[0].parts")
					}
					assert.Contains(t, response.Body.String(), "local_parameter_error")
					if strings.Contains(tc.body, "thoughtSignature") || strings.Contains(tc.body, "thought_signature") {
						assert.Contains(t, response.Body.String(), "Invalid thought signature.")
						assert.NotContains(t, response.Body.String(), "secret-invalid-signature")
					}
					assert.Equal(t, before, upstreamCalls.Load())
				} else {
					assert.Equal(t, 503, response.Code)
					assert.Contains(t, response.Body.String(), "upstream failure")
					assert.Equal(t, before+1, upstreamCalls.Load())
				}
			}
			// 验证上游返回 524 (openai_error) 后记录请求指纹冷却，同一用户重复请求在本地拦截，不同用户互不影响。
			var timeoutCalls atomic.Int32
			currentUserID := user.Id
			timeoutBody := `{"contents":[{"parts":[{"text":"trigger-524-timeout"}]}]}`
			timeoutRouter := gin.New()
			timeoutRouter.Use(middleware.BodyStorageCleanup(), func(c *gin.Context) {
				c.Set("id", currentUserID)
				c.Set("username", user.Username)
				common.SetContextKey(c, constant.ContextKeyUsingGroup, "gemini")
				if middleware.CheckLocalRequestParameters(c) {
					return
				}
				c.Next()
			})
			timeoutRouter.POST("/v1beta/models/*path", func(c *gin.Context) {
				timeoutCalls.Add(1)
				apiErr := types.NewOpenAIError(fmt.Errorf("bad response status code 524"), types.ErrorCodeBadResponseStatusCode, 524)
				middleware.ObserveGroupUpstreamError(c, apiErr)
				assert.True(t, types.IsSkipRetryError(apiErr))
				c.JSON(524, gin.H{"error": apiErr.ToOpenAIError()})
			})
			firstResp := httptest.NewRecorder()
			timeoutRouter.ServeHTTP(firstResp, httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini:generateContent", strings.NewReader(timeoutBody)))
			assert.Equal(t, 524, firstResp.Code)
			assert.Equal(t, int32(1), timeoutCalls.Load())

			secondResp := httptest.NewRecorder()
			timeoutRouter.ServeHTTP(secondResp, httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini:generateContent", strings.NewReader(timeoutBody)))
			assert.Equal(t, 422, secondResp.Code)
			assert.Contains(t, secondResp.Body.String(), "local_error_cooldown_intercepted")
			assert.Equal(t, int32(1), timeoutCalls.Load(), "冷却期内同一用户的相同 524 请求不得再次发往上游")

			// 切换到另一个用户发送相同请求，不受前一个用户的冷却记录影响。
			currentUserID = user.Id + 100
			otherUserResp := httptest.NewRecorder()
			timeoutRouter.ServeHTTP(otherUserResp, httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini:generateContent", strings.NewReader(timeoutBody)))
			assert.Equal(t, 524, otherUserResp.Code)
			assert.Equal(t, int32(2), timeoutCalls.Load(), "不同用户的请求冷却状态相互独立")

			// 实际分配入口的拒绝请求不进入 Relay，因此即使图片请求也不会创建任务或预扣费。
			router := gin.New()
			router.Use(middleware.BodyStorageCleanup(), func(c *gin.Context) {
				c.Set("id", user.Id)
				c.Set("username", user.Username)
				common.SetContextKey(c, constant.ContextKeyUsingGroup, "gemini")
			}, middleware.Distribute())
			router.POST("/v1beta/models/*path", func(c *gin.Context) { Relay(c, types.RelayFormatGemini) })
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-image:generateContent", strings.NewReader(`{"contents":[]}`)))
			assert.Equal(t, 422, response.Code)
			var tasks int64
			require.NoError(t, db.Model(&model.AsyncRelayTask{}).Count(&tasks).Error)
			assert.Zero(t, tasks)
			var logs []model.Log
			require.NoError(t, db.Order("id").Find(&logs).Error)
			require.Len(t, logs, 11)
			for _, entry := range logs {
				assert.Equal(t, model.LogTypeError, entry.Type)
				assert.Equal(t, "gemini", entry.Group)
				assert.Zero(t, entry.Quota)
				assert.NotContains(t, entry.Content, "secret-invalid-signature")
				assert.NotContains(t, entry.Content, "secret-result")
				assert.Contains(t, entry.Other, `"status_code":422`)
			}
			require.NoError(t, db.First(user, user.Id).Error)
			assert.Equal(t, 123456, user.Quota)
			// 关闭策略立即恢复原有行为，已记录的错误仍留在日志里。
			require.NoError(t, model.UpdateOption(operation_setting.LocalRequestValidationOption, strings.Replace(policy, `"enabled":true`, `"enabled":false`, 1)))
			assert.Zero(t, operation_setting.LocalRequestValidationStatus("gemini"))
			t.Log(fmt.Sprintf("%s：设置权限、持久化、分组拦截、524冷却防刷、上游零请求及错误日志通过", kind))
		})
	}
}
