package controller

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/url"
	"os"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
)

// 只采集协议明确的素材字段，普通提示词、回调地址和鉴权字段不会成为参考素材。
var asyncReferenceFields = []string{
	"image", "images", "image_url", "image_urls", "input_image", "input_reference", "reference_image", "reference_images", "first_frame", "last_frame", "image_end", "mask",
	"video", "videos", "video_url", "video_urls", "input_video", "reference_video", "reference_videos",
	"audio", "audios", "audio_url", "audio_urls", "input_audio", "reference_audio", "reference_audios",
}

// captureAsyncRequestDetails 只处理已收到的输入，不访问上游；详情采集失败不会阻断原来的生成请求。
func captureAsyncRequestDetails(task *model.AsyncRelayTask) model.AsyncRelayRequestDetails {
	details := model.AsyncRelayRequestDetails{PromptSource: "request", Parameters: make(map[string]string)}
	file, err := os.Open(task.RequestFilePath)
	if err != nil {
		details.CaptureError = "输入内容未保存"
		return details
	}
	defer file.Close()
	mediaType, params, err := mime.ParseMediaType(task.RequestContentType)
	if err != nil {
		details.CaptureError = "输入格式未识别"
		return details
	}
	if strings.HasPrefix(mediaType, "multipart/") {
		form := multipart.NewReader(file, params["boundary"])
		for {
			part, err := form.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				details.CaptureError = "部分输入内容读取失败"
				break
			}
			name := strings.TrimSuffix(part.FormName(), "[]")
			if part.FileName() != "" && isAsyncReferenceField(name) {
				if len(details.References) >= 128 {
					_ = part.Close()
					details.CaptureError = "参考素材数量超出展示上限"
					continue
				}
				reference := saveAsyncReferenceFile(part, part.FileName(), name)
				details.References = append(details.References, reference)
			} else if part.FileName() == "" && (name == "prompt" || isAsyncSafeParameterName(name) || isAsyncReferenceField(name)) {
				limit := int64(1024 * 1024)
				if isAsyncReferenceField(name) {
					limit = common.AsyncMediaMaxFileBytes
				}
				value, err := io.ReadAll(io.LimitReader(part, limit+1))
				if err == nil && int64(len(value)) <= limit {
					if isAsyncReferenceField(name) {
						var content any
						if common.Unmarshal(value, &content) != nil {
							content = string(value)
						}
						appendAsyncReferences(content, name, &details.References, task.UserID)
					} else if name == "prompt" {
						details.Prompt = string(value)
					} else {
						parameter := string(value)
						if cleaned, ok := sanitizeAsyncParameter(parameter, 0); ok {
							details.Parameters[name] = cleaned.(string)
						}
					}
				} else {
					details.CaptureError = "部分输入文字超过展示上限"
				}
			}
			_ = part.Close()
		}
		saveAsyncInlineReferences(details.References)
		return details
	}
	var fields map[string]json.RawMessage
	if common.DecodeJson(file, &fields) != nil {
		details.CaptureError = "输入内容不是可展示的表单或 JSON"
		return details
	}
	_ = common.Unmarshal(fields["prompt"], &details.Prompt)
	for key, value := range fields {
		if !isAsyncSafeParameterName(key) || len(value) > 16*1024 {
			continue
		}
		parameter, ok := decodeAsyncParameter(value, 0)
		if !ok {
			continue
		}
		if cleaned, ok := sanitizeAsyncParameter(parameter, 0); ok {
			encoded, err := common.Marshal(cleaned)
			if err == nil && len(encoded) <= 16*1024 {
				if text, ok := cleaned.(string); ok {
					details.Parameters[key] = text
				} else {
					details.Parameters[key] = string(encoded)
				}
			}
		}
	}
	var texts []string
	for _, key := range []string{"messages", "input", "contents", "content"} {
		var content any
		if common.Unmarshal(fields[key], &content) == nil {
			collectAsyncRequestContent(content, &texts, &details.References, task.UserID)
		}
	}
	if details.Prompt == "" {
		details.Prompt = strings.Join(texts, "\n\n")
	}
	for _, key := range asyncReferenceFields {
		var value any
		if common.Unmarshal(fields[key], &value) == nil {
			appendAsyncReferences(value, key, &details.References, task.UserID)
		}
	}
	// Gemini 的图片尺寸和比例同时映射为通用参数。
	var generation struct {
		ResponseModalities []string `json:"responseModalities"`
		ImageConfig        struct {
			AspectRatio string `json:"aspectRatio"`
			ImageSize   string `json:"imageSize"`
		} `json:"imageConfig"`
	}
	if common.Unmarshal(fields["generationConfig"], &generation) == nil {
		if generation.ImageConfig.AspectRatio != "" {
			details.Parameters["aspect_ratio"] = generation.ImageConfig.AspectRatio
		}
		if generation.ImageConfig.ImageSize != "" {
			details.Parameters["resolution"] = generation.ImageConfig.ImageSize
		}
	}
	saveAsyncInlineReferences(details.References)
	return details
}

