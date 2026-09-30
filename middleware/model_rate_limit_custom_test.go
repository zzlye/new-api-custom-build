package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
)

func TestRedisSuccessReservationIsAtomicAndExpires(t *testing.T) {
	redisServer, redisClient := useRateLimitMiniRedis(t)
	ctx := context.Background()
	key := "rateLimit:model-success-reservation"
	type reservationResult struct {
		reservation string
		allowed     bool
		err         error
	}

	const attempts = 16
	results := make(chan reservationResult, attempts)
	start := make(chan struct{})
	var waitGroup sync.WaitGroup
	for range attempts {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			<-start
			reservation, allowed, err := reserveRedisSuccessRequest(ctx, redisClient, key, 1, 60)
			results <- reservationResult{reservation: reservation, allowed: allowed, err: err}
		}()
	}
	close(start)
	waitGroup.Wait()
	close(results)

	allowedCount := 0
	winningReservation := ""
	for result := range results {
		require.NoError(t, result.err)
		if result.allowed {
			allowedCount++
			winningReservation = result.reservation
		}
	}
	assert.Equal(t, 1, allowedCount)
	assert.Equal(t, 60*time.Second, redisServer.TTL(key))
	require.NoError(t, rollbackRedisSuccessRequest(ctx, redisClient, key, winningReservation))
	assert.False(t, redisServer.Exists(key))
}

func useModelRequestRateLimitTestSettings(t *testing.T) {
	t.Helper()

	previousEnabled := setting.ModelRequestRateLimitEnabled
	previousDuration := setting.ModelRequestRateLimitDurationMinutes
	previousCount := setting.ModelRequestRateLimitCount
	previousSuccessCount := setting.ModelRequestRateLimitSuccessCount
	previousGroupJSON := setting.ModelRequestRateLimitGroup2JSONString()
	previousModelJSON := setting.ModelRequestRateLimitModel2JSONString()

	setting.ModelRequestRateLimitEnabled = true
	setting.ModelRequestRateLimitDurationMinutes = 1
	setting.ModelRequestRateLimitCount = 0
	setting.ModelRequestRateLimitSuccessCount = 1000
	require.NoError(t, setting.UpdateModelRequestRateLimitGroupByJSONString(`{}`))
	require.NoError(t, setting.UpdateModelRequestRateLimitModelByJSONString(`{}`))

	t.Cleanup(func() {
		setting.ModelRequestRateLimitEnabled = previousEnabled
		setting.ModelRequestRateLimitDurationMinutes = previousDuration
		setting.ModelRequestRateLimitCount = previousCount
		setting.ModelRequestRateLimitSuccessCount = previousSuccessCount
		require.NoError(t, setting.UpdateModelRequestRateLimitGroupByJSONString(previousGroupJSON))
		require.NoError(t, setting.UpdateModelRequestRateLimitModelByJSONString(previousModelJSON))
	})
}

func newModelRequestRateLimitTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(BodyStorageCleanup())
	router.Use(func(c *gin.Context) {
		userID, _ := strconv.Atoi(c.GetHeader("X-Test-User"))
		c.Set("id", userID)
		common.SetContextKey(c, constant.ContextKeyTokenGroup, c.GetHeader("X-Test-Group"))
	})
	router.Use(ModelRequestRateLimit())
	router.POST("/v1/chat/completions", func(c *gin.Context) {
		status, _ := strconv.Atoi(c.GetHeader("X-Test-Status"))
		if status == 0 {
			status = http.StatusNoContent
		}
		c.Status(status)
	})
	return router
}

