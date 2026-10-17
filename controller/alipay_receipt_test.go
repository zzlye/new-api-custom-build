package controller

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/alipayaccountlog"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// 保留发布版充值表形状，验证真实旧数据升级而非只建全新的表。
type releasedReceiptTopUp struct {
	Id              int
	UserId          int `gorm:"index"`
	Amount          int64
	Money           float64
	TradeNo         string `gorm:"unique;type:varchar(255);index"`
	PaymentMethod   string `gorm:"type:varchar(50)"`
	PaymentProvider string `gorm:"type:varchar(50);default:''"`
	CreateTime      int64
	CompleteTime    int64
	Status          string
}

func (releasedReceiptTopUp) TableName() string { return "top_ups" }

func receiptTestConfig(t *testing.T) (model.AlipayReceiptConfig, *rsa.PrivateKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	pub, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	require.NoError(t, err)
	return model.AlipayReceiptConfig{Enabled: true, DailyLimit: 2, AccountID: "2088000000000001", AppID: "2026000000000001", QRCode: "https://qr.alipay.com/receipt-test", PrivateKey: base64.StdEncoding.EncodeToString(x509.MarshalPKCS1PrivateKey(key)), PublicKey: base64.StdEncoding.EncodeToString(pub)}, key
}

func TestAlipayReceiptDatabaseMatrix(t *testing.T) {
	cfg, key := receiptTestConfig(t)
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			dsn := os.Getenv("TEST_" + strings.ToUpper(dialect) + "_DSN")
			if dialect != "sqlite" && dsn == "" {
				t.Skip("未配置真实数据库")
			}
			for _, upgrade := range []bool{false, true} {
				t.Run(fmt.Sprintf("upgrade_%v", upgrade), func(t *testing.T) {
					db, _ := newAuditTestDatabase(t, dialect, dsn)
					oldDB, oldLog, oldRedis, oldBatch := model.DB, model.LOG_DB, common.RedisEnabled, common.BatchUpdateEnabled
					oldCommission := common.InviteTopUpCommissionRatio
					mainType, logType := common.MainDatabaseType(), common.LogDatabaseType()
					t.Cleanup(func() {
						model.DB, model.LOG_DB, common.RedisEnabled, common.BatchUpdateEnabled = oldDB, oldLog, oldRedis, oldBatch
						common.InviteTopUpCommissionRatio = oldCommission
						common.SetDatabaseTypes(mainType, logType)
					})
					model.DB, model.LOG_DB, common.RedisEnabled, common.BatchUpdateEnabled = db, db, false, false
					common.SetDatabaseTypes(common.DatabaseType(dialect), common.DatabaseType(dialect))
					common.InviteTopUpCommissionRatio = 0.1
					sqlDB, err := db.DB()
					require.NoError(t, err)
					sqlDB.SetMaxOpenConns(8)
					var version string
					versionSQL := "SELECT VERSION()"
					if dialect == "sqlite" {
						versionSQL = "SELECT sqlite_version()"
					}
					require.NoError(t, db.Raw(versionSQL).Scan(&version).Error)
					t.Log("数据库版本：", version)
					if upgrade {
						require.NoError(t, db.AutoMigrate(&releasedReceiptTopUp{}))
						require.NoError(t, db.Create(&releasedReceiptTopUp{UserId: 1, Amount: 10, Money: 50, TradeNo: "legacy", Status: common.TopUpStatusSuccess}).Error)
					}
					for range 2 {
						require.NoError(t, db.AutoMigrate(&model.User{}, &model.Option{}, &model.TopUp{}, &model.ReceiptGate{}, &model.ReceiptOrder{}, &model.ReceiptPayment{}, &model.OrderEvent{}, &model.Log{}))
					}
					if upgrade {
						var legacy model.TopUp
						require.NoError(t, db.Where("trade_no = ?", "legacy").First(&legacy).Error)
						assert.EqualValues(t, 10, legacy.Amount)
						assert.Equal(t, 50.0, legacy.Money)
						err := db.Transaction(func(tx *gorm.DB) error { return tx.Create(&model.TopUp{TradeNo: "legacy"}).Error })
						require.Error(t, err)
					}
					for _, user := range []model.User{{Id: 1, Username: "receipt-a", AffCode: "receipt-a", Status: common.UserStatusEnabled, InviterId: 3}, {Id: 2, Username: "receipt-b", AffCode: "receipt-b", Status: common.UserStatusEnabled}, {Id: 3, Username: "inviter", AffCode: "inviter", Status: common.UserStatusEnabled}} {
						require.NoError(t, db.Create(&user).Error)
					}
					require.NoError(t, model.SaveAlipayReceiptConfig(cfg))
					now := time.Now().Add(-20 * time.Second)
					first, err := model.CreateReceiptOrder(1, 10, 1000, 5000000, "127.0.0.1", now)
					require.NoError(t, err)
					second, err := model.CreateReceiptOrder(2, 10, 1000, 5000000, "127.0.0.2", now)
					require.NoError(t, err)
					assert.EqualValues(t, 1000, first.Receipt.AmountCents)
					assert.EqualValues(t, 1001, second.Receipt.AmountCents)
					assert.Equal(t, 5000000, second.Receipt.CreditQuota)
					replay, err := model.CreateReceiptOrder(1, 10, 1000, 5000000, "", now)
					require.NoError(t, err)
					assert.Equal(t, first.Id, replay.Id)
					_, err = model.GetReceiptOrder(first.TradeNo, 2)
					require.Error(t, err)
					require.Error(t, model.ConfirmReceiptOrder(first.TradeNo, 2, now))
					require.Error(t, model.ClosePaymentOrder(first.TradeNo, 2, 2))
					require.Error(t, model.ManualCompleteTopUp(first.TradeNo, ""))
					before, err := model.GetReceiptOrder(first.TradeNo, 1)
					require.NoError(t, err)
					assert.Empty(t, before.Receipt.QRCode)
					require.NoError(t, model.ConfirmReceiptOrder(first.TradeNo, 1, now.Add(time.Second)))
					confirmed, err := model.GetReceiptOrder(first.TradeNo, 1)
					require.NoError(t, err)
					assert.NotEmpty(t, confirmed.Receipt.QRCode)
					changed := cfg
					changed.QRCode = "https://qr.alipay.com/another"
					require.Error(t, model.SaveAlipayReceiptConfig(changed))
					payment := alipayaccountlog.Record{AccountLogID: "income-1", AlipayOrderNo: "alipay-1", Direction: "收入", Type: "收款", Amount: "10.00", OccurredAt: now.Add(3 * time.Second)}
					client, err := cfg.Client()
					require.NoError(t, err)
					body := []byte(fmt.Sprintf(`{"total_size":1,"detail_list":[{"account_log_id":"income-1","alipay_order_no":"alipay-1","direction":"收入","type":"收款","trans_amount":"10.00","trans_dt":%q}]}`, payment.OccurredAt.In(time.FixedZone("CST", 28800)).Format("2006-01-02 15:04:05")))
					page, err := client.VerifyPage(receiptSignedHeader(t, key, body, time.Now()), body)
					require.NoError(t, err)
					payment = page.Records[0]
					// 同秒之前的收款、支出、错金额不入账；未匹配流水不能事后认领。
					unmatched := payment
					unmatched.AccountLogID, unmatched.AlipayOrderNo = "old-income", "old-trade"
					unmatched.OccurredAt = now
					unmatchedID, err := model.AcceptReceipt(cfg, unmatched)
					require.NoError(t, err)
					assert.Zero(t, unmatchedID)
					unmatched.OccurredAt = payment.OccurredAt
					unmatchedID, err = model.AcceptReceipt(cfg, unmatched)
					require.NoError(t, err)
					assert.Zero(t, unmatchedID)
					outgoing := payment
					outgoing.Direction = "支出"
					outgoingID, err := model.AcceptReceipt(cfg, outgoing)
					require.NoError(t, err)
					assert.Zero(t, outgoingID)
					wrongAmount := payment
					wrongAmount.Amount = "10.02"
					wrongAmount.AccountLogID, wrongAmount.AlipayOrderNo = "wrong-amount", "wrong-trade"
					wrongID, err := model.AcceptReceipt(cfg, wrongAmount)
					require.NoError(t, err)
					assert.Zero(t, wrongID)
					id, err := model.AcceptReceipt(cfg, payment)
					require.NoError(t, err)
					assert.Equal(t, first.Id, id)
					received, err := model.GetReceiptOrder(first.TradeNo, 1)
					require.NoError(t, err)
					assert.Empty(t, received.Receipt.QRCode)
					require.Error(t, model.ClosePaymentOrder(first.TradeNo, 1, 1))
					// 已验证的原始流水可直接补回调，不再请求支付宝或要求人工改余额。
					callback := receiptControllerRequest(RetryOrderCallback, 1, common.RoleRootUser, "", first.TradeNo)
					assert.Contains(t, callback.Body.String(), `"success":true`)
					var wg sync.WaitGroup
					results := make(chan error, 4)
					for range 4 {
						wg.Go(func() { results <- model.SettleReceiptOrder(first.Id) })
					}
					wg.Wait()
					close(results)
					for err := range results {
						require.NoError(t, err)
					}
					var user, inviter model.User
					require.NoError(t, db.First(&user, 1).Error)
					require.NoError(t, db.First(&inviter, 3).Error)
					assert.Equal(t, 5000000, user.Quota)
					assert.Equal(t, 500000, inviter.GiftQuota)
					// 成功入账沿用充值详情格式，重复结算不能增加日志。
					var topupLogs []model.Log
					require.NoError(t, db.Where("user_id = ? AND type = ?", 1, model.LogTypeTopup).Find(&topupLogs).Error)
					require.Len(t, topupLogs, 1)
					assert.Equal(t, fmt.Sprintf("使用在线充值成功，充值金额: %v，支付金额：10.000000", logger.LogQuota(5000000)), topupLogs[0].Content)
					assert.Contains(t, topupLogs[0].Other, `"payment_method":"alipay_receipt"`)
					// 并发创建并关闭仅剩的一次名额，不能依赖前端按钮或进程内计数。
					ids := make(chan int, 6)
					errorsSeen := make(chan error, 6)
					for range 6 {
						wg.Go(func() {
							order, err := model.CreateReceiptOrder(1, 10, 1000, 5000000, "", now)
							if err == nil {
								ids <- order.Id
								err = model.ClosePaymentOrder(order.TradeNo, 1, 1)
							}
							errorsSeen <- err
						})
					}
					wg.Wait()
					close(ids)
					close(errorsSeen)
					unique := map[int]bool{}
					for id := range ids {
						unique[id] = true
					}
					assert.Len(t, unique, 1)
					for err := range errorsSeen {
						if err != nil {
							assert.Contains(t, err.Error(), "次数已用完")
						}
					}
					_, err = model.CreateReceiptOrder(1, 10, 1000, 5000000, "", now)
					require.ErrorContains(t, err, "次数已用完")
					// 其他用户、其他通道独立计数。
					require.NoError(t, model.ClosePaymentOrder(second.TradeNo, 2, 2))
					_, err = model.CreateReceiptOrder(2, 10, 1000, 5000000, "", now)
					require.NoError(t, err)
					require.NoError(t, (&model.TopUp{UserId: 1, TradeNo: "epay-independent", PaymentProvider: model.PaymentProviderEpay, Status: common.TopUpStatusPending}).Insert())
					logs, count, err := model.ListOrderLogs("receipt-a", "success", model.AlipayReceiptProvider, 0)
					require.NoError(t, err)
					assert.EqualValues(t, 1, count)
					require.Len(t, logs, 1)
					assert.Equal(t, first.TradeNo, logs[0].TradeNo)
					n, err := model.DeleteOldOrderLogs(context.Background(), time.Now().Unix()+1, 100)
					require.NoError(t, err)
					assert.GreaterOrEqual(t, n, int64(2))
					_, err = model.GetOrderLogDetail(first.TradeNo)
					require.Error(t, err)
					_, err = model.CreateReceiptOrder(1, 10, 1000, 5000000, "", now)
					require.ErrorContains(t, err, "次数已用完")
					id, err = model.AcceptReceipt(cfg, payment)
					require.NoError(t, err)
					require.NoError(t, model.SettleReceiptOrder(id))
					require.NoError(t, db.First(&user, 1).Error)
					assert.Equal(t, 5000000, user.Quota)
					var events int64
					require.NoError(t, db.Model(&model.OrderEvent{}).Where("top_up_id = ?", first.Id).Count(&events).Error)
					assert.Zero(t, events)
					// 北京时间跨零点后恢复个人额度，不使用浏览器时间。
					local := now.In(time.FixedZone("CST", 28800))
					nextDay := time.Date(local.Year(), local.Month(), local.Day()+1, 0, 0, 0, 0, local.Location())
					_, err = model.CreateReceiptOrder(1, 10, 1000, 5000000, "", nextDay)
					require.NoError(t, err)
					// 尾差仅用于付款匹配，到账及返佣按原充值额度，日志单独记录实付金额。
					tailOrder, err := model.CreateReceiptOrder(2, 10, 1000, 5000000, "127.0.0.2", nextDay)
					require.NoError(t, err)
					require.EqualValues(t, 1001, tailOrder.Receipt.AmountCents)
					require.NoError(t, model.ConfirmReceiptOrder(tailOrder.TradeNo, 2, nextDay.Add(time.Second)))
					tailPayment := alipayaccountlog.Record{AccountLogID: "tail-income", AlipayOrderNo: "tail-trade", Direction: "收入", Type: "收款", Amount: "10.01", OccurredAt: nextDay.Add(3 * time.Second)}
					tailID, err := model.AcceptReceipt(cfg, tailPayment)
					require.NoError(t, err)
					require.Equal(t, tailOrder.Id, tailID)
					require.NoError(t, model.SettleReceiptOrder(tailID))
					var tailLog model.Log
					require.NoError(t, db.Where("user_id = ? AND type = ?", 2, model.LogTypeTopup).First(&tailLog).Error)
					assert.Equal(t, fmt.Sprintf("使用在线充值成功，充值金额: %v，支付金额：10.010000", logger.LogQuota(5000000)), tailLog.Content)
					settings := receiptControllerRequest(GetAlipayReceiptSettings, 1, common.RoleRootUser, "", "")
					assert.NotContains(t, settings.Body.String(), cfg.PrivateKey)
					assert.Contains(t, settings.Body.String(), `"private_key_configured":true`)
					for _, role := range []int{common.RoleCommonUser, common.RoleAdminUser} {
						assert.Equal(t, http.StatusForbidden, receiptControllerRequest(SaveAlipayReceiptSettings, 1, role, `{}`, "").Code)
					}
				})
			}
		})
	}
}

