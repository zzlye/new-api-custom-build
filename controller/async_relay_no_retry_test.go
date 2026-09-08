package controller

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 即使全局允许重试，图片和视频生成也只向上游提交一次。
func TestAsyncRelayNoAutomaticRetrySubmission(t *testing.T) {
	for _, status := range []int{429, 500, 502, 503} {
		for _, video := range []bool{false, true} {
			t.Run(fmt.Sprintf("status_%d_video_%t", status, video), func(t *testing.T) {
				var requests atomic.Int32
				modelName, path, body := "dall-e-3", "/v1/images/generations", `{"model":"dall-e-3","prompt":"只提交一次","n":1,"size":"1024x1024"}`
				format := relaytypes.RelayFormat(relaytypes.RelayFormatOpenAIImage)
				if video {
					modelName, path, body = "sora-2", "/v1/videos", `{"model":"sora-2","prompt":"只提交一次","seconds":"4","size":"720x1280"}`
					format = relaytypes.RelayFormatTask
				}
				user, token := prepareAsyncCompatRelay(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					assert.Equal(t, http.MethodPost, r.Method)
					requests.Add(1)
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(status)
					_, err := w.Write([]byte(`{"error":{"message":"上游失败替身","type":"server_error"}}`))
					assert.NoError(t, err)
				}), modelName)
				previousRetry := common.RetryTimes
				common.RetryTimes = 3
				t.Cleanup(func() { common.RetryTimes = previousRetry })
				_, done, _ := beginAsyncCompatRequest(t, user, token, path, body, format)
				count, err := ProcessAsyncRelayTasks(context.Background(), 1)
				require.NoError(t, err)
				require.Equal(t, 1, count)
				select {
				case <-done:
				case <-time.After(5 * time.Second):
					t.Fatal("失败响应应及时返回")
				}
				for range 3 {
					count, err = ProcessAsyncRelayTasks(context.Background(), 1)
					require.NoError(t, err)
					assert.Zero(t, count)
				}
				assert.Equal(t, int32(1), requests.Load())
				var task model.AsyncRelayTask
				require.NoError(t, model.DB.First(&task).Error)
				assert.Equal(t, model.AsyncRelayTaskStatusFailed, task.Status)
			})
		}
	}
}

// 升级前的待重试记录和保存中断记录只能结束，不能再次尝试保存。
func TestAsyncRelayNoAutomaticRetryRecovery(t *testing.T) {
	for _, status := range []model.AsyncRelayTaskStatus{model.AsyncRelayTaskStatusPending, model.AsyncRelayTaskStatusWaiting, model.AsyncRelayTaskStatusProcessing} {
		for _, attempts := range []int{0, 1} {
			t.Run(fmt.Sprintf("%s_attempts_%d", status, attempts), func(t *testing.T) {
				prepareAsyncMediaController(t)
				settings := system_setting.GetFetchSetting()
				previousProtection := settings.EnableSSRFProtection
				settings.EnableSSRFProtection = false
				t.Cleanup(func() { settings.EnableSSRFProtection = previousProtection })
				service.InitHttpClient()
				var requests atomic.Int32
				source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					requests.Add(1)
					w.WriteHeader(http.StatusBadGateway)
				}))
				t.Cleanup(source.Close)
				path, file, err := common.CreateAsyncMediaFile()
				require.NoError(t, err)
				_, err = fmt.Fprintf(file, `{"data":[{"url":"%s/image.png"}]}`, source.URL)
				require.NoError(t, err)
				require.NoError(t, file.Close())
				task := &model.AsyncRelayTask{UserID: 91, NodeID: common.NodeName, Status: status,
					RequestFormat: string(relaytypes.RelayFormatOpenAIImage), WorkerID: "旧执行者",
					ResultFilePath: path, ResultContentType: "application/json", MediaAttempts: attempts}
				if attempts == 0 {
					task.NextAttemptAt = common.GetTimestamp() + 3600
				}
				require.NoError(t, task.InsertWithLog("default", "IMAGE"))
				require.NoError(t, model.DB.Model(task).Update("updated_at", common.GetTimestamp()-1200).Error)
				_, err = model.RecoverStaleAsyncRelayTasks(60)
				require.NoError(t, err)
				require.NoError(t, model.DB.Model(task).Update("updated_at", common.GetTimestamp()-10).Error)
				_, err = ProcessAsyncRelayTasks(context.Background(), 1)
				require.NoError(t, err)
				saved, err := model.GetAsyncRelayTaskByTaskID(task.TaskID)
				require.NoError(t, err)
				assert.Equal(t, model.AsyncRelayTaskStatusFailed, saved.Status)
				assert.Zero(t, saved.NextAttemptAt)
				assert.Contains(t, saved.Error, "自动重试已关闭")
				assert.NotZero(t, saved.FinishedAt)
				assert.Zero(t, requests.Load())
			})
		}
	}
}

