package model

import (
	"context"
	"crypto/rand"
	"errors"
	"math/big"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

const (
	LotteryDraft            = "draft"
	LotteryActive           = "active"
	LotteryPaused           = "paused"
	LotteryEnded            = "ended"
	LotteryExhausted        = "exhausted"
	LotteryInitialPercent   = "initial_percent"
	LotteryRemainingPercent = "remaining_percent"
	LotteryRandomShares     = "random_shares"
	LotteryFixed            = "fixed"
	LotteryTopUpLifetime    = "lifetime"
	LotteryTopUpPeriod      = "period"
)

// 活动独立记账，开始后固定规则，暂停和恢复均不重置奖池或个人次数。
type LotteryActivity struct {
	ID              int    `json:"id"`
	Name            string `json:"name" gorm:"size:120"`
	Status          string `json:"status" gorm:"size:20;index"`
	Mode            string `json:"mode" gorm:"size:24"`
	TotalQuota      int    `json:"total_quota" gorm:"type:bigint"`
	RemainingQuota  int    `json:"remaining_quota" gorm:"type:bigint"`
	MinTopUpQuota   int    `json:"min_topup_quota" gorm:"type:bigint"`
	TopUpScope      string `json:"topup_scope" gorm:"size:16;default:lifetime"`
	TopUpStart      int64  `json:"topup_start" gorm:"default:0"`
	TopUpEnd        int64  `json:"topup_end" gorm:"default:0"`
	DeletedAt       int64  `json:"-" gorm:"default:0;index"`
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

// 清理只隐藏展示记录并清空 IP，流水继续作为次数、财务及幂等凭据。
type LotteryDraw struct {
	ID           int    `json:"id"`
	ActivityID   int    `json:"activity_id" gorm:"index;uniqueIndex:idx_lottery_request,priority:1;index:idx_lottery_user,priority:1"`
	UserID       int    `json:"user_id" gorm:"uniqueIndex:idx_lottery_request,priority:2;index:idx_lottery_user,priority:2"`
	RequestID    string `json:"-" gorm:"size:64;uniqueIndex:idx_lottery_request,priority:3"`
	Quota        int    `json:"quota" gorm:"type:bigint"`
	CreatedAt    int64  `json:"created_at"`
	LogClearedAt int64  `json:"-" gorm:"default:0;index"`
	IP           string `json:"ip,omitempty" gorm:"size:64"`
	Username     string `json:"username,omitempty" gorm:"-"`
}

type LotteryRules struct {
	Name          string `json:"name"`
	Mode          string `json:"mode"`
	TotalQuota    int    `json:"total_quota"`
	MinTopUpQuota int    `json:"min_topup_quota"`
	TopUpScope    string `json:"topup_scope"`
	TopUpStart    int64  `json:"topup_start"`
	TopUpEnd      int64  `json:"topup_end"`
	PerUserLimit  int    `json:"per_user_limit"`
	MinPercent    int    `json:"min_percent"`
	MaxPercent    int    `json:"max_percent"`
	Shares        int    `json:"shares"`
	FixedQuota    int    `json:"fixed_quota"`
}

func (rules LotteryRules) activity() (LotteryActivity, error) {
	name := strings.TrimSpace(rules.Name)
	if name == "" || utf8.RuneCountInString(name) > 80 || rules.TotalQuota <= 0 || rules.TotalQuota > common.MaxWalletQuota || rules.MinTopUpQuota < 0 || rules.MinTopUpQuota > common.MaxWalletQuota || rules.PerUserLimit < 1 || rules.PerUserLimit > common.MaxQuota {
		return LotteryActivity{}, errors.New("抽奖名称、奖池、充值门槛或每人次数无效")
	}
	a := LotteryActivity{Name: name, Mode: rules.Mode, TotalQuota: rules.TotalQuota, RemainingQuota: rules.TotalQuota, MinTopUpQuota: rules.MinTopUpQuota, PerUserLimit: rules.PerUserLimit}
	// 未配置的新字段沿用累计充值，升级不能改变旧活动的资格规则。
	a.TopUpScope = rules.TopUpScope
	if a.TopUpScope == "" {
		a.TopUpScope = LotteryTopUpLifetime
	}
	switch a.TopUpScope {
	case LotteryTopUpLifetime:
	case LotteryTopUpPeriod:
		if rules.TopUpStart <= 0 || rules.TopUpEnd <= rules.TopUpStart || rules.TopUpEnd > 253402271999 {
			return a, errors.New("请选择有效的充值起止时间")
		}
		a.TopUpStart, a.TopUpEnd = rules.TopUpStart, rules.TopUpEnd
	default:
		return a, errors.New("充值门槛类型无效")
	}
	switch rules.Mode {
	case LotteryInitialPercent, LotteryRemainingPercent:
		// 百分比用万分之一的整数存储，避免浮点误差改变抽奖边界。
		if rules.MinPercent < 1 || rules.MaxPercent > 10000 || rules.MaxPercent < rules.MinPercent {
			return a, errors.New("随机比例须在 0.01% 至 100% 之间，且上限不能小于下限")
		}
		a.MinPercent, a.MaxPercent = rules.MinPercent, rules.MaxPercent
	case LotteryRandomShares:
		if rules.Shares < 1 || rules.Shares > common.MaxQuota || rules.Shares > rules.TotalQuota {
			return a, errors.New("奖池份数无效或金额不足以分配")
		}
		a.Shares, a.RemainingShares = rules.Shares, rules.Shares
	case LotteryFixed:
		if rules.FixedQuota < 1 || rules.FixedQuota > rules.TotalQuota {
			return a, errors.New("固定奖励必须大于零且不能超过总奖池")
		}
		a.FixedQuota = rules.FixedQuota
	default:
		return a, errors.New("抽奖方式无效")
	}
	return a, nil
}

func SaveLotteryActivity(id, operator int, rules LotteryRules) (*LotteryActivity, error) {
	a, err := rules.activity()
	if err != nil {
		return nil, err
	}
	if id == 0 {
		a.Status, a.CreatedBy, a.CreatedAt = LotteryDraft, operator, time.Now().Unix()
		return &a, DB.Create(&a).Error
	}
	err = DB.Transaction(func(tx *gorm.DB) error {
		old, err := lockLotteryActivity(tx, id)
		if err != nil {
			return err
		}
		if old.DeletedAt != 0 || old.Status != LotteryDraft {
			return errors.New("已开始的活动不能修改规则，请新建活动")
		}
		a.ID, a.Status, a.CreatedBy, a.CreatedAt = old.ID, old.Status, old.CreatedBy, old.CreatedAt
		return tx.Save(&a).Error
	})
	return &a, err
}

func lockLotteryActivity(tx *gorm.DB, id int) (*LotteryActivity, error) {
	if common.UsingMainDatabase(common.DatabaseTypeSQLite) {
		// 先取得写锁再读取，SQLite 与另外两种数据库使用同一套原子记账语义。
		if err := tx.Model(&LotteryActivity{}).Where("id = ?", id).UpdateColumn("remaining_quota", gorm.Expr("remaining_quota")).Error; err != nil {
			return nil, err
		}
	}
	var a LotteryActivity
	if err := lockForUpdate(tx).First(&a, id).Error; err != nil {
		return nil, err
	}
	return &a, nil
}

func ChangeLotteryState(id int, action string) (*LotteryActivity, error) {
	var a *LotteryActivity
	err := DB.Transaction(func(tx *gorm.DB) error {
		var err error
		a, err = lockLotteryActivity(tx, id)
		if err != nil {
			return err
		}
		if a.DeletedAt != 0 {
			return errors.New("活动已删除")
		}
		switch action {
		case "start":
			if a.Status != LotteryDraft && a.Status != LotteryPaused {
				return errors.New("该活动不能开始")
			}
			a.Status = LotteryActive
			if a.StartedAt == 0 {
				a.StartedAt = time.Now().Unix()
			}
		case "pause":
			if a.Status != LotteryActive {
				return errors.New("该活动不能暂停")
			}
			a.Status = LotteryPaused
		case "end":
			if a.Status != LotteryActive && a.Status != LotteryPaused && a.Status != LotteryDraft {
				return errors.New("该活动已经结束")
			}
			a.Status, a.EndedAt = LotteryEnded, time.Now().Unix()
		default:
			return errors.New("活动操作无效")
		}
		return tx.Save(a).Error
	})
	return a, err
}

// 只累计成功充值的到账额度；不读取余额、赠金或展示日志，兼容各支付渠道的历史单位。
func LotteryPaidTopUpQuota(tx *gorm.DB, userID int) (int, error) {
	rows, err := tx.Model(&TopUp{}).Where("user_id = ? AND status = ?", userID, common.TopUpStatusSuccess).
		Select("COALESCE(payment_provider, ''), COALESCE(credit_quota, 0), COALESCE(amount, 0), COALESCE(money, 0)").Rows()
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	total := decimal.Zero
	for rows.Next() {
		var provider string
		var credit, amount, money decimal.Decimal
		if err := rows.Scan(&provider, &credit, &amount, &money); err != nil {
			return 0, err
		}
		value := amount.Mul(decimal.NewFromFloat(common.QuotaPerUnit))
		switch provider {
		case PaymentProviderStripe:
			value = money.Mul(decimal.NewFromFloat(common.QuotaPerUnit))
		case PaymentProviderCreem:
			value = amount
		case "", PaymentProviderEpay, PaymentProviderWaffo, PaymentProviderWaffoPancake, AlipayReceiptProvider:
		default:
			continue
		}
		if credit.IsPositive() {
			value = credit
		}
		// 逐笔沿用入账舍入规则，并用十进制累加，避免数据库 SUM(bigint) 溢出。
		if value.IsPositive() {
			total = total.Add(value.Round(0))
		}
		// 资格门槛不超过钱包上限，累计值饱和不影响资格判断。
		if total.GreaterThanOrEqual(decimal.NewFromInt(common.MaxWalletQuota)) {
			return common.MaxWalletQuota, nil
		}
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	return common.WalletQuotaFromDecimalStrict(total)
}

func (a *LotteryActivity) paidTopUpQuota(tx *gorm.DB, userID int) (int, error) {
	if a.TopUpScope == LotteryTopUpPeriod {
		// 以实际到账时间为准，起止秒均包含；没有到账时间的旧记录不猜测归属。
		tx = tx.Where("complete_time >= ? AND complete_time <= ?", a.TopUpStart, a.TopUpEnd)
	}
	return LotteryPaidTopUpQuota(tx, userID)
}

// 删除只隐藏活动，保留已发赠金、财务流水和请求幂等凭据。
func DeleteLotteryActivity(id int) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		a, err := lockLotteryActivity(tx, id)
		if err != nil {
			return err
		}
		if a.DeletedAt != 0 {
			return nil
		}
		if a.Status != LotteryDraft && a.Status != LotteryEnded && a.Status != LotteryExhausted {
			return errors.New("请先结束活动再删除")
		}
		return tx.Model(a).Update("deleted_at", time.Now().Unix()).Error
	})
}

