package video_setting

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVideoProtocolNestedMappingsPreserveValues(t *testing.T) {
	p := Presets()["standard"]
	p.Fields[2] = Field{Source: "duration", Target: "parameters.seconds", Format: "string"}
	p.Fields[4] = Field{Source: "resolution", Target: "parameters.resolution", Format: "number"}
	p.Fields[5] = Field{Source: "generate_audio", Target: "parameters.generate_audio", Format: "identity"}
	p.Fields[6] = Field{Source: "image_urls", Target: "input.references", Format: "objects", ItemKey: "image_url.url"}
	p.Defaults = map[string]any{"parameters.watermark": false}
	body, err := p.MapRequest(map[string]any{"model": "future", "prompt": "测试", "duration": 9, "resolution": "1080p", "generate_audio": false, "image_urls": []string{"first", "second"}})
	require.NoError(t, err)
	assert.JSONEq(t, `{"model":"future","prompt":"测试","parameters":{"seconds":"9","resolution":1080,"generate_audio":false,"watermark":false},"input":{"references":[{"image_url":{"url":"first"}},{"image_url":{"url":"second"}}]}}`, string(body))
}

func TestVideoProtocolRejectsAmbiguousAndBillingOverrides(t *testing.T) {
	for _, mutate := range []func(*Protocol){
		func(p *Protocol) { p.Fields[3].Target = "duration" },
		func(p *Protocol) { p.Fields[3].Target = "parameters.seconds" },
		func(p *Protocol) { p.Defaults = map[string]any{"parameters": map[string]any{"seconds": 999999999}} },
		func(p *Protocol) { p.Fields[2].Format = "boolean" },
		func(p *Protocol) { p.Headers = map[string]string{"X-API-Key": "key\r\nHost:other"} },
		func(p *Protocol) { p.SubmitPath = "//other.test/submit" },
	} {
		p := Presets()["standard"]
		mutate(&p)
		assert.Error(t, p.Validate())
	}
}

func TestVideoProtocolTaskIDCannotChangeQueryOrPath(t *testing.T) {
	id := "job/a&admin=true#fragment"
	value, err := Endpoint("https://channel.test", "/jobs?task={id}", id)
	require.NoError(t, err)
	u, err := url.Parse(value)
	require.NoError(t, err)
	assert.Equal(t, id, u.Query().Get("task"))
	assert.Empty(t, u.Query().Get("admin"))
	assert.Empty(t, u.Fragment)
	value, err = Endpoint("https://channel.test", "/jobs/{id}", "..")
	require.NoError(t, err)
	assert.Equal(t, "https://channel.test/jobs/%2E%2E", value)
}
