package model

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func openLogDetailFilterTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	previousType := common.LogDatabaseType()
	previousLogDB := LOG_DB
	dialect := common.DatabaseType(os.Getenv("TEST_LOG_STATS_DIALECT"))
	var driver gorm.Dialector
	switch dialect {
	case "", common.DatabaseTypeSQLite:
		dialect, driver = common.DatabaseTypeSQLite, sqlite.Open(":memory:")
	case common.DatabaseTypeMySQL:
		require.NotEmpty(t, os.Getenv("TEST_MYSQL_DSN"))
		driver = mysql.Open(os.Getenv("TEST_MYSQL_DSN"))
	case common.DatabaseTypePostgreSQL:
		require.NotEmpty(t, os.Getenv("TEST_POSTGRES_DSN"))
		driver = postgres.Open(os.Getenv("TEST_POSTGRES_DSN"))
	default:
		t.Fatalf("未知测试数据库 %q", dialect)
	}
	common.SetLogDatabaseType(dialect)
	initCol()
	t.Cleanup(func() {
		common.SetLogDatabaseType(previousType)
		initCol()
		LOG_DB = previousLogDB
	})

	db, err := gorm.Open(driver, &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	require.NoError(t, db.AutoMigrate(&Log{}))
	// 外部数据库仅由独立测试库运行，清理本次日志表以隔离下一用例。
	t.Cleanup(func() { require.NoError(t, db.Migrator().DropTable(&Log{})) })
	versionQuery := "SELECT version()"
	if dialect == common.DatabaseTypeSQLite {
		versionQuery = "SELECT sqlite_version()"
	}
	var version string
	require.NoError(t, db.Raw(versionQuery).Scan(&version).Error)
	t.Logf("database: %s %s", dialect, version)
	LOG_DB = db
	return db
}

// 退款是正数流水，汇总必须区分扣费与退款，不能把退款叠加到用量或漏掉跨日退款。
func TestLogStatsRefundTotals(t *testing.T) {
	db := openLogDetailFilterTestDB(t)
	now := time.Now().Unix()
	base := Log{UserId: 10, Username: "alice", ModelName: "video", ChannelId: 223, Group: "video-group", TokenName: "video-key", CreatedAt: now, Content: "match", Other: `{}`}
	consume, refund, partial := base, base, base
	consume.Type, consume.Quota, consume.PromptTokens = LogTypeConsume, 107, 5
	refund.Type, refund.Quota, refund.PromptTokens = LogTypeRefund, 60, 100
	partial.Type, partial.Quota = LogTypeRefund, 12
	rows := []Log{consume, refund, partial}
	// 每个筛选维度放一条不应计入的退款，避免只验证无筛选的总数。
	for _, field := range []string{"user", "model", "channel", "group", "token", "time", "detail", "type"} {
		row := refund
		row.Quota = 1000
		switch field {
		case "user":
			row.Username = "bob"
		case "model":
			row.ModelName = "other"
		case "channel":
			row.ChannelId = 224
		case "group":
			row.Group = "other"
		case "token":
			row.TokenName = "other"
		case "time":
			row.CreatedAt = now - 86400
		case "detail":
			row.Content = "different"
		case "type":
			row.Type = LogTypeTopup
		}
		rows = append(rows, row)
	}
	require.NoError(t, db.Create(&rows).Error)
	for _, logType := range []int{LogTypeUnknown, LogTypeConsume, LogTypeRefund} {
		// 类型只控制下方流水列表；总览始终同时展示扣费和退款，沿用原统计口径。
		stat, err := SumUsedQuota(logType, now-1, now+1, "video", "alice", "video-key", 223, "video-group", "match", false)
		require.NoError(t, err)
		encoded, err := common.Marshal(stat)
		require.NoError(t, err)
		var values map[string]int
		require.NoError(t, common.Unmarshal(encoded, &values))
		assert.Equal(t, 107, values["quota"])
		assert.Equal(t, 72, values["refund_quota"])
		assert.Equal(t, 1, stat.Rpm, "退款不能增加请求速率")
		assert.Equal(t, 5, stat.Tpm, "退款不能增加 Token 速率")
	}
	for _, tc := range []struct {
		name          string
		quota, refund int
	}{
		{"empty", 0, 0}, {"full", 30, 30}, {"cross-day", 0, 30},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, b := base, base
			a.ModelName, b.ModelName = tc.name, tc.name
			a.Type, a.Quota, b.Type, b.Quota = LogTypeConsume, tc.quota, LogTypeRefund, tc.refund
			if tc.name != "empty" {
				require.NoError(t, db.Create(&[]Log{a, b}).Error)
			}
			stat, err := SumUsedQuota(0, now-1, now+1, tc.name, "alice", "", 0, "", "", false)
			require.NoError(t, err)
			encoded, err := common.Marshal(stat)
			require.NoError(t, err)
			var values map[string]int
			require.NoError(t, common.Unmarshal(encoded, &values))
			assert.Equal(t, tc.quota, values["quota"])
			assert.Equal(t, tc.refund, values["refund_quota"])
		})
	}
}

