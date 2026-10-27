package service

import (
	"context"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupChannelRateTest(t *testing.T, redisMode bool) {
	t.Helper()
	enabled, client, rateEnabled := common.RedisEnabled, common.RDB, setting.ModelRequestRateLimitEnabled
	previous := setting.ModelRequestRateLimitChannel2JSONString()
	duration := setting.ModelRequestRateLimitDurationMinutes
	common.RedisEnabled = redisMode
	setting.ModelRequestRateLimitEnabled = true
	setting.ModelRequestRateLimitDurationMinutes = 1
	require.NoError(t, setting.UpdateModelRequestRateLimitChannelByJSONString(`{}`))
	channelRateMemory.Lock()
	previousEntries := channelRateMemory.entries
	channelRateMemory.entries = make(map[int]map[string]channelRateEntry)
	channelRateMemory.Unlock()
	if redisMode {
		server := miniredis.RunT(t)
		common.RDB = redis.NewClient(&redis.Options{Addr: server.Addr()})
		testClient := common.RDB
		t.Cleanup(func() { _ = testClient.Close() })
	}
	t.Cleanup(func() {
		channelRateMemory.Lock()
		channelRateMemory.entries = previousEntries
		channelRateMemory.Unlock()
		common.RedisEnabled, common.RDB, setting.ModelRequestRateLimitEnabled = enabled, client, rateEnabled
		setting.ModelRequestRateLimitDurationMinutes = duration
		require.NoError(t, setting.UpdateModelRequestRateLimitChannelByJSONString(previous))
	})
}

func TestChannelRateLimitSharedAttemptsAndFailureRefund(t *testing.T) {
	for _, mode := range []bool{false, true} {
		t.Run(map[bool]string{false: "memory", true: "redis"}[mode], func(t *testing.T) {
			setupChannelRateTest(t, mode)
			ctx := context.Background()
			first, ok, err := reserveChannelRate(ctx, 175, 2, 1, time.Minute)
			require.NoError(t, err)
			require.True(t, ok)
			_, ok, err = reserveChannelRate(ctx, 175, 2, 1, time.Minute)
			require.NoError(t, err)
			assert.False(t, ok, "在途请求预占成功额度，不能并发穿透")
			first.finish(false)
			second, ok, err := reserveChannelRate(ctx, 175, 2, 1, time.Minute)
			require.NoError(t, err)
			require.True(t, ok)
			second.finish(false)
			_, ok, err = reserveChannelRate(ctx, 175, 2, 1, time.Minute)
			require.NoError(t, err)
			assert.False(t, ok, "失败与重试仍占总次数")
			_, ok, err = reserveChannelRate(ctx, 176, 2, 1, time.Minute)
			require.NoError(t, err)
			assert.True(t, ok, "其他渠道独立计数")
		})
	}
}

func TestChannelRateLimitConcurrentRequestsAndExpiry(t *testing.T) {
	for _, mode := range []bool{false, true} {
		t.Run(map[bool]string{false: "memory", true: "redis"}[mode], func(t *testing.T) {
			setupChannelRateTest(t, mode)
			var admitted atomic.Int32
			var wg sync.WaitGroup
			for range 8 {
				wg.Go(func() {
					_, ok, err := reserveChannelRate(context.Background(), 175, 2, 0, time.Minute)
					assert.NoError(t, err)
					if ok {
						admitted.Add(1)
					}
				})
			}
			wg.Wait()
			assert.EqualValues(t, 2, admitted.Load())
			// 直接推进历史时间，不依赖睡眠或调度速度。
			if mode {
				key := "channelRate:v1:{175}:total"
				members, err := common.RDB.ZRange(context.Background(), key, 0, -1).Result()
				require.NoError(t, err)
				for _, member := range members {
					require.NoError(t, common.RDB.ZAdd(context.Background(), key, &redis.Z{Score: 1, Member: member}).Err())
					require.NoError(t, common.RDB.ZAdd(context.Background(), "channelRate:v1:{175}:success", &redis.Z{Score: 1, Member: member}).Err())
				}
			} else {
				channelRateMemory.Lock()
				for token, entry := range channelRateMemory.entries[175] {
					entry.at = time.Unix(0, 0)
					channelRateMemory.entries[175][token] = entry
				}
				channelRateMemory.Unlock()
			}
			_, ok, err := reserveChannelRate(context.Background(), 175, 2, 0, time.Minute)
			require.NoError(t, err)
			assert.True(t, ok)
		})
	}
}

func TestChannelRateLimitRoutingAndConfiguration(t *testing.T) {
	db := setupChannelSelectAutoGroupsTest(t)
	setupChannelRateTest(t, false)
	createChannelSelectAutoGroupsChannel(t, db, 175, "default", "test-model")
	createChannelSelectAutoGroupsChannel(t, db, 176, "default", "test-model")
	model.InitChannelCache()
	require.NoError(t, setting.UpdateModelRequestRateLimitChannelByJSONString(`{"175":[1,0],"176":[1,0]}`))
	assert.JSONEq(t, `{"175":[1,0],"176":[1,0]}`, setting.ModelRequestRateLimitChannel2JSONString())
	for _, bad := range []string{`{"0":[1,0]}`, `{"0175":[1,0]}`, `{"name":[1,0]}`, `{"175":[-1,0]}`, `{"175":[1]}`, `null`} {
		assert.Error(t, setting.UpdateModelRequestRateLimitChannelByJSONString(bad))
	}
	newRequest := func(user int) *gin.Context {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
		c.Set("id", user)
		common.SetContextKey(c, constant.ContextKeyUsingGroup, "default")
		return c
	}
	first := newRequest(1)
	_, _, apiErr := ReserveChannelForRequest(first, &model.Channel{Id: 175}, nil, true)
	require.Nil(t, apiErr)
	FinishChannelRateLimit(first, true)
	second := newRequest(2)
	retry := &RetryParam{Ctx: second, TokenGroup: "default", ModelName: "test-model", RequestPath: "/v1/chat/completions"}
	selected, _, apiErr := ReserveChannelForRequest(second, &model.Channel{Id: 175}, retry, false)
	require.Nil(t, apiErr)
	assert.Equal(t, 176, selected.Id)
	assert.Equal(t, 0, retry.GetRetry())
	FinishChannelRateLimit(second, true)
	pinned := newRequest(3)
	GetChannelConstraints(pinned).AddPin(dto.ChannelPin{ChannelId: 175})
	_, _, apiErr = ReserveChannelForRequest(pinned, &model.Channel{Id: 175}, retry, false)
	require.NotNil(t, apiErr)
	assert.Equal(t, 429, apiErr.StatusCode)
	exhausted := newRequest(4)
	retry.Ctx = exhausted
	_, _, apiErr = ReserveChannelForRequest(exhausted, &model.Channel{Id: 175}, retry, false)
	require.NotNil(t, apiErr)
	assert.Equal(t, 429, apiErr.StatusCode)
	oldDisable := common.AutomaticDisableChannelEnabled
	common.AutomaticDisableChannelEnabled = true
	assert.False(t, ShouldDisableChannel(apiErr), "本地满额不能自动封禁渠道")
	common.AutomaticDisableChannelEnabled = oldDisable
	// 删除规则后立即恢复放行，不要求等旧窗口结束。
	require.NoError(t, setting.UpdateModelRequestRateLimitChannelByJSONString(`{}`))
	_, _, apiErr = ReserveChannelForRequest(exhausted, &model.Channel{Id: 175}, nil, true)
	assert.Nil(t, apiErr)
	require.NoError(t, setting.UpdateModelRequestRateLimitChannelByJSONString(`{"175":[1,0]}`))
	setting.ModelRequestRateLimitEnabled = false
	_, _, apiErr = ReserveChannelForRequest(exhausted, &model.Channel{Id: 175}, nil, true)
	assert.Nil(t, apiErr)
}

func TestChannelRateLimitRedisFailureDoesNotForward(t *testing.T) {
	setupChannelRateTest(t, true)
	require.NoError(t, setting.UpdateModelRequestRateLimitChannelByJSONString(`{"175":[2,0]}`))
	require.NoError(t, common.RDB.Close())
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	_, _, err := ReserveChannelForRequest(c, &model.Channel{Id: 175}, nil, true)
	require.NotNil(t, err)
	assert.Equal(t, 503, err.StatusCode)
}
