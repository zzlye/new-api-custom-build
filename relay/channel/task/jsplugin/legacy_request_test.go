package jsplugin

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	pluginruntime "github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/plugins"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 表单入口同时保留扩展字段和参考文件，不改变上传文件内容。
func TestLegacyVideoPluginPreservesMultipartFieldsAndFile(t *testing.T) {
	source, err := plugins.Source("sora")
	require.NoError(t, err)
	plugin, err := pluginruntime.NewRegistry().RegisterFactory(source, pluginruntime.Options{Key: "sora"})
	require.NoError(t, err)
	var input bytes.Buffer
	writer := multipart.NewWriter(&input)
	for key, value := range map[string]string{"model": "wan-3.0-1080p", "prompt": "测试参考帧", "duration": "8", "resolution": "1080p", "aspect_ratio": "16:9", "seed": "0", "metadata": `{"customer":"fixture"}`} {
		require.NoError(t, writer.WriteField(key, value))
	}
	file, err := writer.CreateFormFile("input_reference", "reference.png")
	require.NoError(t, err)
	_, err = io.WriteString(file, "reference-image-bytes")
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", &input)
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	t.Cleanup(func() {
		common.CleanupBodyStorage(c)
		if c.Request.MultipartForm != nil {
			_ = c.Request.MultipartForm.RemoveAll()
		}
	})
	info := &relaycommon.RelayInfo{OriginModelName: "wan-3.0-1080p", ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: "https://provider.example", UpstreamModelName: "wan-3.0-1080p"}, TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
	adaptor := New(plugin)
	adaptor.Init(info)
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	body, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPost, "/v1/videos", body)
	request.Header.Set("Content-Type", c.GetHeader("Content-Type"))
	reader, err := request.MultipartReader()
	require.NoError(t, err)
	form, err := reader.ReadForm(1 << 20)
	require.NoError(t, err)
	t.Cleanup(func() { _ = form.RemoveAll() })
	assert.Equal(t, []string{"1080p"}, form.Value["resolution"])
	assert.Equal(t, []string{"16:9"}, form.Value["aspect_ratio"])
	assert.Equal(t, []string{"8"}, form.Value["duration"])
	assert.Equal(t, []string{"0"}, form.Value["seed"])
	require.Len(t, form.Value["metadata"], 1)
	assert.JSONEq(t, `{"customer":"fixture"}`, form.Value["metadata"][0])
	require.Len(t, form.File["input_reference"], 1)
	opened, err := form.File["input_reference"][0].Open()
	require.NoError(t, err)
	defer opened.Close()
	data, err := io.ReadAll(opened)
	require.NoError(t, err)
	assert.Equal(t, "reference-image-bytes", string(data))
}

// 完整字段透传仍须经过原来的提示词和时长校验，不能绕过计费上限。
func TestLegacyVideoPluginRejectsInvalidRequests(t *testing.T) {
	source, err := plugins.Source("sora")
	require.NoError(t, err)
	plugin, err := pluginruntime.NewRegistry().RegisterFactory(source, pluginruntime.Options{Key: "sora"})
	require.NoError(t, err)
	for _, raw := range []string{`{"prompt":"","duration":8}`, `{"prompt":"测试","duration":3601}`, `{"prompt":"测试","duration":-1}`, `{"prompt":"测试","seconds":"3601"}`, `{"prompt":`, `[]`} {
		t.Run(raw, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(raw))
			c.Request.Header.Set("Content-Type", "application/json")
			t.Cleanup(func() { common.CleanupBodyStorage(c) })
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}, TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
			adaptor := New(plugin)
			adaptor.Init(info)
			taskErr := adaptor.ValidateRequestAndSetAction(c, info)
			require.NotNil(t, taskErr)
			assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
		})
	}
}

// 走真实旧入口校验和出厂插件，检查出站报文而不是入站日志快照。
func TestLegacyVideoPluginPreservesRequestFields(t *testing.T) {
	source, err := plugins.Source("sora")
	require.NoError(t, err)
	plugin, err := pluginruntime.NewRegistry().RegisterFactory(source, pluginruntime.Options{Key: "sora"})
	require.NoError(t, err)
	for _, tc := range []struct {
		name, duration, metadata string
	}{
		{"数字时长", `8`, `{"customer":"fixture"}`},
		{"字符串时长", `"8"`, `"{\"customer\":\"fixture\"}"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := `{"model":"wan-alias","prompt":"测试镜头","duration":` + tc.duration + `,"resolution":"1080p","aspect_ratio":"16:9","input_reference":{"image_url":"https://example.test/ref.png","role":"first_frame"},"metadata":` + tc.metadata + `,"generate_audio":false,"seed":0,"provider_options":{"items":[0,false,null],"strength":0.5}}`
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(raw))
			c.Request.Header.Set("Content-Type", "application/json")
			t.Cleanup(func() { common.CleanupBodyStorage(c) })
			info := &relaycommon.RelayInfo{OriginModelName: "wan-alias", ChannelMeta: &relaycommon.ChannelMeta{ChannelType: 1, ChannelBaseUrl: "https://provider.example", UpstreamModelName: "wan-3.0-1080p"}, TaskRelayInfo: &relaycommon.TaskRelayInfo{PublicTaskID: "fixture"}}
			adaptor := New(plugin)
			adaptor.Init(info)
			require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
			body, err := adaptor.BuildRequestBody(c, info)
			require.NoError(t, err)
			wire, err := io.ReadAll(body)
			require.NoError(t, err)
			assert.JSONEq(t, `{"model":"wan-3.0-1080p","prompt":"测试镜头","duration":8,"resolution":"1080p","aspect_ratio":"16:9","input_reference":{"image_url":"https://example.test/ref.png","role":"first_frame"},"metadata":{"customer":"fixture"},"generate_audio":false,"seed":0,"provider_options":{"items":[0,false,null],"strength":0.5}}`, string(wire))
			ratios, err := adaptor.EstimateBillingValidated(c, info)
			require.NoError(t, err)
			assert.Equal(t, float64(8), ratios["seconds"])
			// 构建出站请求后仍能复读原文，异步存档与后续处理不受影响。
			storage, err := common.GetBodyStorage(c)
			require.NoError(t, err)
			original, err := storage.Bytes()
			require.NoError(t, err)
			assert.Equal(t, raw, string(original))
		})
	}
}

// 已完成协议解码的请求必须保持解码结果，不能重新注入原始字段。
func TestLegacyVideoPluginDoesNotOverwriteDecodedRequest(t *testing.T) {
	source, err := plugins.Source("sora")
	require.NoError(t, err)
	plugin, err := pluginruntime.NewRegistry().RegisterFactory(source, pluginruntime.Options{Key: "sora"})
	require.NoError(t, err)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{"prompt":"原始","resolution":"1080p","discarded":true}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("task_request", map[string]any{"prompt": "已解码", "resolution": "720p", "seconds": 4})
	info := &relaycommon.RelayInfo{OriginModelName: "sora-2", ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: "https://provider.example", UpstreamModelName: "sora-2"}, TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
	adaptor := New(plugin)
	adaptor.Init(info)
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	body, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	wire, err := io.ReadAll(body)
	require.NoError(t, err)
	assert.JSONEq(t, `{"model":"sora-2","prompt":"已解码","resolution":"720p","seconds":4}`, string(wire))
}
