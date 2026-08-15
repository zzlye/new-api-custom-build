package controller

import (
	"bytes"
	"mime/multipart"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newAsyncRelayTestContext(path string, body string) *gin.Context {
	gin.SetMode(gin.TestMode)
	request := httptest.NewRequest("POST", path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = request
	return context
}

func TestShouldQueueAsyncRelay(t *testing.T) {
	t.Run("图片请求通过查询参数开启", func(t *testing.T) {
		context := newAsyncRelayTestContext("/v1/images/generations?async=true", `{"model":"gpt-image-1","prompt":"一只猫"}`)
		defer common.CleanupBodyStorage(context)

		queued, err := ShouldQueueAsyncRelay(context, relaytypes.RelayFormatOpenAIImage)
		require.NoError(t, err)
		assert.True(t, queued)
	})

	t.Run("Gemini 香蕉生图通过模型名开启", func(t *testing.T) {
		context := newAsyncRelayTestContext("/v1beta/models/nano-banana-pro-preview:generateContent?background=true", `{"contents":[]}`)
		defer common.CleanupBodyStorage(context)

		queued, err := ShouldQueueAsyncRelay(context, relaytypes.RelayFormatGemini)
		require.NoError(t, err)
		assert.True(t, queued)
	})

	t.Run("普通 Gemini 文本请求保持同步", func(t *testing.T) {
		context := newAsyncRelayTestContext("/v1beta/models/gemini-2.5-flash:generateContent?async=true", `{"contents":[{"parts":[{"text":"你好"}]}]}`)
		defer common.CleanupBodyStorage(context)

		queued, err := ShouldQueueAsyncRelay(context, relaytypes.RelayFormatGemini)
		require.NoError(t, err)
		assert.False(t, queued)
	})

	t.Run("异步流式请求返回错误", func(t *testing.T) {
		context := newAsyncRelayTestContext("/v1/images/generations?async=true", `{"model":"gpt-image-1","stream":true}`)
		defer common.CleanupBodyStorage(context)

		queued, err := ShouldQueueAsyncRelay(context, relaytypes.RelayFormatOpenAIImage)
		assert.False(t, queued)
		require.EqualError(t, err, "异步图片请求不支持流式响应")
	})

	t.Run("图片编辑 multipart 流式请求返回错误", func(t *testing.T) {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		require.NoError(t, writer.WriteField("model", "gpt-image-1"))
		require.NoError(t, writer.WriteField("prompt", "一只猫"))
		require.NoError(t, writer.WriteField("stream", "true"))
		require.NoError(t, writer.Close())
		request := httptest.NewRequest("POST", "/v1/images/edits?async=true", &body)
		request.Header.Set("Content-Type", writer.FormDataContentType())
		context, _ := gin.CreateTestContext(httptest.NewRecorder())
		context.Request = request
		defer common.CleanupBodyStorage(context)

		queued, err := ShouldQueueAsyncRelay(context, relaytypes.RelayFormatOpenAIImage)
		assert.False(t, queued)
		require.EqualError(t, err, "异步图片请求不支持流式响应")
	})
}

func TestAsyncRelayRequestFlags(t *testing.T) {
	assert.True(t, isAsyncFlag("TRUE"))
	assert.True(t, isAsyncFlag("1"))
	assert.False(t, isAsyncFlag("false"))

	cleaned := stripAsyncJSONFlags([]byte(`{"model":"gpt-image-1","async":true,"background":true}`), "application/json")
	assert.JSONEq(t, `{"model":"gpt-image-1"}`, string(cleaned))
	assert.Equal(t, "model=gpt-image-1", removeAsyncQuery("async=true&model=gpt-image-1&background=1"))
}
