package helper

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"math"
	"mime"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// ValidateLocalRequest 使用默认全量规则执行本地参数校验，不请求外部素材或探测上游。
func ValidateLocalRequest(c *gin.Context) error {
	return ValidateLocalRequestWithPolicy(c, operation_setting.LocalRequestValidationPolicy{})
}

// ValidateLocalRequestWithPolicy 按策略开关对当前请求做本地结构、媒体数据、生成参数和关键词校验。
// 未知扩展字段继续透传，避免用过期的模型白名单误伤合法请求。
func ValidateLocalRequestWithPolicy(c *gin.Context, policy operation_setting.LocalRequestValidationPolicy) error {
	if c.Request.Method != http.MethodPost {
		return nil
	}
	path := c.Request.URL.Path
	var format types.RelayFormat
	switch {
	case (strings.HasPrefix(path, "/v1beta/models/") || strings.HasPrefix(path, "/v1/models/")) &&
		(strings.HasSuffix(path, ":generateContent") || strings.HasSuffix(path, ":streamGenerateContent")):
		format = types.RelayFormatGemini
	case path == "/v1/chat/completions" || path == "/pg/chat/completions" || path == "/v1/completions":
		format = types.RelayFormatOpenAI
	case path == "/v1/images/generations" || path == "/v1/images/edits":
		format = types.RelayFormatOpenAIImage
	case path == "/v1/responses":
		format = types.RelayFormatOpenAIResponses
	case path == "/v1/messages":
		format = types.RelayFormatClaude
	default:
		return nil
	}
	// 编辑图片表单沿用既有解析器，避免把上传文件当作 JSON。
	if format == types.RelayFormatOpenAIImage && c.ContentType() == "multipart/form-data" {
		if !policy.StructureCheckEnabled() && !policy.GenerationConfigCheckEnabled() && strings.TrimSpace(policy.BlockedKeywords) == "" {
			return nil
		}
		request, err := GetAndValidOpenAIImageRequest(c, relayconstant.Path2RelayMode(path))
		if err != nil {
			return err
		}
		if policy.StructureCheckEnabled() && (strings.TrimSpace(request.Model) == "" || strings.TrimSpace(request.Prompt) == "") {
			return errors.New("model 和 prompt 必须是非空字符串")
		}
		if err := matchBlockedRequestKeywords(request.Prompt, policy.BlockedKeywords); err != nil {
			return err
		}
		return nil
	}
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return err
	}
	body, err := storage.Bytes()
	if err != nil {
		return err
	}
	if !gjson.ValidBytes(body) {
		return errors.New("请求体不是有效的 JSON")
	}
	root := gjson.ParseBytes(body)
	if !root.IsObject() {
		return errors.New("请求体必须是 JSON 对象")
	}
	if err := matchBlockedRequestKeywords(string(body), policy.BlockedKeywords); err != nil {
		return err
	}
	if format == types.RelayFormatGemini {
		return validateLocalGemini(root, policy)
	}
	// 复用原有必填字段和数量边界，额外检查不会改写提交给上游的参数。
	if policy.StructureCheckEnabled() || policy.GenerationConfigCheckEnabled() {
		if _, err := GetAndValidateRequest(c, format); err != nil {
			return fmt.Errorf("请求参数无效：%w", err)
		}
	}
	if (format == types.RelayFormatOpenAI || format == types.RelayFormatClaude) && (policy.StructureCheckEnabled() || policy.MediaCheckEnabled()) {
		if messages := root.Get("messages"); messages.IsArray() {
			for i, message := range messages.Array() {
				field := fmt.Sprintf("messages[%d]", i)
				if policy.StructureCheckEnabled() {
					if !message.IsObject() || message.Get("role").Type != gjson.String || strings.TrimSpace(message.Get("role").Str) == "" {
						return fmt.Errorf("%s.role 必须是非空字符串", field)
					}
					content := message.Get("content")
					// 工具调用及音频回复允许没有文本，保留多模态对话的合法上下文。
					if content.Type == gjson.Null && !message.Get("tool_calls").Exists() && !message.Get("function_call").Exists() && !message.Get("audio").IsObject() {
						return fmt.Errorf("%s.content 不能为空", field)
					}
					if content.Type != gjson.Null && content.Type != gjson.String && !content.IsArray() {
						return fmt.Errorf("%s.content 必须是字符串或数组", field)
					}
				}
				if policy.MediaCheckEnabled() {
					if err := validateOpenAIMessageMedia(message.Get("content"), field+".content"); err != nil {
						return err
					}
				}
			}
		}
	}
	if policy.StructureCheckEnabled() && format == types.RelayFormatOpenAIImage && (root.Get("prompt").Type != gjson.String || strings.TrimSpace(root.Get("prompt").Str) == "") {
		return errors.New("prompt 必须是非空字符串")
	}
	return nil
}

