package common

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTaskSubmitReqAcceptsBafangImageObject(t *testing.T) {
	var req TaskSubmitReq
	err := json.Unmarshal([]byte(`{"model":"grok-imagine-video-1.5-720p","prompt":"画面动起来","image":{"url":"data:image/png;base64,abc"},"duration":6}`), &req)

	require.NoError(t, err)
	require.Equal(t, "data:image/png;base64,abc", req.Image)
	require.Equal(t, 6, req.Duration)
	require.True(t, req.HasImage())
}

func TestTaskSubmitReqKeepsImageStringCompatible(t *testing.T) {
	var req TaskSubmitReq
	err := json.Unmarshal([]byte(`{"model":"sora-2","prompt":"test","image":"https://example.com/a.png","duration":"8"}`), &req)

	require.NoError(t, err)
	require.Equal(t, "https://example.com/a.png", req.Image)
	require.Equal(t, 8, req.Duration)
	require.True(t, req.HasImage())
}