func lotteryRandomBetween(low, high int) (int, error) {
	value, err := rand.Int(rand.Reader, big.NewInt(int64(high-low)+1))
	if err != nil {
		return 0, err
	}
	return low + int(value.Int64()), nil
}

func (a *LotteryActivity) drawQuota() (int, error) {
	switch a.Mode {
	case LotteryFixed:
		// 最后一份只发剩余额度，避免货币换算尾差使奖池无法领完。
		return min(a.FixedQuota, a.RemainingQuota), nil
	case LotteryRandomShares:
		if a.RemainingShares < 1 {
			return 0, errors.New("奖池已领完")
		}
		if a.RemainingShares == 1 {
			return a.RemainingQuota, nil
		}
		// 二倍均值随机分配，并为每个后续份额保留至少一个最小余额单位。
		upper := min(a.RemainingQuota-a.RemainingShares+1, 2*(a.RemainingQuota/a.RemainingShares))
		return lotteryRandomBetween(1, max(1, upper))
	case LotteryInitialPercent, LotteryRemainingPercent:
		base := a.TotalQuota
		if a.Mode == LotteryRemainingPercent {
			base = a.RemainingQuota
		}
		low, err := common.WalletQuotaFromDecimalStrict(decimal.NewFromInt(int64(base)).Mul(decimal.NewFromInt(int64(a.MinPercent))).Div(decimal.NewFromInt(10000)).Floor())
		if err != nil {
			return 0, err
		}
		high, err := common.WalletQuotaFromDecimalStrict(decimal.NewFromInt(int64(base)).Mul(decimal.NewFromInt(int64(a.MaxPercent))).Div(decimal.NewFromInt(10000)).Floor())
		if err != nil {
			return 0, err
		}
		// 尾额不超过剩余奖池；保底一个最小余额单位，避免小奖池永远领不完。
		return lotteryRandomBetween(min(a.RemainingQuota, max(1, low)), min(a.RemainingQuota, max(1, high)))
	}
	return 0, errors.New("抽奖方式无效")
}

