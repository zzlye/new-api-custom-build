package controller

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const asyncFixturePNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jhXcAAAAASUVORK5CYII="

// prepareAsyncMediaController 使用独立数据库和临时媒体目录，不触及运行实例的数据。
func prepareAsyncMediaController(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousMainType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
	previousRedis, previousMemory, previousBatch := common.RedisEnabled, common.MemoryCacheEnabled, common.BatchUpdateEnabled
	// Redis 开关未变化时保持只读，避免后台统计采样与测试准备发生无意义的同值写入竞争。
	if common.RedisEnabled {
		common.RedisEnabled = false
	}
	common.MemoryCacheEnabled, common.BatchUpdateEnabled = false, false
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	model.DB, model.LOG_DB = db, db
	t.Setenv("LOG_SQL_DSN", "")
	require.NoError(t, model.InitLogDB())
	require.NoError(t, db.AutoMigrate(&model.AsyncRelayTask{}, &model.Task{}, &model.Midjourney{}, &model.Option{}, &model.User{}, &model.Token{}, &model.Channel{}, &model.Ability{}, &model.Log{}, &model.QuotaData{}))
	t.Setenv("ASYNC_MEDIA_DIR", t.TempDir())
	common.OptionMapRWMutex.Lock()
	previousOptions := common.OptionMap
	common.OptionMap = map[string]string{common.AsyncMediaRetentionOption: "2"}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMainType, previousLogType)
		if common.RedisEnabled != previousRedis {
			common.RedisEnabled = previousRedis
		}
		common.MemoryCacheEnabled, common.BatchUpdateEnabled = previousMemory, previousBatch
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previousOptions
		common.OptionMapRWMutex.Unlock()
		require.NoError(t, sqlDB.Close())
	})
}

func createCompletedAsyncImage(t *testing.T, userID int) *model.AsyncRelayTask {
	t.Helper()
	task := &model.AsyncRelayTask{UserID: userID, NodeID: common.NodeName, RequestFormat: string(relaytypes.RelayFormatOpenAIImage)}
	require.NoError(t, task.InsertWithLog("default", "IMAGE"))
	claimed, won, err := model.ClaimAsyncRelayTask(task.ID, "fixture-worker")
	require.NoError(t, err)
	require.True(t, won)
	path, file, err := common.CreateAsyncMediaFile()
	require.NoError(t, err)
	_, err = fmt.Fprintf(file, `{"data":[{"b64_json":"%s"}]}`, asyncFixturePNG)
	require.NoError(t, err)
	require.NoError(t, file.Close())
	require.True(t, completeAsyncRelayResult(context.Background(), claimed, path, "application/json"))
	return claimed
}

func asyncControllerRequest(handler gin.HandlerFunc, method, path string, userID, role int, params gin.Params, body string) *httptest.ResponseRecorder {
	recording := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recording)
	c.Request = httptest.NewRequest(method, path, strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = params
	c.Set("id", userID)
	c.Set("role", role)
	handler(c)
	return recording
}

func TestAsyncRelayEnqueuePersistsBeforeReturningAndPreservesMultipart(t *testing.T) {
	prepareAsyncMediaController(t)
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	require.NoError(t, form.WriteField("model", "gpt-image-1"))
	require.NoError(t, form.WriteField("stream", "true"))
	require.NoError(t, form.WriteField("background", "transparent"))
	require.NoError(t, form.WriteField("n", "2"))
	part, err := form.CreateFormFile("image[]", "输入图片.png")
	require.NoError(t, err)
	_, err = part.Write([]byte("image-input"))
	require.NoError(t, err)
	require.NoError(t, form.Close())
	recording := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recording)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits?async=true&key=secret", &body)
	c.Request.Header.Set("Content-Type", form.FormDataContentType())
	c.Request.Header.Set("X-Secret-Key", "do-not-store")
	c.Set("id", 31)
	c.Set("token_id", 11)
	require.NoError(t, EnqueueAsyncRelayRequest(c, relaytypes.RelayFormatOpenAIImage))
	common.CleanupBodyStorage(c)
	require.Equal(t, http.StatusAccepted, recording.Code)
	var response struct {
		TaskID string `json:"task_id"`
		Status string `json:"status"`
	}
	require.NoError(t, common.Unmarshal(recording.Body.Bytes(), &response))
	require.NotEmpty(t, response.TaskID)
	assert.Equal(t, "pending", response.Status)
	task, err := model.GetAsyncRelayTaskByUserAndTaskID(31, response.TaskID)
	require.NoError(t, err)
	require.NotNil(t, task)
	assert.NotZero(t, task.LogID)
	assert.Empty(t, task.RequestQuery)
	assert.NotContains(t, task.RequestFiles, "do-not-store")
	file, err := os.Open(task.RequestFilePath)
	require.NoError(t, err)
	defer file.Close()
	saved := httptest.NewRequest(http.MethodPost, "/", file)
	saved.Header.Set("Content-Type", task.RequestContentType)
	require.NoError(t, saved.ParseMultipartForm(1024))
	defer saved.MultipartForm.RemoveAll()
	assert.Equal(t, "true", saved.FormValue("stream"))
	assert.Equal(t, "2", saved.FormValue("n"))
	assert.Equal(t, "transparent", saved.FormValue("background"))
	attachment, header, err := saved.FormFile("image[]")
	require.NoError(t, err)
	defer attachment.Close()
	contents, err := io.ReadAll(attachment)
	require.NoError(t, err)
	assert.Equal(t, "输入图片.png", header.Filename)
	assert.Equal(t, "image-input", string(contents))
	var log model.Task
	require.NoError(t, model.DB.First(&log, task.LogID).Error)
	assert.Equal(t, model.TaskStatus(model.TaskStatusQueued), log.Status)
	assert.Equal(t, task.TaskID, log.TaskID)
}

