package sora

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 同时经过入口校验和上游请求组装，防止只测 DTO 而遗漏原始请求的对象形状。
func TestVideoReferenceValidationAndForwarding(t *testing.T) {
	for _, source := range []string{"data:image/png;base64,cmVm", "https://example.test/reference.png"} {
		for _, object := range []bool{false, true} {
			var reference any = source
			if object {
				reference = map[string]any{"image_url": source}
			}
			payload := map[string]any{
				"model": "future-video", "prompt": "镜头平移", "duration": 5,
				"input_reference": reference, "generate_audio": false,
				"audio_urls": []any{"https://example.test/music.wav"},
				"video_urls": []any{"https://example.test/reference.mp4"},
			}
			encoded, err := common.Marshal(payload)
			require.NoError(t, err)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader(encoded))
			c.Request.Header.Set("Content-Type", "application/json")
			defer common.CleanupBodyStorage(c)
			info := &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{}, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "future-video"}}
			require.Nil(t, (&TaskAdaptor{}).ValidateRequestAndSetAction(c, info))
			req, err := relaycommon.GetTaskRequest(c)
			require.NoError(t, err)
			assert.Equal(t, []string{source}, req.Images)
			assert.Equal(t, constant.TaskActionGenerate, info.Action)
			body, err := (&TaskAdaptor{}).BuildRequestBody(c, info)
			require.NoError(t, err)
			data, err := io.ReadAll(body)
			require.NoError(t, err)
			var forwarded map[string]any
			require.NoError(t, common.Unmarshal(data, &forwarded))
			assert.Equal(t, reference, forwarded["input_reference"])
			assert.Equal(t, payload["audio_urls"], forwarded["audio_urls"])
			assert.Equal(t, payload["video_urls"], forwarded["video_urls"])
			assert.Equal(t, false, forwarded["generate_audio"])
		}
	}
}

func TestVideoReferenceRejectsMalformedObjects(t *testing.T) {
	for _, reference := range []string{`{}`, `{"image_url":123}`, `{"image_url":" "}`, `[]`, `false`} {
		var req relaycommon.TaskSubmitReq
		assert.Error(t, common.Unmarshal([]byte(`{"input_reference":`+reference+`}`), &req))
	}
}
