package helper

import (
	"fmt"
	"slices"
	"strings"

	"github.com/tidwall/gjson"
)

// ValidateGeminiOutboundParameters 只校验转换与渠道覆盖后的请求，不修改参数。
// 型号能力依据 https://ai.google.dev/gemini-api/docs/generate-content/thinking 。
// 未收录的型号保留上游扩展能力，不根据 gemini-3 等系列前缀猜测支持范围。
func ValidateGeminiOutboundParameters(body []byte, model string) error {
	root := gjson.ParseBytes(body)
	if !gjson.ValidBytes(body) || !root.IsObject() {
		return fmt.Errorf("Gemini 请求体必须是有效的 JSON 对象")
	}
	if err := validateGeminiGenerationConfig(root); err != nil {
		return err
	}
	var levels []string
	var minBudget, maxBudget int
	var budgetModel, canDisable bool
	switch strings.ToLower(model) {
	case "gemini-3-flash", "gemini-3-flash-preview",
		"gemini-3.1-flash-lite", "gemini-3.1-flash-lite-preview",
		"gemini-3.5-flash", "gemini-3.5-flash-lite", "gemini-3.6-flash",
		"gemini-robotics-er-2-preview":
		levels = []string{"minimal", "low", "medium", "high"}
	case "gemini-3-pro", "gemini-3-pro-preview":
		levels = []string{"low", "high"}
	case "gemini-3.1-pro", "gemini-3.1-pro-preview", "gemini-3.1-pro-preview-customtools",
		"gemini-3.8-flash", "gemini-4-argon":
		levels = []string{"low", "medium", "high"}
	case "gemini-3.1-flash-lite-image", "gemini-3.1-flash-lite-image-preview":
		levels = []string{"minimal", "high"}
	case "gemini-2.5-pro":
		budgetModel, minBudget, maxBudget = true, 128, 32768
	case "gemini-2.5-flash":
		budgetModel, canDisable, maxBudget = true, true, 24576
	case "gemini-2.5-flash-lite":
		budgetModel, canDisable, minBudget, maxBudget = true, true, 512, 24576
	default:
		return nil
	}
	for _, name := range []string{"generationConfig", "generation_config"} {
		for _, key := range []string{"thinkingConfig", "thinking_config"} {
			thinking := root.Get(name + "." + key)
			if !thinking.IsObject() {
				continue
			}
			level := thinking.Get("thinkingLevel")
			if alternate := thinking.Get("thinking_level"); alternate.Exists() && alternate.Type != gjson.Null {
				level = alternate
			}
			budget := thinking.Get("thinkingBudget")
			if alternate := thinking.Get("thinking_budget"); alternate.Exists() && alternate.Type != gjson.Null {
				budget = alternate
			}
			value := strings.ToLower(strings.TrimSpace(level.Str))
			// 官方默认枚举等同未指定，不与 thinkingBudget 构成冲突。
			if value == "thinking_level_unspecified" {
				value = ""
			}
			hasBudget := budget.Exists() && budget.Type != gjson.Null
			if value != "" && hasBudget {
				return fmt.Errorf("generationConfig.thinkingConfig 不能同时设置 thinkingLevel 和 thinkingBudget")
			}
			if value != "" && !slices.Contains(levels, value) {
				if budgetModel {
					return fmt.Errorf("模型 %s 的 thinkingConfig 应使用 thinkingBudget，不能直接发送 thinkingLevel", model)
				}
				return fmt.Errorf("模型 %s 的 thinkingLevel 仅支持 %s", model, strings.Join(levels, ", "))
			}
			if budgetModel && hasBudget {
				n := budget.Int()
				if n != -1 && !(n == 0 && canDisable) && (n < int64(minBudget) || n > int64(maxBudget)) {
					return fmt.Errorf("模型 %s 的 thinkingBudget 必须在 %d 到 %d 之间，或使用 -1 动态预算；允许 0 关闭：%t", model, minBudget, maxBudget, canDisable)
				}
			}
		}
	}
	return nil
}
