package model

import (
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func prepareAsyncRelayTaskTable(t *testing.T) {
	t.Helper()
	require.NoError(t, DB.AutoMigrate(&AsyncRelayTask{}))
	require.NoError(t, DB.Where("1 = 1").Delete(&AsyncRelayTask{}).Error)
}

func TestAsyncRelayTaskCreateAndQuery(t *testing.T) {
	prepareAsyncRelayTaskTable(t)

	task := &AsyncRelayTask{
		UserID:             101,
		TokenID:            202,
		RequestPath:        "/v1/images/generations",
		RequestQuery:       "async=true",
		RequestContentType: "application/json",
		RequestFilePath:    "D:/tmp/input.png",
		ResultContentType:  "image/png",
		Status:             AsyncRelayTaskStatusPending,
	}
	require.NoError(t, CreateAsyncRelayTask(task))

	assert.NotZero(t, task.ID)
	assert.NotEmpty(t, task.TaskID)
	assert.Equal(t, AsyncRelayTaskStatusPending, task.Status)
	assert.NotZero(t, task.CreatedAt)
	assert.NotZero(t, task.UpdatedAt)

	loaded, err := GetAsyncRelayTaskByUserAndTaskID(task.UserID, task.TaskID)
	require.NoError(t, err)
	require.NotNil(t, loaded)
	assert.Equal(t, task.ID, loaded.ID)
	assert.Equal(t, task.RequestFilePath, loaded.RequestFilePath)
	assert.Equal(t, task.ResultContentType, loaded.ResultContentType)

	missing, err := GetAsyncRelayTaskByTaskID("missing_async_task")
	require.NoError(t, err)
	assert.Nil(t, missing)
}

func TestAsyncRelayTaskPendingQueries(t *testing.T) {
	prepareAsyncRelayTaskTable(t)

	for _, status := range []AsyncRelayTaskStatus{
		AsyncRelayTaskStatusPending,
		AsyncRelayTaskStatusProcessing,
		AsyncRelayTaskStatusSucceeded,
	} {
		require.NoError(t, CreateAsyncRelayTask(&AsyncRelayTask{
			UserID: 1,
			Status: status,
		}))
	}

	pending, err := FindPendingAsyncRelayTasks(10)
	require.NoError(t, err)
	require.Len(t, pending, 1)
	assert.True(t, HasPendingAsyncRelayTasks())

	require.NoError(t, DB.Model(&AsyncRelayTask{}).
		Where("status = ?", AsyncRelayTaskStatusPending).
		Update("status", AsyncRelayTaskStatusFailed).Error)
	assert.False(t, HasPendingAsyncRelayTasks())

	userTasks, err := ListAsyncRelayTasksByUserID(1, 0, 10)
	require.NoError(t, err)
	assert.Len(t, userTasks, 3)
}

func TestAsyncRelayTaskClaimCAS(t *testing.T) {
	prepareAsyncRelayTaskTable(t)
	task := &AsyncRelayTask{UserID: 5, Status: AsyncRelayTaskStatusPending}
	require.NoError(t, CreateAsyncRelayTask(task))

	const workers = 5
	wins := make([]bool, workers)
	errs := make([]error, workers)
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func(index int) {
			defer wg.Done()
			claimed, won, err := ClaimAsyncRelayTask(task.ID, "worker-")
			errs[index] = err
			wins[index] = won
			if won {
				assert.Equal(t, AsyncRelayTaskStatusProcessing, claimed.Status)
			}
		}(i)
	}
	wg.Wait()

	for _, err := range errs {
		require.NoError(t, err)
	}
	winsCount := 0
	for _, won := range wins {
		if won {
			winsCount++
		}
	}
	assert.Equal(t, 1, winsCount)

	loaded, err := GetAsyncRelayTaskByTaskID(task.TaskID)
	require.NoError(t, err)
	require.NotNil(t, loaded)
	assert.Equal(t, AsyncRelayTaskStatusProcessing, loaded.Status)
	assert.NotZero(t, loaded.StartedAt)
}

func TestAsyncRelayTaskUpdateWithStatus(t *testing.T) {
	prepareAsyncRelayTaskTable(t)
	task := &AsyncRelayTask{UserID: 9, Status: AsyncRelayTaskStatusPending}
	require.NoError(t, CreateAsyncRelayTask(task))

	task.Status = AsyncRelayTaskStatusSucceeded
	task.ResultFilePath = "D:/tmp/result.png"
	won, err := task.UpdateWithStatus(AsyncRelayTaskStatusPending)
	require.NoError(t, err)
	assert.True(t, won)

	reloaded, err := GetAsyncRelayTaskByTaskID(task.TaskID)
	require.NoError(t, err)
	require.NotNil(t, reloaded)
	assert.Equal(t, AsyncRelayTaskStatusSucceeded, reloaded.Status)
	assert.Equal(t, task.ResultFilePath, reloaded.ResultFilePath)
	assert.NotZero(t, reloaded.FinishedAt)

	task.Status = AsyncRelayTaskStatusFailed
	won, err = task.UpdateWithStatus(AsyncRelayTaskStatusPending)
	require.NoError(t, err)
	assert.False(t, won)

	assert.False(t, HasPendingAsyncRelayTasks())
	assert.GreaterOrEqual(t, common.GetTimestamp(), reloaded.FinishedAt)
}

func TestRecoverStaleAsyncRelayTasks(t *testing.T) {
	prepareAsyncRelayTaskTable(t)
	now := common.GetTimestamp()
	stale := &AsyncRelayTask{
		UserID:    12,
		Status:    AsyncRelayTaskStatusProcessing,
		WorkerID:  "worker-stale",
		StartedAt: now - 120,
		UpdatedAt: now - 120,
	}
	fresh := &AsyncRelayTask{
		UserID:    13,
		Status:    AsyncRelayTaskStatusProcessing,
		WorkerID:  "worker-fresh",
		StartedAt: now - 10,
		UpdatedAt: now - 10,
	}
	require.NoError(t, CreateAsyncRelayTask(stale))
	require.NoError(t, CreateAsyncRelayTask(fresh))

	recovered, err := RecoverStaleAsyncRelayTasks(60)
	require.NoError(t, err)
	assert.Equal(t, int64(1), recovered)

	reloadedStale, err := GetAsyncRelayTaskByTaskID(stale.TaskID)
	require.NoError(t, err)
	require.NotNil(t, reloadedStale)
	assert.Equal(t, AsyncRelayTaskStatusPending, reloadedStale.Status)
	assert.Empty(t, reloadedStale.WorkerID)
	assert.Zero(t, reloadedStale.StartedAt)
	assert.NotZero(t, reloadedStale.UpdatedAt)

	reloadedFresh, err := GetAsyncRelayTaskByTaskID(fresh.TaskID)
	require.NoError(t, err)
	require.NotNil(t, reloadedFresh)
	assert.Equal(t, AsyncRelayTaskStatusProcessing, reloadedFresh.Status)
	assert.Equal(t, "worker-fresh", reloadedFresh.WorkerID)

	// 省略超时时间时应使用默认值，不会误回收刚刚更新的任务。
	recovered, err = RecoverStaleAsyncRelayTasks()
	require.NoError(t, err)
	assert.Zero(t, recovered)
}
