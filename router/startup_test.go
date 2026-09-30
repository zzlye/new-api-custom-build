package router

import (
	"bytes"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 使用独立插件注册表和目录，按真实启动顺序组合路由且不污染其他测试。
func newStartupTestRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	t.Chdir(t.TempDir())
	t.Setenv("FRONTEND_BASE_URL", "")
	previousRegistry := jsplugin.DefaultRegistry
	jsplugin.DefaultRegistry = jsplugin.NewRegistry()
	t.Cleanup(func() { jsplugin.DefaultRegistry = previousRegistry })
	engine := gin.New()
	require.NotPanics(t, func() {
		SetRouter(engine, WebAssets{IndexPage: []byte("<!doctype html><title>New API</title>")})
	})
	return engine
}

// 单独路由测试捕获不到组合冲突，因此必须覆盖真正的启动入口。
func TestSetRouterRegistersAllRoutes(t *testing.T) {
	engine := newStartupTestRouter(t)
	counts := make(map[string]int)
	for _, route := range engine.Routes() {
		counts[route.Method+" "+route.Path]++
	}
	for _, route := range []string{
		"GET /api/status",
		"POST /v1/images/generations",
		"POST /v1/images/edits",
		"POST /v1/videos",
		"GET /v1/videos/:task_id",
		"POST /v1/tasks/:key",
		"GET /v1/tasks/:key",
		"GET /v1/tasks/:key/media/:index",
		"GET /v1/tasks/:key/artifacts",
		"GET /v1/tasks/:key/artifacts/:artifact_key/content",
		"HEAD /v1/tasks/:key/artifacts/:artifact_key/content",
	} {
		assert.Equal(t, 1, counts[route], route)
	}
}

