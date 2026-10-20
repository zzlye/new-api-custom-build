package service

import (
	"context"
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// prepareLogCleanupDatabase 为联合清理创建独立数据库和文件目录，不触及正在使用的日志。
func prepareLogCleanupDatabase(t *testing.T) {
	t.Helper()
	previousDB, previousLogs := model.DB, model.LOG_DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	model.DB, model.LOG_DB = db, db
	require.NoError(t, db.AutoMigrate(&model.Log{}, &model.Task{}, &model.AsyncRelayTask{}, &model.Midjourney{}, &model.SystemTask{}, &model.SystemTaskLock{}))
	t.Setenv("ASYNC_MEDIA_DIR", t.TempDir())
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogs
		require.NoError(t, sqlDB.Close())
	})
}

func runLogCleanupFixture(t *testing.T, task *model.SystemTask) *model.SystemTask {
	t.Helper()
	claimed, won, err := model.ClaimSystemTask(task.ID, task.Type, "cleanup-fixture", common.GetTimestamp()+60)
	require.NoError(t, err)
	require.True(t, won)
	runLogCleanupTask(context.Background(), claimed, "cleanup-fixture")
	result, err := model.GetSystemTaskByTaskID(task.TaskID)
	require.NoError(t, err)
	require.NotNil(t, result)
	return result
}

func TestLogCleanupClearsOrderDetailsButKeepsFinancialDeduplication(t *testing.T) {
	prepareLogCleanupDatabase(t)
	require.NoError(t, model.DB.AutoMigrate(&model.TopUp{}, &model.ReceiptOrder{}, &model.ReceiptPayment{}, &model.OrderEvent{}, &model.ReceiptGate{}))
	top := model.TopUp{TradeNo: "cleanup-receipt", CreateTime: 100, Status: common.TopUpStatusSuccess, CreateIP: "192.0.2.1", LastError: "旧错误"}
	require.NoError(t, model.DB.Create(&top).Error)
	require.NoError(t, model.DB.Create(&model.TopUp{TradeNo: "keep-pending", CreateTime: 100, Status: common.TopUpStatusPending}).Error)
	require.NoError(t, model.DB.Create(&model.ReceiptOrder{TopUpID: top.Id, QRCode: "https://qr.alipay.com/old", CreditQuota: 100}).Error)
	require.NoError(t, model.DB.Create(&model.ReceiptPayment{ID: "keep-hash", TradeKey: "keep-trade-hash", TopUpID: top.Id}).Error)
	require.NoError(t, model.DB.Create(&model.OrderEvent{TopUpID: top.Id, Message: "旧详情"}).Error)
	task, err := StartLogCleanupTask(200, false, true, false)
	require.NoError(t, err)
	finished := runLogCleanupFixture(t, task)
	assert.Equal(t, model.SystemTaskStatusSucceeded, finished.Status)
	var result LogCleanupResult
	require.NoError(t, common.UnmarshalJsonStr(finished.Result, &result))
	assert.EqualValues(t, 1, result.DeletedOrders)
	require.NoError(t, model.DB.First(&top, top.Id).Error)
	assert.Empty(t, top.CreateIP)
	assert.Empty(t, top.LastError)
	assert.Positive(t, top.LogClearedAt)
	var count int64
	require.NoError(t, model.DB.Model(&model.ReceiptPayment{}).Count(&count).Error)
	assert.EqualValues(t, 1, count)
	require.NoError(t, model.DB.Model(&model.OrderEvent{}).Count(&count).Error)
	assert.Zero(t, count)
}

