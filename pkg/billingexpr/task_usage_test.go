package billingexpr

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskUsageExpressionUsesFactsAndTaskQuotaConversion(t *testing.T) {
	expression := `tier("1080p", u("seconds") * (u("resolution") == "1080p" ? 0.4 : 0.2))`
	cost, trace, err := RunExprWithRequest(expression, TokenParams{}, RequestInput{Usage: map[string]any{"seconds": 10.0, "resolution": "1080p"}})
	require.NoError(t, err)
	assert.Equal(t, 4.0, cost)
	assert.Equal(t, "1080p", trace.MatchedTier)
	result, err := ComputeTieredQuotaWithRequest(&BillingSnapshot{ExprString: expression, ExprHash: ExprHashString(expression), GroupRatio: 2, QuotaPerUnit: 500000, ExprVersion: 1, TaskUsageBilling: true}, TokenParams{}, RequestInput{Usage: map[string]any{"seconds": 10.0, "resolution": "1080p"}})
	require.NoError(t, err)
	assert.Equal(t, 4_000_000, result.ActualQuotaAfterGroup)
}

func TestTaskMixedFixedAndUsageBranchesConvertSelectedUnit(t *testing.T) {
	expression := `u("seconds") <= 8 ? tier("request", fixed(2.5)) : tier("seconds", u("seconds") * 0.4)`
	snap := &BillingSnapshot{ExprString: expression, ExprHash: ExprHashString(expression), GroupRatio: 0.5, QuotaPerUnit: 500000, TaskUsageBilling: true}
	for _, tc := range []struct {
		seconds float64
		quota   int
		unit    BillingUnit
	}{
		{4, 625000, BillingUnitRequest},
		{8, 625000, BillingUnitRequest},
		{10, 1000000, BillingUnitToken},
	} {
		result, err := ComputeTieredQuotaWithRequest(snap, TokenParams{}, RequestInput{Usage: map[string]any{"seconds": tc.seconds}})
		require.NoError(t, err)
		assert.Equal(t, tc.quota, result.ActualQuotaAfterGroup)
		assert.Equal(t, tc.unit, result.BillingUnit)
	}
}
