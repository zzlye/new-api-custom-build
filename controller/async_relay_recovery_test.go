package controller

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 已收到原接口答复的任务只恢复归档；没有令牌和渠道也不应再次请求生成。
func TestAsyncRelayRecoversSavedResponseWithoutResubmission(t *testing.T) {
	cases := []struct {
		name, body string
		status     int
		expected   model.AsyncRelayTaskStatus
	}{
		{"成功响应", fmt.Sprintf(`{"data":[{"b64_json":"%s"}]}`, asyncFixturePNG), http.StatusOK, model.AsyncRelayTaskStatusSucceeded},
		{"失败响应", `{"error":{"message":"Invalid token"}}`, http.StatusUnauthorized, model.AsyncRelayTaskStatusFailed},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			prepareAsyncMediaController(t)
			path, file, err := common.CreateAsyncMediaFile()
			require.NoError(t, err)
			_, err = file.WriteString(test.body)
			require.NoError(t, err)
			require.NoError(t, file.Close())
			now := common.GetTimestamp()
			task := &model.AsyncRelayTask{UserID: 101, NodeID: common.NodeName, RequestFormat: string(relaytypes.RelayFormatOpenAIImage), Status: model.AsyncRelayTaskStatusProcessing, WorkerID: "interrupted-fixture", UpdatedAt: now - 120, StartedAt: now - 120, DispatchStartedAt: now - 120, ResponseFilePath: path, ResponseContentType: "application/json", ResponseStatusCode: test.status}
			require.NoError(t, task.InsertWithLog("default", "IMAGE"))
			require.NoError(t, model.DB.Model(task).Update("updated_at", now-120).Error)
			recovered, err := model.RecoverStaleAsyncRelayTasks(60)
			require.NoError(t, err)
			require.EqualValues(t, 1, recovered)
			require.NoError(t, model.DB.Model(task).Update("updated_at", now-10).Error)
			processed, err := ProcessAsyncRelayTasks(context.Background(), 1)
			require.NoError(t, err)
			require.Equal(t, 1, processed)
			saved, err := model.GetAsyncRelayTaskByTaskID(task.TaskID)
			require.NoError(t, err)
			require.Equal(t, test.expected, saved.Status, saved.Error)
			if test.status >= 400 {
				assert.Contains(t, saved.Error, "Invalid token")
			}
			require.NoError(t, model.DB.Model(saved).Update("finished_at", now-common.AsyncMediaRetentionSeconds()-1).Error)
			require.NoError(t, model.CleanupExpiredAsyncRelayTasks(common.NodeName))
			_, err = os.Stat(path)
			require.True(t, os.IsNotExist(err), "原接口响应没有随保留时长到期清理")
			expired, err := model.GetAsyncRelayTaskByTaskID(task.TaskID)
			require.NoError(t, err)
			assert.Empty(t, expired.ResponseFilePath)
			var log model.Task
			require.NoError(t, model.DB.First(&log, task.LogID).Error)
			assert.Equal(t, task.TaskID, log.TaskID)
		})
	}
}

// 另一个执行进程完成回执时，普通请求仍从持久记录取得原生返回体，不切换为任务协议。
func TestAsyncRelayDeliveryRestoresPersistedNativeReceipt(t *testing.T) {
	prepareAsyncMediaController(t)
	path, file, err := common.CreateAsyncMediaFile()
	require.NoError(t, err)
	body := `{"id":"native-video-fixture","object":"video","status":"queued"}`
	_, err = file.WriteString(body)
	require.NoError(t, err)
	require.NoError(t, file.Close())
	task := &model.AsyncRelayTask{UserID: 102, NodeID: common.NodeName, Status: model.AsyncRelayTaskStatusWaiting, RequestFormat: string(relaytypes.RelayFormatTask), ResponseFilePath: path, ResponseStatusCode: http.StatusOK, ResponseContentType: "application/json"}
	require.NoError(t, task.InsertWithLog("default", "VIDEO"))
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	delivery := &asyncRelayDelivery{changed: make(chan struct{}, 1)}
	require.NoError(t, delivery.serve(c, task.TaskID))
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.JSONEq(t, body, recorder.Body.String())
	assert.Equal(t, "application/json", recorder.Header().Get("Content-Type"))
	saved, err := model.GetAsyncRelayTaskByTaskID(task.TaskID)
	require.NoError(t, err)
	assert.Equal(t, model.AsyncRelayTaskStatusWaiting, saved.Status)
}

// 绘图任务继续使用原生编号读取子任务，后台编号只用于日志归属和文件管理。
func TestAsyncRelayMidjourneyUsesNativeTaskIDForCompletion(t *testing.T) {
	prepareAsyncMediaController(t)
	parent := &model.AsyncRelayTask{UserID: 103, NodeID: common.NodeName, RequestFormat: string(relaytypes.RelayFormatMjProxy)}
	require.NoError(t, parent.InsertWithLog("default", "IMAGE"))
	_, won, err := model.ClaimAsyncRelayTask(parent.ID, "mj-fixture")
	require.NoError(t, err)
	require.True(t, won)
	child := &model.Midjourney{UserId: parent.UserID, MjId: "native-mj-fixture", Status: "SUCCESS", ImageUrl: "data:image/png;base64," + asyncFixturePNG, Quota: 17}
	require.NoError(t, model.InsertMidjourneyForAsyncRelay(child, parent.TaskID))
	claimed, err := model.GetAsyncRelayTaskByTaskID(parent.TaskID)
	require.NoError(t, err)
	pollLinkedAsyncRelayTask(context.Background(), claimed)
	assert.Equal(t, model.AsyncRelayTaskStatusSucceeded, claimed.Status, claimed.Error)
	var media []model.AsyncRelayMedia
	require.NoError(t, common.Unmarshal([]byte(claimed.ResultFiles), &media))
	require.Len(t, media, 1)
	assert.Equal(t, "image", media[0].Kind)
	var log model.Task
	require.NoError(t, model.DB.First(&log, parent.LogID).Error)
	assert.Equal(t, 17, log.Quota)
}
