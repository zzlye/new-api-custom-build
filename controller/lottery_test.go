package controller

import (
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
			for range 2 {
				require.NoError(t, db.AutoMigrate(&model.User{}, &model.TopUp{}, &model.LotteryActivity{}, &model.LotteryDraw{}))
			}
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
			t.Run("根权限及身份不可从请求伪造", func(t *testing.T) {
				gin.SetMode(gin.TestMode)
				for _, role := range []int{1, 10} {
					r := gin.New()
					r.Use(func(c *gin.Context) { c.Set("role", role); c.Set("id", 1) })
					r.POST("/", SaveLotteryActivity)
					w := httptest.NewRecorder()
					r.ServeHTTP(w, httptest.NewRequest("POST", "/", strings.NewReader(`{"role":100}`)))
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
