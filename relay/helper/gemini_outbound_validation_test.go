package helper

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// 同一等级在不同具体模型上可能合法或非法，不能复用分组级黑名单。
func TestGeminiDocumentedThinkingParameters(t *testing.T) {
	for _, tc := range []struct{ model, config, want string }{
		{"gemini-3-flash-preview", `{"thinkingLevel":"MINIMAL"}`, ""},
		{"gemini-3-flash", `{"thinking_level":"medium"}`, ""},
		{"gemini-3-pro-preview", `{"thinkingLevel":"medium"}`, "thinkingLevel"},
		{"gemini-3.1-pro-preview", `{"thinkingLevel":"medium"}`, ""},
		{"gemini-3.1-pro-preview", `{"thinkingLevel":"minimal"}`, "thinkingLevel"},
		{"gemini-3.1-flash-lite", `{"thinkingLevel":"minimal"}`, ""},
		{"gemini-3.6-flash", `{"thinkingLevel":"minimal"}`, ""},
		{"gemini-3.8-flash", `{"thinkingLevel":"minimal"}`, "thinkingLevel"},
		{"gemini-4-argon", `{"thinkingLevel":"high"}`, ""},
		{"gemini-4-argon", `{"thinkingLevel":"minimal"}`, "thinkingLevel"},
		{"gemini-3.1-flash-lite-image", `{"thinkingLevel":"low"}`, "thinkingLevel"},
		{"gemini-3.1-flash-lite-image", `{"thinkingLevel":"high"}`, ""},
		{"gemini-3-flash-preview", `{"thinkingLevel":"THINKING_LEVEL_MINIMAL"}`, "thinkingLevel"},
		{"gemini-3-flash-preview", `{"thinkingLevel":"THINKING_LEVEL_UNSPECIFIED","thinkingBudget":1024}`, ""},
		{"gemini-3-flash-preview", `{"thinkingLevel":"low","thinkingBudget":1024}`, "同时"},
		{"gemini-3-flash-preview", `{"thinking_level":"low","thinking_budget":1024}`, "同时"},
		{"gemini-2.5-pro", `{"thinkingBudget":0}`, "thinkingBudget"},
		{"gemini-2.5-pro", `{"thinkingBudget":128}`, ""},
		{"gemini-2.5-pro", `{"thinkingBudget":32768}`, ""},
		{"gemini-2.5-pro", `{"thinkingBudget":32769}`, "thinkingBudget"},
		{"gemini-2.5-flash", `{"thinkingBudget":0}`, ""},
		{"gemini-2.5-flash", `{"thinkingBudget":24577}`, "thinkingBudget"},
		{"gemini-2.5-flash-lite", `{"thinkingBudget":0}`, ""},
		{"gemini-2.5-flash-lite", `{"thinkingBudget":-1}`, ""},
		{"gemini-2.5-flash-lite", `{"thinkingBudget":511}`, "thinkingBudget"},
		{"gemini-2.5-flash-lite", `{"thinkingBudget":512}`, ""},
		{"gemini-2.5-flash", `{"thinkingLevel":"high"}`, "thinkingBudget"},
		{"gemini-3-unknown", `{"thinkingLevel":"future"}`, ""},
		{"gemini-2.5-unknown", `{"thinkingBudget":50000}`, ""},
		{"gemini-3-flash-preview", `{"includeThoughts":false}`, ""},
		{"gemini-3-flash-preview", `{"thinkingLevel":{}}`, "字符串"},
		{"gemini-3-flash-preview", `{"includeThoughts":1}`, "布尔"},
	} {
		t.Run(tc.model+tc.config, func(t *testing.T) {
			body := []byte(fmt.Sprintf(`{"generationConfig":{"thinkingConfig":%s}}`, tc.config))
			err := ValidateGeminiOutboundParameters(body, tc.model)
			if tc.want == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.want)
			}
		})
	}
}
