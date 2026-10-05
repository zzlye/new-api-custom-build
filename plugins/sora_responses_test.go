package plugins_test

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/plugins"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSoraResponsesProtocol(t *testing.T) {
	testVideoResponsesProtocol(t, videoResponsesTestCase{
		pluginKey: "sora",
		model:     "sora-2-pro",
		requestBody: map[string]any{
			"model":   "sora-2-pro",
			"input":   "waves at sunset",
			"seconds": 8,
			"size":    "1792x1024",
		},
		wantAction: "text_to_video",
		wantRequest: map[string]any{
			"model":   "sora-2-pro",
			"prompt":  "waves at sunset",
			"seconds": float64(8),
			"size":    "1792x1024",
		},
		wantUsageKeys:  []string{"seconds", "size"},
		wantVendorName: "sora",
	})
}

func TestSoraOpenAIVideoAcceptsUnifiedImageURLs(t *testing.T) {
	source, err := plugins.Source("sora")
	require.NoError(t, err)
	plugin, err := jsplugin.NewRegistry().RegisterFactory(source, jsplugin.Options{Key: "sora"})
	require.NoError(t, err)

	value, err := plugin.Engine.CallPath(t.Context(), "protocols", []string{"openai_video", "decodeRequest"}, map[string]any{
		"model": "sd-2.0-ch1",
		"body": map[string]any{"kind": "json", "value": map[string]any{
			"model":      "sd-2.0-ch1",
			"prompt":     "起身看向外面",
			"duration":   5,
			"image_urls": []string{"https://cdn.example/reference.png"},
		}},
	})
	require.NoError(t, err)

	encoded, err := common.Marshal(value)
	require.NoError(t, err)
	var decoded map[string]any
	require.NoError(t, common.Unmarshal(encoded, &decoded))
	assert.Equal(t, "image_to_video", decoded["action"])
	requestBody := decoded["requestBody"].(map[string]any)
	assert.Equal(t, "https://cdn.example/reference.png", requestBody["input_reference"])
	assert.Equal(t, float64(5), requestBody["seconds"])
	assert.NotContains(t, requestBody, "image_urls")
	assert.NotContains(t, requestBody, "duration")

	_, err = plugin.Engine.CallPath(t.Context(), "protocols", []string{"openai_video", "decodeRequest"}, map[string]any{
		"model": "sd-2.0-ch1",
		"body": map[string]any{"kind": "json", "value": map[string]any{
			"model":      "sd-2.0-ch1",
			"prompt":     "起身看向外面",
			"image_urls": []string{"first", "second"},
		}},
	})
	require.ErrorContains(t, err, "at most one image")
}
