package model

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

var receiptManualAmount = regexp.MustCompile(`^[0-9]{1,6}(\.[0-9]{1,2})?$`)
var receiptManualTrade = regexp.MustCompile(`^[0-9]{8,64}$`)

// 人工补单不冒充自动核验；交易防重、充值、返佣和操作记录必须在同一事务完成。
func ManualCreditReceiptOrder(tradeNo string, actorID int, providerTradeNo, actualMoney, reason, actorIP string) error {
	providerTradeNo, actualMoney, reason = strings.TrimSpace(providerTradeNo), strings.TrimSpace(actualMoney), strings.TrimSpace(reason)
	if !receiptManualTrade.MatchString(providerTradeNo) {
		return errors.New("请填写有效的支付宝交易号（8 至 64 位数字）")
	}
	if !receiptManualAmount.MatchString(actualMoney) {
		return errors.New("实收金额须为正数，最多两位小数")
	}
	amount, err := decimal.NewFromString(actualMoney)
	if err != nil || !amount.IsPositive() || amount.GreaterThan(decimal.NewFromInt(100001)) {
		return errors.New("实收金额须大于零且不超过 100001 元")
	}
	if reason == "" || utf8.RuneCountInString(reason) > 500 {
		return errors.New("请填写补单原因，最多 500 字")
	}
	var top TopUp
	var receipt ReceiptOrder
	var commission *TopUpInviteCommissionResult
	credited := false
	err = DB.Transaction(func(tx *gorm.DB) error {
		if err := lockReceiptGate(tx); err != nil {
			return err
		}
		var actor User
		if err := tx.Where("id = ? AND role = ? AND status = ?", actorID, common.RoleRootUser, common.UserStatusEnabled).First(&actor).Error; err != nil {
			return errors.New("仅根用户可以人工补单")
		}
		if err := lockForUpdate(tx).Where("trade_no = ? AND COALESCE(log_cleared_at, 0) = 0", tradeNo).First(&top).Error; err != nil {
			return errors.New("订单不存在或日志已清理")
		}
		if top.PaymentProvider != AlipayReceiptProvider {
			return ErrPaymentMethodMismatch
		}
		// 已到账请求直接幂等结束，不再产生流水、返佣或操作日志。
		if top.Status == common.TopUpStatusSuccess {
			if top.ProviderTradeNo != providerTradeNo || !decimal.NewFromFloat(top.ActualMoney).Equal(amount) {
				return errors.New("订单已到账，请核对已记录的交易号和实收金额")
			}
			return nil
		}
		if top.Status != common.TopUpStatusPending && top.Status != common.TopUpStatusExpired && top.Status != "closed" {
			return ErrTopUpStatusInvalid
		}
		if err := tx.First(&receipt, "top_up_id = ?", top.Id).Error; err != nil {
			return err
		}
		if receipt.AccountID == "" || receipt.CreditQuota <= 0 || receipt.CreditQuota != top.CreditQuota {
			return ErrInvalidTopUpQuota
		}
		// 与自动扫描使用相同交易键，迟到的账务数据也不能重复到账。
		tradeKey := fmt.Sprintf("%x", sha256.Sum256([]byte(receipt.AccountID+":"+providerTradeNo)))
		var linked int64
		if err := tx.Model(&ReceiptPayment{}).Where("top_up_id = ? AND trade_key <> ?", top.Id, tradeKey).Count(&linked).Error; err != nil {
			return err
		}
		if linked > 0 {
			return errors.New("本订单已关联其他收款流水，请核对交易号")
		}
		var payment ReceiptPayment
		err := tx.Where("trade_key = ?", tradeKey).Take(&payment).Error
		cents := amount.Mul(decimal.NewFromInt(100)).IntPart()
		if err == nil {
			if payment.TopUpID != 0 && payment.TopUpID != top.Id {
				return errors.New("该支付宝交易号已用于其他订单")
			}
			if payment.AmountCents != cents {
				return errors.New("实收金额与已保存的支付宝流水不一致")
			}
			if err := tx.Model(&payment).Update("top_up_id", top.Id).Error; err != nil {
				return err
			}
		} else if errors.Is(err, gorm.ErrRecordNotFound) {
			// 人工记录使用独立流水标识，支付时间为空表示尚无支付宝账务时间证据。
			payment = ReceiptPayment{ID: fmt.Sprintf("%x", sha256.Sum256([]byte("manual:"+receipt.AccountID+":"+providerTradeNo))), TradeKey: tradeKey, TopUpID: top.Id, AmountCents: cents, ProviderTradeNo: providerTradeNo}
			if err := tx.Create(&payment).Error; err != nil {
				return err
			}
		} else {
			return err
		}
		if err := creditTopUpWithCommission(tx, &commission, top.UserId, receipt.CreditQuota, nil); err != nil {
			return err
		}
		if err := tx.Model(&top).Updates(map[string]any{"status": common.TopUpStatusSuccess, "complete_time": time.Now().Unix(), "paid_time": payment.PaidAt, "actual_money": float64(cents) / 100, "provider_trade_no": providerTradeNo, "last_error": ""}).Error; err != nil {
			return err
		}
		// 详细原因仅存订单事件，沿用订单日志清理；流水摘要保留以防重复补单。
		message := fmt.Sprintf("人工补单：应付 %.2f 元，实收 %s 元，按原订单额度 %s 到账；支付宝交易号：%s；原因：%s；操作 IP：%s", top.Money, amount.StringFixed(2), logger.LogQuota(receipt.CreditQuota), providerTradeNo, reason, actorIP)
		if err := recordOrderEvent(tx, top.Id, actorID, message); err != nil {
			return err
		}
		credited = true
		return nil
	})
	if err == nil && credited {
		syncCreditUserQuotaCache(top.UserId, receipt.CreditQuota, "alipay receipt manual")
		finishTopUpInviteCommission(commission)
		RecordTopupLog(top.UserId, fmt.Sprintf("人工补单充值成功，充值金额: %v，支付金额：%s", logger.LogQuota(receipt.CreditQuota), amount.StringFixed(2)), actorIP, AlipayReceiptProvider, AlipayReceiptProvider)
	}
	return err
}
