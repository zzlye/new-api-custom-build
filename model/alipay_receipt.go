package model

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/pkg/alipayaccountlog"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const AlipayReceiptProvider = "alipay_receipt"
const AlipayReceiptOption = "AlipayReceiptConfig"

var receiptChinaTime = time.FixedZone("Asia/Shanghai", 8*60*60)
var receiptDigits = regexp.MustCompile(`^[0-9]{8,32}$`)

// 配置独立保存，通用选项接口不允许读取或覆盖其中的私钥。
type AlipayReceiptConfig struct {
	Enabled    bool   `json:"enabled"`
	AccountID  string `json:"account_id"`
	AppID      string `json:"app_id"`
	QRCode     string `json:"qr_code"`
	PrivateKey string `json:"private_key"`
	PublicKey  string `json:"public_key"`
	DailyLimit int    `json:"daily_limit"`
}

func loadAlipayReceiptConfig(tx *gorm.DB) (AlipayReceiptConfig, error) {
	c := AlipayReceiptConfig{DailyLimit: 10}
	var option Option
	err := tx.Where(&Option{Key: AlipayReceiptOption}).Take(&option).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	err = common.UnmarshalJsonStr(option.Value, &c)
	return c, err
}

func GetAlipayReceiptConfig() (AlipayReceiptConfig, error) { return loadAlipayReceiptConfig(DB) }

func (c AlipayReceiptConfig) Client() (*alipayaccountlog.Client, error) {
	return alipayaccountlog.New(map[string]string{"appId": c.AppID, "privateKey": c.PrivateKey, "publicKey": c.PublicKey})
}

// 单行写锁同时串行化金额分配、个人日限额和配置变更，SQLite 也会先取得写锁。
type ReceiptGate struct {
	ID         int `gorm:"primaryKey"`
	Revision   int64
	LeaseUntil int64
	LeaseToken string `gorm:"size:64"`
	LastScanAt int64
	LastError  string `gorm:"type:text"`
}

func lockReceiptGate(tx *gorm.DB) error {
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&ReceiptGate{ID: 1}).Error; err != nil {
		return err
	}
	return tx.Model(&ReceiptGate{}).Where("id = ?", 1).UpdateColumn("revision", gorm.Expr("revision + 1")).Error
}

func SaveAlipayReceiptConfig(c AlipayReceiptConfig) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := lockReceiptGate(tx); err != nil {
			return err
		}
		old, err := loadAlipayReceiptConfig(tx)
		if err != nil {
			return err
		}
		c.AppID, c.AccountID, c.QRCode = strings.TrimSpace(c.AppID), strings.TrimSpace(c.AccountID), strings.TrimSpace(c.QRCode)
		if strings.TrimSpace(c.PrivateKey) == "" {
			c.PrivateKey = old.PrivateKey
		}
		if c.DailyLimit < 1 || c.DailyLimit > 1000 {
			return errors.New("每人每日订单上限须为 1 至 1000")
		}
		if c.Enabled {
			u, e := url.Parse(c.QRCode)
			if e != nil || u.Scheme != "https" || u.Hostname() != "qr.alipay.com" || u.User != nil || u.Port() != "" || len(c.QRCode) > 2048 {
				return errors.New("请填写支付宝收款码中的 https://qr.alipay.com 地址")
			}
			if !receiptDigits.MatchString(c.AccountID) || !receiptDigits.MatchString(c.AppID) {
				return errors.New("支付宝账号 UID 或 APPID 无效")
			}
			if _, err := c.Client(); err != nil {
				return err
			}
		}
		// 停止新下单后仍扫描已有订单；存在未结束订单时禁止换账户、码或密钥。
		if old.AccountID != c.AccountID || old.AppID != c.AppID || old.QRCode != c.QRCode || old.PrivateKey != c.PrivateKey || old.PublicKey != c.PublicKey {
			var count int64
			if err := tx.Model(&TopUp{}).Where("payment_provider = ? AND status = ?", AlipayReceiptProvider, common.TopUpStatusPending).Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				return errors.New("请先停止新建订单，待已有订单结束后再修改收款账户")
			}
		}
		data, err := common.Marshal(c)
		if err != nil {
			return err
		}
		return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "key"}}, DoUpdates: clause.AssignmentColumns([]string{"value"})}).Create(&Option{Key: AlipayReceiptOption, Value: string(data)}).Error
	})
}

