package controller

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	pluginruntime "github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/relay"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// MJ 新接口经过真实后台队列，验证模型映射、四图一次收费及断线后只查询原任务。
func TestAsyncRelayMidjourneyNativeLifecycle(t *testing.T) {
	for _, scenario := range []struct {
		model       string
		channelType int
		failed      bool
		shape       string
	}{
		{"mj-v8.2", constant.ChannelTypeMidjourney, false, "wrapped"},
		{"mj-niji7", constant.ChannelTypeMidjourneyPlus, false, "wrapped"},
		{"mj-niji7", constant.ChannelTypeMidjourney, true, "wrapped"},
		{"mj-v8.2", constant.ChannelTypeMidjourney, false, "top"},
		{"mj-niji7", constant.ChannelTypeMidjourneyPlus, false, "array"},
		{"mj-niji7", constant.ChannelTypeMidjourney, true, "array"},
	} {
		t.Run(fmt.Sprintf("%s_%s_失败_%v", scenario.model, scenario.shape, scenario.failed), func(t *testing.T) {
			generation := pluginruntime.DefaultRegistry.Generation()
			binding, found := generation.LookupDeclaredRoute(http.MethodPost, "/v1/midjourney/generations")
			require.True(t, found, "MJ 新接口必须注册到任务插件路由")
			require.Equal(t, "midjourney", binding.Plugin.Meta.Key)
			// 本地替身使用回环地址和随机端口，测试结束后恢复媒体下载策略。
			fetchSetting := system_setting.GetFetchSetting()
			previousFetchSetting := *fetchSetting
			fetchSetting.AllowPrivateIp = true
			fetchSetting.AllowedPorts = []string{"1-65535"}
			t.Cleanup(func() { *fetchSetting = previousFetchSetting })
			var submits, queries, downloads atomic.Int32
			user, token := prepareAsyncCompatRelay(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/v1/midjourney/generations":
					submits.Add(1)
					assert.Equal(t, "Bearer fixture-upstream", r.Header.Get("Authorization"))
					var body map[string]any
					assert.NoError(t, common.DecodeJson(r.Body, &body))
					assert.Equal(t, "上游自定义名称", body["model"])
					assert.Equal(t, "  保留原提示词 --ar 16:9  ", body["prompt"])
					assert.Equal(t, "16:9", body["size"])
					assert.Equal(t, false, body["raw"])
					assert.Equal(t, float64(1), body["n"])
					if scenario.shape == "wrapped" {
						assert.Equal(t, float64(0.5), body["quality"])
						assert.Equal(t, []any{"https://example.com/reference.jpg", "data:image/png;base64," + asyncFixturePNG}, body["images"])
					} else {
						// 真实文生图没有参考图；默认品质不应触发上游错误的图片校验。
						assert.NotContains(t, body, "quality")
						assert.NotContains(t, body, "image")
						assert.NotContains(t, body, "images")
					}
					switch scenario.shape {
					case "array":
						fmt.Fprint(w, `{"code":200,"data":[{"task_id":"private-mj-task","status":"submitted"}]}`)
					case "top":
						fmt.Fprint(w, `{"task_id":"private-mj-task","status":"submitted"}`)
					default:
						fmt.Fprint(w, `{"data":{"task_id":"private-mj-task","status":"submitted","progress":"0%"}}`)
					}
				case "/v1/tasks/private-mj-task":
					queryCount := queries.Add(1)
					assert.Equal(t, "Bearer fixture-upstream", r.Header.Get("Authorization"))
					if scenario.shape != "wrapped" && queryCount == 1 {
						fmt.Fprint(w, `{"id":"private-mj-task","task_id":"private-mj-task","status":"processing","progress":"59%"}`)
						return
					}
					var result string
					if scenario.failed {
						result = `{"task_id":"private-mj-task","status":"failed","error_code":"service_error","error_message":"上游拒绝生成"}`
					} else {
						result = fmt.Sprintf(`{"task_id":"private-mj-task","status":"completed","progress":"100%%","result":{"data":{"image_urls":["http://%s/image/1","http://%s/image/2","http://%s/image/3","http://%s/image/4"],"grid_image_url":"http://%s/grid"}}}`, r.Host, r.Host, r.Host, r.Host, r.Host)
					}
					if scenario.shape == "wrapped" {
						fmt.Fprintf(w, `{"data":%s}`, result)
					} else {
						fmt.Fprint(w, result)
					}
				case "/image/1", "/image/2", "/image/3", "/image/4":
					downloads.Add(1)
					assert.Empty(t, r.Header.Get("Authorization"), "媒体下载不能携带渠道密钥")
					w.Header().Set("Content-Type", "image/png")
					png, err := base64.StdEncoding.DecodeString(asyncFixturePNG)
					assert.NoError(t, err)
					_, err = w.Write(png)
					assert.NoError(t, err)
				default:
					t.Errorf("不应调用旧 MJ 路径或下载拼图: %s", r.URL.Path)
					http.NotFound(w, r)
				}
			}), scenario.model)
			require.NoError(t, model.DB.Model(&model.Channel{}).Where("models = ?", scenario.model).Updates(map[string]any{
				"type": scenario.channelType, "model_mapping": fmt.Sprintf(`{"%s":"上游自定义名称"}`, scenario.model),
			}).Error)
			previousFactory := service.GetTaskAdaptorFunc
			service.GetTaskAdaptorFunc = func(platform constant.TaskPlatform) service.TaskPollingAdaptor { return relay.GetTaskAdaptor(platform) }
			t.Cleanup(func() { service.GetTaskAdaptorFunc = previousFactory })
			body := fmt.Sprintf(`{"model":"%s","prompt":"  保留原提示词 --ar 16:9  ","size":"16:9","raw":false,"n":1,"quality":0.5,"images":["https://example.com/reference.jpg","data:image/png;base64,%s"]}`, scenario.model, asyncFixturePNG)
			if scenario.shape != "wrapped" {
				body = fmt.Sprintf(`{"model":"%s","prompt":"  保留原提示词 --ar 16:9  ","size":"16:9","raw":false,"n":1}`, scenario.model)
				if scenario.model == "mj-niji7" {
					body = strings.TrimSuffix(body, "}") + `,"quality":1}`
				}
			}
			response, done, _ := beginAsyncCompatRequest(t, user, token, "/v1/midjourney/generations", body, relaytypes.RelayFormatTask)
			processed, err := ProcessAsyncRelayTasks(t.Context(), 1)
			require.NoError(t, err)
			require.Equal(t, 1, processed)
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("MJ 提交未返回任务编号")
			}
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			assert.NotContains(t, response.Body.String(), "private-mj-task")
			var parent model.AsyncRelayTask
			require.NoError(t, model.DB.First(&parent).Error)
			require.NotEmpty(t, parent.LinkedTaskID)
			var child model.Task
			require.NoError(t, model.DB.Where("task_id = ?", parent.LinkedTaskID).First(&child).Error)
			assert.Equal(t, constant.TaskPlatform("midjourney"), child.Platform)
			assert.Equal(t, scenario.model, child.Properties.OriginModelName)
			assert.Equal(t, "上游自定义名称", child.Properties.UpstreamModelName)
			var taskLog model.Task
			require.NoError(t, model.DB.First(&taskLog, parent.LogID).Error)
			assert.Equal(t, "IMAGE", taskLog.Action)
			service.DispatchPlatformUpdate(t.Context(), child.Platform, map[int][]string{child.ChannelId: {"private-mj-task"}}, map[string]*model.Task{"private-mj-task": &child})
			require.NoError(t, model.DB.First(&child, child.ID).Error)
			if scenario.shape != "wrapped" {
				assert.Equal(t, model.TaskStatus(model.TaskStatusInProgress), child.Status)
				assert.Equal(t, "59%", child.Progress)
				service.DispatchPlatformUpdate(t.Context(), child.Platform, map[int][]string{child.ChannelId: {"private-mj-task"}}, map[string]*model.Task{"private-mj-task": &child})
				require.NoError(t, model.DB.First(&child, child.ID).Error)
			}
			// 恢复执行只查询已经持久化的子任务，不得再次提交上游。
			require.NoError(t, model.DB.Model(&model.AsyncRelayTask{}).Where("id = ?", parent.ID).Updates(map[string]any{"next_attempt_at": 0, "updated_at": common.GetTimestamp() - 10}).Error)
			processed, err = ProcessAsyncRelayTasks(t.Context(), 1)
			require.NoError(t, err)
			require.Equal(t, 1, processed)
			require.NoError(t, model.DB.First(&parent, parent.ID).Error)
			assert.Equal(t, int32(1), submits.Load())
			expectedQueries := int32(1)
			if scenario.shape != "wrapped" {
				expectedQueries = 2
			}
			assert.Equal(t, expectedQueries, queries.Load())
			query := func(tokenID int) *httptest.ResponseRecorder {
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				c.Request = httptest.NewRequest(http.MethodGet, "/v1/tasks/"+parent.TaskID, nil)
				c.Params = gin.Params{{Key: "key", Value: parent.TaskID}}
				c.Set("id", user.Id)
				c.Set("token_id", tokenID)
				GetTask(c)
				return recorder
			}
			result := query(token.Id)
			require.Equal(t, http.StatusOK, result.Code, result.Body.String())
			assert.NotContains(t, result.Body.String(), "private-mj-task")
			assert.Equal(t, http.StatusNotFound, query(token.Id+1).Code)
			var after model.User
			require.NoError(t, model.DB.First(&after, user.Id).Error)
			if scenario.failed {
				assert.Equal(t, model.AsyncRelayTaskStatusFailed, parent.Status)
				assert.Contains(t, result.Body.String(), "上游拒绝生成")
				assert.Equal(t, user.Quota, after.Quota)
				assert.Zero(t, downloads.Load())
			} else {
				assert.Equal(t, model.AsyncRelayTaskStatusSucceeded, parent.Status, parent.Error)
				assert.Equal(t, int32(4), downloads.Load())
				assert.Equal(t, int64(4), gjson.Get(result.Body.String(), "result.data.image_urls.#").Int())
				assert.Equal(t, int(0.04*common.QuotaPerUnit), user.Quota-after.Quota)
				for _, url := range gjson.Get(result.Body.String(), "result.data.image_urls").Array() {
					assert.Contains(t, url.String(), "/task-media/")
				}
				assert.Equal(t, "completed", gjson.Get(result.Body.String(), "data.status").String())
				assert.Equal(t, int64(4), gjson.Get(result.Body.String(), "data.result.data.image_urls.#").Int())
				preview := asyncControllerRequest(GetAsyncRelayMedia, http.MethodGet, "/", user.Id, common.RoleCommonUser, gin.Params{{Key: "task_id", Value: parent.TaskID}, {Key: "index", Value: "0"}}, "")
				require.Equal(t, http.StatusOK, preview.Code, preview.Body.String())
				assert.Contains(t, preview.Header().Get("Content-Type"), "image/png")
			}
		})
	}
}

