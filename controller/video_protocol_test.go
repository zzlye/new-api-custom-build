package controller

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/QuantumNous/new-api/setting/video_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 覆盖队列领取、选渠道、202 提交、冻结规则、状态轮询、文件归档与前端下载的完整链路。
func TestConfiguredVideoAsyncLifecycle(t *testing.T) {
	// 仅测试中允许下载本地 HTTP 替身，生产下载校验保持原样。
	fetchSettings := system_setting.GetFetchSetting()
	previousProtection := fetchSettings.EnableSSRFProtection
	fetchSettings.EnableSSRFProtection = false
	t.Cleanup(func() { fetchSettings.EnableSSRFProtection = previousProtection })
	var submits, polls, downloads atomic.Int32
	video := []byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'm', 'p', '4', '2', 0, 0, 0, 0, 'm', 'p', '4', '2', 'i', 's', 'o', 'm'}
	user, token := prepareAsyncCompatRelay(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "fixture-upstream", r.Header.Get("X-API-Key"))
		switch r.URL.Path {
		case "/custom/create":
			submits.Add(1)
			data, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			assert.JSONEq(t, `{"model":"future-video","prompt":"镜头前进","seconds":"7","generate_audio":false,"input_reference":{"image_url":"https://cdn.test/image.png"},"audio_urls":["https://cdn.test/audio.wav"],"video_urls":["https://cdn.test/video.mp4"]}`, string(data))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"data":{"job":"provider-42"}}`))
		case "/custom/jobs/provider-42":
			polls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"state":"DONE"}}`))
		case "/custom/jobs/provider-42/content":
			downloads.Add(1)
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write(video)
		default:
			t.Errorf("意外请求：%s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}), "future-video")
	var channel model.Channel
	require.NoError(t, model.DB.First(&channel).Error)
	p := video_setting.Presets()["reference_object"]
	p.Enabled = true
	p.SubmitPath = "/custom/create"
	p.PollPath = "/custom/jobs/{id}"
	p.ContentPath = "/custom/jobs/{id}/content"
	p.AuthName = "X-API-Key"
	p.AuthPrefix = ""
	p.Response.ID = "data.job"
	p.Response.Status = "data.state"
	data, err := common.Marshal(p)
	require.NoError(t, err)
	require.NoError(t, model.UpdateOption(video_setting.Key(channel.Id), string(data)))
	response, done, _ := beginAsyncCompatRequest(t, user, token, "/v1/videos?async=true", `{"model":"future-video","prompt":"镜头前进","duration":7,"generate_audio":false,"image_urls":["https://cdn.test/image.png"],"audio_urls":["https://cdn.test/audio.wav"],"video_urls":["https://cdn.test/video.mp4"]}`, relaytypes.RelayFormatTask)
	<-done
	require.Equal(t, http.StatusAccepted, response.Code)
	var parent model.AsyncRelayTask
	require.NoError(t, model.DB.First(&parent).Error)
	_, err = ProcessAsyncRelayTasks(context.Background(), 1)
	require.NoError(t, err)
	var child model.Task
	require.NoError(t, model.DB.Where("platform = ?", video_setting.Platform).First(&child).Error)
	require.NotNil(t, child.PrivateData.VideoProtocol)
	// 修改当前渠道配置后，已经提交的任务仍使用保存的查询和下载路径。
	p.PollPath = "/new-route/{id}"
	data, err = common.Marshal(p)
	require.NoError(t, err)
	require.NoError(t, model.UpdateOption(video_setting.Key(channel.Id), string(data)))
	previous := service.GetTaskAdaptorFunc
	service.GetTaskAdaptorFunc = func(platform constant.TaskPlatform) service.TaskPollingAdaptor { return relay.GetTaskAdaptor(platform) }
	t.Cleanup(func() { service.GetTaskAdaptorFunc = previous })
	upstreamID := child.GetUpstreamTaskID()
	require.NoError(t, service.UpdateVideoTasks(context.Background(), video_setting.Platform, map[int][]string{channel.Id: {upstreamID}}, map[string]*model.Task{upstreamID: &child}))
	require.NoError(t, model.DB.First(&child, child.ID).Error)
	require.EqualValues(t, model.TaskStatusSuccess, child.Status)
	require.NoError(t, model.DB.Model(&parent).Update("updated_at", common.GetTimestamp()-10).Error)
	_, err = ProcessAsyncRelayTasks(context.Background(), 1)
	require.NoError(t, err)
	require.NoError(t, model.DB.First(&parent, parent.ID).Error)
	require.Equal(t, model.AsyncRelayTaskStatusSucceeded, parent.Status, parent.Error)
	result := asyncControllerRequest(GetAsyncRelayTask, http.MethodGet, "/v1/tasks/"+parent.TaskID, user.Id, 1, gin.Params{{Key: "task_id", Value: parent.TaskID}}, "")
	assert.Contains(t, result.Body.String(), `"kind":"video"`)
	file := asyncControllerRequest(GetAsyncRelayMedia, http.MethodGet, "/v1/tasks/"+parent.TaskID+"/media/0", user.Id, 1, gin.Params{{Key: "task_id", Value: parent.TaskID}, {Key: "index", Value: "0"}}, "")
	assert.Equal(t, http.StatusOK, file.Code)
	assert.Equal(t, video, file.Body.Bytes())
	assert.EqualValues(t, 1, submits.Load())
	assert.EqualValues(t, 1, polls.Load())
	assert.EqualValues(t, 1, downloads.Load())
}