func TestLogCleanupIncludesCompletedTasksAndTheirFiles(t *testing.T) {
	prepareLogCleanupDatabase(t)
	oldLog := &model.Log{CreatedAt: 100, Type: model.LogTypeConsume}
	freshLog := &model.Log{CreatedAt: 300, Type: model.LogTypeConsume}
	require.NoError(t, model.LOG_DB.Create(oldLog).Error)
	require.NoError(t, model.LOG_DB.Create(freshLog).Error)
	path, file, err := common.CreateAsyncMediaFile()
	require.NoError(t, err)
	_, err = file.WriteString("本地生成文件")
	require.NoError(t, err)
	require.NoError(t, file.Close())
	referencePath, reference, err := common.CreateAsyncMediaFile()
	require.NoError(t, err)
	require.NoError(t, reference.Close())
	details, err := common.Marshal(model.AsyncRelayRequestDetails{References: []model.AsyncRelayReference{{Path: referencePath}}})
	require.NoError(t, err)
	parent := &model.Task{TaskID: "old-image", IsAsync: true, Status: model.TaskStatusSuccess, SubmitTime: 90, FinishTime: 100}
	child := &model.Task{TaskID: "old-image-child", AsyncParentID: parent.TaskID, Status: model.TaskStatusSuccess, SubmitTime: 90, FinishTime: 100}
	running := &model.Task{TaskID: "running-image", Status: model.TaskStatusInProgress, SubmitTime: 90}
	recent := &model.Task{TaskID: "recent-image", Status: model.TaskStatusSuccess, SubmitTime: 90, FinishTime: 300}
	for _, entry := range []*model.Task{parent, child, running, recent} {
		require.NoError(t, model.DB.Create(entry).Error)
	}
	task := &model.AsyncRelayTask{TaskID: parent.TaskID, LogID: parent.ID, NodeID: common.NodeName, Status: model.AsyncRelayTaskStatusSucceeded, FinishedAt: 100, ResultFilePath: path, RequestDetails: string(details)}
	require.NoError(t, model.DB.Create(task).Error)
	cleanup, err := StartLogCleanupTask(200, true, false, false)
	require.NoError(t, err)
	result := runLogCleanupFixture(t, cleanup)
	assert.Equal(t, model.SystemTaskStatusSucceeded, result.Status, result.Error)
	var logs []model.Log
	require.NoError(t, model.LOG_DB.Find(&logs).Error)
	require.Len(t, logs, 1)
	assert.Equal(t, freshLog.Id, logs[0].Id)
	var tasks []model.Task
	require.NoError(t, model.DB.Order("id").Find(&tasks).Error)
	assert.Len(t, tasks, 2)
	var remaining int64
	require.NoError(t, model.DB.Model(&model.AsyncRelayTask{}).Count(&remaining).Error)
	assert.Zero(t, remaining)
	_, err = os.Stat(path)
	assert.True(t, os.IsNotExist(err), "关联文件应随该任务日志一起清理")
	_, err = os.Stat(referencePath)
	assert.True(t, os.IsNotExist(err), "参考文件应随该任务日志一起清理")
}

func TestLogCleanupStillDeletesTasksWhenUsageLogsAreEmpty(t *testing.T) {
	prepareLogCleanupDatabase(t)
	oldTask := &model.Task{TaskID: "old-failed-task", Status: model.TaskStatusFailure, SubmitTime: 90, FinishTime: 100}
	require.NoError(t, model.DB.Create(oldTask).Error)
	cleanup, err := StartLogCleanupTask(200, true, false, false)
	require.NoError(t, err)
	result := runLogCleanupFixture(t, cleanup)
	assert.Equal(t, model.SystemTaskStatusSucceeded, result.Status, result.Error)
	var remaining int64
	require.NoError(t, model.DB.Model(&model.Task{}).Count(&remaining).Error)
	assert.Zero(t, remaining)
}

func TestLogCleanupSkipsForeignFilesAndContinuesBatchCursor(t *testing.T) {
	prepareLogCleanupDatabase(t)
	foreignPath, file, err := common.CreateAsyncMediaFile()
	require.NoError(t, err)
	require.NoError(t, file.Close())
	foreign := &model.Task{TaskID: "foreign-image", IsAsync: true, Status: model.TaskStatusSuccess, SubmitTime: 90, FinishTime: 100}
	local := &model.Task{TaskID: "local-old-task", Status: model.TaskStatusSuccess, SubmitTime: 90, FinishTime: 100}
	require.NoError(t, model.DB.Create(foreign).Error)
	require.NoError(t, model.DB.Create(local).Error)
	require.NoError(t, model.DB.Create(&model.AsyncRelayTask{TaskID: foreign.TaskID, LogID: foreign.ID, NodeID: "other-" + common.NodeName, Status: model.AsyncRelayTaskStatusSucceeded, FinishedAt: 100, ResultFilePath: foreignPath}).Error)
	task, err := model.CreateSystemTask(model.SystemTaskTypeLogCleanup, LogCleanupPayload{TargetTimestamp: 200, BatchSize: 1, IncludeTaskLogs: true}, LogCleanupState{})
	require.NoError(t, err)
	result := runLogCleanupFixture(t, task)
	assert.Equal(t, model.SystemTaskStatusSucceeded, result.Status, result.Error)
	var counts LogCleanupResult
	require.NoError(t, common.UnmarshalJsonStr(result.Result, &counts))
	assert.Equal(t, int64(1), counts.DeletedTasks)
	assert.Equal(t, int64(1), counts.SkippedTasks)
	assert.Equal(t, int64(1), counts.DeletedCount)
	var remaining []model.Task
	require.NoError(t, model.DB.Find(&remaining).Error)
	require.Len(t, remaining, 1)
	assert.Equal(t, foreign.ID, remaining[0].ID)
	_, err = os.Stat(foreignPath)
	assert.NoError(t, err, "保留其他节点的文件，不在当前节点错误清空")
}

