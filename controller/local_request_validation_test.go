package controller

import (
	"bytes"
	"fmt"
	"io"
	"maps"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLocalParameterValidationPayloads(t *testing.T) {
	for _, tc := range []struct{ name, path, body, want string }{
		{"缺少内容", "/v1beta/models/gemini:generateContent", `{}`, "contents"},
		{"JSON格式错误", "/v1beta/models/gemini:generateContent", `{"contents":`, "JSON"},
		{"JSON空值", "/v1beta/models/gemini:generateContent", `null`, "对象"},
		{"空parts", "/v1beta/models/gemini:generateContent", `{"contents":[{"parts":[]}]}`, "contents[0].parts"},
		{"无效角色", "/v1beta/models/gemini:generateContent", `{"contents":[{"role":"assistant","parts":[{"text":"hi"}]}]}`, "role"},
		{"文本类型错误", "/v1beta/models/gemini:streamGenerateContent", `{"contents":[{"parts":[{"text":123}]}]}`, "text"},
		{"损坏素材", "/v1beta/models/gemini:generateContent", `{"contents":[{"parts":[{"inlineData":{"mimeType":"image/png","data":"secret-not-base64!"}}]}]}`, "Base64"},
		{"素材类型错误", "/v1beta/models/gemini:generateContent", `{"contents":[{"parts":[{"inlineData":{"data":"YWJj"}}]}]}`, "mimeType"},
		{"素材URI为空", "/v1beta/models/gemini:generateContent", `{"contents":[{"parts":[{"fileData":{"fileUri":""}}]}]}`, "fileUri"},
		{"参数超出范围", "/v1beta/models/gemini:generateContent", `{"contents":[{"parts":[{"text":"hi"}]}],"generationConfig":{"temperature":3}}`, "temperature"},
		{"参数类型错误", "/v1beta/models/gemini:generateContent", `{"contents":[{"parts":[{"text":"hi"}]}],"generationConfig":{"maxOutputTokens":"100"}}`, "maxOutputTokens"},
		{"原生生图合法", "/v1beta/models/gemini:generateContent", `{"contents":[{"parts":[{"text":"hi"},{"inlineData":{"mimeType":"image/png","data":"YWJj"}}]}],"generationConfig":{"responseModalities":["IMAGE"],"temperature":0,"imageConfig":{"imageSize":"4K"}}}`, ""},
		{"兼容别名和未知扩展", "/v1/models/gemini:generateContent", `{"contents":[{"parts":[{"file_data":{"file_uri":"gs://bucket/item"}},{"futurePart":{"id":1}}]}],"generation_config":{"temperature":0}}`, ""},
		{"URL安全Base64", "/v1beta/models/gemini:generateContent", `{"contents":[{"parts":[{"inlineData":{"mimeType":"image/png","data":"-_8"}}]}]}`, ""},
		{"工具响应无文本", "/v1beta/models/gemini:generateContent", `{"contents":[{"role":"user","parts":[{"functionResponse":{"name":"get_weather","response":{"ok":true}}}]}]}`, ""},
		{"空对话", "/v1/chat/completions", `{"model":"gemini","messages":[]}`, "messages"},
		{"对话内容错误", "/v1/chat/completions", `{"model":"gemini","messages":[{"role":"user","content":123}]}`, "content"},
		{"工具调用合法", "/v1/chat/completions", `{"model":"gemini","messages":[{"role":"assistant","content":null,"tool_calls":[{"id":"x","type":"function","function":{"name":"f","arguments":"{}"}}]}]}`, ""},
		{"音频回复上下文", "/v1/chat/completions", `{"model":"audio-model","messages":[{"role":"assistant","content":null,"audio":{"id":"audio_reply"}}]}`, ""},
		{"生图缺提示词", "/v1/images/generations", `{"model":"gemini-image"}`, "prompt"},
		{"生图数量越界", "/v1/images/generations", `{"model":"gemini-image","prompt":"hi","n":2147483647}`, "n"},
		{"不支持的接口保持原样", "/v1/videos", `{"provider_specific":true}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
			c.Request.Header.Set("Content-Type", "application/json")
			defer common.CleanupBodyStorage(c)
			err := helper.ValidateLocalRequest(c)
			if tc.want == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.want)
				assert.NotContains(t, err.Error(), "secret-not-base64")
			}
			storage, err := common.GetBodyStorage(c)
			require.NoError(t, err)
			body, err := storage.Bytes()
			require.NoError(t, err)
			assert.Equal(t, tc.body, string(body), "校验不得改写原始请求")
		})
	}
}

func TestLocalParameterValidationMultipart(t *testing.T) {
	// 校验后继续读取同一上传素材，防止提前解析耗尽编辑图片请求的文件流。
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", "gemini-image"))
	require.NoError(t, writer.WriteField("prompt", "修改参考图"))
	file, err := writer.CreateFormFile("image", "reference.png")
	require.NoError(t, err)
	_, err = file.Write([]byte("test-image-content"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", bytes.NewReader(body.Bytes()))
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	defer common.CleanupBodyStorage(c)
	require.NoError(t, helper.ValidateLocalRequest(c))
	request, err := helper.GetAndValidOpenAIImageRequest(c, relayconstant.RelayModeImagesEdits)
	require.NoError(t, err)
	assert.Equal(t, "修改参考图", request.Prompt)
	storage, err := common.GetBodyStorage(c)
	require.NoError(t, err)
	raw, err := storage.Bytes()
	require.NoError(t, err)
	assert.Equal(t, body.Bytes(), raw)
}

func TestLocalParameterValidationPolicy(t *testing.T) {
	for _, value := range []string{
		`null`, `{"enabled":true}`, `{"rules":[{"group":"","status_code":400}]}`,
		`{"rules":[{"group":"gemini","status_code":200}]}`,
		`{"rules":[{"group":"gemini","status_code":600}]}`,
		`{"rules":[{"group":"gemini","status_code":422.5}]}`,
		`{"rules":[{"group":"gemini","status_code":400},{"group":"gemini","status_code":422}]}`,
	} {
		_, err := operation_setting.ParseLocalRequestValidationPolicy(value)
		assert.Error(t, err, value)
	}
}

func TestLocalParameterValidationDatabase(t *testing.T) {
	for _, kind := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(kind, func(t *testing.T) {
			dsn := os.Getenv("TEST_" + strings.ToUpper(kind) + "_DSN")
			if kind != "sqlite" && dsn == "" {
				t.Skip("未配置独立测试库")
			}
			db, _ := newAuditTestDatabase(t, kind, dsn)
			oldDB, oldLog := model.DB, model.LOG_DB
			oldMainType, oldLogType := common.MainDatabaseType(), common.LogDatabaseType()
			oldRedis := common.RedisEnabled
			model.DB, model.LOG_DB, common.RedisEnabled = db, db, false
			common.SetDatabaseTypes(common.DatabaseType(kind), common.DatabaseType(kind))
			common.OptionMapRWMutex.Lock()
			oldOptions := common.OptionMap
			common.OptionMap = maps.Clone(oldOptions)
			if common.OptionMap == nil {
				common.OptionMap = map[string]string{}
			}
			common.OptionMapRWMutex.Unlock()
			t.Cleanup(func() {
				model.DB, model.LOG_DB, common.RedisEnabled = oldDB, oldLog, oldRedis
				common.SetDatabaseTypes(oldMainType, oldLogType)
				common.OptionMapRWMutex.Lock()
				common.OptionMap = oldOptions
				common.OptionMapRWMutex.Unlock()
			})
			require.NoError(t, db.AutoMigrate(&model.Option{}, &model.User{}, &model.Log{}, &model.AsyncRelayTask{}, &model.AuditLog{}))
			user := &model.User{Username: "local-validator", Quota: 123456, Group: "gemini", Status: common.UserStatusEnabled}
			require.NoError(t, db.Create(user).Error)
			policy := `{"enabled":true,"rules":[{"group":"gemini","enabled":true,"status_code":422}]}`
			for _, role := range []int{common.RoleAdminUser, common.RoleRootUser} {
				response := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(response)
				c.Set("role", role)
				payload, err := common.Marshal(map[string]string{"key": operation_setting.LocalRequestValidationOption, "value": policy})
				require.NoError(t, err)
				c.Request = httptest.NewRequest(http.MethodPut, "/api/option/", strings.NewReader(string(payload)))
				UpdateOption(c)
				if role != common.RoleRootUser {
					assert.Equal(t, 403, response.Code)
				} else {
					require.Contains(t, response.Body.String(), `"success":true`)
				}
			}
			var saved model.Option
			require.NoError(t, db.Where(&model.Option{Key: operation_setting.LocalRequestValidationOption}).First(&saved).Error)
			assert.Equal(t, policy, saved.Value)
			assert.Equal(t, 422, operation_setting.LocalRequestValidationStatus("gemini"))
			require.Error(t, model.UpdateOption(operation_setting.LocalRequestValidationOption, `{"enabled":true,"rules":[]}`))
			assert.Equal(t, 422, operation_setting.LocalRequestValidationStatus("gemini"))
			var upstreamCalls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				upstreamCalls.Add(1)
				w.WriteHeader(503)
				_, _ = w.Write([]byte(`{"error":{"message":"upstream failure"}}`))
			}))
			defer upstream.Close()
			for _, tc := range []struct {
				group, auto, body string
				blocked           bool
			}{
				{"gemini", "", `{"contents":[{"parts":[]}]}`, true},
				{"auto", "gemini", `{"contents":[{"parts":[]}]}`, true},
				{"default", "", `{"contents":[{"parts":[]}]}`, false},
				{"gemini", "", `{"contents":[{"parts":[{"text":"hi"}]}]}`, false},
			} {
				before := upstreamCalls.Load()
				router := gin.New()
				router.Use(middleware.BodyStorageCleanup(), func(c *gin.Context) {
					c.Set("id", user.Id)
					c.Set("username", user.Username)
					common.SetContextKey(c, constant.ContextKeyUsingGroup, tc.group)
					common.SetContextKey(c, constant.ContextKeyAutoGroup, tc.auto)
					if middleware.CheckLocalRequestParameters(c) {
						return
					}
					c.Next()
				})
				router.POST("/v1beta/models/*path", func(c *gin.Context) {
					res, err := http.Post(upstream.URL, "application/json", strings.NewReader(tc.body))
					require.NoError(t, err)
					defer res.Body.Close()
					body, err := io.ReadAll(res.Body)
					require.NoError(t, err)
					c.Data(res.StatusCode, "application/json", body)
				})
				response := httptest.NewRecorder()
				router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini:generateContent", strings.NewReader(tc.body)))
				if tc.blocked {
					assert.Equal(t, 422, response.Code)
					assert.Contains(t, response.Body.String(), "contents[0].parts")
					assert.Contains(t, response.Body.String(), "local_parameter_error")
					assert.Equal(t, before, upstreamCalls.Load())
				} else {
					assert.Equal(t, 503, response.Code)
					assert.Contains(t, response.Body.String(), "upstream failure")
					assert.Equal(t, before+1, upstreamCalls.Load())
				}
			}
			// 实际分配入口的拒绝请求不进入 Relay，因此即使图片请求也不会创建任务或预扣费。
			router := gin.New()
			router.Use(middleware.BodyStorageCleanup(), func(c *gin.Context) {
				c.Set("id", user.Id)
				c.Set("username", user.Username)
				common.SetContextKey(c, constant.ContextKeyUsingGroup, "gemini")
			}, middleware.Distribute())
			router.POST("/v1beta/models/*path", func(c *gin.Context) { Relay(c, types.RelayFormatGemini) })
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-image:generateContent", strings.NewReader(`{"contents":[]}`)))
			assert.Equal(t, 422, response.Code)
			var tasks int64
			require.NoError(t, db.Model(&model.AsyncRelayTask{}).Count(&tasks).Error)
			assert.Zero(t, tasks)
			var logs []model.Log
			require.NoError(t, db.Order("id").Find(&logs).Error)
			require.Len(t, logs, 3)
			for _, entry := range logs {
				assert.Equal(t, model.LogTypeError, entry.Type)
				assert.Equal(t, "gemini", entry.Group)
				assert.Zero(t, entry.Quota)
				assert.Contains(t, entry.Other, `"status_code":422`)
				assert.Contains(t, entry.Content, "本地参数校验失败")
			}
			require.NoError(t, db.First(user, user.Id).Error)
			assert.Equal(t, 123456, user.Quota)
			// 关闭策略立即恢复原有行为，已记录的错误仍留在日志里。
			require.NoError(t, model.UpdateOption(operation_setting.LocalRequestValidationOption, strings.Replace(policy, `"enabled":true`, `"enabled":false`, 1)))
			assert.Zero(t, operation_setting.LocalRequestValidationStatus("gemini"))
			t.Log(fmt.Sprintf("%s：设置权限、持久化、分组拦截、上游零请求及错误日志通过", kind))
		})
	}
}
