package vertex

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	geminitask "github.com/QuantumNous/new-api/relay/channel/task/gemini"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestBuildRequestBodyMatchesEstimatedVeoDuration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("task_request", relaycommon.TaskSubmitReq{Prompt: "test", Metadata: map[string]any{"durationSeconds": 10}})
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "veo-3.1-generate-preview"}}
	adaptor := &TaskAdaptor{}

	ratios := adaptor.EstimateBilling(ctx, info)
	body, err := adaptor.BuildRequestBody(ctx, info)
	require.NoError(t, err)
	data, err := io.ReadAll(body)
	require.NoError(t, err)
	var payload geminitask.VeoRequestPayload
	require.NoError(t, common.Unmarshal(data, &payload))

	require.Equal(t, float64(payload.Parameters.DurationSeconds), ratios["seconds"])
	require.Equal(t, 10, payload.Parameters.DurationSeconds)
}
