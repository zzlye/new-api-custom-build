package video_setting

import (
	"fmt"
	"math"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// 最大秒数同时供请求校验和计费使用，模型规则只能在此范围内进一步收窄。
const MaxDurationSeconds = 3600

type DurationConstraint struct {
	Min     int   `json:"min,omitempty"`
	Max     int   `json:"max,omitempty"`
	Values  []int `json:"values,omitempty"`
	Default int   `json:"default,omitempty"`
}

type Parameter struct {
	Key      string   `json:"key"`
	Label    string   `json:"label"`
	Type     string   `json:"type"`
	Editable bool     `json:"editable"`
	Required bool     `json:"required,omitempty"`
	Default  any      `json:"default,omitempty"`
	Options  []any    `json:"options,omitempty"`
	Min      *float64 `json:"min,omitempty"`
	Max      *float64 `json:"max,omitempty"`
}

// 空限制表示文档未明确；不同渠道的能力对象保持独立，不拼接成不存在的参数组合。
type Capabilities struct {
	Combinations            []VideoCombination `json:"combinations,omitempty"`
	Duration                DurationConstraint `json:"duration"`
	Resolutions             []string           `json:"resolutions,omitempty"`
	AspectRatios            []string           `json:"aspect_ratios,omitempty"`
	Modes                   []string           `json:"modes,omitempty"`
	PromptOptionalWithImage bool               `json:"prompt_optional_with_image,omitempty"`
	ImageLimit              *int               `json:"image_limit,omitempty"`
	VideoLimit              *int               `json:"video_limit,omitempty"`
	AudioLimit              *int               `json:"audio_limit,omitempty"`
	AudioRequiresVisual     bool               `json:"audio_requires_visual,omitempty"`
	VideoRequiresImage      bool               `json:"video_requires_image,omitempty"`
	FirstFrameWithVideo     bool               `json:"first_frame_with_video,omitempty"`
	GenerateAudio           *bool              `json:"generate_audio,omitempty"`
	MediaTransport          string             `json:"media_transport,omitempty"`
	Parameters              []Parameter        `json:"parameters,omitempty"`
}

// 同一渠道内部也可能有组合限制，例如长时长仅支持较低清晰度。
type VideoCombination struct {
	Duration     DurationConstraint `json:"duration"`
	Resolutions  []string           `json:"resolutions,omitempty"`
	AspectRatios []string           `json:"aspect_ratios,omitempty"`
	Modes        []string           `json:"modes,omitempty"`
}

func (c *Capabilities) Validate() error {
	if c == nil {
		return nil
	}
	if len(c.Combinations) > 64 {
		return fmt.Errorf("组合限制最多 64 组")
	}
	for _, combo := range c.Combinations {
		part := Capabilities{Duration: combo.Duration, Resolutions: combo.Resolutions, AspectRatios: combo.AspectRatios, Modes: combo.Modes}
		if err := part.Validate(); err != nil {
			return err
		}
	}
	for _, v := range c.Resolutions {
		if !regexp.MustCompile(`^(?:[1-9][0-9]*p|[1-9][0-9]*k)$`).MatchString(v) {
			return fmt.Errorf("清晰度格式无效：%s", v)
		}
	}
	for _, v := range c.AspectRatios {
		if !regexp.MustCompile(`^[1-9][0-9]*:[1-9][0-9]*$`).MatchString(v) {
			return fmt.Errorf("画面比例格式无效：%s", v)
		}
	}
	if c.Duration.Min < 0 || c.Duration.Min > MaxDurationSeconds || c.Duration.Max < 0 || c.Duration.Max > MaxDurationSeconds || (c.Duration.Max > 0 && c.Duration.Min > c.Duration.Max) {
		return fmt.Errorf("时长范围无效")
	}
	for _, n := range c.Duration.Values {
		if n < 1 || n > MaxDurationSeconds {
			return fmt.Errorf("可选时长无效")
		}
	}
	if c.Duration.Default != 0 && !c.durationAllowed(c.Duration.Default) {
		return fmt.Errorf("默认时长不在允许范围内")
	}
	for _, n := range []*int{c.ImageLimit, c.VideoLimit, c.AudioLimit} {
		if n != nil && (*n < 0 || *n > 128) {
			return fmt.Errorf("素材数量应为 0 到 128")
		}
	}
	for _, m := range c.Modes {
		if m != "text" && m != "references" && m != "frames" {
			return fmt.Errorf("生成模式无效")
		}
	}
	if c.MediaTransport != "" && c.MediaTransport != "url" && c.MediaTransport != "either" {
		return fmt.Errorf("素材格式无效")
	}
	seen := map[string]bool{}
	for _, p := range c.Parameters {
		if !fieldPath.MatchString(p.Key) || strings.Contains(p.Key, ".") || seen[p.Key] {
			return fmt.Errorf("自定义参数名无效或重复：%s", p.Key)
		}
		seen[p.Key] = true
		switch p.Type {
		case "string", "number", "integer", "boolean":
		default:
			return fmt.Errorf("自定义参数类型无效：%s", p.Key)
		}
		if p.Min != nil && (math.IsInf(*p.Min, 0) || math.IsNaN(*p.Min)) || p.Max != nil && (math.IsInf(*p.Max, 0) || math.IsNaN(*p.Max)) {
			return fmt.Errorf("参数范围无效")
		}
		if p.Min != nil && p.Max != nil && *p.Min > *p.Max {
			return fmt.Errorf("参数范围无效")
		}
		if p.Default != nil {
			if err := p.validateValue(p.Default); err != nil {
				return err
			}
		}
		for _, value := range p.Options {
			option := p
			option.Options = nil
			if err := option.validateValue(value); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *Capabilities) durationAllowed(n int) bool {
	if n < 1 || n > MaxDurationSeconds || c.Duration.Min > 0 && n < c.Duration.Min || c.Duration.Max > 0 && n > c.Duration.Max {
		return false
	}
	if len(c.Duration.Values) == 0 {
		return true
	}
	for _, v := range c.Duration.Values {
		if v == n {
			return true
		}
	}
	return false
}

func (p Parameter) validateValue(value any) error {
	valid := false
	switch p.Type {
	case "string":
		_, valid = value.(string)
	case "boolean":
		_, valid = value.(bool)
	case "number", "integer":
		n, ok := number(value)
		valid = ok && (p.Type != "integer" || math.Trunc(n) == n) && (p.Min == nil || n >= *p.Min) && (p.Max == nil || n <= *p.Max)
	}
	if !valid {
		return fmt.Errorf("参数 %s 类型或范围不符合要求", p.Key)
	}
	if len(p.Options) > 0 {
		for _, option := range p.Options {
			if reflect.DeepEqual(option, value) {
				return nil
			}
		}
		return fmt.Errorf("参数 %s 不在可选值中", p.Key)
	}
	return nil
}

func number(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, !math.IsNaN(n) && !math.IsInf(n, 0)
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}

// Normalize 保留输入语义，统一别名与类型；不静默钳制秒数、降级分辨率或更改素材用途。
func (p *Protocol) Normalize(input map[string]any) (map[string]any, error) {
	b, err := common.Marshal(input)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err = common.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	// 历史渠道把 input_reference 当作单张参考图，迁移后保留这一语义。
	if p.Capabilities == nil {
		if v, ok := out["input_reference"]; ok {
			if object, ok := v.(map[string]any); ok {
				v = object["image_url"]
			}
			if _, exists := out["image_urls"]; !exists {
				out["image_urls"] = []any{v}
			}
			delete(out, "input_reference")
		}
	}
	for _, aliases := range [][]string{{"duration", "seconds"}, {"image_urls", "reference_images"}, {"video_urls", "reference_videos"}, {"audio_urls", "reference_audios"}, {"first_frame", "input_reference"}, {"last_frame", "image_end"}} {
		if v, ok := out[aliases[1]]; ok {
			if old, exists := out[aliases[0]]; exists && fmt.Sprint(old) != fmt.Sprint(v) {
				return nil, fmt.Errorf("%s 与 %s 冲突", aliases[0], aliases[1])
			}
			out[aliases[0]] = v
			delete(out, aliases[1])
		}
	}
	if strings.TrimSpace(fmt.Sprint(out["model"])) == "" || out["model"] == nil {
		return nil, fmt.Errorf("缺少模型名")
	}
	duration, exists := out["duration"]
	if !exists {
		duration = 4
		if p.Capabilities != nil && p.Capabilities.Duration.Default > 0 {
			duration = p.Capabilities.Duration.Default
		}
	}
	if s, ok := duration.(string); ok {
		n, e := strconv.ParseFloat(strings.TrimSpace(s), 64)
		if e != nil {
			return nil, fmt.Errorf("时长必须为整数")
		}
		duration = n
	}
	n, ok := number(duration)
	if !ok || math.Trunc(n) != n || n < 1 || n > MaxDurationSeconds {
		return nil, fmt.Errorf("时长必须为 1 到 %d 的整数", MaxDurationSeconds)
	}
	out["duration"] = int(n)
	if value, ok := out["resolution"].(string); ok {
		v := strings.ToLower(strings.TrimSpace(value))
		if v == "2160p" {
			v = "4k"
		}
		if _, e := strconv.Atoi(v); e == nil {
			v += "p"
		}
		out["resolution"] = v
	}
	counts := map[string]int{}
	for _, key := range []string{"image_urls", "video_urls", "audio_urls"} {
		if v, present := out[key]; present {
			a, valid := v.([]any)
			if !valid {
				return nil, fmt.Errorf("%s 需要数组", key)
			}
			for _, source := range a {
				if err := validateMediaSource(source); err != nil {
					return nil, fmt.Errorf("%s：%w", key, err)
				}
			}
			counts[key] = len(a)
		}
	}
	for _, key := range []string{"first_frame", "last_frame"} {
		if v, present := out[key]; present {
			if err := validateMediaSource(v); err != nil {
				return nil, err
			}
			counts[key] = 1
		}
	}
	mode, _ := out["mode"].(string)
	if mode == "" || mode == "auto" {
		mode = "text"
		if counts["image_urls"]+counts["video_urls"]+counts["audio_urls"] > 0 {
			mode = "references"
		}
		if counts["first_frame"]+counts["last_frame"] > 0 {
			mode = "frames"
		}
	}
	switch mode {
	case "text", "references", "frames":
	default:
		return nil, fmt.Errorf("生成模式无效")
	}
	if mode == "text" && counts["image_urls"]+counts["video_urls"]+counts["audio_urls"]+counts["first_frame"]+counts["last_frame"] > 0 {
		return nil, fmt.Errorf("文生模式不能携带素材")
	}
	if counts["last_frame"] > 0 && counts["first_frame"] == 0 {
		return nil, fmt.Errorf("尾帧需要同时提供首帧")
	}
	if mode == "frames" && counts["first_frame"] == 0 {
		return nil, fmt.Errorf("首尾帧模式需要首帧")
	}
	if mode == "references" && counts["first_frame"]+counts["last_frame"] > 0 {
		return nil, fmt.Errorf("多参考模式不能混入首尾帧")
	}
	// 新规则使用确定的生成模式；实际发送字段仍由模板声明。
	if _, present := out["mode"]; present || p.Capabilities != nil {
		out["mode"] = mode
	}
	c := p.Capabilities
	prompt, _ := out["prompt"].(string)
	if strings.TrimSpace(prompt) == "" && !(c != nil && c.PromptOptionalWithImage && counts["image_urls"]+counts["first_frame"] > 0) {
		return nil, fmt.Errorf("请输入视频提示词")
	}
	if c == nil {
		return out, nil
	}
	if !c.durationAllowed(int(n)) {
		return nil, fmt.Errorf("该模型不支持 %d 秒", int(n))
	}
	if len(c.Combinations) > 0 {
		matched := false
		for _, combo := range c.Combinations {
			part := Capabilities{Duration: combo.Duration}
			accept := part.durationAllowed(int(n))
			for key, values := range map[string][]string{"resolution": combo.Resolutions, "aspect_ratio": combo.AspectRatios, "mode": combo.Modes} {
				value, present := out[key]
				if !present || len(values) == 0 {
					continue
				}
				found := false
				for _, v := range values {
					found = found || value == v
				}
				accept = accept && found
			}
			matched = matched || accept
		}
		if !matched {
			return nil, fmt.Errorf("该渠道不支持本次时长、清晰度、比例和模式组合")
		}
	}
	for _, item := range []struct {
		key    string
		values []string
	}{{"resolution", c.Resolutions}, {"aspect_ratio", c.AspectRatios}} {
		if v, exists := out[item.key]; exists && len(item.values) > 0 {
			found := false
			for _, allowed := range item.values {
				found = found || v == allowed
			}
			if !found {
				return nil, fmt.Errorf("该模型不支持 %s=%v", item.key, v)
			}
		}
	}
	if len(c.Modes) > 0 {
		found := false
		for _, m := range c.Modes {
			found = found || mode == m
		}
		if !found {
			return nil, fmt.Errorf("该模型不支持此生成模式")
		}
	}
	for _, item := range []struct {
		key   string
		limit *int
	}{{"image_urls", c.ImageLimit}, {"video_urls", c.VideoLimit}, {"audio_urls", c.AudioLimit}} {
		if item.limit != nil && counts[item.key] > *item.limit {
			return nil, fmt.Errorf("%s 最多 %d 个", item.key, *item.limit)
		}
	}
	if mode == "frames" && (counts["image_urls"]+counts["audio_urls"] > 0 || counts["video_urls"] > 0 && (!c.FirstFrameWithVideo || counts["last_frame"] > 0)) {
		return nil, fmt.Errorf("该模型不支持首尾帧与参考素材的组合")
	}
	if c.AudioRequiresVisual && counts["audio_urls"] > 0 && counts["image_urls"]+counts["video_urls"] == 0 {
		return nil, fmt.Errorf("参考音频需要同时提供参考图片或视频")
	}
	if c.VideoRequiresImage && counts["video_urls"] > 0 && counts["image_urls"]+counts["first_frame"] == 0 {
		return nil, fmt.Errorf("参考视频需要同时提供图片")
	}
	if value, exists := out["generate_audio"]; exists {
		if _, ok := value.(bool); !ok {
			return nil, fmt.Errorf("生成音频必须为布尔值")
		}
		if c.GenerateAudio == nil || !*c.GenerateAudio {
			return nil, fmt.Errorf("该模型未声明生成音频控制能力")
		}
	}
	extra := map[string]any{}
	if value, exists := out["extra_parameters"]; exists {
		var ok bool
		extra, ok = value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("附加参数需要对象")
		}
	}
	known := map[string]bool{}
	for _, param := range c.Parameters {
		known[param.Key] = true
		v, exists := extra[param.Key]
		if exists && !param.Editable {
			return nil, fmt.Errorf("参数 %s 不允许用户调整", param.Key)
		}
		if !exists && param.Default != nil {
			v = param.Default
			extra[param.Key] = v
			exists = true
		}
		if !exists {
			if param.Required {
				return nil, fmt.Errorf("缺少参数 %s", param.Key)
			}
			continue
		}
		if err := param.validateValue(v); err != nil {
			return nil, err
		}
	}
	for key := range extra {
		if !known[key] {
			return nil, fmt.Errorf("未声明的附加参数：%s", key)
		}
	}
	if len(extra) > 0 {
		out["extra_parameters"] = extra
	}
	return out, nil
}

func validateMediaSource(v any) error {
	if s, ok := v.(string); ok && (strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "data:")) {
		return nil
	}
	if o, ok := v.(map[string]any); ok && len(o) == 1 {
		if id, ok := o["asset_id"].(string); ok && strings.HasPrefix(id, "va_") {
			return nil
		}
	}
	return fmt.Errorf("素材需要 HTTP 地址、Data URL 或已上传的素材 ID")
}
