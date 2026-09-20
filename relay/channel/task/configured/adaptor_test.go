package configured

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/video_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 使用真实入口校验、请求构建和 HTTP 查询，覆盖协议切换而不发起付费生成。
func TestConfiguredVideoRequestAndPolling(t *testing.T) {
	for _, encoding := range []string{"json", "form", "multipart"} {
		t.Run(encoding, func(t *testing.T) {
			p := video_setting.Presets()["reference_object"]
			p.Enabled = true
			p.Encoding = encoding
			p.PollMethod = "POST"
			p.PollPath = "/jobs/query"
			p.PollIDField = "input.id"
			p.AuthName = "X-Api-Key"
			p.AuthPrefix = ""
			p.Response.ID = "output.job"
			p.Response.Status = "output.state"
			p.Response.URL = "output.files.0.url"
			p.ContentPath = ""
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/jobs/query", r.URL.Path)
				assert.Equal(t, "selected-channel-key", r.Header.Get("X-Api-Key"))
				data, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				assert.JSONEq(t, `{"input":{"id":"job/42"}}`, string(data))
				_, _ = w.Write([]byte(`{"output":{"state":"SUCCEEDED","files":[{"url":"https://cdn.test/result.mp4"}]}}`))
			}))
			defer server.Close()
			info := &relaycommon.RelayInfo{VideoProtocol: &p, TaskRelayInfo: &relaycommon.TaskRelayInfo{PublicTaskID: "task-public"}, ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: server.URL, ApiKey: "selected-channel-key", UpstreamModelName: "arbitrary-future-model"}}
			a := &TaskAdaptor{}
			a.Init(info)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/videos", bytes.NewBufferString(`{"model":"future-model","prompt":"镜头前进","duration":7,"generate_audio":false,"image_urls":["data:image/png;base64,aW1n"],"audio_urls":["data:audio/wav;base64,YQ=="],"video_urls":["data:video/mp4;base64,dg=="]}`))
			c.Request.Header.Set("Content-Type", "application/json")
			defer common.CleanupBodyStorage(c)
			require.Nil(t, a.ValidateRequestAndSetAction(c, info))
			body, err := a.BuildRequestBody(c, info)
			require.NoError(t, err)
			request, _ := http.NewRequest("POST", server.URL, body)
			require.NoError(t, a.BuildRequestHeader(c, request, info))
			if encoding == "json" {
				data, err := io.ReadAll(body)
				require.NoError(t, err)
				assert.JSONEq(t, `{"model":"arbitrary-future-model","prompt":"镜头前进","seconds":"7","generate_audio":false,"input_reference":{"image_url":"data:image/png;base64,aW1n"},"audio_urls":["data:audio/wav;base64,YQ=="],"video_urls":["data:video/mp4;base64,dg=="]}`, string(data))
			} else {
				if encoding == "multipart" {
					require.NoError(t, request.ParseMultipartForm(1<<20))
				} else {
					require.NoError(t, request.ParseForm())
				}
				assert.Equal(t, "7", request.FormValue("seconds"))
				assert.Equal(t, "false", request.FormValue("generate_audio"))
				assert.JSONEq(t, `{"image_url":"data:image/png;base64,aW1n"}`, request.FormValue("input_reference"))
			}
			response := &http.Response{StatusCode: 202, Body: io.NopCloser(bytes.NewBufferString(`{"output":{"job":"job/42"}}`))}
			id, _, taskErr := a.DoResponse(c, response, info)
			require.Nil(t, taskErr)
			assert.Equal(t, "job/42", id)
			poll, err := a.FetchTask(server.URL, "selected-channel-key", map[string]any{"task_id": id}, "")
			require.NoError(t, err)
			defer poll.Body.Close()
			data, err := io.ReadAll(poll.Body)
			require.NoError(t, err)
			result, err := a.ParseTaskResult(data)
			require.NoError(t, err)
			assert.Equal(t, string(model.TaskStatusSuccess), result.Status)
			assert.Equal(t, "https://cdn.test/result.mp4", result.Url)
		})
	}
}

func TestConfiguredVideoDoesNotLoseReferencesOrAcceptUnboundedDuration(t *testing.T) {
	p := video_setting.Presets()["reference_object"]
	_, err := p.MapRequest(map[string]any{"image_urls": []string{"first", "second"}})
	require.ErrorContains(t, err, "实际收到 2 个")
	p.Fields = p.Fields[:len(p.Fields)-1]
	_, err = p.MapRequest(map[string]any{"video_urls": []string{"https://cdn.test/input.mp4"}})
	require.ErrorContains(t, err, "未配置 video_urls")
	p = video_setting.Presets()["standard"]
	info := &relaycommon.RelayInfo{VideoProtocol: &p, TaskRelayInfo: &relaycommon.TaskRelayInfo{}, ChannelMeta: &relaycommon.ChannelMeta{}}
	a := &TaskAdaptor{}
	a.Init(info)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/videos", bytes.NewBufferString(`{"model":"future","prompt":"测试","duration":2147483647}`))
	c.Request.Header.Set("Content-Type", "application/json")
	defer common.CleanupBodyStorage(c)
	require.NotNil(t, a.ValidateRequestAndSetAction(c, info))
}

func TestConfiguredPollingRetainsTaskOnTransportAndUnknownStatus(t *testing.T) {
	p := video_setting.Presets()["standard"]
	a := &TaskAdaptor{protocol: &p}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"busy"}}`))
	}))
	defer server.Close()
	_, err := a.FetchTask(server.URL, "key", map[string]any{"task_id": "existing"}, "")
	require.ErrorContains(t, err, "429")
	_, err = a.ParseTaskResult([]byte(`{"status":"new-status"}`))
	require.ErrorContains(t, err, "未配置")
	result, err := a.ParseTaskResult([]byte(`{"status":"failed","url":"https://cdn.test/old.mp4","error":{"message":"上游明确失败"}}`))
	require.NoError(t, err)
	assert.Equal(t, string(model.TaskStatusFailure), result.Status)
	assert.Empty(t, result.Url)
	assert.Equal(t, "上游明确失败", result.Reason)
}

func TestConfiguredTaskSnapshotSurvivesRuleChanges(t *testing.T) {
	p := video_setting.Presets()["reference_object"]
	info := &relaycommon.RelayInfo{VideoProtocol: &p, TaskRelayInfo: &relaycommon.TaskRelayInfo{PublicTaskID: "task-snapshot"}, ChannelMeta: &relaycommon.ChannelMeta{ApiKey: "selected-key"}}
	task := model.InitTask(video_setting.Platform, info)
	serialized, err := task.PrivateData.Value()
	require.NoError(t, err)
	var restored model.TaskPrivateData
	require.NoError(t, restored.Scan(serialized))
	p.Fields[6].Target = "changed_reference"
	a := &TaskAdaptor{}
	a.SetVideoProtocol(restored.VideoProtocol)
	body, err := a.protocol.MapRequest(map[string]any{"image_urls": []string{"https://cdn.test/input.png"}})
	require.NoError(t, err)
	assert.JSONEq(t, `{"input_reference":{"image_url":"https://cdn.test/input.png"}}`, string(body))
	assert.Equal(t, "selected-key", restored.Key)
}
