package operation_setting

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

const LocalRequestValidationOption = "LocalRequestValidationPolicy"
const DefaultLocalRequestValidationJSON = `{"enabled":false,"rules":[]}`

// LocalRequestValidationRule 按实际使用的分组配置本地参数校验及返回状态码。
type LocalRequestValidationRule struct {
	Group      string `json:"group"`
	Enabled    bool   `json:"enabled"`
	StatusCode int    `json:"status_code"`
}

type LocalRequestValidationPolicy struct {
	Enabled bool                         `json:"enabled"`
	Rules   []LocalRequestValidationRule `json:"rules"`
}

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
	if policy.Enabled && !selected {
		return policy, fmt.Errorf("请至少选择一个启用校验的分组")
	}
	return policy, nil
}

func LocalRequestValidationStatus(group string) int {
	common.OptionMapRWMutex.RLock()
	value := common.OptionMap[LocalRequestValidationOption]
	common.OptionMapRWMutex.RUnlock()
	if value == "" {
		return 0
	}
	policy, err := ParseLocalRequestValidationPolicy(value)
	if err != nil || !policy.Enabled {
		return 0
	}
	for _, rule := range policy.Rules {
		if rule.Enabled && rule.Group == group {
			return rule.StatusCode
		}
	}
	return 0
}
