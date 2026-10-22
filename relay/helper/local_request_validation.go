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
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// ValidateLocalRequest 只做本地结构和已知参数校验，不请求素材地址或探测上游。
// 未知扩展字段继续透传，不能用过期的模型白名单拦截合法请求。
func ValidateLocalRequest(c *gin.Context) error {
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
		request, err := GetAndValidOpenAIImageRequest(c, relayconstant.Path2RelayMode(path))
		if err != nil {
			return err
		}
		if strings.TrimSpace(request.Model) == "" || strings.TrimSpace(request.Prompt) == "" {
			return errors.New("model 和 prompt 必须是非空字符串")
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
	if format == types.RelayFormatGemini {
		return validateLocalGemini(root)
	}
	// 复用原有必填字段和数量边界，额外检查不会改写提交给上游的参数。
	if _, err := GetAndValidateRequest(c, format); err != nil {
		return fmt.Errorf("请求参数无效：%w", err)
	}
	if format == types.RelayFormatOpenAI || format == types.RelayFormatClaude {
		if messages := root.Get("messages"); messages.IsArray() {
			for i, message := range messages.Array() {
				field := fmt.Sprintf("messages[%d]", i)
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
		}
	}
	if format == types.RelayFormatOpenAIImage && (root.Get("prompt").Type != gjson.String || strings.TrimSpace(root.Get("prompt").Str) == "") {
		return errors.New("prompt 必须是非空字符串")
	}
	return nil
}

func validateLocalGemini(root gjson.Result) error {
	contents := root.Get("contents")
	if !contents.IsArray() || len(contents.Array()) == 0 {
		return errors.New("contents 必须是非空数组")
	}
	for i, content := range contents.Array() {
		field := fmt.Sprintf("contents[%d]", i)
		if !content.IsObject() {
			return fmt.Errorf("%s 必须是对象", field)
		}
		if role := content.Get("role"); role.Exists() && role.Type != gjson.Null && (role.Type != gjson.String || (role.Str != "" && role.Str != "user" && role.Str != "model")) {
			return fmt.Errorf("%s.role 必须是 user 或 model", field)
		}
		parts := content.Get("parts")
		if !parts.IsArray() || len(parts.Array()) == 0 {
			return fmt.Errorf("%s.parts 必须是非空数组", field)
		}
		for j, part := range parts.Array() {
			field := fmt.Sprintf("%s.parts[%d]", field, j)
			if !part.IsObject() || len(part.Map()) == 0 {
				return fmt.Errorf("%s 必须是非空对象", field)
			}
			if text := part.Get("text"); text.Exists() && text.Type != gjson.Null && text.Type != gjson.String {
				return fmt.Errorf("%s.text 必须是字符串", field)
			}
			for _, name := range []string{"inlineData", "inline_data", "fileData", "file_data"} {
				data := part.Get(name)
				if !data.Exists() || data.Type == gjson.Null {
					continue
				}
				if !data.IsObject() {
					return fmt.Errorf("%s.%s 必须是对象", field, name)
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
						return fmt.Errorf("%s.%s.mimeType 必须是有效的媒体类型", field, name)
					}
				}
				if inline {
					encoded := data.Get("data")
					if encoded.Type != gjson.String || encoded.Str == "" {
						return fmt.Errorf("%s.%s.data 必须是非空 Base64 字符串", field, name)
					}
					// 流式校验避免再分配整份图片或视频内容；不把素材写入错误日志。
					// Protobuf JSON 也接受 URL 安全字母表及省略填充的 Base64。
					encoding := base64.StdEncoding
					if strings.ContainsAny(encoded.Str, "-_") {
						encoding = base64.URLEncoding
					}
					if !strings.Contains(encoded.Str, "=") {
						encoding = encoding.WithPadding(base64.NoPadding)
					}
					if n, err := io.Copy(io.Discard, base64.NewDecoder(encoding, strings.NewReader(encoded.Str))); err != nil || n == 0 {
						return fmt.Errorf("%s.%s.data 不是有效的 Base64", field, name)
					}
				} else {
					uri := data.Get("fileUri")
					if !uri.Exists() {
						uri = data.Get("file_uri")
					}
					if uri.Type != gjson.String || strings.TrimSpace(uri.Str) == "" {
						return fmt.Errorf("%s.%s.fileUri 必须是非空字符串", field, name)
					}
				}
			}
		}
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
	}
	return nil
}
