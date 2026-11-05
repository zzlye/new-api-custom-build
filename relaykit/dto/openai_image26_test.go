package dto

import (
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/stretchr/testify/require"
	"testing"
)

// 新参数在深拷贝和适配器序列化后仍然完整，未知私有字段不随之放行。
func TestImage26RequestRoundTrip(t *testing.T) {
	input := `{"model":"gpt-image-2.6-flare","prompt":"draw","complexity":"low","frame_count":9,"frame_duration_ms":200,"markup":{"image_url":"https://example.test/overlay.png"},"svg":{"model":"author","reasoning":{"effort":"high"}},"safety_identifier":"hashed-id","output_format":"svg","private_unlisted":"drop"}`
	var request ImageRequest
	require.NoError(t, kitutil.Unmarshal([]byte(input), &request))
	encoded, err := kitutil.Marshal(request)
	require.NoError(t, err)
	var actual, expected map[string]any
	require.NoError(t, kitutil.Unmarshal(encoded, &actual))
	require.NoError(t, kitutil.Unmarshal([]byte(input), &expected))
	delete(expected, "private_unlisted")
	require.Equal(t, expected, actual)
	var omitted ImageRequest
	require.NoError(t, kitutil.Unmarshal([]byte(`{"model":"gpt-image-2.6-sunburst","prompt":"draw"}`), &omitted))
	require.Nil(t, omitted.FrameCount)
	require.Nil(t, omitted.Complexity)
}
