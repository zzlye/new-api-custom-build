package controller

import (
	"net/url"
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
