package controller

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 用明确的放行信号模拟长请求，验证并发设置控制领取而不是重复提交或中止在途任务。
func TestAsyncRelayConcurrencySettingControlsQueuedTasks(t *testing.T) {
	arrivals := make(chan struct{}, 8)
	release := make(chan struct{})
	var closeOnce sync.Once
	user, token := prepareAsyncCompatRelay(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		arrivals <- struct{}{}
		<-release
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"data":[{"b64_json":"%s"}]}`, asyncFixturePNG)
	}))
	var workers sync.WaitGroup
	t.Cleanup(func() { closeOnce.Do(func() { close(release) }); workers.Wait() })
	require.NoError(t, model.UpdateOption("AsyncMediaConcurrency", "1"))
	for i := 0; i < 6; i++ {
		body := `{"model":"dall-e-3","prompt":"队列测试","size":"1024x1024"}`
		response, done, _ := beginAsyncCompatRequest(t, user, token, "/v1/images/generations?async=true", body)
		<-done
		require.Equal(t, http.StatusAccepted, response.Code)
	}
	startWorker := func() {
		workers.Add(1)
		go func() {
			defer workers.Done()
			_, err := ProcessAsyncRelayTasks(context.Background(), 1)
			assert.NoError(t, err)
		}()
		select {
		case <-arrivals:
		case <-time.After(5 * time.Second):
			t.Fatal("有可用名额时任务没有发往替身")
		}
	}
	startWorker()
	assertQueued := func() {
		t.Helper()
		result := make(chan int, 1)
		workers.Add(1)
		go func() {
			defer workers.Done()
			count, err := ProcessAsyncRelayTasks(context.Background(), 1)
			assert.NoError(t, err)
			result <- count
		}()
		select {
		case count := <-result:
			require.Zero(t, count)
		case <-arrivals:
			t.Fatal("没有可用并发名额时仍然向上游提交了任务")
		case <-time.After(5 * time.Second):
			t.Fatal("任务领取既未返回排队也未提交")
		}
	}
	assertQueued()
	require.NoError(t, model.UpdateOption("AsyncMediaConcurrency", "2"))
	startWorker()
	require.NoError(t, model.UpdateOption("AsyncMediaConcurrency", "1"))
	assertQueued()
	require.NoError(t, model.UpdateOption("AsyncMediaConcurrency", "0"))
	for i := 0; i < 4; i++ {
		startWorker()
	}
	closeOnce.Do(func() { close(release) })
	workers.Wait()
	var count int64
	require.NoError(t, model.DB.Model(&model.AsyncRelayTask{}).Where("status = ?", model.AsyncRelayTaskStatusSucceeded).Count(&count).Error)
	assert.EqualValues(t, 6, count, "解除额外上限后应超过原先固定四个并发并全部完成")
}

func TestAsyncMediaConcurrencyOptionValidationAndRootAccess(t *testing.T) {
	prepareAsyncMediaController(t)
	for _, value := range []string{"-1", "1.5", "257", "unlimited", ""} {
		assert.Error(t, model.UpdateOption("AsyncMediaConcurrency", value), value)
	}
	for _, role := range []int{common.RoleCommonUser, common.RoleAdminUser} {
		response := asyncControllerRequest(UpdateOption, http.MethodPut, "/api/option/", 31, role, nil, `{"key":"AsyncMediaConcurrency","value":8}`)
		assert.Equal(t, http.StatusForbidden, response.Code)
	}
	response := asyncControllerRequest(UpdateOption, http.MethodPut, "/api/option/", 31, common.RoleRootUser, nil, `{"key":"AsyncMediaConcurrency","value":8}`)
	assert.Equal(t, http.StatusOK, response.Code)
	assert.Contains(t, response.Body.String(), `"success":true`)
}