// 走完整路由和真实令牌认证，验证两种任务响应、文件读取及用户隔离。
func TestUnifiedTaskRoutesPreserveContracts(t *testing.T) {
	require.NoError(t, i18n.Init())
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousRedis, previousMaster, previousPath := common.RedisEnabled, common.IsMasterNode, common.SQLitePath
	previousDatabase, previousLogDatabase := common.MainDatabaseType(), common.LogDatabaseType()
	t.Setenv("SQL_DSN", "local")
	t.Setenv("LOG_SQL_DSN", "")
	common.RedisEnabled, common.IsMasterNode = false, false
	common.SQLitePath = filepath.Join(t.TempDir(), "tasks.db")
	t.Cleanup(func() {
		if model.DB != nil && model.DB != previousDB {
			if sqlDB, err := model.DB.DB(); err == nil {
				assert.NoError(t, sqlDB.Close())
			}
		}
		model.DB, model.LOG_DB, common.RedisEnabled = previousDB, previousLogDB, previousRedis
		common.IsMasterNode, common.SQLitePath = previousMaster, previousPath
		common.SetDatabaseTypes(previousDatabase, previousLogDatabase)
	})
	// 真实初始化同时配置数据库字段转义，直接打开连接会漏掉令牌查询依赖。
	require.NoError(t, model.InitDB())
	db := model.DB
	model.LOG_DB = db
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.Task{}, &model.AsyncRelayTask{}))
	owner := model.User{Username: "route-owner", AffCode: "route-owner", Status: common.UserStatusEnabled, Role: common.RoleCommonUser, Group: "default", Quota: 1000}
	other := model.User{Username: "route-other", AffCode: "route-other", Status: common.UserStatusEnabled, Role: common.RoleCommonUser, Group: "default", Quota: 1000}
	require.NoError(t, db.Create(&owner).Error)
	require.NoError(t, db.Create(&other).Error)
	now := common.GetTimestamp()
	for _, token := range []model.Token{
		{UserId: owner.Id, Key: "routeowner", Status: common.TokenStatusEnabled, ExpiredTime: -1, UnlimitedQuota: true},
		{UserId: other.Id, Key: "routeother", Status: common.TokenStatusEnabled, ExpiredTime: -1, UnlimitedQuota: true},
		{UserId: owner.Id, Key: "routeempty", Status: common.TokenStatusEnabled, ExpiredTime: -1, RemainQuota: 0},
		{UserId: owner.Id, Key: "routeexpired", Status: common.TokenStatusEnabled, ExpiredTime: now - 60, UnlimitedQuota: true},
		{UserId: owner.Id, Key: "routedisabled", Status: common.TokenStatusDisabled, ExpiredTime: -1, UnlimitedQuota: true},
	} {
		require.NoError(t, db.Create(&token).Error)
	}
	var imageContent bytes.Buffer
	require.NoError(t, png.Encode(&imageContent, image.NewRGBA(image.Rect(0, 0, 2, 2))))
	mediaPath := filepath.Join(t.TempDir(), "generated.png")
	require.NoError(t, os.WriteFile(mediaPath, imageContent.Bytes(), 0o600))
	files, err := common.Marshal([]model.AsyncRelayMedia{{Path: mediaPath, Kind: "image", ContentType: "image/png"}})
	require.NoError(t, err)
	internal := model.AsyncRelayTask{UserID: owner.Id, Status: model.AsyncRelayTaskStatusSucceeded, FinishedAt: now, ModelName: "gpt-image-2", ResultFiles: string(files)}
	require.NoError(t, db.Create(&internal).Error)
	native := model.Task{TaskID: "task_routing_native", UserId: owner.Id, Platform: "document", Status: model.TaskStatusSuccess, Progress: "100%", SubmitTime: now - 1, FinishTime: now}
	require.NoError(t, db.Create(&native).Error)
	// 内部任务同时存在汇总日志时，也必须返回内部任务结果而非官方简略状态。
	require.NoError(t, db.Create(&model.Task{TaskID: internal.TaskID, UserId: owner.Id, Status: model.TaskStatusSuccess}).Error)
	engine := newStartupTestRouter(t)
	request := func(path, token string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, path, nil)
		if token != "" {
			r.Header.Set("Authorization", "Bearer sk-"+token)
		}
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, r)
		return w
	}
	internalPath, nativePath := "/v1/tasks/"+internal.TaskID, "/v1/tasks/"+native.TaskID
	for _, tc := range []struct {
		name, path, token string
		status            int
	}{
		{"internal_owner", internalPath, "routeowner", http.StatusOK},
		{"native_owner", nativePath, "routeowner", http.StatusOK},
		{"internal_empty_quota", internalPath, "routeempty", http.StatusOK},
		{"internal_expired_token", internalPath, "routeexpired", http.StatusOK},
		{"native_empty_quota", nativePath, "routeempty", http.StatusUnauthorized},
		{"native_expired_token", nativePath, "routeexpired", http.StatusUnauthorized},
		{"internal_foreign", internalPath, "routeother", http.StatusNotFound},
		{"native_foreign", nativePath, "routeother", http.StatusNotFound},
		{"media_foreign", internalPath + "/media/0", "routeother", http.StatusNotFound},
		{"internal_missing", "/v1/tasks/async_missing", "routeowner", http.StatusNotFound},
		{"native_missing", "/v1/tasks/task_missing", "routeowner", http.StatusNotFound},
		{"artifacts_owner", nativePath + "/artifacts", "routeowner", http.StatusOK},
		{"artifacts_foreign", nativePath + "/artifacts", "routeother", http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := request(tc.path, tc.token)
			assert.Equal(t, tc.status, w.Code, w.Body.String())
		})
	}
	for _, path := range []string{internalPath, nativePath, internalPath + "/media/0", nativePath + "/artifacts", nativePath + "/artifacts/video/content"} {
		for _, token := range []string{"", "unknown", "routedisabled"} {
			w := request(path, token)
			assert.Equal(t, http.StatusUnauthorized, w.Code, "%s: %s", path, w.Body.String())
		}
	}
	var internalResponse, nativeResponse map[string]any
	require.NoError(t, common.Unmarshal(request(internalPath, "routeowner").Body.Bytes(), &internalResponse))
	require.NoError(t, common.Unmarshal(request(nativePath, "routeowner").Body.Bytes(), &nativeResponse))
	assert.Equal(t, string(model.AsyncRelayTaskStatusSucceeded), internalResponse["status"])
	assert.Equal(t, internalPath, internalResponse["poll_url"])
	assert.Contains(t, internalResponse, "result")
	assert.Contains(t, internalResponse, "media")
	assert.Equal(t, native.TaskID, nativeResponse["task_id"])
	assert.Equal(t, "SUCCESS", nativeResponse["status"])
	assert.Equal(t, "document", nativeResponse["platform"])
	media := request(internalPath+"/media/0", "routeempty")
	require.Equal(t, http.StatusOK, media.Code, media.Body.String())
	assert.Equal(t, "image/png", media.Header().Get("Content-Type"))
	assert.Equal(t, imageContent.Bytes(), media.Body.Bytes())
	// 过期后只拒绝文件读取，任务状态和日志继续可查。
	require.NoError(t, db.Model(&internal).Update("finished_at", now-common.AsyncMediaRetentionSeconds()-1).Error)
	assert.Equal(t, http.StatusGone, request(internalPath+"/media/0", "routeowner").Code)
	assert.Equal(t, http.StatusOK, request(internalPath, "routeowner").Code)
	var logCount int64
	require.NoError(t, db.Model(&model.Task{}).Where("task_id = ?", internal.TaskID).Count(&logCount).Error)
	assert.EqualValues(t, 1, logCount)
}
