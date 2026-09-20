package video_setting

import (
	"github.com/tidwall/gjson"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 文档列出的八个模型逐一保护已知边界，未知能力不由示例推成限制。
func TestEightDocumentedVideoModels(t *testing.T) {
	for _, tc := range []struct {
		name              string
		seconds           int
		resolution, field string
		invalidSeconds    int
	}{
		{"wan-3.0", 30, "720p", "duration", 31}, {"sd2-5-720p", 30, "720p", "seconds", 0},
		{"seedance-2-pro", 15, "4k", "seconds", 16}, {"seedance-2-fast", 10, "480p", "seconds", 7},
		{"seedance-2-mini", 5, "720p", "seconds", 6}, {"seedance-2.5-pro", 30, "1080p", "seconds", 31},
		{"wan-3", 2, "1080p", "seconds", 1}, {"gemini-omni-1.1", 3, "360p", "seconds", 11},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := BuiltinTemplates()[tc.name].Protocol
			input := map[string]any{"model": tc.name, "prompt": "镜头推进", "duration": tc.seconds, "resolution": tc.resolution}
			normalized, err := p.Normalize(input)
			require.NoError(t, err)
			body, err := p.MapRequest(normalized)
			require.NoError(t, err)
			assert.EqualValues(t, tc.seconds, gjson.GetBytes(body, tc.field).Int())
			if tc.invalidSeconds != 0 {
				input["duration"] = tc.invalidSeconds
				_, err = p.Normalize(input)
				require.Error(t, err)
			}
			assert.Equal(t, "job", Read([]byte(`{"data":{"id":"job"}}`), p.Response.ID))
			state, err := p.ReadStatus([]byte(`{"status":"in_progress"}`))
			require.NoError(t, err)
			assert.Equal(t, "in_progress", state)
		})
	}
}

func TestVideoCombinationRulesAndDurationIntent(t *testing.T) {
	p := BuiltinTemplates()["seedance-2.5-pro"].Protocol
	p.Capabilities.Resolutions = []string{"720p", "4k"}
	p.Capabilities.Combinations = []VideoCombination{{Duration: DurationConstraint{Max: 30}, Resolutions: []string{"720p"}}, {Duration: DurationConstraint{Max: 10}, Resolutions: []string{"4k"}}}
	_, err := p.Normalize(map[string]any{"model": "future", "prompt": "镜头", "duration": 30, "resolution": "4k"})
	require.Error(t, err)
	_, err = p.Normalize(map[string]any{"model": "future", "prompt": "镜头", "duration": 30, "resolution": "720p"})
	require.NoError(t, err)
	p.Fields[2].Values = map[string]any{"30": 10}
	require.Error(t, p.Validate())
}

func TestDeclaredParameterRequiresMappingBeforePublish(t *testing.T) {
	p := BuiltinTemplates()["seedance-2-mini"].Protocol
	p.Capabilities.Parameters = []Parameter{{Key: "seed", Label: "种子", Type: "integer", Editable: true}}
	require.ErrorContains(t, p.Validate(), "seed")
	p.Fields = append(p.Fields, Field{Source: "extra_parameters.seed", Target: "options.seed", Format: "identity"})
	require.NoError(t, p.Validate())
}

func TestLegacyDefaultMigrationAndNewFieldOmission(t *testing.T) {
	p := Presets()["reference_object"]
	p.Enabled = true
	input, err := p.Normalize(map[string]any{"model": "legacy", "prompt": "测试", "seconds": "5", "input_reference": map[string]any{"image_url": "https://cdn.test/input.png"}})
	require.NoError(t, err)
	body, err := p.MapRequest(input)
	require.NoError(t, err)
	assert.Equal(t, "https://cdn.test/input.png", gjson.GetBytes(body, "input_reference.image_url").String())
	r := Registry{}
	r.SetDefaultProtocol(8, p)
	require.NoError(t, r.Validate())
	assert.Equal(t, &p, r.DefaultProtocol(8))
	p = Presets()["standard"]
	p.Fields = append(p.Fields, Field{Source: "extra_parameters.optional", Target: "options.optional", Format: "identity", When: []Condition{{Source: "mode", Operator: "eq", Value: "references"}}})
	body, err = p.MapRequest(map[string]any{"model": "new", "prompt": "测试", "duration": 5, "mode": "text", "extra_parameters": map[string]any{"optional": false}})
	require.NoError(t, err)
	assert.False(t, gjson.GetBytes(body, "options.optional").Exists())
}

