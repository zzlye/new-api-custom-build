package service

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestImageSVGChargeSeparatesAuthorAndFixedPrice(t *testing.T) {
	info := &relaycommon.RelayInfo{ImageSVGRequested: true, ImageSVGPrices: map[string]string{"author": "p * 2 + c * 10"},
		ImageSVGUsage: &dto.ImageSVGUsage{Model: "author", InputTokens: 1000, OutputTokens: 200, ReasoningTokens: 100, TotalTokens: 1200}}
	info.PriceData.GroupRatioInfo.GroupRatio = 1.4
	quota, details := imageSVGCharge(info, false)
	require.Equal(t, common.QuotaRound(0.004*common.QuotaPerUnit*1.4), quota)
	require.Equal(t, 200, details["output_tokens"])
	require.NotContains(t, details, "error")
	quota, details = imageSVGCharge(info, true)
	require.Zero(t, quota)
	require.Equal(t, true, details["included_in_fixed_price"])
	info.ImageSVGUsage.Model = "not-configured"
	quota, details = imageSVGCharge(info, false)
	require.Zero(t, quota)
	require.Contains(t, details, "error")
	info.ImageSVGUsage.Model = "author"
	info.ImageSVGUsage.InputTokens = -1
	quota, details = imageSVGCharge(info, false)
	require.Zero(t, quota)
	require.Contains(t, details, "error")
}

func TestImageSVGPriceSnapshotUsesUserPriceAndKeepsMissingUsageVisible(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("user_model_pricing_snapshot", map[string]model.PricingValues{
		"author": {"billing_setting.billing_mode": "tiered_expr", "billing_setting.billing_expr": "p * 3 + c * 12"},
	})
	info := &relaycommon.RelayInfo{}
	require.Nil(t, PrepareImageSVGBilling(c, info, []byte(`{"output_format":"svg","svg":{"model":"author"}}`)))
	require.Equal(t, "p * 3 + c * 12", info.ImageSVGPrices["author"])
	quota, detail := imageSVGCharge(info, false)
	require.Zero(t, quota)
	require.Contains(t, detail, "error")
	info.ImageSVGUsage = &dto.ImageSVGUsage{Model: "author", InputTokens: 1000, OutputTokens: 200}
	info.PriceData.GroupRatioInfo.GroupRatio = 1
	quota, detail = imageSVGCharge(info, false)
	require.NotContains(t, detail, "error")
	require.Equal(t, common.QuotaRound(0.0054*common.QuotaPerUnit), quota)
	require.Nil(t, PrepareImageSVGBilling(c, info, []byte(`{"output_format":"png"}`)))
	require.Nil(t, info.ImageSVGUsage)
	require.Nil(t, info.ImageSVGPrices)
	require.False(t, info.ImageSVGRequested)
}
