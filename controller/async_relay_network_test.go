package controller

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 模拟上游已经接收生成请求，却在返回完整响应头前断开连接，重现扣费后结果未送达的情况。
func TestAsyncRelayInterruptedUpstreamDoesNotResubmitGeneration(t *testing.T) {
	var accepted atomic.Int32
	user, token := prepareAsyncCompatRelay(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.Copy(io.Discard, r.Body)
		assert.NoError(t, err)
		accepted.Add(1)
		connection, buffer, err := w.(http.Hijacker).Hijack()
		if !assert.NoError(t, err) {
			return
		}
		defer connection.Close()
		_, err = buffer.WriteString("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\n")
		assert.NoError(t, err)
		assert.NoError(t, buffer.Flush())
	}))
	previousRetry := common.RetryTimes
	common.RetryTimes = 2
	t.Cleanup(func() { common.RetryTimes = previousRetry })
	response, done, _ := beginAsyncCompatRequest(t, user, token, "/v1/images/generations", `{"model":"dall-e-3","prompt":"网络中断回归","n":1,"size":"1024x1024"}`)
	processed, err := ProcessAsyncRelayTasks(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, 1, processed)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("后台错误应按原接口返回")
	}
	assert.Equal(t, int32(1), accepted.Load(), "结果未知时重复提交会带来重复计费")
	assert.Contains(t, response.Body.String(), "上游连接")
	assert.Contains(t, response.Body.String(), "可能已计费")
	assert.NotContains(t, response.Body.String(), "fixture-upstream")
	var task model.AsyncRelayTask
	require.NoError(t, model.DB.First(&task).Error)
	assert.Equal(t, model.AsyncRelayTaskStatusFailed, task.Status)
	assert.True(t, strings.Contains(task.Error, "上游连接"))
	var taskLog model.Task
	require.NoError(t, model.DB.First(&taskLog, task.LogID).Error)
	assert.Zero(t, taskLog.Quota, "本地失败任务应保持未结算")
}
