package controller

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 普通对话与 Gemini 原生请求必须采用渠道配置，不能被全局次数或首次上游冷却提前截断。
func TestChannelRetrySynchronousRequests(t *testing.T) {
	for _, tc := range []struct {
		name              string
		gemini, exhausted bool
		status            int
		message           string
	}{
		{name: "对话429", status: 429, message: "busy"},
		{name: "对话503", status: 503, message: "busy"},
		{name: "Gemini429带等待秒数", gemini: true, status: 429, message: "请在 30 秒后重试"},
		{name: "Gemini重试耗尽后冷却", gemini: true, exhausted: true, status: 429, message: "请在 30 秒后重试"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			modelName := "gemini-2.5-flash"
			var mu sync.Mutex
			var order []int
			attempts := func() []int {
				mu.Lock()
				defer mu.Unlock()
				return append([]int(nil), order...)
			}
			handler := func(id int) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					raw, err := io.ReadAll(r.Body)
					assert.NoError(t, err)
					assert.Contains(t, string(raw), "retry-fixture")
					mu.Lock()
					order = append(order, id)
					mu.Unlock()
					w.Header().Set("Content-Type", "application/json")
					if id == 1 || tc.exhausted {
						w.WriteHeader(tc.status)
						fmt.Fprintf(w, `{"error":{"message":%q,"code":%d,"status":"RESOURCE_EXHAUSTED"}}`, tc.message, tc.status)
						return
					}
					if tc.gemini {
						fmt.Fprint(w, `{"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":1,"totalTokenCount":2}}`)
					} else {
						fmt.Fprint(w, `{"id":"chatcmpl-fixture","object":"chat.completion","model":"gemini-2.5-flash","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
					}
				}
			}
			user, token := prepareAsyncCompatRelay(t, handler(1), modelName)
			previousRetries := common.RetryTimes
			common.RetryTimes = 0
			t.Cleanup(func() { common.RetryTimes = previousRetries })
			backup := httptest.NewServer(handler(2))
			t.Cleanup(backup.Close)
			channelType := constant.ChannelTypeOpenAI
			format := relaytypes.RelayFormatOpenAI
			path := "/v1/chat/completions"
			route := path
			body := `{"model":"gemini-2.5-flash","messages":[{"role":"user","content":"retry-fixture"}]}`
			if tc.gemini {
				channelType = constant.ChannelTypeGemini
				format = relaytypes.RelayFormatGemini
				path = "/v1beta/models/" + modelName + ":generateContent"
				route = "/v1beta/models/*path"
				body = `{"contents":[{"role":"user","parts":[{"text":"retry-fixture"}]}]}`
			}
			require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = 1").Updates(map[string]any{"priority": 10, "type": channelType}).Error)
			require.NoError(t, model.DB.Model(&model.Ability{}).Where("channel_id = 1").Update("priority", 10).Error)
			channel := &model.Channel{Type: channelType, Key: "fixture", Status: common.ChannelStatusEnabled, Name: "备用", Models: modelName, Group: "default", BaseURL: &backup.URL}
			require.NoError(t, model.DB.Create(channel).Error)
			require.NoError(t, model.DB.Create(&model.Ability{Group: "default", Model: modelName, ChannelId: channel.Id, Enabled: true}).Error)
			require.NoError(t, model.UpdateOption(operation_setting.AsyncMediaRetryOption, `{"enabled":true,"max_retries":1,"same_channel_retries":2,"selected_channels_only":true,"channel_ids":[1,2],"channel_status_codes":{"1":"429,503","2":"429,503"}}`))
			// 隔离同用户同模型的冷却缓存，不让前一用例影响本次真实请求。
			middleware.ClearUpstreamErrorCooldownCache()
			t.Cleanup(middleware.ClearUpstreamErrorCooldownCache)
			require.NoError(t, model.UpdateOption(operation_setting.LocalRequestValidationOption, `{"enabled":true,"rules":[{"group":"default","enabled":true,"status_code":400}],"upstream_cooldown_seconds":30,"check_structure":true,"check_media":true,"check_generation_config":true}`))
			router := gin.New()
			router.POST(route, middleware.BodyStorageCleanup(), middleware.TokenAuth(), middleware.Distribute(), func(c *gin.Context) { Relay(c, format) })
			request := func() *httptest.ResponseRecorder {
				req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", "Bearer sk-"+token.Key)
				response := httptest.NewRecorder()
				router.ServeHTTP(response, req)
				return response
			}
			response := request()
			if tc.exhausted {
				assert.Equal(t, 429, response.Code, response.Body.String())
				assert.Equal(t, []int{1, 1, 1, 2, 2, 2}, attempts())
				response = request()
				assert.Equal(t, 429, response.Code, response.Body.String())
				assert.Contains(t, response.Body.String(), "local_rate_limit_cooldown")
				assert.Len(t, attempts(), 6, "冷却命中后不得再请求上游")
			} else {
				assert.Equal(t, 200, response.Code, response.Body.String())
				assert.Equal(t, []int{1, 1, 1, 2}, attempts())
				response = request()
				assert.Equal(t, 200, response.Code, "重试成功不应残留冷却：%s", response.Body.String())
				assert.Equal(t, []int{1, 1, 1, 2, 1, 1, 1, 2}, attempts())
			}
			var after model.User
			require.NoError(t, model.DB.First(&after, user.Id).Error)
			expectedCharge := 0
			if !tc.exhausted {
				// 替身模型固定单价为 0.04，两次成功各结算一次，不按上游尝试次数扣费。
				expectedCharge = 2 * common.QuotaFromFloat(0.04*common.QuotaPerUnit)
			}
			assert.Equal(t, user.Quota-expectedCharge, after.Quota)
		})
	}
}

// 同步和任务提交都只重试确定被拒绝的上游请求，客户端映射不能伪造可重试状态。
func TestChannelRetrySafetyAndFallback(t *testing.T) {
	prepareAsyncCompatRelay(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	require.NoError(t, model.UpdateOption(operation_setting.AsyncMediaRetryOption, `{"enabled":true,"max_retries":1,"same_channel_retries":2,"selected_channels_only":true,"channel_ids":[1],"channel_status_codes":{"1":"429,503"}}`))
	for _, tc := range []struct {
		name, body, reason                                   string
		status, mapped, channel                              int
		partial, cancelled, started, pinned, local, accepted bool
	}{
		{name: "真实429即使映射400仍重试", status: 429, mapped: 400, reason: "retry_same_channel"},
		{name: "真实400映射429不能触发", status: 400, mapped: 429, reason: "status_not_retryable"},
		{name: "未勾选渠道不触发", status: 429, channel: 2, reason: "channel_not_selected"},
		{name: "没有HTTP证据的本地429", reason: "submission_not_rejected"},
		{name: "响应未读完", status: 429, partial: true, reason: "submission_not_rejected"},
		{name: "上游已经接单", status: 503, body: `{"task_id":"accepted"}`, reason: "submission_not_rejected"},
		{name: "请求已取消", status: 429, cancelled: true, reason: "response_started_or_cancelled"},
		{name: "已经开始输出", status: 429, started: true, reason: "response_started_or_cancelled"},
		{name: "固定渠道保护", status: 429, pinned: true, reason: "pinned_channel"},
		{name: "本地拒绝标记", status: 429, local: true, reason: "non_retryable_error"},
		{name: "任务已经受理标记", status: 429, accepted: true, reason: "non_retryable_error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			c.Set("channel_id", max(1, tc.channel))
			service.AppendUsedChannel(c, max(1, tc.channel))
			require.NoError(t, service.BeginAsyncMediaAttempt(c))
			apiErr := relaytypes.NewOpenAIError(fmt.Errorf("local rejection"), relaytypes.ErrorCodeBadResponseStatusCode, 429)
			if tc.status != 0 {
				body := tc.body
				if body == "" {
					body = `{"error":{"message":"busy"}}`
				}
				if tc.partial {
					service.ObserveAsyncMediaHTTPFailure(c.Request.Context(), &http.Response{StatusCode: tc.status}, []byte(body), io.ErrUnexpectedEOF)
				} else {
					apiErr = service.RelayErrorHandler(c.Request.Context(), &http.Response{StatusCode: tc.status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, false)
				}
			}
			if tc.mapped != 0 {
				apiErr.StatusCode = tc.mapped
			}
			if tc.local || tc.accepted {
				relaytypes.ErrOptionWithSkipRetry()(apiErr)
			}
			if tc.cancelled {
				ctx, cancel := context.WithCancel(c.Request.Context())
				cancel()
				c.Request = c.Request.WithContext(ctx)
			}
			if tc.started {
				c.Writer.WriteHeaderNow()
			}
			if tc.pinned {
				c.Set("specific_channel_id", "1")
			}
			decision := service.DecideRelayRetry(c, apiErr, service.RelayRetryLimit(c))
			assert.Equal(t, tc.reason, decision.Reason)
			assert.Equal(t, tc.reason == "retry_same_channel", decision.Action == "retry")
			taskDecision := decideTaskRetry(c, &dto.TaskError{StatusCode: apiErr.StatusCode, LocalError: tc.local, NoRetry: tc.accepted}, service.RelayRetryLimit(c))
			assert.Equal(t, decision, taskDecision, "同步和媒体任务遵循同一套渠道错误码及安全边界")
		})
	}
	// 关闭新策略不会关闭普通请求原有的全局重试，但媒体仍遵守关闭即不重复提交的约定。
	require.NoError(t, model.UpdateOption(operation_setting.AsyncMediaRetryOption, operation_setting.DefaultAsyncMediaRetryJSON))
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	apiErr := relaytypes.NewOpenAIError(fmt.Errorf("busy"), relaytypes.ErrorCodeBadResponseStatusCode, 429)
	assert.Equal(t, common.RetryTimes, service.RelayRetryLimit(c))
	assert.Equal(t, "global", service.DecideRelayRetry(c, apiErr, 1).Source)
	assert.Equal(t, "retry", service.DecideRelayRetry(c, apiErr, 1).Action)
	c.Set(model.AsyncRelayContextKey, "fixture")
	assert.Zero(t, service.RelayRetryLimit(c))
	assert.Equal(t, "stop", service.DecideRelayRetry(c, apiErr, 1).Action)
}

// 原渠道重试遇到本地速率上限时，不得绕过“换渠道次数为零”的配置。
func TestChannelRetryRateLimitCannotExceedSwitchBudget(t *testing.T) {
	prepareAsyncCompatRelay(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(429) }))
	previousRedis, previousClient := common.RedisEnabled, common.RDB
	redisServer := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	common.RedisEnabled, common.RDB = true, client
	t.Cleanup(func() {
		common.RedisEnabled, common.RDB = previousRedis, previousClient
		require.NoError(t, client.Close())
	})
	previousEnabled := setting.ModelRequestRateLimitEnabled
	previousLimits := setting.ModelRequestRateLimitChannel2JSONString()
	setting.ModelRequestRateLimitEnabled = true
	t.Cleanup(func() {
		setting.ModelRequestRateLimitEnabled = previousEnabled
		require.NoError(t, setting.UpdateModelRequestRateLimitChannelByJSONString(previousLimits))
	})
	require.NoError(t, setting.UpdateModelRequestRateLimitChannelByJSONString(`{"98761":[1,0]}`))
	for _, id := range []int{98761, 98762} {
		require.NoError(t, model.DB.Create(&model.Channel{Id: id, Type: constant.ChannelTypeOpenAI, Key: "fixture", Status: common.ChannelStatusEnabled, Models: "retry-budget", Group: "default"}).Error)
		require.NoError(t, model.DB.Create(&model.Ability{ChannelId: id, Model: "retry-budget", Group: "default", Enabled: true}).Error)
	}
	require.NoError(t, model.UpdateOption(operation_setting.AsyncMediaRetryOption, `{"enabled":true,"max_retries":0,"same_channel_retries":2,"selected_channels_only":true,"channel_ids":[98761],"channel_status_codes":{"98761":"429"}}`))
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Set("channel_id", 98761)
	service.AppendUsedChannel(c, 98761)
	_, _, apiErr := service.ReserveChannelForRequest(c, &model.Channel{Id: 98761}, nil, true)
	require.Nil(t, apiErr)
	service.FinishChannelRateLimit(c, false)
	require.NoError(t, service.BeginAsyncMediaAttempt(c))
	service.ObserveAsyncMediaHTTPFailure(c.Request.Context(), &http.Response{StatusCode: 429}, []byte(`{"error":"busy"}`), nil)
	require.Equal(t, "retry_same_channel", service.DecideAsyncMediaRetry(c, 2, false).Reason)
	param := &service.RetryParam{Ctx: c, TokenGroup: "default", ModelName: "retry-budget", Retry: common.GetPointer(1)}
	selected, _, err := service.SelectAsyncMediaRetryChannel(param)
	require.NoError(t, err)
	require.NotNil(t, selected)
	require.Equal(t, 98761, selected.Id)
	selected, _, apiErr = service.ReserveChannelForRequest(c, selected, param, false)
	assert.Nil(t, selected, "禁止换渠道时不能借本地限流转发到备用渠道")
	require.NotNil(t, apiErr)
	assert.Equal(t, 429, apiErr.StatusCode)
}

// 图片编辑经过真实队列重放，原渠道重试和切换都必须保留参考图与表单参数。
func TestAsyncMediaRoutingSameThenUnselectedEdits(t *testing.T) {
	reference, err := base64.StdEncoding.DecodeString(asyncFixturePNG)
	require.NoError(t, err)
	var mu sync.Mutex
	var order []int
	handler := func(id int) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, "/v1/images/edits", r.URL.Path)
			require.NoError(t, r.ParseMultipartForm(1<<20))
			defer r.MultipartForm.RemoveAll()
			require.Equal(t, "gpt-image-2", r.FormValue("model"))
			require.Equal(t, "保留参考图和参数", r.FormValue("prompt"))
			file, _, err := r.FormFile("image")
			require.NoError(t, err)
			defer file.Close()
			data, err := io.ReadAll(file)
			require.NoError(t, err)
			require.Equal(t, reference, data)
			mu.Lock()
			order = append(order, id)
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			if id == 1 {
				w.WriteHeader(503)
				fmt.Fprint(w, `{"error":{"message":"busy","type":"server_error"}}`)
				return
			}
			fmt.Fprintf(w, `{"data":[{"b64_json":"%s"}]}`, asyncFixturePNG)
		}
	}
	user, token := prepareAsyncCompatRelay(t, handler(1), "gpt-image-2")
	backup := httptest.NewServer(handler(2))
	t.Cleanup(backup.Close)
	priority := int64(10)
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = 1").Update("priority", priority).Error)
	require.NoError(t, model.DB.Model(&model.Ability{}).Where("channel_id = 1").Update("priority", priority).Error)
	channel := &model.Channel{Type: constant.ChannelTypeOpenAI, Key: "fixture", Status: common.ChannelStatusEnabled, Name: "未勾选备用", Models: "gpt-image-2", Group: "default", BaseURL: &backup.URL}
	require.NoError(t, model.DB.Create(channel).Error)
	require.NoError(t, model.DB.Create(&model.Ability{Group: "default", Model: "gpt-image-2", ChannelId: channel.Id, Enabled: true}).Error)
	require.NoError(t, model.UpdateOption(operation_setting.AsyncMediaRetryOption, `{"enabled":true,"max_retries":1,"same_channel_retries":1,"selected_channels_only":true,"channel_ids":[1],"channel_status_codes":{"1":"503"}}`))
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	require.NoError(t, form.WriteField("model", "gpt-image-2"))
	require.NoError(t, form.WriteField("prompt", "保留参考图和参数"))
	file, err := form.CreateFormFile("image", "reference.png")
	require.NoError(t, err)
	_, err = file.Write(reference)
	require.NoError(t, err)
	require.NoError(t, form.Close())
	response := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(response)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", &body)
	c.Request.Header.Set("Content-Type", form.FormDataContentType())
	c.Set("id", user.Id)
	c.Set("token_id", token.Id)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer common.CleanupBodyStorage(c)
		Relay(c, relaytypes.RelayFormatOpenAIImage)
	}()
	select {
	case <-asyncRelayWakeup:
	case <-time.After(5 * time.Second):
		t.Fatal("图片编辑没有进入队列")
	}
	_, err = ProcessAsyncRelayTasks(context.Background(), 1)
	require.NoError(t, err)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("客户端没有收到最终响应")
	}
	mu.Lock()
	require.Equal(t, []int{1, 1, 2}, order)
	mu.Unlock()
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var task model.AsyncRelayTask
	require.NoError(t, model.DB.First(&task).Error)
	require.Equal(t, model.AsyncRelayTaskStatusSucceeded, task.Status)
	var details model.AsyncRelayRequestDetails
	require.NoError(t, common.UnmarshalJsonStr(task.RequestDetails, &details))
	reasons := make([]string, 0, len(details.RoutingEvents))
	for _, event := range details.RoutingEvents {
		reasons = append(reasons, event.Decision.Reason)
	}
	require.Contains(t, reasons, "retry_same_channel")
	require.Contains(t, reasons, "retry_status_matched")
	var after model.User
	require.NoError(t, model.DB.First(&after, user.Id).Error)
	var log model.Task
	require.NoError(t, model.DB.First(&log, task.LogID).Error)
	require.Equal(t, user.Quota-log.Quota, after.Quota, "只结算最终成功的一次图片编辑")
}
