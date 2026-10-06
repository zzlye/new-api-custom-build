package model

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const userModelPricingPrefix = "UserModelPricing:"

var userModelPricingMu sync.Mutex

func IsUserModelPricingOption(key string) bool {
	return strings.HasPrefix(key, userModelPricingPrefix)
}

// 用户价格独立于全局价格保存；复用配置同步，让所有实例按原有机制获取更新。
func UserModelPricingFromCache(userID int) (map[string]PricingValues, error) {
	common.OptionMapRWMutex.RLock()
	raw := common.OptionMap[userModelPricingPrefix+strconv.Itoa(userID)]
	common.OptionMapRWMutex.RUnlock()
	values := make(map[string]PricingValues)
	if raw != "" {
		if err := common.UnmarshalJsonStr(raw, &values); err != nil {
			return nil, fmt.Errorf("用户定价配置读取失败: %w", err)
		}
	}
	return values, nil
}

// 一个用户模型规则是完整基础定价，缺少辅助倍率使用引擎默认值，不混入全局基础价格。
func EffectiveUserModelPricing(name string, pricing PricingValues) PricingValues {
	result := maps.Clone(pricing)
	if result["billing_setting.billing_mode"] == nil {
		result["billing_setting.billing_mode"] = billing_setting.BillingModeRatio
	}
	if result["billing_setting.billing_mode"] == billing_setting.BillingModeTieredExpr || result["ModelPrice"] != nil {
		return result
	}
	for key, fallback := range map[string]float64{
		"CompletionRatio":  ratio_setting.GetCompletionRatio(name),
		"CacheRatio":       ratio_setting.DefaultCacheRatio,
		"CreateCacheRatio": ratio_setting.DefaultCreateCacheRatio,
		"ImageRatio":       ratio_setting.DefaultImageRatio,
		"AudioRatio":       1, "AudioCompletionRatio": 1,
	} {
		if _, exists := result[key]; !exists {
			result[key] = fallback
		}
	}
	return result
}

func readUserModelPricing(db *gorm.DB, userID int) (map[string]PricingValues, error) {
	var row Option
	err := db.Where(commonKeyCol+" = ?", userModelPricingPrefix+strconv.Itoa(userID)).First(&row).Error
	values := make(map[string]PricingValues)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return values, nil
	}
	if err != nil {
		return nil, err
	}
	if err := common.UnmarshalJsonStr(row.Value, &values); err != nil {
		return nil, err
	}
	if values == nil {
		return nil, errors.New("用户定价必须是对象")
	}
	return values, nil
}

func GetUserModelPricingSnapshot(userID int) (*ModelPricingSnapshot, error) {
	var user User
	if err := DB.Select("id").First(&user, userID).Error; err != nil {
		return nil, err
	}
	rules, err := readUserModelPricing(DB, userID)
	if err != nil {
		return nil, err
	}
	result := &ModelPricingSnapshot{Entries: []ModelPricingEntry{}, Options: map[string]string{}, EmptyVersion: ModelPricingVersion(PricingValues{})}
	if len(rules) == 0 {
		return result, nil
	}
	names := make([]string, 0, len(rules))
	for name := range rules {
		names = append(names, name)
	}
	// 复用模型元信息，编辑器仍能显示视频用量字段及插件变体。
	result, err = GetModelPricingSnapshot(names)
	if err != nil {
		return nil, err
	}
	result.Options = map[string]string{}
	for i := range result.Entries {
		entry := &result.Entries[i]
		entry.Configured = rules[entry.ModelName]
		entry.Version = ModelPricingVersion(entry.Configured)
		entry.Effective = EffectiveUserModelPricing(entry.ModelName, entry.Configured)
		entry.CacheWriteMode = ResolveCacheWriteMode(entry.ModelName, entry.Configured)
		entry.BillingDetails = ResolveLegacyBillingDetails(entry.ModelName, entry.Effective, entry.Configured, true)
		variants, _ := entry.Configured[billing_setting.PluginBillingExprOption].(map[string]any)
		for j := range entry.PluginVariants {
			variant := &entry.PluginVariants[j]
			variant.Configured, _ = variants[variant.PluginKey].(string)
			variant.Effective = variant.Configured
			if variant.Effective == "" && entry.Effective["billing_setting.billing_mode"] == billing_setting.BillingModeTieredExpr {
				variant.Effective, _ = entry.Effective["billing_setting.billing_expr"].(string)
			}
			variant.Compatible = billing_setting.TaskExprCompatible(variant.Effective, variant.UsageSchema)
		}
	}
	return result, nil
}

