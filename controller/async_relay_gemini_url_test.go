package controller

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// 通过真实队列、Gemini 转发和媒体归档，验证开关及参数不会改变未选择 URL 的请求。
func TestGeminiImageURLReturn(t *testing.T) {
	// 显式初始化生产启动时设置的流式超时，避免测试依赖全局初始化顺序。
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 60
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
	for _, tc := range []struct {
		name                                              string
		enabled, parameter, passthrough, stream, taskMode bool
	}{
		{name: "开启且请求URL", enabled: true, parameter: true},
		{name: "开启但不传参数", enabled: true},
		{name: "关闭且请求URL", parameter: true},
		{name: "透传请求URL", enabled: true, parameter: true, passthrough: true},
		{name: "流式入口请求URL", enabled: true, parameter: true, stream: true},
		{name: "任务模式请求URL", enabled: true, parameter: true, taskMode: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			old := *model_setting.GetGlobalSettings()
			t.Cleanup(func() { *model_setting.GetGlobalSettings() = old })
			require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{"global.gemini_image_url_enabled": fmt.Sprint(tc.enabled)}))
			previousAddress := system_setting.TaskPublicAddress
			system_setting.TaskPublicAddress = "https://media.example/gateway"
			t.Cleanup(func() { system_setting.TaskPublicAddress = previousAddress })
			var calls atomic.Int32
			upstreamBody := fmt.Sprintf(`{"candidates":[{"content":{"role":"model","parts":[{"text":"image generated"},{"inlineData":{"mimeType":"image/png","data":%q},"thoughtSignature":"large-private-signature"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":1,"totalTokenCount":2}}`, asyncFixturePNG)
			user, token := prepareAsyncCompatRelay(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				raw, err := io.ReadAll(r.Body)
				assert.NoError(t, err)
				assert.False(t, gjson.GetBytes(raw, "response_format").Exists(), "本地返回参数不得传给上游，包括透传请求")
				assert.Equal(t, "IMAGE", gjson.GetBytes(raw, "generationConfig.responseModalities.0").String())
				assert.Equal(t, "client-reference-signature", gjson.GetBytes(raw, "contents.0.parts.0.thoughtSignature").String())
				if tc.stream {
					w.Header().Set("Content-Type", "text/event-stream")
					_, err = fmt.Fprintf(w, "data: %s\n\n", upstreamBody)
				} else {
					w.Header().Set("Content-Type", "application/json")
					_, err = io.WriteString(w, upstreamBody)
				}
				assert.NoError(t, err)
			}), "nano-banana-2.1")
			setting := fmt.Sprintf(`{"pass_through_body_enabled":%t}`, tc.passthrough)
			require.NoError(t, model.DB.Model(&model.Channel{}).Where("models = ?", "nano-banana-2.1").Updates(map[string]any{"type": constant.ChannelTypeGemini, "setting": setting}).Error)
			body := `{"contents":[{"role":"user","parts":[{"text":"generate a cat","thoughtSignature":"client-reference-signature"}]}],"generationConfig":{"responseModalities":["IMAGE"],"imageConfig":{"imageSize":"1K"}}}`
			if tc.parameter {
				body = strings.TrimSuffix(body, "}") + `,"response_format":"url"}`
			}
			path := "/v1beta/models/nano-banana-2.1:generateContent"
			if tc.stream {
				path = "/v1beta/models/nano-banana-2.1:streamGenerateContent"
			}
			if tc.taskMode {
				path += "?async=true"
			}
			recording := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recording)
			c.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Request.Header.Set("Authorization", "Bearer sk-"+token.Key)
			middleware.TokenAuth()(c)
			require.False(t, c.IsAborted())
			done := make(chan struct{})
			go func() {
				defer close(done)
				defer common.CleanupBodyStorage(c)
				Relay(c, relaytypes.RelayFormatGemini)
			}()
			t.Cleanup(func() {
				select {
				case <-done:
				case <-time.After(5 * time.Second):
					t.Error("请求没有结束")
				}
			})
			var task model.AsyncRelayTask
			require.Eventually(t, func() bool { return model.DB.Order("id desc").First(&task).Error == nil }, 5*time.Second, time.Millisecond)
			// 提交后改变总开关，已提交任务及其查询仍应遵循提交时的选择。
			require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{"global.gemini_image_url_enabled": "false"}))
			_, err := ProcessAsyncRelayTasks(context.Background(), 1)
			require.NoError(t, err)
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("客户端等待超时")
			}
			require.NoError(t, model.DB.Where("task_id = ?", task.TaskID).First(&task).Error)
			require.Equal(t, model.AsyncRelayTaskStatusSucceeded, task.Status, task.Error)
			assert.Equal(t, int32(1), calls.Load(), "每个任务只能向上游生成一次")
			params := gin.Params{{Key: "task_id", Value: task.TaskID}}
			poll := asyncControllerRequest(GetAsyncRelayTask, http.MethodGet, "/v1/tasks/"+task.TaskID, user.Id, common.RoleCommonUser, params, "")
			require.Equal(t, http.StatusOK, poll.Code)
			if !tc.enabled || !tc.parameter {
				assert.Contains(t, recording.Body.String(), asyncFixturePNG)
				assert.Contains(t, poll.Body.String(), "large-private-signature")
				return
			}
			var payload []byte
			if tc.taskMode {
				require.Equal(t, http.StatusAccepted, recording.Code)
				payload = []byte(gjson.GetBytes(poll.Body.Bytes(), "result").Raw)
			} else {
				require.Equal(t, http.StatusOK, recording.Code, recording.Body.String())
				payload = recording.Body.Bytes()
			}
			assert.NotContains(t, string(payload), "inlineData")
			assert.NotContains(t, string(payload), "thoughtSignature")
			assert.NotContains(t, poll.Body.String(), asyncFixturePNG)
			link := gjson.GetBytes(payload, "candidates.0.content.parts.0.fileData.fileUri").String()
			require.True(t, strings.HasPrefix(link, "https://media.example/gateway/task-media/"), link)
			assert.Equal(t, link, gjson.GetBytes(poll.Body.Bytes(), "result.candidates.0.content.parts.0.fileData.fileUri").String())
			parsed, err := url.Parse(link)
			require.NoError(t, err)
			media := asyncControllerRequest(GetAsyncRelayMediaDirect, http.MethodGet, strings.TrimPrefix(parsed.RequestURI(), "/gateway"), 0, 0, gin.Params{{Key: "task_id", Value: task.TaskID}, {Key: "kind", Value: "media"}, {Key: "index", Value: "0"}}, "")
			require.Equal(t, http.StatusOK, media.Code, media.Body.String())
			assert.Equal(t, "image/png", media.Header().Get("Content-Type"))
			raw, err := os.ReadFile(task.ResponseFilePath)
			require.NoError(t, err)
			assert.Contains(t, string(raw), "large-private-signature", "原始响应仅在服务器保留，不能为了精简回执丢失取证数据")
		})
	}
}