func TestAlipayReceiptManualCreditDatabaseMatrix(t *testing.T) {
	cfg, _ := receiptTestConfig(t)
	cfg.DailyLimit = 20
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			dsn := os.Getenv("TEST_" + strings.ToUpper(dialect) + "_DSN")
			if dialect != "sqlite" && dsn == "" {
				t.Skip("未配置真实数据库")
			}
			db, _ := newAuditTestDatabase(t, dialect, dsn)
			oldDB, oldLog, oldRedis, oldBatch, oldCommission := model.DB, model.LOG_DB, common.RedisEnabled, common.BatchUpdateEnabled, common.InviteTopUpCommissionRatio
			mainType, logType := common.MainDatabaseType(), common.LogDatabaseType()
			t.Cleanup(func() {
				model.DB, model.LOG_DB, common.RedisEnabled, common.BatchUpdateEnabled, common.InviteTopUpCommissionRatio = oldDB, oldLog, oldRedis, oldBatch, oldCommission
				common.SetDatabaseTypes(mainType, logType)
			})
			model.DB, model.LOG_DB, common.RedisEnabled, common.BatchUpdateEnabled, common.InviteTopUpCommissionRatio = db, db, false, false, 0.1
			common.SetDatabaseTypes(common.DatabaseType(dialect), common.DatabaseType(dialect))
			require.NoError(t, db.AutoMigrate(&model.User{}, &model.Option{}, &model.TopUp{}, &model.ReceiptGate{}, &model.ReceiptOrder{}, &model.ReceiptPayment{}, &model.OrderEvent{}, &model.Log{}))
			for _, user := range []model.User{
				{Id: 1, Username: "buyer", AffCode: "buyer", InviterId: 3, Status: common.UserStatusEnabled},
				{Id: 2, Username: "other", AffCode: "other", Status: common.UserStatusEnabled},
				{Id: 3, Username: "inviter", AffCode: "inviter", Status: common.UserStatusEnabled},
				{Id: 99, Username: "root", AffCode: "root", Role: common.RoleRootUser, Status: common.UserStatusEnabled},
			} {
				require.NoError(t, db.Create(&user).Error)
			}
			require.NoError(t, model.SaveAlipayReceiptConfig(cfg))
			now := time.Now().Add(-20 * time.Second)
			other, err := model.CreateReceiptOrder(2, 10, 1000, 5000000, "192.0.2.2", now)
			require.NoError(t, err)
			order, err := model.CreateReceiptOrder(1, 10, 1000, 5000000, "192.0.2.1", now)
			require.NoError(t, err)
			assert.Equal(t, 10.01, order.Money)
			assert.Equal(t, 5000000, order.CreditQuota)
			require.NoError(t, model.ConfirmReceiptOrder(order.TradeNo, 1, now))
			wrongPayment := alipayaccountlog.Record{AccountLogID: "wrong-income", AlipayOrderNo: "202610170000000000000001", Amount: "9.80", Direction: "收入", Type: "收款", OccurredAt: now.Add(2 * time.Second)}
			id, err := model.AcceptReceipt(cfg, wrongPayment)
			require.NoError(t, err)
			assert.Zero(t, id)
			require.ErrorContains(t, model.SettleReceiptOrder(order.Id), "尚未查到")
			require.ErrorContains(t, model.ManualCreditReceiptOrder(order.TradeNo, 99, wrongPayment.AlipayOrderNo, "9.90", "错额付款", "192.0.2.99"), "不一致")
			require.ErrorContains(t, model.ManualCreditReceiptOrder(order.TradeNo, 1, wrongPayment.AlipayOrderNo, "9.80", "错额付款", ""), "仅根用户")
			for _, role := range []int{common.RoleAdminUser, common.RoleCommonUser} {
				assert.Equal(t, http.StatusForbidden, receiptControllerRequest(ManualCreditOrderLog, 99, role, `{}`, order.TradeNo).Code)
			}
			for _, money := range []string{"0", "-1", "1.001", "100001.01", "1e999999999", "NaN"} {
				require.Error(t, model.ManualCreditReceiptOrder(order.TradeNo, 99, wrongPayment.AlipayOrderNo, money, "错额付款", ""))
			}
			require.Error(t, model.ManualCreditReceiptOrder(order.TradeNo, 99, "", "9.80", "错额付款", ""))
			require.Error(t, model.ManualCreditReceiptOrder(order.TradeNo, 99, wrongPayment.AlipayOrderNo, "9.80", " ", ""))
			// 模拟重复提交，充值、邀请赠金和操作记录都只能写入一次。
			var wg sync.WaitGroup
			results := make(chan error, 4)
			for range 4 {
				wg.Go(func() {
					results <- model.ManualCreditReceiptOrder(order.TradeNo, 99, wrongPayment.AlipayOrderNo, "9.80", "错额付款人工核对", "192.0.2.99")
				})
			}
			wg.Wait()
			close(results)
			for err := range results {
				require.NoError(t, err)
			}
			var buyer, inviter model.User
			require.NoError(t, db.First(&buyer, 1).Error)
			require.NoError(t, db.First(&inviter, 3).Error)
			assert.Equal(t, 5000000, buyer.Quota)
			assert.Equal(t, 500000, inviter.GiftQuota)
			require.ErrorContains(t, model.ManualCreditReceiptOrder(order.TradeNo, 99, "202610170000000000000099", "9.80", "错误的重复提交", ""), "订单已到账")
			detail, err := model.GetOrderLogDetail(order.TradeNo)
			require.NoError(t, err)
			assert.Equal(t, common.TopUpStatusSuccess, detail.Status)
			assert.Equal(t, 10.01, detail.Money)
			assert.Equal(t, 9.8, detail.ActualMoney)
			assert.Equal(t, wrongPayment.OccurredAt.Unix(), detail.PaidTime)
			require.Len(t, detail.Events, 3)
			assert.Equal(t, 99, detail.Events[2].ActorID)
			assert.Contains(t, detail.Events[2].Message, "错额付款人工核对")
			assert.Contains(t, detail.Events[2].Message, "192.0.2.99")
			var count int64
			require.NoError(t, db.Model(&model.Log{}).Where("user_id = ? AND type = ?", 1, model.LogTypeTopup).Count(&count).Error)
			assert.EqualValues(t, 1, count)
			require.ErrorContains(t, model.ManualCreditReceiptOrder(other.TradeNo, 99, wrongPayment.AlipayOrderNo, "9.80", "重复流水", ""), "其他订单")
			// 关闭不释放金额槽位；超过原保留期才复用，且仍按原额度入账。
			require.NoError(t, model.ClosePaymentOrder(other.TradeNo, 2, 99))
			replacement, err := model.CreateReceiptOrder(2, 10, 1000, 5000000, "", now)
			require.NoError(t, err)
			assert.Equal(t, 10.02, replacement.Money)
			assert.Equal(t, 5000000, replacement.CreditQuota)
			// 未扫到的真实收款可人工登记；迟到扫描与自动结算不会重复入账。
			body := `{"provider_trade_no":"202610170000000000000002","actual_money":"9.50","reason":"核对支付宝后补单","credit_quota":999999999,"user_id":99}`
			response := receiptControllerRequest(ManualCreditOrderLog, 99, common.RoleRootUser, body, other.TradeNo)
			assert.Contains(t, response.Body.String(), `"success":true`)
			late := wrongPayment
			late.AccountLogID, late.AlipayOrderNo, late.Amount = "late-income", "202610170000000000000002", "9.50"
			id, err = model.AcceptReceipt(cfg, late)
			require.NoError(t, err)
			assert.Equal(t, other.Id, id)
			require.NoError(t, model.SettleReceiptOrder(other.Id))
			var otherUser model.User
			require.NoError(t, db.First(&otherUser, 2).Error)
			assert.Equal(t, 5000000, otherUser.Quota)
			// 超限时整个事务回滚，修正余额后可以用同一交易号补已过期订单。
			require.NoError(t, model.ExpireReceiptOrder(replacement.Id))
			require.NoError(t, db.Model(&model.User{}).Where("id = ?", 2).Update("quota", common.MaxWalletQuota).Error)
			err = model.ManualCreditReceiptOrder(replacement.TradeNo, 99, "202610170000000000000003", "10", "过期补单", "")
			require.ErrorIs(t, err, model.ErrTopUpQuotaLimitExceeded)
			require.NoError(t, db.Model(&model.ReceiptPayment{}).Where("provider_trade_no = ?", "202610170000000000000003").Count(&count).Error)
			assert.Zero(t, count)
			require.NoError(t, db.Model(&model.User{}).Where("id = ?", 2).Update("quota", 5000000).Error)
			require.NoError(t, model.ManualCreditReceiptOrder(replacement.TradeNo, 99, "202610170000000000000003", "10", "过期补单", ""))
			reused, err := model.CreateReceiptOrder(1, 10, 1000, 5000000, "", time.Unix(other.Receipt.ReservedUntil, 0))
			require.NoError(t, err)
			assert.Equal(t, 10.0, reused.Money)
			// 清理删除人工原因和详情，但不删除交易防重记录，也不允许补已清理订单。
			_, err = model.DeleteOldOrderLogs(context.Background(), time.Now().Unix()+1, 100)
			require.NoError(t, err)
			require.NoError(t, db.Model(&model.OrderEvent{}).Where("top_up_id = ?", order.Id).Count(&count).Error)
			assert.Zero(t, count)
			require.ErrorContains(t, model.ManualCreditReceiptOrder(order.TradeNo, 99, wrongPayment.AlipayOrderNo, "9.80", "再补", ""), "已清理")
			require.ErrorContains(t, model.ManualCreditReceiptOrder(reused.TradeNo, 99, wrongPayment.AlipayOrderNo, "9.80", "再补", ""), "其他订单")
			epay := model.TopUp{TradeNo: "epay-manual-rejected", UserId: 1, PaymentProvider: model.PaymentProviderEpay, Status: common.TopUpStatusPending}
			require.NoError(t, db.Create(&epay).Error)
			require.ErrorIs(t, model.ManualCreditReceiptOrder(epay.TradeNo, 99, "202610170000000000000004", "10", "通道错误", ""), model.ErrPaymentMethodMismatch)
		})
	}
}

