package model

import (
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAsyncRelayNativeTaskUsesOneLogAndExistingPoller(t *testing.T) {
	prepareAsyncRelayTaskTable(t)
	parent := &AsyncRelayTask{UserID: 8241, RequestFormat: "task", NodeID: "fixture-node"}
	require.NoError(t, parent.InsertWithLog("default", "VIDEO"))
	claimed, won, err := ClaimAsyncRelayTask(parent.ID, "fixture-worker")
	require.NoError(t, err)
	require.True(t, won)
	child := &Task{TaskID: "native_" + parent.TaskID, UserId: parent.UserID, Platform: "1", Quota: 120, Status: TaskStatusSubmitted, Progress: "0%"}
	require.NoError(t, AttachAsyncRelayNativeTask(parent.TaskID, child))
	t.Cleanup(func() {
		require.NoError(t, DB.Where("task_id IN ?", []string{parent.TaskID, child.TaskID}).Delete(&Task{}).Error)
	})
	assert.Equal(t, parent.TaskID, child.AsyncParentID)
	loaded, err := GetAsyncRelayTaskByTaskID(parent.TaskID)
	require.NoError(t, err)
	require.NotNil(t, loaded)
	assert.Equal(t, child.TaskID, loaded.LinkedTaskID)
	assert.Equal(t, int64(1), TaskCountAllTasks(SyncTaskQueryParams{UserID: strconv.Itoa(parent.UserID)}))
	logs := TaskGetAllUserTask(parent.UserID, 0, 20, SyncTaskQueryParams{})
	require.Len(t, logs, 1)
	assert.Equal(t, parent.TaskID, logs[0].TaskID)
	assert.Equal(t, 120, logs[0].Quota)
	unfinished := GetAllUnFinishSyncTasks(1000)
	var selected []string
	for _, task := range unfinished {
		if task.UserId == parent.UserID {
			selected = append(selected, task.TaskID)
		}
	}
	assert.Equal(t, []string{child.TaskID}, selected)
	// 已完成的原生任务不可被再次提交关联，失败时也不留下重复的子记录。
	claimed.Status = AsyncRelayTaskStatusFailed
	_, err = claimed.UpdateWithStatus(AsyncRelayTaskStatusProcessing)
	require.NoError(t, err)
	duplicate := &Task{TaskID: "duplicate_" + parent.TaskID, UserId: parent.UserID}
	require.Error(t, AttachAsyncRelayNativeTask(parent.TaskID, duplicate))
	var count int64
	require.NoError(t, DB.Model(&Task{}).Where("task_id = ?", duplicate.TaskID).Count(&count).Error)
	assert.Zero(t, count)
}

func TestAsyncRelayStaleSubmittedRequestIsNotRepeated(t *testing.T) {
	prepareAsyncRelayTaskTable(t)
	now := common.GetTimestamp()
	sent := &AsyncRelayTask{UserID: 8251, Status: AsyncRelayTaskStatusProcessing, WorkerID: "old", DispatchStartedAt: now - 120, UpdatedAt: now - 120}
	waiting := &AsyncRelayTask{UserID: 8252, Status: AsyncRelayTaskStatusProcessing, WorkerID: "old", DispatchStartedAt: now - 120, UpdatedAt: now - 120, LinkedTaskID: "upstream-existing"}
	require.NoError(t, sent.Insert())
	require.NoError(t, waiting.Insert())
	recovered, err := RecoverStaleAsyncRelayTasks(60)
	require.NoError(t, err)
	assert.Equal(t, int64(2), recovered)
	sent, err = GetAsyncRelayTaskByTaskID(sent.TaskID)
	require.NoError(t, err)
	assert.Equal(t, AsyncRelayTaskStatusFailed, sent.Status)
	assert.NotZero(t, sent.FinishedAt)
	waiting, err = GetAsyncRelayTaskByTaskID(waiting.TaskID)
	require.NoError(t, err)
	assert.Equal(t, AsyncRelayTaskStatusWaiting, waiting.Status)
	assert.Equal(t, "upstream-existing", waiting.LinkedTaskID)
}

func TestAsyncRelayClaimRespectsFileNodeAndWorkerLease(t *testing.T) {
	prepareAsyncRelayTaskTable(t)
	task := &AsyncRelayTask{UserID: 8261, NodeID: "node-a"}
	require.NoError(t, task.Insert())
	wrongNode, err := ClaimAsyncRelayTaskForNode("node-b", "other-worker")
	require.NoError(t, err)
	assert.Nil(t, wrongNode)
	owner, err := ClaimAsyncRelayTaskForNode("node-a", "current-worker")
	require.NoError(t, err)
	require.NotNil(t, owner)
	stale := *owner
	stale.WorkerID = "stale-worker"
	stale.Status = AsyncRelayTaskStatusSucceeded
	won, err := stale.UpdateWithStatus(AsyncRelayTaskStatusProcessing)
	require.NoError(t, err)
	assert.False(t, won)
	saved, err := GetAsyncRelayTaskByTaskID(task.TaskID)
	require.NoError(t, err)
	assert.Equal(t, AsyncRelayTaskStatusProcessing, saved.Status)
	assert.Equal(t, "current-worker", saved.WorkerID)
}

func TestAsyncRelayCleanupRemovesOrphansButKeepsQueuedInputs(t *testing.T) {
	prepareAsyncRelayTaskTable(t)
	t.Setenv("ASYNC_MEDIA_DIR", t.TempDir())
	common.OptionMapRWMutex.Lock()
	previous := common.OptionMap
	common.OptionMap = map[string]string{common.AsyncMediaRetentionOption: "2"}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() { common.OptionMapRWMutex.Lock(); common.OptionMap = previous; common.OptionMapRWMutex.Unlock() })
	old := time.Now().Add(-3 * time.Hour)
	orphan, file, err := common.CreateAsyncMediaFile()
	require.NoError(t, err)
	require.NoError(t, file.Close())
	require.NoError(t, os.Chtimes(orphan, old, old))
	input, file, err := common.CreateAsyncMediaFile()
	require.NoError(t, err)
	require.NoError(t, file.Close())
	require.NoError(t, os.Chtimes(input, old, old))
	recent, file, err := common.CreateAsyncMediaFile()
	require.NoError(t, err)
	require.NoError(t, file.Close())
	task := &AsyncRelayTask{UserID: 8271, RequestFilePath: input}
	require.NoError(t, task.Insert())
	require.NoError(t, CleanupOrphanAsyncMediaFiles())
	_, err = os.Stat(orphan)
	assert.True(t, os.IsNotExist(err))
	_, err = os.Stat(input)
	require.NoError(t, err)
	_, err = os.Stat(recent)
	require.NoError(t, err)
	require.Error(t, ExpireAsyncRelayTaskFiles(task))
	_, err = os.Stat(input)
	require.NoError(t, err)
}

