package taskcommon

import (
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

func TestResolveVideoDurationSecondsUsesMetadataAndClamps(t *testing.T) {
	req := relaycommon.TaskSubmitReq{
		Duration: 6,
		Metadata: map[string]any{"durationSeconds": 5000},
	}

	require.Equal(t, relaycommon.MaxTaskDurationSeconds, ResolveVideoDurationSeconds(req, 1))
}

func TestResolveVideoDurationSecondsUsesSecondsString(t *testing.T) {
	req := relaycommon.TaskSubmitReq{Seconds: "12"}

	require.Equal(t, 12, ResolveVideoDurationSeconds(req, 1))
}

func TestResolveVideoDurationSecondsUsesFallbackWhenMissing(t *testing.T) {
	require.Equal(t, 4, ResolveVideoDurationSeconds(relaycommon.TaskSubmitReq{}, 4))
}

func TestVideoDurationSecondsFromFramesRoundsUp(t *testing.T) {
	require.Equal(t, 5, VideoDurationSecondsFromFrames(121, 24, 5))
	require.Equal(t, 10, VideoDurationSecondsFromFrames(241, 24, 5))
	require.Equal(t, 5, VideoDurationSecondsFromFrames(120, 24, 5))
}

func TestNormalizeVideoFramesUsesDefaultAndCaps(t *testing.T) {
	require.Equal(t, 121, NormalizeVideoFrames(0, 24, 5))
	require.Equal(t, relaycommon.MaxTaskDurationSeconds*24+1, NormalizeVideoFrames(int(^uint(0)>>1), 24, 5))
}
