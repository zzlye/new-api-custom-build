package controller

import (
	"context"
	"database/sql"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// 用独立连接在事务首次读取后竞争写锁，稳定复现注销与会话撤销的读锁升级问题。
func TestSecurityAccountSQLiteWriteLockBeforeRead(t *testing.T) {
	for _, table := range []string{"users", "user_sessions"} {
		t.Run(table, func(t *testing.T) {
			user, identity := setupSecurityEnrollmentTest(t)
			if !common.UsingMainDatabase(common.DatabaseTypeSQLite) {
				t.Skip("仅验证 SQLite 的 WAL 读锁升级行为")
			}
			require.NoError(t, model.DB.Create(&model.Option{Key: "write-lock-probe", Value: "before"}).Error)
			pool, err := model.DB.DB()
			require.NoError(t, err)
			conn, err := pool.Conn(context.Background())
			require.NoError(t, err)
			defer conn.Close()
			_, err = conn.ExecContext(context.Background(), "PRAGMA busy_timeout = 10")
			require.NoError(t, err)
			var attempted bool
			var contenderErr error
			callback := "test:account_sqlite_write_lock"
			require.NoError(t, model.DB.Callback().Query().After("gorm:query").Register(callback, func(tx *gorm.DB) {
				_, inTransaction := tx.Statement.ConnPool.(*sql.Tx)
				if attempted || !inTransaction || tx.Statement.Table != table {
					return
				}
				attempted = true
				_, contenderErr = conn.ExecContext(context.Background(), "UPDATE options SET value = ? WHERE key = ?", "after", "write-lock-probe")
			}))
			defer func() { require.NoError(t, model.DB.Callback().Query().Remove(callback)) }()
			if table == "users" {
				require.NoError(t, model.DeleteUserForSession(identity))
				var deleted model.User
				require.NoError(t, model.DB.Unscoped().First(&deleted, user.Id).Error)
				assert.True(t, deleted.DeletedAt.Valid)
				assert.Equal(t, user.AuthVersion+1, deleted.AuthVersion)
			} else {
				affected, err := model.RevokeAllUserSessions(user.Id, "test")
				require.NoError(t, err)
				assert.EqualValues(t, 1, affected)
			}
			assert.True(t, attempted, "必须在真实事务读取后触发竞争连接")
			var sqliteError interface{ Code() int }
			require.ErrorAs(t, contenderErr, &sqliteError)
			assert.Equal(t, 5, sqliteError.Code()&255, "竞争连接必须返回 SQLITE_BUSY")
			var session model.UserSession
			require.NoError(t, model.DB.Where("sid = ?", identity.SessionID).First(&session).Error)
			assert.Equal(t, model.UserSessionStatusRevoked, session.Status)
		})
	}
}