func TestAsyncRelayCompletedMediaOwnershipExpiryAndDeletion(t *testing.T) {
	prepareAsyncMediaController(t)
	task := createCompletedAsyncImage(t, 41)
	params := gin.Params{{Key: "task_id", Value: task.TaskID}, {Key: "index", Value: "0"}}
	stranger := asyncControllerRequest(GetAsyncRelayMedia, http.MethodGet, "/", 99, common.RoleCommonUser, params, "")
	assert.Equal(t, http.StatusNotFound, stranger.Code)
	preview := asyncControllerRequest(GetAsyncRelayMedia, http.MethodGet, "/", 41, common.RoleCommonUser, params, "")
	require.Equal(t, http.StatusOK, preview.Code)
	expected, err := base64.StdEncoding.DecodeString(asyncFixturePNG)
	require.NoError(t, err)
	assert.Equal(t, expected, preview.Body.Bytes())
	assert.Contains(t, preview.Header().Get("Cache-Control"), "no-store")
	adminPreview := asyncControllerRequest(GetAsyncRelayMedia, http.MethodGet, "/", 99, common.RoleAdminUser, params, "")
	assert.Equal(t, http.StatusOK, adminPreview.Code)
	denied := asyncControllerRequest(DeleteTaskLog, http.MethodDelete, "/", 99, common.RoleAdminUser, gin.Params{{Key: "id", Value: strconv.FormatInt(task.LogID, 10)}}, "")
	assert.Equal(t, http.StatusForbidden, denied.Code)
	task.FinishedAt = common.GetTimestamp() - 7200
	require.NoError(t, model.DB.Model(task).Update("finished_at", task.FinishedAt).Error)
	poll := asyncControllerRequest(GetAsyncRelayTask, http.MethodGet, "/", 41, common.RoleCommonUser, params, "")
	require.Equal(t, http.StatusOK, poll.Code)
	assert.Contains(t, poll.Body.String(), `"status":"succeeded"`)
	assert.Contains(t, poll.Body.String(), `"result_expired":true`)
	assert.NotContains(t, poll.Body.String(), "b64_json")
	expired := asyncControllerRequest(GetAsyncRelayMedia, http.MethodGet, "/", 41, common.RoleCommonUser, params, "")
	assert.Equal(t, http.StatusGone, expired.Code)
	paths, err := model.ListAsyncRelayTaskFiles(task)
	require.NoError(t, err)
	require.NoError(t, model.CleanupExpiredAsyncRelayTasks(common.NodeName))
	for _, path := range paths {
		if path != "" {
			_, err := os.Stat(path)
			assert.True(t, os.IsNotExist(err), path)
		}
	}
	var retained model.Task
	require.NoError(t, model.DB.First(&retained, task.LogID).Error)
	assert.Equal(t, model.TaskStatus(model.TaskStatusSuccess), retained.Status)
	deleted := asyncControllerRequest(DeleteTaskLog, http.MethodDelete, "/", 99, common.RoleRootUser, gin.Params{{Key: "id", Value: strconv.FormatInt(task.LogID, 10)}}, "")
	require.Equal(t, http.StatusOK, deleted.Code)
	require.ErrorIs(t, model.DB.First(&retained, task.LogID).Error, gorm.ErrRecordNotFound)
	missing, err := model.GetAsyncRelayTaskByTaskID(task.TaskID)
	require.NoError(t, err)
	assert.Nil(t, missing)
}