func TestVideoProtocolAuthenticationUsesServerRole(t *testing.T) {
	prepareAsyncMediaController(t)
	engine := gin.New()
	engine.GET("/api/video-protocol/", middleware.RootAuth(), ListVideoProtocols)
	for _, role := range []int{common.RoleCommonUser, common.RoleAdminUser, common.RoleRootUser} {
		token := fmt.Sprintf("video-root-access-%d", role)
		user := model.User{Username: fmt.Sprintf("role-%d", role), Password: "fixture", Role: role, Status: common.UserStatusEnabled, Group: "default", AccessToken: &token, AuthVersion: 1, AffCode: fmt.Sprintf("video-role-%d", role)}
		require.NoError(t, model.DB.Create(&user).Error)
		request := httptest.NewRequest(http.MethodGet, "/api/video-protocol/", nil)
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("role", "100")
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		if role == common.RoleRootUser {
			assert.Equal(t, http.StatusOK, response.Code)
		} else {
			assert.Equal(t, http.StatusForbidden, response.Code)
		}
	}
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/video-protocol/", nil))
	assert.Equal(t, http.StatusUnauthorized, response.Code)
}

func TestVideoProtocolRootAccessAndPersistence(t *testing.T) {
	prepareAsyncMediaController(t)
	empty := asyncControllerRequest(ListVideoProtocols, http.MethodGet, "/api/video-protocol/", 31, common.RoleRootUser, nil, "")
	assert.Contains(t, empty.Body.String(), `"channels":[]`)
	require.NoError(t, model.DB.Create(&model.Channel{Id: 8, Name: "视频渠道", Key: "secret-not-returned"}).Error)
	params := gin.Params{{Key: "id", Value: "8"}}
	p := video_setting.Presets()["reference_object"]
	p.Enabled = true
	data, err := common.Marshal(p)
	require.NoError(t, err)
	for _, role := range []int{0, common.RoleCommonUser, common.RoleAdminUser} {
		for _, handler := range []gin.HandlerFunc{ListVideoProtocols, GetVideoProtocol, SaveVideoProtocol} {
			response := asyncControllerRequest(handler, http.MethodPut, "/api/video-protocol/8", 31, role, params, string(data))
			assert.Equal(t, http.StatusForbidden, response.Code)
		}
	}
	response := asyncControllerRequest(SaveVideoProtocol, http.MethodPut, "/api/video-protocol/8", 31, common.RoleRootUser, params, string(data))
	require.Equal(t, http.StatusOK, response.Code)
	require.Contains(t, response.Body.String(), `"success":true`)
	var stored model.Option
	require.NoError(t, model.DB.First(&stored, "key = ?", video_setting.Key(8)).Error)
	assert.JSONEq(t, string(data), stored.Value)
	loaded, err := video_setting.Load(8)
	require.NoError(t, err)
	assert.Equal(t, &p, loaded)
	response = asyncControllerRequest(GetVideoProtocol, http.MethodGet, "/api/video-protocol/8", 31, 100, params, "")
	assert.Contains(t, response.Body.String(), `"enabled":true`)
	response = asyncControllerRequest(ListVideoProtocols, http.MethodGet, "/api/video-protocol/", 31, 100, nil, "")
	assert.NotContains(t, response.Body.String(), "secret-not-returned")
	// 通用设置接口也不能绕过专用协议校验。
	response = asyncControllerRequest(UpdateOption, http.MethodPut, "/api/option/", 31, 100, nil, `{"key":"VideoProtocol.8","value":"{}"}`)
	assert.Equal(t, http.StatusBadRequest, response.Code)
	p.PollPath = "https://other.test/tasks"
	data, err = common.Marshal(p)
	require.NoError(t, err)
	response = asyncControllerRequest(SaveVideoProtocol, http.MethodPut, "/api/video-protocol/8", 31, 100, params, string(data))
	assert.Equal(t, http.StatusBadRequest, response.Code)
	loaded, err = video_setting.Load(8)
	require.NoError(t, err)
	assert.Equal(t, "/v1/videos/{id}", loaded.PollPath)
}
