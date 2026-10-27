package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
)

const channelRateReservationKey = "channel_rate_reservation"

// 使用 Redis 服务端时间和原子双计数，防止多用户、多实例同时穿透上限。
var reserveChannelRateScript = redis.NewScript(`
local clock = redis.call('TIME')
local now = tonumber(clock[1]) * 1000 + math.floor(tonumber(clock[2]) / 1000)
local duration = tonumber(ARGV[1])
for i = 1, 2 do
 redis.call('ZREMRANGEBYSCORE', KEYS[i], '-inf', now-duration)
 local cap = tonumber(ARGV[i+1])
 if cap > 0 and redis.call('ZCARD', KEYS[i]) >= cap then return 0 end
end
for i = 1, 2 do
 redis.call('ZADD', KEYS[i], now, ARGV[4])
 redis.call('PEXPIRE', KEYS[i], duration)
end
return 1
`)

type channelRateEntry struct {
	at      time.Time
	success bool
}

var channelRateMemory = struct {
	sync.Mutex
	entries map[int]map[string]channelRateEntry
}{entries: make(map[int]map[string]channelRateEntry)}

type channelRateReservation struct {
	id         string
	channelID  int
	client     *redis.Client
	successKey string
	once       sync.Once
}

// 失败只退还成功额度；失败和重试仍占用总请求额度。
func (r *channelRateReservation) finish(success bool) {
	if r == nil {
		return
	}
	r.once.Do(func() {
		if success {
			return
		}
		if r.client != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := r.client.ZRem(ctx, r.successKey, r.id).Err(); err != nil {
				common.SysError("退还渠道成功请求额度失败：" + err.Error())
			}
			return
		}
		channelRateMemory.Lock()
		defer channelRateMemory.Unlock()
		if entries := channelRateMemory.entries[r.channelID]; entries != nil {
			if entry, ok := entries[r.id]; ok {
				entry.success = false
				entries[r.id] = entry
			}
		}
	})
}

func FinishChannelRateLimit(c *gin.Context, success bool) {
	if value, ok := c.Get(channelRateReservationKey); ok {
		if reservation, ok := value.(*channelRateReservation); ok {
			reservation.finish(success)
		}
		c.Set(channelRateReservationKey, (*channelRateReservation)(nil))
	}
}

func reserveChannelRate(ctx context.Context, channelID, total, success int, duration time.Duration) (*channelRateReservation, bool, error) {
	reservation := &channelRateReservation{id: common.GetUUID(), channelID: channelID}
	if common.RedisEnabled {
		if common.RDB == nil {
			return nil, false, errors.New("Redis 不可用")
		}
		// 花括号保证同一渠道的两个键在 Redis 集群中落到同一槽位。
		prefix := fmt.Sprintf("channelRate:v1:{%d}:", channelID)
		reservation.client, reservation.successKey = common.RDB, prefix+"success"
		result, err := reserveChannelRateScript.Run(ctx, common.RDB, []string{prefix + "total", reservation.successKey}, duration.Milliseconds(), total, success, reservation.id).Int()
		return reservation, result == 1, err
	}
	now := time.Now()
	channelRateMemory.Lock()
	defer channelRateMemory.Unlock()
	// 顺便回收过期渠道，避免规则删除后计数长期留在内存。
	for id, entries := range channelRateMemory.entries {
		for token, entry := range entries {
			if !entry.at.After(now.Add(-duration)) {
				delete(entries, token)
			}
		}
		if len(entries) == 0 {
			delete(channelRateMemory.entries, id)
		}
	}
	entries := channelRateMemory.entries[channelID]
	successful := 0
	for _, entry := range entries {
		if entry.success {
			successful++
		}
	}
	if total > 0 && len(entries) >= total || success > 0 && successful >= success {
		return nil, false, nil
	}
	if entries == nil {
		entries = make(map[string]channelRateEntry)
		channelRateMemory.entries[channelID] = entries
	}
	entries[reservation.id] = channelRateEntry{at: now, success: true}
	return reservation, true, nil
}

// 每次真正进入转发尝试前预占额度；本地满额不触发渠道封禁，也不消耗上游重试次数。
func ReserveChannelForRequest(c *gin.Context, channel *model.Channel, retry *RetryParam, locked bool) (*model.Channel, string, *types.NewAPIError) {
	FinishChannelRateLimit(c, false)
	group := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	if !setting.ModelRequestRateLimitEnabled {
		return channel, group, nil
	}
	duration := time.Duration(max(1, min(setting.ModelRequestRateLimitDurationMinutes, 1440))) * time.Minute
	seen := make(map[int]bool)
	for {
		total, successful, found := setting.GetChannelRateLimit(channel.Id)
		if !found || total == 0 && successful == 0 {
			return channel, group, nil
		}
		reservation, allowed, err := reserveChannelRate(c.Request.Context(), channel.Id, total, successful, duration)
		if err != nil {
			common.SysError("渠道速率检查失败：" + err.Error())
			return nil, group, types.NewErrorWithStatusCode(errors.New("渠道速率检查暂时不可用"), "channel_rate_limit_unavailable", http.StatusServiceUnavailable, types.ErrOptionWithSkipRetry())
		}
		if allowed {
			c.Set(channelRateReservationKey, reservation)
			return channel, group, nil
		}
		limited := types.NewErrorWithStatusCode(errors.New("渠道请求频率已达上限，请稍后重试"), "channel_rate_limit_exceeded", http.StatusTooManyRequests, types.ErrOptionWithSkipRetry())
		RequestPolicy(c).AddEvent(PolicyEvent{ChannelID: channel.Id, Status: 429, ErrorCode: "channel_rate_limit_exceeded", Decision: PolicyDecision{Action: "skip", Reason: "channel_rate_limit", Source: "local"}})
		// 固定渠道和严格会话绑定不能借限流绕过原有选路约束。
		_, pinned, _ := GetChannelConstraints(c).ResolvedPin()
		_, specific := c.Get("specific_channel_id")
		if locked || pinned || specific || RequestPolicy(c).SessionMode == "strict" || retry == nil || seen[channel.Id] {
			return nil, group, limited
		}
		seen[channel.Id] = true
		GetChannelConstraints(c).AddFilter(dto.ChannelFilter{Kind: dto.FilterExcludedChannels, ExcludedChannelIDs: map[int]bool{channel.Id: true}})
		// 异步重试沿用原有分组和已尝试渠道约束，不能借限流重新回到失败渠道。
		if c.GetString(model.AsyncRelayContextKey) != "" && retry.GetRetry() > 0 {
			next, selected, selectErr := SelectAsyncMediaRetryChannel(retry)
			if selectErr != nil || next == nil {
				return nil, group, limited
			}
			channel, group = next, selected
		} else {
			next, selected, selectErr := SelectChannelForRequest(c, retry.ModelName, retry)
			if selectErr != nil || next == nil {
				return nil, group, limited
			}
			channel, group = next, selected
		}
	}
}