func TestAsyncRelayRetentionSettingIsRootOnly(t *testing.T) {
	prepareAsyncMediaController(t)
	denied := asyncControllerRequest(UpdateOption, http.MethodPut, "/api/option", 1, common.RoleAdminUser, nil, `{"key":"AsyncMediaRetentionHours","value":1}`)
	assert.Equal(t, http.StatusForbidden, denied.Code)
	assert.Equal(t, int64(7200), common.AsyncMediaRetentionSeconds())
	saved := asyncControllerRequest(UpdateOption, http.MethodPut, "/api/option", 1, common.RoleRootUser, nil, `{"key":"AsyncMediaRetentionHours","value":1}`)
	require.Equal(t, http.StatusOK, saved.Code)
	assert.Contains(t, saved.Body.String(), `"success":true`)
	assert.Equal(t, int64(3600), common.AsyncMediaRetentionSeconds())
	invalid := asyncControllerRequest(UpdateOption, http.MethodPut, "/api/option", 1, common.RoleRootUser, nil, `{"key":"AsyncMediaRetentionHours","value":1.5}`)
	assert.Contains(t, invalid.Body.String(), `"success":false`)
	assert.Equal(t, int64(3600), common.AsyncMediaRetentionSeconds())
}

func TestAsyncRelayWorkerSurvivesClientCancellationAndChargesOnce(t *testing.T) {
	prepareAsyncMediaController(t)
	var requests atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/images/generations", r.URL.Path)
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, err := fmt.Fprintf(w, `{"created":1,"data":[{"b64_json":"%s"}]}`, asyncFixturePNG)
		assert.NoError(t, err)
	}))
	defer upstream.Close()
	service.InitHttpClient()
	oldPrices, err := common.Marshal(ratio_setting.GetModelPriceCopy())
	require.NoError(t, err)
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString("{\"dall-e-3\":0.04}"))
	t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(string(oldPrices))) })
	user := &model.User{Id: 71, Username: "async-worker-fixture", Status: common.UserStatusEnabled, Role: common.RoleCommonUser, Group: "default", Quota: 100000000, Setting: `{"billing_preference":"wallet_only"}`}
	require.NoError(t, model.DB.Create(user).Error)
	token := &model.Token{UserId: user.Id, Key: "asyncFixtureToken", Status: common.TokenStatusEnabled, Name: "后台生成测试", ExpiredTime: -1, UnlimitedQuota: true}
	require.NoError(t, model.DB.Create(token).Error)
	channel := &model.Channel{Type: constant.ChannelTypeOpenAI, Key: "fixture-upstream", Status: common.ChannelStatusEnabled, Name: "本地生成测试", Models: "dall-e-3", Group: "default", BaseURL: &upstream.URL}
	require.NoError(t, model.DB.Create(channel).Error)
	require.NoError(t, model.DB.Create(&model.Ability{Group: "default", Model: "dall-e-3", ChannelId: channel.Id, Enabled: true}).Error)
	recording := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recording)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations?async=true", strings.NewReader(`{"model":"dall-e-3","prompt":"一只猫","n":1,"size":"1024x1024"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	clientCtx, cancel := context.WithCancel(c.Request.Context())
	c.Request = c.Request.WithContext(clientCtx)
	c.Set("id", user.Id)
	c.Set("token_id", token.Id)
	require.NoError(t, EnqueueAsyncRelayRequest(c, relaytypes.RelayFormatOpenAIImage))
	common.CleanupBodyStorage(c)
	assert.Equal(t, int32(0), requests.Load())
	cancel()
	processed, err := ProcessAsyncRelayTasks(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, 1, processed)
	var task model.AsyncRelayTask
	require.NoError(t, model.DB.First(&task).Error)
	require.Equal(t, model.AsyncRelayTaskStatusSucceeded, task.Status, task.Error)
	assert.Equal(t, int32(1), requests.Load())
	var log model.Task
	require.NoError(t, model.DB.First(&log, task.LogID).Error)
	require.Greater(t, log.Quota, 0)
	var after model.User
	require.NoError(t, model.DB.First(&after, user.Id).Error)
	assert.Equal(t, user.Quota-log.Quota, after.Quota)
	again, err := ProcessAsyncRelayTasks(context.Background(), 1)
	require.NoError(t, err)
	assert.Zero(t, again)
	assert.Equal(t, int32(1), requests.Load())
}

