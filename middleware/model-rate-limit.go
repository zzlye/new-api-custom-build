package middleware

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/common/limiter"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
)

const (
	ModelRequestRateLimitCountMark        = "MRRL"
	ModelRequestRateLimitSuccessCountMark = "MRRLS"
	modelRateLimitScopeModel              = "model"
	modelRateLimitScopeGroup              = "group"
	modelRateLimitScopeGlobal             = "global"
	modelRateLimitGlobalScopeName         = "default"
)

var modelRateLimitReservationSequence uint64

var reserveRedisSuccessRequestScript = redis.NewScript(`
local key = KEYS[1]
local now = tonumber(ARGV[1])
local cutoff = tonumber(ARGV[2])
local max_count = tonumber(ARGV[3])
local ttl = tonumber(ARGV[4])
local reservation = ARGV[5]

redis.call('ZREMRANGEBYSCORE', key, '-inf', cutoff)
if redis.call('ZCARD', key) >= max_count then
  return 0
end

redis.call('ZADD', key, now, reservation)
redis.call('PEXPIRE', key, ttl)
return 1
`)

var rollbackRedisSuccessRequestScript = redis.NewScript(`
redis.call('ZREM', KEYS[1], ARGV[1])
if redis.call('ZCARD', KEYS[1]) == 0 then
  redis.call('DEL', KEYS[1])
end
return 1
`)

type modelRequestRateLimitRule struct {
	scopeType       string
	scopeName       string
	totalMaxCount   int
	successMaxCount int
}

func modelRequestRateLimitKey(mark string, userID int, rule modelRequestRateLimitRule) string {
	return fmt.Sprintf("rateLimit:%s:%d:%s:%s", mark, userID, rule.scopeType, rule.scopeName)
}

func resolveModelRequestRateLimitRule(c *gin.Context) modelRequestRateLimitRule {
	// 没有模型专属规则时不额外解析请求，保持 WebSocket 等空请求体入口的行为。
	setting.ModelRequestRateLimitMutex.RLock()
	hasModelRules := len(setting.ModelRequestRateLimitModel) > 0
	setting.ModelRequestRateLimitMutex.RUnlock()
	if hasModelRules {
		modelRequest, _, err := getModelRequest(c)
		if err == nil && modelRequest != nil && modelRequest.Model != "" {
			if totalCount, successCount, found := setting.GetModelRateLimit(modelRequest.Model); found {
				return modelRequestRateLimitRule{
					scopeType:       modelRateLimitScopeModel,
					scopeName:       modelRequest.Model,
					totalMaxCount:   totalCount,
					successMaxCount: successCount,
				}
			}
		}
	}

	group := common.GetContextKeyString(c, constant.ContextKeyTokenGroup)
	if group == "" {
		group = common.GetContextKeyString(c, constant.ContextKeyUserGroup)
	}
	if totalCount, successCount, found := setting.GetGroupRateLimit(group); found {
		return modelRequestRateLimitRule{
			scopeType:       modelRateLimitScopeGroup,
			scopeName:       group,
			totalMaxCount:   totalCount,
			successMaxCount: successCount,
		}
	}

	return modelRequestRateLimitRule{
		scopeType:       modelRateLimitScopeGlobal,
		scopeName:       modelRateLimitGlobalScopeName,
		totalMaxCount:   setting.ModelRequestRateLimitCount,
		successMaxCount: setting.ModelRequestRateLimitSuccessCount,
	}
}

// reserveRedisSuccessRequest 原子预占成功请求额度，失败响应会退回该额度。
func reserveRedisSuccessRequest(ctx context.Context, rdb *redis.Client, key string, maxCount int, duration int64) (string, bool, error) {
	if maxCount <= 0 || duration <= 0 {
		return "", true, nil
	}

	now := time.Now()
	nowMilliseconds := now.UnixMilli()
	durationMilliseconds := rateLimitCapacity(1000, duration)
	sequence := atomic.AddUint64(&modelRateLimitReservationSequence, 1)
	reservation := strconv.FormatInt(now.UnixNano(), 36) + "-" + strconv.FormatUint(sequence, 36)
	result, err := reserveRedisSuccessRequestScript.Run(
		ctx,
		rdb,
		[]string{key},
		nowMilliseconds,
		nowMilliseconds-durationMilliseconds,
		maxCount,
		durationMilliseconds,
		reservation,
	).Int()
	if err != nil {
		return "", false, err
	}
	return reservation, result == 1, nil
}

func rollbackRedisSuccessRequest(ctx context.Context, rdb *redis.Client, key, reservation string) error {
	if reservation == "" {
		return nil
	}
	return rollbackRedisSuccessRequestScript.Run(ctx, rdb, []string{key}, reservation).Err()
}