func receiptSignedHeader(t *testing.T, key *rsa.PrivateKey, body []byte, now time.Time) http.Header {
	t.Helper()
	timestamp := strconv.FormatInt(now.UnixMilli(), 10)
	digest := sha256.Sum256(append([]byte(timestamp+"\nfixture-nonce\n"), append(append([]byte{}, body...), '\n')...))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	require.NoError(t, err)
	header := http.Header{}
	header.Set("Alipay-Timestamp", timestamp)
	header.Set("Alipay-Nonce", "fixture-nonce")
	header.Set("Alipay-Signature", base64.StdEncoding.EncodeToString(sig))
	return header
}

func TestAlipayReceiptSignatureValidation(t *testing.T) {
	cfg, key := receiptTestConfig(t)
	client, err := cfg.Client()
	require.NoError(t, err)
	body := []byte(`{"total_size":"0"}`)
	_, err = client.VerifyPage(receiptSignedHeader(t, key, body, time.Now()), body)
	require.NoError(t, err)
	_, err = client.VerifyPage(receiptSignedHeader(t, key, body, time.Now().Add(-6*time.Minute)), body)
	require.Error(t, err)
	_, err = client.VerifyPage(http.Header{}, body)
	require.Error(t, err)
	header := receiptSignedHeader(t, key, body, time.Now())
	_, err = client.VerifyPage(header, []byte(`{"total_size":1}`))
	require.Error(t, err)
	header.Add("Alipay-Nonce", "duplicate")
	_, err = client.VerifyPage(header, body)
	require.Error(t, err)
}