type ReceiptOrder struct {
	TopUpID       int    `json:"top_up_id" gorm:"primaryKey;autoIncrement:false"`
	AccountID     string `json:"-" gorm:"size:32;index:idx_receipt_amount,priority:1"`
	AppID         string `json:"-" gorm:"size:32"`
	QRCode        string `json:"qr_code" gorm:"type:text"`
	AmountCents   int64  `json:"amount_cents" gorm:"index:idx_receipt_amount,priority:2"`
	CreditQuota   int    `json:"credit_quota"`
	ConfirmedAt   int64  `json:"confirmed_at"`
	ExpiresAt     int64  `json:"expires_at" gorm:"index"`
	ReservedUntil int64  `json:"-" gorm:"index:idx_receipt_amount,priority:3"`
}

// 流水摘要永久防重，清理展示日志不删除本表；未匹配收入也不能被后来的订单认领。
type ReceiptPayment struct {
	ID              string `gorm:"primaryKey;size:64"`
	TradeKey        string `gorm:"uniqueIndex;size:64"`
	TopUpID         int    `gorm:"index"`
	AmountCents     int64
	PaidAt          int64
	ProviderTradeNo string `gorm:"size:128"`
}

type OrderEvent struct {
	ID        int    `json:"id"`
	TopUpID   int    `json:"-" gorm:"index"`
	CreatedAt int64  `json:"created_at"`
	ActorID   int    `json:"actor_id"`
	Message   string `json:"message" gorm:"type:text"`
}

type ReceiptOrderView struct {
	TopUp
	Receipt ReceiptOrder `json:"receipt"`
}

func recordOrderEvent(tx *gorm.DB, id, actor int, message string) error {
	return tx.Create(&OrderEvent{TopUpID: id, ActorID: actor, CreatedAt: time.Now().Unix(), Message: message}).Error
}

func CreateReceiptOrder(userID int, amount int64, baseCents int64, quota int, ip string, now time.Time) (*ReceiptOrderView, error) {
	if userID <= 0 || amount <= 0 || baseCents < 1 || baseCents > 10000000 || quota <= 0 || int64(quota) > common.MaxWalletQuota {
		return nil, errors.New("充值金额无效或过大")
	}
	var view ReceiptOrderView
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := lockReceiptGate(tx); err != nil {
			return err
		}
		cfg, err := loadAlipayReceiptConfig(tx)
		if err != nil {
			return err
		}
		if !cfg.Enabled {
			return errors.New("支付宝收款码暂未开放")
		}
		var user User
		if err := tx.Where("id = ? AND status = ?", userID, common.UserStatusEnabled).First(&user).Error; err != nil {
			return errors.New("用户不可用")
		}
		// 同一用户重复点击返回原有效订单，不占用额外次数或金额槽位。
		var active TopUp
		err = tx.Where("user_id = ? AND payment_provider = ? AND status = ?", userID, AlipayReceiptProvider, common.TopUpStatusPending).Order("id desc").Take(&active).Error
		if err == nil {
			var existing ReceiptOrder
			if err := tx.First(&existing, "top_up_id = ?", active.Id).Error; err != nil {
				return err
			}
			if existing.ExpiresAt > now.Unix() {
				view = ReceiptOrderView{TopUp: active, Receipt: existing}
				return nil
			}
			// 过期订单仍保留等待迟到的账务数据，但不阻挡新的个人日限额检查。
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		day := now.In(receiptChinaTime)
		start := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, receiptChinaTime).Unix()
		var used int64
		if err := tx.Model(&TopUp{}).Where("user_id = ? AND payment_provider = ? AND create_time >= ? AND create_time < ?", userID, AlipayReceiptProvider, start, start+86400).Count(&used).Error; err != nil {
			return err
		}
		if used >= int64(cfg.DailyLimit) {
			return errors.New("今日支付宝收款码订单次数已用完")
		}
		var reserved []int64
		if err := tx.Model(&ReceiptOrder{}).Where("account_id = ? AND amount_cents >= ? AND amount_cents <= ? AND reserved_until > ?", cfg.AccountID, baseCents, baseCents+99, now.Unix()).Pluck("amount_cents", &reserved).Error; err != nil {
			return err
		}
		occupied := make(map[int64]bool, len(reserved))
		for _, v := range reserved {
			occupied[v] = true
		}
		payCents := baseCents
		for occupied[payCents] && payCents <= baseCents+99 {
			payCents++
		}
		if payCents > baseCents+99 {
			return errors.New("当前相同金额订单较多，请稍后再试")
		}
		// 尾差仅用于识别付款，到账额度固定为原充值报价，不随实付尾差增加。
		credit := quota
		if user.Quota > common.MaxWalletQuota-credit {
			return ErrWalletQuotaLimitExceeded
		}
		view.TopUp = TopUp{CreditQuota: credit, UserId: userID, Amount: amount, Money: float64(payCents) / 100, TradeNo: "AR" + common.GetUUID(), PaymentMethod: AlipayReceiptProvider, PaymentProvider: AlipayReceiptProvider, CreateTime: now.Unix(), CreateIP: ip, Status: common.TopUpStatusPending}
		if err := tx.Create(&view.TopUp).Error; err != nil {
			return err
		}
		view.Receipt = ReceiptOrder{TopUpID: view.Id, AccountID: cfg.AccountID, AppID: cfg.AppID, QRCode: cfg.QRCode, AmountCents: payCents, CreditQuota: credit, ExpiresAt: now.Unix() + 180, ReservedUntil: now.Unix() + 180 + 3600}
		if err := tx.Create(&view.Receipt).Error; err != nil {
			return err
		}
		return recordOrderEvent(tx, view.Id, userID, "创建收款码订单")
	})
	return &view, err
}

