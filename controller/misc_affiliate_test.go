package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetStatusReturnsInviteTopUpCommissionRatio(t *testing.T) {
	originalRatio := common.InviteTopUpCommissionRatio
	originalOptionMap := common.OptionMap
	t.Cleanup(func() {
		common.InviteTopUpCommissionRatio = originalRatio
		common.OptionMap = originalOptionMap
	})
	common.InviteTopUpCommissionRatio = 0.15
	common.OptionMap = map[string]string{}

	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/status", nil)

	GetStatus(context)

	var payload struct {
		Success bool `json:"success"`
		Data    struct {
			InviteTopUpCommissionRatio float64 `json:"invite_top_up_commission_ratio"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &payload))
	require.True(t, payload.Success)
	assert.InDelta(t, 0.15, payload.Data.InviteTopUpCommissionRatio, 0.000001)
}