func TestMidjourneyNativeValidation(t *testing.T) {
	plugin, found := pluginruntime.DefaultRegistry.Generation().Get("midjourney")
	require.True(t, found)
	for _, body := range []string{
		`{"model":"mj-niji7","prompt":"cat","n":4}`,
		`{"model":"mj-niji7","prompt":"cat","raw":"false"}`,
		`{"model":"mj-niji7","prompt":"cat","quality":3}`,
		`{"model":"mj-niji7","prompt":"cat","size":"1024x1024"}`,
		`{"model":"mj-niji7","prompt":"cat","images":["http://a","http://b","http://c","http://d","http://e","http://f"]}`,
		`{"model":"mj-niji7","prompt":"cat","action":"upscale","task_id":"private-id"}`,
	} {
		t.Run(body, func(t *testing.T) {
			var value map[string]any
			require.NoError(t, common.DecodeJson(strings.NewReader(body), &value))
			_, err := plugin.Engine.CallMember(t.Context(), "native", "decodeSubmit", map[string]any{"body": map[string]any{"kind": "json", "value": value}})
			require.Error(t, err)
		})
	}
	unknown, err := plugin.Engine.Call(t.Context(), "parseTaskResult", map[string]any{}, map[string]any{"data": map[string]any{"status": "unrecognized"}})
	require.NoError(t, err)
	encoded, err := common.Marshal(unknown)
	require.NoError(t, err)
	assert.Equal(t, "UNKNOWN", gjson.GetBytes(encoded, "status").String())
}