func GetReceiptOrder(tradeNo string, userID int) (*ReceiptOrderView, error) {
	var view ReceiptOrderView
	q := DB.Where("trade_no = ? AND payment_provider = ?", tradeNo, AlipayReceiptProvider)
	if userID > 0 {
		q = q.Where("user_id = ?", userID)
	}
	if err := q.First(&view.TopUp).Error; err != nil {
		return nil, errors.New("订单不存在")
	}
	if err := DB.First(&view.Receipt, "top_up_id = ?", view.Id).Error; err != nil {
		return nil, err
	}
	// 只有确认金额后且仍在付款窗口内才返回收款码。
	if view.Receipt.ConfirmedAt == 0 || view.Receipt.ConfirmedAt > time.Now().Unix() || view.Receipt.ExpiresAt <= time.Now().Unix() || view.Status != common.TopUpStatusPending || view.PaidTime > 0 {
		view.Receipt.QRCode = ""
	}
	return &view, nil
}

func ConfirmReceiptOrder(tradeNo string, userID int, now time.Time) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := lockReceiptGate(tx); err != nil {
			return err
		}
		var top TopUp
		if err := lockForUpdate(tx).Where("trade_no = ? AND user_id = ? AND payment_provider = ?", tradeNo, userID, AlipayReceiptProvider).First(&top).Error; err != nil {
			return errors.New("订单不存在")
		}
		var receipt ReceiptOrder
		if err := tx.First(&receipt, "top_up_id = ?", top.Id).Error; err != nil {
			return err
		}
		if top.Status != common.TopUpStatusPending || receipt.ExpiresAt <= now.Unix() {
			return errors.New("订单已结束")
		}
		if receipt.ConfirmedAt != 0 {
			return nil
		}
		// 账务只有秒精度，从下一秒开始匹配；页面也跨过该边界后才展示收款码。
		if err := tx.Model(&receipt).Update("confirmed_at", now.Unix()+1).Error; err != nil {
			return err
		}
		return recordOrderEvent(tx, top.Id, userID, "确认付款金额")
	})
}