func TestLogCleanupFileFailureKeepsTaskAndReportsPartialProgress(t *testing.T) {
	prepareLogCleanupDatabase(t)
	oldLog := &model.Log{CreatedAt: 100, Type: model.LogTypeConsume}
	require.NoError(t, model.DB.Create(oldLog).Error)
	parent := &model.Task{TaskID: "unsafe-path-image", IsAsync: true, Status: model.TaskStatusSuccess, SubmitTime: 90, FinishTime: 100}
	require.NoError(t, model.DB.Create(parent).Error)
	require.NoError(t, model.DB.Create(&model.AsyncRelayTask{TaskID: parent.TaskID, LogID: parent.ID, NodeID: common.NodeName, Status: model.AsyncRelayTaskStatusSucceeded, FinishedAt: 100, ResultFilePath: t.TempDir() + "/outside-media.txt"}).Error)
	cleanup, err := StartLogCleanupTask(200, true, false, false)
	require.NoError(t, err)
	result := runLogCleanupFixture(t, cleanup)
	assert.Equal(t, model.SystemTaskStatusFailed, result.Status)
	assert.Contains(t, result.Error, parent.TaskID)
	var remaining int64
	require.NoError(t, model.DB.Model(&model.Task{}).Count(&remaining).Error)
	assert.Equal(t, int64(1), remaining)
	var state LogCleanupState
	require.NoError(t, result.DecodeState(&state))
	assert.Equal(t, int64(1), state.DeletedLogs)
	assert.Zero(t, state.DeletedTasks)
}

func TestLogCleanupLegacyQueuedJobKeepsItsOriginalScope(t *testing.T) {
	prepareLogCleanupDatabase(t)
	oldLog := &model.Log{CreatedAt: 100, Type: model.LogTypeConsume}
	require.NoError(t, model.DB.Create(oldLog).Error)
	oldTask := &model.Task{TaskID: "legacy-kept-task", Status: model.TaskStatusSuccess, SubmitTime: 90, FinishTime: 100}
	require.NoError(t, model.DB.Create(oldTask).Error)
	cleanup, err := model.CreateSystemTask(model.SystemTaskTypeLogCleanup, map[string]any{"target_timestamp": 200, "batch_size": 1}, LogCleanupState{})
	require.NoError(t, err)
	result := runLogCleanupFixture(t, cleanup)
	assert.Equal(t, model.SystemTaskStatusSucceeded, result.Status, result.Error)
	var count int64
	require.NoError(t, model.DB.Model(&model.Task{}).Count(&count).Error)
	assert.Equal(t, int64(1), count)
	require.NoError(t, model.LOG_DB.Model(&model.Log{}).Count(&count).Error)
	assert.Zero(t, count)
}

