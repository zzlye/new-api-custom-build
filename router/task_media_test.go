package router

import (
	"bytes"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// 从实际详情接口获取地址，再经正式路由读取文件，不直接调用签名实现来构造成功条件。
func TestTaskMediaDirectURLsAndExpirationPreserveLogs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousDB := model.DB
	previousOptions := common.OptionMap
	previousDatabase := common.MainDatabaseType()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	model.DB = db
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	common.OptionMap = map[string]string{common.AsyncMediaRetentionOption: "2"}
	t.Cleanup(func() {
		model.DB = previousDB
		common.OptionMap = previousOptions
		common.SetMainDatabaseType(previousDatabase)
		require.NoError(t, sqlDB.Close())
	})
	t.Setenv("ASYNC_MEDIA_DIR", t.TempDir())
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.AsyncRelayTask{}))
	var content bytes.Buffer
	require.NoError(t, png.Encode(&content, image.NewRGBA(image.Rect(0, 0, 2, 2))))
	path, file, err := common.CreateAsyncMediaFile()
	require.NoError(t, err)
	_, err = file.Write(content.Bytes())
	require.NoError(t, err)
	require.NoError(t, file.Close())
	files, err := common.Marshal([]model.AsyncRelayMedia{{Path: path, ContentType: "image/png", Kind: "image", SourceURL: "https://upstream.example/result.png"}})
	require.NoError(t, err)
	details, err := common.Marshal(model.AsyncRelayRequestDetails{Prompt: "自动到期只清图片", References: []model.AsyncRelayReference{{Path: path, Kind: "image", ContentType: "image/png", Source: "https://upstream.example/reference.png"}}})
	require.NoError(t, err)
	task := &model.AsyncRelayTask{UserID: 31, ModelName: "gpt-image-2", RequestDetails: string(details), ResultFiles: string(files), Status: model.AsyncRelayTaskStatusSucceeded,
		FinishedAt: common.GetTimestamp() - 60, RequestMethod: http.MethodPost, RequestPath: "/v1/images/generations"}
	require.NoError(t, task.InsertWithLog("default", "IMAGE"))
	require.NoError(t, task.SyncLog(db))
	engine := gin.New()
	SetApiRouter(engine)
	// 登录身份只在测试入口设置，生产文件地址仍必须自行验证其文件级签名。
	engine.GET("/fixture-details/:task_id", func(c *gin.Context) {
		c.Set("id", 31)
		c.Set("role", common.RoleCommonUser)
		controller.GetAsyncRelayTaskDetails(c)
	})
	request := func(path string, headers ...string) *httptest.ResponseRecorder {
		t.Helper()
		response := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, path, nil)
		if len(headers) > 0 {
			r.Header.Set("Range", headers[0])
		}
		engine.ServeHTTP(response, r)
		return response
	}
	response := request("/fixture-details/" + task.TaskID)
	var payload struct {
		Data dto.AsyncTaskDetails `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &payload))
	require.Len(t, payload.Data.Media, 1)
	require.Len(t, payload.Data.References, 1)
	preview := payload.Data.Media[0].PreviewURL
	imageResponse := request(preview)
	require.Equal(t, http.StatusOK, imageResponse.Code, imageResponse.Body.String())
	assert.Equal(t, "image/png", imageResponse.Header().Get("Content-Type"))
	assert.Equal(t, content.Bytes(), imageResponse.Body.Bytes())
	assert.Equal(t, "private, no-store", imageResponse.Header().Get("Cache-Control"))
	assert.Equal(t, "inline", imageResponse.Header().Get("Content-Disposition"))
	assert.Contains(t, imageResponse.Header().Get("Content-Security-Policy"), "script-src 'none'")
	headResponse := httptest.NewRecorder()
	engine.ServeHTTP(headResponse, httptest.NewRequest(http.MethodHead, preview, nil))
	assert.Equal(t, http.StatusOK, headResponse.Code)
	assert.Equal(t, "image/png", headResponse.Header().Get("Content-Type"))
	assert.Equal(t, strconv.Itoa(content.Len()), headResponse.Header().Get("Content-Length"))
	assert.Empty(t, headResponse.Body.String())
	assert.NotContains(t, response.Body.String(), "upstream.example")
	assert.NotContains(t, response.Body.String(), "source_url")
	assert.Equal(t, content.Bytes(), request(payload.Data.References[0].PreviewURL).Body.Bytes())
	rangeResponse := request(preview, "bytes=0-7")
	assert.Equal(t, http.StatusPartialContent, rangeResponse.Code)
	assert.Equal(t, content.Bytes()[:8], rangeResponse.Body.Bytes())
	parsed, err := url.Parse(preview)
	require.NoError(t, err)
	assert.Equal(t, strconv.FormatInt(task.FinishedAt+7200, 10), parsed.Query().Get("expires"))
	for _, tampered := range []string{
		strings.Replace(preview, "/media/0", "/media/1", 1),
		strings.Replace(preview, "/media/0", "/reference/0", 1),
		strings.Replace(preview, task.TaskID, "async_other", 1),
		preview + "&expires=9999999999",
		preview + "&signature=duplicate",
		preview + "&api_key=unexpected",
		parsed.Path,
	} {
		assert.Equal(t, http.StatusUnauthorized, request(tampered).Code, tampered)
	}
	assert.Equal(t, http.StatusUnauthorized, request(payload.Data.Media[0].URL).Code)
	// 文件到期后原链接停止读取；日志、提示词、状态和完成时间继续保留。
	task.FinishedAt = common.GetTimestamp() - 7201
	require.NoError(t, db.Model(task).Update("finished_at", task.FinishedAt).Error)
	require.NoError(t, model.CleanupExpiredAsyncRelayTasks(common.NodeName))
	assert.Equal(t, http.StatusGone, request(preview).Code)
	_, err = os.Stat(path)
	assert.True(t, os.IsNotExist(err))
	var log model.Task
	require.NoError(t, db.First(&log, task.LogID).Error)
	assert.Equal(t, model.TaskStatus(model.TaskStatusSuccess), log.Status)
	fresh, err := model.GetAsyncRelayTaskByTaskID(task.TaskID)
	require.NoError(t, err)
	require.NotNil(t, fresh)
	assert.Equal(t, task.FinishedAt, fresh.FinishedAt)
	assert.Equal(t, model.AsyncRelayTaskStatusSucceeded, fresh.Status)
	assert.Empty(t, fresh.ResultFiles)
	assert.Contains(t, fresh.RequestDetails, "自动到期只清图片")
	assert.NotContains(t, fresh.RequestDetails, "upstream.example")
	assert.NotContains(t, fresh.RequestDetails, path)
}
