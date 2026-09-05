package controller

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
)

// asyncMediaSource 仅在执行期间存在，下载完成后只保存本地文件清单。
type asyncMediaSource struct {
	Value       string
	ContentType string
	Base64      bool
}

// collectAsyncMediaSources 识别图片接口、Gemini、Responses 及聊天生图的结果结构。
func collectAsyncMediaSources(value any, sources *[]asyncMediaSource) {
	switch item := value.(type) {
	case []any:
		for _, child := range item {
			collectAsyncMediaSources(child, sources)
		}
	case string:
		if strings.HasPrefix(item, "https://") || strings.HasPrefix(item, "http://") || strings.HasPrefix(item, "data:") {
			*sources = append(*sources, asyncMediaSource{Value: item})
		}
	case map[string]any:
		hasInline := false
		if data, ok := item["b64_json"].(string); ok && data != "" {
			*sources = append(*sources, asyncMediaSource{Value: data, Base64: true})
			hasInline = true
		}
		if item["type"] == "image_generation_call" {
			if data, ok := item["result"].(string); ok && data != "" {
				*sources = append(*sources, asyncMediaSource{Value: data, Base64: true})
				hasInline = true
			}
		}
		for _, key := range []string{"inlineData", "inline_data"} {
			if inline, ok := item[key].(map[string]any); ok {
				contentType, _ := inline["mimeType"].(string)
				if contentType == "" {
					contentType, _ = inline["mime_type"].(string)
				}
				if data, ok := inline["data"].(string); ok && (strings.HasPrefix(contentType, "image/") || strings.HasPrefix(contentType, "video/")) {
					*sources = append(*sources, asyncMediaSource{Value: data, ContentType: contentType, Base64: true})
					hasInline = true
				}
			}
		}
		// 内嵌图片和下载地址通常是同一份结果；已有完整图片时不再访问备用地址。
		if !hasInline {
			for _, key := range []string{"url", "image_url", "imageUrl", "video_url", "videoUrl"} {
				if child, ok := item[key]; ok {
					collectAsyncMediaSources(child, sources)
				}
			}
		}
		// 仅遍历结果容器，不下载提示词、错误描述或用量字段里出现的地址。
		for _, key := range []string{"data", "candidates", "content", "parts", "output", "choices", "message", "delta", "images", "results", "videoUrls"} {
			if child, ok := item[key]; ok {
				collectAsyncMediaSources(child, sources)
			}
		}
	}
}

// readAsyncRelayMediaSources 同时解析普通响应和原样保存的事件流，不要求调用方关闭流式功能。
func readAsyncRelayMediaSources(reader io.Reader, contentType string) ([]asyncMediaSource, error) {
	var sources []asyncMediaSource
	if !strings.Contains(strings.ToLower(contentType), "text/event-stream") {
		var value any
		if err := common.DecodeJson(reader, &value); err != nil {
			return nil, fmt.Errorf("上游生成结果格式有误")
		}
		collectAsyncMediaSources(value, &sources)
		return sources, nil
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), int(common.AsyncMediaMaxFileBytes))
	var event strings.Builder
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if err := collectAsyncStreamMediaEvent(event.String(), &sources); err != nil {
				return nil, err
			}
			event.Reset()
			continue
		}
		if strings.HasPrefix(line, "data:") {
			if event.Len() > 0 {
				event.WriteByte('\n')
			}
			event.WriteString(strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("读取上游生成事件失败")
	}
	if err := collectAsyncStreamMediaEvent(event.String(), &sources); err != nil {
		return nil, err
	}
	return sources, nil
}

// collectAsyncStreamMediaEvent 只收集最终图片，Responses 的完整结果与完成事件随后统一去重。
func collectAsyncStreamMediaEvent(data string, sources *[]asyncMediaSource) error {
	data = strings.TrimSpace(data)
	if data == "" || data == "[DONE]" {
		return nil
	}
	var value any
	if common.Unmarshal([]byte(data), &value) != nil {
		return fmt.Errorf("上游流式生成结果格式有误")
	}
	if event, ok := value.(map[string]any); ok {
		eventType, _ := event["type"].(string)
		if strings.Contains(eventType, "partial_image") {
			return nil
		}
		switch eventType {
		case "response.output_item.done":
			value = event["item"]
		case "response.completed":
			value = event["response"]
		case "error", "response.failed":
			detail := "上游流式生成失败"
			failure, _ := event["error"].(map[string]any)
			if response, ok := event["response"].(map[string]any); ok {
				failure, _ = response["error"].(map[string]any)
			}
			if message, ok := failure["message"].(string); ok && message != "" {
				detail += "：" + message
			}
			return fmt.Errorf("%s", detail)
		}
	}
	collectAsyncMediaSources(value, sources)
	return nil
}

