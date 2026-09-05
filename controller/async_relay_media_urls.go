package controller

import (
	"net/url"
	"os"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// asyncMediaSourceURL 只记录完整的普通网络地址，排除内嵌内容、临时地址及含登录凭据的链接。
func asyncMediaSourceURL(value string) string {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Hostname() == "" || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return ""
	}
	return value
}

// uniqueAsyncMediaSources 合并事件流的重复结果，并保留后续完成事件补充的来源地址。
func uniqueAsyncMediaSources(sources []asyncMediaSource) []asyncMediaSource {
	unique := make([]asyncMediaSource, 0, len(sources))
	seen := make(map[string]int, len(sources))
	for _, source := range sources {
		if index, ok := seen[source.Value]; ok {
			if unique[index].SourceURL == "" {
				unique[index].SourceURL = source.SourceURL
			}
			continue
		}
		seen[source.Value] = len(unique)
		unique = append(unique, source)
	}
	return unique
}

// restoreAsyncRelayMediaURLs 从仍在保存期内的原始响应补齐历史来源，严格按归档时的去重顺序匹配。
func restoreAsyncRelayMediaURLs(task *model.AsyncRelayTask) {
	if model.AsyncRelayTaskExpired(task, common.GetTimestamp()) || task.ResultFiles == "" || task.ResultFilePath == "" {
		return
	}
	var files []model.AsyncRelayMedia
	if common.UnmarshalJsonStr(task.ResultFiles, &files) != nil || len(files) == 0 {
		return
	}
	checked := true
	for _, file := range files {
		checked = checked && file.SourceChecked
	}
	if checked || (!strings.Contains(task.ResultContentType, "json") && !strings.Contains(task.ResultContentType, "text/event-stream")) {
		return
	}
	response, err := os.Open(task.ResultFilePath)
	if err != nil {
		return
	}
	sources, err := readAsyncRelayMediaSources(response, task.ResultContentType)
	_ = response.Close()
	if err != nil {
		return
	}
	sources = uniqueAsyncMediaSources(sources)
	// 数量不一致意味着历史归档规则不同，跳过补齐，避免给图片配错地址。
	if len(sources) != len(files) || model.AsyncRelayTaskExpired(task, common.GetTimestamp()) {
		return
	}
	for index := range files {
		if !files[index].SourceChecked {
			files[index].SourceURL = asyncMediaSourceURL(sources[index].SourceURL)
			files[index].SourceChecked = true
		}
	}
	encoded, err := common.Marshal(files)
	if err != nil {
		return
	}
	// 条件更新避免覆盖同时发生的清理和归档，只补清单，不触碰计费、状态或时间。
	updated := model.DB.Model(&model.AsyncRelayTask{}).
		Where("id = ? AND result_files = ? AND (result_expired_at = 0 OR result_expired_at IS NULL)", task.ID, task.ResultFiles).
		UpdateColumn("result_files", string(encoded))
	if updated.Error == nil && updated.RowsAffected == 1 {
		task.ResultFiles = string(encoded)
	}
}
