package controller

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// prepareAsyncCompatRelay 让兼容性测试经过真实队列、鉴权、渠道选择和结算，但只访问本地替身。
func prepareAsyncCompatRelay(t *testing.T, handler http.Handler, models ...string) (*model.User, *model.Token) {
	t.Helper()
	prepareAsyncMediaController(t)
	select {
	case <-asyncRelayWakeup:
	default:
	}
	upstream := httptest.NewServer(handler)
	t.Cleanup(upstream.Close)
	service.InitHttpClient()
	oldPrices, err := common.Marshal(ratio_setting.GetModelPriceCopy())
	require.NoError(t, err)
	modelName := "dall-e-3"
	if len(models) > 0 {
		modelName = models[0]
	}
	prices, err := common.Marshal(map[string]float64{modelName: 0.04})
	require.NoError(t, err)
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(string(prices)))
	t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(string(oldPrices))) })
	user := &model.User{Id: 81, Username: "compat-relay-fixture", Status: common.UserStatusEnabled, Role: common.RoleCommonUser, Group: "default", Quota: 100000000, Setting: `{"billing_preference":"wallet_only"}`}
	require.NoError(t, model.DB.Create(user).Error)
	token := &model.Token{UserId: user.Id, Key: "compatFixtureToken", Status: common.TokenStatusEnabled, Name: "兼容性测试", ExpiredTime: -1, UnlimitedQuota: true}
	require.NoError(t, model.DB.Create(token).Error)
	channel := &model.Channel{Type: constant.ChannelTypeOpenAI, Key: "fixture-upstream", Status: common.ChannelStatusEnabled, Name: "兼容性替身", Models: modelName, Group: "default", BaseURL: &upstream.URL}
	require.NoError(t, model.DB.Create(channel).Error)
	require.NoError(t, model.DB.Create(&model.Ability{Group: "default", Model: modelName, ChannelId: channel.Id, Enabled: true}).Error)
	return user, token
}

// beginAsyncCompatRequest 模拟普通客户端等待原接口响应，不让客户端参与任务轮询。
func beginAsyncCompatRequest(t *testing.T, user *model.User, token *model.Token, path, body string, formats ...relaytypes.RelayFormat) (*httptest.ResponseRecorder, <-chan struct{}, context.CancelFunc) {
	t.Helper()
	recording := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recording)
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	ctx, cancel := context.WithCancel(request.Context())
	c.Request = request.WithContext(ctx)
	c.Set("id", user.Id)
	c.Set("token_id", token.Id)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer common.CleanupBodyStorage(c)
		if len(formats) > 0 && formats[0] == relaytypes.RelayFormatTask {
			RelayTask(c)
		} else {
			Relay(c, relaytypes.RelayFormatOpenAIImage)
		}
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("客户端处理器没有结束")
		}
	})
	select {
	case <-asyncRelayWakeup:
	case <-time.After(5 * time.Second):
		t.Fatal("请求没有进入后台队列")
	}
	return recording, done, cancel
}

func TestAsyncRelayDefaultPreservesImagesAPIResponse(t *testing.T) {
	user, token := prepareAsyncCompatRelay(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/images/generations", r.URL.Path)
		assert.Equal(t, "Bearer fixture-upstream", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, err := fmt.Fprintf(w, `{"created":1,"data":[{"b64_json":"%s"}]}`, asyncFixturePNG)
		assert.NoError(t, err)
	}))
	response, done, _ := beginAsyncCompatRequest(t, user, token, "/v1/images/generations", `{"model":"dall-e-3","prompt":"一只猫","n":1,"size":"1024x1024"}`)
	processed, err := ProcessAsyncRelayTasks(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, 1, processed)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("普通客户端没有收到图片结果")
	}
	require.Equal(t, http.StatusOK, response.Code)
	assert.Contains(t, response.Header().Get("Content-Type"), "application/json")
	assert.JSONEq(t, fmt.Sprintf(`{"created":1,"data":[{"b64_json":"%s"}]}`, asyncFixturePNG), response.Body.String())
	assert.NotContains(t, response.Body.String(), "poll_url")
	var task model.AsyncRelayTask
	require.NoError(t, model.DB.First(&task).Error)
	assert.Equal(t, model.AsyncRelayTaskStatusSucceeded, task.Status)
	assert.NotZero(t, task.LogID)
}

