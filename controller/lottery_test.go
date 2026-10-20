package controller

import (
	"context"
	"fmt"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 同一组真实业务覆盖三种数据库，包含旧钱包升级、并发抢最后一份与请求重放。
func TestLotteryDatabaseMatrix(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			dsn := os.Getenv("TEST_" + strings.ToUpper(dialect) + "_DSN")
			if dialect != "sqlite" && dsn == "" {
				t.Skip("未配置真实测试数据库")
			}
			db, _ := newAuditTestDatabase(t, dialect, dsn)
			oldDB, oldLog, oldRedis, oldUnit := model.DB, model.LOG_DB, common.RedisEnabled, common.QuotaPerUnit
			mainType, logType := common.MainDatabaseType(), common.LogDatabaseType()
			t.Cleanup(func() {
				model.DB, model.LOG_DB, common.RedisEnabled, common.QuotaPerUnit = oldDB, oldLog, oldRedis, oldUnit
				common.SetDatabaseTypes(mainType, logType)
			})
			model.DB, model.LOG_DB, common.RedisEnabled, common.QuotaPerUnit = db, db, false, 100
			common.SetDatabaseTypes(common.DatabaseType(dialect), common.DatabaseType(dialect))
			t.Run("全新数据库重复建表和流水唯一约束", func(t *testing.T) {
				fresh, _ := newAuditTestDatabase(t, dialect, dsn)
				for range 2 {
					require.NoError(t, fresh.AutoMigrate(&model.LotteryActivity{}, &model.LotteryDraw{}))
				}
				a := model.LotteryActivity{Name: "全新活动", Status: model.LotteryDraft, TotalQuota: 100, RemainingQuota: 100}
				require.NoError(t, fresh.Create(&a).Error)
				draw := model.LotteryDraw{ActivityID: a.ID, UserID: 1, RequestID: "fresh-unique-request", Quota: 10}
				require.NoError(t, fresh.Create(&draw).Error)
				draw.ID = 0
				require.Error(t, fresh.Create(&draw).Error)
			})
			t.Run("旧抽奖流水升级保留记录和幂等唯一约束", func(t *testing.T) {
				fresh, _ := newAuditTestDatabase(t, dialect, dsn)
				require.NoError(t, fresh.AutoMigrate(&lotteryDrawBeforeCleanup{}))
				old := lotteryDrawBeforeCleanup{ActivityID: 1, UserID: 1, RequestID: "upgrade-request-1", Quota: 10, CreatedAt: 100, IP: "192.0.2.1"}
				require.NoError(t, fresh.Create(&old).Error)
				for range 2 {
					require.NoError(t, fresh.AutoMigrate(&model.LotteryDraw{}))
				}
				var migrated model.LotteryDraw
				require.NoError(t, fresh.First(&migrated, old.ID).Error)
				assert.Zero(t, migrated.LogClearedAt)
				assert.Equal(t, old.Quota, migrated.Quota)
				assert.Equal(t, old.IP, migrated.IP)
				assert.True(t, fresh.Migrator().HasIndex(&model.LotteryDraw{}, "idx_lottery_user"))
				migrated.ID = 0
				require.Error(t, fresh.Create(&migrated).Error)
			})
			var version string
			versionSQL := "SELECT VERSION()"
			if dialect == "sqlite" {
				versionSQL = "SELECT sqlite_version()"
			}
			require.NoError(t, db.Raw(versionSQL).Scan(&version).Error)
			t.Log("数据库版本：", version)
			require.NoError(t, db.AutoMigrate(&model.User{}, &model.TopUp{}))
			for _, user := range []model.User{
				{Id: 1, Username: "paid-user", AffCode: "paid-user", Status: 1, Quota: 100, GiftQuota: 90},
				{Id: 2, Username: "gift-only", AffCode: "gift-only", Status: 1, Quota: 100000, GiftQuota: 100000},
				{Id: 3, Username: "wallet-full", AffCode: "wallet-full", Status: 1, Quota: common.MaxWalletQuota},
				{Id: 4, Username: "disabled", AffCode: "disabled", Status: 2},
			} {
				require.NoError(t, db.Create(&user).Error)
			}
			// 充值底账已经存在时增加活动表；重复迁移不能改变老账号和历史订单。
			require.NoError(t, db.Create(&model.TopUp{UserId: 1, TradeNo: "paid-100", Amount: 100, Money: 80, Status: common.TopUpStatusSuccess, PaymentProvider: model.PaymentProviderEpay, LogClearedAt: 1}).Error)
			// 使用上一版本活动结构建立旧库，再验证新增门槛字段的默认值与重复迁移。
			require.NoError(t, db.AutoMigrate(&lotteryActivityBeforePeriod{}))
			require.NoError(t, db.Create(&lotteryActivityBeforePeriod{Name: "升级前活动", Status: model.LotteryDraft, Mode: model.LotteryFixed, TotalQuota: 100, RemainingQuota: 100, FixedQuota: 10, PerUserLimit: 1}).Error)
			for range 2 {
				require.NoError(t, db.AutoMigrate(&model.User{}, &model.TopUp{}, &model.LotteryActivity{}, &model.LotteryDraw{}))
			}
			var migrated model.LotteryActivity
			require.NoError(t, db.First(&migrated, 1).Error)
			assert.Equal(t, model.LotteryTopUpLifetime, migrated.TopUpScope)
			assert.Zero(t, migrated.TopUpStart)
			assert.Zero(t, migrated.DeletedAt)
			assert.Equal(t, 100, migrated.RemainingQuota)
			var original model.User
			require.NoError(t, db.First(&original, 1).Error)
			assert.Equal(t, 100, original.Quota)
			assert.Equal(t, 90, original.GiftQuota)
			paid, err := model.LotteryPaidTopUpQuota(db, 1)
			require.NoError(t, err)
			assert.Equal(t, 10000, paid)

			t.Run("充值资格排除赠金未付款和钱包转账", func(t *testing.T) {
				for _, row := range []model.TopUp{
					{UserId: 1, TradeNo: "pending", Amount: 9999, Status: common.TopUpStatusPending, PaymentProvider: model.PaymentProviderEpay},
					{UserId: 1, TradeNo: "balance", Amount: 9999, CreditQuota: 9999, Status: common.TopUpStatusSuccess, PaymentProvider: model.PaymentProviderBalance},
					{UserId: 1, TradeNo: "stripe", Amount: 9999, Money: 2, Status: common.TopUpStatusSuccess, PaymentProvider: model.PaymentProviderStripe},
					{UserId: 1, TradeNo: "creem", Amount: 300, Money: 999, Status: common.TopUpStatusSuccess, PaymentProvider: model.PaymentProviderCreem},
					{UserId: 1, TradeNo: "receipt", Amount: 99, Money: 99.01, CreditQuota: 400, Status: common.TopUpStatusSuccess, PaymentProvider: model.AlipayReceiptProvider},
					{UserId: 1, TradeNo: "unknown", Amount: 9999, Status: common.TopUpStatusSuccess, PaymentProvider: "gift"},
				} {
					require.NoError(t, db.Create(&row).Error)
				}
				paid, err := model.LotteryPaidTopUpQuota(db, 1)
				require.NoError(t, err)
				assert.Equal(t, 10900, paid)
				paid, err = model.LotteryPaidTopUpQuota(db, 2)
				require.NoError(t, err)
				assert.Zero(t, paid)
			})

			t.Run("资格次数暂停恢复和重放不重发", func(t *testing.T) {
				rules := model.LotteryRules{Name: "限次活动", Mode: model.LotteryFixed, TotalQuota: 90, FixedQuota: 30, MinTopUpQuota: 10000, PerUserLimit: 1}
				a, err := model.SaveLotteryActivity(0, 100, rules)
				require.NoError(t, err)
				_, err = model.DrawLottery(a.ID, 1, "draft-request-0001", "")
				require.Error(t, err)
				_, err = model.ChangeLotteryState(a.ID, "start")
				require.NoError(t, err)
				_, err = model.DrawLottery(a.ID, 2, "gift-request-0001", "")
				require.ErrorContains(t, err, "门槛")
				first, err := model.DrawLottery(a.ID, 1, "first-request-0001", "127.0.0.1")
				require.NoError(t, err)
				assert.Equal(t, 30, first.Quota)
				assert.Empty(t, first.IP)
				replay, err := model.DrawLottery(a.ID, 1, "first-request-0001", "")
				require.NoError(t, err)
				assert.Equal(t, first.ID, replay.ID)
				_, err = model.ChangeLotteryState(a.ID, "pause")
				require.NoError(t, err)
				_, err = model.DrawLottery(a.ID, 1, "paused-request-01", "")
				require.Error(t, err)
				_, err = model.ChangeLotteryState(a.ID, "start")
				require.NoError(t, err)
				_, err = model.DrawLottery(a.ID, 1, "another-request-1", "")
				require.ErrorContains(t, err, "次数")
				_, err = model.SaveLotteryActivity(a.ID, 100, rules)
				require.Error(t, err)
				var wallet model.User
				require.NoError(t, db.First(&wallet, 1).Error)
				assert.Equal(t, 130, wallet.Quota)
				assert.Equal(t, 120, wallet.GiftQuota)
				rows, total, err := model.ListLotteryDraws(a.ID, 2, 0, 20, false)
				require.NoError(t, err)
				assert.Empty(t, rows)
				assert.Zero(t, total)
				rows, total, err = model.ListLotteryDraws(a.ID, 0, 0, 20, true)
				require.NoError(t, err)
				require.Len(t, rows, 1)
				assert.EqualValues(t, 1, total)
				assert.Equal(t, "paid-user", rows[0].Username)
				assert.Equal(t, "127.0.0.1", rows[0].IP)
				_, err = model.ChangeLotteryState(a.ID, "end")
				require.NoError(t, err)
				_, err = model.ChangeLotteryState(a.ID, "start")
				require.Error(t, err)
			})

			t.Run("四种发奖与尾额守恒", func(t *testing.T) {
				for _, tc := range []struct {
					name, mode                    string
					total, shares, fixed, percent int
					want                          []int
				}{
					{"固定", model.LotteryFixed, 100, 0, 50, 0, []int{50, 50}},
					{"固定尾差", model.LotteryFixed, 100, 0, 60, 0, []int{60, 40}},
					{"初始比例", model.LotteryInitialPercent, 100, 0, 0, 6000, []int{60, 40}},
					{"剩余比例", model.LotteryRemainingPercent, 100, 0, 0, 5000, []int{50, 25}},
					{"最小尾额", model.LotteryRemainingPercent, 1, 0, 0, 1, []int{1}},
					{"拼手气", model.LotteryRandomShares, 100, 3, 0, 0, nil},
				} {
					t.Run(tc.name, func(t *testing.T) {
						a, err := model.SaveLotteryActivity(0, 100, model.LotteryRules{Name: tc.name, Mode: tc.mode, TotalQuota: tc.total, PerUserLimit: 10, MinPercent: tc.percent, MaxPercent: tc.percent, Shares: tc.shares, FixedQuota: tc.fixed})
						require.NoError(t, err)
						_, err = model.ChangeLotteryState(a.ID, "start")
						require.NoError(t, err)
						n := len(tc.want)
						if tc.shares > 0 {
							n = tc.shares
						}
						sum := 0
						for i := range n {
							r, err := model.DrawLottery(a.ID, 1, fmt.Sprintf("mode-request-%06d", i), "")
							require.NoError(t, err)
							assert.Positive(t, r.Quota)
							sum += r.Quota
							if tc.want != nil {
								assert.Equal(t, tc.want[i], r.Quota)
							}
						}
						require.NoError(t, db.First(a, a.ID).Error)
						assert.Equal(t, tc.total, a.RemainingQuota+sum)
						if tc.shares > 0 {
							assert.Zero(t, a.RemainingShares)
							assert.Zero(t, a.RemainingQuota)
						}
						if a.RemainingQuota == 0 {
							assert.Equal(t, model.LotteryExhausted, a.Status)
							_, err = model.DrawLottery(a.ID, 1, "after-empty-00001", "")
							require.Error(t, err)
						}
					})
				}
			})

			t.Run("同一请求并发只发一次及最后一份只有一个赢家", func(t *testing.T) {
				for _, same := range []bool{true, false} {
					a, err := model.SaveLotteryActivity(0, 100, model.LotteryRules{Name: "并发活动", Mode: model.LotteryFixed, TotalQuota: 10, FixedQuota: 10, PerUserLimit: 1})
					require.NoError(t, err)
					_, err = model.ChangeLotteryState(a.ID, "start")
					require.NoError(t, err)
					start := make(chan struct{})
					results := make(chan error, 2)
					var wg sync.WaitGroup
					for i := range 2 {
						wg.Go(func() {
							<-start
							uid := 1
							key := "concurrent-same-01"
							if !same {
								uid = i + 1
								key = fmt.Sprintf("concurrent-%06d", i)
							}
							_, err := model.DrawLottery(a.ID, uid, key, "")
							results <- err
						})
					}
					close(start)
					wg.Wait()
					close(results)
					success := 0
					for err := range results {
						if err == nil {
							success++
						}
					}
					if same {
						assert.Equal(t, 2, success)
					} else {
						assert.Equal(t, 1, success)
					}
					var count int64
					require.NoError(t, db.Model(&model.LotteryDraw{}).Where("activity_id = ?", a.ID).Count(&count).Error)
					assert.EqualValues(t, 1, count)
					require.NoError(t, db.First(a, a.ID).Error)
					assert.Zero(t, a.RemainingQuota)
					assert.Equal(t, 1, a.DrawCount)
				}
			})

			t.Run("钱包超限回滚且禁用用户不能抽", func(t *testing.T) {
				a, err := model.SaveLotteryActivity(0, 100, model.LotteryRules{Name: "回滚活动", Mode: model.LotteryFixed, TotalQuota: 10, FixedQuota: 10, PerUserLimit: 1})
				require.NoError(t, err)
				_, err = model.ChangeLotteryState(a.ID, "start")
				require.NoError(t, err)
				for _, id := range []int{3, 4} {
					_, err = model.DrawLottery(a.ID, id, "rollback-test-001", "")
					require.Error(t, err)
				}
				require.NoError(t, db.First(a, a.ID).Error)
				assert.Equal(t, 10, a.RemainingQuota)
				assert.Zero(t, a.DrawCount)
				var count int64
				require.NoError(t, db.Model(&model.LotteryDraw{}).Where("activity_id = ?", a.ID).Count(&count).Error)
				assert.Zero(t, count)
			})
			t.Run("累计充值逐笔舍入兼容空渠道且不会溢出", func(t *testing.T) {
				for i, row := range []model.TopUp{
					{UserId: 90, Amount: 0, Money: 0.005, PaymentProvider: model.PaymentProviderStripe},
					{UserId: 90, Amount: 0, Money: 0.005, PaymentProvider: model.PaymentProviderStripe},
					{UserId: 90, Amount: 1, PaymentProvider: ""},
					{UserId: 90, Amount: -9, PaymentProvider: model.PaymentProviderEpay},
					{UserId: 91, CreditQuota: common.MaxWalletQuota, PaymentProvider: model.PaymentProviderEpay},
					{UserId: 91, CreditQuota: common.MaxWalletQuota, PaymentProvider: model.PaymentProviderEpay},
				} {
					row.Status, row.TradeNo = common.TopUpStatusSuccess, fmt.Sprintf("boundary-%d", i)
					require.NoError(t, db.Create(&row).Error)
					if i == 2 {
						require.NoError(t, db.Model(&row).UpdateColumn("payment_provider", nil).Error)
					}
				}
				paid, err := model.LotteryPaidTopUpQuota(db, 90)
				require.NoError(t, err)
				assert.Equal(t, 102, paid)
				paid, err = model.LotteryPaidTopUpQuota(db, 91)
				require.NoError(t, err)
				assert.Equal(t, common.MaxWalletQuota, paid)
			})
			t.Run("同一用户不同请求并发不能突破每场次数", func(t *testing.T) {
				a, err := model.SaveLotteryActivity(0, 100, model.LotteryRules{Name: "限次并发", Mode: model.LotteryFixed, TotalQuota: 30, FixedQuota: 10, PerUserLimit: 1})
				require.NoError(t, err)
				_, err = model.ChangeLotteryState(a.ID, "start")
				require.NoError(t, err)
				start := make(chan struct{})
				results := make(chan error, 2)
				var wg sync.WaitGroup
				for i := range 2 {
					wg.Go(func() {
						<-start
						_, err := model.DrawLottery(a.ID, 1, fmt.Sprintf("same-user-%08d", i), "")
						results <- err
					})
				}
				close(start)
				wg.Wait()
				close(results)
				success := 0
				for err := range results {
					if err == nil {
						success++
					} else {
						assert.ErrorContains(t, err, "次数")
					}
				}
				assert.Equal(t, 1, success)
				require.NoError(t, db.First(a, a.ID).Error)
				assert.Equal(t, 20, a.RemainingQuota)
				assert.Equal(t, 1, a.DrawCount)
				// 发奖后再次迁移仍保留流水及唯一约束。
				require.NoError(t, db.AutoMigrate(&model.LotteryActivity{}, &model.LotteryDraw{}))
				var original model.LotteryDraw
				require.NoError(t, db.Where("activity_id = ?", a.ID).First(&original).Error)
				_, err = model.DrawLottery(a.ID, 1, original.RequestID, "")
				require.NoError(t, err)
				original.ID = 0
				require.Error(t, db.Create(&original).Error)
			})
			t.Run("无效活动配置不能入库", func(t *testing.T) {
				for _, rules := range []model.LotteryRules{
					{Name: "", Mode: model.LotteryFixed, TotalQuota: 10, FixedQuota: 1, PerUserLimit: 1},
					{Name: "超限", Mode: model.LotteryFixed, TotalQuota: common.MaxWalletQuota + 1, FixedQuota: 1, PerUserLimit: 1},
					{Name: "负门槛", Mode: model.LotteryFixed, TotalQuota: 10, FixedQuota: 1, PerUserLimit: 1, MinTopUpQuota: -1},
					{Name: "过多份数", Mode: model.LotteryRandomShares, TotalQuota: 10, Shares: 11, PerUserLimit: 1},
					{Name: "错误比例", Mode: model.LotteryInitialPercent, TotalQuota: 10, MinPercent: 300, MaxPercent: 100, PerUserLimit: 1},
					{Name: "超额奖励", Mode: model.LotteryFixed, TotalQuota: 10, FixedQuota: 11, PerUserLimit: 1},
				} {
					_, err := model.SaveLotteryActivity(0, 100, rules)
					require.Error(t, err)
				}
			})
			t.Run("指定时段只计实际到账并包含首尾秒", func(t *testing.T) {
				require.NoError(t, db.Create(&model.User{Id: 10, Username: "period-user", AffCode: "period-user", Status: 1, Quota: 10000, GiftQuota: 10000}).Error)
				for i, row := range []model.TopUp{
					{CompleteTime: 99, CreditQuota: 10000},
					{CreateTime: 1, CompleteTime: 100, CreditQuota: 400},
					{CreateTime: 300, CompleteTime: 200, CreditQuota: 600},
					{CreateTime: 150, CompleteTime: 201, CreditQuota: 10000},
					{CreateTime: 150, CompleteTime: 0, CreditQuota: 10000},
				} {
					row.UserId, row.TradeNo, row.Status, row.PaymentProvider = 10, fmt.Sprintf("period-%d", i), common.TopUpStatusSuccess, model.AlipayReceiptProvider
					require.NoError(t, db.Create(&row).Error)
				}
				rules := model.LotteryRules{Name: "时段活动", Mode: model.LotteryFixed, TotalQuota: 100, FixedQuota: 10, PerUserLimit: 2, MinTopUpQuota: 1000, TopUpScope: model.LotteryTopUpPeriod, TopUpStart: 100, TopUpEnd: 200}
				for _, bounds := range [][2]int64{{0, 200}, {200, 100}, {100, 100}, {100, 253402272000}} {
					invalid := rules
					invalid.TopUpStart, invalid.TopUpEnd = bounds[0], bounds[1]
					_, err := model.SaveLotteryActivity(0, 100, invalid)
					require.Error(t, err)
				}
				a, err := model.SaveLotteryActivity(0, 100, rules)
				require.NoError(t, err)
				_, err = model.ChangeLotteryState(a.ID, "start")
				require.NoError(t, err)
				items, _, err := model.ListLotteryActivities(10, 0, 100, false)
				require.NoError(t, err)
				found := false
				for _, item := range items {
					if item.ID == a.ID {
						found = true
						assert.Equal(t, 1000, item.EligibleTopUpQuota)
					}
				}
				require.True(t, found)
				_, err = model.DrawLottery(a.ID, 10, "period-eligible-01", "")
				require.NoError(t, err)
				rules.MinTopUpQuota = 1001
				b, err := model.SaveLotteryActivity(0, 100, rules)
				require.NoError(t, err)
				_, err = model.ChangeLotteryState(b.ID, "start")
				require.NoError(t, err)
				_, err = model.DrawLottery(b.ID, 10, "period-ineligible", "")
				require.ErrorContains(t, err, "门槛")
				rules.TopUpScope = model.LotteryTopUpLifetime
				c, err := model.SaveLotteryActivity(0, 100, rules)
				require.NoError(t, err)
				assert.Zero(t, c.TopUpStart)
				assert.Zero(t, c.TopUpEnd)
				_, err = model.ChangeLotteryState(c.ID, "start")
				require.NoError(t, err)
				_, err = model.DrawLottery(c.ID, 10, "lifetime-eligible", "")
				require.NoError(t, err)
			})
			t.Run("删除活动保留钱包流水及幂等且隐藏中奖栏", func(t *testing.T) {
				a, err := model.SaveLotteryActivity(0, 100, model.LotteryRules{Name: "删除测试", Mode: model.LotteryFixed, TotalQuota: 30, FixedQuota: 10, PerUserLimit: 3})
				require.NoError(t, err)
				_, err = model.ChangeLotteryState(a.ID, "start")
				require.NoError(t, err)
				require.Error(t, model.DeleteLotteryActivity(a.ID))
				first, err := model.DrawLottery(a.ID, 1, "delete-replay-001", "127.0.0.1")
				require.NoError(t, err)
				winners, err := model.ListLotteryWinners(a.ID)
				require.NoError(t, err)
				require.Len(t, winners, 1)
				assert.Equal(t, "p***r", winners[0].Username)
				payload, err := common.Marshal(winners)
				require.NoError(t, err)
				assert.JSONEq(t, fmt.Sprintf(`[{"id":%d,"username":"p***r","quota":10}]`, first.ID), string(payload))
				_, err = model.ChangeLotteryState(a.ID, "end")
				require.NoError(t, err)
				var before model.User
				require.NoError(t, db.First(&before, 1).Error)
				for range 2 {
					require.NoError(t, model.DeleteLotteryActivity(a.ID))
				}
				_, err = model.DrawLottery(a.ID, 1, "deleted-new-draw1", "")
				require.Error(t, err)
				replay, err := model.DrawLottery(a.ID, 1, "delete-replay-001", "")
				require.NoError(t, err)
				assert.Equal(t, first.ID, replay.ID)
				var after model.User
				require.NoError(t, db.First(&after, 1).Error)
				assert.Equal(t, before.Quota, after.Quota)
				assert.Equal(t, before.GiftQuota, after.GiftQuota)
				winners, err = model.ListLotteryWinners(a.ID)
				require.NoError(t, err)
				assert.Empty(t, winners)
				_, _, err = model.ListLotteryDraws(a.ID, 1, 0, 20, false)
				require.Error(t, err)
				_, total, err := model.ListLotteryDraws(a.ID, 0, 0, 20, true)
				require.NoError(t, err)
				assert.EqualValues(t, 1, total)
				for _, root := range []bool{false, true} {
					items, _, err := model.ListLotteryActivities(1, 0, 100, root)
					require.NoError(t, err)
					for _, item := range items {
						assert.NotEqual(t, a.ID, item.ID)
					}
				}
				_, err = model.ChangeLotteryState(a.ID, "start")
				require.Error(t, err)
			})
			t.Run("结束删除和抽奖并发仍保持钱包奖池守恒", func(t *testing.T) {
				a, err := model.SaveLotteryActivity(0, 100, model.LotteryRules{Name: "并发删除", Mode: model.LotteryFixed, TotalQuota: 30, FixedQuota: 10, PerUserLimit: 3})
				require.NoError(t, err)
				_, err = model.ChangeLotteryState(a.ID, "start")
				require.NoError(t, err)
				var before model.User
				require.NoError(t, db.First(&before, 1).Error)
				start := make(chan struct{})
				drawn := make(chan error, 1)
				deleted := make(chan error, 1)
				go func() { <-start; _, err := model.DrawLottery(a.ID, 1, "concurrent-delete", ""); drawn <- err }()
				go func() {
					<-start
					_, err := model.ChangeLotteryState(a.ID, "end")
					if err == nil {
						err = model.DeleteLotteryActivity(a.ID)
					}
					deleted <- err
				}()
				close(start)
				drawErr := <-drawn
				require.NoError(t, <-deleted)
				var after model.User
				require.NoError(t, db.First(&after, 1).Error)
				require.NoError(t, db.First(a, a.ID).Error)
				count := 0
				if drawErr == nil {
					count = 1
				} else {
					assert.ErrorContains(t, drawErr, "活动未开放")
				}
				assert.Equal(t, count*10, after.GiftQuota-before.GiftQuota)
				assert.Equal(t, count*10, after.Quota-before.Quota)
				assert.Equal(t, 30-count*10, a.RemainingQuota)
				assert.Equal(t, count, a.DrawCount)
				assert.Positive(t, a.DeletedAt)
			})
			t.Run("清理只隐藏终态历史记录并保留财务和防重", func(t *testing.T) {
				cases := []struct {
					name    string
					status  string
					created int64
					deleted bool
					clear   bool
				}{
					{"已结束", model.LotteryEnded, 100, false, true},
					{"已领完", model.LotteryExhausted, 100, false, true},
					{"已删除活动", model.LotteryEnded, 100, true, true},
					{"进行中", model.LotteryActive, 100, false, false},
					{"暂停中", model.LotteryPaused, 100, false, false},
					{"截止边界", model.LotteryEnded, 200, false, false},
					{"新记录", model.LotteryEnded, 201, false, false},
				}
				activities := make([]*model.LotteryActivity, 0, len(cases))
				draws := make([]*model.LotteryDraw, 0, len(cases))
				for i, tc := range cases {
					total := 30
					if tc.status == model.LotteryExhausted {
						total = 10
					}
					a, err := model.SaveLotteryActivity(0, 100, model.LotteryRules{Name: tc.name, Mode: model.LotteryFixed, TotalQuota: total, FixedQuota: 10, PerUserLimit: 1})
					require.NoError(t, err)
					_, err = model.ChangeLotteryState(a.ID, "start")
					require.NoError(t, err)
					draw, err := model.DrawLottery(a.ID, 1, fmt.Sprintf("cleanup-matrix-%02d", i), "192.0.2.1")
					require.NoError(t, err)
					require.NoError(t, db.Model(draw).Update("created_at", tc.created).Error)
					if tc.status == model.LotteryEnded {
						_, err = model.ChangeLotteryState(a.ID, "end")
						require.NoError(t, err)
					} else if tc.status == model.LotteryPaused {
						_, err = model.ChangeLotteryState(a.ID, "pause")
						require.NoError(t, err)
					}
					if tc.deleted {
						require.NoError(t, model.DeleteLotteryActivity(a.ID))
					}
					require.NoError(t, db.First(a, a.ID).Error)
					activities, draws = append(activities, a), append(draws, draw)
				}
				var before model.User
				require.NoError(t, db.First(&before, 1).Error)
				var count int64
				require.NoError(t, model.OldLotteryLogQuery(context.Background(), 200).Count(&count).Error)
				assert.EqualValues(t, 3, count)
				for _, expected := range []int64{2, 1, 0} {
					n, err := model.DeleteOldLotteryLogs(context.Background(), 200, 2)
					require.NoError(t, err)
					assert.Equal(t, expected, n)
				}
				for i, tc := range cases {
					a, draw := activities[i], draws[i]
					var stored model.LotteryDraw
					require.NoError(t, db.First(&stored, draw.ID).Error)
					assert.Equal(t, draw.RequestID, stored.RequestID)
					assert.Equal(t, draw.Quota, stored.Quota)
					assert.Equal(t, tc.clear, stored.LogClearedAt > 0, tc.name)
					if tc.clear {
						assert.Empty(t, stored.IP)
					} else {
						assert.Equal(t, "192.0.2.1", stored.IP)
					}
					for _, root := range []bool{false, true} {
						rows, total, err := model.ListLotteryDraws(a.ID, 1, 0, 20, root)
						if tc.deleted && !root {
							require.Error(t, err)
							continue
						}
						require.NoError(t, err)
						if tc.clear {
							assert.Empty(t, rows)
							assert.Zero(t, total)
						} else {
							assert.Len(t, rows, 1)
							assert.EqualValues(t, 1, total)
						}
					}
					winners, err := model.ListLotteryWinners(a.ID)
					require.NoError(t, err)
					if tc.clear {
						assert.Empty(t, winners)
					} else {
						assert.Len(t, winners, 1)
					}
					replayed, err := model.DrawLottery(a.ID, 1, draw.RequestID, "")
					require.NoError(t, err)
					assert.Equal(t, draw.ID, replayed.ID)
					if tc.status == model.LotteryPaused {
						_, err = model.ChangeLotteryState(a.ID, "start")
						require.NoError(t, err)
					}
					_, err = model.DrawLottery(a.ID, 1, "cleanup-new-request", "")
					require.Error(t, err)
					if tc.status == model.LotteryActive || tc.status == model.LotteryPaused {
						assert.ErrorContains(t, err, "次数")
					}
					var afterActivity model.LotteryActivity
					require.NoError(t, db.First(&afterActivity, a.ID).Error)
					assert.Equal(t, a.RemainingQuota, afterActivity.RemainingQuota)
					assert.Equal(t, a.DrawCount, afterActivity.DrawCount)
				}
				var after model.User
				require.NoError(t, db.First(&after, 1).Error)
				assert.Equal(t, before.Quota, after.Quota)
				assert.Equal(t, before.GiftQuota, after.GiftQuota)
				// 重启重复迁移不得恢复已清理记录，也不得移除幂等约束。
				require.NoError(t, db.AutoMigrate(&model.LotteryDraw{}))
				rows, total, err := model.ListLotteryDraws(activities[0].ID, 1, 0, 20, true)
				require.NoError(t, err)
				assert.Empty(t, rows)
				assert.Zero(t, total)
				duplicate := *draws[0]
				duplicate.ID = 0
				require.Error(t, db.Create(&duplicate).Error)
			})
			t.Run("抽奖清理仅接受根用户明确选择的范围", func(t *testing.T) {
				gin.SetMode(gin.TestMode)
				require.NoError(t, db.AutoMigrate(&model.SystemTask{}, &model.SystemTaskLock{}))
				for _, role := range []int{1, 10, 100} {
					r := gin.New()
					r.Use(func(c *gin.Context) { c.Set("role", role) })
					r.POST("/", CreateLogCleanupSystemTask)
					for _, query := range []string{"", "&include_lottery_logs=true", "&include_lottery_logs=invalid"} {
						w := httptest.NewRecorder()
						r.ServeHTTP(w, httptest.NewRequest("POST", "/?target_timestamp=200"+query, nil))
						if role != 100 {
							assert.Equal(t, 403, w.Code)
							continue
						}
						if strings.Contains(query, "invalid") {
							assert.Equal(t, 400, w.Code)
							continue
						}
						require.Equal(t, 200, w.Code)
						task, err := model.GetActiveSystemTask(model.SystemTaskTypeLogCleanup)
						require.NoError(t, err)
						require.NotNil(t, task)
						var payload struct {
							IncludeLotteryLogs bool `json:"include_lottery_logs"`
						}
						require.NoError(t, task.DecodePayload(&payload))
						assert.Equal(t, query != "", payload.IncludeLotteryLogs)
						_, won, err := model.ClaimSystemTask(task.ID, task.Type, "cleanup-scope", common.GetTimestamp()+60)
						require.NoError(t, err)
						require.True(t, won)
						require.NoError(t, model.FinishSystemTask(task.TaskID, "cleanup-scope", model.SystemTaskStatusSucceeded, nil, ""))
					}
				}
			})
			t.Run("根权限及身份不可从请求伪造", func(t *testing.T) {
				gin.SetMode(gin.TestMode)
				for _, role := range []int{1, 10} {
					r := gin.New()
					r.Use(func(c *gin.Context) { c.Set("role", role); c.Set("id", 1) })
					r.POST("/", SaveLotteryActivity)
					r.DELETE("/:id", DeleteLotteryActivity)
					w := httptest.NewRecorder()
					r.ServeHTTP(w, httptest.NewRequest("POST", "/", strings.NewReader(`{"role":100}`)))
					assert.Equal(t, 403, w.Code)
					w = httptest.NewRecorder()
					r.ServeHTTP(w, httptest.NewRequest("DELETE", "/1", nil))
					assert.Equal(t, 403, w.Code)
				}
				a, err := model.SaveLotteryActivity(0, 100, model.LotteryRules{Name: "接口活动", Mode: model.LotteryFixed, TotalQuota: 10, FixedQuota: 10, PerUserLimit: 1})
				require.NoError(t, err)
				_, err = model.ChangeLotteryState(a.ID, "start")
				require.NoError(t, err)
				r := gin.New()
				r.Use(func(c *gin.Context) { c.Set("id", 2) })
				r.POST("/:id", DrawLottery)
				w := httptest.NewRecorder()
				req := httptest.NewRequest("POST", fmt.Sprintf("/%d", a.ID), strings.NewReader(`{"request_id":"api-identity-0001","user_id":1,"quota":9999}`))
				req.Header.Set("Content-Type", "application/json")
				r.ServeHTTP(w, req)
				assert.Equal(t, 200, w.Code)
				var response struct {
					Success bool
					Data    model.LotteryDraw
				}
				require.NoError(t, common.Unmarshal(w.Body.Bytes(), &response))
				require.True(t, response.Success)
				assert.Equal(t, 2, response.Data.UserID)
				assert.Equal(t, 10, response.Data.Quota)
			})
		})
	}
}