func performModelRequestRateLimitRequest(router http.Handler, userID int, group, model string, status int) *httptest.ResponseRecorder {
	body, _ := common.Marshal(map[string]string{"model": model})
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(string(body)))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Test-User", strconv.Itoa(userID))
	request.Header.Set("X-Test-Group", group)
	if status != 0 {
		request.Header.Set("X-Test-Status", strconv.Itoa(status))
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

func TestResolveModelRequestRateLimitRulePriorityAndModelExtraction(t *testing.T) {
	useModelRequestRateLimitTestSettings(t)
	setting.ModelRequestRateLimitCount = 31
	setting.ModelRequestRateLimitSuccessCount = 30
	require.NoError(t, setting.UpdateModelRequestRateLimitGroupByJSONString(`{"priority-group":[21,20]}`))
	require.NoError(t, setting.UpdateModelRequestRateLimitModelByJSONString(`{"json-model":[11,10],"gemini-model":[12,10],"realtime-model":[13,10]}`))

	tests := []struct {
		name        string
		method      string
		target      string
		body        string
		contentType string
		group       string
		wantType    string
		wantName    string
		wantTotal   int
	}{
		{name: "JSON请求模型优先于分组", method: http.MethodPost, target: "/v1/chat/completions", body: `{"model":"json-model"}`, contentType: "application/json", group: "priority-group", wantType: modelRateLimitScopeModel, wantName: "json-model", wantTotal: 11},
		{name: "Gemini路径模型", method: http.MethodPost, target: "/v1beta/models/gemini-model:generateContent", body: `{}`, contentType: "application/json", group: "priority-group", wantType: modelRateLimitScopeModel, wantName: "gemini-model", wantTotal: 12},
		{name: "Realtime查询模型", method: http.MethodGet, target: "/v1/realtime?model=realtime-model", group: "priority-group", wantType: modelRateLimitScopeModel, wantName: "realtime-model", wantTotal: 13},
		{name: "未配置模型使用分组", method: http.MethodPost, target: "/v1/chat/completions", body: `{"model":"unknown-model"}`, contentType: "application/json", group: "priority-group", wantType: modelRateLimitScopeGroup, wantName: "priority-group", wantTotal: 21},
		{name: "未配置分组使用全局", method: http.MethodPost, target: "/v1/chat/completions", body: `{"model":"unknown-model"}`, contentType: "application/json", group: "other-group", wantType: modelRateLimitScopeGlobal, wantName: modelRateLimitGlobalScopeName, wantTotal: 31},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(recorder)
			context.Request = httptest.NewRequest(test.method, test.target, strings.NewReader(test.body))
			if test.contentType != "" {
				context.Request.Header.Set("Content-Type", test.contentType)
			}
			common.SetContextKey(context, constant.ContextKeyTokenGroup, test.group)

			rule := resolveModelRequestRateLimitRule(context)
			common.CleanupBodyStorage(context)
			assert.Equal(t, test.wantType, rule.scopeType)
			assert.Equal(t, test.wantName, rule.scopeName)
			assert.Equal(t, test.wantTotal, rule.totalMaxCount)
		})
	}
}

func TestMemoryModelRequestRateLimitKeepsBucketsIndependent(t *testing.T) {
	useModelRequestRateLimitTestSettings(t)
	previousRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = previousRedisEnabled })

	require.NoError(t, setting.UpdateModelRequestRateLimitModelByJSONString(`{"bucket-model-a":[1,10],"bucket-model-b":[1,10]}`))
	require.NoError(t, setting.UpdateModelRequestRateLimitGroupByJSONString(`{"bucket-group-a":[1,10],"bucket-group-b":[1,10]}`))
	router := newModelRequestRateLimitTestRouter()

	assert.Equal(t, http.StatusNoContent, performModelRequestRateLimitRequest(router, 8101, "unused-group", "bucket-model-a", 0).Code)
	assert.Equal(t, http.StatusNoContent, performModelRequestRateLimitRequest(router, 8101, "unused-group", "bucket-model-b", 0).Code)
	assert.Equal(t, http.StatusTooManyRequests, performModelRequestRateLimitRequest(router, 8101, "unused-group", "bucket-model-a", 0).Code)
	assert.Equal(t, http.StatusNoContent, performModelRequestRateLimitRequest(router, 8102, "unused-group", "bucket-model-a", 0).Code)

	assert.Equal(t, http.StatusNoContent, performModelRequestRateLimitRequest(router, 8103, "bucket-group-a", "group-fallback-model", 0).Code)
	assert.Equal(t, http.StatusNoContent, performModelRequestRateLimitRequest(router, 8103, "bucket-group-b", "group-fallback-model", 0).Code)
	assert.Equal(t, http.StatusTooManyRequests, performModelRequestRateLimitRequest(router, 8103, "bucket-group-a", "group-fallback-model", 0).Code)
}