func TestAlipayReceiptRequiresAuthenticatedOwner(t *testing.T) {
	for _, handler := range []gin.HandlerFunc{GetAlipayReceiptOrder, ConfirmAlipayReceiptOrder, CloseAlipayReceiptOrder} {
		assert.Equal(t, http.StatusUnauthorized, receiptControllerRequest(handler, 0, 0, "", "AR-private-order").Code)
	}
}

func TestAlipayReceiptRateLimitIsPerUserAndCannotBeDisabled(t *testing.T) {
	oldRedis, oldEnabled := common.RedisEnabled, common.CriticalRateLimitEnable
	common.RedisEnabled, common.CriticalRateLimitEnable = false, false
	t.Cleanup(func() { common.RedisEnabled, common.CriticalRateLimitEnable = oldRedis, oldEnabled })
	router := gin.New()
	// 认证边界注入两个测试用户；切换来源地址不能重置同一人的写入计数。
	router.Use(func(c *gin.Context) { id, _ := strconv.Atoi(c.GetHeader("Test-User")); c.Set("id", id) })
	router.POST("/write", middleware.AlipayReceiptRateLimit(true), func(c *gin.Context) { c.Status(http.StatusOK) })
	router.GET("/read", middleware.AlipayReceiptRateLimit(false), func(c *gin.Context) { c.Status(http.StatusOK) })
	for i := range 13 {
		r := httptest.NewRequest(http.MethodPost, "/write", nil)
		r.Header.Set("Test-User", "9001")
		r.RemoteAddr = fmt.Sprintf("192.0.2.%d:1234", i+1)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if i < 12 {
			assert.Equal(t, http.StatusOK, w.Code)
		} else {
			assert.Equal(t, http.StatusTooManyRequests, w.Code)
		}
	}
	for _, input := range []struct{ path, method, id string }{{"/write", http.MethodPost, "9002"}, {"/read", http.MethodGet, "9001"}} {
		r := httptest.NewRequest(input.method, input.path, nil)
		r.Header.Set("Test-User", input.id)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		assert.Equal(t, http.StatusOK, w.Code)
	}
}

