package hailuo

import (
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestEstimateBillingUsesFinalHailuoDuration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name     string
		metadata map[string]any
		want     float64
	}{
		{name: "default", want: 6},
		{name: "metadata override", metadata: map[string]any{"duration": 10}, want: 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Set("task_request", relaycommon.TaskSubmitReq{Prompt: "test", Metadata: tt.metadata})
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "MiniMax-Hailuo-2.3"}}

			ratios := (&TaskAdaptor{}).EstimateBilling(ctx, info)
			require.Equal(t, tt.want, ratios["seconds"])
		})
	}
}
