package jimeng

import (
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestEstimateBillingConvertsFinalJimengFrames(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name     string
		metadata map[string]any
		want     float64
	}{
		{name: "default frames", want: 5},
		{name: "metadata frames", metadata: map[string]any{"frames": 241}, want: 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Set("task_request", relaycommon.TaskSubmitReq{Prompt: "test", Metadata: tt.metadata})
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "jimeng_vgfm_t2v_l20"}}

			ratios := (&TaskAdaptor{}).EstimateBilling(ctx, info)
			require.Equal(t, tt.want, ratios["seconds"])
		})
	}
}
