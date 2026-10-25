package helper

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"math"
	"mime"
	"net/http"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// extractGeminiModelFromPath 从 Gemini 请求路径中提取模型名称。
func extractGeminiModelFromPath(path string) string {
	_, rest, ok := strings.Cut(path, "/models/")
	if !ok {
		return ""
	}
	model, _, _ := strings.Cut(rest, ":")
	return strings.ToLower(strings.TrimSpace(model))
}

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
			return errors.New("Invalid request: model and prompt are required (model 和 prompt 必须是非空字符串)")
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
		return errors.New("Invalid JSON payload received (请求体不是有效的 JSON)")
	}
	root := gjson.ParseBytes(body)
	if !root.IsObject() {
		return errors.New("Invalid JSON payload: root must be a JSON object (请求体必须是 JSON 对象)")
	}
	if path, matched := policy.MatchLocalParameter(body); matched {
		return fmt.Errorf("本地参数规则命中：%s", path)
	}
	if err := matchBlockedRequestKeywords(string(body), policy.BlockedKeywords); err != nil {
		return err
	}
	if format == types.RelayFormatGemini {
		return validateLocalGemini(root, extractGeminiModelFromPath(path), policy)
	}
	// 复用原有必填字段和数量边界，额外检查不会改写提交给上游的参数。
	if policy.StructureCheckEnabled() || policy.GenerationConfigCheckEnabled() {
		if _, err := GetAndValidateRequest(c, format); err != nil {
			return fmt.Errorf("Invalid request parameters (请求参数无效): %w", err)
		}
	}
	if (format == types.RelayFormatOpenAI || format == types.RelayFormatClaude) && (policy.StructureCheckEnabled() || policy.MediaCheckEnabled() || policy.GenerationConfigCheckEnabled()) {
		if policy.GenerationConfigCheckEnabled() && format == types.RelayFormatOpenAI {
			if tools := root.Get("tools"); tools.IsArray() {
				var missingItems []string
				for i, tool := range tools.Array() {
					if !tool.IsObject() {
						return fmt.Errorf("Invalid tool definition: tools[%d] must be an object (tools[%d] 必须是对象)", i, i)
					}
					if tool.Get("type").Str == "function" {
						fn := tool.Get("function")
						if !fn.IsObject() || fn.Get("name").Type != gjson.String || strings.TrimSpace(fn.Get("name").Str) == "" {
							return fmt.Errorf("The GenerateContentRequest proto is invalid: * tools[%d].function_declarations[0].name: [REQUIRED_FIELD_MISSING] (tools[%d].function.name 必须是非空字符串)", i, i)
						}
						if params := fn.Get("parameters"); params.IsObject() {
							validateGeminiSchemaItems(params, fmt.Sprintf("GenerateContentRequest.tools[%d].function_declarations[0].parameters", i), &missingItems)
						}
					}
				}
				if len(missingItems) > 0 {
					return errors.New(strings.Join(missingItems, "\n"))
				}
			}
		}
		if messages := root.Get("messages"); messages.IsArray() {
			hasNonSystemMessage := false
			for i, message := range messages.Array() {
				field := fmt.Sprintf("messages[%d]", i)
				roleStr := strings.TrimSpace(message.Get("role").Str)
				if roleStr != "" && roleStr != "system" && roleStr != "developer" {
					hasNonSystemMessage = true
				}
				if policy.StructureCheckEnabled() {
					if !message.IsObject() || message.Get("role").Type != gjson.String || roleStr == "" {
						return fmt.Errorf("Invalid message format: %s.role must be a non-empty string (%s.role 必须是非空字符串)", field, field)
					}
					content := message.Get("content")
					// 工具调用及音频回复允许没有文本，保留多模态对话的合法上下文。
					if content.Type == gjson.Null && !message.Get("tool_calls").Exists() && !message.Get("function_call").Exists() && !message.Get("audio").IsObject() {
						return fmt.Errorf("Invalid message format: %s.content is required (%s.content 不能为空)", field, field)
					}
					if content.Type != gjson.Null && content.Type != gjson.String && !content.IsArray() {
						return fmt.Errorf("Invalid message format: %s.content must be a string or array (%s.content 必须是字符串或数组)", field, field)
					}
				}
				if policy.MediaCheckEnabled() {
					if err := validateOpenAIMessageMedia(message.Get("content"), field+".content"); err != nil {
						return err
					}
				}
			}
			// 防止只传 system 消息导致转换为 Gemini 后 contents 为空而打向上游报 400。
			if policy.StructureCheckEnabled() && len(messages.Array()) > 0 && !hasNonSystemMessage {
				return errors.New("contents is required: messages must contain at least one non-system message (messages 必须至少包含一条非 system 消息)")
			}
		}
	}
	if policy.StructureCheckEnabled() && format == types.RelayFormatOpenAIImage && (root.Get("prompt").Type != gjson.String || strings.TrimSpace(root.Get("prompt").Str) == "") {
		return errors.New("prompt is required (prompt 必须是非空字符串)")
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
			return fmt.Errorf("Request rejected by local keyword rule: %s (请求包含被拦截的参数或特征：%s)", keyword, keyword)
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
			return fmt.Errorf("Invalid media part: %s must be an object (%s 必须是对象)", partField, partField)
		}
		rawURL := imageURL.Get("url")
		if rawURL.Type != gjson.String || strings.TrimSpace(rawURL.Str) == "" {
			return fmt.Errorf("Invalid media part: %s.url must be a non-empty string (%s.url 必须是非空字符串)", partField, partField)
		}
		if strings.HasPrefix(strings.ToLower(rawURL.Str), "data:") {
			header, encoded, ok := strings.Cut(rawURL.Str[5:], ",")
			if !ok || !strings.HasSuffix(strings.ToLower(header), ";base64") {
				return fmt.Errorf("The provided file is malformed or corrupted and could not be decoded. (%s.url Data URI 格式无效)", partField)
			}
			mimeType := strings.TrimSuffix(header, header[len(header)-7:])
			parsed, _, err := mime.ParseMediaType(mimeType)
			if err != nil || !strings.Contains(parsed, "/") {
				return fmt.Errorf("Invalid mimeType in %s.url (%s.url 媒体类型无效)", partField, partField)
			}
			if err := validateBase64Payload(encoded, parsed, partField+".url"); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateBase64Payload 流式校验 Base64 数据合法性及伪装错误页，不分配完整缓冲区且不将敏感素材写入报错文本。
func validateBase64Payload(encoded, mediaType, field string) error {
	if encoded == "" || strings.HasPrefix(strings.ToLower(encoded), "http://") || strings.HasPrefix(strings.ToLower(encoded), "https://") || strings.HasPrefix(strings.ToLower(encoded), "data:") {
		return fmt.Errorf("The provided file is malformed or corrupted and could not be decoded. (%s 不是有效的 Base64)", field)
	}
	encoding := base64.StdEncoding
	if strings.ContainsAny(encoded, "-_") {
		encoding = base64.URLEncoding
	}
	if !strings.Contains(encoded, "=") {
		encoding = encoding.WithPadding(base64.NoPadding)
	}
	decoder := base64.NewDecoder(encoding, strings.NewReader(encoded))
	var head [64]byte
	headN, headErr := io.ReadFull(decoder, head[:])
	if headErr != nil && !errors.Is(headErr, io.EOF) && !errors.Is(headErr, io.ErrUnexpectedEOF) {
		return fmt.Errorf("The provided file is malformed or corrupted and could not be decoded. (%s 不是有效的 Base64)", field)
	}
	restN, err := io.Copy(io.Discard, decoder)
	if err != nil || int64(headN)+restN == 0 {
		return fmt.Errorf("The provided file is malformed or corrupted and could not be decoded. (%s 不是有效的 Base64)", field)
	}
	// 拦截客户端将 HTML/XML/JSON 错误响应页伪装成图片或 PDF 的损坏媒体数据。
	if strings.HasPrefix(mediaType, "image/") || mediaType == "application/pdf" {
		sample := bytes.ToLower(bytes.TrimSpace(head[:headN]))
		for _, badPrefix := range [][]byte{
			[]byte("<!doctype"),
			[]byte("<html"),
			[]byte("<?xml"),
			[]byte("<error"),
			[]byte(`{"error"`),
		} {
			if bytes.HasPrefix(sample, badPrefix) {
				return fmt.Errorf("The provided file is malformed or corrupted and could not be decoded. (%s 媒体文件内容已损坏)", field)
			}
		}
	}
	return nil
}

// isGeminiPartMetadataKey 判断字段是否仅为 Part 元数据（不属于 Protobuf oneof data 内容字段）。
func isGeminiPartMetadataKey(key string) bool {
	switch key {
	case "thought", "thoughtSignature", "thought_signature", "mediaResolution", "media_resolution", "videoMetadata", "video_metadata":
		return true
	default:
		return false
	}
}

// validateGeminiSchemaItems 递归检查 Gemini Schema（tools 参数或 response_schema）中 type=array 的节点是否缺失必填的 items 定义。
func validateGeminiSchemaItems(schema gjson.Result, pathPrefix string, missing *[]string) {
	if !schema.IsObject() || len(*missing) >= 32 {
		return
	}
	schemaType := strings.ToLower(strings.TrimSpace(schema.Get("type").Str))
	if schemaType == "array" {
		items := schema.Get("items")
		if !items.Exists() || items.Type == gjson.Null || (items.IsObject() && len(items.Map()) == 0) {
			*missing = append(*missing, fmt.Sprintf("* %s.items: missing field.", pathPrefix))
		} else if items.IsObject() {
			validateGeminiSchemaItems(items, pathPrefix+".items", missing)
		}
	} else if items := schema.Get("items"); items.IsObject() {
		validateGeminiSchemaItems(items, pathPrefix+".items", missing)
	}
	if props := schema.Get("properties"); props.IsObject() {
		propMap := props.Map()
		keys := make([]string, 0, len(propMap))
		for k := range propMap {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			validateGeminiSchemaItems(propMap[k], fmt.Sprintf("%s.properties[%s]", pathPrefix, k), missing)
		}
	}
	for _, combKey := range []string{"anyOf", "any_of", "oneOf", "one_of", "allOf", "all_of"} {
		if arr := schema.Get(combKey); arr.IsArray() {
			for i, sub := range arr.Array() {
				validateGeminiSchemaItems(sub, fmt.Sprintf("%s.any_of[%d]", pathPrefix, i), missing)
			}
		}
	}
}

// validateLocalGemini 针对 Gemini 原生请求执行结构、多模态素材及生成参数范围校验，并返回与上游一致的详细错误信息。
func validateLocalGemini(root gjson.Result, modelName string, policy operation_setting.LocalRequestValidationPolicy) error {
	contents := root.Get("contents")
	if policy.StructureCheckEnabled() {
		if !contents.IsArray() || len(contents.Array()) == 0 {
			return errors.New("Unable to submit request because Model input cannot be empty: contents is required (contents 必须是非空数组)")
		}
	}
	// 具体 Gemini 模型名称由 Adaptor.ConvertGeminiRequest 自动将常见角色别名（assistant/system/function 等）归一化为 user/model。
	allowRoleAliases := strings.HasPrefix(modelName, "gemini-")
	if contents.IsArray() && (policy.StructureCheckEnabled() || policy.MediaCheckEnabled()) {
		for i, content := range contents.Array() {
			field := fmt.Sprintf("contents[%d]", i)
			if policy.StructureCheckEnabled() {
				if !content.IsObject() {
					return fmt.Errorf("The GenerateContentRequest proto is invalid: %s must be an object (%s 必须是对象)", field, field)
				}
				if role := content.Get("role"); role.Exists() && role.Type != gjson.Null {
					if role.Type != gjson.String {
						return fmt.Errorf("Please use a valid role: user, model. (%s.role 必须是 user 或 model)", field)
					}
					roleVal := strings.TrimSpace(role.Str)
					if allowRoleAliases {
						switch strings.ToLower(roleVal) {
						case "", "user", "model", "system", "assistant", "function", "tool", "developer", "human":
						default:
							return fmt.Errorf("Please use a valid role: user, model. (%s.role 必须是 user 或 model)", field)
						}
					} else if roleVal != "" && roleVal != "user" && roleVal != "model" {
						return fmt.Errorf("Please use a valid role: user, model. (%s.role 必须是 user 或 model)", field)
					}
				}
			}
			parts := content.Get("parts")
			if policy.StructureCheckEnabled() && (!parts.IsArray() || len(parts.Array()) == 0) {
				return fmt.Errorf("The GenerateContentRequest proto is invalid: %s.parts cannot be empty (%s.parts 必须是非空数组)", field, field)
			}
			if !parts.IsArray() {
				continue
			}
			for j, part := range parts.Array() {
				partField := fmt.Sprintf("%s.parts[%d]", field, j)
				if policy.StructureCheckEnabled() {
					partMap := part.Map()
					if !part.IsObject() || len(partMap) == 0 {
						return fmt.Errorf("* GenerateContentRequest.%s.data: required oneof field 'data' must have one initialized field (%s 必须是非空对象)", partField, partField)
					}
					if text := part.Get("text"); text.Exists() && text.Type != gjson.Null && text.Type != gjson.String {
						return fmt.Errorf("The GenerateContentRequest proto is invalid: %s.text must be a string (%s.text 必须是字符串)", partField, partField)
					}
					// 检查 Part 是否包含至少一个有效的 oneof data 字段，拦截会被序列化为空对象的空文本 Part。
					hasDataField := false
					for k, v := range partMap {
						if isGeminiPartMetadataKey(k) || v.Type == gjson.Null {
							continue
						}
						if k == "text" {
							if v.Str != "" {
								hasDataField = true
							}
							continue
						}
						hasDataField = true
					}
					if !hasDataField {
						return fmt.Errorf("* GenerateContentRequest.%s.data: required oneof field 'data' must have one initialized field (%s.data 必须包含非空内容)", partField, partField)
					}
					for _, fnKey := range []string{"functionCall", "function_call", "functionResponse", "function_response"} {
						fnVal := part.Get(fnKey)
						if fnVal.Exists() && fnVal.Type != gjson.Null {
							if !fnVal.IsObject() || fnVal.Get("name").Type != gjson.String || strings.TrimSpace(fnVal.Get("name").Str) == "" {
								return fmt.Errorf("The GenerateContentRequest proto is invalid: * %s.%s.name: [REQUIRED_FIELD_MISSING] (%s.%s.name 必须是非空字符串)", partField, fnKey, partField, fnKey)
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
							return fmt.Errorf("The GenerateContentRequest proto is invalid: %s.%s must be an object (%s.%s 必须是对象)", partField, name, partField, name)
						}
						mimeType := data.Get("mimeType")
						if !mimeType.Exists() {
							mimeType = data.Get("mime_type")
						}
						// fileData 的 MIME 可由上游推断；inlineData 必须显式给出。
						inline := strings.HasPrefix(name, "inline")
						var parsedMime string
						if inline || mimeType.Exists() {
							var err error
							parsedMime, _, err = mime.ParseMediaType(mimeType.Str)
							if err != nil || !strings.Contains(parsedMime, "/") {
								return fmt.Errorf("Unable to submit request because %s.%s.mimeType is invalid or missing (%s.%s.mimeType 必须是有效的媒体类型)", partField, name, partField, name)
							}
						}
						if inline {
							encoded := data.Get("data")
							if encoded.Type != gjson.String || encoded.Str == "" {
								return fmt.Errorf("The provided file is malformed or corrupted and could not be decoded. (%s.%s.data 必须是非空 Base64 字符串)", partField, name)
							}
							if err := validateBase64Payload(encoded.Str, parsedMime, fmt.Sprintf("%s.%s.data", partField, name)); err != nil {
								return err
							}
						} else {
							uri := data.Get("fileUri")
							if !uri.Exists() {
								uri = data.Get("file_uri")
							}
							if uri.Type != gjson.String || strings.TrimSpace(uri.Str) == "" {
								return fmt.Errorf("Unable to submit request because %s.%s.fileUri is empty (%s.%s.fileUri 必须是非空字符串)", partField, name, partField, name)
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
	var missingSchemaItems []string
	if tools := root.Get("tools"); tools.Exists() && tools.Type != gjson.Null {
		if tools.IsArray() {
			for i, tool := range tools.Array() {
				if !tool.IsObject() {
					return fmt.Errorf("The GenerateContentRequest proto is invalid: tools[%d] must be an object (tools[%d] 必须是对象)", i, i)
				}
				for _, declKey := range []string{"functionDeclarations", "function_declarations"} {
					decls := tool.Get(declKey)
					if !decls.Exists() || decls.Type == gjson.Null {
						continue
					}
					if !decls.IsArray() || len(decls.Array()) == 0 {
						return fmt.Errorf("The GenerateContentRequest proto is invalid: tools[%d].%s cannot be empty (tools[%d].%s 必须是非空数组)", i, declKey, i, declKey)
					}
					for j, decl := range decls.Array() {
						if !decl.IsObject() || decl.Get("name").Type != gjson.String || strings.TrimSpace(decl.Get("name").Str) == "" {
							return fmt.Errorf("The GenerateContentRequest proto is invalid: * tools[%d].%s[%d].name: [REQUIRED_FIELD_MISSING] (tools[%d].%s[%d].name 必须是非空字符串)", i, declKey, j, i, declKey, j)
						}
						for _, paramKey := range []string{"parameters", "parametersJsonSchema", "parameters_json_schema"} {
							if params := decl.Get(paramKey); params.IsObject() {
								validateGeminiSchemaItems(params, fmt.Sprintf("GenerateContentRequest.tools[%d].function_declarations[%d].parameters", i, j), &missingSchemaItems)
							}
						}
					}
				}
			}
		} else if !tools.IsObject() {
			return errors.New("The GenerateContentRequest proto is invalid: tools must be an array or object (tools 必须是数组或对象)")
		}
	}
	// Gemini 2.x 与 3.x 模型（如 gemini-3.1-pro-preview、gemini-3.8-flash）的 maxOutputTokens 上限为 65536；gemini-4 系列支持到 1048576。
	maxOutputLimit := float64(1048576)
	if strings.HasPrefix(modelName, "gemini-2") || strings.HasPrefix(modelName, "gemini-3") {
		maxOutputLimit = 65536
	}
	for _, name := range []string{"generationConfig", "generation_config"} {
		config := root.Get(name)
		if !config.Exists() || config.Type == gjson.Null {
			continue
		}
		if !config.IsObject() {
			return fmt.Errorf("The GenerateContentRequest proto is invalid: %s must be an object (%s 必须是对象)", name, name)
		}
		for _, schemaKey := range []string{"responseSchema", "response_schema", "responseJsonSchema", "response_json_schema"} {
			if respSchema := config.Get(schemaKey); respSchema.IsObject() {
				validateGeminiSchemaItems(respSchema, "GenerateContentRequest.generation_config.response_schema", &missingSchemaItems)
			}
		}
		for _, key := range []string{"responseModalities", "response_modalities", "stopSequences", "stop_sequences"} {
			value := config.Get(key)
			if !value.Exists() || value.Type == gjson.Null {
				continue
			}
			if !value.IsArray() {
				return fmt.Errorf("The GenerateContentRequest proto is invalid: %s.%s must be an array of strings (%s.%s 必须是字符串数组)", name, key, name, key)
			}
			for _, item := range value.Array() {
				if item.Type != gjson.String {
					return fmt.Errorf("The GenerateContentRequest proto is invalid: %s.%s must be an array of strings (%s.%s 必须是字符串数组)", name, key, name, key)
				}
			}
		}
		for _, rule := range []struct {
			key      string
			min, max float64
			integer  bool
		}{
			{"temperature", 0, 2, false}, {"topP", 0, 1, false}, {"top_p", 0, 1, false},
			{"topK", 1, 500, true}, {"top_k", 1, 500, true},
			{"maxOutputTokens", 1, maxOutputLimit, true}, {"max_output_tokens", 1, maxOutputLimit, true},
			{"candidateCount", 1, maxTokensLimit, true}, {"candidate_count", 1, maxTokensLimit, true},
		} {
			value := config.Get(rule.key)
			if !value.Exists() || value.Type == gjson.Null {
				continue
			}
			if value.Type != gjson.Number || value.Num < rule.min || value.Num > rule.max || (rule.integer && math.Trunc(value.Num) != value.Num) {
				switch rule.key {
				case "maxOutputTokens", "max_output_tokens":
					return fmt.Errorf("Unable to submit request because it has a maxOutputTokens value of %s but the supported range is from 1 (inclusive) to %d (exclusive). Update the value and try again. (%s.%s 数值或类型无效)", value.Raw, int(rule.max)+1, name, rule.key)
				case "temperature":
					return fmt.Errorf("Unable to submit request because it has a temperature value of %s but the supported range is from 0 (inclusive) to 2.0001 (exclusive). Update the value and try again. (%s.%s 数值或类型无效)", value.Raw, name, rule.key)
				case "topP", "top_p":
					return fmt.Errorf("Unable to submit request because it has a topP value of %s but the supported range is from 0 (inclusive) to 1 (inclusive). Update the value and try again. (%s.%s 数值或类型无效)", value.Raw, name, rule.key)
				case "topK", "top_k":
					return fmt.Errorf("Unable to submit request because it has a topK value of %s but the supported range is from 1 (inclusive) to 65 (exclusive). Update the value and try again. (%s.%s 数值或类型无效)", value.Raw, name, rule.key)
				default:
					return fmt.Errorf("Unable to submit request because %s.%s=%s is invalid (%s.%s 数值或类型无效)", name, rule.key, value.Raw, name, rule.key)
				}
			}
		}
		for _, thinkKey := range []string{"thinkingConfig", "thinking_config"} {
			thinking := config.Get(thinkKey)
			if !thinking.Exists() || thinking.Type == gjson.Null {
				continue
			}
			if !thinking.IsObject() {
				return fmt.Errorf("The GenerateContentRequest proto is invalid: %s.%s must be an object (%s.%s 必须是对象)", name, thinkKey, name, thinkKey)
			}
			for _, budgetKey := range []string{"thinkingBudget", "thinking_budget"} {
				budget := thinking.Get(budgetKey)
				if !budget.Exists() || budget.Type == gjson.Null {
					continue
				}
				if budget.Type != gjson.Number || budget.Num < -1 || budget.Num > maxTokensLimit || math.Trunc(budget.Num) != budget.Num {
					return fmt.Errorf("Unable to submit request because %s.%s.%s=%s is invalid (%s.%s.%s 数值或类型无效)", name, thinkKey, budgetKey, budget.Raw, name, thinkKey, budgetKey)
				}
			}
		}
	}
	if len(missingSchemaItems) > 0 {
		return errors.New(strings.Join(missingSchemaItems, "\n"))
	}
	return nil
}