// inspectAsyncMedia 使用文件头识别可展示的媒体，不把上游返回的网页或脚本当作图片。
func inspectAsyncMedia(path string) (string, string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", "", err
	}
	defer file.Close()
	prefix := make([]byte, 512)
	n, err := file.Read(prefix)
	if err != nil && err != io.EOF {
		return "", "", err
	}
	if n == 0 {
		return "", "", fmt.Errorf("生成媒体文件为空")
	}
	contentType := http.DetectContentType(prefix[:n])
	switch contentType {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return contentType, "image", nil
	case "video/mp4", "video/webm", "video/avi", "video/mpeg":
		return contentType, "video", nil
	}
	if n >= 12 && string(prefix[4:8]) == "ftyp" {
		if string(prefix[8:12]) == "avif" || string(prefix[8:12]) == "avis" {
			return "image/avif", "image", nil
		}
		return "video/mp4", "video", nil
	}
	return "", "", fmt.Errorf("上游返回的文件不是可预览的图片或视频")
}

func saveAsyncMediaSource(ctx context.Context, source asyncMediaSource) (model.AsyncRelayMedia, error) {
	var reader io.Reader
	var closer io.Closer
	if strings.HasPrefix(source.Value, "data:") {
		parts := strings.SplitN(source.Value, ",", 2)
		if len(parts) != 2 || !strings.HasSuffix(parts[0], ";base64") {
			return model.AsyncRelayMedia{}, fmt.Errorf("媒体内嵌数据格式有误")
		}
		source.Value, source.Base64 = parts[1], true
	}
	if source.Base64 {
		encoding := base64.StdEncoding
		if len(source.Value)%4 != 0 {
			encoding = base64.RawStdEncoding
		}
		reader = base64.NewDecoder(encoding, strings.NewReader(source.Value))
	} else {
		if err := service.ValidateSSRFProtectedFetchURL(source.Value); err != nil {
			return model.AsyncRelayMedia{}, fmt.Errorf("生成文件地址校验失败")
		}
		downloadCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
		request, err := http.NewRequestWithContext(downloadCtx, http.MethodGet, source.Value, nil)
		if err != nil {
			return model.AsyncRelayMedia{}, err
		}
		client := service.GetSSRFProtectedHTTPClient()
		if client == nil {
			return model.AsyncRelayMedia{}, fmt.Errorf("媒体下载服务尚未初始化")
		}
		response, err := client.Do(request)
		if err != nil {
			return model.AsyncRelayMedia{}, fmt.Errorf("下载生成文件失败")
		}
		closer, reader = response.Body, response.Body
		defer closer.Close()
		if response.StatusCode != http.StatusOK {
			return model.AsyncRelayMedia{}, fmt.Errorf("下载生成文件失败（HTTP %d）", response.StatusCode)
		}
	}
	path, file, err := common.CreateAsyncMediaFile()
	if err != nil {
		return model.AsyncRelayMedia{}, err
	}
	keep := false
	defer func() {
		_ = file.Close()
		if !keep {
			_ = common.RemoveAsyncMediaFile(path)
		}
	}()
	size, err := io.Copy(file, io.LimitReader(reader, common.AsyncMediaMaxFileBytes+1))
	if err != nil {
		return model.AsyncRelayMedia{}, err
	}
	if size > common.AsyncMediaMaxFileBytes {
		return model.AsyncRelayMedia{}, fmt.Errorf("生成文件超过保存上限")
	}
	if err := file.Sync(); err != nil {
		return model.AsyncRelayMedia{}, err
	}
	if err := file.Close(); err != nil {
		return model.AsyncRelayMedia{}, err
	}
	contentType, kind, err := inspectAsyncMedia(path)
	if err != nil {
		return model.AsyncRelayMedia{}, err
	}
	keep = true
	return model.AsyncRelayMedia{Path: path, ContentType: contentType, Kind: kind}, nil
}

