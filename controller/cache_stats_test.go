package controller

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCacheStatsNormalization(t *testing.T) {
	for _, tc := range []struct {
		name               string
		prompt             int
		other              string
		input, read, write int64
	}{
		{"OpenAI输入已经含读取", 100, `{"cache_tokens":40}`, 100, 40, 0},
		{"Gemini输入已经含读取", 200, `{"cache_tokens":60,"usage_semantic":"gemini"}`, 200, 60, 0},
		{"Claude读取和写入均在分母", 4, `{"claude":true,"cache_tokens":267921,"cache_creation_tokens":700}`, 268625, 267921, 700},
		{"分档写入不能重复累加", 10, `{"usage_semantic":"anthropic","cache_tokens":40,"cache_creation_tokens":50,"cache_creation_tokens_5m":20,"cache_creation_tokens_1h":30}`, 100, 40, 50},
		{"写入总数保留未分档部分", 10, `{"claude":true,"cache_tokens":40,"cache_creation_tokens":60,"cache_creation_tokens_5m":20,"cache_creation_tokens_1h":30}`, 110, 40, 60},
		{"显式总输入优先", 4, `{"claude":true,"cache_tokens":40,"input_tokens_total":100}`, 100, 40, 0},
		{"计费上下文长度不是拆分后的普通输入", 10, `{"cache_tokens":40,"billing_tokens":{"len":100,"p":60}}`, 100, 40, 0},
		{"纯缓存Claude请求", 0, `{"claude":true,"cache_tokens":100}`, 100, 100, 0},
		{"无缓存为零命中", 100, `{}`, 100, 0, 0},
		{"只有写入不算命中", 40, `{"cache_creation_tokens":40}`, 40, 0, 40},
		{"带参考图的对话仍统计", 100, `{"image":true,"image_output":50,"cache_tokens":40}`, 100, 40, 0},
		{"按次计费文本不排除", 100, `{"billing_unit":"request","cache_tokens":40}`, 100, 40, 0},
		{"新OpenAI口径优先于协议标记", 100, `{"usage_semantic":"openai","claude":true,"cache_tokens":40}`, 100, 40, 0},
		{"新日志明确总输入无需补缓存", 100, `{"cache_input_includes_cache":true,"claude":true,"cache_tokens":40,"cache_creation_tokens_5m":20}`, 100, 40, 20},
		{"转换后的原始输入明确不含缓存", 10, `{"cache_input_includes_cache":false,"cache_tokens":40,"cache_creation_tokens":50}`, 100, 40, 50},
		{"零输入没有百分比", 0, `{}`, 0, 0, 0},
		{"坏JSON不统计", 10, `{`, 0, 0, 0},
		{"超过输入的缓存不能造出大于百分百", 10, `{"cache_tokens":100}`, 0, 0, 0},
		{"负数字符串和小数不是Token数", 10, `{"cache_tokens":-1,"cache_creation_tokens":"123","cache_write_tokens":0.2}`, 10, 0, 0},
		{"图片路径不统计", 100, `{"request_path":"/v1/images/generations","cache_tokens":40}`, 0, 0, 0},
		{"视频路径不统计", 100, `{"request_path":"/v1/videos","cache_tokens":40}`, 0, 0, 0},
		{"图片任务不统计", 100, `{"is_task":true,"cache_tokens":40}`, 0, 0, 0},
		{"明确图片数量不统计", 100, `{"image_count":1,"cache_tokens":40}`, 0, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := model.CacheUsageForLog(&model.Log{Type: model.LogTypeConsume, PromptTokens: tc.prompt, Other: tc.other})
			if tc.input == 0 {
				assert.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			assert.Equal(t, tc.input, got.InputTokens)
			assert.Equal(t, tc.read, got.ReadTokens)
			assert.Equal(t, tc.write, got.WriteTokens)
			assert.InDelta(t, float64(tc.read)/float64(tc.input)*100, got.HitRate, 0.00001)
		})
	}
	assert.Nil(t, model.CacheUsageForLog(&model.Log{Type: model.LogTypeError, PromptTokens: 10, Other: `{}`}))
	assert.Nil(t, model.CacheUsageForLog(&model.Log{Type: model.LogTypeConsume, PromptTokens: 10, Other: `{}`, RequestId: "async_gemini-image"}))
}

