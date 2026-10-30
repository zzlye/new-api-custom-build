package operation_setting

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
)

// 冷却错误映射独立保存，修改用户名不会清空已有请求指纹或重置冷却时间。
const CooldownErrorMappingOption = "CooldownErrorMappingPolicy"
const DefaultCooldownErrorMappingJSON = `{"enabled":false,"usernames":[]}`

type CooldownErrorMappingPolicy struct {
	Enabled   bool     `json:"enabled"`
	Usernames []string `json:"usernames"`
}

// ParseCooldownErrorMappingPolicy 限制配置规模并拒绝空名、重复名称，用户名按原样精确匹配。
func ParseCooldownErrorMappingPolicy(value string) (CooldownErrorMappingPolicy, error) {
	var policy CooldownErrorMappingPolicy
	if len(value) > 131072 {
		return policy, fmt.Errorf("冷却错误映射配置不能超过 128 KB")
	}
	if err := common.UnmarshalJsonStr(value, &policy); err != nil || strings.TrimSpace(value) == "null" {
		return policy, fmt.Errorf("冷却错误映射设置格式无效")
	}
	if len(policy.Usernames) > 1000 {
		return policy, fmt.Errorf("最多配置 1000 个用户名")
	}
	seen := make(map[string]bool, len(policy.Usernames))
	for _, username := range policy.Usernames {
		if strings.TrimSpace(username) == "" || strings.TrimSpace(username) != username ||
			utf8.RuneCountInString(username) > 64 || strings.ContainsAny(username, "\r\n\x00") || seen[username] {
			return policy, fmt.Errorf("用户名不能为空、重复或超过 64 字符，且不能包含首尾空白或换行")
		}
		seen[username] = true
	}
	return policy, nil
}

// ShouldMapCooldownError 仅使用认证上下文中的用户名，不从客户端请求体或请求头取值。
func ShouldMapCooldownError(username string) bool {
	if username == "" {
		return false
	}
	common.OptionMapRWMutex.RLock()
	value := common.OptionMap[CooldownErrorMappingOption]
	common.OptionMapRWMutex.RUnlock()
	policy, err := ParseCooldownErrorMappingPolicy(value)
	return err == nil && policy.Enabled && slices.Contains(policy.Usernames, username)
}
