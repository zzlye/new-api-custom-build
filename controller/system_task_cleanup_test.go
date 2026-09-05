package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogCleanupEndpointRequiresRootAndQueuesCombinedScope(t *testing.T) {
	prepareAsyncMediaController(t)
	require.NoError(t, model.DB.AutoMigrate(&model.SystemTask{}, &model.SystemTaskLock{}))
	for _, role := range []int{common.RoleCommonUser, common.RoleAdminUser} {
		response := asyncControllerRequest(CreateLogCleanupSystemTask, http.MethodPost, "/api/system-task/log-cleanup?target_timestamp=200&include_task_logs=true", 1, role, nil, "")
		assert.Equal(t, http.StatusForbidden, response.Code)
	}
	var count int64
	require.NoError(t, model.DB.Model(&model.SystemTask{}).Count(&count).Error)
	assert.Zero(t, count)
	response := asyncControllerRequest(CreateLogCleanupSystemTask, http.MethodPost, "/api/system-task/log-cleanup?target_timestamp=200&include_task_logs=true", 1, common.RoleRootUser, nil, "")
	assert.Equal(t, http.StatusOK, response.Code)
	var task model.SystemTask
	require.NoError(t, model.DB.First(&task).Error)
	var payload service.LogCleanupPayload
	require.NoError(t, task.DecodePayload(&payload))
	assert.True(t, payload.IncludeTaskLogs)
	assert.Equal(t, int64(200), payload.TargetTimestamp)
	assert.Equal(t, model.SystemTaskStatusPending, task.Status, "接口提交后直接返回，由后台执行清理")
}

func TestLogCleanupOldClientDoesNotExpandDeletionScope(t *testing.T) {
	prepareAsyncMediaController(t)
	require.NoError(t, model.DB.AutoMigrate(&model.SystemTask{}, &model.SystemTaskLock{}))
	response := asyncControllerRequest(CreateLogCleanupSystemTask, http.MethodPost, "/api/system-task/log-cleanup?target_timestamp=200", 1, common.RoleRootUser, nil, "")
	assert.Equal(t, http.StatusOK, response.Code)
	var task model.SystemTask
	require.NoError(t, model.DB.First(&task).Error)
	var payload service.LogCleanupPayload
	require.NoError(t, task.DecodePayload(&payload))
	assert.False(t, payload.IncludeTaskLogs, "旧页面只确认普通日志时，不扩大删除范围")
}
