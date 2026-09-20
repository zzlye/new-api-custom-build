package video_setting

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

const RegistryKey = "VideoAdapters.V1"

type Template struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Protocol Protocol `json:"protocol"`
}

type Rule struct {
	ID         string    `json:"id"`
	Enabled    bool      `json:"enabled"`
	ChannelIDs []int     `json:"channel_ids"`
	Models     []string  `json:"models"`
	TemplateID string    `json:"template_id"`
	Override   *Protocol `json:"override,omitempty"`
}

type Registry struct {
	Version   int        `json:"version"`
	Templates []Template `json:"templates"`
	Rules     []Rule     `json:"rules"`
}

// 兼容接口只读写渠道默认规则，避免历史编辑入口产生第二套配置。
func (r *Registry) DefaultProtocol(channelID int) *Protocol {
	for _, rule := range r.Rules {
		if len(rule.Models) != 0 {
			continue
		}
		for _, id := range rule.ChannelIDs {
			if id != channelID {
				continue
			}
			var p Protocol
			if rule.Override != nil {
				p = *rule.Override
			} else {
				for _, t := range r.Templates {
					if t.ID == rule.TemplateID {
						p = t.Protocol
						break
					}
				}
			}
			p.Enabled = rule.Enabled
			return &p
		}
	}
	return nil
}

func (r *Registry) SetDefaultProtocol(channelID int, p Protocol) {
	// 共享默认规则拆出当前渠道，其余渠道继续引用原模板。
	kept := []Rule{}
	for _, rule := range r.Rules {
		if len(rule.Models) == 0 {
			ids := []int{}
			for _, id := range rule.ChannelIDs {
				if id != channelID {
					ids = append(ids, id)
				}
			}
			rule.ChannelIDs = ids
		}
		if len(rule.ChannelIDs) > 0 {
			kept = append(kept, rule)
		}
	}
	id := fmt.Sprintf("compat-default-%d", channelID)
	found := false
	for i := range r.Templates {
		if r.Templates[i].ID == id {
			r.Templates[i].Protocol = p
			found = true
		}
	}
	if !found {
		r.Templates = append(r.Templates, Template{ID: id, Name: fmt.Sprintf("渠道 %d 默认协议", channelID), Protocol: p})
	}
	r.Rules = append(kept, Rule{ID: id, Enabled: p.Enabled, ChannelIDs: []int{channelID}, Models: []string{}, TemplateID: id})
}

// 初次读取把历史配置投影为默认规则；首次发布时持久化，原选项保留作备份。
func LoadRegistry() (*Registry, error) {
	common.OptionMapRWMutex.RLock()
	raw := common.OptionMap[RegistryKey]
	legacy := map[string]string{}
	if raw == "" {
		for k, v := range common.OptionMap {
			if strings.HasPrefix(k, OptionPrefix) {
				legacy[k] = v
			}
		}
	}
	common.OptionMapRWMutex.RUnlock()
	if raw != "" {
		var r Registry
		if err := common.UnmarshalJsonStr(raw, &r); err != nil {
			return nil, err
		}
		return &r, r.Validate()
	}
	r := &Registry{Templates: []Template{}, Rules: []Rule{}}
	builtins := BuiltinTemplates()
	keys := []string{}
	for key := range builtins {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		r.Templates = append(r.Templates, builtins[key])
	}
	keys = nil
	for key := range legacy {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		id, e := strconv.Atoi(strings.TrimPrefix(key, OptionPrefix))
		if e != nil || id <= 0 {
			continue
		}
		var p Protocol
		if e = common.UnmarshalJsonStr(legacy[key], &p); e != nil {
			return nil, e
		}
		if e = p.Validate(); e != nil {
			return nil, e
		}
		name := "legacy-" + strconv.Itoa(id)
		r.Templates = append(r.Templates, Template{ID: name, Name: fmt.Sprintf("原渠道 %d 协议", id), Protocol: p})
		r.Rules = append(r.Rules, Rule{ID: name, Enabled: p.Enabled, ChannelIDs: []int{id}, Models: []string{}, TemplateID: name})
	}
	return r, nil
}