func TestLogCleanupKeepsActiveChildrenAndCutoffBoundary(t *testing.T) {
	prepareLogCleanupDatabase(t)
	parent := &model.Task{TaskID: "parent-with-active-child", IsAsync: true, Status: model.TaskStatusSuccess, SubmitTime: 90, FinishTime: 100}
	require.NoError(t, model.DB.Create(parent).Error)
	child := &model.Task{TaskID: "still-generating-child", AsyncParentID: parent.TaskID, Status: model.TaskStatusInProgress, SubmitTime: 90}
	exactCutoff := &model.Task{TaskID: "exact-cutoff", Status: model.TaskStatusFailure, SubmitTime: 90, FinishTime: 200}
	oldLegacy := &model.Task{TaskID: "legacy-no-finish-time", Status: model.TaskStatusFailure, SubmitTime: 90}
	for _, task := range []*model.Task{child, exactCutoff, oldLegacy} {
		require.NoError(t, model.DB.Create(task).Error)
	}
	require.NoError(t, model.DB.Create(&model.AsyncRelayTask{TaskID: parent.TaskID, LogID: parent.ID, Status: model.AsyncRelayTaskStatusSucceeded, FinishedAt: 100}).Error)
	cleanup, err := StartLogCleanupTask(200, true, false, false)
	require.NoError(t, err)
	result := runLogCleanupFixture(t, cleanup)
	assert.Equal(t, model.SystemTaskStatusSucceeded, result.Status, result.Error)
	var tasks []model.Task
	require.NoError(t, model.DB.Order("id").Find(&tasks).Error)
	require.Len(t, tasks, 3)
	assert.Equal(t, parent.TaskID, tasks[0].TaskID)
	assert.Equal(t, child.TaskID, tasks[1].TaskID)
	assert.Equal(t, exactCutoff.TaskID, tasks[2].TaskID)
}

func TestLogCleanupIncludesLotteryOnlyWhenRequested(t *testing.T) {
	prepareLogCleanupDatabase(t)
	require.NoError(t, model.DB.AutoMigrate(&model.LotteryActivity{}, &model.LotteryDraw{}))
	a := model.LotteryActivity{Status: model.LotteryEnded}
	require.NoError(t, model.DB.Create(&a).Error)
	for _, request := range []string{"cleanup-request-1", "cleanup-request-2"} {
		require.NoError(t, model.DB.Create(&model.LotteryDraw{ActivityID: a.ID, UserID: 1, RequestID: request, Quota: 10, CreatedAt: 100, IP: "192.0.2.1"}).Error)
	}
	// 旧版排队任务没有新范围标记，升级后继续运行也不能清掉抽奖记录。
	legacy, err := model.CreateSystemTask(model.SystemTaskTypeLogCleanup, map[string]any{"target_timestamp": 200, "batch_size": 1}, LogCleanupState{})
	require.NoError(t, err)
	assert.Equal(t, model.SystemTaskStatusSucceeded, runLogCleanupFixture(t, legacy).Status)
	var remaining int64
	require.NoError(t, model.OldLotteryLogQuery(context.Background(), 200).Count(&remaining).Error)
	assert.EqualValues(t, 2, remaining)
	// 验证没有其他日志时仍能分批完成，并保存准确的进度和结果。
	task, err := StartLogCleanupTask(200, false, false, true)
	require.NoError(t, err)
	var payload LogCleanupPayload
	require.NoError(t, task.DecodePayload(&payload))
	require.True(t, payload.IncludeLotteryLogs)
	payload.BatchSize = 1
	encoded, err := common.Marshal(payload)
	require.NoError(t, err)
	require.NoError(t, model.DB.Model(task).Update("payload", string(encoded)).Error)
	finished := runLogCleanupFixture(t, task)
	require.Equal(t, model.SystemTaskStatusSucceeded, finished.Status, finished.Error)
	var result LogCleanupResult
	require.NoError(t, common.UnmarshalJsonStr(finished.Result, &result))
	assert.EqualValues(t, 2, result.DeletedLottery)
	assert.EqualValues(t, 2, result.DeletedCount)
	var state LogCleanupState
	require.NoError(t, finished.DecodeState(&state))
	assert.EqualValues(t, 2, state.Processed)
	assert.Equal(t, 100, state.Progress)
	assert.Zero(t, state.Remaining)
	repeated, err := StartLogCleanupTask(200, false, false, true)
	require.NoError(t, err)
	finished = runLogCleanupFixture(t, repeated)
	require.Equal(t, model.SystemTaskStatusSucceeded, finished.Status, finished.Error)
	require.NoError(t, common.UnmarshalJsonStr(finished.Result, &result))
	assert.Zero(t, result.DeletedCount)
}