// completeAsyncRelayResult 生成结果和预览文件全部就绪后才提交成功状态。
func completeAsyncRelayResult(ctx context.Context, task *model.AsyncRelayTask, path, contentType string) bool {
	// 先持久化完整上游响应，重启或下载失败后只重试保存，不重新发起生成。
	if task.ResultFilePath != path {
		result := model.DB.Model(&model.AsyncRelayTask{}).Where("id = ? AND status = ? AND worker_id = ?", task.ID, model.AsyncRelayTaskStatusProcessing, task.WorkerID).
			Updates(map[string]any{"result_file_path": path, "result_content_type": contentType, "updated_at": common.GetTimestamp()})
		if result.Error != nil || result.RowsAffected == 0 {
			failAsyncRelayTask(task, "保存完整生成结果失败")
			return false
		}
		task.ResultFilePath, task.ResultContentType = path, contentType
	}
	var media []model.AsyncRelayMedia
	keep := false
	defer func() {
		if !keep {
			for _, item := range media {
				if item.Path != path {
					_ = common.RemoveAsyncMediaFile(item.Path)
				}
			}
		}
	}()
	if strings.HasPrefix(contentType, "image/") || strings.HasPrefix(contentType, "video/") {
		detected, kind, err := inspectAsyncMedia(path)
		if err != nil {
			failAsyncRelayTask(task, err.Error())
			return true
		}
		media = append(media, model.AsyncRelayMedia{Path: path, ContentType: detected, Kind: kind})
		contentType = detected
	} else {
		file, err := os.Open(path)
		if err != nil {
			failAsyncRelayTask(task, "读取生成结果失败")
			return true
		}
		sources, err := readAsyncRelayMediaSources(file, contentType)
		_ = file.Close()
		if err != nil {
			failAsyncRelayTask(task, err.Error())
			return true
		}
		if len(sources) == 0 {
			failAsyncRelayTask(task, "上游未返回可保存的图片或视频")
			return true
		}
		if len(sources) > 128 {
			failAsyncRelayTask(task, "生成文件数量超过保存上限")
			return true
		}
		var totalBytes int64
		seen := make(map[string]bool)
		for _, source := range sources {
			if seen[source.Value] {
				continue
			}
			seen[source.Value] = true
			result, err := saveAsyncMediaSource(ctx, source)
			if err != nil {
				retryAsyncRelayMedia(task, "生成已完成，保存媒体失败："+err.Error())
				return true
			}
			media = append(media, result)
			info, err := os.Stat(result.Path)
			if err != nil {
				failAsyncRelayTask(task, "读取生成文件大小失败")
				return true
			}
			totalBytes += info.Size()
			if totalBytes > common.AsyncMediaMaxFileBytes {
				failAsyncRelayTask(task, "生成文件总大小超过保存上限")
				return true
			}
		}
		if !strings.Contains(strings.ToLower(contentType), "text/event-stream") {
			contentType = "application/json"
		}
	}
	encoded, err := common.Marshal(media)
	if err != nil {
		failAsyncRelayTask(task, "保存媒体清单失败")
		return true
	}
	task.ResultFiles, task.ResultFilePath, task.ResultContentType = string(encoded), path, contentType
	task.Status, task.Error, task.NextAttemptAt = model.AsyncRelayTaskStatusSucceeded, "", 0
	if task.ResponseStatusCode == 0 {
		task.ResponseStatusCode = http.StatusOK
	}
	won, err := task.UpdateWithStatus(model.AsyncRelayTaskStatusProcessing)
	if err != nil {
		common.SysError("保存媒体任务结果失败: " + err.Error())
		return true
	}
	keep = won
	return true
}

// retryAsyncRelayMedia 使用有限次数的延后重试，只处理已经生成的媒体文件。
func retryAsyncRelayMedia(task *model.AsyncRelayTask, message string) {
	task.MediaAttempts++
	if task.MediaAttempts > 3 {
		failAsyncRelayTask(task, message)
		return
	}
	task.Status = model.AsyncRelayTaskStatusWaiting
	task.Error = message + "，稍后自动重试保存"
	task.NextAttemptAt = common.GetTimestamp() + int64(task.MediaAttempts*10)
	if _, err := task.UpdateWithStatus(model.AsyncRelayTaskStatusProcessing); err != nil {
		common.SysError("保存媒体重试状态失败: " + err.Error())
	}
}
