package model

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

var (
	ErrInvalidUserQuotaAdjustment = errors.New("invalid user quota adjustment")
	ErrUserQuotaPermission        = errors.New("cannot adjust quota for this user role")
)

// UserQuotaAdjustment is the immutable database snapshot of a committed manual
// adjustment. Pending relay deductions in the quota cache are not part of it.
type UserQuotaAdjustment struct {
	UserID     int
	Username   string
	Before     int
	After      int
	GiftBefore int
	GiftAfter  int
	Target     string
}

func AdjustUserQuota(userID, operatorRole int, mode string, value int, targets ...string) (*UserQuotaAdjustment, error) {
	// 空类型保留旧接口总余额语义，新界面明确选择普通余额或赠金。
	target := "total"
	if len(targets) > 0 && targets[0] != "" {
		target = targets[0]
	}
	if target != "total" && target != "balance" && target != "gift" {
		return nil, ErrInvalidUserQuotaAdjustment
	}
	if userID <= 0 || (mode != "add" && mode != "subtract" && mode != "override") {
		return nil, ErrInvalidUserQuotaAdjustment
	}
	if mode != "override" && value <= 0 {
		return nil, ErrInvalidUserQuotaAdjustment
	}
	if value > common.MaxWalletQuota || value < -common.MaxWalletQuota {
		return nil, ErrWalletQuotaLimitExceeded
	}

	var adjustment UserQuotaAdjustment
	err := DB.Transaction(func(tx *gorm.DB) error {
		var user User
		if err := lockForUpdate(tx).First(&user, userID).Error; err != nil {
			return err
		}
		if operatorRole != common.RoleRootUser && operatorRole <= user.Role {
			return ErrUserQuotaPermission
		}
		if user.Quota > common.MaxWalletQuota || user.Quota < -common.MaxWalletQuota {
			return ErrWalletQuotaLimitExceeded
		}
		current := user.Quota
		if target == "balance" {
			current -= user.GiftQuota
		}
		if target == "gift" {
			current = user.GiftQuota
		}
		quota := decimal.NewFromInt(int64(value))
		switch mode {
		case "add":
			quota = decimal.NewFromInt(int64(current)).Add(quota)
		case "subtract":
			quota = decimal.NewFromInt(int64(current)).Sub(quota)
		}
		after, err := common.WalletQuotaFromDecimalStrict(quota)
		if err != nil {
			return ErrWalletQuotaLimitExceeded
		}
		giftAfter := user.GiftQuota
		switch target {
		case "gift":
			if after < 0 {
				return ErrInsufficientGiftQuota
			}
			giftAfter = after
			after, err = common.WalletQuotaFromDecimalStrict(decimal.NewFromInt(int64(user.Quota)).Add(decimal.NewFromInt(int64(giftAfter - user.GiftQuota))))
		case "balance":
			after, err = common.WalletQuotaFromDecimalStrict(decimal.NewFromInt(int64(after)).Add(decimal.NewFromInt(int64(user.GiftQuota))))
		default:
			// 老客户端减少总额时仍先扣赠金，不能让剩余赠金超过可用总额。
			if after < user.Quota {
				giftAfter = max(0, user.GiftQuota-(user.Quota-after))
			}
		}
		if err != nil {
			return ErrWalletQuotaLimitExceeded
		}
		// An unchanged override is a successful operation, including on MySQL
		// configurations that count only changed rows in RowsAffected.
		if after != user.Quota {
			result := tx.Model(&User{}).Where("id = ?", userID).Updates(map[string]any{"quota": after, "gift_quota": giftAfter})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return gorm.ErrRecordNotFound
			}
		}
		adjustment = UserQuotaAdjustment{UserID: user.Id, Username: user.Username, Before: user.Quota, After: after, GiftBefore: user.GiftQuota, GiftAfter: giftAfter, Target: target}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Apply only the committed difference, preserving outstanding reservations.
	// Both balances are bounded above, so their difference fits in int64.
	delta := int64(adjustment.After) - int64(adjustment.Before)
	if delta != 0 {
		if err := cacheIncrUserQuota(userID, delta); err != nil {
			common.SysError(fmt.Sprintf("failed to sync manual quota adjustment for user %d: %s", userID, err))
		}
	}
	return &adjustment, nil
}