// 原生任务的正常等待不占保存机会；成功后的文件下载失败则直接结束。
func TestAsyncRelayNoAutomaticRetryNativeMedia(t *testing.T) {
	for _, video := range []bool{false, true} {
		t.Run(fmt.Sprintf("video_%t", video), func(t *testing.T) {
			prepareAsyncMediaController(t)
			settings := system_setting.GetFetchSetting()
			previousProtection := settings.EnableSSRFProtection
			settings.EnableSSRFProtection = false
			t.Cleanup(func() { settings.EnableSSRFProtection = previousProtection })
			service.InitHttpClient()
			var downloads atomic.Int32
			source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				downloads.Add(1)
				// 实际发送下载请求之前，唯一保存机会已经持久化。
				var saved model.AsyncRelayTask
				if assert.NoError(t, model.DB.First(&saved).Error) {
					assert.Equal(t, 1, saved.MediaAttempts)
				}
				w.WriteHeader(http.StatusBadGateway)
			}))
			t.Cleanup(source.Close)
			channel := &model.Channel{Type: constant.ChannelTypeOpenAI, Key: "fixture-upstream", Status: common.ChannelStatusEnabled, Name: "保存失败替身", BaseURL: &source.URL}
			require.NoError(t, model.DB.Create(channel).Error)
			parent := &model.AsyncRelayTask{UserID: 92, NodeID: common.NodeName, RequestFormat: string(relaytypes.RelayFormatMjProxy)}
			if video {
				parent.RequestFormat = string(relaytypes.RelayFormatTask)
			}
			require.NoError(t, parent.InsertWithLog("default", "MEDIA"))
			claimed, won, err := model.ClaimAsyncRelayTask(parent.ID, "fixture-worker")
			require.NoError(t, err)
			require.True(t, won)
			claimed.LinkedTaskID = "native_" + parent.TaskID
			child := &model.Task{TaskID: claimed.LinkedTaskID, UserId: parent.UserID, ChannelId: channel.Id, Status: model.TaskStatusInProgress, Quota: 123, PrivateData: model.TaskPrivateData{UpstreamTaskID: "provider-fixture", ResultURL: source.URL + "/video.mp4"}}
			mj := &model.Midjourney{MjId: claimed.LinkedTaskID, UserId: parent.UserID, ChannelId: channel.Id, Status: "IN_PROGRESS", Quota: 123, ImageUrl: source.URL + "/image.png"}
			if video {
				require.NoError(t, model.AttachAsyncRelayNativeTask(parent.TaskID, child))
			} else {
				require.NoError(t, model.DB.Create(mj).Error)
			}
			pollLinkedAsyncRelayTask(context.Background(), claimed)
			require.Equal(t, model.AsyncRelayTaskStatusWaiting, claimed.Status)
			assert.Zero(t, claimed.MediaAttempts)
			assert.Zero(t, downloads.Load())
			if video {
				require.NoError(t, model.DB.Model(child).Update("status", model.TaskStatusSuccess).Error)
			} else {
				require.NoError(t, model.DB.Model(mj).Update("status", "SUCCESS").Error)
			}
			require.NoError(t, model.DB.Model(claimed).Update("updated_at", common.GetTimestamp()-10).Error)
			count, err := ProcessAsyncRelayTasks(context.Background(), 1)
			require.NoError(t, err)
			require.Equal(t, 1, count)
			saved, err := model.GetAsyncRelayTaskByTaskID(parent.TaskID)
			require.NoError(t, err)
			assert.Equal(t, model.AsyncRelayTaskStatusFailed, saved.Status)
			assert.Contains(t, saved.Error, "自动重试已关闭")
			assert.Zero(t, saved.NextAttemptAt)
			require.NoError(t, model.DB.Model(saved).Update("updated_at", common.GetTimestamp()-1200).Error)
			for range 3 {
				count, err = ProcessAsyncRelayTasks(context.Background(), 1)
				require.NoError(t, err)
				assert.Zero(t, count)
			}
			assert.Equal(t, int32(1), downloads.Load())
			var taskLog model.Task
			require.NoError(t, model.DB.First(&taskLog, saved.LogID).Error)
			assert.Equal(t, 123, taskLog.Quota, "保存失败不能抹除已经生成的计费记录")
			assert.Equal(t, model.TaskStatus(model.TaskStatusFailure), taskLog.Status)
		})
	}
}