func TestAsyncRelayStorageRetryReusesSavedResponseWithoutResubmission(t *testing.T) {
	prepareAsyncMediaController(t)
	settings := system_setting.GetFetchSetting()
	previousProtection := settings.EnableSSRFProtection
	settings.EnableSSRFProtection = false
	t.Cleanup(func() { settings.EnableSSRFProtection = previousProtection })
	service.InitHttpClient()
	var downloads atomic.Int32
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		if downloads.Add(1) == 1 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		data, err := base64.StdEncoding.DecodeString(asyncFixturePNG)
		assert.NoError(t, err)
		_, err = w.Write(data)
		assert.NoError(t, err)
	}))
	defer source.Close()
	task := &model.AsyncRelayTask{UserID: 91, NodeID: common.NodeName, RequestFormat: string(relaytypes.RelayFormatOpenAIImage)}
	require.NoError(t, task.InsertWithLog("default", "IMAGE"))
	claimed, won, err := model.ClaimAsyncRelayTask(task.ID, "fixture-worker")
	require.NoError(t, err)
	require.True(t, won)
	path, file, err := common.CreateAsyncMediaFile()
	require.NoError(t, err)
	_, err = fmt.Fprintf(file, `{"data":[{"url":"%s/image.png"}]}`, source.URL)
	require.NoError(t, err)
	require.NoError(t, file.Close())
	// 即便预览下载暂时失败，已经生成的完整响应仍归任务持有。
	require.True(t, completeAsyncRelayResult(context.Background(), claimed, path, "application/json"))
	saved, err := model.GetAsyncRelayTaskByTaskID(task.TaskID)
	require.NoError(t, err)
	assert.Equal(t, model.AsyncRelayTaskStatusWaiting, saved.Status)
	assert.Equal(t, path, saved.ResultFilePath)
	_, err = os.Stat(path)
	require.NoError(t, err)
	count, err := ProcessAsyncRelayTasks(context.Background(), 1)
	require.NoError(t, err)
	assert.Zero(t, count)
	require.NoError(t, model.DB.Model(saved).Updates(map[string]any{"updated_at": common.GetTimestamp() - 10, "next_attempt_at": 0}).Error)
	count, err = ProcessAsyncRelayTasks(context.Background(), 1)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	saved, err = model.GetAsyncRelayTaskByTaskID(task.TaskID)
	require.NoError(t, err)
	require.Equal(t, model.AsyncRelayTaskStatusSucceeded, saved.Status, saved.Error)
	assert.Equal(t, int32(2), downloads.Load())
	assert.Empty(t, saved.Error)
	// 仅提供下载链接的图片也保留来源，重试归档不会把地址丢掉。
	media := asyncRelayMediaLinks(saved, "/api/task/")
	require.Len(t, media, 1)
	assert.Equal(t, source.URL+"/image.png", media[0].SourceURL)
}

