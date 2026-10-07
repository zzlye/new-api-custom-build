package controller

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	pluginruntime "github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
