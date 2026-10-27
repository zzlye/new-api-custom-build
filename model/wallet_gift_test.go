package model

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func TestGiftWalletDatabaseMatrix(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			dsn := "local"
			if dialect == "mysql" {
				dsn = os.Getenv("TEST_MYSQL_DSN")
			}
			if dialect == "postgres" {
				dsn = os.Getenv("TEST_POSTGRES_DSN")
			}
			if dsn == "" {
				t.Skip("test database DSN is not configured")
			}
			oldDB, oldLogDB := DB, LOG_DB
			oldPath, oldRedis, oldBatch := common.SQLitePath, common.RedisEnabled, common.BatchUpdateEnabled
			mainType, logType := common.MainDatabaseType(), common.LogDatabaseType()
			t.Cleanup(func() {
				DB, LOG_DB = oldDB, oldLogDB
				common.SQLitePath, common.RedisEnabled, common.BatchUpdateEnabled = oldPath, oldRedis, oldBatch
				common.SetDatabaseTypes(mainType, logType)
				initCol()
			})
			common.SQLitePath = filepath.Join(t.TempDir(), "gift.db")
			t.Setenv("GIFT_TEST_DSN", dsn)
			db, dbType, err := chooseDB("GIFT_TEST_DSN", false)
			require.NoError(t, err)
			prefix := fmt.Sprintf("gift_%d_", time.Now().UnixNano())
			db.Config.NamingStrategy = schema.NamingStrategy{TablePrefix: prefix}
			// 主库覆盖三种引擎，日志另存一个 SQLite 文件，验证账单缺失和独立日志库场景。
			primaryPath := common.SQLitePath
			common.SQLitePath = filepath.Join(t.TempDir(), "gift-log.db")
			t.Setenv("GIFT_TEST_LOG_DSN", "local")
			logDB, logDBType, err := chooseDB("GIFT_TEST_LOG_DSN", true)
			require.NoError(t, err)
			common.SQLitePath = primaryPath
			logSQLDB, err := logDB.DB()
			require.NoError(t, err)
			logSQLDB.SetMaxOpenConns(1)
			t.Cleanup(func() { _ = logSQLDB.Close() })
			require.NoError(t, logDB.AutoMigrate(&Log{}))
			DB, LOG_DB = db, logDB
			common.SetDatabaseTypes(dbType, logDBType)
			common.RedisEnabled, common.BatchUpdateEnabled = false, false
			initCol()
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(8)
			t.Cleanup(func() {
				_ = db.Migrator().DropTable(&Log{}, &Token{}, &User{}, &Checkin{}, &Task{}, &Midjourney{})
				_ = sqlDB.Close()
			})
			var version string
			versionSQL := "SELECT VERSION()"
			if dialect == "sqlite" {
				versionSQL = "SELECT sqlite_version()"
			}
			require.NoError(t, db.Raw(versionSQL).Scan(&version).Error)
			t.Logf("database=%s version=%s", dialect, version)
			// 注册奖励场景会自动创建默认媒体密钥，三个数据库均需准备令牌表。
			require.NoError(t, db.AutoMigrate(&User{}, &Token{}, &Log{}, &Checkin{}, &Task{}, &Midjourney{}))
			require.NoError(t, db.AutoMigrate(&User{}, &Token{}, &Log{}, &Checkin{}, &Task{}, &Midjourney{}))

			t.Run("奖励优先消费并按类别退款", func(t *testing.T) {
				u := User{Username: "gift-spend", AffCode: "gift-spend", Quota: 1000}
				require.NoError(t, DB.Create(&u).Error)
				require.NoError(t, CreditGiftQuota(u.Id, 400))
				gift, err := ConsumeWalletQuota(u.Id, 600, true)
				require.NoError(t, err)
				assert.Equal(t, 400, gift)
				require.NoError(t, DB.First(&u, u.Id).Error)
				assert.Equal(t, 800, u.Quota)
				assert.Zero(t, u.GiftQuota)
				giftRefund := WalletGiftRefund(600, gift, 300)
				assert.Equal(t, 100, giftRefund)
				require.NoError(t, RefundWalletQuota(u.Id, 300, giftRefund))
				require.NoError(t, DB.First(&u, u.Id).Error)
				assert.Equal(t, 1100, u.Quota)
				assert.Equal(t, 100, u.GiftQuota)
				_, err = ConsumeWalletQuota(u.Id, 1101, true)
				assert.ErrorIs(t, err, ErrInsufficientWalletQuota)
			})

			t.Run("管理员单独扣赠金不动普通余额", func(t *testing.T) {
				u := User{Username: "gift-adjust", AffCode: "gift-adjust", Quota: 1000, GiftQuota: 400, Role: common.RoleCommonUser}
				require.NoError(t, DB.Create(&u).Error)
				_, err := AdjustUserQuota(u.Id, common.RoleRootUser, "subtract", 150, "gift")
				require.NoError(t, err)
				require.NoError(t, DB.First(&u, u.Id).Error)
				assert.Equal(t, 850, u.Quota)
				assert.Equal(t, 250, u.GiftQuota)
				assert.Equal(t, 600, u.Quota-u.GiftQuota)
				_, err = AdjustUserQuota(u.Id, common.RoleRootUser, "subtract", 251, "gift")
				assert.ErrorIs(t, err, ErrInsufficientGiftQuota)
				_, err = AdjustUserQuota(u.Id, common.RoleRootUser, "override", 300, "balance")
				require.NoError(t, err)
				require.NoError(t, DB.First(&u, u.Id).Error)
				assert.Equal(t, 550, u.Quota)
				assert.Equal(t, 250, u.GiftQuota)
				_, err = AdjustUserQuota(u.Id, common.RoleCommonUser, "add", 50, "gift")
				assert.ErrorIs(t, err, ErrUserQuotaPermission)
			})

			t.Run("并发扣款不会重复花赠金", func(t *testing.T) {
				u := User{Username: "gift-concurrent", AffCode: "gift-concurrent", Quota: 1000, GiftQuota: 400}
				require.NoError(t, DB.Create(&u).Error)
				var wg sync.WaitGroup
				results := make(chan int, 2)
				errs := make(chan error, 2)
				for range 2 {
					wg.Go(func() { gift, err := ConsumeWalletQuota(u.Id, 600, true); results <- gift; errs <- err })
				}
				wg.Wait()
				close(results)
				close(errs)
				gifts, successes := 0, 0
				for gift := range results {
					gifts += gift
				}
				for err := range errs {
					if err == nil {
						successes++
					} else {
						assert.ErrorIs(t, err, ErrInsufficientWalletQuota)
					}
				}
				assert.Equal(t, 1, successes)
				assert.Equal(t, 400, gifts)
				require.NoError(t, DB.First(&u, u.Id).Error)
				assert.Equal(t, 400, u.Quota)
				assert.Zero(t, u.GiftQuota)
			})

			t.Run("旧表升级重复迁移不重复送余额", func(t *testing.T) {
				require.NoError(t, DB.Migrator().DropColumn(&User{}, "gift_quota"))
				require.NoError(t, DB.Migrator().DropColumn(&User{}, "gift_quota_version"))
				require.NoError(t, DB.AutoMigrate(&User{}))
				require.NoError(t, DB.AutoMigrate(&User{}))
				u := User{Username: "gift-legacy", AffCode: "gift-legacy", Quota: 1250000, UsedQuota: 250000, AffHistoryQuota: 500000}
				require.NoError(t, DB.Create(&u).Error)
				for i, log := range []Log{
					{Type: LogTypeTopup, Content: "使用在线充值成功，充值金额: ＄2.000000 额度"},
					{Type: LogTypeSystem, Content: "邀请用户充值返利 ＄1.000000 额度，已自动到账余额"},
					{Type: LogTypeConsume, Quota: 250000, RequestId: "gift-history", Other: `{"task_id":"gift-pending"}`},
				} {
					log.UserId = u.Id
					log.CreatedAt = int64(i + 1)
					require.NoError(t, LOG_DB.Create(&log).Error)
				}
				missing := User{Username: "gift-missing", AffCode: "gift-missing", Quota: 900000, AffHistoryQuota: 500000}
				require.NoError(t, DB.Create(&missing).Error)
				pending := Task{TaskID: "gift-pending", UserId: u.Id, Quota: 250000, Status: TaskStatusSubmitted}
				require.NoError(t, DB.Create(&pending).Error)
				require.NoError(t, DB.Migrator().DropColumn(&Task{}, "gift_quota"))
				require.NoError(t, DB.Migrator().DropColumn(&Midjourney{}, "gift_quota"))
				require.NoError(t, DB.AutoMigrate(&Task{}, &Midjourney{}))
				require.NoError(t, DB.AutoMigrate(&Task{}, &Midjourney{}))
				partial := User{Username: "gift-partial", AffCode: "gift-partial", Quota: 700000, UsedQuota: 8000000, AffHistoryQuota: 2000000}
				require.NoError(t, DB.Create(&partial).Error)
				require.NoError(t, LOG_DB.Create(&Log{UserId: partial.Id, Type: LogTypeSystem, Content: "邀请用户充值返利 ＄1.000000 额度", CreatedAt: 1}).Error)
				require.NoError(t, LOG_DB.Create(&Log{UserId: partial.Id, Type: LogTypeConsume, Quota: 100000, CreatedAt: 2}).Error)
				// 模拟主进程首次启动：缓存开关已开启，但 Redis 客户端尚未初始化。
				common.RedisEnabled = true
				require.NotPanics(t, func() { require.NoError(t, ReconcileLegacyGiftQuotas()) })
				common.RedisEnabled = false
				require.NoError(t, ReconcileLegacyGiftQuotas())
				require.NoError(t, DB.First(&u, u.Id).Error)
				assert.Equal(t, 1250000, u.Quota)
				assert.Equal(t, 250000, u.GiftQuota)
				assert.Equal(t, 1, u.GiftQuotaVersion)
				require.NoError(t, DB.First(&pending, pending.ID).Error)
				assert.Equal(t, 250000, pending.Quota)
				assert.Equal(t, 250000, pending.GiftQuota)
				require.NoError(t, DB.First(&missing, missing.Id).Error)
				assert.Equal(t, 900000, missing.Quota)
				assert.Zero(t, missing.GiftQuota)
				assert.Equal(t, 1, missing.GiftQuotaVersion)
				require.NoError(t, DB.First(&partial, partial.Id).Error)
				assert.Equal(t, 700000, partial.Quota)
				assert.Equal(t, 400000, partial.GiftQuota)
				// 消费后再次启动不得从历史奖励中把已花掉的赠金重新补回。
				_, err := ConsumeWalletQuota(partial.Id, 200000, true)
				require.NoError(t, err)
				require.NoError(t, ReconcileLegacyGiftQuotas())
				require.NoError(t, DB.First(&partial, partial.Id).Error)
				assert.Equal(t, 500000, partial.Quota)
				assert.Equal(t, 200000, partial.GiftQuota)
			})
			t.Run("欠费账户奖励仍受赠金上限约束", func(t *testing.T) {
				u := User{Username: "gift-limit", AffCode: "gift-limit", Quota: 100, GiftQuota: common.MaxWalletQuota}
				require.NoError(t, DB.Create(&u).Error)
				assert.ErrorIs(t, CreditGiftQuota(u.Id, 1), ErrTopUpQuotaLimitExceeded)
				require.NoError(t, DB.First(&u, u.Id).Error)
				assert.Equal(t, 100, u.Quota)
				assert.Equal(t, common.MaxWalletQuota, u.GiftQuota)
			})
			t.Run("注册奖励邀请奖励签到进入赠金", func(t *testing.T) {
				previous := common.QuotaForNewUser
				common.QuotaForNewUser = 250
				t.Cleanup(func() { common.QuotaForNewUser = previous })
				u := User{Username: "gift-sources"}
				require.NoError(t, DB.Transaction(func(tx *gorm.DB) error { return u.InsertWithTx(tx, 0) }))
				assert.Equal(t, 250, u.Quota)
				assert.Equal(t, 250, u.GiftQuota)
				assert.Equal(t, 1, u.GiftQuotaVersion)
				require.NoError(t, inviteUser(u.Id, 100))
				checkin := &Checkin{UserId: u.Id, CheckinDate: "2026-10-11", QuotaAwarded: 50}
				if dialect == "sqlite" {
					_, err = userCheckinWithoutTransaction(checkin, u.Id, 50)
				} else {
					_, err = userCheckinWithTransaction(checkin, u.Id, 50)
				}
				require.NoError(t, err)
				require.NoError(t, DB.First(&u, u.Id).Error)
				assert.Equal(t, 400, u.Quota)
				assert.Equal(t, 400, u.GiftQuota)
				require.NoError(t, IncreaseUserQuota(u.Id, 600, true))
				require.NoError(t, DB.First(&u, u.Id).Error)
				assert.Equal(t, 1000, u.Quota)
				assert.Equal(t, 400, u.GiftQuota)
			})
		})
	}
}

