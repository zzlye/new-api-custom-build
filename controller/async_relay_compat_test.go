package controller

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
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
	"github.com/tidwall/gjson"
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

// 同提示词的两张不同参考图同时经过真实入队、文件恢复、渠道映射和回传，防止参考图丢失或串任务。
func TestAsyncRelayGeminiConcurrentReferenceIntegrity(t *testing.T) {
	for _, tc := range []struct {
		channelType     int
		alias, upstream string
	}{
		{constant.ChannelTypeGemini, "nano-banana-2", "gemini-3.1-flash-image-preview"},
		{constant.ChannelTypeGemini, "nano-banana-pro", "gemini-3-pro-image-preview"},
		{constant.ChannelTypeOpenAI, "nano-banana-2", "gemini-3.1-flash-image-preview"},
		{constant.ChannelTypeOpenAI, "nano-banana-pro", "gemini-3-pro-image-preview"},
	} {
		t.Run(fmt.Sprintf("channel_%d/%s", tc.channelType, tc.alias), func(t *testing.T) {
			channelType, modelAlias, actualModel := tc.channelType, tc.alias, tc.upstream
			const prompt = "只提升参考图的清晰度，不改变主体、数量、颜色和构图。"
			type captured struct{ path, body, accept string }
			captures := make(chan captured, 2)
			release := make(chan struct{})
			var releaseOnce sync.Once
			user, token := prepareAsyncCompatRelay(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				raw, err := io.ReadAll(r.Body)
				assert.NoError(t, err)
				captures <- captured{r.URL.Path, string(raw), r.Header.Get("Accept")}
				select {
				case <-release:
				case <-r.Context().Done():
					return
				}
				// 上游替身原样回显收到的参考图，任何队列串图都会体现在对应客户端结果中。
				data := gjson.GetBytes(raw, "contents.0.parts.1.inlineData.data").String()
				w.Header().Set("Content-Type", "application/json")
				_, err = fmt.Fprintf(w, "{\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"inlineData\":{\"mimeType\":\"image/png\",\"data\":%q}}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":1,\"candidatesTokenCount\":1,\"totalTokenCount\":2}}", data)
				assert.NoError(t, err)
			}), modelAlias)
			t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
			require.NoError(t, model.DB.Model(&model.Channel{}).Where("models = ?", modelAlias).Updates(map[string]any{"type": channelType, "model_mapping": fmt.Sprintf("{%q:%q}", modelAlias, actualModel)}).Error)

			// 使用确定性的大图，覆盖实际客户几十万字节的参考图，而不是只测试几个字符的占位数据。
			refs := make([][]byte, 2)
			bodies := make([]string, 2)
			expected := make(map[[32]byte]bool)
			for i := range refs {
				picture := image.NewNRGBA(image.Rect(0, 0, 512, 512))
				for y := range 512 {
					for x := range 512 {
						v := uint32(x+y*512+1) * uint32(2654435761+2*i)
						picture.SetNRGBA(x, y, color.NRGBA{R: byte(v), G: byte(v >> 8), B: byte(v >> 16), A: 255})
					}
				}
				var encoded bytes.Buffer
				encoder := png.Encoder{CompressionLevel: png.NoCompression}
				require.NoError(t, encoder.Encode(&encoded, picture))
				refs[i] = encoded.Bytes()
				require.Greater(t, len(refs[i]), 600000)
				expected[sha256.Sum256(refs[i])] = true
				bodies[i] = fmt.Sprintf("{\"contents\":[{\"role\":\"user\",\"parts\":[{\"text\":%q},{\"inlineData\":{\"mimeType\":\"image/png\",\"data\":%q}}]}],\"generationConfig\":{\"responseModalities\":[\"IMAGE\"],\"imageConfig\":{\"aspectRatio\":\"3:4\",\"imageSize\":\"4K\"}}}", prompt, base64.StdEncoding.EncodeToString(refs[i]))
			}
			require.Len(t, expected, 2)
			router := gin.New()
			router.POST("/v1beta/models/*path", middleware.TokenAuth(), middleware.Distribute(), func(c *gin.Context) {
				defer common.CleanupBodyStorage(c)
				Relay(c, relaytypes.RelayFormatGemini)
			})
			gateway := httptest.NewServer(router)

			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			t.Cleanup(func() {
				releaseOnce.Do(func() { close(release) })
				cancel()
				gateway.Close()
			})
			type result struct {
				index, status int
				body          []byte
				taskID        string
				err           error
			}
			results := make(chan result, 2)
			// 浏览器默认通配响应类型与脚本显式 JSON 都必须完整转发参考图。
			accepts := []string{"*/*", "application/json"}
			for i, body := range bodies {
				go func() {
					req, err := http.NewRequestWithContext(ctx, http.MethodPost, gateway.URL+"/v1beta/models/"+modelAlias+":generateContent", strings.NewReader(body))
					if err != nil {
						results <- result{index: i, err: err}
						return
					}
					req.Header.Set("Content-Type", "application/json")
					req.Header.Set("Accept", accepts[i])
					req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
					req.Header.Set("Authorization", "Bearer sk-"+token.Key)
					req.Header.Set("x-goog-api-key", "sk-"+token.Key)
					response, err := http.DefaultClient.Do(req)
					if err != nil {
						results <- result{index: i, err: err}
						return
					}
					defer response.Body.Close()
					raw, err := io.ReadAll(response.Body)
					results <- result{index: i, status: response.StatusCode, body: raw, taskID: response.Header.Get("X-New-Api-Task-Id"), err: err}
				}()
			}
			require.Eventually(t, func() bool {
				var count int64
				return model.DB.Model(&model.AsyncRelayTask{}).Where("user_id = ?", user.Id).Count(&count).Error == nil && count == 2
			}, 5*time.Second, time.Millisecond, "两个请求都应进入后台队列")
			workers := make(chan error, 2)
			for range 2 {
				go func() { _, err := ProcessAsyncRelayTasks(ctx, 1); workers <- err }()
			}
			for range 2 {
				select {
				case got := <-captures:
					assert.Equal(t, "/v1beta/models/"+actualModel+":generateContent", got.path)
					assert.Equal(t, prompt, gjson.Get(got.body, "contents.0.parts.0.text").String())
					assert.Equal(t, "image/png", gjson.Get(got.body, "contents.0.parts.1.inlineData.mimeType").String())
					assert.Equal(t, "3:4", gjson.Get(got.body, "generationConfig.imageConfig.aspectRatio").String())
					assert.Equal(t, "4K", gjson.Get(got.body, "generationConfig.imageConfig.imageSize").String())
					assert.Equal(t, "IMAGE", gjson.Get(got.body, "generationConfig.responseModalities.0").String())
					assert.False(t, gjson.Get(got.body, "messages").Exists())
					reference, err := base64.StdEncoding.DecodeString(gjson.Get(got.body, "contents.0.parts.1.inlineData.data").String())
					require.NoError(t, err)
					digest := sha256.Sum256(reference)
					assert.True(t, expected[digest], "出站参考图必须和一个原始输入逐字节一致，且不得重复另一个任务的图片")
					delete(expected, digest)
					for i, original := range refs {
						if sha256.Sum256(original) == digest {
							assert.Equal(t, accepts[i], got.accept, "响应协商头不得与其他任务串用")
						}
					}
					t.Logf("实际出站图片：Accept=%s，%d 字节，SHA256=%x", got.accept, len(reference), digest)
				case <-ctx.Done():
					t.Fatal("上游替身没有同时收到两张参考图")
				}
			}
			assert.Empty(t, expected)
			releaseOnce.Do(func() { close(release) })
			for range 2 {
				select {
				case err := <-workers:
					require.NoError(t, err)
				case <-ctx.Done():
					t.Fatal("后台工作未按时结束")
				}
			}
			seenTasks := make(map[string]bool)
			for range 2 {
				got := <-results
				require.NoError(t, got.err)
				require.Equal(t, http.StatusOK, got.status, string(got.body))
				assert.NotEmpty(t, got.taskID)
				assert.False(t, seenTasks[got.taskID], "并发请求必须返回不同任务")
				seenTasks[got.taskID] = true
				output, err := base64.StdEncoding.DecodeString(gjson.GetBytes(got.body, "candidates.0.content.parts.0.inlineData.data").String())
				require.NoError(t, err)
				assert.Equal(t, sha256.Sum256(refs[got.index]), sha256.Sum256(output), "客户端必须收到自己的参考图对应结果")
				var task model.AsyncRelayTask
				require.NoError(t, model.DB.Where("task_id = ?", got.taskID).First(&task).Error)
				assert.Equal(t, model.AsyncRelayTaskStatusSucceeded, task.Status, task.Error)
				// 直接读取结果清单，检查落盘结果和回传结果也完全一致。
				path := gjson.Get(task.ResultFiles, "0.path").String()
				stored, err := os.ReadFile(path)
				require.NoError(t, err)
				assert.Equal(t, sha256.Sum256(output), sha256.Sum256(stored))
			}
			assert.Empty(t, captures, "每个任务只向上游提交一次")
		})
	}
}