func TestAsyncRelayRetentionChangesApplyToRemainingResults(t *testing.T) {
	common.OptionMapRWMutex.Lock()
	previous := common.OptionMap
	common.OptionMap = map[string]string{common.AsyncMediaRetentionOption: "2"}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() { common.OptionMapRWMutex.Lock(); common.OptionMap = previous; common.OptionMapRWMutex.Unlock() })
	now := int64(10000)
	task := &AsyncRelayTask{Status: AsyncRelayTaskStatusSucceeded, FinishedAt: now - 5400}
	assert.False(t, AsyncRelayTaskExpired(task, now))
	common.OptionMapRWMutex.Lock()
	common.OptionMap[common.AsyncMediaRetentionOption] = "1"
	common.OptionMapRWMutex.Unlock()
	assert.True(t, AsyncRelayTaskExpired(task, now))
	task.ResultExpiredAt = now
	common.OptionMapRWMutex.Lock()
	common.OptionMap[common.AsyncMediaRetentionOption] = "3"
	common.OptionMapRWMutex.Unlock()
	assert.True(t, AsyncRelayTaskExpired(task, now))
	for _, value := range []string{"1", "2", "168"} {
		assert.NoError(t, validateOptionValue(common.AsyncMediaRetentionOption, value))
	}
	for _, value := range []string{"0", "169", "-1", "1.5", "NaN", ""} {
		assert.Error(t, validateOptionValue(common.AsyncMediaRetentionOption, value))
	}
}

func TestAsyncRelayExpirationClearsNativeInlineMediaAndKeepsBilling(t *testing.T) {
	prepareAsyncRelayTaskTable(t)
	parent := &AsyncRelayTask{UserID: 8281, RequestFormat: "task"}
	require.NoError(t, parent.InsertWithLog("default", "VIDEO"))
	claimed, won, err := ClaimAsyncRelayTask(parent.ID, "fixture-worker")
	require.NoError(t, err)
	require.True(t, won)
	child := &Task{TaskID: "native_" + parent.TaskID, UserId: parent.UserID, Status: TaskStatusSuccess, PrivateData: TaskPrivateData{Key: "private-fixture", UpstreamTaskID: "provider-id", ResultURL: "https://example.test/result.mp4", TokenId: 82}}
	child.SetData(map[string]any{"video": "inline-video-data"})
	require.NoError(t, AttachAsyncRelayNativeTask(parent.TaskID, child))
	t.Cleanup(func() {
		require.NoError(t, DB.Where("task_id IN ?", []string{parent.TaskID, child.TaskID}).Delete(&Task{}).Error)
	})
	claimed.LinkedTaskID, claimed.Status = child.TaskID, AsyncRelayTaskStatusSucceeded
	_, err = claimed.UpdateWithStatus(AsyncRelayTaskStatusProcessing)
	require.NoError(t, err)
	require.NoError(t, ExpireAsyncRelayTaskFiles(claimed))
	var saved Task
	require.NoError(t, DB.First(&saved, child.ID).Error)
	assert.Empty(t, saved.Data)
	assert.Empty(t, saved.PrivateData.ResultURL)
	assert.Equal(t, "provider-id", saved.PrivateData.UpstreamTaskID)
	assert.Equal(t, 82, saved.PrivateData.TokenId)
	assert.Equal(t, "private-fixture", saved.PrivateData.Key)
}