func ClosePaymentOrder(tradeNo string, userID, actorID int) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := lockReceiptGate(tx); err != nil {
			return err
		}
		var top TopUp
		q := lockForUpdate(tx).Where("trade_no = ?", tradeNo)
		if userID > 0 {
			q = q.Where("user_id = ? AND payment_provider = ?", userID, AlipayReceiptProvider)
		}
		if err := q.First(&top).Error; err != nil {
			return errors.New("订单不存在")
		}
		if top.Status == "closed" {
			return nil
		}
		if top.Status != common.TopUpStatusPending {
			return errors.New("只有待支付订单可以关闭")
		}
		var paid int64
		if err := tx.Model(&ReceiptPayment{}).Where("top_up_id = ?", top.Id).Count(&paid).Error; err != nil {
			return err
		}
		if paid > 0 {
			return errors.New("已确认收款，请先补回调完成入账")
		}
		if err := tx.Model(&top).Updates(map[string]any{"status": "closed", "closed_time": time.Now().Unix()}).Error; err != nil {
			return err
		}
		return recordOrderEvent(tx, top.Id, actorID, "关闭订单")
	})
}

// AcceptReceipt 仅接收已验签的收入记录，先保存不可重分配的匹配结果，再独立重试入账。
func AcceptReceipt(cfg AlipayReceiptConfig, row alipayaccountlog.Record) (int, error) {
	if row.Direction != "收入" || (row.Type != "" && row.Type != "交易" && row.Type != "转账" && row.Type != "收款") {
		return 0, nil
	}
	if row.AccountLogID == "" || row.AlipayOrderNo == "" || len(row.AccountLogID) > 128 || len(row.AlipayOrderNo) > 128 {
		return 0, errors.New("收款流水标识无效")
	}
	amount, err := decimal.NewFromString(row.Amount)
	if err != nil || !amount.IsPositive() || !amount.Equal(amount.Round(2)) || amount.GreaterThan(decimal.NewFromInt(100001)) {
		return 0, errors.New("账务金额无效")
	}
	key := fmt.Sprintf("%x", sha256.Sum256([]byte(cfg.AccountID+":"+row.AccountLogID)))
	tradeKey := fmt.Sprintf("%x", sha256.Sum256([]byte(cfg.AccountID+":"+row.AlipayOrderNo)))
	payment := ReceiptPayment{ID: key, TradeKey: tradeKey, AmountCents: amount.Mul(decimal.NewFromInt(100)).IntPart(), PaidAt: row.OccurredAt.Unix(), ProviderTradeNo: row.AlipayOrderNo}
	err = DB.Transaction(func(tx *gorm.DB) error {
		if err := lockReceiptGate(tx); err != nil {
			return err
		}
		var existing ReceiptPayment
		err := tx.Where("id = ? OR trade_key = ?", key, tradeKey).Take(&existing).Error
		if err == nil {
			payment = existing
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var candidates []ReceiptOrder
		err = tx.Table("receipt_orders").Select("receipt_orders.*").Joins("JOIN top_ups ON top_ups.id = receipt_orders.top_up_id").Where("receipt_orders.account_id = ? AND receipt_orders.app_id = ? AND receipt_orders.amount_cents = ? AND receipt_orders.confirmed_at > 0 AND receipt_orders.confirmed_at <= ? AND receipt_orders.expires_at >= ? AND top_ups.status = ?", cfg.AccountID, cfg.AppID, payment.AmountCents, payment.PaidAt, payment.PaidAt, common.TopUpStatusPending).Limit(2).Find(&candidates).Error
		if err != nil {
			return err
		}
		if len(candidates) == 1 {
			var linked int64
			if err := tx.Model(&ReceiptPayment{}).Where("top_up_id = ?", candidates[0].TopUpID).Count(&linked).Error; err != nil {
				return err
			}
			if linked == 0 {
				payment.TopUpID = candidates[0].TopUpID
			}
		}
		if err := tx.Create(&payment).Error; err != nil {
			return err
		}
		if payment.TopUpID == 0 {
			return nil
		}
		if err := tx.Model(&TopUp{}).Where("id = ?", payment.TopUpID).Updates(map[string]any{"paid_time": payment.PaidAt, "actual_money": float64(payment.AmountCents) / 100, "provider_trade_no": payment.ProviderTradeNo}).Error; err != nil {
			return err
		}
		return recordOrderEvent(tx, payment.TopUpID, 0, "已核验支付宝收入流水")
	})
	return payment.TopUpID, err
}

func SettleReceiptOrder(id int) error {
	var top TopUp
	var receipt ReceiptOrder
	var commission *TopUpInviteCommissionResult
	credited := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := lockReceiptGate(tx); err != nil {
			return err
		}
		if err := lockForUpdate(tx).First(&top, id).Error; err != nil {
			return err
		}
		if top.PaymentProvider != AlipayReceiptProvider {
			return ErrPaymentMethodMismatch
		}
		if top.Status == common.TopUpStatusSuccess {
			return nil
		}
		if top.Status != common.TopUpStatusPending {
			return ErrTopUpStatusInvalid
		}
		var payment ReceiptPayment
		if err := tx.Where("top_up_id = ?", id).Take(&payment).Error; err != nil {
			return errors.New("尚未查到该订单的有效收款流水")
		}
		if err := tx.First(&receipt, "top_up_id = ?", id).Error; err != nil {
			return err
		}
		if receipt.CreditQuota <= 0 || receipt.AmountCents != payment.AmountCents {
			return ErrInvalidTopUpQuota
		}
		if err := creditTopUpWithCommission(tx, &commission, top.UserId, receipt.CreditQuota, nil); err != nil {
			return err
		}
		if err := tx.Model(&top).Updates(map[string]any{"status": common.TopUpStatusSuccess, "complete_time": time.Now().Unix(), "last_error": ""}).Error; err != nil {
			return err
		}
		credited = true
		return recordOrderEvent(tx, id, 0, "充值已到账")
	})
	if err == nil && credited {
		syncCreditUserQuotaCache(top.UserId, receipt.CreditQuota, "alipay receipt")
		finishTopUpInviteCommission(commission)
		// 原充值额度与核验实付金额分别记录，尾差不增加余额。
		RecordTopupLog(top.UserId, fmt.Sprintf("使用在线充值成功，充值金额: %v，支付金额：%f", logger.LogQuota(receipt.CreditQuota), top.ActualMoney), top.CreateIP, AlipayReceiptProvider, AlipayReceiptProvider)
	}
	if err != nil {
		DB.Model(&TopUp{}).Where("id = ? AND status = ? AND paid_time > 0", id, common.TopUpStatusPending).Update("last_error", "已收款但入账失败，请补回调")
	}
	return err
}

