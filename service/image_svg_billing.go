package service

import (
	"fmt"
	"math"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"github.com/tidwall/gjson"
)

// PrepareImageSVGBilling 冻结已配置作者模型价格；不猜测未配置模型的价格。
func PrepareImageSVGBilling(c *gin.Context, info *relaycommon.RelayInfo, body []byte) *types.NewAPIError {
	info.ImageSVGUsage, info.ImageSVGPrices, info.ImageSVGRequested = nil, nil, false
	if gjson.GetBytes(body, "output_format").String() != "svg" {
		return nil
	}
	info.ImageSVGRequested = true
	prices := make(map[string]string)
	for model, ratio := range ratio_setting.GetModelRatioCopy() {
		// 旧倍率转换为当前额度单位对应的美元单价，显式表达式随后覆盖。
		input := ratio * 1000000 / common.QuotaPerUnit
		prices[model] = fmt.Sprintf("p * %g + c * %g", input, input*ratio_setting.GetCompletionRatio(model))
	}
	for model, price := range ratio_setting.GetModelPriceCopy() {
		prices[model] = fmt.Sprintf("tier(\"request\", fixed(%g))", price)
	}
	for model, expression := range billing_setting.GetBillingExprCopy() {
		if billing_setting.GetBillingMode(model) == billing_setting.BillingModeTieredExpr {
			prices[model] = expression
		}
	}
	// 用户专属价格沿用主请求已经冻结的快照，不能回退成全站价格。
	if value, exists := c.Get("user_model_pricing_snapshot"); exists {
		if rules, ok := value.(map[string]model.PricingValues); ok {
			for name, raw := range rules {
				effective := model.EffectiveUserModelPricing(name, raw)
				if effective["billing_setting.billing_mode"] == billing_setting.BillingModeTieredExpr {
					prices[name], _ = effective["billing_setting.billing_expr"].(string)
				} else if price, ok := effective["ModelPrice"].(float64); ok {
					prices[name] = fmt.Sprintf("tier(\"request\", fixed(%g))", price)
				} else if ratio, ok := effective["ModelRatio"].(float64); ok {
					completion, _ := effective["CompletionRatio"].(float64)
					input := ratio * 1000000 / common.QuotaPerUnit
					prices[name] = fmt.Sprintf("p * %g + c * %g", input, input*completion)
				} else {
					delete(prices, name)
				}
			}
		}
	}
	info.ImageSVGPrices = prices
	author := gjson.GetBytes(body, "svg.model").String()
	fixed := info.PriceData.UsePrice
	if snap := info.TieredBillingSnapshot; snap != nil {
		fixed = snap.EstimatedBillingUnit == billingexpr.BillingUnitRequest
	}
	if author != "" && !fixed {
		expression, ok := prices[ratio_setting.FormatMatchingModelName(author)]
		if !ok || expression == "" {
			return types.NewErrorWithStatusCode(fmt.Errorf("SVG 作者模型 %s 尚未配置价格", author), types.ErrorCodeModelPriceError, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
		// 显式作者在发送前追加预留，复用图片请求已有的输出估计，实际结算仍按真实用量。
		request := billingexpr.RequestInput{}
		if info.BillingRequestInput != nil {
			request = *info.BillingRequestInput
		}
		completion := 0
		if info.Request != nil {
			completion = info.Request.GetTokenCountMeta().MaxTokens
		}
		prompt := max(0, info.GetEstimatePromptTokens())
		cost, _, err := billingexpr.RunExprWithRequest(expression, billingexpr.TokenParams{P: float64(prompt), C: float64(completion), Len: float64(prompt)}, request)
		if err != nil || cost < 0 || math.IsNaN(cost) || math.IsInf(cost, 0) {
			return types.NewErrorWithStatusCode(fmt.Errorf("SVG 作者模型价格无法计算"), types.ErrorCodeModelPriceError, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
		quota, err := common.QuotaFromDecimalStrict(decimal.NewFromFloat(cost).Div(decimal.NewFromInt(1000000)).Mul(decimal.NewFromFloat(common.QuotaPerUnit)).Mul(decimal.NewFromFloat(info.PriceData.GroupRatioInfo.GroupRatio)).Add(decimal.NewFromInt(int64(info.PriceData.QuotaToPreConsume))))
		if err != nil {
			return types.NewErrorWithStatusCode(err, types.ErrorCodeModelPriceError, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
		if quota > 0 {
			info.PriceData.FreeModel = false
			if info.Billing == nil {
				return PreConsumeBilling(c, quota, info)
			}
			if err := info.Billing.Reserve(quota); err != nil {
				return types.NewErrorWithStatusCode(err, types.ErrorCodeInsufficientUserQuota, http.StatusForbidden, types.ErrOptionWithSkipRetry())
			}
			info.FinalPreConsumedQuota = info.Billing.GetPreConsumedQuota()
		}
	}
	return nil
}

// imageSVGCharge 不把作者 token 混入图片用量；失败留审计原因，不推测费用。
func imageSVGCharge(info *relaycommon.RelayInfo, fixed bool) (int, map[string]any) {
	usage := info.ImageSVGUsage
	if !info.ImageSVGRequested {
		return 0, nil
	}
	if usage == nil {
		detail := map[string]any{"model": "", "input_tokens": 0, "output_tokens": 0, "reasoning_tokens": 0, "quota": 0}
		if fixed {
			detail["included_in_fixed_price"] = true
		} else {
			detail["error"] = "上游未返回 SVG 作者用量，未估算扣费"
		}
		return 0, detail
	}
	detail := map[string]any{"model": usage.Model, "input_tokens": usage.InputTokens, "output_tokens": usage.OutputTokens, "reasoning_tokens": usage.ReasoningTokens, "quota": 0}
	if fixed {
		detail["included_in_fixed_price"] = true
		return 0, detail
	}
	for _, count := range []int{usage.InputTokens, usage.OutputTokens, usage.ReasoningTokens, usage.TotalTokens} {
		if count < 0 || count > math.MaxInt32 {
			detail["error"] = "SVG 作者用量超出有效范围"
			return 0, detail
		}
	}
	if strings.TrimSpace(usage.Model) == "" || usage.ReasoningTokens > usage.OutputTokens {
		detail["error"] = "SVG 作者用量不完整或不一致"
		return 0, detail
	}
	expression, ok := info.ImageSVGPrices[ratio_setting.FormatMatchingModelName(usage.Model)]
	if !ok {
		detail["error"] = "上游选择的 SVG 作者模型尚未配置价格，未估算扣费"
		return 0, detail
	}
	request := billingexpr.RequestInput{}
	if info.BillingRequestInput != nil {
		request = *info.BillingRequestInput
	}
	cost, trace, err := billingexpr.RunExprWithRequest(expression, billingexpr.TokenParams{P: float64(usage.InputTokens), C: float64(usage.OutputTokens), Len: float64(usage.InputTokens)}, request)
	if err != nil || cost < 0 || math.IsNaN(cost) || math.IsInf(cost, 0) {
		detail["error"] = "SVG 作者计价表达式求值失败"
		return 0, detail
	}
	quota, clamp := common.QuotaRoundChecked(cost / 1000000 * common.QuotaPerUnit * info.PriceData.GroupRatioInfo.GroupRatio)
	noteQuotaClamp(info, clamp)
	detail["quota"], detail["expression"], detail["group_ratio"], detail["matched_tier"] = quota, expression, info.PriceData.GroupRatioInfo.GroupRatio, trace.MatchedTier
	return quota, detail
}