func UpdateUserModelPricing(userID int, changes []ModelPricingChange) error {
	if userID <= 0 || len(changes) == 0 || len(changes) > 100 {
		return errors.New("请选择用户和模型定价，每次最多保存 100 条")
	}
	userModelPricingMu.Lock()
	defer userModelPricingMu.Unlock()
	key := userModelPricingPrefix + strconv.Itoa(userID)
	var encoded []byte
	err := DB.Transaction(func(tx *gorm.DB) error {
		var user User
		if err := tx.Select("id").First(&user, userID).Error; err != nil {
			return errors.New("用户不存在")
		}
		// 先创建唯一配置行再加锁，保证首次写入和跨实例并发也进行版本检查。
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&Option{Key: key, Value: "{}"}).Error; err != nil {
			return err
		}
		rules, err := readUserModelPricing(lockForUpdate(tx), userID)
		if err != nil {
			return err
		}
		seen := map[string]bool{}
		for _, change := range changes {
			if strings.TrimSpace(change.ModelName) != change.ModelName || change.ModelName == "" || len(change.ModelName) > 255 || seen[change.ModelName] {
				return errors.New("模型名称为空、重复或过长")
			}
			seen[change.ModelName] = true
			previous := rules[change.ModelName]
			if previous == nil {
				previous = PricingValues{}
			}
			if change.ExpectedVersion != ModelPricingVersion(previous) {
				return ErrModelPricingConflict
			}
			if change.Reset {
				delete(rules, change.ModelName)
				continue
			}
			pricing := change.Pricing
			if err := validateModelPricing(change.ModelName, pricing, previous); err != nil {
				return err
			}
			mode, _ := pricing["billing_setting.billing_mode"].(string)
			if mode == billing_setting.BillingModeTieredExpr {
				if expr, _ := pricing["billing_setting.billing_expr"].(string); strings.TrimSpace(expr) == "" {
					return errors.New("请设置用户的计费表达式")
				}
			} else if pricing["ModelPrice"] == nil && (mode == billing_setting.BillingModePerSecond || pricing["ModelRatio"] == nil) {
				return errors.New("请设置用户的模型基础价格")
			}
			rules[change.ModelName] = EffectiveUserModelPricing(change.ModelName, pricing)
		}
		encoded, err = common.Marshal(rules)
		if err != nil {
			return err
		}
		return tx.Model(&Option{}).Where(commonKeyCol+" = ?", key).Update("value", string(encoded)).Error
	})
	if err != nil {
		return err
	}
	return updateOptionMap(key, string(encoded))
}

// 价格页面只覆盖当前登录用户的基础价，复制缓存切片防止其他用户看到专属价格。
func PricingForUser(pricing []Pricing, userID int) ([]Pricing, error) {
	rules, err := UserModelPricingFromCache(userID)
	if err != nil {
		return nil, err
	}
	if len(rules) == 0 {
		return pricing, nil
	}
	result := slices.Clone(pricing)
	for i := range result {
		item := &result[i]
		configured, ok := rules[item.ModelName]
		if !ok {
			continue
		}
		values := EffectiveUserModelPricing(item.ModelName, configured)
		item.ModelPrice, _ = values["ModelPrice"].(float64)
		item.ModelRatio, _ = values["ModelRatio"].(float64)
		item.CompletionRatio, _ = values["CompletionRatio"].(float64)
		item.BillingMode, _ = values["billing_setting.billing_mode"].(string)
		item.BillingExpr, _ = values["billing_setting.billing_expr"].(string)
		item.QuotaType = 0
		if _, hasPrice := values["ModelPrice"]; hasPrice && item.BillingMode != billing_setting.BillingModeTieredExpr {
			item.QuotaType = 1
		}
		for key, target := range map[string]**float64{
			"CacheRatio": &item.CacheRatio, "CreateCacheRatio": &item.CreateCacheRatio,
			"ImageRatio": &item.ImageRatio, "AudioRatio": &item.AudioRatio, "AudioCompletionRatio": &item.AudioCompletionRatio,
		} {
			*target = nil
			if number, exists := values[key].(float64); exists {
				*target = &number
			}
		}
		item.PricingVersion = ModelPricingVersion(configured)
		item.BillingPluginVariants = slices.Clone(item.BillingPluginVariants)
		variants, _ := configured[billing_setting.PluginBillingExprOption].(map[string]any)
		for j := range item.BillingPluginVariants {
			variant := &item.BillingPluginVariants[j]
			variant.BillingMode, variant.BillingExpr = item.BillingMode, ""
			if item.BillingMode == billing_setting.BillingModeTieredExpr {
				variant.BillingExpr = item.BillingExpr
			}
			if expression, exists := variants[variant.PluginKey].(string); exists {
				variant.BillingMode, variant.BillingExpr = billing_setting.BillingModeTieredExpr, expression
			}
		}
	}
	return result, nil
}