func AcquireReceiptScan(now int64, token string) (bool, error) {
	var ok bool
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := lockReceiptGate(tx); err != nil {
			return err
		}
		result := tx.Model(&ReceiptGate{}).Where("id = ? AND lease_until <= ?", 1, now).Updates(map[string]any{"lease_token": token, "lease_until": now + 90})
		ok = result.RowsAffected == 1
		return result.Error
	})
	return ok, err
}

func FinishReceiptScan(token, message string, now int64) error {
	// 租约令牌防止过期的工作者覆盖新工作者状态，失败时统一退避。
	delay := int64(5)
	if message != "" {
		delay = 30
	}
	return DB.Model(&ReceiptGate{}).Where("id = ? AND lease_token = ?", 1, token).Updates(map[string]any{"lease_until": now + delay, "last_scan_at": now, "last_error": message}).Error
}

// 清理仅隐藏终态订单并抹除展示详情，财务订单及流水摘要继续用于限额和防重。
func OldOrderLogQuery(ctx context.Context, before int64) *gorm.DB {
	return DB.WithContext(ctx).Model(&TopUp{}).Where("create_time < ? AND status <> ? AND COALESCE(log_cleared_at, 0) = 0", before, common.TopUpStatusPending)
}

func DeleteOldOrderLogs(ctx context.Context, before int64, limit int) (int64, error) {
	var n int64
	err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockReceiptGate(tx); err != nil {
			return err
		}
		var ids []int
		if err := tx.Model(&TopUp{}).Where("create_time < ? AND status <> ? AND COALESCE(log_cleared_at, 0) = 0", before, common.TopUpStatusPending).Order("id").Limit(limit).Pluck("id", &ids).Error; err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		if err := tx.Where("top_up_id IN ?", ids).Delete(&OrderEvent{}).Error; err != nil {
			return err
		}
		if err := tx.Model(&ReceiptOrder{}).Where("top_up_id IN ?", ids).Update("qr_code", "").Error; err != nil {
			return err
		}
		result := tx.Model(&TopUp{}).Where("id IN ?", ids).Updates(map[string]any{"create_ip": "", "last_error": "", "provider_trade_no": "", "log_cleared_at": time.Now().Unix()})
		n = result.RowsAffected
		return result.Error
	})
	return n, err
}

