package controller

import "strings"

// 仅提取结构化的失败类别，不把上游回复文本、签名链接或密钥写入公开任务日志。
func asyncMediaResponseDiagnostic(value any) string {
	switch item := value.(type) {
	case []any:
		var reason string
		for _, child := range item {
			detail := asyncMediaResponseDiagnostic(child)
			if detail != "" {
				reason = detail
			}
		}
		return reason
	case map[string]any:
		for _, key := range []string{"promptFeedback", "prompt_feedback"} {
			if feedback, ok := item[key].(map[string]any); ok {
				blockReason, _ := feedback["blockReason"].(string)
				if blockReason == "" {
					blockReason, _ = feedback["block_reason"].(string)
				}
				if blockReason != "" && blockReason != "BLOCK_REASON_UNSPECIFIED" {
					return "上游拦截了生成请求，未返回图片或视频"
				}
			}
		}
		reason, _ := item["finishReason"].(string)
		if reason == "" {
			reason, _ = item["finish_reason"].(string)
		}
		switch reason {
		case "MAX_TOKENS", "length":
			return "上游达到输出长度上限，未返回图片或视频"
		case "SAFETY", "IMAGE_SAFETY", "IMAGE_PROHIBITED_CONTENT", "PROHIBITED_CONTENT", "BLOCKLIST", "RECITATION", "content_filter":
			return "上游拦截了生成结果，未返回图片或视频"
		case "NO_IMAGE", "IMAGE_OTHER":
			return "上游未生成图片"
		}
		if item["error"] != nil {
			return "上游返回错误响应，未返回图片或视频"
		}
		var detail string
		if text, ok := item["text"].(string); ok && strings.TrimSpace(text) != "" {
			detail = "上游仅返回文字，未返回图片或视频"
		}
		for _, key := range []string{"candidates", "content", "parts", "output", "choices", "message", "delta", "response"} {
			if child, ok := item[key]; ok {
				if reason := asyncMediaResponseDiagnostic(child); reason != "" {
					detail = reason
				}
			}
		}
		return detail
	}
	return ""
}

// 地址策略仍然严格执行；对用户只显示可排查的类别，不暴露完整地址及鉴权参数。
func asyncMediaURLValidationReason(err error) string {
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "dns resolution failed"):
		return "生成文件地址校验失败：服务器 DNS 解析失败"
	case strings.Contains(message, "private ip address"):
		return "生成文件地址校验失败：目标为私有或保留 IP，已被抓取策略拦截"
	case strings.Contains(message, "domain not in whitelist"), strings.Contains(message, "domain in blacklist"):
		return "生成文件地址校验失败：域名不符合抓取策略"
	case strings.Contains(message, "ip not in whitelist"), strings.Contains(message, "ip in blacklist"):
		return "生成文件地址校验失败：目标 IP 不符合抓取策略"
	case strings.Contains(message, "unsupported protocol"), strings.Contains(message, "invalid url"), strings.Contains(message, "invalid host"):
		return "生成文件地址校验失败：地址格式或协议有误"
	case strings.Contains(message, "port"):
		return "生成文件地址校验失败：目标端口或端口策略有误"
	default:
		return "生成文件地址校验失败：请检查服务器媒体抓取策略"
	}
}