func findLogIDsByDetail(t *testing.T, db *gorm.DB, detail string, includeFullOther bool) []int {
	t.Helper()
	tx, err := applyLogDetailFilter(db.Model(&Log{}), detail, includeFullOther)
	require.NoError(t, err)

	var logs []Log
	require.NoError(t, tx.Order("id asc").Find(&logs).Error)
	ids := make([]int, 0, len(logs))
	for _, log := range logs {
		ids = append(ids, log.Id)
	}
	return ids
}

func TestApplyLogDetailFilterMatchesErrorCodes(t *testing.T) {
	db := openLogDetailFilterTestDB(t)
	require.NoError(t, db.Create([]Log{
		{Id: 1, Content: "status_code=429, rate limited", Other: `{"error_code":"rate_limit"}`},
		{Id: 2, Content: "status_code=500, upstream failed", Other: `{"error_code":"server_error"}`},
		{Id: 3, Content: "request rejected", Other: `{"error_code":"insufficient_quota"}`},
		{Id: 4, Content: "progress 100% complete", Other: `{}`},
		{Id: 5, Content: "progress 1000 complete", Other: `{}`},
		{Id: 6, Content: "request completed", Other: `{"admin_info":{"route":"hidden-marker"}}`},
		{Id: 7, Content: "request rejected", Other: `{"error_code":"quota_100%"}`},
	}).Error)

	assert.Equal(t, []int{1}, findLogIDsByDetail(t, db, "429", true))
	assert.Equal(t, []int{3}, findLogIDsByDetail(t, db, "insufficient_quota", true))
	assert.Equal(t, []int{3}, findLogIDsByDetail(t, db, "insufficient_quota", false))
	assert.Equal(t, []int{4, 7}, findLogIDsByDetail(t, db, "100%", true))
	assert.Empty(t, findLogIDsByDetail(t, db, "hidden-marker", false))
	assert.Equal(t, []int{6}, findLogIDsByDetail(t, db, "hidden-marker", true))
	assert.Equal(t, []int{7}, findLogIDsByDetail(t, db, "quota_100%", false))
	assert.Empty(t, findLogIDsByDetail(t, db, "quota_100", false))
}

