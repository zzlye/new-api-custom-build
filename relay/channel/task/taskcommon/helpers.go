package taskcommon

import (
	"encoding/base64"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
)

// UnmarshalMetadata converts a map[string]any metadata to a typed struct via JSON round-trip.
// This replaces the repeated pattern: json.Marshal(metadata) → json.Unmarshal(bytes, &target).
func UnmarshalMetadata(metadata map[string]any, target any) error {
	if metadata == nil {
		return nil
	}
	// Prevent metadata from overriding model fields to avoid billing bypass.
	delete(metadata, "model")
	metaBytes, err := common.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("marshal metadata failed: %w", err)
	}
	if err := common.Unmarshal(metaBytes, target); err != nil {
		return fmt.Errorf("unmarshal metadata failed: %w", err)
	}
	return nil
}

// DefaultString returns val if non-empty, otherwise fallback.
func DefaultString(val, fallback string) string {
	if val == "" {
		return fallback
	}
	return val
}

// DefaultInt returns val if non-zero, otherwise fallback.
func DefaultInt(val, fallback int) int {
	if val == 0 {
		return fallback
	}
	return val
}

// ResolveVideoDurationSeconds 解析视频请求的有效计费时长。
//
// 不同视频渠道使用 duration、seconds 或 durationSeconds 三种字段，且
// metadata 可能覆盖顶层字段。统一在计费前解析并限制上限，避免绕过标准
// 请求校验的 metadata 造成超大倍率。
func ResolveVideoDurationSeconds(req relaycommon.TaskSubmitReq, fallback int) int {
	duration := req.Duration
	if duration <= 0 {
		duration = parsePositiveDuration(req.Seconds)
	}

	// 上游适配器允许 metadata 覆盖时长，计费解析必须使用同一有效值。
	for _, key := range []string{"durationSeconds", "duration", "seconds"} {
		if value, ok := req.Metadata[key]; ok {
			if parsed := parseDurationValue(value); parsed > 0 {
				duration = parsed
				break
			}
		}
	}

	return NormalizeVideoDurationSeconds(duration, fallback)
}

// NormalizeVideoDurationSeconds 为视频计费提供统一的默认值和上限处理。
func NormalizeVideoDurationSeconds(duration, fallback int) int {
	if duration <= 0 {
		duration = fallback
	}
	if duration <= 0 {
		duration = 1
	}
	if duration > relaycommon.MaxTaskDurationSeconds {
		duration = relaycommon.MaxTaskDurationSeconds
	}
	return duration
}

// VideoDurationSecondsFromFrames 将首尾均计数的帧数向上换算为计费秒数。
func VideoDurationSecondsFromFrames(frames, framesPerSecond, fallback int) int {
	if frames <= 1 || framesPerSecond <= 0 {
		return NormalizeVideoDurationSeconds(0, fallback)
	}

	duration := (int64(frames) - 1 + int64(framesPerSecond) - 1) / int64(framesPerSecond)
	if duration > int64(relaycommon.MaxTaskDurationSeconds) {
		return relaycommon.MaxTaskDurationSeconds
	}
	return NormalizeVideoDurationSeconds(int(duration), fallback)
}

// NormalizeVideoFrames 为帧数协议补齐默认帧数，并限制其对应的视频时长上限。
func NormalizeVideoFrames(frames, framesPerSecond, fallbackSeconds int) int {
	if framesPerSecond <= 0 {
		return frames
	}
	if frames <= 1 {
		return NormalizeVideoDurationSeconds(0, fallbackSeconds)*framesPerSecond + 1
	}
	maxFrames := relaycommon.MaxTaskDurationSeconds*framesPerSecond + 1
	if frames > maxFrames {
		return maxFrames
	}
	return frames
}

func parseDurationValue(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int8:
		return int(v)
	case int16:
		return int(v)
	case int32:
		return int(v)
	case int64:
		return int(v)
	case uint:
		if uint64(v) > uint64(relaycommon.MaxTaskDurationSeconds) {
			return relaycommon.MaxTaskDurationSeconds
		}
		return int(v)
	case uint8:
		return int(v)
	case uint16:
		return int(v)
	case uint32:
		return int(v)
	case uint64:
		if v > uint64(relaycommon.MaxTaskDurationSeconds) {
			return relaycommon.MaxTaskDurationSeconds
		}
		return int(v)
	case float32:
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) || v <= 0 {
			return 0
		}
		if v > float32(relaycommon.MaxTaskDurationSeconds) {
			return relaycommon.MaxTaskDurationSeconds
		}
		return int(v)
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 {
			return 0
		}
		if v > float64(relaycommon.MaxTaskDurationSeconds) {
			return relaycommon.MaxTaskDurationSeconds
		}
		return int(v)
	case string:
		return parsePositiveDuration(v)
	default:
		return 0
	}
}

func parsePositiveDuration(value string) int {
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0
	}
	return n
}

// EncodeLocalTaskID encodes an upstream operation name to a URL-safe base64 string.
// Used by Gemini/Vertex to store upstream names as task IDs.
func EncodeLocalTaskID(name string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(name))
}

// DecodeLocalTaskID decodes a base64-encoded upstream operation name.
func DecodeLocalTaskID(id string) (string, error) {
	b, err := base64.RawURLEncoding.DecodeString(id)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// BuildProxyURL constructs the video proxy URL using the public task ID.
// e.g., "https://your-server.com/v1/videos/task_xxxx/content"
func BuildProxyURL(taskID string) string {
	return fmt.Sprintf("%s/v1/videos/%s/content", system_setting.ServerAddress, taskID)
}

// Status-to-progress mapping constants for polling updates.
const (
	ProgressSubmitted  = "10%"
	ProgressQueued     = "20%"
	ProgressInProgress = "30%"
	ProgressComplete   = "100%"
)

// ---------------------------------------------------------------------------
// BaseBilling — embeddable no-op implementations for TaskAdaptor billing methods.
// Adaptors that do not need custom billing can embed this struct directly.
// ---------------------------------------------------------------------------

type BaseBilling struct{}

// EstimateBilling returns nil (no extra ratios; use base model price).
func (BaseBilling) EstimateBilling(_ *gin.Context, _ *relaycommon.RelayInfo) map[string]float64 {
	return nil
}

// AdjustBillingOnSubmit returns nil (no submit-time adjustment).
func (BaseBilling) AdjustBillingOnSubmit(_ *relaycommon.RelayInfo, _ []byte) map[string]float64 {
	return nil
}

// AdjustBillingOnComplete returns 0 (keep pre-charged amount).
func (BaseBilling) AdjustBillingOnComplete(_ *model.Task, _ *relaycommon.TaskInfo) int {
	return 0
}