func DrawLottery(activityID, userID int, requestID, ip string) (*LotteryDraw, error) {
	if len(requestID) < 16 || len(requestID) > 64 || strings.TrimSpace(requestID) != requestID {
		return nil, errors.New("抽奖请求编号无效")
	}
	var result LotteryDraw
	var credited bool
	err := DB.Transaction(func(tx *gorm.DB) error {
		a, err := lockLotteryActivity(tx, activityID)
		if err != nil {
			return err
		}
		// 重放同一请求只返回第一次结果，断网重试不会多扣次数或多发赠金。
		err = tx.Where("activity_id = ? AND user_id = ? AND request_id = ?", activityID, userID, requestID).First(&result).Error
		if err == nil {
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if a.DeletedAt != 0 || a.Status != LotteryActive || a.RemainingQuota <= 0 {
			return errors.New("活动未开放或奖池已领完")
		}
		var user User
		if err := tx.Select("id", "status").First(&user, userID).Error; err != nil {
			return err
		}
		if user.Status != common.UserStatusEnabled {
			return errors.New("账号不可参与抽奖")
		}
		paid, err := a.paidTopUpQuota(tx, userID)
		if err != nil {
			return err
		}
		if paid < a.MinTopUpQuota {
			return errors.New("充值未达到活动门槛")
		}
		var used int64
		if err := tx.Model(&LotteryDraw{}).Where("activity_id = ? AND user_id = ?", activityID, userID).Count(&used).Error; err != nil {
			return err
		}
		if used >= int64(a.PerUserLimit) {
			return errors.New("本场活动的抽奖次数已用完")
		}
		amount, err := a.drawQuota()
		if err != nil {
			return err
		}
		if amount <= 0 || amount > a.RemainingQuota {
			return errors.New("奖池金额异常")
		}
		// 发奖、流水、剩余金额和次数在同一事务内提交，任何一步失败都不消耗抽奖机会。
		if err := creditGiftQuota(tx, userID, amount, nil); err != nil {
			return err
		}
		result = LotteryDraw{ActivityID: activityID, UserID: userID, RequestID: requestID, IP: ip, Quota: amount, CreatedAt: time.Now().Unix()}
		if err := tx.Create(&result).Error; err != nil {
			return err
		}
		a.RemainingQuota -= amount
		a.DrawCount++
		if a.Mode == LotteryRandomShares {
			a.RemainingShares--
		}
		if a.RemainingQuota == 0 {
			a.Status, a.EndedAt = LotteryExhausted, time.Now().Unix()
		}
		if err := tx.Save(a).Error; err != nil {
			return err
		}
		credited = true
		return nil
	})
	if err != nil {
		return nil, err
	}
	if credited {
		syncCreditUserQuotaCache(userID, result.Quota, "lottery reward")
	}
	result.IP = ""
	return &result, nil
}

type LotteryUserActivity struct {
	LotteryActivity
	UsedDraws          int64 `json:"used_draws"`
	EligibleTopUpQuota int   `json:"eligible_topup_quota"`
}

func ListLotteryActivities(userID, offset, limit int, root bool) ([]LotteryUserActivity, int64, error) {
	query := DB.Model(&LotteryActivity{}).Where("deleted_at = 0")
	if !root {
		query = query.Where("status <> ?", LotteryDraft)
		query = query.Order("CASE WHEN status = 'active' THEN 0 WHEN status = 'paused' THEN 1 ELSE 2 END")
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var activities []LotteryActivity
	if err := query.Order("id DESC").Offset(max(0, offset)).Limit(min(100, max(1, limit))).Find(&activities).Error; err != nil {
		return nil, 0, err
	}
	items := make([]LotteryUserActivity, 0, len(activities))
	paidByPeriod := make(map[[2]int64]int)
	for _, a := range activities {
		var used int64
		var paid int
		if !root {
			if err := DB.Model(&LotteryDraw{}).Where("activity_id = ? AND user_id = ?", a.ID, userID).Count(&used).Error; err != nil {
				return nil, 0, err
			}
			period := [2]int64{a.TopUpStart, a.TopUpEnd}
			var ok bool
			paid, ok = paidByPeriod[period]
			if !ok {
				var err error
				paid, err = a.paidTopUpQuota(DB, userID)
				if err != nil {
					return nil, 0, err
				}
				paidByPeriod[period] = paid
			}
		}
		items = append(items, LotteryUserActivity{LotteryActivity: a, UsedDraws: used, EligibleTopUpQuota: paid})
	}
	return items, total, nil
}

func ListLotteryDraws(activityID, userID, offset, limit int, root bool) ([]LotteryDraw, int64, error) {
	if !root {
		var activity LotteryActivity
		if err := DB.Where("deleted_at = 0 AND status <> ?", LotteryDraft).First(&activity, activityID).Error; err != nil {
			return nil, 0, err
		}
	}
	query := DB.Model(&LotteryDraw{}).Where("activity_id = ? AND log_cleared_at = 0", activityID)
	if !root {
		query = query.Where("user_id = ?", userID)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	items := make([]LotteryDraw, 0)
	if err := query.Order("id DESC").Offset(max(0, offset)).Limit(min(100, max(1, limit))).Find(&items).Error; err != nil {
		return nil, 0, err
	}
	if root && len(items) > 0 {
		ids := make([]int, 0, len(items))
		for _, item := range items {
			ids = append(ids, item.UserID)
		}
		var users []User
		if err := DB.Select("id", "username").Where("id IN ?", ids).Find(&users).Error; err != nil {
			return nil, 0, err
		}
		names := make(map[int]string, len(users))
		for _, u := range users {
			names[u.Id] = u.Username
		}
		for i := range items {
			items[i].Username = names[items[i].UserID]
		}
	} else {
		for i := range items {
			items[i].IP = ""
		}
	}
	return items, total, nil
}

type LotteryWinner struct {
	ID       int    `json:"id"`
	Username string `json:"username"`
	Quota    int    `json:"quota"`
}

// 中奖滚动栏只返回脱敏名称和奖励，不暴露用户编号、地址或请求凭据。
func ListLotteryWinners(activityID int) ([]LotteryWinner, error) {
	items := make([]LotteryWinner, 0)
	err := DB.Table("lottery_draws AS d").
		Joins("JOIN lottery_activities AS a ON a.id = d.activity_id").
		Joins("LEFT JOIN users AS u ON u.id = d.user_id").
		Where("a.id = ? AND a.deleted_at = 0 AND a.status <> ?", activityID, LotteryDraft).
		Where("d.log_cleared_at = 0").
		Select("d.id, COALESCE(u.username, '') AS username, d.quota").Order("d.id DESC").Limit(30).Scan(&items).Error
	if err != nil {
		return nil, err
	}
	for i := range items {
		name := []rune(items[i].Username)
		items[i].Username = "***"
		if len(name) > 1 {
			items[i].Username = string(name[0]) + "***"
		}
		if len(name) > 2 {
			items[i].Username += string(name[len(name)-1])
		}
	}
	return items, nil
}

// 只清理已结束或已领完活动中早于截止时间的记录；终态活动不能再恢复。
func OldLotteryLogQuery(ctx context.Context, before int64) *gorm.DB {
	finished := DB.WithContext(ctx).Model(&LotteryActivity{}).Select("id").
		Where("status IN ?", []string{LotteryEnded, LotteryExhausted})
	return DB.WithContext(ctx).Model(&LotteryDraw{}).
		Where("created_at < ? AND log_cleared_at = 0", before).
		Where("activity_id IN (?)", finished)
}

func DeleteOldLotteryLogs(ctx context.Context, before int64, limit int) (int64, error) {
	var ids []int
	if err := OldLotteryLogQuery(ctx, before).Order("id").Limit(min(1000, max(1, limit))).Pluck("id", &ids).Error; err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	// 再次限定清理范围，重复执行不重复计数；不改动奖池、钱包和请求编号。
	result := OldLotteryLogQuery(ctx, before).Where("id IN ?", ids).
		Updates(map[string]any{"log_cleared_at": time.Now().Unix(), "ip": ""})
	return result.RowsAffected, result.Error
}