// matchBlockedRequestKeywords 检查请求内容是否命中管理员配置的本地拦截关键词。
func matchBlockedRequestKeywords(content, keywords string) error {
	if strings.TrimSpace(keywords) == "" || content == "" {
		return nil
	}
	lowerContent := strings.ToLower(content)
	for line := range strings.SplitSeq(keywords, "\n") {
		keyword := strings.TrimSpace(line)
		if keyword == "" {
			continue
		}
		if strings.Contains(lowerContent, strings.ToLower(keyword)) {
			return fmt.Errorf("请求包含被拦截的参数或特征：%s", keyword)
		}
	}
	return nil
}

// validateOpenAIMessageMedia 校验 OpenAI 兼容多模态消息中的内联 Data URI 图片格式。
func validateOpenAIMessageMedia(content gjson.Result, field string) error {
	if !content.IsArray() {
		return nil
	}
	for i, part := range content.Array() {
		if !part.IsObject() || part.Get("type").Str != "image_url" {
			continue
		}
		partField := fmt.Sprintf("%s[%d].image_url", field, i)
		imageURL := part.Get("image_url")
		if !imageURL.IsObject() {
			return fmt.Errorf("%s 必须是对象", partField)
		}
		rawURL := imageURL.Get("url")
		if rawURL.Type != gjson.String || strings.TrimSpace(rawURL.Str) == "" {
			return fmt.Errorf("%s.url 必须是非空字符串", partField)
		}
		if strings.HasPrefix(strings.ToLower(rawURL.Str), "data:") {
			header, encoded, ok := strings.Cut(rawURL.Str[5:], ",")
			if !ok || !strings.HasSuffix(strings.ToLower(header), ";base64") {
				return fmt.Errorf("%s.url Data URI 格式无效", partField)
			}
			mimeType := strings.TrimSuffix(header, header[len(header)-7:])
			parsed, _, err := mime.ParseMediaType(mimeType)
			if err != nil || !strings.Contains(parsed, "/") {
				return fmt.Errorf("%s.url 媒体类型无效", partField)
			}
			if err := validateBase64Payload(encoded, partField+".url"); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateBase64Payload 流式校验 Base64 数据合法性，不分配完整缓冲区且不将敏感素材写入报错文本。
func validateBase64Payload(encoded, field string) error {
	if encoded == "" || strings.HasPrefix(strings.ToLower(encoded), "http://") || strings.HasPrefix(strings.ToLower(encoded), "https://") || strings.HasPrefix(strings.ToLower(encoded), "data:") {
		return fmt.Errorf("%s 不是有效的 Base64", field)
	}
	encoding := base64.StdEncoding
	if strings.ContainsAny(encoded, "-_") {
		encoding = base64.URLEncoding
	}
	if !strings.Contains(encoded, "=") {
		encoding = encoding.WithPadding(base64.NoPadding)
	}
	if n, err := io.Copy(io.Discard, base64.NewDecoder(encoding, strings.NewReader(encoded))); err != nil || n == 0 {
		return fmt.Errorf("%s 不是有效的 Base64", field)
	}
	return nil
}

// validateLocalGemini 针对 Gemini 原生请求执行结构、多模态素材及生成参数范围校验。
func validateLocalGemini(root gjson.Result, policy operation_setting.LocalRequestValidationPolicy) error {
	contents := root.Get("contents")
	if policy.StructureCheckEnabled() {
		if !contents.IsArray() || len(contents.Array()) == 0 {
			return errors.New("contents 必须是非空数组")
		}
	}
	if contents.IsArray() && (policy.StructureCheckEnabled() || policy.MediaCheckEnabled()) {
		for i, content := range contents.Array() {
			field := fmt.Sprintf("contents[%d]", i)
			if policy.StructureCheckEnabled() {
				if !content.IsObject() {
					return fmt.Errorf("%s 必须是对象", field)
				}
				if role := content.Get("role"); role.Exists() && role.Type != gjson.Null && (role.Type != gjson.String || (role.Str != "" && role.Str != "user" && role.Str != "model")) {
					return fmt.Errorf("%s.role 必须是 user 或 model", field)
				}
			}
			parts := content.Get("parts")
			if policy.StructureCheckEnabled() && (!parts.IsArray() || len(parts.Array()) == 0) {
				return fmt.Errorf("%s.parts 必须是非空数组", field)
			}
			if !parts.IsArray() {
				continue
			}
			for j, part := range parts.Array() {
				partField := fmt.Sprintf("%s.parts[%d]", field, j)
				if policy.StructureCheckEnabled() {
					if !part.IsObject() || len(part.Map()) == 0 {
						return fmt.Errorf("%s 必须是非空对象", partField)
					}
					if text := part.Get("text"); text.Exists() && text.Type != gjson.Null && text.Type != gjson.String {
						return fmt.Errorf("%s.text 必须是字符串", partField)
					}
					for _, fnKey := range []string{"functionCall", "function_call", "functionResponse", "function_response"} {
						fnVal := part.Get(fnKey)
						if fnVal.Exists() && fnVal.Type != gjson.Null {
							if !fnVal.IsObject() || fnVal.Get("name").Type != gjson.String || strings.TrimSpace(fnVal.Get("name").Str) == "" {
								return fmt.Errorf("%s.%s.name 必须是非空字符串", partField, fnKey)
							}
						}
					}
				}
				if policy.MediaCheckEnabled() && part.IsObject() {
					for _, name := range []string{"inlineData", "inline_data", "fileData", "file_data"} {
						data := part.Get(name)
						if !data.Exists() || data.Type == gjson.Null {
							continue
						}
						if !data.IsObject() {
							return fmt.Errorf("%s.%s 必须是对象", partField, name)
						}
						mimeType := data.Get("mimeType")
						if !mimeType.Exists() {
							mimeType = data.Get("mime_type")
						}
						// fileData 的 MIME 可由上游推断；inlineData 必须显式给出。
						inline := strings.HasPrefix(name, "inline")
						if inline || mimeType.Exists() {
							parsed, _, err := mime.ParseMediaType(mimeType.Str)
							if err != nil || !strings.Contains(parsed, "/") {
								return fmt.Errorf("%s.%s.mimeType 必须是有效的媒体类型", partField, name)
							}
						}
						if inline {
							encoded := data.Get("data")
							if encoded.Type != gjson.String || encoded.Str == "" {
								return fmt.Errorf("%s.%s.data 必须是非空 Base64 字符串", partField, name)
							}
							if err := validateBase64Payload(encoded.Str, fmt.Sprintf("%s.%s.data", partField, name)); err != nil {
								return err
							}
						} else {
							uri := data.Get("fileUri")
							if !uri.Exists() {
								uri = data.Get("file_uri")
							}
							if uri.Type != gjson.String || strings.TrimSpace(uri.Str) == "" {
								return fmt.Errorf("%s.%s.fileUri 必须是非空字符串", partField, name)
							}
						}
					}
				}
			}
		}
	}
	if !policy.GenerationConfigCheckEnabled() {
		return nil
	}
	for _, name := range []string{"generationConfig", "generation_config"} {
		config := root.Get(name)
		if !config.Exists() || config.Type == gjson.Null {
			continue
		}
		if !config.IsObject() {
			return fmt.Errorf("%s 必须是对象", name)
		}
		for _, key := range []string{"responseModalities", "response_modalities", "stopSequences", "stop_sequences"} {
			value := config.Get(key)
			if !value.Exists() || value.Type == gjson.Null {
				continue
			}
			if !value.IsArray() {
				return fmt.Errorf("%s.%s 必须是字符串数组", name, key)
			}
			for _, item := range value.Array() {
				if item.Type != gjson.String {
					return fmt.Errorf("%s.%s 必须是字符串数组", name, key)
				}
			}
		}
		for _, rule := range []struct {
			key      string
			min, max float64
			integer  bool
		}{
			{"temperature", 0, 2, false}, {"topP", 0, 1, false}, {"top_p", 0, 1, false},
			{"topK", 1, maxTokensLimit, true}, {"top_k", 1, maxTokensLimit, true},
			{"maxOutputTokens", 1, maxTokensLimit, true}, {"max_output_tokens", 1, maxTokensLimit, true},
			{"candidateCount", 1, maxTokensLimit, true}, {"candidate_count", 1, maxTokensLimit, true},
		} {
			value := config.Get(rule.key)
			if !value.Exists() || value.Type == gjson.Null {
				continue
			}
			if value.Type != gjson.Number || value.Num < rule.min || value.Num > rule.max || (rule.integer && math.Trunc(value.Num) != value.Num) {
				return fmt.Errorf("%s.%s 数值或类型无效", name, rule.key)
			}
		}
		for _, thinkKey := range []string{"thinkingConfig", "thinking_config"} {
			thinking := config.Get(thinkKey)
			if !thinking.Exists() || thinking.Type == gjson.Null {
				continue
			}
			if !thinking.IsObject() {
				return fmt.Errorf("%s.%s 必须是对象", name, thinkKey)
			}
			for _, budgetKey := range []string{"thinkingBudget", "thinking_budget"} {
				budget := thinking.Get(budgetKey)
				if !budget.Exists() || budget.Type == gjson.Null {
					continue
				}
				if budget.Type != gjson.Number || budget.Num < -1 || budget.Num > maxTokensLimit || math.Trunc(budget.Num) != budget.Num {
					return fmt.Errorf("%s.%s.%s 数值或类型无效", name, thinkKey, budgetKey)
				}
			}
		}
	}
	return nil
}