func ExpireReceiptOrder(id int) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := lockReceiptGate(tx); err != nil {
			return err
		}
		var paid int64
		if err := tx.Model(&ReceiptPayment{}).Where("top_up_id = ?", id).Count(&paid).Error; err != nil {
			return err
		}
		if paid > 0 {
			return nil
		}
		result := tx.Model(&TopUp{}).Where("id = ? AND status = ?", id, common.TopUpStatusPending).Updates(map[string]any{"status": common.TopUpStatusExpired, "closed_time": time.Now().Unix()})
		if result.Error != nil || result.RowsAffected == 0 {
			return result.Error
		}
		return recordOrderEvent(tx, id, 0, "订单已过期")
	})
}

func RequestReceiptRecheck(id, actor int) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := lockReceiptGate(tx); err != nil {
			return err
		}
		var top TopUp
		if err := lockForUpdate(tx).Where("id = ? AND status = ? AND COALESCE(log_cleared_at, 0) = 0", id, common.TopUpStatusPending).First(&top).Error; err != nil {
			return errors.New("订单已结束")
		}
		return recordOrderEvent(tx, id, actor, "请求核对收款并补回调")
	})
}

type OrderLogRow struct {
	TopUp
	Username    string `json:"username"`
	CreditQuota int    `json:"credit_quota"`
}

func ListOrderLogs(keyword, status, provider string, offset int) ([]OrderLogRow, int64, error) {
	q := DB.Table("top_ups").Joins("LEFT JOIN users ON users.id = top_ups.user_id").Joins("LEFT JOIN receipt_orders ON receipt_orders.top_up_id = top_ups.id").Where("COALESCE(top_ups.log_cleared_at, 0) = 0")
	if status != "" {
		q = q.Where("top_ups.status = ?", status)
	}
	if provider != "" {
		q = q.Where("top_ups.payment_provider = ?", provider)
	}
	if keyword != "" {
		pattern, err := sanitizeLikePattern(keyword)
		if err != nil {
			return nil, 0, err
		}
		userID, _ := strconv.Atoi(keyword)
		q = q.Where("users.username LIKE ? ESCAPE '!' OR top_ups.trade_no LIKE ? ESCAPE '!' OR top_ups.provider_trade_no LIKE ? ESCAPE '!' OR top_ups.user_id = ?", pattern, pattern, pattern, userID)
	}
	var count int64
	if err := q.Count(&count).Error; err != nil {
		return nil, 0, err
	}
	rows := []OrderLogRow{}
	err := q.Select("top_ups.*, users.username, COALESCE(receipt_orders.credit_quota, top_ups.credit_quota) AS credit_quota").Order("top_ups.id DESC").Offset(offset).Limit(20).Scan(&rows).Error
	return rows, count, err
}

type OrderLogDetail struct {
	TopUp
	Receipt *ReceiptOrder `json:"receipt,omitempty"`
	Events  []OrderEvent  `json:"events"`
}

func GetOrderLogDetail(tradeNo string) (*OrderLogDetail, error) {
	d := &OrderLogDetail{Events: []OrderEvent{}}
	if err := DB.Where("trade_no = ? AND COALESCE(log_cleared_at, 0) = 0", tradeNo).First(&d.TopUp).Error; err != nil {
		return nil, err
	}
	if d.PaymentProvider == AlipayReceiptProvider {
		d.Receipt = &ReceiptOrder{}
		if err := DB.First(d.Receipt, "top_up_id = ?", d.Id).Error; err != nil {
			return nil, err
		}
		d.Receipt.QRCode = ""
	}
	err := DB.Where("top_up_id = ?", d.Id).Order("id").Find(&d.Events).Error
	// 旧通道未保存独立事件时，以现有订单事实展示创建和入账记录，不补造付款时间。
	if err == nil && len(d.Events) == 0 {
		d.Events = append(d.Events, OrderEvent{ID: -1, CreatedAt: d.CreateTime, ActorID: d.UserId, Message: "创建充值订单"})
		if d.Status == common.TopUpStatusSuccess && d.CompleteTime > 0 {
			d.Events = append(d.Events, OrderEvent{ID: -2, CreatedAt: d.CompleteTime, Message: "充值已到账"})
		}
	}
	return d, err
}
