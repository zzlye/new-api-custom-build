package service

import (
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestTaskBillingContextPriceDataPerSecondSkipsDurationMultiplier(t *testing.T) {
	priceData := taskBillingContextPriceData(&model.TaskBillingContext{
		PerSecondBilling: true,
		OtherRatios: map[string]float64{
			"seconds":    8,
			"resolution": 1.5,
		},
	})

	require.NotNil(t, priceData)
	assert.True(t, priceData.PerSecondBilling)
	assert.Equal(t, 1.5, priceData.OtherRatioMultiplier())
	assert.Equal(t, map[string]float64{"seconds": 8, "resolution": 1.5}, priceData.OtherRatios())
}

func TestTaskConsumptionLogContentPerSecondOverridesPricePatch(t *testing.T) {
	savedPatches := append([]string(nil), constant.TaskPricePatches...)
	t.Cleanup(func() { constant.TaskPricePatches = savedPatches })
	constant.TaskPricePatches = []string{"video-per-second"}

	info := &relaycommon.RelayInfo{OriginModelName: "video-per-second", TaskRelayInfo: &relaycommon.TaskRelayInfo{Action: constant.TaskActionGenerate}}
	info.PriceData.PerSecondBilling = true
	info.PriceData.AddOtherRatio("seconds", 1)
	info.PriceData.AddOtherRatio("resolution", 1.5)

	content := taskConsumptionLogContent(info)
	assert.Contains(t, content, "按秒计费")
	assert.NotContains(t, content, "按次计费")
	assert.Contains(t, content, "seconds: 1.00")
	assert.Contains(t, content, "resolution: 1.50")
}