// Redis限流处理器
func redisRateLimitHandler(duration int64, rule modelRequestRateLimitRule) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.GetInt("id")
		ctx := context.Background()
		rdb := common.RDB

		// 1. 原子预占成功请求额度
		successKey := modelRequestRateLimitKey(ModelRequestRateLimitSuccessCountMark, userID, rule)
		reservation, allowed, err := reserveRedisSuccessRequest(ctx, rdb, successKey, rule.successMaxCount, duration)
		if err != nil {
			fmt.Println("检查成功请求数限制失败:", err.Error())
			abortWithOpenAiMessage(c, http.StatusInternalServerError, "rate_limit_check_failed")
			return
		}
		if !allowed {
			abortWithOpenAiMessage(c, http.StatusTooManyRequests, fmt.Sprintf("您已达到请求数限制：%d分钟内最多请求%d次", setting.ModelRequestRateLimitDurationMinutes, rule.successMaxCount))
			return
		}
		keepReservation := false
		defer func() {
			if !keepReservation {
				if rollbackErr := rollbackRedisSuccessRequest(ctx, rdb, successKey, reservation); rollbackErr != nil {
					fmt.Println("退回成功请求额度失败:", rollbackErr.Error())
				}
			}
		}()

		// 2. 检查总请求数限制并记录总请求
		if rule.totalMaxCount > 0 {
			totalKey := modelRequestRateLimitKey(ModelRequestRateLimitCountMark, userID, rule)
			tb := limiter.New(ctx, rdb)
			allowed, err = tb.Allow(
				ctx,
				totalKey,
				limiter.WithCapacity(rateLimitCapacity(rule.totalMaxCount, duration)),
				limiter.WithRate(int64(rule.totalMaxCount)),
				limiter.WithRequested(duration),
			)

			if err != nil {
				fmt.Println("检查总请求数限制失败:", err.Error())
				abortWithOpenAiMessage(c, http.StatusInternalServerError, "rate_limit_check_failed")
				return
			}
			totalKeyTTL := time.Duration(rateLimitCapacity(int(time.Second), duration))
			if totalKeyTTL <= 0 {
				totalKeyTTL = time.Second
			}
			if expireErr := rdb.Expire(ctx, totalKey, totalKeyTTL).Err(); expireErr != nil {
				fmt.Println("设置总请求限流过期时间失败:", expireErr.Error())
			}

			if !allowed {
				abortWithOpenAiMessage(c, http.StatusTooManyRequests, fmt.Sprintf("您已达到总请求数限制：%d分钟内最多请求%d次，包括失败次数，请检查您的请求是否正确", setting.ModelRequestRateLimitDurationMinutes, rule.totalMaxCount))
				return
			}
		}

		// 3. 处理请求，只有成功响应才保留预占额度
		c.Next()
		keepReservation = modelRequestSucceeded(c)
	}
}

// 内存限流处理器
func memoryRateLimitHandler(duration int64, rule modelRequestRateLimitRule) gin.HandlerFunc {
	inMemoryRateLimiter.Init(time.Duration(setting.ModelRequestRateLimitDurationMinutes) * time.Minute)

	return func(c *gin.Context) {
		userID := c.GetInt("id")
		totalKey := modelRequestRateLimitKey(ModelRequestRateLimitCountMark, userID, rule)
		successKey := modelRequestRateLimitKey(ModelRequestRateLimitSuccessCountMark, userID, rule)

		// 1. 检查总请求数限制（当totalMaxCount为0时跳过）
		if rule.totalMaxCount > 0 && !inMemoryRateLimiter.Request(totalKey, rule.totalMaxCount, duration) {
			abortWithOpenAiMessage(c, http.StatusTooManyRequests, fmt.Sprintf("您已达到总请求数限制：%d分钟内最多请求%d次，包括失败次数，请检查您的请求是否正确", setting.ModelRequestRateLimitDurationMinutes, rule.totalMaxCount))
			return
		}

		var reservation *common.RateLimitReservation
		if rule.successMaxCount > 0 {
			reservation = inMemoryRateLimiter.Reserve(successKey, rule.successMaxCount, duration)
			if reservation == nil {
				abortWithOpenAiMessage(c, http.StatusTooManyRequests, fmt.Sprintf("您已达到请求数限制：%d分钟内最多请求%d次", setting.ModelRequestRateLimitDurationMinutes, rule.successMaxCount))
				return
			}
			defer reservation.Complete(false)
		}
		c.Next()
		reservation.Complete(modelRequestSucceeded(c))
	}
}

// ModelRequestRateLimit 模型请求限流中间件
func ModelRequestRateLimit() func(c *gin.Context) {
	return func(c *gin.Context) {
		// 在每个请求时检查是否启用限流
		if !setting.ModelRequestRateLimitEnabled {
			c.Next()
			return
		}

		// 计算限流参数并按模型、分组、全局的顺序选择规则
		duration := rateLimitDurationSeconds(setting.ModelRequestRateLimitDurationMinutes)
		rule := resolveModelRequestRateLimitRule(c)

		// 根据存储类型选择并执行限流处理器
		if common.RedisEnabled {
			redisRateLimitHandler(duration, rule)(c)
		} else {
			memoryRateLimitHandler(duration, rule)(c)
		}
	}
}

func modelRequestSucceeded(c *gin.Context) bool {
	status, _ := common.GetContextKeyType[*relaycommon.StreamStatus](c, constant.ContextKeyResponseStreamStatus)
	return c.Writer.Status() < 400 && !status.ResponseFailed()
}

func rateLimitDurationSeconds(durationMinutes int) int64 {
	if durationMinutes <= 0 {
		return 0
	}
	minutes := int64(durationMinutes)
	if minutes > math.MaxInt64/60 {
		return math.MaxInt64
	}
	return minutes * 60
}

func rateLimitCapacity(count int, durationSeconds int64) int64 {
	if count <= 0 || durationSeconds <= 0 {
		return 0
	}
	c := int64(count)
	if c > math.MaxInt64/durationSeconds {
		return math.MaxInt64
	}
	return c * durationSeconds
}
