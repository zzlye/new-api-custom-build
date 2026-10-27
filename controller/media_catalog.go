package controller

import (
	"slices"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

// 目录只包含当前 Key 已通过权限和计费配置检查的模型，不改变分组选路。
func buildMediaCatalog(pricing []model.Pricing, allowedModels, groups []string, ratios map[string]float64) gin.H {
	allowed := make(map[string]bool, len(allowedModels))
	for _, name := range allowedModels {
		allowed[name] = true
	}
	filtered := make([]model.Pricing, 0)
	for _, item := range pricing {
		if !allowed[item.ModelName] {
			continue
		}
		visibleGroups := make([]string, 0)
		for _, group := range groups {
			if slices.Contains(item.EnableGroup, group) || slices.Contains(item.EnableGroup, "all") {
				visibleGroups = append(visibleGroups, group)
			}
		}
		if len(visibleGroups) > 0 {
			item.EnableGroup = visibleGroups
			filtered = append(filtered, item)
		}
	}
	return gin.H{"groups": groups, "group_ratio": ratios, "data": filtered}
}

func mediaCatalogForRequest(c *gin.Context, models, groups []string, userGroup string) (gin.H, error) {
	pricing, err := model.PricingForUser(model.GetPricing(), c.GetInt("id"))
	if err != nil {
		return nil, err
	}
	ratios := make(map[string]float64, len(groups))
	for _, group := range groups {
		ratios[group] = service.GetUserGroupRatio(userGroup, group)
	}
	return buildMediaCatalog(pricing, models, groups, ratios), nil
}

// 额度展示沿用站点配置，客户端不能自行假定人民币或内部额度换算。
func mediaCatalogCurrency() gin.H {
	settings := operation_setting.GetGeneralSetting()
	return gin.H{
		"quota_display_type":            settings.QuotaDisplayType,
		"custom_currency_symbol":        settings.CustomCurrencySymbol,
		"custom_currency_exchange_rate": settings.CustomCurrencyExchangeRate,
		"usd_exchange_rate":             operation_setting.USDExchangeRate,
	}
}