func TestLogDetailFilterKeepsListTotalAndStatsConsistent(t *testing.T) {
	db := openLogDetailFilterTestDB(t)
	now := time.Now().Unix()
	require.NoError(t, db.Create([]Log{
		{
			Id: 1, UserId: 10, Username: "alice", CreatedAt: now, Type: LogTypeConsume,
			Content: "upstream rejected request", Other: `{"error_code":"rate_limit","admin_info":{"route":"secret"}}`,
			Quota: 100, PromptTokens: 3, CompletionTokens: 2,
		},
		{
			Id: 2, UserId: 10, Username: "alice", CreatedAt: now, Type: LogTypeConsume,
			Content: "request completed", Other: `{"error_code":"ok"}`,
			Quota: 200, PromptTokens: 5, CompletionTokens: 5,
		},
		{
			Id: 3, UserId: 20, Username: "bob", CreatedAt: now, Type: LogTypeConsume,
			Content: "upstream rejected request", Other: `{"error_code":"rate_limit"}`,
			Quota: 300, PromptTokens: 7, CompletionTokens: 3,
		},
	}).Error)

	logs, total, err := GetUserLogs(10, LogTypeUnknown, 0, 0, "", "", 0, 20, "", "", "", "rate_limit")
	require.NoError(t, err)
	require.Len(t, logs, 1)
	assert.Equal(t, int64(1), total)
	assert.Contains(t, logs[0].Other, `"error_code":"rate_limit"`)
	assert.NotContains(t, logs[0].Other, "admin_info")

	hiddenLogs, hiddenTotal, err := GetUserLogs(10, LogTypeUnknown, 0, 0, "", "", 0, 20, "", "", "", "secret")
	require.NoError(t, err)
	assert.Empty(t, hiddenLogs)
	assert.Zero(t, hiddenTotal)

	userStat, err := SumUsedQuota(LogTypeUnknown, 0, 0, "", "alice", "", 0, "", "rate_limit", false)
	require.NoError(t, err)
	assert.Equal(t, Stat{Quota: 100, Rpm: 1, Tpm: 5}, userStat)
	hiddenStat, err := SumUsedQuota(LogTypeUnknown, 0, 0, "", "alice", "", 0, "", "secret", false)
	require.NoError(t, err)
	assert.Equal(t, Stat{}, hiddenStat)
	adminHiddenLogs, adminHiddenTotal, err := GetAllLogs(LogTypeUnknown, 0, 0, "", "", "", 0, 20, 0, "", "", "", "secret")
	require.NoError(t, err)
	assert.Len(t, adminHiddenLogs, 1)
	assert.Equal(t, int64(1), adminHiddenTotal)
	adminHiddenStat, err := SumUsedQuota(LogTypeUnknown, 0, 0, "", "", "", 0, "", "secret", true)
	require.NoError(t, err)
	assert.Equal(t, Stat{Quota: 100, Rpm: 1, Tpm: 5}, adminHiddenStat)

	adminLogs, adminTotal, err := GetAllLogs(LogTypeUnknown, 0, 0, "", "", "", 0, 20, 0, "", "", "", "rate_limit")
	require.NoError(t, err)
	assert.Len(t, adminLogs, 2)
	assert.Equal(t, int64(2), adminTotal)

	adminStat, err := SumUsedQuota(LogTypeUnknown, 0, 0, "", "", "", 0, "", "rate_limit", true)
	require.NoError(t, err)
	assert.Equal(t, Stat{Quota: 400, Rpm: 2, Tpm: 15}, adminStat)
}

func TestApplyLogDetailFilterRejectsOversizedKeyword(t *testing.T) {
	db := openLogDetailFilterTestDB(t)
	_, err := applyLogDetailFilter(
		db.Model(&Log{}),
		strings.Repeat("错", logDetailFilterMaxRunes+1),
		true,
	)
	require.ErrorContains(t, err, "不能超过 256 个字符")
}

func TestBuildLogContainsConditionUsesClickHouseEscaping(t *testing.T) {
	previousType := common.LogDatabaseType()
	common.SetLogDatabaseType(common.DatabaseTypeClickHouse)
	t.Cleanup(func() {
		common.SetLogDatabaseType(previousType)
	})

	condition, pattern := buildLogContainsCondition("logs.content", `rate_100%\retry`)

	assert.Equal(t, "logs.content LIKE ?", condition)
	assert.Equal(t, `%rate\_100\%\\retry%`, pattern)

	errorCodeCondition, errorCodePattern := buildLogJSONFieldEqualsCondition("logs.other", "error_code", `rate_100%\retry`)
	assert.Equal(t, "logs.other LIKE ?", errorCodeCondition)
	assert.Equal(t, `%"error\_code":"rate\_100\%\\\\retry"%`, errorCodePattern)
}