// 固定上一版本结构用于升级验证，避免新字段反向污染迁移基线。
type lotteryActivityBeforePeriod struct {
	ID              int    `json:"id"`
	Name            string `json:"name" gorm:"size:120"`
	Status          string `json:"status" gorm:"size:20;index"`
	Mode            string `json:"mode" gorm:"size:24"`
	TotalQuota      int    `json:"total_quota" gorm:"type:bigint"`
	RemainingQuota  int    `json:"remaining_quota" gorm:"type:bigint"`
	MinTopUpQuota   int    `json:"min_topup_quota" gorm:"type:bigint"`
	PerUserLimit    int    `json:"per_user_limit"`
	MinPercent      int    `json:"min_percent"`
	MaxPercent      int    `json:"max_percent"`
	Shares          int    `json:"shares"`
	RemainingShares int    `json:"remaining_shares"`
	FixedQuota      int    `json:"fixed_quota" gorm:"type:bigint"`
	DrawCount       int    `json:"draw_count" gorm:"type:bigint"`
	CreatedBy       int    `json:"created_by"`
	CreatedAt       int64  `json:"created_at"`
	StartedAt       int64  `json:"started_at"`
	EndedAt         int64  `json:"ended_at"`
}

func (lotteryActivityBeforePeriod) TableName() string { return "lottery_activities" }

// 固定清理功能上线前的流水结构，验证升级默认保留所有历史展示记录。
type lotteryDrawBeforeCleanup struct {
	ID         int
	ActivityID int    `gorm:"index;uniqueIndex:idx_lottery_request,priority:1;index:idx_lottery_user,priority:1"`
	UserID     int    `gorm:"uniqueIndex:idx_lottery_request,priority:2;index:idx_lottery_user,priority:2"`
	RequestID  string `gorm:"size:64;uniqueIndex:idx_lottery_request,priority:3"`
	Quota      int    `gorm:"type:bigint"`
	CreatedAt  int64
	IP         string `gorm:"size:64"`
}

func (lotteryDrawBeforeCleanup) TableName() string { return "lottery_draws" }