func TestLegacyGiftReplayRewardsConsumptionAndRefunds(t *testing.T) {
	r := legacyGiftReplay{charges: make(map[string][2]int)}
	for _, content := range []string{"新用户注册赠送 ＄1.000000 额度", "使用邀请码赠送 ＄1.000000 额度", "邀请用户赠送 ＄1.000000 额度", "用户签到，获得额度 ＄1.000000 额度"} {
		require.NoError(t, r.apply(Log{Type: LogTypeSystem, Content: content}))
	}
	assert.Equal(t, 2000000, r.gift)
	require.NoError(t, r.apply(Log{Type: LogTypeConsume, Quota: 2500000, Other: `{"task_id":"video"}`}))
	assert.Zero(t, r.gift)
	require.NoError(t, r.apply(Log{Type: LogTypeRefund, Quota: 750000, Other: `{"task_id":"video"}`}))
	assert.Equal(t, 250000, r.gift)
	require.NoError(t, r.apply(Log{Type: LogTypeRefund, Quota: 500000, RequestId: "missing-charge"}))
	require.NoError(t, r.apply(Log{Type: LogTypeConsume, Quota: 500000, Other: `{"billing_source":"subscription"}`}))
	assert.Equal(t, 250000, r.gift)
	require.NoError(t, r.apply(Log{Type: LogTypeTopup, Content: "使用余额购买订阅成功，扣除额度: 100000"}))
	assert.Equal(t, 150000, r.gift)
	require.NoError(t, r.apply(Log{UserId: 1, Type: LogTypeManage, Other: `{"op":{"action":"user.quota_subtract","params":{"target_user_id":42,"from":400000,"to":200000}}}`}))
	assert.Equal(t, 150000, r.gift)
	require.NoError(t, r.apply(Log{Type: LogTypeTopup, Other: `{"op":{"action":"user.quota_subtract","params":{"from":400000,"to":200000}}}`}))
	assert.Zero(t, r.gift)
}
