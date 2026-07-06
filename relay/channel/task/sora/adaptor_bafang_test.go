package sora

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

func TestBafangGrokImagineVideo15UsesGenerationsEndpoint(t *testing.T) {
	adaptor := &TaskAdaptor{baseURL: "https://api.example.com"}
	url, err := adaptor.BuildRequestURL(&relaycommon.RelayInfo{OriginModelName: "grok-imagine-video-1.5-1080p"})

	require.NoError(t, err)
	require.Equal(t, "https://api.example.com/v1/videos/generations", url)
}

func TestBafangGrokImagineVideo15ParsesDoneVideoUrl(t *testing.T) {
	adaptor := &TaskAdaptor{}
	info, err := adaptor.ParseTaskResult([]byte(`{"request_id":"req_1","status":"done","video":{"url":"https://cdn.example.com/video.mp4"}}`))

	require.NoError(t, err)
	require.Equal(t, model.TaskStatusSuccess, info.Status)
	require.Equal(t, "https://cdn.example.com/video.mp4", info.Url)
}

func TestBafangGrokImagineVideo15ConvertsFetchResponse(t *testing.T) {
	adaptor := &TaskAdaptor{}
	task := &model.Task{
		TaskID:     "task_public",
		Status:     model.TaskStatusSuccess,
		FailReason: "https://cdn.example.com/video.mp4",
		Properties: model.Properties{
			OriginModelName: "grok-imagine-video-1.5-720p",
		},
	}

	body, err := adaptor.ConvertToOpenAIVideo(task)

	require.NoError(t, err)
	require.JSONEq(t, `{"request_id":"task_public","status":"done","video":{"url":"https://cdn.example.com/video.mp4"}}`, string(body))
}
