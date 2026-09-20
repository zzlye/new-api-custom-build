package video_setting

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// MapRequest 只转换根用户定义的字段，不接受客户端传入规则或上游地址。
func (p *Protocol) MapRequest(input map[string]any) ([]byte, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	source, err := common.Marshal(input)
	if err != nil {
		return nil, err
	}
	output := []byte(`{}`)
	for key, value := range p.Defaults {
		output, err = sjson.SetBytes(output, key, value)
		if err != nil {
			return nil, err
		}
	}
	mappedMedia := map[string]bool{}
	declaredSources := map[string]bool{}
	for _, field := range p.Fields {
		declaredSources[field.Source] = true
		include := true
		for _, condition := range field.When {
			v := gjson.GetBytes(source, condition.Source)
			switch condition.Operator {
			case "exists":
				include = include && v.Exists()
			case "missing":
				include = include && !v.Exists()
			case "eq":
				include = include && reflect.DeepEqual(v.Value(), condition.Value)
			case "ne":
				include = include && !reflect.DeepEqual(v.Value(), condition.Value)
			}
		}
		if !include {
			continue
		}
		value := gjson.GetBytes(source, field.Source)
		if !value.Exists() && field.Fallback == nil {
			continue
		}
		inputValue := value.Value()
		if !value.Exists() {
			inputValue = field.Fallback
		}
		if field.Scale != 0 {
			n, ok := number(inputValue)
			if !ok {
				return nil, fmt.Errorf("%s 无法进行单位换算", field.Source)
			}
			inputValue = n * field.Scale
		}
		if len(field.Values) > 0 {
			mapped, ok := field.Values[fmt.Sprint(inputValue)]
			if !ok {
				return nil, fmt.Errorf("%s 没有对应的枚举值", field.Source)
			}
			inputValue = mapped
		}
		converted, err := convertField(inputValue, field)
		if err != nil {
			return nil, fmt.Errorf("%s：%w", field.Source, err)
		}
		output, err = sjson.SetBytes(output, field.Target, converted)
		if err != nil {
			return nil, err
		}
		mappedMedia[field.Source] = true
	}
	for _, key := range []string{"image_urls", "audio_urls", "video_urls", "first_frame", "last_frame"} {
		v := gjson.GetBytes(source, key)
		if v.Exists() && (!v.IsArray() || len(v.Array()) > 0) && !mappedMedia[key] {
			return nil, fmt.Errorf("渠道未配置 %s 映射", key)
		}
	}
	if value, ok := input["generate_audio"]; ok && value != nil && !mappedMedia["generate_audio"] {
		return nil, fmt.Errorf("渠道未配置生成音频映射")
	}
	if extra, ok := input["extra_parameters"].(map[string]any); ok && !declaredSources["extra_parameters"] {
		for key := range extra {
			if !declaredSources["extra_parameters."+key] {
				return nil, fmt.Errorf("渠道未配置附加参数 %s 映射", key)
			}
		}
	}
	return output, nil
}

func convertField(value any, field Field) (any, error) {
	switch field.Format {
	case "identity":
		return value, nil
	case "string":
		switch value.(type) {
		case string, bool, float64:
			return fmt.Sprint(value), nil
		}
	case "number":
		text := fmt.Sprint(value)
		if field.Source == "resolution" {
			text = strings.TrimSuffix(text, "p")
		}
		n, err := strconv.ParseFloat(text, 64)
		if err == nil {
			return n, nil
		}
	case "boolean":
		b, err := strconv.ParseBool(fmt.Sprint(value))
		if err == nil {
			return b, nil
		}
	case "not":
		b, ok := value.(bool)
		if ok {
			return !b, nil
		}
	case "array":
		return []any{value}, nil
	case "object":
		body, err := sjson.Set(`{}`, field.ItemKey, value)
		if err != nil {
			return nil, err
		}
		var result map[string]any
		err = common.UnmarshalJsonStr(body, &result)
		return result, err
	case "single", "single_object", "objects", "frames":
		items, ok := value.([]any)
		if !ok || len(items) == 0 {
			return nil, fmt.Errorf("需要非空数组")
		}
		if (field.Format == "single" || field.Format == "single_object") && len(items) != 1 {
			return nil, fmt.Errorf("当前映射只接收一个素材，实际收到 %d 个", len(items))
		}
		if field.Format == "single" {
			return items[0], nil
		}
		if field.Format == "frames" {
			if len(items) > 2 {
				return nil, fmt.Errorf("首尾帧映射最多接收两张图片")
			}
			frames := map[string]any{"first_frame": items[0]}
			if len(items) == 2 {
				frames["last_frame"] = items[1]
			}
			return frames, nil
		}
		objects := make([]any, 0, len(items))
		for _, item := range items {
			body, err := sjson.Set(`{}`, field.ItemKey, item)
			if err != nil {
				return nil, err
			}
			var object map[string]any
			if err = common.UnmarshalJsonStr(body, &object); err != nil {
				return nil, err
			}
			objects = append(objects, object)
		}
		if field.Format == "single_object" {
			return objects[0], nil
		}
		return objects, nil
	}
	return nil, fmt.Errorf("值与字段格式 %s 不匹配", field.Format)
}

// Read 支持以 | 分隔的候选路径，只解析数据，不执行表达式或脚本。
func Read(body []byte, paths string) string {
	for _, path := range strings.Split(paths, "|") {
		if path == "" {
			continue
		}
		value := gjson.GetBytes(body, path)
		if value.Exists() && value.Type != gjson.Null && !value.IsObject() && !value.IsArray() && strings.TrimSpace(value.String()) != "" {
			return strings.TrimSpace(value.String())
		}
	}
	return ""
}

func (p *Protocol) ReadStatus(body []byte) (string, error) {
	raw := strings.ToLower(Read(body, p.Response.Status))
	status := p.Response.States[raw]
	if status == "" {
		for name, mapped := range p.Response.States {
			if strings.EqualFold(strings.TrimSpace(name), raw) {
				status = mapped
				break
			}
		}
	}
	if status == "" {
		return "", fmt.Errorf("渠道返回未配置的任务状态：%s", raw)
	}
	return status, nil
}
