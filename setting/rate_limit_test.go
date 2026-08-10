package setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModelRequestRateLimitModelJSONRoundTrip(t *testing.T) {
	previousModelJSON := ModelRequestRateLimitModel2JSONString()
	t.Cleanup(func() {
		require.NoError(t, UpdateModelRequestRateLimitModelByJSONString(previousModelJSON))
	})

	modelJSON := `{"gpt-test":[12,9],"gemini-test":[0,4]}`
	require.NoError(t, CheckModelRequestRateLimitModel(modelJSON))
	require.NoError(t, UpdateModelRequestRateLimitModelByJSONString(modelJSON))
	assert.JSONEq(t, modelJSON, ModelRequestRateLimitModel2JSONString())

	totalCount, successCount, found := GetModelRateLimit("gpt-test")
	assert.True(t, found)
	assert.Equal(t, 12, totalCount)
	assert.Equal(t, 9, successCount)
}

func TestModelRequestRateLimitGroupKeepsLegacyJSONFormat(t *testing.T) {
	previousGroupJSON := ModelRequestRateLimitGroup2JSONString()
	t.Cleanup(func() {
		require.NoError(t, UpdateModelRequestRateLimitGroupByJSONString(previousGroupJSON))
	})

	legacyJSON := `{"default":[30,20]}`
	require.NoError(t, CheckModelRequestRateLimitGroup(legacyJSON))
	require.NoError(t, UpdateModelRequestRateLimitGroupByJSONString(legacyJSON))
	assert.JSONEq(t, legacyJSON, ModelRequestRateLimitGroup2JSONString())

	totalCount, successCount, found := GetGroupRateLimit("default")
	assert.True(t, found)
	assert.Equal(t, 30, totalCount)
	assert.Equal(t, 20, successCount)
}

func TestModelRequestRateLimitRejectsInvalidValuesWithoutReplacingConfiguration(t *testing.T) {
	previousModelJSON := ModelRequestRateLimitModel2JSONString()
	t.Cleanup(func() {
		require.NoError(t, UpdateModelRequestRateLimitModelByJSONString(previousModelJSON))
	})

	require.NoError(t, UpdateModelRequestRateLimitModelByJSONString(`{"kept-model":[8,6]}`))
	invalidConfigurations := []string{
		`null`,
		`{" ":[1,1]}`,
		`{"invalid-model":[1]}`,
		`{"invalid-model":[1,2,3]}`,
		`{"invalid-model":[1,0]}`,
		`{"invalid-model":[2147483648,1]}`,
		`{"broken"`,
	}
	for _, configuration := range invalidConfigurations {
		assert.Error(t, CheckModelRequestRateLimitModel(configuration))
		assert.Error(t, UpdateModelRequestRateLimitModelByJSONString(configuration))
	}

	totalCount, successCount, found := GetModelRateLimit("kept-model")
	assert.True(t, found)
	assert.Equal(t, 8, totalCount)
	assert.Equal(t, 6, successCount)
}