// URL 模式的查询沿用归属和有效期限制，失败归档也不得重新泄露图片数据。
func TestGeminiImageURLTaskBoundaries(t *testing.T) {
	prepareAsyncMediaController(t)
	metadata, err := common.Marshal(asyncRelayMetadata{GeminiImageURL: true})
	require.NoError(t, err)
	path, file, err := common.CreateAsyncMediaFile()
	require.NoError(t, err)
	_, err = file.WriteString(`{"inlineData":{"data":"private-image"},"thoughtSignature":"private-signature"}`)
	require.NoError(t, err)
	require.NoError(t, file.Close())
	task := model.AsyncRelayTask{TaskID: "gemini-url-boundaries", UserID: 7, RequestFormat: string(relaytypes.RelayFormatGemini), RequestMetadata: string(metadata), Status: model.AsyncRelayTaskStatusFailed, Error: "upstream failed", ResultContentType: "application/json", ResultFilePath: path, FinishedAt: common.GetTimestamp()}
	require.NoError(t, model.DB.Create(&task).Error)
	params := gin.Params{{Key: "task_id", Value: task.TaskID}}
	failed := asyncControllerRequest(GetAsyncRelayTask, http.MethodGet, "/v1/tasks/"+task.TaskID, 7, common.RoleCommonUser, params, "")
	assert.Equal(t, http.StatusOK, failed.Code)
	assert.Contains(t, failed.Body.String(), "upstream failed")
	assert.NotContains(t, failed.Body.String(), "private-image")
	assert.NotContains(t, failed.Body.String(), "private-signature")
	otherUser := asyncControllerRequest(GetAsyncRelayTask, http.MethodGet, "/v1/tasks/"+task.TaskID, 8, common.RoleCommonUser, params, "")
	assert.Equal(t, http.StatusNotFound, otherUser.Code)
	require.NoError(t, model.DB.Model(&task).Updates(map[string]any{"status": model.AsyncRelayTaskStatusSucceeded, "finished_at": common.GetTimestamp() - common.AsyncMediaRetentionSeconds() - 1}).Error)
	expired := asyncControllerRequest(GetAsyncRelayTask, http.MethodGet, "/v1/tasks/"+task.TaskID, 7, common.RoleCommonUser, params, "")
	assert.Equal(t, http.StatusOK, expired.Code)
	assert.True(t, gjson.GetBytes(expired.Body.Bytes(), "result_expired").Bool())
	assert.False(t, gjson.GetBytes(expired.Body.Bytes(), "result").Exists())
}

