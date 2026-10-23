package operation_setting

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// LocalRequestValidationOption 保存按分组生效的本地请求校验与错误拦截策略配置键名。
const LocalRequestValidationOption = "LocalRequestValidationPolicy"

// DefaultLocalRequestValidationJSON 定义默认关闭的本地参数校验与错误拦截配置。
const DefaultLocalRequestValidationJSON = `{"enabled":false,"rules":[]}`

// DefaultUpstreamInterceptStatusCodes 默认针对上游 400 参数错误与 524 传输超时进行冷却拦截。
const DefaultUpstreamInterceptStatusCodes = "400,524"

// DefaultUpstreamCooldownSeconds 默认对重复触发上游错误的相同请求拦截 60 秒。
const DefaultUpstreamCooldownSeconds = 60

// LocalRequestValidationRule 按实际使用的分组配置是否启用错误拦截及本地返回的 HTTP 状态码。
type LocalRequestValidationRule struct {
	Group      string `json:"group"`
	Enabled    bool   `json:"enabled"`
	StatusCode int    `json:"status_code"`
}

// LocalRequestValidationPolicy 定义按分组配置的本地参数预检规则与上游错误冷却拦截策略。
type LocalRequestValidationPolicy struct {
	Enabled                 bool                         `json:"enabled"`
	Rules                   []LocalRequestValidationRule `json:"rules"`
	CheckStructure          *bool                        `json:"check_structure,omitempty"`
	CheckMedia              *bool                        `json:"check_media,omitempty"`
	CheckGenerationConfig   *bool                        `json:"check_generation_config,omitempty"`
	BlockedKeywords         string                       `json:"blocked_keywords,omitempty"`
	UpstreamCooldownSeconds *int                         `json:"upstream_cooldown_seconds,omitempty"`
	UpstreamStatusCodes     string                       `json:"upstream_status_codes,omitempty"`
	UpstreamErrorKeywords   string                       `json:"upstream_error_keywords,omitempty"`
	upstreamRanges          []StatusCodeRange
}

// StructureCheckEnabled 判断是否启用消息与基础结构校验（未显式配置时默认开启）。
func (p LocalRequestValidationPolicy) StructureCheckEnabled() bool {
	return p.CheckStructure == nil || *p.CheckStructure
}

// MediaCheckEnabled 判断是否启用多模态媒体数据（Base64/MIME/FileURI）校验（未显式配置时默认开启）。
func (p LocalRequestValidationPolicy) MediaCheckEnabled() bool {
	return p.CheckMedia == nil || *p.CheckMedia
}

// GenerationConfigCheckEnabled 判断是否启用生成与思考参数范围校验（未显式配置时默认开启）。
func (p LocalRequestValidationPolicy) GenerationConfigCheckEnabled() bool {
	return p.CheckGenerationConfig == nil || *p.CheckGenerationConfig
}

// CooldownSeconds 返回相同错误请求的本地冷却拦截时长（秒），未显式配置时默认 60 秒。
func (p LocalRequestValidationPolicy) CooldownSeconds() int {
	if p.UpstreamCooldownSeconds == nil {
		return DefaultUpstreamCooldownSeconds
	}
	return *p.UpstreamCooldownSeconds
}

// GroupStatus 返回指定分组启用拦截时的本地 HTTP 状态码；未启用时返回 0。
func (p LocalRequestValidationPolicy) GroupStatus(group string) int {
	if !p.Enabled {
		return 0
	}
	for _, rule := range p.Rules {
		if rule.Enabled && rule.Group == group {
			return rule.StatusCode
		}
	}
	return 0
}

