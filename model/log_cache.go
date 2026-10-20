package model

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/tidwall/gjson"
)

type LogCacheUsage struct {
	InputTokens int64   `json:"input_tokens"`
	ReadTokens  int64   `json:"read_tokens"`
	WriteTokens int64   `json:"write_tokens"`
	HitRate     float64 `json:"hit_rate"`
}

type ModelCacheStats struct {
	ModelName string `json:"model_name"`
	Requests  int64  `json:"requests"`
	LogCacheUsage
}

// JSON 数字在网页端能精确表示的最大整数，与钱包额度及计费上限无关。
const maxCacheStatTokens int64 = 1<<53 - 1

func CanViewCacheStats(role int) bool {
	return role == common.RoleRootUser || (role >= common.RoleCommonUser && operation_setting.GetGeneralSetting().CacheStatsPublic)
}

// 日志中的用量必须是有限非负整数；坏数据不能把百分比变成负数或无穷。
func logCacheToken(value gjson.Result) int64 {
	if value.Type != gjson.Number || value.Num < 0 || value.Num > float64(maxCacheStatTokens) || math.Trunc(value.Num) != value.Num {
		return 0
	}
	return value.Int()
}

// CacheUsageForLog 只规范化展示口径，不参与扣款；逐条日志和模型汇总共用同一公式。
func CacheUsageForLog(log *Log) *LogCacheUsage {
	if log == nil || log.Type != LogTypeConsume || (log.Other != "" && !gjson.Valid(log.Other)) {
		return nil
	}
	other := gjson.Parse(log.Other)
	path := strings.ToLower(other.Get("request_path").String())
	unit := other.Get("billing_unit").String()
	// 根据真实请求/任务事实排除媒体生成，不能把带参考图的文字对话误认为生图。
	if strings.HasPrefix(log.RequestId, "async_") || other.Get("is_task").Bool() || other.Get("task_sync").Bool() || other.Get("image_count").Exists() ||
		unit == "image" || unit == "video" || unit == "second" ||
		strings.Contains(path, "/images") || strings.Contains(path, "/videos") || strings.HasPrefix(path, "/mj/") {
		return nil
	}
	read := logCacheToken(other.Get("cache_tokens"))
	write := max(logCacheToken(other.Get("cache_write_tokens")), logCacheToken(other.Get("cache_creation_tokens")),
		logCacheToken(other.Get("cache_creation_tokens_5m"))+logCacheToken(other.Get("cache_creation_tokens_1h")))
	input := int64(max(log.PromptTokens, 0))
	if total := logCacheToken(other.Get("input_tokens_total")); total > 0 {
		input = total
	} else if total := logCacheToken(other.Get("billing_tokens.len")); total > 0 {
		input = total
	} else {
		semantic := other.Get("usage_semantic").String()
		// Claude 的原始 input 不含缓存；历史转换记录带分档写入时同样采用此口径。
		exclusive := semantic == "anthropic" || (semantic == "" && (other.Get("claude").Bool() ||
			other.Get("cache_creation_tokens_5m").Int() > 0 || other.Get("cache_creation_tokens_1h").Int() > 0))
		if includes := other.Get("cache_input_includes_cache"); includes.Type == gjson.True || includes.Type == gjson.False {
			exclusive = !includes.Bool()
		}
		if exclusive {
			input += read + write
		}
	}
	if input <= 0 || input > maxCacheStatTokens || read+write > input {
		return nil
	}
	return &LogCacheUsage{InputTokens: input, ReadTokens: read, WriteTokens: write, HitRate: float64(read) / float64(input) * 100}
}

func AttachLogCacheUsage(logs []*Log, role int) {
	visible := CanViewCacheStats(role)
	for _, log := range logs {
		log.CacheUsage = nil
		if visible {
			log.CacheUsage = CacheUsageForLog(log)
		}
	}
}

// 按索引限定时间与归属，流式读取所需字段，内存只保留模型汇总而不是完整日志。
// 直接读取日志保证历史数据可用，且清理日志后不会遗留一份独立缓存底表。
func GetModelCacheStats(ctx context.Context, start, end int64, userID int, username string) ([]ModelCacheStats, error) {
	query := LOG_DB.WithContext(ctx).Model(&Log{}).
		Select("model_name, prompt_tokens, other, request_id").
		Where("type = ? AND created_at >= ? AND created_at <= ?", LogTypeConsume, start, end)
	if userID > 0 {
		query = query.Where("user_id = ?", userID)
	} else if username != "" {
		query = query.Where("username = ?", username)
	}
	rows, err := query.Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byModel := make(map[string]*ModelCacheStats)
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var name, other, requestID sql.NullString
		var prompt sql.NullInt64
		// 兼容升级前允许 NULL 的日志列，不因一条旧记录中断整个查询。
		if err := rows.Scan(&name, &prompt, &other, &requestID); err != nil {
			return nil, err
		}
		log := Log{Type: LogTypeConsume, ModelName: name.String, PromptTokens: int(prompt.Int64), Other: other.String, RequestId: requestID.String}
		usage := CacheUsageForLog(&log)
		if usage == nil {
			continue
		}
		item := byModel[log.ModelName]
		if item == nil {
			item = &ModelCacheStats{ModelName: log.ModelName}
			byModel[log.ModelName] = item
		}
		if item.InputTokens > maxCacheStatTokens-usage.InputTokens {
			return nil, errors.New("缓存统计数值过大，请缩小时间范围")
		}
		item.Requests++
		item.InputTokens += usage.InputTokens
		item.ReadTokens += usage.ReadTokens
		item.WriteTokens += usage.WriteTokens
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	result := make([]ModelCacheStats, 0, len(byModel))
	for _, item := range byModel {
		item.HitRate = float64(item.ReadTokens) / float64(item.InputTokens) * 100
		result = append(result, *item)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].ReadTokens == result[j].ReadTokens {
			return result[i].ModelName < result[j].ModelName
		}
		return result[i].ReadTokens > result[j].ReadTokens
	})
	return result, nil
}
