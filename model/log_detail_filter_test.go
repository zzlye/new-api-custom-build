package model

import (
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func openLogDetailFilterTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	previousType := common.LogDatabaseType()
	previousLogDB := LOG_DB
	common.SetLogDatabaseType(common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		common.SetLogDatabaseType(previousType)
		LOG_DB = previousLogDB
	})

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Log{}))
	LOG_DB = db
	return db
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
