package controller

import (
	"context"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/video_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestVideoAdaptersRootPreviewPublishAndVersion(t *testing.T) {
	prepareAsyncMediaController(t)
	for _, h := range []gin.HandlerFunc{GetVideoAdapters, SaveVideoAdapters, PreviewVideoAdapter} {
		for _, role := range []int{common.RoleCommonUser, common.RoleAdminUser} {
			r := asyncControllerRequest(h, "POST", "/api/video-adapters", 31, role, nil, `{}`)
			assert.Equal(t, 403, r.Code)
		}
	}
	require.NoError(t, model.DB.Create(&model.Channel{Id: 1, Name: "测试", Key: "private-secret"}).Error)
	r, err := video_setting.LoadRegistry()
	require.NoError(t, err)
	r.Rules = []video_setting.Rule{{ID: "new", Enabled: true, ChannelIDs: []int{1}, Models: []string{"new-video"}, TemplateID: "seedance-2-mini"}}
	data, err := common.Marshal(r)
	require.NoError(t, err)
	response := asyncControllerRequest(SaveVideoAdapters, "PUT", "/api/video-adapters", 31, common.RoleRootUser, nil, string(data))
	require.Equal(t, 200, response.Code)
	assert.Contains(t, response.Body.String(), `"version":1`)
	response = asyncControllerRequest(SaveVideoAdapters, "PUT", "/api/video-adapters", 31, common.RoleRootUser, nil, string(data))
	assert.Equal(t, 409, response.Code)
	p := video_setting.BuiltinTemplates()["seedance-2-mini"].Protocol
	preview, err := common.Marshal(map[string]any{"protocol": p, "input": map[string]any{"model": "new-video", "prompt": "测试", "duration": 5, "generate_audio": false}, "response": map[string]any{"id": "upstream", "status": "completed", "url": "https://cdn.test/v.mp4"}})
	require.NoError(t, err)
	response = asyncControllerRequest(PreviewVideoAdapter, "POST", "/api/video-adapters/preview", 31, common.RoleRootUser, nil, string(preview))
	assert.Contains(t, response.Body.String(), `"sound_effects":false`)
	assert.Contains(t, response.Body.String(), `"status":"completed"`)
	response = asyncControllerRequest(GetVideoAdapters, "GET", "/api/video-adapters", 31, common.RoleRootUser, nil, "")
	assert.NotContains(t, response.Body.String(), "private-secret")
}

func TestVideoCapabilitiesExposeOnlyEditableDefinitions(t *testing.T) {
	prepareAsyncCompatRelay(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("能力查询不应生成视频") }), "future")
	template := video_setting.BuiltinTemplates()["seedance-2-mini"]
	template.Protocol.Capabilities.Parameters = []video_setting.Parameter{
		{Key: "vendor_option", Type: "string", Default: "internal-value"},
		{Key: "seed", Label: "种子", Type: "integer", Editable: true, Default: 0},
	}
	template.Protocol.Fields = append(template.Protocol.Fields, video_setting.Field{Source: "extra_parameters", Target: "options", Format: "identity"})
	r := video_setting.Registry{Templates: []video_setting.Template{template}, Rules: []video_setting.Rule{{ID: "future", Enabled: true, ChannelIDs: []int{1}, Models: []string{"future"}, TemplateID: template.ID}}}
	require.NoError(t, model.SaveVideoAdapters(&r))
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/v1/video/capabilities?model=future", nil)
	common.SetContextKey(c, constant.ContextKeyUserGroup, "default")
	GetVideoCapabilities(c)
	require.Equal(t, 200, w.Code)
	assert.Contains(t, w.Body.String(), `"key":"seed"`)
	assert.NotContains(t, w.Body.String(), "vendor_option")
	assert.NotContains(t, w.Body.String(), "internal-value")
	assert.NotContains(t, w.Body.String(), "submit_path")
}

// 新模型、新字段经过真实鉴权、队列与转换，排队后改模板不影响原请求及计费秒数。
func TestVideoAdapterPublishedSnapshotSurvivesQueuedChanges(t *testing.T) {
	submits := 0
	user, token := prepareAsyncCompatRelay(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		submits++
		assert.Equal(t, "/original/create", r.URL.Path)
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		assert.JSONEq(t, `{"model":"brand-new","prompt":"镜头","seconds":5,"sound_effects":false,"settings":{"seed":0,"muted":true}}`, string(body))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(202)
		_, _ = w.Write([]byte(`{"id":"job-1"}`))
	}), "brand-new")
	var ch model.Channel
	require.NoError(t, model.DB.First(&ch).Error)
	template := video_setting.BuiltinTemplates()["seedance-2-mini"]
	template.Protocol.SubmitPath = "/original/create"
	template.Protocol.Capabilities.Parameters = []video_setting.Parameter{{Key: "seed", Label: "种子", Type: "integer", Editable: true}}
	template.Protocol.Fields = append(template.Protocol.Fields, video_setting.Field{Source: "extra_parameters.seed", Target: "settings.seed", Format: "number"}, video_setting.Field{Source: "generate_audio", Target: "settings.muted", Format: "not"})
	registry := video_setting.Registry{Templates: []video_setting.Template{template}, Rules: []video_setting.Rule{{ID: "r", Enabled: true, ChannelIDs: []int{ch.Id}, Models: []string{"brand-new"}, TemplateID: template.ID}}}
	require.NoError(t, model.SaveVideoAdapters(&registry))
	engine := gin.New()
	engine.POST("/v1/videos", middleware.TokenAuth(), middleware.Distribute(), RelayTask)
	request := httptest.NewRequest("POST", "/v1/videos", strings.NewReader(`{"model":"brand-new","prompt":"镜头","duration":5,"generate_audio":false,"extra_parameters":{"seed":0}}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer sk-"+token.Key)
	request.Header.Set("Prefer", "respond-async")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	require.Equal(t, 202, response.Code, response.Body.String())
	var parent model.AsyncRelayTask
	require.NoError(t, model.DB.First(&parent).Error)
	assert.Contains(t, parent.RequestMetadata, "/original/create")
	registry.Templates[0].Protocol.SubmitPath = "/changed/create"
	require.NoError(t, model.SaveVideoAdapters(&registry))
	_, err := ProcessAsyncRelayTasks(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, 1, submits)
	var child model.Task
	require.NoError(t, model.DB.Where("platform = ?", video_setting.Platform).First(&child).Error)
	require.NotNil(t, child.PrivateData.VideoProtocol)
	assert.Equal(t, "/original/create", child.PrivateData.VideoProtocol.SubmitPath)
	require.NotNil(t, child.PrivateData.VideoInput)
	assert.EqualValues(t, 5, (*child.PrivateData.VideoInput)["duration"])
	before := user.Quota
	require.NoError(t, model.DB.First(user, user.Id).Error)
	assert.Less(t, user.Quota, before)
	// 能力响应只公开参数，不带渠道地址和模板内部映射。
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/v1/video/capabilities?model=brand-new", nil)
	common.SetContextKey(c, constant.ContextKeyUserGroup, "default")
	candidates, err := service.VideoCandidates(c, "brand-new")
	require.NoError(t, err)
	assert.Len(t, candidates, 1)
}
