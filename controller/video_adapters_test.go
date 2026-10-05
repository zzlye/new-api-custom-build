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

// 导入先生成草稿，校验、渠道重绑和冲突处理均不发布线上配置。
func TestVideoAdapterImportPreview(t *testing.T) {
	prepareAsyncMediaController(t)
	require.NoError(t, model.DB.Create(&model.Channel{Id: 9, Name: "导入渠道", Key: "private-secret"}).Error)
	for _, handler := range []gin.HandlerFunc{PreviewVideoAdapterImport, DownloadVideoAdapterTemplate, DownloadVideoAdapterGuide} {
		for _, role := range []int{common.RoleCommonUser, common.RoleAdminUser} {
			response := asyncControllerRequest(handler, "POST", "/api/video-adapters/import-preview", 31, role, nil, `{}`)
			assert.Equal(t, 403, response.Code)
		}
	}
	base, err := video_setting.LoadRegistry()
	require.NoError(t, err)
	var bundle map[string]any
	require.NoError(t, common.Unmarshal(video_setting.AdapterImportTemplate, &bundle))
	body := map[string]any{"registry": base, "bundle": bundle, "channel_ids": []int{9}}
	data, err := common.Marshal(body)
	require.NoError(t, err)
	response := asyncControllerRequest(PreviewVideoAdapterImport, "POST", "/api/video-adapters/import-preview", 31, common.RoleRootUser, nil, string(data))
	require.Equal(t, 200, response.Code, response.Body.String())
	var result struct {
		Success bool                   `json:"success"`
		Data    video_setting.Registry `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
	require.True(t, result.Success)
	assert.Equal(t, base.Version, result.Data.Version)
	require.Len(t, result.Data.Rules, len(base.Rules)+1)
	assert.Equal(t, []int{9}, result.Data.Rules[len(base.Rules)].ChannelIDs)
	current, err := video_setting.LoadRegistry()
	require.NoError(t, err)
	assert.Equal(t, base, current)
	assert.NotContains(t, response.Body.String(), "private-secret")

	// 下载内容与实际导入协议一致，不只返回一份不可用的说明。
	download := asyncControllerRequest(DownloadVideoAdapterTemplate, "GET", "/api/video-adapters/import-template", 31, common.RoleRootUser, nil, "")
	assert.Equal(t, 200, download.Code)
	assert.JSONEq(t, string(video_setting.AdapterImportTemplate), download.Body.String())
	guide := asyncControllerRequest(DownloadVideoAdapterGuide, "GET", "/api/video-adapters/import-guide", 31, common.RoleRootUser, nil, "")
	assert.Contains(t, guide.Body.String(), "image_urls")
	assert.Contains(t, guide.Body.String(), "AI")

	// 同一批再次导入必须报冲突，不覆盖原规则，也不悄悄停用任何模型。
	body["registry"] = result.Data
	data, err = common.Marshal(body)
	require.NoError(t, err)
	response = asyncControllerRequest(PreviewVideoAdapterImport, "POST", "/api/video-adapters/import-preview", 31, common.RoleRootUser, nil, string(data))
	assert.Equal(t, 400, response.Code)
	body["registry"] = base
	body["channel_ids"] = []int{999999}
	data, err = common.Marshal(body)
	require.NoError(t, err)
	response = asyncControllerRequest(PreviewVideoAdapterImport, "POST", "/api/video-adapters/import-preview", 31, common.RoleRootUser, nil, string(data))
	assert.Equal(t, 400, response.Code)
	assert.Contains(t, response.Body.String(), "不存在")

	// 显式空数组仍可导入渠道默认规则，且响应保持数组，供页面直接预览。
	bundle["rules"].([]any)[0].(map[string]any)["models"] = []any{}
	body["channel_ids"] = []int{9}
	data, err = common.Marshal(body)
	require.NoError(t, err)
	response = asyncControllerRequest(PreviewVideoAdapterImport, "POST", "/api/video-adapters/import-preview", 31, common.RoleRootUser, nil, string(data))
	require.Equal(t, 200, response.Code, response.Body.String())
	assert.Contains(t, response.Body.String(), `"models":[]`)
}

// 导入边界统一走真实处理器，错误文件不会落库或调用上游。
func TestVideoAdapterImportInvalidConfiguration(t *testing.T) {
	prepareAsyncMediaController(t)
	require.NoError(t, model.DB.Create(&model.Channel{Id: 9, Name: "导入渠道"}).Error)
	for _, tc := range []struct {
		name     string
		change   func(map[string]any)
		expected string
	}{
		{"格式版本不支持", func(bundle map[string]any) { bundle["format_version"] = 99 }, "版本不支持"},
		{"未知配置属性", func(bundle map[string]any) { bundle["auto_detect"] = true }, "unknown field"},
		{"模板为空", func(bundle map[string]any) { bundle["templates"] = []any{} }, "1 到 512"},
		{"左侧误填上游字段", func(bundle map[string]any) {
			protocol := bundle["templates"].([]any)[0].(map[string]any)["protocol"].(map[string]any)
			protocol["fields"].([]any)[5].(map[string]any)["source"] = "image_refs"
		}, "未知统一输入字段 image_refs"},
		{"对象包装缺少字段", func(bundle map[string]any) {
			protocol := bundle["templates"].([]any)[0].(map[string]any)["protocol"].(map[string]any)
			protocol["fields"].([]any)[5].(map[string]any)["format"] = "objects"
		}, "对象内字段不能为空"},
		{"查询路径没有编号", func(bundle map[string]any) {
			bundle["templates"].([]any)[0].(map[string]any)["protocol"].(map[string]any)["poll_path"] = "/v1/videos"
		}, "查询路径需要包含"},
		{"模板引用丢失", func(bundle map[string]any) {
			bundle["rules"].([]any)[0].(map[string]any)["template_id"] = "missing"
		}, "模板未包含"},
		{"遗漏模型列表", func(bundle map[string]any) {
			delete(bundle["rules"].([]any)[0].(map[string]any), "models")
		}, "模型列表必须显式填写"},
		{"模型列表为 null", func(bundle map[string]any) {
			bundle["rules"].([]any)[0].(map[string]any)["models"] = nil
		}, "模型列表必须显式填写"},
		{"条件误填上游字段", func(bundle map[string]any) {
			protocol := bundle["templates"].([]any)[0].(map[string]any)["protocol"].(map[string]any)
			protocol["fields"].([]any)[5].(map[string]any)["when"] = []any{map[string]any{"source": "image_refs", "operator": "exists"}}
		}, "未知条件输入字段 image_refs"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base, err := video_setting.LoadRegistry()
			require.NoError(t, err)
			var bundle map[string]any
			require.NoError(t, common.Unmarshal(video_setting.AdapterImportTemplate, &bundle))
			tc.change(bundle)
			data, err := common.Marshal(map[string]any{"registry": base, "bundle": bundle, "channel_ids": []int{9}})
			require.NoError(t, err)
			response := asyncControllerRequest(PreviewVideoAdapterImport, "POST", "/api/video-adapters/import-preview", 31, common.RoleRootUser, nil, string(data))
			assert.Equal(t, 400, response.Code, response.Body.String())
			assert.Contains(t, response.Body.String(), tc.expected)
			after, err := video_setting.LoadRegistry()
			require.NoError(t, err)
			assert.Equal(t, base, after)
		})
	}
	response := asyncControllerRequest(PreviewVideoAdapterImport, "POST", "/api/video-adapters/import-preview", 31, common.RoleRootUser, nil, `{} {}`)
	assert.Equal(t, 400, response.Code)
	response = asyncControllerRequest(PreviewVideoAdapterImport, "POST", "/api/video-adapters/import-preview", 31, common.RoleRootUser, nil, `{"bundle":{"format":"`+strings.Repeat("x", 4<<20)+`"}}`)
	assert.Equal(t, 400, response.Code)
}

// 模板编号冲突只重命名导入副本，转换后仍保留音频关闭和种子零值。
func TestVideoAdapterImportPreservesDraftAndValues(t *testing.T) {
	var bundle video_setting.ImportBundle
	require.NoError(t, common.Unmarshal(video_setting.AdapterImportTemplate, &bundle))
	p := &bundle.Templates[0].Protocol
	supported := true
	p.Capabilities.GenerateAudio = &supported
	p.Capabilities.Parameters = []video_setting.Parameter{{Key: "seed", Label: "种子", Type: "integer", Editable: true}}
	p.Fields = append(p.Fields, video_setting.Field{Source: "generate_audio", Target: "sound", Format: "identity"}, video_setting.Field{Source: "extra_parameters.seed", Target: "seed", Format: "identity"})
	// 已声明的自定义字段和统一素材字段可以作为条件，零值与 false 仍正常发送。
	p.Fields[len(p.Fields)-1].When = []video_setting.Condition{{Source: "extra_parameters.seed", Operator: "exists"}, {Source: "image_urls", Operator: "exists"}}
	base := video_setting.Registry{Version: 12, Templates: bundle.Templates, Rules: []video_setting.Rule{}}
	before, err := common.Marshal(base)
	require.NoError(t, err)
	next, err := video_setting.MergeImport(base, bundle, []int{9})
	require.NoError(t, err)
	require.Len(t, next.Templates, 2)
	assert.Equal(t, base.Templates[0], next.Templates[0])
	assert.NotEqual(t, next.Templates[0].ID, next.Templates[1].ID)
	assert.Equal(t, next.Templates[1].ID, next.Rules[0].TemplateID)
	after, err := common.Marshal(base)
	require.NoError(t, err)
	assert.JSONEq(t, string(before), string(after))
	resolved, err := next.Resolve(9, "sd-2.5-ch1")
	require.NoError(t, err)
	require.NotNil(t, resolved)
	input, err := resolved.Normalize(map[string]any{"model": "sd-2.5-ch1", "prompt": "镜头", "duration": 4, "generate_audio": false, "extra_parameters": map[string]any{"seed": 0}, "image_urls": []any{"https://example.com/one.png", "https://example.com/two.png"}})
	require.NoError(t, err)
	output, err := resolved.MapRequest(input)
	require.NoError(t, err)
	assert.JSONEq(t, `{"model":"sd-2.5-ch1","prompt":"镜头","duration":4,"sound":false,"seed":0,"image_refs":["https://example.com/one.png","https://example.com/two.png"]}`, string(output))
	_, err = video_setting.MergeImport(base, bundle, nil)
	require.ErrorContains(t, err, "尚未选择适用渠道")
	bundle.Rules = nil
	templatesOnly, err := video_setting.MergeImport(base, bundle, nil)
	require.NoError(t, err)
	assert.Len(t, templatesOnly.Templates, 2)
	assert.Empty(t, templatesOnly.Rules)
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