func TestAsyncRelayVideoCompletionStoresLocalVideoAndKeepsOneTaskLog(t *testing.T) {
	prepareAsyncMediaController(t)
	settings := system_setting.GetFetchSetting()
	previousProtection := settings.EnableSSRFProtection
	settings.EnableSSRFProtection = false
	t.Cleanup(func() { settings.EnableSSRFProtection = previousProtection })
	service.InitHttpClient()
	video := []byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'm', 'p', '4', '2', 0, 0, 0, 0, 'm', 'p', '4', '2', 'i', 's', 'o', 'm'}
	var downloads atomic.Int32
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		downloads.Add(1)
		w.Header().Set("Content-Type", "video/mp4")
		_, err := w.Write(video)
		assert.NoError(t, err)
	}))
	defer source.Close()
	channel := &model.Channel{Type: constant.ChannelTypeOpenAI, Key: "fixture-upstream", Status: common.ChannelStatusEnabled, Name: "视频下载测试", BaseURL: &source.URL}
	require.NoError(t, model.DB.Create(channel).Error)
	parent := &model.AsyncRelayTask{UserID: 92, NodeID: common.NodeName, RequestFormat: string(relaytypes.RelayFormatTask)}
	require.NoError(t, parent.InsertWithLog("default", "VIDEO"))
	claimed, won, err := model.ClaimAsyncRelayTask(parent.ID, "fixture-worker")
	require.NoError(t, err)
	require.True(t, won)
	child := &model.Task{TaskID: "native_" + parent.TaskID, UserId: parent.UserID, ChannelId: channel.Id, Status: model.TaskStatusSuccess, Quota: 123, PrivateData: model.TaskPrivateData{UpstreamTaskID: "provider-fixture", ResultURL: source.URL + "/video.mp4"}}
	require.NoError(t, model.AttachAsyncRelayNativeTask(parent.TaskID, child))
	claimed.LinkedTaskID, claimed.Status = child.TaskID, model.AsyncRelayTaskStatusWaiting
	_, err = claimed.UpdateWithStatus(model.AsyncRelayTaskStatusProcessing)
	require.NoError(t, err)
	require.NoError(t, model.DB.Model(claimed).Update("updated_at", common.GetTimestamp()-10).Error)
	count, err := ProcessAsyncRelayTasks(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	saved, err := model.GetAsyncRelayTaskByTaskID(parent.TaskID)
	require.NoError(t, err)
	require.Equal(t, model.AsyncRelayTaskStatusSucceeded, saved.Status, saved.Error)
	assert.Equal(t, int32(1), downloads.Load())
	media := asyncRelayMediaLinks(saved, "/api/task/")
	require.Len(t, media, 1)
	assert.Equal(t, source.URL+"/video.mp4", media[0].SourceURL)
	preview := asyncControllerRequest(GetAsyncRelayMedia, http.MethodGet, "/", parent.UserID, common.RoleCommonUser, gin.Params{{Key: "task_id", Value: parent.TaskID}, {Key: "index", Value: "0"}}, "")
	require.Equal(t, http.StatusOK, preview.Code)
	assert.Equal(t, video, preview.Body.Bytes())
	assert.Equal(t, "video/mp4", preview.Header().Get("Content-Type"))
	logs := model.TaskGetAllUserTask(parent.UserID, 0, 20, model.SyncTaskQueryParams{})
	require.Len(t, logs, 1)
	assert.Equal(t, 123, logs[0].Quota)
}

func TestAsyncRelayRootDeletionKeepsRunningTask(t *testing.T) {
	prepareAsyncMediaController(t)
	task := &model.AsyncRelayTask{UserID: 93}
	require.NoError(t, task.InsertWithLog("default", "IMAGE"))
	response := asyncControllerRequest(DeleteTaskLog, http.MethodDelete, "/", 1, common.RoleRootUser, gin.Params{{Key: "id", Value: strconv.FormatInt(task.LogID, 10)}}, "")
	assert.Equal(t, http.StatusConflict, response.Code)
	saved, err := model.GetAsyncRelayTaskByTaskID(task.TaskID)
	require.NoError(t, err)
	require.NotNil(t, saved)
	assert.Equal(t, model.AsyncRelayTaskStatusPending, saved.Status)
}

// 相同上游编号可能复用生成结果，根用户删除时仍应只影响选定的主任务。
func TestAsyncRelayMidjourneyDeletionKeepsOtherParents(t *testing.T) {
	prepareAsyncMediaController(t)
	first := createCompletedAsyncImage(t, 41)
	second := createCompletedAsyncImage(t, 41)
	for _, parent := range []*model.AsyncRelayTask{first, second} {
		parent.RequestFormat = string(relaytypes.RelayFormatMjProxy)
		parent.LinkedTaskID = "shared-upstream-image"
		require.NoError(t, parent.Update())
		child := &model.Midjourney{UserId: 41, MjId: parent.LinkedTaskID, AsyncParentID: parent.TaskID, Status: "SUCCESS", Progress: "100%"}
		require.NoError(t, model.DB.Create(child).Error)
	}
	selected := model.GetByMJId(41, second.TaskID)
	require.NotNil(t, selected)
	assert.Equal(t, second.TaskID, selected.AsyncParentID)
	deleted := asyncControllerRequest(DeleteTaskLog, http.MethodDelete, "/", 1, common.RoleRootUser,
		gin.Params{{Key: "id", Value: strconv.FormatInt(first.LogID, 10)}}, "")
	require.Equal(t, http.StatusOK, deleted.Code)
	remaining := model.GetByMJId(41, second.TaskID)
	require.NotNil(t, remaining)
	assert.Equal(t, second.TaskID, remaining.AsyncParentID)
	var otherLog model.Task
	require.NoError(t, model.DB.First(&otherLog, second.LogID).Error)
	assert.Equal(t, model.TaskStatus(model.TaskStatusSuccess), otherLog.Status)
}