// 新版插件必须继续经过内部队列，对普通图片客户端保持同步响应与一次结算。
func TestAsyncRelayPluginImagePreservesResponseBillingAndExpiry(t *testing.T) {
	for _, failover := range []bool{false, true} {
		t.Run(fmt.Sprint("换渠道_", failover), func(t *testing.T) {
			var calls atomic.Int32
			user, token := prepareAsyncCompatRelay(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if calls.Add(1) == 1 && failover {
					w.WriteHeader(500)
					fmt.Fprint(w, `{"error":"渠道拒绝"}`)
					return
				}
				assert.Equal(t, "/fixture/images", r.URL.Path)
				w.Header().Set("Content-Type", "application/json")
				_, err := fmt.Fprintf(w, `{"data":[{"b64_json":"%s"}]}`, asyncFixturePNG)
				assert.NoError(t, err)
			}), "async-fixture-image")
			require.NoError(t, model.DB.Model(&model.Channel{}).Where("models = ?", "async-fixture-image").Updates(map[string]any{
				"type": constant.ChannelTypeTaskPlugin, "setting": `{"task_plugin_key":"async-fixture"}`,
			}).Error)
			if failover {
				var channel model.Channel
				require.NoError(t, model.DB.First(&channel).Error)
				channel.Id = 0
				require.NoError(t, model.DB.Create(&channel).Error)
				require.NoError(t, model.DB.Create(&model.Ability{ChannelId: channel.Id, Group: "default", Model: "async-fixture-image", Enabled: true}).Error)
				require.NoError(t, model.UpdateOption(operation_setting.AsyncMediaRetryOption, `{"enabled":true,"max_retries":2,"status_codes":"500","channel_ids":[]}`))
			}
			_, err := pluginruntime.DefaultRegistry.Register(`
export const meta = {
  apiVersion: 1, key: "async-fixture", name: "内部队列兼容性验证",
  version: "1.0.0", author: {name: "Test"}, models: ["async-fixture-image"],
  fetchMode: "per_task", protocols: ["openai_image"]
};
export function buildSubmitRequest(ctx) {
  return {url: ctx.baseUrl + "/fixture/images", method: "POST", body: ctx.requestBody};
}
export function parseSubmitResponse(ctx, resp) {
  return {taskId: ctx.publicTaskId, taskData: resp.body, immediate: {status: "SUCCESS", progress: "100%"}};
}
export function buildQueryRequest(ctx) { return {url: ctx.baseUrl + "/fixture/query"}; }
export function parseTaskResult() { throw new Error("即时结果不应重复请求"); }
export function listArtifacts() { return []; }
export function buildContentRequest() { throw new Error("artifact_not_found"); }
export const protocols = {openai_image: {
  decodeRequest(ctx) { return {kind: "submit", model: ctx.model, requestBody: ctx.body.value}; },
  render(ctx, task) { return task.data; }
}};
`, pluginruntime.Options{})
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, pluginruntime.DefaultRegistry.Unregister("async-fixture")) })
			response, done, _ := beginAsyncCompatRequest(t, user, token, "/v1/images/generations", `{"model":"async-fixture-image","prompt":"一只猫","n":1}`)
			processed, err := ProcessAsyncRelayTasks(context.Background(), 1)
			require.NoError(t, err)
			require.Equal(t, 1, processed)
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("普通图片客户端未收到插件结果")
			}
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			assert.Contains(t, response.Body.String(), asyncFixturePNG)
			assert.NotContains(t, response.Body.String(), "poll_url")
			expectedCalls := int32(1)
			if failover {
				expectedCalls = 2
			}
			assert.Equal(t, expectedCalls, calls.Load())
			var parent model.AsyncRelayTask
			require.NoError(t, model.DB.First(&parent).Error)
			require.Equal(t, model.AsyncRelayTaskStatusSucceeded, parent.Status, parent.Error)
			var children []model.Task
			require.NoError(t, model.DB.Where("async_parent_id = ?", parent.TaskID).Find(&children).Error)
			require.Len(t, children, 1)
			child := children[0]
			require.NotNil(t, child.PrivateData.Execution)
			require.NotNil(t, child.PrivateData.Execution.TaskPlugin)
			assert.Equal(t, "async-fixture", child.PrivateData.Execution.TaskPlugin.Key)
			// 即时插件结果只保存在父任务的受控媒体目录，任务日志不重复展示子任务。
			assert.False(t, child.ResultRetrievable())
			assert.Len(t, model.TaskGetAllUserTask(user.Id, 0, 10, model.SyncTaskQueryParams{}), 1)
			params := gin.Params{{Key: "task_id", Value: parent.TaskID}, {Key: "index", Value: "0"}}
			preview := asyncControllerRequest(GetAsyncRelayMedia, http.MethodGet, "/", user.Id, common.RoleCommonUser, params, "")
			require.Equal(t, http.StatusOK, preview.Code, preview.Body.String())
			assert.Contains(t, preview.Header().Get("Content-Type"), "image/png")
			var log model.Task
			require.NoError(t, model.DB.First(&log, parent.LogID).Error)
			var after model.User
			require.NoError(t, model.DB.First(&after, user.Id).Error)
			assert.Greater(t, log.Quota, 0)
			assert.Equal(t, user.Quota-log.Quota, after.Quota)
			// 到期只删除媒体；插件查询入口失效，文字日志和收费记录仍存在。
			require.NoError(t, model.ExpireAsyncRelayTaskFiles(&parent))
			expired := asyncControllerRequest(GetAsyncRelayMedia, http.MethodGet, "/", user.Id, common.RoleCommonUser, params, "")
			assert.Equal(t, http.StatusGone, expired.Code)
			require.NoError(t, model.DB.First(&child, child.ID).Error)
			assert.False(t, child.ResultRetrievable())
			assert.Empty(t, child.Data)
			require.NoError(t, model.DB.First(&log, parent.LogID).Error)
			assert.Equal(t, model.TaskStatus(model.TaskStatusSuccess), log.Status)
		})
	}
}