// 同一个协议模板能够校验模型能力，并且转换时不丢失显式关闭音频的值。
func TestAdapterNormalizesAndMapsDocumentedVideoRequest(t *testing.T) {
	template := BuiltinTemplates()["seedance-2-mini"]
	input, err := template.Protocol.Normalize(map[string]any{"model": "seedance-2-mini", "prompt": "镜头前进", "duration": 5, "resolution": "480", "generate_audio": false})
	require.NoError(t, err)
	body, err := template.Protocol.MapRequest(input)
	require.NoError(t, err)
	assert.JSONEq(t, `{"model":"seedance-2-mini","prompt":"镜头前进","seconds":5,"resolution":"480p","sound_effects":false}`, string(body))
	_, err = template.Protocol.Normalize(map[string]any{"model": "seedance-2-mini", "prompt": "镜头前进", "duration": 7, "resolution": "1080p"})
	assert.Error(t, err)
}

func TestDocumentedTemplatesKeepModesAndCapabilitiesDistinct(t *testing.T) {
	for name, template := range BuiltinTemplates() {
		t.Run(name, func(t *testing.T) { require.NoError(t, template.Protocol.Validate()) })
	}
	p := BuiltinTemplates()["gemini-omni-1.1"].Protocol
	_, err := p.Normalize(map[string]any{"model": "gemini-omni-1.1", "duration": 3, "resolution": "360p", "mode": "frames", "first_frame": "https://cdn.test/start.png", "video_urls": []string{"https://cdn.test/motion.mp4"}})
	require.NoError(t, err)
	_, err = p.Normalize(map[string]any{"model": "gemini-omni-1.1", "prompt": "测试", "duration": 3, "audio_urls": []string{"https://cdn.test/audio.wav"}})
	assert.Error(t, err)
	for _, name := range []string{"seedance-2-pro", "seedance-2.5-pro"} {
		p := BuiltinTemplates()[name].Protocol
		_, err := p.Normalize(map[string]any{"model": name, "prompt": "跟随节拍", "duration": 5, "audio_urls": []string{"https://cdn.test/audio.wav"}})
		if name == "seedance-2.5-pro" {
			assert.NoError(t, err)
		} else {
			assert.Error(t, err)
		}
	}
}

func TestRegistrySelectsExactModelWithoutGuessingNames(t *testing.T) {
	a := BuiltinTemplates()["wan-3.0"]
	b := BuiltinTemplates()["sd2-5-720p"]
	r := Registry{Version: 4, Templates: []Template{a, b}, Rules: []Rule{{ID: "default", Enabled: true, ChannelIDs: []int{1}, TemplateID: a.ID}, {ID: "exact", Enabled: true, ChannelIDs: []int{1}, Models: []string{"my-video"}, TemplateID: b.ID}}}
	require.NoError(t, r.Validate())
	p, err := r.Resolve(1, "my-video")
	require.NoError(t, err)
	assert.Equal(t, "seconds", p.Fields[2].Target)
	assert.Equal(t, 4, p.Revision)
	p, err = r.Resolve(1, "wan-3-new")
	require.NoError(t, err)
	assert.Equal(t, "duration", p.Fields[2].Target)
	p, err = r.Resolve(2, "my-video")
	require.NoError(t, err)
	assert.Nil(t, p)
	r.Rules = append(r.Rules, r.Rules[1])
	assert.Error(t, r.Validate())
}

func TestCustomParameterConversionDoesNotRequireModelCode(t *testing.T) {
	p := BuiltinTemplates()["seedance-2-mini"].Protocol
	p.Fields[2].Target = "parameters.clip_length"
	p.Fields[2].Scale = 1000
	p.Capabilities.Parameters = []Parameter{{Key: "camera", Label: "镜头", Type: "string", Editable: true, Options: []any{"static", "moving"}}}
	p.Fields = append(p.Fields, Field{Source: "extra_parameters.camera", Target: "options.camera", Format: "identity"}, Field{Source: "generate_audio", Target: "no_music", Format: "not"})
	input, err := p.Normalize(map[string]any{"model": "brand-new", "prompt": "测试", "duration": 5, "generate_audio": false, "extra_parameters": map[string]any{"camera": "static"}})
	require.NoError(t, err)
	data, err := p.MapRequest(input)
	require.NoError(t, err)
	assert.JSONEq(t, `{"model":"brand-new","prompt":"测试","parameters":{"clip_length":5000},"sound_effects":false,"no_music":true,"options":{"camera":"static"}}`, string(data))
}