func TestAsyncRelayDefaultPreservesUpstreamAuthenticationError(t *testing.T) {
	user, token := prepareAsyncCompatRelay(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer fixture-upstream", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, err := w.Write([]byte(`{"error":{"message":"Invalid token","type":"authentication_error","code":"invalid_api_key"}}`))
		assert.NoError(t, err)
	}))
	response, done, _ := beginAsyncCompatRequest(t, user, token, "/v1/images/generations", `{"model":"dall-e-3","prompt":"一只猫","n":1,"size":"1024x1024"}`)
	processed, err := ProcessAsyncRelayTasks(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, 1, processed)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("普通客户端没有收到上游错误")
	}
	require.Equal(t, http.StatusUnauthorized, response.Code)
	assert.Contains(t, response.Body.String(), "Invalid token")
	assert.NotContains(t, response.Body.String(), "poll_url")
	var task model.AsyncRelayTask
	require.NoError(t, model.DB.First(&task).Error)
	assert.Equal(t, model.AsyncRelayTaskStatusFailed, task.Status)
	assert.Contains(t, task.Error, "Invalid token")
}

// 普通请求断开后仍只生成一次，后台完成日志和结算，不依赖前端保存任务编号。
func TestAsyncRelayDefaultContinuesAfterClientDisconnect(t *testing.T) {
	var requests atomic.Int32
	user, token := prepareAsyncCompatRelay(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, err := fmt.Fprintf(w, `{"created":1,"data":[{"b64_json":"%s"}]}`, asyncFixturePNG)
		assert.NoError(t, err)
	}))
	_, done, cancel := beginAsyncCompatRequest(t, user, token, "/v1/images/generations", `{"model":"dall-e-3","prompt":"一只猫","n":1,"size":"1024x1024"}`)
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("断开客户端后处理器没有退出")
	}
	processed, err := ProcessAsyncRelayTasks(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, 1, processed)
	var task model.AsyncRelayTask
	require.NoError(t, model.DB.First(&task).Error)
	require.Equal(t, model.AsyncRelayTaskStatusSucceeded, task.Status, task.Error)
	assert.Equal(t, int32(1), requests.Load())
	var log model.Task
	require.NoError(t, model.DB.First(&log, task.LogID).Error)
	var after model.User
	require.NoError(t, model.DB.First(&after, user.Id).Error)
	assert.Greater(t, log.Quota, 0)
	assert.Equal(t, user.Quota-log.Quota, after.Quota)
}

