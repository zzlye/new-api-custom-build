package helper

import (
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/gin-gonic/gin"
)

// 同一次请求只读取一次用户价格，渠道切换期间继续使用提交时的配置。
func ResolveUserModelPricing(c *gin.Context, info *relaycommon.RelayInfo, names ...string) (model.PricingValues, error) {
	if info == nil || info.UserId <= 0 {
		return nil, nil
	}
	const key = "user_model_pricing_snapshot"
	var value any
	var exists bool
	if c != nil {
		value, exists = c.Get(key)
	}
	if !exists {
		rules, err := model.UserModelPricingFromCache(info.UserId)
		if err != nil {
			return nil, err
		}
		value = rules
		if c != nil {
			c.Set(key, value)
		}
	}
	rules, _ := value.(map[string]model.PricingValues)
	for _, name := range names {
		if pricing, exists := rules[name]; exists {
			return model.EffectiveUserModelPricing(name, pricing), nil
		}
	}
	return nil, nil
}

// 用户基础价优先于全局插件价，未配置的用户模型仍沿用原来的路由定价。
func ResolveUserTaskBilling(c *gin.Context, info *relaycommon.RelayInfo, pluginKey string) (string, string, bool, error) {
	pricing, err := ResolveUserModelPricing(c, info, info.OriginModelName, info.GetUpstreamModelName())
	if err != nil {
		return "", "", false, err
	}
	if pricing == nil {
		expr, exists := billing_setting.ResolveTaskBillingExpr(pluginKey, info.OriginModelName, info.GetUpstreamModelName())
		return billing_setting.GetBillingMode(info.OriginModelName), expr, exists, nil
	}
	mode, _ := pricing["billing_setting.billing_mode"].(string)
	variants, _ := pricing[billing_setting.PluginBillingExprOption].(map[string]any)
	if expr, ok := variants[pluginKey].(string); ok {
		return mode, expr, true, nil
	}
	if mode == billing_setting.BillingModeTieredExpr {
		expr, ok := pricing["billing_setting.billing_expr"].(string)
		return mode, expr, ok, nil
	}
	return mode, "", false, nil
}
