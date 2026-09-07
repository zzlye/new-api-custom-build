package controller

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 模型筛选必须在分页前执行，并与本人范围、其他条件和总数使用同一规则。
func TestTaskLogsFilterByModel(t *testing.T) {
	prepareAsyncMediaController(t)
	for _, userID := range []int{31, 32} {
		require.NoError(t, model.DB.Create(&model.User{Id: userID, Username: fmt.Sprintf("model-filter-%d", userID), AffCode: fmt.Sprintf("filter-%d", userID), Status: common.UserStatusEnabled}).Error)
		for _, name := range []string{"gpt-image-2", "gpt-image-2-4k"} {
			task := &model.AsyncRelayTask{UserID: userID, ModelName: name, CreatedAt: 100, NodeID: common.NodeName}
			require.NoError(t, task.InsertWithLog("default", "IMAGE"))
		}
	}
	native := &model.Task{UserId: 31, TaskID: "native-model-fixture", Status: model.TaskStatusSuccess, SubmitTime: 100,
		Properties: model.Properties{OriginModelName: "gpt-image-2"}}
	require.NoError(t, model.DB.Create(native).Error)
	child := &model.Task{UserId: 31, TaskID: "hidden-child-fixture", AsyncParentID: "parent-fixture", SubmitTime: 100,
		Properties: model.Properties{OriginModelName: "gpt-image-2"}}
	require.NoError(t, model.DB.Create(child).Error)
	for _, tc := range []struct {
		name, query        string
		handler            gin.HandlerFunc
		role, total, count int
	}{
		{"本人精确筛选", "model_name=gpt-image-2", GetUserTask, common.RoleCommonUser, 2, 2},
		{"全部范围及分页总数", "model_name=gpt-image-2&p=2&page_size=1", GetAllTask, common.RoleRootUser, 3, 1},
		{"前后空格", "model_name=%20gpt-image-2%20", GetUserTask, common.RoleCommonUser, 2, 2},
		{"任务编号组合", "model_name=gpt-image-2&task_id=native-model-fixture", GetUserTask, common.RoleCommonUser, 1, 1},
		{"时间条件组合", "model_name=gpt-image-2&start_timestamp=101", GetAllTask, common.RoleRootUser, 0, 0},
		{"不存在的模型", "model_name=unknown-model", GetAllTask, common.RoleRootUser, 0, 0},
		{"通配符是普通名称", "model_name=" + url.QueryEscape("%_' OR 1=1 --"), GetUserTask, common.RoleCommonUser, 0, 0},
		{"清空后恢复本人列表", "", GetUserTask, common.RoleCommonUser, 3, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := asyncControllerRequest(tc.handler, http.MethodGet, "/api/task?"+tc.query, 31, tc.role, nil, "")
			require.Equal(t, http.StatusOK, response.Code)
			var payload struct {
				Success bool `json:"success"`
				Data    struct {
					Items []dto.TaskDto `json:"items"`
					Total int           `json:"total"`
				} `json:"data"`
			}
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &payload))
			require.True(t, payload.Success)
			assert.Equal(t, tc.total, payload.Data.Total)
			assert.Len(t, payload.Data.Items, tc.count)
			for _, item := range payload.Data.Items {
				assert.NotEqual(t, child.TaskID, item.TaskID)
				if tc.role == common.RoleCommonUser {
					assert.Equal(t, 31, item.UserId)
				}
			}
		})
	}
}
