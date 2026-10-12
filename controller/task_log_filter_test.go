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

// 用户名筛选在分页前匹配账号，计数与列表一致，本人接口始终限制在当前用户。
func TestTaskLogsFilterByUsername(t *testing.T) {
	prepareAsyncMediaController(t)
	for _, user := range []model.User{
		{Id: 31, Username: "alice", AffCode: "username-filter-31"},
		{Id: 32, Username: "alice-team", AffCode: "username-filter-32"},
		{Id: 33, Username: "小明", AffCode: "username-filter-33"},
		{Id: 34, Username: "literal_!%", AffCode: "username-filter-34"},
	} {
		require.NoError(t, model.DB.Create(&user).Error)
		parent := &model.AsyncRelayTask{UserID: user.Id, ModelName: "gpt-image-2", CreatedAt: 100, NodeID: common.NodeName}
		require.NoError(t, parent.InsertWithLog("default", "IMAGE"))
	}
	native := &model.Task{UserId: 31, TaskID: "username-native", ChannelId: 6, SubmitTime: 100,
		Properties: model.Properties{OriginModelName: "gpt-image-2"}}
	require.NoError(t, model.DB.Create(native).Error)
	require.NoError(t, model.DB.Create(&model.Task{UserId: 31, TaskID: "username-child", AsyncParentID: native.TaskID}).Error)
	for _, tc := range []struct {
		name, query string
		self        bool
		total       int
		users       []int
	}{
		{"模糊匹配及分页", "username=alice&p=2&page_size=1", false, 3, []int{32}},
		{"大小写与前后空格", "username=" + url.QueryEscape(" ALICE "), false, 3, []int{31, 32, 31}},
		{"中文名字", "username=" + url.QueryEscape("小"), false, 1, []int{33}},
		{"字面通配符", "username=" + url.QueryEscape("_!%"), false, 1, []int{34}},
		{"不存在的用户", "username=missing-user", false, 0, nil},
		{"输入不成为SQL", "username=" + url.QueryEscape("' OR 1=1 --"), false, 0, nil},
		{"组合模型渠道", "username=alice&model_name=gpt-image-2&channel_id=6", false, 1, []int{31}},
		{"组合任务编号", "username=alice&task_id=username-native", false, 1, []int{31}},
		{"组合时间", "username=alice&start_timestamp=101", false, 0, nil},
		{"空白恢复列表", "username=%20%20", false, 5, []int{31, 34, 33, 32, 31}},
		{"本人接口忽略他人用户名", "username=alice-team", true, 2, []int{31, 31}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler := GetAllTask
			role := common.RoleRootUser
			if tc.self {
				handler, role = GetUserTask, common.RoleCommonUser
			}
			response := asyncControllerRequest(handler, http.MethodGet, "/api/task?"+tc.query, 31, role, nil, "")
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
			var users []int
			for _, item := range payload.Data.Items {
				assert.NotEqual(t, "username-child", item.TaskID)
				users = append(users, item.UserId)
			}
			assert.Equal(t, tc.users, users)
		})
	}
}