func (r *Registry) Validate() error {
	if len(r.Templates) > 512 || len(r.Rules) > 4096 {
		return fmt.Errorf("视频适配配置数量超过上限")
	}
	templates := map[string]bool{}
	for _, t := range r.Templates {
		if t.ID == "" || templates[t.ID] {
			return fmt.Errorf("模板编号为空或重复")
		}
		templates[t.ID] = true
		if err := t.Protocol.Validate(); err != nil {
			return fmt.Errorf("模板 %s：%w", t.Name, err)
		}
	}
	ids := map[string]bool{}
	bindings := map[string]bool{}
	for _, rule := range r.Rules {
		if rule.ID == "" || ids[rule.ID] || !templates[rule.TemplateID] || len(rule.ChannelIDs) == 0 {
			return fmt.Errorf("规则编号、渠道或模板无效")
		}
		ids[rule.ID] = true
		if rule.Override != nil {
			if err := rule.Override.Validate(); err != nil {
				return err
			}
		}
		models := rule.Models
		if len(models) == 0 {
			models = []string{""}
		}
		for _, id := range rule.ChannelIDs {
			if id <= 0 {
				return fmt.Errorf("渠道编号无效")
			}
			for _, name := range models {
				if len(rule.Models) > 0 && (strings.TrimSpace(name) == "" || name != strings.TrimSpace(name)) {
					return fmt.Errorf("模型名为空或含首尾空格")
				}
				key := fmt.Sprintf("%d\n%s", id, name)
				if rule.Enabled && bindings[key] {
					return fmt.Errorf("渠道 %d 的模型 %s 存在重复规则", id, name)
				}
				bindings[key] = bindings[key] || rule.Enabled
			}
		}
	}
	return nil
}

func (r *Registry) Resolve(channelID int, model string) (*Protocol, error) {
	var match *Rule
	for i := range r.Rules {
		rule := &r.Rules[i]
		if !rule.Enabled {
			continue
		}
		channelMatch := false
		for _, id := range rule.ChannelIDs {
			channelMatch = channelMatch || id == channelID
		}
		if !channelMatch {
			continue
		}
		if len(rule.Models) == 0 {
			if match == nil {
				match = rule
			}
			continue
		}
		for _, name := range rule.Models {
			if name == model {
				match = rule
			}
		}
	}
	if match == nil {
		return nil, nil
	}
	var p *Protocol
	if match.Override != nil {
		p = match.Override
	} else {
		for i := range r.Templates {
			if r.Templates[i].ID == match.TemplateID {
				p = &r.Templates[i].Protocol
				break
			}
		}
	}
	if p == nil {
		return nil, fmt.Errorf("规则引用的模板不存在")
	}
	// 返回独立快照，模板修改与并发请求不会共享可变字段。
	b, err := common.Marshal(p)
	if err != nil {
		return nil, err
	}
	var copy Protocol
	if err = common.Unmarshal(b, &copy); err != nil {
		return nil, err
	}
	copy.Enabled = true
	copy.Revision = r.Version
	return &copy, nil
}

func Resolve(channelID int, model string) (*Protocol, error) {
	r, err := LoadRegistry()
	if err != nil {
		return nil, err
	}
	return r.Resolve(channelID, model)
}

