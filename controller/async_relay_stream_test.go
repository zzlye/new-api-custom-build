package controller

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 流式返回保持原文，任务日志只提取最终图片，不把预览分片当成生成结果。
func TestAsyncRelayStoresFinalMediaFromStreamingResponses(t *testing.T) {
	cases := []struct{ name, body string }{
		{"Images最终图片", fmt.Sprintf("event: image_generation.partial_image\ndata: {\"type\":\"image_generation.partial_image\",\"b64_json\":\"partial\"}\n\nevent: image_generation.completed\ndata: {\"type\":\"image_generation.completed\",\"b64_json\":\"%s\"}\n\ndata: [DONE]\n\n", asyncFixturePNG)},
		{"Responses去重最终结果", fmt.Sprintf("data: {\"type\":\"response.image_generation_call.partial_image\",\"partial_image_b64\":\"partial\"}\n\ndata: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"image_generation_call\",\"result\":\"%s\"}}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"output\":[{\"type\":\"image_generation_call\",\"result\":\"%s\"}]}}\n\n", asyncFixturePNG, asyncFixturePNG)},
		{"Gemini图片分块", fmt.Sprintf(": keepalive\n\ndata: {\"candidates\":[{\"content\":{\"parts\":[{\"inlineData\":{\"mimeType\":\"image/png\",\"data\":\"%s\"}}]}}]}\n\n", asyncFixturePNG)},
		{"聊天图片增量", fmt.Sprintf("data: {\"choices\":[{\"delta\":{\"images\":[{\"image_url\":{\"url\":\"data:image/png;base64,%s\"}}]}}]}\n\ndata: [DONE]\n\n", asyncFixturePNG)},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			prepareAsyncMediaController(t)
			task := &model.AsyncRelayTask{UserID: 91, NodeID: common.NodeName, RequestFormat: string(relaytypes.RelayFormatOpenAIImage)}
			require.NoError(t, task.InsertWithLog("default", "IMAGE"))
			claimed, won, err := model.ClaimAsyncRelayTask(task.ID, "stream-fixture")
			require.NoError(t, err)
			require.True(t, won)
			path, file, err := common.CreateAsyncMediaFile()
			require.NoError(t, err)
			_, err = file.WriteString(test.body)
			require.NoError(t, err)
			require.NoError(t, file.Close())
			require.True(t, completeAsyncRelayResult(context.Background(), claimed, path, "text/event-stream"))
			require.Equal(t, model.AsyncRelayTaskStatusSucceeded, claimed.Status, claimed.Error)
			assert.Equal(t, "text/event-stream", claimed.ResultContentType)
			var media []model.AsyncRelayMedia
			require.NoError(t, common.Unmarshal([]byte(claimed.ResultFiles), &media))
			require.Len(t, media, 1)
			expected, err := base64.StdEncoding.DecodeString(asyncFixturePNG)
			require.NoError(t, err)
			actual, err := os.ReadFile(media[0].Path)
			require.NoError(t, err)
			assert.Equal(t, expected, actual)
			raw, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Equal(t, test.body, string(raw))
		})
	}
}