func TestMemorySuccessLimitReservesQuotaBeforeRequestCompletes(t *testing.T) {
	useModelRequestRateLimitTestSettings(t)
	previousRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = previousRedisEnabled })
	require.NoError(t, setting.UpdateModelRequestRateLimitModelByJSONString(`{"concurrent-model":[0,1]}`))

	entered := make(chan struct{})
	release := make(chan struct{})
	router := gin.New()
	router.Use(BodyStorageCleanup())
	router.Use(func(c *gin.Context) {
		c.Set("id", 8121)
	})
	router.Use(ModelRequestRateLimit())
	router.POST("/v1/chat/completions", func(c *gin.Context) {
		entered <- struct{}{}
		<-release
		c.Status(http.StatusNoContent)
	})

	firstDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		firstDone <- performModelRequestRateLimitRequest(router, 8121, "unused-group", "concurrent-model", 0)
	}()
	<-entered

	secondDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		secondDone <- performModelRequestRateLimitRequest(router, 8121, "unused-group", "concurrent-model", 0)
	}()

	select {
	case secondResponse := <-secondDone:
		assert.Equal(t, http.StatusTooManyRequests, secondResponse.Code)
	case <-entered:
		close(release)
		<-firstDone
		<-secondDone
		t.Fatal("第二个并发请求在首个请求完成前穿透了成功请求限额")
	case <-time.After(time.Second):
		close(release)
		<-firstDone
		t.Fatal("等待第二个并发请求的限流结果超时")
	}

	close(release)
	assert.Equal(t, http.StatusNoContent, (<-firstDone).Code)
}

func TestRedisModelTotalRateLimitSetsScopedTTL(t *testing.T) {
	useModelRequestRateLimitTestSettings(t)
	redisServer, _ := useRateLimitMiniRedis(t)
	require.NoError(t, setting.UpdateModelRequestRateLimitModelByJSONString(`{"ttl-model":[1,10]}`))
	router := newModelRequestRateLimitTestRouter()

	assert.Equal(t, http.StatusNoContent, performModelRequestRateLimitRequest(router, 8151, "unused-group", "ttl-model", 0).Code)
	rule := modelRequestRateLimitRule{scopeType: modelRateLimitScopeModel, scopeName: "ttl-model"}
	totalKey := modelRequestRateLimitKey(ModelRequestRateLimitCountMark, 8151, rule)
	assert.True(t, redisServer.Exists(totalKey))
	assert.Equal(t, time.Minute, redisServer.TTL(totalKey))
}

func TestModelRequestRateLimitOnlySuccessfulResponsesConsumeSuccessQuota(t *testing.T) {
	for _, backend := range []string{"memory", "redis"} {
		t.Run(backend, func(t *testing.T) {
			useModelRequestRateLimitTestSettings(t)
			modelName := "success-quota-" + backend
			modelJSON, err := common.Marshal(map[string][2]int{modelName: {0, 1}})
			require.NoError(t, err)
			require.NoError(t, setting.UpdateModelRequestRateLimitModelByJSONString(string(modelJSON)))

			var redisServerExists func(string) bool
			if backend == "redis" {
				redisServer, _ := useRateLimitMiniRedis(t)
				redisServerExists = func(key string) bool {
					return redisServer.Exists(key)
				}
			} else {
				previousRedisEnabled := common.RedisEnabled
				common.RedisEnabled = false
				t.Cleanup(func() { common.RedisEnabled = previousRedisEnabled })
			}

			router := newModelRequestRateLimitTestRouter()
			assert.Equal(t, http.StatusInternalServerError, performModelRequestRateLimitRequest(router, 8201, "unused-group", modelName, http.StatusInternalServerError).Code)
			assert.Equal(t, http.StatusNoContent, performModelRequestRateLimitRequest(router, 8201, "unused-group", modelName, 0).Code)
			assert.Equal(t, http.StatusTooManyRequests, performModelRequestRateLimitRequest(router, 8201, "unused-group", modelName, 0).Code)

			if redisServerExists != nil {
				rule := modelRequestRateLimitRule{scopeType: modelRateLimitScopeModel, scopeName: modelName}
				assert.True(t, redisServerExists(modelRequestRateLimitKey(ModelRequestRateLimitSuccessCountMark, 8201, rule)))
				assert.False(t, redisServerExists("rateLimit:"+ModelRequestRateLimitSuccessCountMark+":8201"))
			}
		})
	}
}

func TestDisabledModelRequestRateLimitPassesThrough(t *testing.T) {
	useModelRequestRateLimitTestSettings(t)
	previousRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = previousRedisEnabled })
	setting.ModelRequestRateLimitEnabled = false
	require.NoError(t, setting.UpdateModelRequestRateLimitModelByJSONString(`{"disabled-model":[1,1]}`))
	router := newModelRequestRateLimitTestRouter()

	assert.Equal(t, http.StatusNoContent, performModelRequestRateLimitRequest(router, 8301, "unused-group", "disabled-model", 0).Code)
	assert.Equal(t, http.StatusNoContent, performModelRequestRateLimitRequest(router, 8301, "unused-group", "disabled-model", 0).Code)
}
