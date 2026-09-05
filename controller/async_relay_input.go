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
)

var asyncRequestParameterNames = []string{"model", "size", "quality", "n", "output_format", "response_format", "background", "moderation", "seconds", "duration", "aspect_ratio", "resolution", "seed", "negative_prompt", "stream"}

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
					details.CaptureError = "参考图数量超出展示上限"
					continue
				}
				reference := saveAsyncMultipartReference(part, name)
				details.References = append(details.References, reference)
			} else if part.FileName() == "" && (name == "prompt" || isAsyncRequestParameter(name)) {
				value, err := io.ReadAll(io.LimitReader(part, 1024*1024+1))
				if err == nil && len(value) <= 1024*1024 {
					if name == "prompt" {
						details.Prompt = string(value)
					} else {
						details.Parameters[name] = string(value)
					}
				} else {
					details.CaptureError = "部分输入文字超过展示上限"
				}
			}
			_ = part.Close()
		}
		return details
	}
	var fields map[string]json.RawMessage
	if common.DecodeJson(file, &fields) != nil {
		details.CaptureError = "输入内容不是可展示的表单或 JSON"
		return details
	}
	_ = common.Unmarshal(fields["prompt"], &details.Prompt)
	for _, key := range asyncRequestParameterNames {
		value, exists := fields[key]
		if !exists {
			continue
		}
		kind := common.GetJsonType(value)
		if kind == "string" || kind == "number" || kind == "boolean" {
			details.Parameters[key] = common.JsonRawMessageToString(value)
		}
	}
	var texts []string
	for _, key := range []string{"messages", "input", "contents"} {
		var content any
		if common.Unmarshal(fields[key], &content) == nil {
			collectAsyncRequestContent(content, &texts, &details.References)
		}
	}
	if details.Prompt == "" {
		details.Prompt = strings.Join(texts, "\n\n")
	}
	for _, key := range []string{"image", "images", "image_url", "input_image", "input_reference", "mask"} {
		var value any
		if common.Unmarshal(fields[key], &value) == nil {
			appendAsyncReferences(value, key, &details.References)
		}
	}
	// Gemini 的图片尺寸和比例来自生成配置，仅提取这些展示参数。
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
	for index := range details.References {
		reference := &details.References[index]
		if strings.HasPrefix(reference.Source, "https://") || strings.HasPrefix(reference.Source, "http://") {
			parsed, err := url.Parse(reference.Source)
			if err != nil || parsed.User != nil {
				reference.Source = ""
				reference.Error = "参考图地址格式有误"
			}
			continue
		}
		if strings.HasPrefix(reference.Source, "file-") {
			reference.Source = ""
			reference.Error = "参考图只有上游文件编号，未包含可保存的图片"
			continue
		}
		media, err := saveAsyncMediaSource(context.Background(), asyncMediaSource{Value: reference.Source, Base64: !strings.HasPrefix(reference.Source, "data:")})
		reference.Source = ""
		if err != nil {
			reference.Error = "参考图内容保存失败"
			continue
		}
		reference.Path, reference.Kind, reference.ContentType = media.Path, media.Kind, media.ContentType
	}
	return details
}

func isAsyncRequestParameter(name string) bool {
	for _, key := range asyncRequestParameterNames {
		if name == key {
			return true
		}
	}
	return false
}

func isAsyncReferenceField(name string) bool {
	switch name {
	case "image", "images", "mask", "input_image", "input_reference", "reference_image":
		return true
	}
	return false
}

// saveAsyncMultipartReference 复制上传的参考文件，不依赖请求结束后会被清理的表单缓存。
func saveAsyncMultipartReference(part *multipart.Part, field string) model.AsyncRelayReference {
	reference := model.AsyncRelayReference{Name: part.FileName(), Role: "reference", Kind: "image"}
	if field == "mask" {
		reference.Role = "mask"
	}
	path, file, err := common.CreateAsyncMediaFile()
	if err != nil {
		reference.Error = "参考图保存失败"
		return reference
	}
	keep := false
	defer func() {
		_ = file.Close()
		if !keep {
			_ = common.RemoveAsyncMediaFile(path)
		}
	}()
	size, err := io.Copy(file, io.LimitReader(part, common.AsyncMediaMaxFileBytes+1))
	if err != nil || size > common.AsyncMediaMaxFileBytes {
		reference.Error = "参考图内容读取失败"
		return reference
	}
	if err = file.Sync(); err != nil {
		reference.Error = "参考图保存失败"
		return reference
	}
	_ = file.Close()
	contentType, kind, err := inspectAsyncMedia(path)
	if err != nil {
		reference.Error = "参考文件不是可预览的图片或视频"
		return reference
	}
	reference.Path, reference.ContentType, reference.Kind = path, contentType, kind
	keep = true
	return reference
}

// appendAsyncReferences 只接收明确的图片字段，不把提示词中的链接当成参考图。
func appendAsyncReferences(value any, role string, references *[]model.AsyncRelayReference) {
	if len(*references) >= 128 {
		return
	}
	if role != "mask" {
		role = "reference"
	}
	switch item := value.(type) {
	case string:
		if item != "" {
			*references = append(*references, model.AsyncRelayReference{Source: item, Role: role, Kind: "image"})
		}
	case []any:
		for _, child := range item {
			appendAsyncReferences(child, role, references)
		}
	case map[string]any:
		if data, ok := item["b64_json"].(string); ok && data != "" {
			appendAsyncReferences("data:application/octet-stream;base64,"+data, role, references)
			return
		}
		for _, key := range []string{"url", "image_url", "file_uri", "fileUri"} {
			if child, ok := item[key]; ok {
				appendAsyncReferences(child, role, references)
				return
			}
		}
	}
}

// collectAsyncRequestContent 覆盖聊天、Responses 和 Gemini 输入中的提示词及参考媒体。
func collectAsyncRequestContent(value any, texts *[]string, references *[]model.AsyncRelayReference) {
	switch item := value.(type) {
	case string:
		if item != "" {
			*texts = append(*texts, item)
		}
	case []any:
		for _, child := range item {
			collectAsyncRequestContent(child, texts, references)
		}
	case map[string]any:
		if role, _ := item["role"].(string); role == "assistant" || role == "model" {
			return
		}
		if text, ok := item["text"].(string); ok && text != "" {
			*texts = append(*texts, text)
		}
		for _, key := range []string{"image_url", "input_image"} {
			if image, ok := item[key]; ok {
				appendAsyncReferences(image, "reference", references)
			}
		}
		for _, key := range []string{"inlineData", "inline_data"} {
			if inline, ok := item[key].(map[string]any); ok {
				contentType, _ := inline["mimeType"].(string)
				if contentType == "" {
					contentType, _ = inline["mime_type"].(string)
				}
				if data, ok := inline["data"].(string); ok && strings.HasPrefix(contentType, "image/") {
					appendAsyncReferences("data:"+contentType+";base64,"+data, "reference", references)
				}
			}
		}
		for _, key := range []string{"fileData", "file_data"} {
			if file, ok := item[key]; ok {
				appendAsyncReferences(file, "reference", references)
			}
		}
		for _, key := range []string{"content", "parts"} {
			if child, ok := item[key]; ok {
				collectAsyncRequestContent(child, texts, references)
			}
		}
	}
}
