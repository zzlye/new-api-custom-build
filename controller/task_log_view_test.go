package controller

import (
	"net/http"
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// 三种数据库均通过同一条批量查询补充名称，普通用户与缺失渠道保持原来的响应。
func TestTaskLogChannelNamesDatabaseMatrix(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			var driver gorm.Dialector
			switch dialect {
			case "sqlite":
				driver = sqlite.Open(":memory:")
			case "mysql":
				if os.Getenv("TEST_MYSQL_DSN") == "" {
					t.Skip("未设置测试 MySQL")
				}
				driver = mysql.Open(os.Getenv("TEST_MYSQL_DSN"))
			case "postgres":
				if os.Getenv("TEST_POSTGRES_DSN") == "" {
					t.Skip("未设置测试 PostgreSQL")
				}
				driver = postgres.Open(os.Getenv("TEST_POSTGRES_DSN"))
			}
			db, err := gorm.Open(driver, &gorm.Config{NamingStrategy: schema.NamingStrategy{TablePrefix: "task_channel_test_"}})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			oldDB := model.DB
			model.DB = db
			t.Cleanup(func() {
				model.DB = oldDB
				require.NoError(t, db.Migrator().DropTable(&model.Channel{}))
				require.NoError(t, sqlDB.Close())
			})
			require.NoError(t, db.AutoMigrate(&model.Channel{}))
			query := "SELECT version()"
			if dialect == "sqlite" {
				query = "SELECT sqlite_version()"
			}
			var version string
			require.NoError(t, db.Raw(query).Scan(&version).Error)
			t.Logf("数据库版本: %s", version)
			require.NoError(t, db.Create(&model.Channel{Id: 69, Name: "图片主渠道", Key: "secret-key-canary"}).Error)
			require.NoError(t, db.Create(&model.Channel{Id: 100, Name: "视频备用渠道", Key: "secret-key-canary", Status: common.ChannelStatusManuallyDisabled}).Error)
			tasks := []*model.Task{{ChannelId: 69}, {ChannelId: 69}, {ChannelId: 100}, {ChannelId: 999}, {ChannelId: 0}}
			for _, role := range []int{common.RoleAdminUser, common.RoleRootUser} {
				views := tasksToDto(tasks, false, role)
				for i, name := range []string{"图片主渠道", "图片主渠道", "视频备用渠道", "", ""} {
					encoded, err := common.Marshal(views[i])
					require.NoError(t, err)
					var fields map[string]any
					require.NoError(t, common.Unmarshal(encoded, &fields))
					if name == "" {
						assert.NotContains(t, fields, "channel_name")
					} else {
						assert.Equal(t, name, fields["channel_name"])
					}
					assert.Equal(t, tasks[i].ChannelId, views[i].ChannelId)
					assert.NotContains(t, string(encoded), "secret-key-canary")
				}
			}
			encoded, err := common.Marshal(tasksToDto(tasks, false, common.RoleCommonUser))
			require.NoError(t, err)
			assert.NotContains(t, string(encoded), "channel_name")
			// 改名即时生效，删除渠道或查询失败都不能影响任务列表本身。
			require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 69).Update("name", "更新后的渠道").Error)
			encoded, err = common.Marshal(tasksToDto(tasks[:1], false, common.RoleAdminUser))
			require.NoError(t, err)
			assert.Contains(t, string(encoded), "更新后的渠道")
			require.NoError(t, db.Delete(&model.Channel{}, 69).Error)
			encoded, err = common.Marshal(tasksToDto(tasks[:1], false, common.RoleAdminUser))
			require.NoError(t, err)
			assert.NotContains(t, string(encoded), "channel_name")
			require.NoError(t, db.Migrator().DropTable(&model.Channel{}))
			assert.Len(t, tasksToDto(tasks, false, common.RoleRootUser), len(tasks))
			assert.Empty(t, tasksToDto(nil, false, common.RoleRootUser))
		})
	}
}

func TestTaskLogChannelNamesListVisibility(t *testing.T) {
	prepareAsyncMediaController(t)
	require.NoError(t, model.DB.Create(&model.User{Id: 31, Username: "channel-viewer", AffCode: "channel-viewer"}).Error)
	require.NoError(t, model.DB.Create(&model.Channel{Id: 69, Name: "图片主渠道", Key: "secret-key-canary"}).Error)
	task := &model.AsyncRelayTask{UserID: 31, Status: model.AsyncRelayTaskStatusSucceeded}
	require.NoError(t, task.InsertWithLog("default", "IMAGE"))
	require.NoError(t, model.DB.Model(&model.Task{}).Where("id = ?", task.LogID).Update("channel_id", 69).Error)
	for _, role := range []int{common.RoleAdminUser, common.RoleRootUser} {
		response := asyncControllerRequest(GetAllTask, http.MethodGet, "/api/task/", 31, role, nil, "")
		require.Equal(t, http.StatusOK, response.Code)
		assert.Contains(t, response.Body.String(), `"channel_name":"图片主渠道"`)
		assert.NotContains(t, response.Body.String(), "secret-key-canary")
		response = asyncControllerRequest(GetUserTask, http.MethodGet, "/api/task/self", 31, role, nil, "")
		assert.NotContains(t, response.Body.String(), "channel_name")
	}
}