// MatchesUpstreamError 判断上游返回的错误是否命中当前策略的冷却拦截条件（如 400、524 或特定错误特征）。
func (p LocalRequestValidationPolicy) MatchesUpstreamError(statusCode int, errorType, errorCode, message string) bool {
	if !p.Enabled || p.CooldownSeconds() <= 0 || statusCode < 400 || statusCode > 599 {
		return false
	}
	if shouldMatchStatusCodeRanges(p.upstreamRanges, statusCode) {
		return true
	}
	if strings.TrimSpace(p.UpstreamErrorKeywords) == "" {
		return false
	}
	summary := strings.ToLower(fmt.Sprintf("status_code=%d %s %s %s", statusCode, errorType, errorCode, message))
	for line := range strings.SplitSeq(p.UpstreamErrorKeywords, "\n") {
		line = strings.TrimSpace(strings.ToLower(line))
		if line == "" {
			continue
		}
		matched := true
		hasToken := false
		// 支持单行用逗号组合多个条件（如 status_code=524,openai_error），所有片段同时命中才算匹配。
		for token := range strings.SplitSeq(line, ",") {
			token = strings.TrimSpace(token)
			if token == "" {
				continue
			}
			hasToken = true
			if !strings.Contains(summary, token) {
				matched = false
				break
			}
		}
		if hasToken && matched {
			return true
		}
	}
	return false
}

// ParseLocalRequestValidationPolicy 解析并校验本地参数校验与错误拦截策略 JSON。
func ParseLocalRequestValidationPolicy(value string) (LocalRequestValidationPolicy, error) {
	var policy LocalRequestValidationPolicy
	if err := common.UnmarshalJsonStr(value, &policy); err != nil || strings.TrimSpace(value) == "null" {
		return policy, fmt.Errorf("本地参数校验设置格式无效")
	}
	if len(policy.Rules) > 10000 {
		return policy, fmt.Errorf("分组配置过多")
	}
	seen := make(map[string]bool, len(policy.Rules))
	selected := false
	for _, rule := range policy.Rules {
		if strings.TrimSpace(rule.Group) == "" || seen[rule.Group] {
			return policy, fmt.Errorf("分组名称不能为空或重复")
		}
		if rule.StatusCode < 400 || rule.StatusCode > 599 {
			return policy, fmt.Errorf("本地返回状态码必须是 400 到 599 的整数")
		}
		seen[rule.Group] = true
		selected = selected || rule.Enabled
	}
	if policy.UpstreamCooldownSeconds != nil && (*policy.UpstreamCooldownSeconds < 0 || *policy.UpstreamCooldownSeconds > 86400) {
		return policy, fmt.Errorf("错误冷却时间必须是 0 到 86400 的整数")
	}
	effectiveCodes := strings.TrimSpace(policy.UpstreamStatusCodes)
	if effectiveCodes == "" && policy.UpstreamCooldownSeconds == nil {
		effectiveCodes = DefaultUpstreamInterceptStatusCodes
	}
	if effectiveCodes != "" {
		ranges, err := ParseHTTPStatusCodeRanges(effectiveCodes)
		if err != nil || len(ranges) == 0 {
			return policy, fmt.Errorf("上游拦截状态码格式无效")
		}
		for _, r := range ranges {
			if r.Start < 400 || r.End > 599 {
				return policy, fmt.Errorf("上游拦截状态码必须是 400 到 599 的整数")
			}
		}
		policy.upstreamRanges = ranges
	}
	if policy.Enabled && !selected {
		return policy, fmt.Errorf("请至少选择一个启用校验的分组")
	}
	if policy.Enabled &&
		!policy.StructureCheckEnabled() &&
		!policy.MediaCheckEnabled() &&
		!policy.GenerationConfigCheckEnabled() &&
		strings.TrimSpace(policy.BlockedKeywords) == "" &&
		policy.CooldownSeconds() <= 0 {
		return policy, fmt.Errorf("请至少启用一项拦截规则")
	}
	return policy, nil
}

// GetLocalRequestValidationPolicy 读取当前生效的本地参数校验与错误拦截策略。
func GetLocalRequestValidationPolicy() (LocalRequestValidationPolicy, bool) {
	common.OptionMapRWMutex.RLock()
	value := common.OptionMap[LocalRequestValidationOption]
	common.OptionMapRWMutex.RUnlock()
	if value == "" {
		return LocalRequestValidationPolicy{}, false
	}
	policy, err := ParseLocalRequestValidationPolicy(value)
	if err != nil || !policy.Enabled {
		return LocalRequestValidationPolicy{}, false
	}
	return policy, true
}

// LocalRequestValidationStatus 返回指定分组配置的本地拦截状态码；未启用时返回 0。
func LocalRequestValidationStatus(group string) int {
	policy, ok := GetLocalRequestValidationPolicy()
	if !ok {
		return 0
	}
	return policy.GroupStatus(group)
}