// 先读取预览片段再允许上游完成，防止把原有流式响应意外缓冲成整包返回。
func TestAsyncRelayDefaultForwardsStreamBeforeGenerationFinishes(t *testing.T) {
	previousTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = previousTimeout })
	release := make(chan struct{})
	var releaseOnce sync.Once
	user, token := prepareAsyncCompatRelay(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Stream bool `json:"stream"`
		}
		assert.NoError(t, common.DecodeJson(r.Body, &body))
		assert.True(t, body.Stream)
		w.Header().Set("Content-Type", "text/event-stream")
		_, err := fmt.Fprintf(w, "event: image_generation.partial_image\ndata: {\"type\":\"image_generation.partial_image\",\"partial_image_index\":0,\"b64_json\":\"%s\"}\n\n", asyncFixturePNG)
		assert.NoError(t, err)
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		_, err = fmt.Fprintf(w, "event: image_generation.completed\ndata: {\"type\":\"image_generation.completed\",\"b64_json\":\"%s\"}\n\ndata: [DONE]\n\n", asyncFixturePNG)
		assert.NoError(t, err)
	}), "gpt-image-1")
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	router := gin.New()
	router.POST("/v1/images/generations", func(c *gin.Context) {
		defer common.CleanupBodyStorage(c)
		c.Set("id", user.Id)
		c.Set("token_id", token.Id)
		Relay(c, relaytypes.RelayFormatOpenAIImage)
	})
	gateway := httptest.NewServer(router)
	t.Cleanup(gateway.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, gateway.URL+"/v1/images/generations", strings.NewReader(`{"model":"gpt-image-1","prompt":"一只猫","stream":true,"n":1}`))
	require.NoError(t, err)
	request.Header.Set("Content-Type", "application/json")
	type clientResponse struct {
		response *http.Response
		err      error
	}
	received := make(chan clientResponse, 1)
	go func() { response, err := http.DefaultClient.Do(request); received <- clientResponse{response, err} }()
	select {
	case <-asyncRelayWakeup:
	case <-ctx.Done():
		t.Fatal("流式请求没有入队")
	}
	workerDone := make(chan error, 1)
	go func() {
		_, err := ProcessAsyncRelayTasks(context.Background(), 1)
		workerDone <- err
		close(workerDone)
	}()
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(release) })
		select {
		case <-workerDone:
		case <-time.After(5 * time.Second):
			t.Error("流式后台任务没有结束")
		}
	})
	var result clientResponse
	select {
	case result = <-received:
	case <-ctx.Done():
		t.Fatal("客户端没有收到流式响应头")
	}
	require.NoError(t, result.err)
	defer result.response.Body.Close()
	if result.response.StatusCode != http.StatusOK {
		details, _ := io.ReadAll(result.response.Body)
		t.Fatalf("流式返回 %d：%s", result.response.StatusCode, details)
	}
	require.Contains(t, result.response.Header.Get("Content-Type"), "text/event-stream")
	reader := bufio.NewReader(result.response.Body)
	firstEvent, err := reader.ReadString('\n')
	require.NoError(t, err)
	require.Contains(t, firstEvent, "image_generation.partial_image")
	releaseOnce.Do(func() { close(release) })
	rest, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Contains(t, string(rest), "image_generation.completed")
	require.NoError(t, <-workerDone)
	var task model.AsyncRelayTask
	require.NoError(t, model.DB.First(&task).Error)
	assert.Equal(t, model.AsyncRelayTaskStatusSucceeded, task.Status, task.Error)
}

// 原生视频接口保留原有编号和查询结果，后台归档不替换已有视频协议。
func TestAsyncRelayDefaultPreservesNativeVideoSubmissionAndFetch(t *testing.T) {
	user, token := prepareAsyncCompatRelay(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/videos", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"id":"video-fixture","object":"video","status":"queued","model":"sora-2","created_at":1,"seconds":"4","size":"720x1280"}`))
		assert.NoError(t, err)
	}), "sora-2")
	response, done, _ := beginAsyncCompatRequest(t, user, token, "/v1/videos", `{"model":"sora-2","prompt":"一只猫","seconds":"4","size":"720x1280"}`, relaytypes.RelayFormatTask)
	processed, err := ProcessAsyncRelayTasks(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, 1, processed)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("原生视频提交没有返回")
	}
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var submitted struct {
		ID     string `json:"id"`
		Object string `json:"object"`
		Status string `json:"status"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &submitted))
	require.NotEmpty(t, submitted.ID)
	assert.False(t, strings.HasPrefix(submitted.ID, "async_"))
	assert.Equal(t, "video", submitted.Object)
	assert.Equal(t, "queued", submitted.Status)
	var parent model.AsyncRelayTask
	require.NoError(t, model.DB.First(&parent).Error)
	assert.Equal(t, model.AsyncRelayTaskStatusWaiting, parent.Status)
	assert.Equal(t, submitted.ID, parent.LinkedTaskID)
	fetch := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(fetch)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/videos/"+submitted.ID, nil)
	c.Request.Header.Set("Authorization", "Bearer sk-"+token.Key)
	c.Params = gin.Params{{Key: "task_id", Value: submitted.ID}}
	middleware.TokenAuthReadOnly()(c)
	require.False(t, c.IsAborted(), fetch.Body.String())
	middleware.Distribute()(c)
	require.False(t, c.IsAborted(), fetch.Body.String())
	RelayTaskFetch(c)
	require.Equal(t, http.StatusOK, fetch.Code, fetch.Body.String())
	var polled struct {
		ID     string `json:"id"`
		Object string `json:"object"`
	}
	require.NoError(t, common.Unmarshal(fetch.Body.Bytes(), &polled))
	assert.Equal(t, submitted.ID, polled.ID)
	assert.Equal(t, "video", polled.Object)
	assert.NotContains(t, fetch.Body.String(), "poll_url")
}