func receiptControllerRequest(handler gin.HandlerFunc, id, role int, body, tradeNo string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("id", id)
	c.Set("role", role)
	c.Params = gin.Params{{Key: "trade_no", Value: tradeNo}}
	handler(c)
	return w
}

// 展示配置不决定收款开关，也不能改写全局设置或重复显示同一入口。
func TestReceiptPaymentMethods(t *testing.T) {
	configured := []map[string]string{{"type": "alipay_receipt", "name": "西米露支付宝", "icon": "SiAlipay"}, {"type": "stripe", "name": "银行卡"}, {"type": "alipay_receipt", "name": "重复"}}
	got := receiptPaymentMethods(configured, true, 1)
	require.Len(t, got, 2)
	assert.Equal(t, "西米露支付宝", got[0]["name"])
	assert.Equal(t, "SiAlipay", got[0]["icon"])
	assert.Equal(t, "1", got[0]["min_topup"])
	assert.Equal(t, "stripe", got[1]["type"])
	assert.NotContains(t, configured[0], "min_topup")
	assert.Equal(t, []map[string]string{configured[1]}, receiptPaymentMethods(configured, false, 1))
	assert.Empty(t, receiptPaymentMethods(nil, true, 5))
	epayOnly := []map[string]string{{"type": "alipay", "name": "支付宝"}}
	assert.Equal(t, epayOnly, receiptPaymentMethods(epayOnly, true, 5))
	assert.Empty(t, receiptPaymentMethods(nil, false, 5))
}
