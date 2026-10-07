package controller

import (
	"context"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/video_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// 从真实队列验证失败换渠道，客户端仍然只收到最终的原协议响应。
func TestAsyncMediaRoutingSwitchesChannel(t *testing.T) {
	var first, second atomic.Int32
	user, token := prepareAsyncCompatRelay(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		first.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(500)
		fmt.Fprint(w, `{"error":{"message":"unavailable","type":"server_error"}}`)
	}))
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		second.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"created":1,"data":[{"b64_json":"%s"}]}`, asyncFixturePNG)
	}))
	t.Cleanup(upstream.Close)
	priority := int64(10)
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = 1").Update("priority", priority).Error)
	require.NoError(t, model.DB.Model(&model.Ability{}).Where("channel_id = 1").Update("priority", priority).Error)
	channel := &model.Channel{Type: constant.ChannelTypeOpenAI, Key: "second", Status: common.ChannelStatusEnabled, Name: "备用渠道", Models: "dall-e-3", Group: "default", BaseURL: &upstream.URL}
	require.NoError(t, model.DB.Create(channel).Error)
	require.NoError(t, model.DB.Create(&model.Ability{Group: "default", Model: "dall-e-3", ChannelId: channel.Id, Enabled: true}).Error)
	common.OptionMapRWMutex.Lock()
	common.OptionMap["AsyncMediaRetryPolicy"] = `{"enabled":true,"max_retries":2,"status_codes":"500","channel_ids":[]}`
	common.OptionMapRWMutex.Unlock()
	response, done, _ := beginAsyncCompatRequest(t, user, token, "/v1/images/generations", `{"model":"dall-e-3","prompt":"路由测试","n":1,"size":"1024x1024"}`)
	count, err := ProcessAsyncRelayTasks(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("没有返回最终响应")
	}
	require.Equal(t, 200, response.Code, response.Body.String())
	require.Equal(t, int32(1), first.Load())
	require.Equal(t, int32(1), second.Load())
	var task model.AsyncRelayTask
	require.NoError(t, model.DB.First(&task).Error)
	require.Equal(t, model.AsyncRelayTaskStatusSucceeded, task.Status)
	var log model.Task
	require.NoError(t, model.DB.First(&log, task.LogID).Error)
	var after model.User
	require.NoError(t, model.DB.First(&after, user.Id).Error)
	require.Positive(t, log.Quota)
	require.Equal(t, user.Quota-log.Quota, after.Quota, "只能结算最终成功的一次生成")
}

// 次数、错误码、白名单与接单保护同时经过图片及视频的实际提交链路。
func TestAsyncMediaRoutingRules(t *testing.T) {
	for _, video := range []bool{false, true} {
		for _, test := range []struct {
			name     string
			status   int
			codes    string
			budget   int
			selected []int
			body     string
			mapping  string
			mode     string
			allFail  bool
			want     [3]int32
		}{
			{name: "状态匹配后成功", status: 500, codes: "500", budget: 2, want: [3]int32{1, 1, 0}},
			{name: "两次重试最多三个渠道", status: 503, codes: "500-503", budget: 2, allFail: true, want: [3]int32{1, 1, 1}},
			{name: "一次重试最多两个渠道", status: 500, codes: "500", budget: 1, allFail: true, want: [3]int32{1, 1, 0}},
			{name: "零次不重试", status: 500, codes: "500", budget: 0, want: [3]int32{1, 0, 0}},
			{name: "渠道用尽不循环", status: 500, codes: "500", budget: 20, allFail: true, want: [3]int32{1, 1, 1}},
			{name: "错误码不匹配", status: 400, codes: "500", budget: 2, want: [3]int32{1, 0, 0}},
			{name: "根用户选择400生效", status: 400, codes: "400,500", budget: 2, want: [3]int32{1, 1, 0}},
			{name: "跳过未勾选备用渠道", status: 500, codes: "500", budget: 2, selected: []int{1, 3}, want: [3]int32{1, 0, 1}},
			{name: "首次渠道未勾选不重试", status: 500, codes: "500", budget: 2, selected: []int{2, 3}, want: [3]int32{1, 0, 0}},
			{name: "白名单用尽", status: 500, codes: "500", budget: 2, selected: []int{1}, want: [3]int32{1, 0, 0}},
			{name: "网关超时不重复扣费", status: 504, codes: "500-599", budget: 2, want: [3]int32{1, 0, 0}},
			{name: "请求超时不重发", status: 408, codes: "400-599", budget: 2, want: [3]int32{1, 0, 0}},
			{name: "响应含任务编号不重发", status: 500, codes: "500", budget: 2, body: `{"task_id":"accepted-job","error":{"message":"later error"}}`, want: [3]int32{1, 0, 0}},
			{name: "错误映射不隐藏上游500", status: 500, mapping: `{"500":"400"}`, codes: "500", budget: 2, want: [3]int32{1, 1, 0}},
			{name: "错误映射不伪造上游500", status: 400, mapping: `{"400":"500"}`, codes: "500", budget: 2, want: [3]int32{1, 0, 0}},
			{name: "响应携带job不重发", status: 500, codes: "500", budget: 2, body: `{"data":{"job":"paid"}}`, want: [3]int32{1, 0, 0}},
			{name: "成功响应解析失败不重发", status: 200, codes: "500-599", budget: 2, body: `{invalid`, want: [3]int32{1, 0, 0}},
			{name: "断网状态不明不重发", status: 500, codes: "500-599", budget: 2, mode: "disconnect", want: [3]int32{1, 0, 0}},
			{name: "响应未读完整不重发", status: 500, codes: "500-599", budget: 2, mode: "truncated", want: [3]int32{1, 0, 0}},
		} {
			t.Run(fmt.Sprintf("video_%t/%s", video, test.name), func(t *testing.T) {
				modelName, path, request := "dall-e-3", "/v1/images/generations", `{"model":"dall-e-3","prompt":"保留参数","n":1,"size":"1024x1024"}`
				format := relaytypes.RelayFormat(relaytypes.RelayFormatOpenAIImage)
				if video {
					modelName, path, request, format = "sora-2", "/v1/videos", `{"model":"sora-2","prompt":"保留参数","seconds":"4","size":"720x1280","generate_audio":false,"seed":0}`, relaytypes.RelayFormatTask
				}
				var hits [3]atomic.Int32
				handler := func(index int) http.HandlerFunc {
					return func(w http.ResponseWriter, r *http.Request) {
						hits[index].Add(1)
						data, err := io.ReadAll(r.Body)
						require.NoError(t, err)
						require.Contains(t, string(data), "保留参数")
						if index == 0 && test.mode == "disconnect" {
							conn, _, err := w.(http.Hijacker).Hijack()
							require.NoError(t, err)
							conn.Close()
							return
						}
						w.Header().Set("Content-Type", "application/json")
						if index == 0 || test.allFail {
							if test.mode == "truncated" {
								w.Header().Set("Content-Length", "99999")
							}
							w.WriteHeader(test.status)
							body := test.body
							if body == "" {
								body = `{"error":{"message":"fixture rejected","type":"server_error"}}`
							}
							fmt.Fprint(w, body)
							return
						}
						if video {
							fmt.Fprint(w, `{"id":"accepted-provider-task","status":"queued","model":"sora-2"}`)
						} else {
							fmt.Fprintf(w, `{"data":[{"b64_json":"%s"}]}`, asyncFixturePNG)
						}
					}
				}
				user, token := prepareAsyncCompatRelay(t, handler(0), modelName)
				if test.mapping != "" {
					require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = 1").Update("status_code_mapping", test.mapping).Error)
				}
				for index := 0; index < 3; index++ {
					priority := int64(30 - index*10)
					if index == 0 {
						require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = 1").Update("priority", priority).Error)
						require.NoError(t, model.DB.Model(&model.Ability{}).Where("channel_id = 1").Update("priority", priority).Error)
						continue
					}
					upstream := httptest.NewServer(handler(index))
					t.Cleanup(upstream.Close)
					channel := &model.Channel{Type: constant.ChannelTypeOpenAI, Key: "fixture", Name: fmt.Sprint("备用", index), BaseURL: &upstream.URL, Models: modelName, Group: "default", Priority: &priority, Status: common.ChannelStatusEnabled}
					require.NoError(t, model.DB.Create(channel).Error)
					require.NoError(t, model.DB.Create(&model.Ability{Group: "default", Model: modelName, ChannelId: channel.Id, Enabled: true, Priority: &priority}).Error)
				}
				policy, err := common.Marshal(operation_setting.AsyncMediaRetryPolicy{Enabled: true, MaxRetries: test.budget, StatusCodes: test.codes, ChannelIDs: test.selected})
				require.NoError(t, err)
				require.NoError(t, model.UpdateOption(operation_setting.AsyncMediaRetryOption, string(policy)))
				response, done, _ := beginAsyncCompatRequest(t, user, token, path, request, format)
				_, err = ProcessAsyncRelayTasks(context.Background(), 1)
				require.NoError(t, err)
				select {
				case <-done:
				case <-time.After(5 * time.Second):
					t.Fatal("没有返回最终响应")
				}
				for index, want := range test.want {
					require.Equal(t, want, hits[index].Load(), "渠道%d", index+1)
				}
				var task model.AsyncRelayTask
				require.NoError(t, model.DB.First(&task).Error)
				require.Equal(t, task.TaskID, response.Header().Get("X-New-Api-Task-Id"))
				var details model.AsyncRelayRequestDetails
				require.NoError(t, common.UnmarshalJsonStr(task.RequestDetails, &details))
				require.NotEmpty(t, details.RoutingEvents)
				var after model.User
				require.NoError(t, model.DB.First(&after, user.Id).Error)
				var log model.Task
				require.NoError(t, model.DB.First(&log, task.LogID).Error)
				require.Equal(t, user.Quota-log.Quota, after.Quota, "失败不多扣费，成功只结算一次")
				if !test.allFail && test.want[1]+test.want[2] > 0 {
					require.Equal(t, 200, response.Code, response.Body.String())
					require.Positive(t, log.Quota)
					if video {
						require.NotEmpty(t, task.LinkedTaskID)
						require.Equal(t, model.AsyncRelayTaskStatusWaiting, task.Status)
					} else {
						require.Equal(t, model.AsyncRelayTaskStatusSucceeded, task.Status)
					}
				} else {
					require.Zero(t, log.Quota)
				}
			})
		}
	}
}

// 三种真实数据库验证配置持久化、渠道过滤与路由日志租约，不改表结构。
func TestAsyncMediaRoutingDatabaseMatrix(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			var driver gorm.Dialector
			switch dialect {
			case "sqlite":
				driver = sqlite.Open(":memory:")
			case "mysql":
				if os.Getenv("TEST_MYSQL_DSN") == "" {
					t.Skip("未设置测试 MySQL")
				}
				driver = mysql.Open(os.Getenv("TEST_MYSQL_DSN"))
			case "postgres":
				if os.Getenv("TEST_POSTGRES_DSN") == "" {
					t.Skip("未设置测试 PostgreSQL")
				}
				driver = postgres.Open(os.Getenv("TEST_POSTGRES_DSN"))
			}
			db, err := gorm.Open(driver, &gorm.Config{NamingStrategy: schema.NamingStrategy{TablePrefix: "media_retry_test_"}})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			oldDB, oldCache := model.DB, common.MemoryCacheEnabled
			common.OptionMapRWMutex.Lock()
			oldOptions := maps.Clone(common.OptionMap)
			common.OptionMap = map[string]string{}
			common.OptionMapRWMutex.Unlock()
			model.DB, common.MemoryCacheEnabled = db, false
			t.Cleanup(func() {
				require.NoError(t, db.Migrator().DropTable(&model.AsyncRelayTask{}, &model.Task{}, &model.Channel{}, &model.Ability{}, &model.Option{}))
				model.DB, common.MemoryCacheEnabled = oldDB, oldCache
				common.OptionMapRWMutex.Lock()
				common.OptionMap = oldOptions
				common.OptionMapRWMutex.Unlock()
				require.NoError(t, sqlDB.Close())
			})
			require.NoError(t, db.AutoMigrate(&model.AsyncRelayTask{}, &model.Task{}, &model.Channel{}, &model.Ability{}, &model.Option{}))
			query := "SELECT version()"
			if dialect == "sqlite" {
				query = "SELECT sqlite_version()"
			}
			var version string
			require.NoError(t, db.Raw(query).Scan(&version).Error)
			t.Logf("数据库版本: %s", version)
			config := `{"enabled":true,"max_retries":2,"status_codes":"500,502-503","channel_ids":[1,2,3]}`
			require.NoError(t, model.UpdateOption(operation_setting.AsyncMediaRetryOption, config))
			var saved model.Option
			require.NoError(t, db.First(&saved).Error)
			require.Equal(t, config, saved.Value)
			for _, invalid := range []string{`{}`, `{"max_retries":21,"status_codes":"500"}`, `{"max_retries":1.5,"status_codes":"500"}`, `{"status_codes":"200"}`, `{"status_codes":"600"}`, `{"status_codes":"503-500"}`, `{"status_codes":"500","channel_ids":[1,1]}`, `{"status_codes":"500","channel_ids":[0]}`} {
				require.Error(t, model.UpdateOption(operation_setting.AsyncMediaRetryOption, invalid))
				require.Equal(t, 2, operation_setting.GetAsyncMediaRetryPolicy().MaxRetries)
			}
			for id := 1; id <= 5; id++ {
				group, name, status := "default", "fixture-image", common.ChannelStatusEnabled
				if id == 3 {
					status = common.ChannelStatusManuallyDisabled
				}
				if id == 4 {
					group = "private"
				}
				if id == 5 {
					name = "another-model"
				}
				channel := &model.Channel{Id: id, Type: constant.ChannelTypeOpenAI, Key: "test-only", Name: fmt.Sprint(id), Models: name, Group: group, Status: status}
				require.NoError(t, db.Create(channel).Error)
				require.NoError(t, db.Create(&model.Ability{ChannelId: id, Model: name, Group: group, Enabled: true}).Error)
			}
			for _, cache := range []bool{false, true} {
				common.MemoryCacheEnabled = cache
				model.InitChannelCache()
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
				c.Set("use_channel", []string{"1"})
				param := &service.RetryParam{Ctx: c, TokenGroup: "default", ModelName: "fixture-image", Retry: common.GetPointer(1)}
				channel, group, err := service.SelectAsyncMediaRetryChannel(param)
				require.NoError(t, err)
				require.NotNil(t, channel)
				require.Equal(t, 2, channel.Id)
				require.Equal(t, "default", group)
				c.Set("use_channel", []string{"1", "2"})
				channel, _, err = service.SelectAsyncMediaRetryChannel(param)
				require.NoError(t, err)
				require.Nil(t, channel)
			}
			common.MemoryCacheEnabled = false
			task := &model.AsyncRelayTask{UserID: 31, Status: model.AsyncRelayTaskStatusProcessing, WorkerID: "worker-1", RequestDetails: `{"prompt":"保留提示词"}`}
			require.NoError(t, task.InsertWithLog("default", "IMAGE"))
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			service.RequestPolicy(c).BeginAttempt(&model.Channel{Id: 2}, "default")
			require.NoError(t, persistAsyncRelayRouting(task, c))
			require.NoError(t, persistAsyncRelayRouting(task, c), "相同事件重复保存仍属于当前执行者")
			var loaded model.AsyncRelayTask
			require.NoError(t, db.First(&loaded, task.ID).Error)
			require.Contains(t, loaded.RequestDetails, `"channel_id":2`)
			require.NoError(t, db.Model(&loaded).Update("worker_id", "worker-2").Error)
			require.Error(t, persistAsyncRelayRouting(task, c), "旧执行者不得覆盖新租约")
			require.NoError(t, db.Model(&loaded).Update("status", model.AsyncRelayTaskStatusSucceeded).Error)
			loaded.Status = model.AsyncRelayTaskStatusSucceeded
			require.NoError(t, model.ExpireAsyncRelayTaskFiles(&loaded))
			require.NoError(t, db.First(&loaded, task.ID).Error)
			require.Contains(t, loaded.RequestDetails, `"channel_id":2`)
			require.Contains(t, loaded.RequestDetails, "保留提示词")
			require.NoError(t, db.Migrator().DropTable(&model.Option{}))
			require.Error(t, model.UpdateOption(operation_setting.AsyncMediaRetryOption, operation_setting.DefaultAsyncMediaRetryJSON))
			require.True(t, operation_setting.GetAsyncMediaRetryPolicy().Enabled, "持久化失败不能修改内存设置")
		})
	}
}

func TestAsyncMediaRoutingConfiguredVideo(t *testing.T) {
	var first, second atomic.Int32
	user, token := prepareAsyncCompatRelay(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		first.Add(1)
		require.Equal(t, "/first/create", r.URL.Path)
		w.WriteHeader(500)
		fmt.Fprint(w, `{"error":"unavailable"}`)
	}), "future-video")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		second.Add(1)
		require.Equal(t, "/second/create", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(202)
		fmt.Fprint(w, `{"data":{"job":"provider-42"}}`)
	}))
	t.Cleanup(upstream.Close)
	channel := &model.Channel{Type: constant.ChannelTypeOpenAI, Key: "fixture", Name: "视频备用", BaseURL: &upstream.URL, Models: "future-video", Group: "default", Status: common.ChannelStatusEnabled}
	require.NoError(t, model.DB.Create(channel).Error)
	require.NoError(t, model.DB.Create(&model.Ability{Group: "default", Model: "future-video", ChannelId: channel.Id, Enabled: true}).Error)
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = 1").Update("priority", 10).Error)
	require.NoError(t, model.DB.Model(&model.Ability{}).Where("channel_id = 1").Update("priority", 10).Error)
	for id, path := range map[int]string{1: "/first/create", channel.Id: "/second/create"} {
		p := video_setting.Presets()["reference_object"]
		p.Enabled = true
		p.SubmitPath = path
		p.Response.ID = "data.job"
		data, err := common.Marshal(p)
		require.NoError(t, err)
		require.NoError(t, model.UpdateOption(video_setting.Key(id), string(data)))
	}
	require.NoError(t, model.UpdateOption(operation_setting.AsyncMediaRetryOption, `{"enabled":true,"max_retries":2,"status_codes":"500","channel_ids":[]}`))
	response, done, _ := beginAsyncCompatRequest(t, user, token, "/v1/videos?async=true", `{"model":"future-video","prompt":"配置换渠道","duration":7}`, relaytypes.RelayFormatTask)
	<-done
	require.Equal(t, 202, response.Code)
	_, err := ProcessAsyncRelayTasks(context.Background(), 1)
	require.NoError(t, err)
	require.EqualValues(t, 1, first.Load())
	require.EqualValues(t, 1, second.Load())
	var task model.AsyncRelayTask
	require.NoError(t, model.DB.First(&task).Error)
	require.Equal(t, model.AsyncRelayTaskStatusWaiting, task.Status, task.Error)
	child, found, err := model.GetByTaskId(user.Id, task.LinkedTaskID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, channel.Id, child.ChannelId)
	require.Equal(t, "/second/create", child.PrivateData.VideoProtocol.SubmitPath)
}

func TestAsyncMediaRoutingRootSettingsAndDetails(t *testing.T) {
	prepareAsyncMediaController(t)
	require.NoError(t, model.DB.AutoMigrate(&model.AuditLog{}))
	value := `{"enabled":true,"max_retries":3,"status_codes":"500-503","channel_ids":[1,2]}`
	body, err := common.Marshal(map[string]any{"key": operation_setting.AsyncMediaRetryOption, "value": value})
	require.NoError(t, err)
	for _, role := range []int{0, common.RoleCommonUser, common.RoleAdminUser} {
		response := asyncControllerRequest(UpdateOption, http.MethodPut, "/api/option/", 31, role, nil, string(body))
		require.Equal(t, 403, response.Code)
		response = asyncControllerRequest(GetAsyncMediaRetryChannels, http.MethodGet, "/", 31, role, nil, "")
		require.Equal(t, 403, response.Code)
	}
	response := asyncControllerRequest(UpdateOption, http.MethodPut, "/api/option/", 31, common.RoleRootUser, nil, string(body))
	require.Contains(t, response.Body.String(), `"success":true`)
	require.Equal(t, 3, operation_setting.GetAsyncMediaRetryPolicy().MaxRetries)
	event := dto.TaskRoutingEvent{Attempt: 1, ChannelID: 71, Status: 500}
	event.Decision.Action = "retry"
	input, err := common.Marshal(model.AsyncRelayRequestDetails{Prompt: "只保存文字", RoutingEvents: []dto.TaskRoutingEvent{event}})
	require.NoError(t, err)
	task := &model.AsyncRelayTask{UserID: 31, RequestDetails: string(input), Status: model.AsyncRelayTaskStatusSucceeded, FinishedAt: common.GetTimestamp()}
	require.NoError(t, task.InsertWithLog("default", "IMAGE"))
	require.NoError(t, model.ExpireAsyncRelayTaskFiles(task))
	params := gin.Params{{Key: "task_id", Value: task.TaskID}}
	response = asyncControllerRequest(GetAsyncRelayTaskDetails, http.MethodGet, "/", 31, common.RoleRootUser, params, "")
	require.Contains(t, response.Body.String(), `"channel_id":71`)
	response = asyncControllerRequest(GetAsyncRelayTaskDetails, http.MethodGet, "/", 31, common.RoleCommonUser, params, "")
	require.NotContains(t, response.Body.String(), `routing_events`)
}

func TestAsyncMediaRoutingRespectsRequestLocks(t *testing.T) {
	prepareAsyncMediaController(t)
	require.NoError(t, model.UpdateOption(operation_setting.AsyncMediaRetryOption, `{"enabled":true,"max_retries":2,"status_codes":"500","channel_ids":[]}`))
	for _, lock := range []string{"pin", "fixed", "strict", "stream", "cancel"} {
		t.Run(lock, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
			c.Set(model.AsyncRelayContextKey, "fixture")
			c.Set("channel_id", 1)
			require.NoError(t, service.BeginAsyncMediaAttempt(c))
			service.ObserveAsyncMediaHTTPFailure(c.Request.Context(), &http.Response{StatusCode: 500, Header: http.Header{}}, []byte(`{"error":"failed"}`), nil)
			require.Equal(t, "retry", service.DecideAsyncMediaRetry(c, 2, false).Action)
			switch lock {
			case "pin":
				service.GetChannelConstraints(c).AddPin(dto.ChannelPin{ChannelId: 1, Source: dto.PinSourceOriginTask})
			case "fixed":
				c.Set("specific_channel_id", "1")
			case "strict":
				c.Set("channel_affinity_skip_retry_on_failure", true)
			case "stream":
				c.Writer.WriteHeaderNow()
			case "cancel":
				ctx, cancel := context.WithCancel(c.Request.Context())
				cancel()
				c.Request = c.Request.WithContext(ctx)
			}
			require.Equal(t, "stop", service.DecideAsyncMediaRetry(c, 2, false).Action)
		})
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	c.Set(model.AsyncRelayContextKey, "fixture")
	require.NoError(t, service.BeginAsyncMediaAttempt(c))
	service.ObserveAsyncMediaHTTPFailure(c.Request.Context(), &http.Response{StatusCode: 500}, []byte(`{"data":{"custom_ticket":"paid-task"}}`), nil, "data.custom_ticket")
	require.Equal(t, "submission_not_rejected", service.DecideAsyncMediaRetry(c, 2, false).Reason)
}