// 远程地址延迟到查看时读取，内联素材立即保留快照，不影响原请求的内容和发送方式。
func saveAsyncInlineReferences(references []model.AsyncRelayReference) {
	for index := range references {
		reference := &references[index]
		if reference.Source == "" {
			continue
		}
		if strings.HasPrefix(reference.Source, "https://") || strings.HasPrefix(reference.Source, "http://") {
			parsed, err := url.Parse(reference.Source)
			if err != nil || parsed.User != nil {
				reference.Source = ""
				reference.Error = "参考素材地址格式有误"
			}
			continue
		}
		if strings.HasPrefix(reference.Source, "file-") {
			reference.Source = ""
			reference.Error = "参考素材只有上游文件编号，未包含可保存的内容"
			continue
		}
		media, err := saveAsyncMediaSource(context.Background(), asyncMediaSource{Value: reference.Source, Base64: !strings.HasPrefix(reference.Source, "data:"), AllowAudio: true})
		reference.Source = ""
		if err != nil {
			reference.Error = "参考素材内容保存失败"
			continue
		}
		reference.Path, reference.Kind, reference.ContentType = media.Path, media.Kind, media.ContentType
	}
}

// isAsyncSafeParameterName 过滤输入正文、媒体和凭据字段，只记录可审阅的生成参数。
func isAsyncSafeParameterName(name string) bool {
	if name == "" || isAsyncReferenceField(name) {
		return false
	}
	blocked := []string{"prompt", "content", "message", "input", "instruction", "file", "url", "uri", "token", "secret", "auth", "credential", "password", "key", "callback", "webhook", "base64", "inline"}
	lower := strings.ToLower(name)
	for _, part := range blocked {
		if strings.Contains(lower, part) {
			return false
		}
	}
	switch lower {
	case "messages", "contents", "parts", "data", "metadata", "headers", "tools":
		return false
	}
	return true
}

// decodeAsyncParameter 保留 JSON 数字精度，并在递归解析时过滤正文及敏感字段。
func decodeAsyncParameter(raw json.RawMessage, depth int) (any, bool) {
	if depth > 8 {
		return nil, false
	}
	switch common.GetJsonType(raw) {
	case "object":
		var fields map[string]json.RawMessage
		if common.Unmarshal(raw, &fields) != nil {
			return nil, false
		}
		result := make(map[string]any, len(fields))
		for key, value := range fields {
			if !isAsyncSafeParameterName(key) {
				continue
			}
			if cleaned, ok := decodeAsyncParameter(value, depth+1); ok {
				result[key] = cleaned
			}
		}
		return result, len(result) > 0
	case "array":
		var values []json.RawMessage
		if common.Unmarshal(raw, &values) != nil {
			return nil, false
		}
		result := make([]any, 0, min(len(values), 128))
		for _, value := range values {
			if cleaned, ok := decodeAsyncParameter(value, depth+1); ok {
				result = append(result, cleaned)
			}
			if len(result) >= 128 {
				break
			}
		}
		return result, len(result) > 0
	case "string":
		var text string
		if common.Unmarshal(raw, &text) != nil {
			return nil, false
		}
		return sanitizeAsyncParameter(text, depth)
	case "number":
		return json.Number(raw), true
	case "boolean":
		var value bool
		if common.Unmarshal(raw, &value) != nil {
			return nil, false
		}
		return value, true
	default:
		return nil, false
	}
}

// sanitizeAsyncParameter 限制参数快照的深度和体积，并递归剔除正文及敏感字段。
func sanitizeAsyncParameter(value any, depth int) (any, bool) {
	if depth > 8 {
		return nil, false
	}
	switch item := value.(type) {
	case string:
		if len(item) > 4096 || strings.HasPrefix(item, "data:") || strings.HasPrefix(item, "http://") || strings.HasPrefix(item, "https://") {
			return nil, false
		}
		return item, true
	default:
		return value, true
	}
}

func isAsyncReferenceField(name string) bool {
	for _, field := range asyncReferenceFields {
		if name == field {
			return true
		}
	}
	return false
}

func asyncReferenceKind(field string) string {
	if strings.Contains(field, "video") {
		return "video"
	}
	if strings.Contains(field, "audio") {
		return "audio"
	}
	return "image"
}

