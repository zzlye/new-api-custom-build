package model

import (
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// 历史文字账单只用于一次性核对；新余额从此使用整型赠金字段记账。
type legacyGiftReplay struct {
	gift    int
	charges map[string][2]int
}

func legacyLoggedQuota(content string) (int, error) {
	unit := decimal.NewFromFloat(common.QuotaPerUnit)
	symbol := operation_setting.GetGeneralSetting().CustomCurrencySymbol
	rate := operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate
	if rate <= 0 {
		rate = 1
	}
	if symbol == "" {
		symbol = "¤"
	}
	for _, format := range []struct {
		symbol string
		rate   float64
	}{{"＄", 1}, {"$", 1}, {"¥", operation_setting.USDExchangeRate}, {symbol, rate}} {
		_, value, found := strings.Cut(content, format.symbol)
		if !found || format.rate <= 0 {
			continue
		}
		value, _, found = strings.Cut(value, " 额度")
		if !found {
			continue
		}
		amount, err := decimal.NewFromString(strings.TrimSpace(value))
		if err != nil || amount.IsNegative() {
			continue
		}
		quota := amount.Mul(unit).Div(decimal.NewFromFloat(format.rate))
		if !quota.Equal(quota.Truncate(0)) {
			return 0, errors.New("历史金额精度不足")
		}
		return common.WalletQuotaFromDecimalStrict(quota)
	}
	fields := strings.Fields(content)
	for i := 1; i < len(fields); i++ {
		if strings.HasPrefix(fields[i], "点额度") {
			value, err := decimal.NewFromString(fields[i-1])
			if err == nil && !value.IsNegative() && value.Equal(value.Truncate(0)) {
				return common.WalletQuotaFromDecimalStrict(value)
			}
		}
	}
	return 0, errors.New("历史金额格式未识别")
}

func (r *legacyGiftReplay) apply(log Log) error {
	var other struct {
		BillingSource string `json:"billing_source"`
		TaskID        string `json:"task_id"`
		Op            *struct {
			Action string         `json:"action"`
			Params map[string]any `json:"params"`
		} `json:"op"`
	}
	if log.Other != "" {
		if err := common.UnmarshalJsonStr(log.Other, &other); err != nil {
			return errors.New("历史明细格式异常")
		}
	}
	switch log.Type {
	case LogTypeConsume, LogTypeRefund:
		if log.Quota < 0 {
			return errors.New("历史消费存在负数")
		}
		if other.BillingSource == "subscription" {
			return nil
		}
		key := log.RequestId
		if other.TaskID != "" {
			key = "task:" + other.TaskID
		}
		if log.Type == LogTypeConsume {
			gift := min(r.gift, log.Quota)
			r.gift -= gift
			if key != "" {
				entry := r.charges[key]
				entry[0] += log.Quota
				entry[1] += gift
				r.charges[key] = entry
			}
			return nil
		}
		entry, ok := r.charges[key]
		if !ok || entry[0] < log.Quota {
			// 缺少原扣款记录的退款保留普通余额，不凭退款金额推测赠金。
			return nil
		}
		gift := WalletGiftRefund(entry[0], entry[1], log.Quota)
		r.gift += gift
		r.charges[key] = [2]int{entry[0] - log.Quota, entry[1] - gift}
	case LogTypeSystem:
		isAffiliate := strings.HasPrefix(log.Content, "邀请用户赠送 ") || strings.HasPrefix(log.Content, "邀请用户充值返利 ")
		isCheckin := strings.HasPrefix(log.Content, "用户签到，获得额度 ")
		isRegistration := strings.HasPrefix(log.Content, "新用户注册赠送 ") || strings.HasPrefix(log.Content, "使用邀请码赠送 ")
		if !isAffiliate && !isCheckin && !isRegistration {
			return nil
		}
		amount, err := legacyLoggedQuota(log.Content)
		if err != nil {
			return err
		}
		if amount > common.MaxWalletQuota-r.gift {
			return ErrWalletQuotaLimitExceeded
		}
		r.gift += amount
	case LogTypeTopup, LogTypeManage:
		if other.Op != nil && strings.HasPrefix(other.Op.Action, "user.quota_") {
			if target, ok := other.Op.Params["target_user_id"].(float64); ok && target != float64(log.UserId) {
				// 操作员账单中的其他用户额度调整，不属于操作员自己的消费。
				return nil
			}
			from, beforeOK := other.Op.Params["from"].(float64)
			to, afterOK := other.Op.Params["to"].(float64)
			if !beforeOK || !afterOK {
				return errors.New("人工调整缺少金额记录")
			}
			after, err := common.WalletQuotaFromDecimalStrict(decimal.NewFromFloat(to))
			if err != nil {
				return err
			}
			before, err := common.WalletQuotaFromDecimalStrict(decimal.NewFromFloat(from))
			if err != nil {
				return err
			}
			if after < before {
				r.gift = max(0, r.gift-(before-after))
			}
			return nil
		}
		if log.Type == LogTypeManage {
			return nil
		}
		if strings.HasPrefix(log.Content, "使用余额购买订阅成功") {
			_, value, ok := strings.Cut(log.Content, "扣除额度: ")
			if !ok {
				return errors.New("历史订阅购买缺少扣款金额")
			}
			amount, err := decimal.NewFromString(strings.TrimSpace(value))
			if err != nil || amount.IsNegative() {
				return errors.New("历史订阅购买金额异常")
			}
			quota, err := common.WalletQuotaFromDecimalStrict(amount)
			if err != nil {
				return err
			}
			r.gift = max(0, r.gift-quota)
			return nil
		}
		// 普通充值和兑换码不改变赠金，缺失或未知的入账不推测。
	}
	return nil
}

// ReconcileLegacyGiftQuotas 只重放保留的账单，缺失记录不补发也不倒推。
// 拆出的赠金最多为当前可用余额；版本标记防止重启后重复划分已消费的赠金。
func ReconcileLegacyGiftQuotas() error {
	var users []User
	if err := DB.Where("gift_quota_version = ?", 0).Find(&users).Error; err != nil {
		return err
	}
	for _, user := range users {
		replay := legacyGiftReplay{charges: make(map[string][2]int)}
		var records []Log
		err := LOG_DB.Select("id", "user_id", "type", "quota", "content", "other", "request_id").
			Where("user_id = ? AND type IN ?", user.Id, []int{LogTypeConsume, LogTypeRefund, LogTypeTopup, LogTypeSystem, LogTypeManage}).
			Order("created_at, id").Find(&records).Error
		if err != nil {
			return err
		}
		skipped := 0
		for _, record := range records {
			if replay.apply(record) != nil {
				skipped++
			}
		}
		gift := min(max(replay.gift, user.GiftQuota), max(user.Quota, 0))
		// 比较读取时的账面数值，避免其他节点正在入账时覆盖其新数据。
		migrated := false
		err = DB.Transaction(func(tx *gorm.DB) error {
			result := tx.Model(&User{}).Where("id = ? AND gift_quota_version = 0 AND quota = ? AND used_quota = ? AND gift_quota = ? AND aff_history = ?", user.Id, user.Quota, user.UsedQuota, user.GiftQuota, user.AffHistoryQuota).
				Updates(map[string]any{"gift_quota": gift, "gift_quota_version": 1})
			if result.Error != nil || result.RowsAffected == 0 {
				return result.Error
			}
			// 尚在执行的旧任务也保留账单可证明的赠金扣款，升级后失败退款仍按原类别退回。
			var pending []Task
			if err := tx.Select("id", "task_id", "quota", "private_data").Where("user_id = ? AND quota > 0 AND gift_quota = 0 AND status NOT IN ?", user.Id, []TaskStatus{TaskStatusSuccess, TaskStatusFailure}).Find(&pending).Error; err != nil {
				return err
			}
			for _, task := range pending {
				if task.PrivateData.BillingSource == "subscription" {
					continue
				}
				charge := replay.charges["task:"+task.TaskID]
				giftConsumed := min(charge[1], task.Quota)
				if giftConsumed > 0 {
					if err := tx.Model(&Task{}).Where("id = ? AND quota = ? AND gift_quota = 0", task.ID, task.Quota).Update("gift_quota", giftConsumed).Error; err != nil {
						return err
					}
				}
			}
			migrated = true
			return nil
		})
		if err != nil {
			return err
		}
		if migrated {
			// 启动迁移时缓存尚未初始化，直接使用已读取的用户名记录账目。
			if err := createLog(&Log{UserId: user.Id, Username: user.Username, Type: LogTypeSystem, CreatedAt: common.GetTimestamp(),
				Content: fmt.Sprintf("历史赠金按保留记录划分；总余额保持 %d，赠金 %d，跳过无金额依据记录 %d 条", user.Quota, gift, skipped),
			}); err != nil {
				common.SysError("记录历史赠金划分失败: " + err.Error())
			}
		}
	}
	return nil
}
