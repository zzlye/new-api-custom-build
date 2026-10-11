package model

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

var ErrInsufficientGiftQuota = errors.New("赠金不足")
var ErrInsufficientWalletQuota = errors.New("余额不足")

// ConsumeWalletQuota 在同一事务内扣总余额和赠金，返回本次实际扣除的赠金。
// 钱包分类必须立即一起落库，不能把两个余额分别排队，否则并发退款会改变资金类别。
func ConsumeWalletQuota(userID, amount int, requireSufficient bool) (int, error) {
	if amount < 0 || amount > common.MaxWalletQuota {
		return 0, ErrInvalidUserQuotaAdjustment
	}
	if amount == 0 {
		return 0, nil
	}
	var gift int
	err := DB.Transaction(func(tx *gorm.DB) error {
		if common.UsingMainDatabase(common.DatabaseTypeSQLite) {
			// SQLite 没有行锁，先取得写锁再读余额，防止并发读事务升级写锁时互相阻塞。
			if err := tx.Model(&User{}).Where("id = ?", userID).UpdateColumn("quota", gorm.Expr("quota")).Error; err != nil {
				return err
			}
		}
		var user User
		if err := lockForUpdate(tx).First(&user, userID).Error; err != nil {
			return err
		}
		if requireSufficient && user.Quota < amount {
			return ErrInsufficientWalletQuota
		}
		if user.Quota < -common.MaxWalletQuota+amount {
			return ErrWalletQuotaLimitExceeded
		}
		gift = min(max(user.GiftQuota, 0), amount)
		return tx.Model(&User{}).Where("id = ?", userID).Updates(map[string]any{
			"quota": user.Quota - amount, "gift_quota": user.GiftQuota - gift,
		}).Error
	})
	if err != nil {
		return 0, err
	}
	if cacheErr := cacheIncrUserQuota(userID, -int64(amount)); cacheErr != nil {
		common.SysError("同步钱包扣款缓存失败: " + cacheErr.Error())
	}
	return gift, nil
}

// RefundWalletQuota 仅由持有原扣款明细的调用者使用，避免把赠金退款变成普通余额。
func RefundWalletQuota(userID, amount, gift int) error {
	if amount < 0 || gift < 0 || gift > amount || amount > common.MaxWalletQuota {
		return ErrInvalidUserQuotaAdjustment
	}
	if amount == 0 {
		return nil
	}
	err := creditTopUpQuota(DB, userID, amount, nil, gift)
	if err == nil {
		syncCreditUserQuotaCache(userID, amount, "wallet refund")
	}
	return err
}

// creditGiftQuota 在奖励所属事务中同时限制总余额和赠金，欠费账户也不能绕过赠金上限。
func creditGiftQuota(tx *gorm.DB, userID, amount int, updates map[string]any) error {
	if amount < 0 || amount > common.MaxWalletQuota {
		return ErrInvalidUserQuotaAdjustment
	}
	if amount == 0 {
		return nil
	}
	return creditTopUpQuota(tx, userID, amount, updates, amount)
}

// CreditGiftQuota 奖励仍直接进入可用总余额，同时记录其中的赠金。
func CreditGiftQuota(userID, amount int) error {
	return RefundWalletQuota(userID, amount, amount)
}

// WalletGiftRefund 按赠金优先消费的规则计算部分退款：先退普通余额，再退赠金。
func WalletGiftRefund(consumed, giftConsumed, refund int) int {
	return min(max(refund-max(consumed-giftConsumed, 0), 0), max(giftConsumed, 0))
}