// saveAsyncReferenceFile 为上传文件保存独立快照，日志清理不会删除其他任务共用的素材。
func saveAsyncReferenceFile(reader io.Reader, name, field string) model.AsyncRelayReference {
	reference := model.AsyncRelayReference{Name: name, Role: "reference", Kind: asyncReferenceKind(field)}
	if field == "mask" {
		reference.Role = "mask"
	}
	path, file, err := common.CreateAsyncMediaFile()
	if err != nil {
		reference.Error = "参考素材保存失败"
		return reference
	}
	keep := false
	defer func() {
		_ = file.Close()
		if !keep {
			_ = common.RemoveAsyncMediaFile(path)
		}
	}()
	size, err := io.Copy(file, io.LimitReader(reader, common.AsyncMediaMaxFileBytes+1))
	if err != nil || size > common.AsyncMediaMaxFileBytes {
		reference.Error = "参考素材内容读取失败"
		return reference
	}
	if err = file.Sync(); err != nil {
		reference.Error = "参考素材保存失败"
		return reference
	}
	_ = file.Close()
	contentType, kind, err := inspectAsyncMediaWithAudio(path, true)
	if err != nil {
		reference.Error = "参考文件不是可预览的图片、视频或音频"
		return reference
	}
	reference.Path, reference.ContentType, reference.Kind = path, contentType, kind
	keep = true
	return reference
}

// appendAsyncReferences 根据输入字段保留媒体类型，上传素材沿用原有归属和有效期校验。
func appendAsyncReferences(value any, field string, references *[]model.AsyncRelayReference, userID int) {
	if len(*references) >= 128 {
		return
	}
	role, kind := "reference", asyncReferenceKind(field)
	if field == "mask" {
		role = "mask"
	}
	switch item := value.(type) {
	case string:
		if item != "" {
			*references = append(*references, model.AsyncRelayReference{Source: item, Role: role, Kind: kind})
		}
	case []any:
		for _, child := range item {
			appendAsyncReferences(child, field, references, userID)
		}
	case map[string]any:
		if id, ok := item["asset_id"].(string); ok && id != "" {
			reference := model.AsyncRelayReference{Role: role, Kind: kind, Error: "参考素材不存在或已过期"}
			if service.ValidateVideoAssets(map[string]any{kind + "_urls": []any{item}}, userID) == nil {
				var asset model.VideoAsset
				if model.DB.Where("id = ? AND user_id = ?", id, userID).First(&asset).Error == nil {
					if file, err := os.Open(asset.Path); err == nil {
						reference = saveAsyncReferenceFile(file, "", field)
						_ = file.Close()
					}
				}
			}
			*references = append(*references, reference)
			return
		}
		if data, ok := item["b64_json"].(string); ok && data != "" {
			appendAsyncReferences("data:application/octet-stream;base64,"+data, field, references, userID)
			return
		}
		if data, ok := item["data"].(string); ok && data != "" && field == "input_audio" {
			appendAsyncReferences(data, field, references, userID)
			return
		}
		for _, key := range []string{"mimeType", "mime_type"} {
			if contentType, ok := item[key].(string); ok {
				field = asyncReferenceKind(contentType)
			}
		}
		for _, key := range []string{"url", "image_url", "video_url", "audio_url", "file_uri", "fileUri"} {
			if child, ok := item[key]; ok {
				appendAsyncReferences(child, field, references, userID)
				return
			}
		}
	}
}

// collectAsyncRequestContent 覆盖聊天、Responses 和 Gemini 输入中的提示词及参考媒体。
func collectAsyncRequestContent(value any, texts *[]string, references *[]model.AsyncRelayReference, userID int) {
	switch item := value.(type) {
	case string:
		if item != "" {
			*texts = append(*texts, item)
		}
	case []any:
		for _, child := range item {
			collectAsyncRequestContent(child, texts, references, userID)
		}
	case map[string]any:
		if role, _ := item["role"].(string); role == "assistant" || role == "model" {
			return
		}
		if text, ok := item["text"].(string); ok && text != "" {
			*texts = append(*texts, text)
		}
		for _, key := range []string{"image_url", "input_image", "video_url", "input_video", "audio_url", "input_audio"} {
			if image, ok := item[key]; ok {
				appendAsyncReferences(image, key, references, userID)
			}
		}
		for _, key := range []string{"inlineData", "inline_data"} {
			if inline, ok := item[key].(map[string]any); ok {
				contentType, _ := inline["mimeType"].(string)
				if contentType == "" {
					contentType, _ = inline["mime_type"].(string)
				}
				if data, ok := inline["data"].(string); ok && (strings.HasPrefix(contentType, "image/") || strings.HasPrefix(contentType, "video/") || strings.HasPrefix(contentType, "audio/")) {
					appendAsyncReferences("data:"+contentType+";base64,"+data, asyncReferenceKind(contentType), references, userID)
				}
			}
		}
		for _, key := range []string{"fileData", "file_data"} {
			if file, ok := item[key]; ok {
				appendAsyncReferences(file, "reference", references, userID)
			}
		}
		for _, key := range []string{"content", "parts"} {
			if child, ok := item[key]; ok {
				collectAsyncRequestContent(child, texts, references, userID)
			}
		}
	}
}
