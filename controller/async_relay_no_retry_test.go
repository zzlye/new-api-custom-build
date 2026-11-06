package controller

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
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
					task.NextAttemptAt = common.GetTimestamp() - 1
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

// 原生任务正常等待不占保存机会；视频下载临时失败只重新归档，绘图仍保持原行为。
func TestAsyncRelayNoAutomaticRetryNativeMedia(t *testing.T) {
	for _, video := range []bool{false, true} {
		t.Run(fmt.Sprintf("video_%t", video), func(t *testing.T) {
			prepareAsyncMediaController(t)
			require.NoError(t, model.UpdateOption(operation_setting.AsyncMediaRetryOption, `{"enabled":true,"max_retries":2,"status_codes":"500-599","channel_ids":[]}`))
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
			if video {
				assert.Equal(t, model.AsyncRelayTaskStatusWaiting, saved.Status)
				assert.Greater(t, saved.NextAttemptAt, common.GetTimestamp())
				assert.Zero(t, saved.FinishedAt)
				count, err = ProcessAsyncRelayTasks(context.Background(), 1)
				require.NoError(t, err)
				assert.Zero(t, count, "退避期间不能领取任务")
				assert.Equal(t, int32(1), downloads.Load())
				return
			}
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

// 通过真实领取、下载、归档和日志链路验证：只重试已有文件，原订单和余额不变。
func TestAsyncVideoArchiveRecovery(t *testing.T) {
	for _, scenario := range []string{"临时错误", "响应头超时", "断流", "权限拒绝", "文件删除", "次数耗尽", "进程中断", "完整检查点"} {
		t.Run(scenario, func(t *testing.T) {
			prepareAsyncMediaController(t)
			if os.Getenv("TEST_TASK_DB_DIALECT") != "" {
				previousDB, previousLogDB := model.DB, model.LOG_DB
				mainType, logType := common.MainDatabaseType(), common.LogDatabaseType()
				db, dialect := openTaskDialectDatabase(t, &model.AsyncRelayTask{}, &model.Task{}, &model.Channel{}, &model.User{}, &model.Token{}, &model.Log{})
				model.DB, model.LOG_DB = db, db
				common.SetDatabaseTypes(dialect, dialect)
				t.Cleanup(func() { model.DB, model.LOG_DB = previousDB, previousLogDB; common.SetDatabaseTypes(mainType, logType) })
			}
			allowPrivateTaskMediaTest(t)
			oldTimeout := asyncVideoArchiveHeaderTimeout
			asyncVideoArchiveHeaderTimeout = 20 * time.Millisecond
			// 非超时用例保留足够等待时间，避免数据库测试环境变慢引入偶发失败。
			if scenario != "响应头超时" {
				asyncVideoArchiveHeaderTimeout = time.Second
			}
			t.Cleanup(func() { asyncVideoArchiveHeaderTimeout = oldTimeout })
			video := []byte("\x00\x00\x00\x18ftypmp42\x00\x00\x00\x00mp42isom")
			var gets, posts atomic.Int32
			source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					posts.Add(1)
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				attempt := gets.Add(1)
				assert.Equal(t, "/video.mp4", r.URL.Path)
				if scenario == "次数耗尽" || attempt == 1 && scenario == "临时错误" {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				if scenario == "权限拒绝" {
					w.WriteHeader(http.StatusForbidden)
					return
				}
				if scenario == "文件删除" {
					w.WriteHeader(http.StatusGone)
					return
				}
				if scenario == "响应头超时" && attempt == 1 {
					<-r.Context().Done()
					return
				}
				w.Header().Set("Content-Type", "video/mp4")
				if scenario == "断流" && attempt == 1 {
					w.Header().Set("Content-Length", "1000")
				}
				_, err := w.Write(video)
				assert.NoError(t, err)
			}))
			t.Cleanup(source.Close)
			user := &model.User{Username: "archive-owner", Quota: 1000}
			require.NoError(t, model.DB.Create(user).Error)
			token := &model.Token{UserId: user.Id, Key: "archive-test-key", RemainQuota: 900}
			require.NoError(t, model.DB.Create(token).Error)
			channel := &model.Channel{Type: constant.ChannelTypeOpenAI, Key: "fixture", Status: common.ChannelStatusEnabled, BaseURL: &source.URL}
			require.NoError(t, model.DB.Create(channel).Error)
			parent := &model.AsyncRelayTask{UserID: user.Id, TokenID: token.Id, NodeID: common.NodeName, RequestFormat: string(relaytypes.RelayFormatTask), RequestPath: "/v1/videos"}
			require.NoError(t, parent.InsertWithLog("default", "VIDEO"))
			child := &model.Task{TaskID: "native_" + parent.TaskID, UserId: user.Id, ChannelId: channel.Id, Status: model.TaskStatusSuccess, Quota: 123, PrivateData: model.TaskPrivateData{UpstreamTaskID: "provider-fixture", ResultURL: source.URL + "/video.mp4"}}
			_, won, err := model.ClaimAsyncRelayTask(parent.ID, "archive-fixture")
			require.NoError(t, err)
			require.True(t, won)
			require.NoError(t, model.AttachAsyncRelayNativeTask(parent.TaskID, child))
			require.NoError(t, model.DB.First(parent, parent.ID).Error)
			if scenario == "进程中断" || scenario == "完整检查点" {
				updates := map[string]any{"media_attempts": 1, "updated_at": common.GetTimestamp() - 120}
				if scenario == "完整检查点" {
					path, file, err := common.CreateAsyncMediaFile()
					require.NoError(t, err)
					_, err = file.Write(video)
					require.NoError(t, err)
					require.NoError(t, file.Close())
					updates["result_file_path"], updates["result_content_type"] = path, "video/mp4"
				}
				require.NoError(t, model.DB.Model(parent).Updates(updates).Error)
				count, err := model.RecoverStaleAsyncRelayTasks(60)
				require.NoError(t, err)
				require.EqualValues(t, 1, count)
				countProcessed, err := ProcessAsyncRelayTasks(context.Background(), 1)
				require.NoError(t, err)
				require.Zero(t, countProcessed)
			} else {
				parent.Status = model.AsyncRelayTaskStatusWaiting
				won, err := parent.UpdateWithStatus(model.AsyncRelayTaskStatusProcessing)
				require.NoError(t, err)
				require.True(t, won)
			}
			limit := 2
			if scenario == "次数耗尽" {
				limit = model.AsyncVideoArchiveMaxAttempts
			}
			for step := 0; step < limit; step++ {
				// 模拟退避期到期，不靠真实长时间等待测试后台调度。
				require.NoError(t, model.DB.Model(parent).Updates(map[string]any{"next_attempt_at": common.GetTimestamp() - 1, "updated_at": common.GetTimestamp() - 10}).Error)
				processed, err := ProcessAsyncRelayTasks(context.Background(), 1)
				require.NoError(t, err)
				require.Equal(t, 1, processed)
				require.NoError(t, model.DB.First(parent, parent.ID).Error)
				if parent.Status.IsTerminal() {
					break
				}
				require.Equal(t, model.AsyncRelayTaskStatusWaiting, parent.Status)
				require.Greater(t, parent.NextAttemptAt, common.GetTimestamp())
				require.Empty(t, parent.ResultFilePath, "失败片段不能作为可见文件")
				processed, err = ProcessAsyncRelayTasks(context.Background(), 1)
				require.NoError(t, err)
				require.Zero(t, processed)
			}
			terminalFailure := scenario == "权限拒绝" || scenario == "文件删除" || scenario == "次数耗尽"
			if terminalFailure {
				require.Equal(t, model.AsyncRelayTaskStatusFailed, parent.Status)
				require.Empty(t, parent.ResultFilePath)
				want := int32(1)
				if scenario == "次数耗尽" {
					want = model.AsyncVideoArchiveMaxAttempts
				}
				assert.Equal(t, want, gets.Load())
			} else {
				require.Equal(t, model.AsyncRelayTaskStatusSucceeded, parent.Status, parent.Error)
				content, err := os.ReadFile(parent.ResultFilePath)
				require.NoError(t, err)
				assert.Equal(t, video, content)
				if scenario == "完整检查点" {
					assert.Zero(t, gets.Load())
				}
			}
			assert.Zero(t, posts.Load(), "不能再次提交生成")
			require.NoError(t, model.DB.First(user, user.Id).Error)
			assert.Equal(t, 1000, user.Quota)
			require.NoError(t, model.DB.First(token, token.Id).Error)
			assert.Equal(t, 900, token.RemainQuota)
			require.NoError(t, model.DB.First(child, child.ID).Error)
			assert.Equal(t, 123, child.Quota)
			assert.Equal(t, model.TaskStatus(model.TaskStatusSuccess), child.Status)
			var taskLog model.Task
			require.NoError(t, model.DB.First(&taskLog, parent.LogID).Error)
			assert.Equal(t, 123, taskLog.Quota)
			var logs int64
			require.NoError(t, model.LOG_DB.Model(&model.Log{}).Count(&logs).Error)
			assert.Zero(t, logs)
		})
	}
}
