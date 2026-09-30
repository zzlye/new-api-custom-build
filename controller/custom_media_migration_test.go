package controller

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 三种媒体表的定义与升级前一致；旧任务行使用原有 JSON 字段验证新版读取兼容性。
func TestCustomMediaDatabaseMatrix(t *testing.T) {
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
	previousMaster, previousSQLite, previousRedis := common.IsMasterNode, common.SQLitePath, common.RedisEnabled
	common.IsMasterNode, common.RedisEnabled = true, false
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMain, previousLog)
		common.IsMasterNode, common.SQLitePath, common.RedisEnabled = previousMaster, previousSQLite, previousRedis
	})
	for _, tc := range []struct {
		name, env string
		typ       common.DatabaseType
	}{
		{"sqlite", "", common.DatabaseTypeSQLite},
		{"mysql", "AUDIT_MYSQL_DSN", common.DatabaseTypeMySQL},
		{"postgres", "AUDIT_POSTGRES_DSN", common.DatabaseTypePostgreSQL},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dsn := os.Getenv(tc.env)
			if tc.env != "" && dsn == "" {
				t.Skip(tc.env + " is not configured")
			}
			for _, upgrade := range []bool{false, true} {
				t.Run(fmt.Sprintf("upgrade=%v", upgrade), func(t *testing.T) {
					db, isolated := newAuditTestDatabase(t, tc.name, dsn)
					model.DB, model.LOG_DB = db, db
					common.SetDatabaseTypes(tc.typ, tc.typ)
					t.Setenv("LOG_SQL_DSN", "")
					if tc.name == "sqlite" {
						common.SQLitePath = isolated
						t.Setenv("SQL_DSN", "local")
					} else {
						t.Setenv("SQL_DSN", isolated)
					}
					legacy := releasedCustomMediaTask{
						TaskID: "legacy-task", Status: "SUCCESS", IsAsync: true, Quota: 4321,
						Properties:  `{"origin_model_name":"legacy-video","input":"旧提示词"}`,
						PrivateData: `{"upstream_task_id":"upstream-legacy","result_url":"https://media.example/legacy.mp4","video_input":{"resolution":"720p"}}`,
						Data:        json.RawMessage(`{"duration":5}`),
					}
					if upgrade {
						require.NoError(t, db.AutoMigrate(&model.AsyncRelayTask{}, &model.VideoAsset{}, &model.VideoAssetUse{}, &releasedCustomMediaTask{}))
						require.NoError(t, db.Create(&legacy).Error)
						require.NoError(t, db.Create(&model.AsyncRelayTask{TaskID: "legacy-async", UserID: 1, LogID: legacy.ID, Status: model.AsyncRelayTaskStatusSucceeded, ResultFiles: "[]", RequestDetails: `{"prompt":"旧提示词"}`}).Error)
						require.NoError(t, db.Create(&model.VideoAsset{ID: "legacy-asset", UserID: 1, Path: "legacy/reference.png", Kind: "image", Size: 123}).Error)
						require.NoError(t, db.Create(&model.VideoAssetUse{AssetID: "legacy-asset", TaskID: "legacy-async"}).Error)
					}
					for range 2 {
						require.NoError(t, model.InitDB())
						conn, err := model.DB.DB()
						require.NoError(t, err)
						t.Cleanup(func() { _ = conn.Close() })
						require.NoError(t, model.InitLogDB())
					}
					for _, table := range []any{&model.AsyncRelayTask{}, &model.VideoAsset{}, &model.VideoAssetUse{}} {
						assert.True(t, model.DB.Migrator().HasTable(table))
					}
					if upgrade {
						var task model.Task
						require.NoError(t, model.DB.Where("task_id = ?", legacy.TaskID).First(&task).Error)
						assert.Equal(t, 4321, task.Quota)
						assert.Equal(t, "旧提示词", task.Properties.Input)
						assert.Equal(t, "legacy-video", task.Properties.OriginModelName)
						assert.Equal(t, "upstream-legacy", task.PrivateData.UpstreamTaskID)
						assert.Equal(t, "https://media.example/legacy.mp4", task.PrivateData.ResultURL)
						assert.True(t, task.ResultRetrievable())
						assert.JSONEq(t, string(legacy.Data), string(task.Data))
						parent, err := model.GetAsyncRelayTaskByTaskID("legacy-async")
						require.NoError(t, err)
						require.NotNil(t, parent)
						assert.Equal(t, legacy.ID, parent.LogID)
						assert.Contains(t, parent.RequestDetails, "旧提示词")
						var asset model.VideoAsset
						require.NoError(t, model.DB.Where("id = ?", "legacy-asset").First(&asset).Error)
						assert.Equal(t, "legacy/reference.png", asset.Path)
						require.Error(t, model.DB.Create(&model.VideoAssetUse{AssetID: "legacy-asset", TaskID: "legacy-async"}).Error, "升级后素材使用关系仍保持唯一")
					}
				})
			}
		})
	}
}

// 保留原发布版任务表字段与索引，以验证重复启动不会丢失历史媒体信息。
type releasedCustomMediaTask struct {
	// 后台生成的上游子任务只参与轮询和计费，不在任务日志重复展示。
	AsyncParentID string `json:"-" gorm:"type:varchar(191);index"`
	IsAsync       bool   `json:"is_async" gorm:"index"`
	ID            int64  `json:"id" gorm:"primary_key;AUTO_INCREMENT"`
	CreatedAt     int64  `json:"created_at" gorm:"index"`
	UpdatedAt     int64  `json:"updated_at"`
	TaskID        string `json:"task_id" gorm:"type:varchar(191);index"` // 第三方id，不一定有/ song id\ Task id
	Platform      string `json:"platform" gorm:"type:varchar(30);index"` // 平台
	UserId        int    `json:"user_id" gorm:"index"`
	Group         string `json:"group" gorm:"type:varchar(50)"` // 修正计费用
	ChannelId     int    `json:"channel_id" gorm:"index"`
	Quota         int    `json:"quota"`
	Action        string `json:"action" gorm:"type:varchar(40);index"` // 任务类型, song, lyrics, description-mode
	Status        string `json:"status" gorm:"type:varchar(20);index"` // 任务状态
	FailReason    string `json:"fail_reason"`
	SubmitTime    int64  `json:"submit_time" gorm:"index"`
	StartTime     int64  `json:"start_time" gorm:"index"`
	FinishTime    int64  `json:"finish_time" gorm:"index"`
	Progress      string `json:"progress" gorm:"type:varchar(20);index"`
	Properties    string `json:"properties" gorm:"type:json"`
	Username      string `json:"username,omitempty" gorm:"-"`
	// 禁止返回给用户，内部可能包含key等隐私信息
	PrivateData string          `json:"-" gorm:"column:private_data;type:json"`
	Data        json.RawMessage `json:"data" gorm:"type:json"`
}

func (releasedCustomMediaTask) TableName() string { return "tasks" }