func TestCacheStatsAccessAndDatabase(t *testing.T) {
	for _, kind := range []string{"sqlite", "mysql", "postgres", "clickhouse"} {
		t.Run(kind, func(t *testing.T) {
			dsn := os.Getenv("TEST_" + strings.ToUpper(kind) + "_DSN")
			if kind != "sqlite" && dsn == "" {
				t.Skip("未配置独立测试库")
				return
			}
			db, _ := newAuditTestDatabase(t, kind, dsn)
			previousDB, previousLog := model.DB, model.LOG_DB
			mainType, logType := common.MainDatabaseType(), common.LogDatabaseType()
			previousPublic := operation_setting.GetGeneralSetting().CacheStatsPublic
			previousRedis := common.RedisEnabled
			common.RedisEnabled = false
			model.DB, model.LOG_DB = db, db
			common.SetDatabaseTypes(common.DatabaseType(kind), common.DatabaseType(kind))
			t.Cleanup(func() {
				model.DB, model.LOG_DB = previousDB, previousLog
				common.SetDatabaseTypes(mainType, logType)
				operation_setting.GetGeneralSetting().CacheStatsPublic = previousPublic
				common.RedisEnabled = previousRedis
			})
			if kind == "clickhouse" {
				require.NoError(t, db.Exec(releasedClickHouseLogSchema).Error)
			} else {
				require.NoError(t, db.AutoMigrate(&model.Log{}))
			}
			if kind != "clickhouse" {
				require.NoError(t, db.AutoMigrate(&model.Option{}, &model.AuditLog{}, &model.User{}))
				require.NoError(t, db.Create(&model.User{Username: "root", Role: common.RoleRootUser, Status: common.UserStatusEnabled}).Error)
				common.OptionMapRWMutex.Lock()
				previousOptions := common.OptionMap
				common.OptionMap = maps.Clone(previousOptions)
				if common.OptionMap == nil {
					common.OptionMap = make(map[string]string)
				}
				common.OptionMapRWMutex.Unlock()
				t.Cleanup(func() {
					common.OptionMapRWMutex.Lock()
					common.OptionMap = previousOptions
					common.OptionMapRWMutex.Unlock()
				})
				for _, public := range []bool{true, false} {
					response := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(response)
					c.Set("id", 1)
					c.Set("role", common.RoleRootUser)
					c.Set("username", "root")
					c.Request = httptest.NewRequest("PUT", "/api/option/", strings.NewReader(fmt.Sprintf(`{"key":"general_setting.cache_stats_public","value":%t}`, public)))
					UpdateOption(c)
					assert.Contains(t, response.Body.String(), `"success":true`)
					assert.Equal(t, public, operation_setting.GetGeneralSetting().CacheStatsPublic)
					var saved model.Option
					require.NoError(t, db.Where(&model.Option{Key: "general_setting.cache_stats_public"}).First(&saved).Error)
					assert.Equal(t, fmt.Sprint(public), saved.Value)
				}
			}
			fixtures := []model.Log{
				{UserId: 1, Username: "甲", CreatedAt: 100, ModelName: "text", PromptTokens: 100, Other: `{"cache_tokens":50}`},
				{UserId: 1, Username: "甲", CreatedAt: 200, ModelName: "text", PromptTokens: 900, Other: `{"cache_tokens":90}`},
				{UserId: 2, Username: "乙", CreatedAt: 150, ModelName: "text", PromptTokens: 100, Other: `{"cache_tokens":100}`},
				{UserId: 1, Username: "甲", CreatedAt: 99, ModelName: "excluded", PromptTokens: 100, Other: `{}`},
				{UserId: 1, Username: "甲", CreatedAt: 201, ModelName: "excluded", PromptTokens: 100, Other: `{}`},
				{UserId: 1, Username: "甲", CreatedAt: 150, ModelName: "video", PromptTokens: 100, Other: `{"is_task":true}`},
				{UserId: 1, Username: "甲", CreatedAt: 150, ModelName: "image", PromptTokens: 100, Other: `{}`, RequestId: "async_image"},
			}
			for i := range fixtures {
				fixtures[i].Type = model.LogTypeConsume
				if fixtures[i].RequestId == "" {
					fixtures[i].RequestId = fmt.Sprintf("cache-%d", i)
				}
				require.NoError(t, db.Create(&fixtures[i]).Error)
			}
			for _, public := range []bool{false, true} {
				operation_setting.GetGeneralSetting().CacheStatsPublic = public
				for _, role := range []int{common.RoleCommonUser, common.RoleAdminUser, common.RoleRootUser} {
					response := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(response)
					c.Set("id", 1)
					c.Set("role", role)
					c.Request = httptest.NewRequest(http.MethodGet, "/api/data/cache?start_timestamp=100&end_timestamp=200&user_id=2", nil)
					GetModelCacheStats(c)
					visible := public || role == common.RoleRootUser
					if !visible {
						assert.Equal(t, http.StatusForbidden, response.Code)
					} else {
						var body struct {
							Success bool
							Data    []model.ModelCacheStats
						}
						require.NoError(t, common.Unmarshal(response.Body.Bytes(), &body))
						require.True(t, body.Success, response.Body.String())
						require.Len(t, body.Data, 1)
						if role == common.RoleRootUser {
							assert.EqualValues(t, 1100, body.Data[0].InputTokens)
							assert.EqualValues(t, 240, body.Data[0].ReadTokens)
						} else {
							assert.EqualValues(t, 1000, body.Data[0].InputTokens)
							assert.InDelta(t, 14, body.Data[0].HitRate, 0.00001, "按Token加权而非平均两次命中率")
						}
					}
					logs := []*model.Log{&fixtures[0]}
					model.AttachLogCacheUsage(logs, role)
					assert.Equal(t, visible, logs[0].CacheUsage != nil)
					assert.Equal(t, `{"cache_tokens":50}`, logs[0].Other, "原缓存数字不受公开开关影响")
					response = httptest.NewRecorder()
					c, _ = gin.CreateTestContext(response)
					c.Set("id", 1)
					c.Set("role", role)
					c.Request = httptest.NewRequest("GET", "/api/log/self?type=2&start_timestamp=100&end_timestamp=200", nil)
					GetUserLogs(c)
					var logBody struct {
						Success bool
						Data    struct{ Items []model.Log }
					}
					require.NoError(t, common.Unmarshal(response.Body.Bytes(), &logBody))
					require.True(t, logBody.Success, response.Body.String())
					require.Len(t, logBody.Data.Items, 4)
					for _, entry := range logBody.Data.Items {
						assert.Equal(t, visible && entry.ModelName == "text", entry.CacheUsage != nil)
					}
				}
			}
			// 根用户按用户名筛选；普通用户伪造同名参数仍只能查本人。
			for _, role := range []int{common.RoleRootUser, common.RoleCommonUser} {
				response := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(response)
				c.Set("id", 1)
				c.Set("role", role)
				c.Request = httptest.NewRequest("GET", "/api/data/cache?start_timestamp=100&end_timestamp=200&username=%E4%B9%99&user_id=2", nil)
				GetModelCacheStats(c)
				var body struct{ Data []model.ModelCacheStats }
				require.NoError(t, common.Unmarshal(response.Body.Bytes(), &body))
				require.Len(t, body.Data, 1)
				want := int64(1000)
				if role == common.RoleRootUser {
					want = 100
				}
				assert.Equal(t, want, body.Data[0].InputTokens)
			}
			cancelled, cancel := context.WithCancel(context.Background())
			cancel()
			_, err := model.GetModelCacheStats(cancelled, 100, 200, 1, "")
			require.Error(t, err, "取消查询不能返回半份统计")
			_, err = model.DeleteOldLogBatch(context.Background(), 201, 100)
			require.NoError(t, err)
			empty, err := model.GetModelCacheStats(context.Background(), 100, 200, 0, "")
			require.NoError(t, err)
			assert.Empty(t, empty, "清理日志后统计也移除")
		})
	}
}

func TestCacheStatsSettingsAndInvalidRange(t *testing.T) {
	for _, role := range []int{common.RoleCommonUser, common.RoleAdminUser} {
		response := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(response)
		c.Set("role", role)
		c.Request = httptest.NewRequest("PUT", "/api/option/", strings.NewReader(`{"key":"general_setting.cache_stats_public","value":true}`))
		UpdateOption(c)
		assert.Equal(t, http.StatusForbidden, response.Code)
	}
	for _, query := range []string{"", "start_timestamp=-1&end_timestamp=2", "start_timestamp=2&end_timestamp=1", "start_timestamp=1&end_timestamp=99999999"} {
		response := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(response)
		c.Set("id", 1)
		c.Set("role", common.RoleRootUser)
		c.Request = httptest.NewRequest("GET", "/api/data/cache?"+query, nil)
		GetModelCacheStats(c)
		assert.Contains(t, response.Body.String(), `"success":false`)
	}
}
