package doubao

import (
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestEstimateBillingUsesFinalDoubaoDuration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name     string
		metadata map[string]any
		want     float64
	}{
		{name: "default", want: 5},
		{name: "duration", metadata: map[string]any{"duration": 8}, want: 8},
		{name: "frames", metadata: map[string]any{"frames": 241}, want: 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Set("task_request", relaycommon.TaskSubmitReq{Model: "doubao-video-test", Prompt: "test", Metadata: tt.metadata})
			info := &relaycommon.RelayInfo{OriginModelName: "doubao-video-test"}

			ratios := (&TaskAdaptor{}).EstimateBilling(ctx, info)
			require.Equal(t, tt.want, ratios["seconds"])
		})
	}
}