func TestTaskLogDTOSeparatesUserAdminAndRootDetails(t *testing.T) {
	task := &model.Task{
		TaskID:   "task_public",
		Platform: "document-parser",
		PrivateData: model.TaskPrivateData{
			Key:            "channel-secret-canary",
			UpstreamTaskID: "upstream-private",
			NodeName:       "node-a",
			Execution: &model.TaskExecutionSnapshot{
				RequestID:   "request-public",
				RequestPath: "/v1/documents",
				TaskPlugin: &model.TaskPluginSnapshot{
					Key:     "document-parser",
					Name:    "Document Parser",
					Version: "1.2.3",
					Author: &model.TaskPluginAuthorSnapshot{
						Name: "Community Author",
						URL:  "https://plugins.example/author",
					},
					APIVersion: 1,
					Generation: 42,
				},
			},
		},
	}

	userView := tasksToDto([]*model.Task{task}, false, common.RoleCommonUser)[0]
	assert.Nil(t, userView.AdminInfo)
	assert.Nil(t, userView.RootInfo)

	adminView := tasksToDto([]*model.Task{task}, false, common.RoleAdminUser)[0]
	require.NotNil(t, adminView.AdminInfo)
	require.NotNil(t, adminView.AdminInfo.TaskPlugin)
	assert.Equal(t, "document-parser", adminView.AdminInfo.TaskPlugin.Key)
	assert.Equal(t, "Document Parser", adminView.AdminInfo.TaskPlugin.Name)
	assert.Equal(t, "1.2.3", adminView.AdminInfo.TaskPlugin.Version)
	require.NotNil(t, adminView.AdminInfo.TaskPlugin.Author)
	assert.Equal(t, "Community Author", adminView.AdminInfo.TaskPlugin.Author.Name)
	assert.Equal(t, "https://plugins.example/author", adminView.AdminInfo.TaskPlugin.Author.URL)
	assert.Equal(t, "request-public", adminView.AdminInfo.RequestID)
	assert.Equal(t, "/v1/documents", adminView.AdminInfo.RequestPath)
	assert.Nil(t, adminView.RootInfo)

	rootView := tasksToDto([]*model.Task{task}, false, common.RoleRootUser)[0]
	require.NotNil(t, rootView.AdminInfo)
	require.NotNil(t, rootView.RootInfo)
	require.NotNil(t, rootView.RootInfo.TaskPlugin)
	assert.Equal(t, 1, rootView.RootInfo.TaskPlugin.APIVersion)
	assert.Equal(t, uint64(42), rootView.RootInfo.TaskPlugin.Generation)
	assert.Equal(t, "upstream-private", rootView.RootInfo.UpstreamTaskID)
	assert.Equal(t, "node-a", rootView.RootInfo.NodeName)

	adminJSON, err := common.Marshal(adminView)
	require.NoError(t, err)
	assert.NotContains(t, string(adminJSON), "channel-secret-canary")
	assert.NotContains(t, string(adminJSON), "upstream-private")

	rootJSON, err := common.Marshal(rootView)
	require.NoError(t, err)
	assert.NotContains(t, string(rootJSON), "channel-secret-canary")
	assert.Contains(t, string(rootJSON), "upstream-private")
}

func TestTaskLogDTODoesNotInventHistoricalPluginProvenance(t *testing.T) {
	task := &model.Task{
		TaskID:   "task_without_snapshot",
		Platform: "document-parser",
	}

	adminView := tasksToDto([]*model.Task{task}, false, common.RoleAdminUser)[0]

	assert.Nil(t, adminView.AdminInfo)
	assert.Nil(t, adminView.RootInfo)
}

func TestTaskLogDTOReplacesLegacyVideoURLWithAvailabilityFlag(t *testing.T) {
	task := &model.Task{
		TaskID:     "task_legacy_video",
		Platform:   "jimeng",
		Action:     constant.TaskActionTextToVideo,
		Status:     model.TaskStatusSuccess,
		FailReason: "https://private-upstream.invalid/video.mp4?signature=secret",
	}

	view := tasksToDto([]*model.Task{task}, false, common.RoleCommonUser)[0]
	assert.True(t, view.LegacyVideoAvailable)
	assert.Empty(t, view.ResultURL)
	assert.Empty(t, view.FailReason)
	encoded, err := common.Marshal(view)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "private-upstream.invalid")
	assert.NotContains(t, string(encoded), "result_url")
	assert.Contains(t, string(encoded), "legacy_video_available")
}

func TestTaskLogDTOKeepsFailureReasonAndDoesNotMarkPluginTaskLegacy(t *testing.T) {
	failed := &model.Task{
		TaskID:     "task_failed",
		Platform:   "jimeng",
		Action:     constant.TaskActionTextToVideo,
		Status:     model.TaskStatusFailure,
		FailReason: "provider rejected the request",
	}
	failedView := tasksToDto([]*model.Task{failed}, false, common.RoleCommonUser)[0]
	assert.Equal(t, "provider rejected the request", failedView.FailReason)
	assert.False(t, failedView.LegacyVideoAvailable)

	pluginTask := &model.Task{
		TaskID:     "task_plugin_video",
		Platform:   "community-video",
		Action:     constant.TaskActionTextToVideo,
		Status:     model.TaskStatusSuccess,
		FailReason: "https://stale-upstream.invalid/plugin-video.mp4",
		PrivateData: model.TaskPrivateData{
			ResultURL: "https://private-upstream.invalid/plugin-video.mp4",
			Execution: &model.TaskExecutionSnapshot{
				TaskPlugin: &model.TaskPluginSnapshot{Key: "community-video"},
			},
		},
	}
	pluginView := tasksToDto([]*model.Task{pluginTask}, false, common.RoleCommonUser)[0]
	assert.False(t, pluginView.LegacyVideoAvailable)
	assert.Empty(t, pluginView.ResultURL)
	assert.Empty(t, pluginView.FailReason)
}
