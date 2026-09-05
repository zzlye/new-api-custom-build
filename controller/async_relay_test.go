package controller

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newAsyncRelayTestContext(path, body string) *gin.Context {
	gin.SetMode(gin.TestMode)
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = request
	return c
}

func TestShouldQueueAsyncRelay(t *testing.T) {
	cases := []struct {
		name, path, body string
		format           relaytypes.RelayFormat
		expected         bool
	}{
		{"图片默认入队", "/v1/images/generations", `{"model":"gpt-image-1","prompt":"一只猫"}`, relaytypes.RelayFormatOpenAIImage, true},
		{"显式关闭异步也仍然入队", "/v1/images/generations?async=false", `{"model":"gpt-image-1","async":false}`, relaytypes.RelayFormatOpenAIImage, true},
		{"流式生图交给后台完整生成", "/v1/images/generations", `{"model":"gpt-image-1","stream":true}`, relaytypes.RelayFormatOpenAIImage, true},
		{"视频默认入队", "/v1/videos", `{"model":"sora-2"}`, relaytypes.RelayFormatTask, true},
		{"Gemini生图默认入队", "/v1beta/models/gemini-2.5-flash-image:streamGenerateContent", `{"contents":[]}`, relaytypes.RelayFormatGemini, true},
		{"Gemini明确输出图片入队", "/v1beta/models/gemini-2.5-flash:generateContent", `{"generationConfig":{"responseModalities":["TEXT","IMAGE"]}}`, relaytypes.RelayFormatGemini, true},
		{"普通识图保持原有行为", "/v1beta/models/gemini-2.5-flash:generateContent", `{"contents":[{"parts":[{"text":"解释image-generation和banana"},{"inlineData":{"mimeType":"image/png","data":"eA=="}}]}]}`, relaytypes.RelayFormatGemini, false},
		{"Responses生图工具入队", "/v1/responses", `{"model":"gpt-4.1","tools":[{"type":"image_generation"}]}`, relaytypes.RelayFormatOpenAIResponses, true},
		{"普通对话不入队", "/v1/chat/completions", `{"model":"gpt-4.1","messages":[{"role":"user","content":"画一张图片"}]}`, relaytypes.RelayFormatOpenAI, false},
		{"音乐任务保留原有提交协议", "/suno/submit/music", `{}`, relaytypes.RelayFormatTask, false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			c := newAsyncRelayTestContext(test.path, test.body)
			defer common.CleanupBodyStorage(c)
			queued, err := ShouldQueueAsyncRelay(c, test.format)
			require.NoError(t, err)
			assert.Equal(t, test.expected, queued)
		})
	}
}

func TestAsyncRelayWorkerMarkerIsServerOnly(t *testing.T) {
	c := newAsyncRelayTestContext("/v1/images/generations", `{}`)
	c.Request.Header.Set("X-New-Api-Async-Worker", "true")
	queued, err := ShouldQueueAsyncRelay(c, relaytypes.RelayFormatOpenAIImage)
	require.NoError(t, err)
	assert.True(t, queued)
	c.Set(model.AsyncRelayContextKey, "async_internal")
	queued, err = ShouldQueueAsyncRelay(c, relaytypes.RelayFormatOpenAIImage)
	require.NoError(t, err)
	assert.False(t, queued)
}

func TestAsyncRelayMultipartStreamIsQueued(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", "gpt-image-1"))
	require.NoError(t, writer.WriteField("stream", "true"))
	require.NoError(t, writer.Close())
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", &body)
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	queued, err := ShouldQueueAsyncRelay(c, relaytypes.RelayFormatOpenAIImage)
	require.NoError(t, err)
	assert.True(t, queued)
}

// 原接口字段保留测试同时保护计费数值、透明背景和客户端选择的流式协议。
func TestAsyncRelayEnqueuePreservesOriginalRequestProtocol(t *testing.T) {
	cases := []struct {
		name, path, body, savedQuery string
		format                       relaytypes.RelayFormat
	}{
		{"图片透明背景和大整数", "/v1/images/generations?async=true&key=secret", `{"model":"gpt-image-1","n":18446744073709551615,"async":false,"background":"transparent","stream":true}`, "", relaytypes.RelayFormatOpenAIImage},
		{"Gemini流式地址和查询参数", "/v1beta/models/gemini-2.5-flash-image:streamGenerateContent?async=true&alt=sse&key=secret", `{"contents":[],"generationConfig":{"responseModalities":["IMAGE"]}}`, "alt=sse", relaytypes.RelayFormatGemini},
		{"Responses原生后台参数", "/v1/responses?async=true", `{"model":"gpt-4.1","tools":[{"type":"image_generation"}],"background":true,"stream":true}`, "", relaytypes.RelayFormatOpenAIResponses},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			prepareAsyncMediaController(t)
			c := newAsyncRelayTestContext(test.path, test.body)
			defer common.CleanupBodyStorage(c)
			c.Request.Header.Set("Accept", "text/event-stream")
			c.Set("id", 31)
			c.Set("token_id", 11)
			require.NoError(t, EnqueueAsyncRelayRequest(c, test.format))
			assert.Equal(t, http.StatusAccepted, c.Writer.Status())
			var task model.AsyncRelayTask
			require.NoError(t, model.DB.First(&task).Error)
			saved, err := os.ReadFile(task.RequestFilePath)
			require.NoError(t, err)
			assert.Equal(t, test.body, string(saved))
			assert.Equal(t, c.Request.URL.Path, task.RequestPath)
			assert.Equal(t, test.savedQuery, task.RequestQuery)
			assert.Contains(t, task.RequestFiles, "text/event-stream")
			assert.NotContains(t, task.RequestQuery, "secret")
		})
	}
}
