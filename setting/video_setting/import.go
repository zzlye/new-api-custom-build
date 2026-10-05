package video_setting

import (
	_ "embed"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// 下载文件与导入功能随程序一起发布，避免说明和示例版本不一致。
//
//go:embed import-template.json
var AdapterImportTemplate []byte

//go:embed import-guide.md
var AdapterImportGuide []byte

type ImportBundle struct {
	Format        string     `json:"format"`
	FormatVersion int        `json:"format_version"`
	Templates     []Template `json:"templates"`
	Rules         []Rule     `json:"rules"`
}

// MergeImport 只生成独立草稿；同名模板重新编号，现有规则和发布版本保持不变。
func MergeImport(base Registry, bundle ImportBundle, channelIDs []int) (*Registry, error) {
	if bundle.Format != "newapi-video-adapters" || bundle.FormatVersion != 1 {
		return nil, fmt.Errorf("配置文件格式或版本不支持，请使用下载的导入模板")
	}
	if len(bundle.Templates) == 0 || len(bundle.Templates) > 512 || len(bundle.Rules) > 4096 {
		return nil, fmt.Errorf("请提供 1 到 512 个模板，模型规则最多 4096 条")
	}
	bytes, err := common.Marshal(base)
	if err != nil {
		return nil, err
	}
	var next Registry
	if err = common.Unmarshal(bytes, &next); err != nil {
		return nil, err
	}
	templateIDs := map[string]bool{}
	ruleIDs := map[string]bool{}
	for _, template := range next.Templates {
		templateIDs[template.ID] = true
	}
	for _, rule := range next.Rules {
		ruleIDs[rule.ID] = true
	}
	importedIDs := map[string]string{}
	for _, template := range bundle.Templates {
		if template.ID == "" || importedIDs[template.ID] != "" {
			return nil, fmt.Errorf("导入模板编号为空或重复")
		}
		if strings.TrimSpace(template.Name) == "" {
			return nil, fmt.Errorf("模板 %s 缺少名称", template.ID)
		}
		if err = validateImportProtocol(template.Protocol); err != nil {
			return nil, fmt.Errorf("模板 %s：%w", template.Name, err)
		}
		original := template.ID
		template.ID = availableImportID(original, templateIDs)
		importedIDs[original] = template.ID
		next.Templates = append(next.Templates, template)
	}
	seenRules := map[string]bool{}
	for _, rule := range bundle.Rules {
		if rule.ID == "" || seenRules[rule.ID] {
			return nil, fmt.Errorf("导入规则编号为空或重复")
		}
		seenRules[rule.ID] = true
		templateID, exists := importedIDs[rule.TemplateID]
		if !exists {
			return nil, fmt.Errorf("规则 %s 引用的模板未包含在文件中", rule.ID)
		}
		if rule.Override != nil {
			if err = validateImportProtocol(*rule.Override); err != nil {
				return nil, fmt.Errorf("规则 %s：%w", rule.ID, err)
			}
		}
		rule.ID = availableImportID(rule.ID, ruleIDs)
		rule.TemplateID = templateID
		if len(channelIDs) > 0 {
			rule.ChannelIDs = append([]int(nil), channelIDs...)
		}
		if len(rule.ChannelIDs) == 0 {
			return nil, fmt.Errorf("规则 %s 尚未选择适用渠道", rule.ID)
		}
		next.Rules = append(next.Rules, rule)
	}
	if err = next.Validate(); err != nil {
		return nil, err
	}
	return &next, nil
}

// 文件编号只用于关联模板，导入不得借相同编号覆盖当前草稿。
func availableImportID(id string, used map[string]bool) string {
	candidate := id
	for suffix := 1; used[candidate]; suffix++ {
		candidate = fmt.Sprintf("%s-import-%d", id, suffix)
	}
	used[candidate] = true
	return candidate
}

// 左侧字段来自统一输入；上游的新字段应写在目标路径或已声明的附加参数中。
func validateImportProtocol(protocol Protocol) error {
	if err := protocol.Validate(); err != nil {
		return err
	}
	allowed := map[string]bool{}
	for _, key := range []string{"model", "prompt", "duration", "resolution", "aspect_ratio", "generate_audio", "image_urls", "video_urls", "audio_urls", "first_frame", "last_frame", "mode", "extra_parameters"} {
		allowed[key] = true
	}
	if protocol.Capabilities != nil {
		for _, parameter := range protocol.Capabilities.Parameters {
			allowed["extra_parameters."+parameter.Key] = true
		}
	}
	for _, field := range protocol.Fields {
		if !allowed[field.Source] {
			return fmt.Errorf("未知统一输入字段 %s；上游字段请填写 target，新参数先在 capabilities.parameters 声明", field.Source)
		}
	}
	return nil
}