func BuiltinTemplates() map[string]Template {
	out := map[string]Template{}
	limits := func(n int) *int { return &n }
	yes := true
	for _, name := range []string{"wan-3.0", "sd2-5-720p", "seedance-2-pro", "seedance-2-fast", "seedance-2-mini", "seedance-2.5-pro", "wan-3", "gemini-omni-1.1"} {
		p := Presets()["standard"]
		p.Enabled = true
		p.Response.URL = "url|result_url|video_urls.0|metadata.url|metadata.result_url|data.url|data.video_url|output.video_url"
		c := &Capabilities{Duration: DurationConstraint{Min: 1, Max: 30, Default: 5}, Resolutions: []string{"720p"}, MediaTransport: "url", Modes: []string{"text", "references"}}
		p.Capabilities = c
		p.Fields[2] = Field{Source: "duration", Target: "seconds", Format: "number"}
		// 只有文档明确承诺的音频控制才加入模板。
		p.Fields = append(p.Fields[:5], p.Fields[6:]...)
		switch name {
		case "wan-3.0":
			p.Fields[2].Target = "duration"
			c.ImageLimit = limits(2)
			c.Duration.Default = 10
		case "sd2-5-720p":
			c.Duration = DurationConstraint{Default: 30}
			c.GenerateAudio = &yes
			p.Fields = append(p.Fields, Field{Source: "generate_audio", Target: "generate_audio", Format: "boolean"}, Field{Source: "mode", Target: "mode", Format: "identity", Values: map[string]any{"text": "text-to-video", "references": "reference-to-video"}})
		default:
			c.MediaTransport = "either"
			c.Modes = []string{"text", "references", "frames"}
			c.AspectRatios = []string{"21:9", "16:9", "4:3", "1:1", "3:4", "9:16"}
			c.Resolutions = []string{"480p", "720p"}
			c.ImageLimit = limits(9)
			c.VideoLimit = limits(3)
			c.AudioLimit = limits(3)
			c.AudioRequiresVisual = true
			for i := range p.Fields {
				switch p.Fields[i].Source {
				case "image_urls":
					p.Fields[i].Target = "reference_images"
				case "video_urls":
					p.Fields[i].Target = "reference_videos"
				case "audio_urls":
					p.Fields[i].Target = "reference_audios"
				}
			}
			p.Fields = append(p.Fields, Field{Source: "first_frame", Target: "input_reference", Format: "identity"}, Field{Source: "last_frame", Target: "image_end", Format: "identity"})
			if strings.HasPrefix(name, "seedance-") {
				c.GenerateAudio = &yes
				p.Fields = append(p.Fields, Field{Source: "generate_audio", Target: "sound_effects", Format: "boolean"})
			}
			switch name {
			case "seedance-2-pro":
				c.Duration = DurationConstraint{Min: 4, Max: 15, Default: 5}
				c.Resolutions = []string{"480p", "720p", "1080p", "4k"}
			case "seedance-2-fast", "seedance-2-mini":
				c.Duration = DurationConstraint{Values: []int{5, 10}, Default: 5}
			case "seedance-2.5-pro":
				c.Duration = DurationConstraint{Min: 4, Max: 30, Default: 5}
				c.Resolutions = []string{"480p", "720p", "1080p"}
				c.ImageLimit = limits(30)
				c.VideoLimit = limits(10)
				c.AudioLimit = limits(10)
				c.AudioRequiresVisual = false
			case "wan-3":
				c.Duration = DurationConstraint{Min: 2, Max: 30, Default: 5}
				c.Resolutions = []string{"480p", "720p", "1080p"}
				c.AspectRatios = nil
				c.ImageLimit = limits(10)
				c.VideoLimit = limits(5)
				c.AudioLimit = limits(5)
			case "gemini-omni-1.1":
				c.Duration = DurationConstraint{Min: 3, Max: 10, Default: 5}
				c.Resolutions = []string{"360p", "720p", "1080p", "4k"}
				c.AspectRatios = []string{"16:9", "9:16"}
				c.ImageLimit = limits(8)
				c.AudioLimit = limits(0)
				c.VideoRequiresImage = true
				c.FirstFrameWithVideo = true
				c.PromptOptionalWithImage = true
			}
		}
		out[name] = Template{ID: name, Name: name, Protocol: p}
	}
	return out
}