// 在三种真实数据库中验证开关和任务返回方式可保存、重载，不引入新的数据库列。
func TestGeminiImageURLPersistence(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			var driver gorm.Dialector
			switch dialect {
			case "sqlite":
				driver = sqlite.Open(filepath.Join(t.TempDir(), "gemini-url.db"))
			case "mysql":
				dsn := os.Getenv("TEST_MYSQL_DSN")
				if dsn == "" {
					t.Skip("TEST_MYSQL_DSN 未配置")
				}
				driver = mysql.Open(dsn)
			case "postgres":
				dsn := os.Getenv("TEST_POSTGRES_DSN")
				if dsn == "" {
					t.Skip("TEST_POSTGRES_DSN 未配置")
				}
				driver = postgres.Open(dsn)
			}
			db, err := gorm.Open(driver, &gorm.Config{})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
			var version string
			versionSQL := "select version()"
			if dialect == "sqlite" {
				versionSQL = "select sqlite_version()"
			}
			require.NoError(t, db.Raw(versionSQL).Scan(&version).Error)
			t.Logf("%s version: %s", dialect, version)
			options := db.Table("gemini_url_test_options").Session(&gorm.Session{})
			tasks := db.Table("gemini_url_test_tasks").Session(&gorm.Session{})
			require.NoError(t, options.AutoMigrate(&model.Option{}))
			require.NoError(t, tasks.AutoMigrate(&model.AsyncRelayTask{}))
			t.Cleanup(func() {
				require.NoError(t, db.Migrator().DropTable("gemini_url_test_tasks"))
				require.NoError(t, db.Migrator().DropTable("gemini_url_test_options"))
			})
			old := *model_setting.GetGlobalSettings()
			t.Cleanup(func() { *model_setting.GetGlobalSettings() = old })
			for _, enabled := range []bool{true, false} {
				option := model.Option{Key: "global.gemini_image_url_enabled", Value: fmt.Sprint(enabled)}
				require.NoError(t, options.Save(&option).Error)
				var loaded model.Option
				require.NoError(t, options.Where(&model.Option{Key: option.Key}).First(&loaded).Error)
				require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{loaded.Key: loaded.Value}))
				assert.Equal(t, enabled, model_setting.GetGlobalSettings().GeminiImageURLEnabled)
			}
			metadata, err := common.Marshal(asyncRelayMetadata{GeminiImageURL: true})
			require.NoError(t, err)
			task := model.AsyncRelayTask{TaskID: "gemini-url-persisted", RequestFormat: string(relaytypes.RelayFormatGemini), RequestMetadata: string(metadata)}
			require.NoError(t, tasks.Create(&task).Error)
			var loaded model.AsyncRelayTask
			require.NoError(t, tasks.First(&loaded, task.ID).Error)
			assert.True(t, isGeminiImageURLTask(&loaded), "重载任务遵守提交时的返回方式，不受开关关闭影响")
		})
	}
}
