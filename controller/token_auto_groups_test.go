package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	taskdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func configureTokenAutoGroupsTest(t *testing.T, maxCount string, autoGroups string) {
	t.Helper()
	originalMax := setting.GetMaxTokenAutoGroups()
	originalAutoGroups := setting.AutoGroups2JsonString()
	originalUsableGroups := setting.UserUsableGroups2JSONString()
	originalRatios := ratio_setting.GroupRatio2JSONString()
	require.NoError(t, setting.UpdateMaxTokenAutoGroups(maxCount))
	require.NoError(t, setting.UpdateAutoGroupsByJsonString(autoGroups))
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default","vip":"VIP"}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"vip":1}`))
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateMaxTokenAutoGroups(stringInt(originalMax)))
		require.NoError(t, setting.UpdateAutoGroupsByJsonString(originalAutoGroups))
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(originalUsableGroups))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalRatios))
	})
}

func stringInt(value int) string {
	return fmt.Sprintf("%d", value)
}

func setupTokenAutoGroupsControllerTest(t *testing.T) *model.User {
	t.Helper()
	db := setupTokenControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.User{}))
	user := &model.User{
		Id:       101,
		Username: "token-auto-user",
		Password: "password",
		Group:    "default",
		Status:   common.UserStatusEnabled,
	}
	require.NoError(t, db.Create(user).Error)
	return user
}

func baseAutoTokenRequest(name string) map[string]any {
	return map[string]any{
		"name":              name,
		"expired_time":      -1,
		"remain_quota":      0,
		"unlimited_quota":   true,
		"group":             "auto",
		"cross_group_retry": true,
	}
}

func newTokenAutoGroupsAuthenticatedContext(t *testing.T, method string, target string, body any, userID int) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	ctx, recorder := newAuthenticatedContext(t, method, target, body, userID)
	common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
	return ctx, recorder
}

func TestAddTokenEmptyAutoGroupsInheritGlobalAuto(t *testing.T) {
	tests := []struct {
		name         string
		includeField bool
		value        any
	}{
		{name: "omitted"},
		{name: "null", includeField: true, value: nil},
		{name: "empty array", includeField: true, value: []string{}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			configureTokenAutoGroupsTest(t, "5", `["default","vip"]`)
			user := setupTokenAutoGroupsControllerTest(t)
			request := baseAutoTokenRequest("create-" + test.name)
			if test.includeField {
				request["auto_groups"] = test.value
			}

			ctx, recorder := newTokenAutoGroupsAuthenticatedContext(t, http.MethodPost, "/api/token/", request, user.Id)
			AddToken(ctx)

			response := decodeAPIResponse(t, recorder)
			require.True(t, response.Success, response.Message)
			var token model.Token
			require.NoError(t, model.DB.Where("name = ?", request["name"]).First(&token).Error)
			assert.Empty(t, token.AutoGroups)
			assert.True(t, token.CrossGroupRetry)
			payload, err := common.Marshal(buildMaskedTokenResponse(&token))
			require.NoError(t, err)
			var responseData map[string]any
			require.NoError(t, common.Unmarshal(payload, &responseData))
			assert.Nil(t, responseData["auto_groups"])
		})
	}
}

func TestAddTokenPersistsOrderedAutoGroupsSnapshot(t *testing.T) {
	configureTokenAutoGroupsTest(t, "5", `["default","vip"]`)
	user := setupTokenAutoGroupsControllerTest(t)
	request := baseAutoTokenRequest("ordered-snapshot")
	request["auto_groups"] = []string{"vip", "default"}

	ctx, recorder := newTokenAutoGroupsAuthenticatedContext(t, http.MethodPost, "/api/token/", request, user.Id)
	AddToken(ctx)
	require.True(t, decodeAPIResponse(t, recorder).Success)

	var token model.Token
	require.NoError(t, model.DB.Where("name = ?", "ordered-snapshot").First(&token).Error)
	assert.JSONEq(t, `["vip","default"]`, token.AutoGroups)

	getCtx, getRecorder := newTokenAutoGroupsAuthenticatedContext(t, http.MethodGet, "/api/token/"+stringInt(token.Id), nil, user.Id)
	getCtx.Params = append(getCtx.Params, gin.Param{Key: "id", Value: stringInt(token.Id)})
	GetToken(getCtx)
	getResponse := decodeAPIResponse(t, getRecorder)
	require.True(t, getResponse.Success)
	var data struct {
		AutoGroups []string `json:"auto_groups"`
	}
	require.NoError(t, common.Unmarshal(getResponse.Data, &data))
	assert.Equal(t, []string{"vip", "default"}, data.AutoGroups)
}

func TestUpdateTokenAutoGroupsTriStateAndNonAutoCleanup(t *testing.T) {
	tests := []struct {
		name               string
		includeField       bool
		value              any
		group              string
		expectedAutoGroups string
		expectedRetry      bool
	}{
		{name: "omitted preserves", group: "auto", expectedAutoGroups: `["vip","default"]`, expectedRetry: true},
		{name: "null inherits", includeField: true, value: nil, group: "auto", expectedRetry: true},
		{name: "empty inherits", includeField: true, value: []string{}, group: "auto", expectedRetry: true},
		{name: "non auto clears and disables retry", includeField: true, value: []string{"vip"}, group: "default"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			configureTokenAutoGroupsTest(t, "5", `["default","vip"]`)
			user := setupTokenAutoGroupsControllerTest(t)
			token := seedToken(t, model.DB, user.Id, "update-auto", "update-auto-key")
			token.Group = "auto"
			token.CrossGroupRetry = true
			require.NoError(t, token.SetAutoGroups([]string{"vip", "default"}))
			require.NoError(t, model.DB.Save(token).Error)

			request := baseAutoTokenRequest("updated-auto")
			request["id"] = token.Id
			request["status"] = common.TokenStatusEnabled
			request["group"] = test.group
			if test.includeField {
				request["auto_groups"] = test.value
			}
			ctx, recorder := newTokenAutoGroupsAuthenticatedContext(t, http.MethodPut, "/api/token/", request, user.Id)
			UpdateToken(ctx)
			response := decodeAPIResponse(t, recorder)
			require.True(t, response.Success, response.Message)

			var updated model.Token
			require.NoError(t, model.DB.First(&updated, token.Id).Error)
			if test.expectedAutoGroups == "" {
				assert.Empty(t, updated.AutoGroups)
			} else {
				assert.JSONEq(t, test.expectedAutoGroups, updated.AutoGroups)
			}
			assert.Equal(t, test.expectedRetry, updated.CrossGroupRetry)
		})
	}
}

func TestAddTokenRejectsInvalidAutoGroups(t *testing.T) {
	tests := []struct {
		name     string
		maxCount string
		groups   []string
	}{
		{name: "over limit", maxCount: "1", groups: []string{"default", "vip"}},
		{name: "duplicate", maxCount: "5", groups: []string{"default", "default"}},
		{name: "auto pseudo group", maxCount: "5", groups: []string{"auto"}},
		{name: "unavailable", maxCount: "5", groups: []string{"missing"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			configureTokenAutoGroupsTest(t, test.maxCount, `["default","vip"]`)
			user := setupTokenAutoGroupsControllerTest(t)
			request := baseAutoTokenRequest("invalid-" + test.name)
			request["auto_groups"] = test.groups

			ctx, recorder := newTokenAutoGroupsAuthenticatedContext(t, http.MethodPost, "/api/token/", request, user.Id)
			AddToken(ctx)

			response := decodeAPIResponse(t, recorder)
			assert.False(t, response.Success)
			var count int64
			require.NoError(t, model.DB.Model(&model.Token{}).Count(&count).Error)
			assert.Zero(t, count)
		})
	}
}

func TestGetTokenAutoGroupsReturnsFullFilteredGlobalOrderAndLimit(t *testing.T) {
	configureTokenAutoGroupsTest(t, "1", `["vip","missing","default"]`)
	user := setupTokenAutoGroupsControllerTest(t)

	ctx, recorder := newTokenAutoGroupsAuthenticatedContext(t, http.MethodGet, "/api/token/auto-groups", nil, user.Id)
	GetTokenAutoGroups(ctx)

	response := decodeAPIResponse(t, recorder)
	require.True(t, response.Success, response.Message)
	var data struct {
		Groups   []string `json:"groups"`
		MaxCount int      `json:"max_count"`
	}
	require.NoError(t, common.Unmarshal(response.Data, &data))
	assert.Equal(t, []string{"vip", "default"}, data.Groups)
	assert.Equal(t, 1, data.MaxCount)
}

// 验证专用 Key 与原有 Key 分页隔离，且请求不能把 root 授权替换成自选分组。
func TestMediaKeyCreationIsolationAndImmutablePurpose(t *testing.T) {
	configureTokenAutoGroupsTest(t, "1", `["default"]`)
	user := setupTokenAutoGroupsControllerTest(t)
	require.NoError(t, model.DB.AutoMigrate(&model.Option{}))
	require.NoError(t, model.DB.Create(&model.Option{Key: model.MediaKeyGroupsOption, Value: `["vip","default"]`}).Error)
	seedToken(t, model.DB, user.Id, "ordinary", "ordinary-test-key")
	req := baseAutoTokenRequest("media-test")
	req["purpose"], req["group"], req["auto_groups"] = "media", "untrusted", []string{"untrusted"}
	ctx, rec := newTokenAutoGroupsAuthenticatedContext(t, http.MethodPost, "/api/token/", req, user.Id)
	AddToken(ctx)
	require.True(t, decodeAPIResponse(t, rec).Success, rec.Body.String())
	var token model.Token
	require.NoError(t, model.DB.Where("name = ?", "media-test").First(&token).Error)
	assert.Equal(t, "media", token.Purpose)
	assert.Equal(t, "auto", token.Group)
	assert.True(t, token.CrossGroupRetry)
	assert.Empty(t, token.AutoGroups)
	for _, test := range []struct{ purpose, name string }{{"", "ordinary"}, {"media", "media-test"}} {
		ctx, rec = newTokenAutoGroupsAuthenticatedContext(t, http.MethodGet, "/api/token/?purpose="+test.purpose, nil, user.Id)
		GetAllTokens(ctx)
		response := decodeAPIResponse(t, rec)
		require.True(t, response.Success)
		var page tokenPageResponse
		require.NoError(t, common.Unmarshal(response.Data, &page))
		require.Len(t, page.Items, 1)
		assert.Equal(t, test.name, page.Items[0].Name)
		found, total, err := model.SearchUserTokens(user.Id, "", "", 0, 10, test.purpose)
		require.NoError(t, err)
		require.Len(t, found, 1)
		assert.EqualValues(t, 1, total)
	}
	req["id"], req["purpose"], req["cross_group_retry"] = token.Id, "", false
	ctx, rec = newTokenAutoGroupsAuthenticatedContext(t, http.MethodPut, "/api/token/", req, user.Id)
	UpdateToken(ctx)
	require.True(t, decodeAPIResponse(t, rec).Success, rec.Body.String())
	saved, err := model.GetTokenByIds(token.Id, user.Id)
	require.NoError(t, err)
	assert.Equal(t, "media", saved.Purpose)
	assert.Equal(t, "auto", saved.Group)
	assert.True(t, saved.CrossGroupRetry)
	assert.Empty(t, saved.AutoGroups)
	// 他人的 ID 不可用于修改或读取密钥。
	ctx, rec = newTokenAutoGroupsAuthenticatedContext(t, http.MethodPut, "/api/token/", req, user.Id+1)
	UpdateToken(ctx)
	assert.False(t, decodeAPIResponse(t, rec).Success)
}

func TestMediaKeyUsesLiveRootGroupsAndFailsClosed(t *testing.T) {
	configureTokenAutoGroupsTest(t, "1", `["default"]`)
	user := setupTokenAutoGroupsControllerTest(t)
	require.NoError(t, model.DB.AutoMigrate(&model.Option{}, &model.Channel{}, &model.Ability{}))
	// 普通用户没有 vip 权限，root 的专用授权仍可赋予该分组。
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default"}`))
	token := &model.Token{Id: 88, UserId: user.Id, Purpose: model.TokenPurposeMedia, Group: "untrusted", AutoGroups: `["untrusted"]`}
	for _, test := range []struct {
		groups string
		want   []string
	}{{`["vip","default"]`, []string{"vip", "default"}}, {`["default"]`, []string{"default"}}, {`[]`, nil}, {`broken`, nil}} {
		require.NoError(t, model.DB.Save(&model.Option{Key: model.MediaKeyGroupsOption, Value: test.groups}).Error)
		ctx, _ := newTokenAutoGroupsAuthenticatedContext(t, http.MethodPost, "/v1/chat/completions", nil, user.Id)
		err := middleware.SetupContextForToken(ctx, token)
		if test.want == nil {
			require.Error(t, err)
			assert.True(t, ctx.IsAborted())
		} else {
			require.NoError(t, err)
			assert.Equal(t, test.want, service.GetRequestAutoGroups(ctx, "default"))
			assert.Equal(t, "auto", common.GetContextKeyString(ctx, constant.ContextKeyUsingGroup))
			assert.True(t, common.GetContextKeyBool(ctx, constant.ContextKeyTokenCrossGroupRetry))
		}
	}
	require.NoError(t, model.DB.Save(&model.Option{Key: model.MediaKeyGroupsOption, Value: `["vip"]`}).Error)
	ctx, _ := newTokenAutoGroupsAuthenticatedContext(t, http.MethodPost, "/v1/chat/completions", nil, user.Id)
	require.Error(t, middleware.SetupContextForToken(ctx, token, "key", "175"))
	ordinary := &model.Token{Id: 89, UserId: user.Id, Group: "auto"}
	ctx, _ = newTokenAutoGroupsAuthenticatedContext(t, http.MethodPost, "/v1/chat/completions", nil, user.Id)
	require.NoError(t, middleware.SetupContextForToken(ctx, ordinary))
	assert.Equal(t, []string{"default"}, service.GetRequestAutoGroups(ctx, "default"))
	for _, raw := range []string{`["auto"]`, `["vip","vip"]`, `["unknown"]`, `null`, `{}`} {
		assert.Error(t, model.ValidateMediaKeyGroups(raw))
	}
	assert.NoError(t, model.ValidateMediaKeyGroups(`["vip","default"]`))
	assert.NoError(t, model.ValidateMediaKeyGroups(`[]`))
	// 未配置时不允许建立一个会回落普通 Auto 的专用密钥。
	require.NoError(t, model.DB.Save(&model.Option{Key: model.MediaKeyGroupsOption, Value: `[]`}).Error)
	req := baseAutoTokenRequest("blocked")
	req["purpose"] = "media"
	ctx, rec := newTokenAutoGroupsAuthenticatedContext(t, http.MethodPost, "/api/token/", req, user.Id)
	AddToken(ctx)
	assert.False(t, decodeAPIResponse(t, rec).Success)
}

// 使用真实分发中间件验证顺序与权限，不调用外部模型或产生扣费。
func TestMediaKeyRoutingAndRetry(t *testing.T) {
	configureTokenAutoGroupsTest(t, "1", `["default"]`)
	user := setupTokenAutoGroupsControllerTest(t)
	t.Setenv("LOG_SQL_DSN", "")
	oldMaster := common.IsMasterNode
	common.IsMasterNode = false
	t.Cleanup(func() { common.IsMasterNode = oldMaster })
	require.NoError(t, model.InitLogDB())
	require.NoError(t, model.DB.AutoMigrate(&model.Option{}, &model.Channel{}, &model.Ability{}))
	oldMemory, oldRetries := common.MemoryCacheEnabled, common.RetryTimes
	common.MemoryCacheEnabled, common.RetryTimes = false, 0
	t.Cleanup(func() { common.MemoryCacheEnabled, common.RetryTimes = oldMemory, oldRetries })
	for i, group := range []string{"vip", "default", "private"} {
		id := i + 1001
		models := "chat-model"
		if group == "private" {
			models = "private-only"
		}
		priority, weight := int64(0), uint(100)
		require.NoError(t, model.DB.Create(&model.Channel{Id: id, Type: constant.ChannelTypeOpenAI, Key: "fixture", Status: common.ChannelStatusEnabled, Name: group, Models: models, Group: group, Priority: &priority, Weight: &weight}).Error)
		require.NoError(t, model.DB.Create(&model.Ability{Group: group, Model: models, ChannelId: id, Enabled: true, Priority: &priority, Weight: weight}).Error)
	}
	token := &model.Token{Id: 88, UserId: user.Id, Key: strings.Repeat("m", 48), Status: common.TokenStatusEnabled, ExpiredTime: -1, UnlimitedQuota: true, Purpose: model.TokenPurposeMedia, Group: "auto"}
	require.NoError(t, model.DB.Create(token).Error)
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default"}`))
	require.NoError(t, model.DB.Save(&model.Option{Key: model.MediaKeyGroupsOption, Value: `["vip","default"]`}).Error)
	for _, path := range []string{"/v1/chat/completions", "/pg/chat/completions"} {
		router := gin.New()
		router.POST(path, middleware.TokenAuth(), middleware.Distribute(), func(c *gin.Context) {
			assert.Equal(t, 1001, c.GetInt("channel_id"))
			assert.Equal(t, "vip", common.GetContextKeyString(c, constant.ContextKeyAutoGroup))
			param := &service.RetryParam{Ctx: c, TokenGroup: "auto", ModelName: "chat-model", RequestPath: path}
			channel, group, err := service.CacheGetRandomSatisfiedChannel(param)
			require.NoError(t, err)
			require.NotNil(t, channel)
			assert.Equal(t, 1002, channel.Id)
			assert.Equal(t, "default", group)
			// 异步重试也只能切换到 root 授权的分组。
			c.Set("use_channel", []string{"1001"})
			channel, group, err = service.SelectAsyncMediaRetryChannel(param)
			require.NoError(t, err)
			require.NotNil(t, channel)
			assert.Equal(t, 1002, channel.Id)
			assert.Equal(t, "default", group)
			c.Status(http.StatusNoContent)
		})
		request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"chat-model","group":"default","messages":[{"role":"user","content":"hello"}]}`))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer "+token.GetFullKey())
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		require.Equal(t, http.StatusNoContent, response.Code, response.Body.String())
	}
	ctx, _ := newTokenAutoGroupsAuthenticatedContext(t, http.MethodPost, "/v1/chat/completions", nil, user.Id)
	require.NoError(t, middleware.SetupContextForToken(ctx, token))
	channel, _, selectionErr := service.SelectChannelForRequest(ctx, "private-only", &service.RetryParam{Ctx: ctx, TokenGroup: "auto", ModelName: "private-only", RequestPath: "/v1/chat/completions"})
	assert.Nil(t, channel)
	require.NotNil(t, selectionErr)
	assert.True(t, selectionErr.NoAvailableChannel)
	for _, test := range []struct {
		id      int
		model   string
		allowed bool
	}{{1001, "chat-model", true}, {1003, "private-only", false}} {
		ctx, _ := newTokenAutoGroupsAuthenticatedContext(t, http.MethodPost, "/v1/videos/task/remix", nil, user.Id)
		require.NoError(t, middleware.SetupContextForToken(ctx, token))
		service.GetChannelConstraints(ctx).AddPin(taskdto.ChannelPin{ChannelId: test.id, Source: taskdto.PinSourceOriginTask})
		channel, group, err := service.SelectChannelForRequest(ctx, test.model, &service.RetryParam{Ctx: ctx, TokenGroup: "auto", ModelName: test.model})
		if test.allowed {
			require.Nil(t, err)
			require.NotNil(t, channel)
			assert.Equal(t, "vip", group)
		} else {
			require.NotNil(t, err)
			assert.Equal(t, http.StatusForbidden, err.StatusCode)
			assert.Nil(t, channel)
		}
	}
}

// 默认密钥的标识不可由客户端伪造，停用后补齐也不能重新启用它。
func TestDefaultMediaKeyLifecycleAndDeletionProtection(t *testing.T) {
	user := setupTokenAutoGroupsControllerTest(t)
	require.NoError(t, model.DB.AutoMigrate(&model.Log{}))
	t.Setenv("LOG_SQL_DSN", "")
	oldMaster, oldQuota := common.IsMasterNode, common.QuotaForNewUser
	common.IsMasterNode, common.QuotaForNewUser = false, 0
	t.Cleanup(func() { common.IsMasterNode, common.QuotaForNewUser = oldMaster, oldQuota })
	require.NoError(t, model.InitLogDB())
	require.NoError(t, model.BackfillDefaultMediaTokens(model.DB))
	var token model.Token
	require.NoError(t, model.DB.Where("default_for_user = ?", user.Id).First(&token).Error)
	assert.Equal(t, model.TokenPurposeMedia, token.Purpose)
	assert.Len(t, token.Key, 48)
	assert.True(t, token.UnlimitedQuota)
	assert.EqualValues(t, -1, token.ExpiredTime)
	ctx, rec := newTokenAutoGroupsAuthenticatedContext(t, http.MethodGet, "/api/token/?purpose=media", nil, user.Id)
	GetAllTokens(ctx)
	assert.Contains(t, rec.Body.String(), `"is_default":true`)
	assert.NotContains(t, rec.Body.String(), token.Key)
	assert.NotContains(t, rec.Body.String(), "default_for_user")
	require.Error(t, model.DeleteTokenById(token.Id, user.Id))
	extra := seedToken(t, model.DB, user.Id, "extra", "extra-key")
	count, err := model.BatchDeleteTokens([]int{token.Id, extra.Id}, user.Id)
	require.Error(t, err)
	assert.Zero(t, count)
	require.NoError(t, model.DB.First(&model.Token{}, extra.Id).Error)
	ctx, rec = newTokenAutoGroupsAuthenticatedContext(t, http.MethodPut, "/api/token/?status_only=true", map[string]any{"id": token.Id, "status": common.TokenStatusDisabled, "default_for_user": nil, "is_default": false}, user.Id)
	UpdateToken(ctx)
	require.True(t, decodeAPIResponse(t, rec).Success)
	require.NoError(t, model.BackfillDefaultMediaTokens(model.DB))
	var unchanged model.Token
	require.NoError(t, model.DB.First(&unchanged, token.Id).Error)
	assert.Equal(t, token.Key, unchanged.Key)
	assert.Equal(t, common.TokenStatusDisabled, unchanged.Status)
	require.NotNil(t, unchanged.DefaultForUser)
	count, err = model.BatchDeleteTokens([]int{extra.Id}, user.Id)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	// 普通注册和第三方登录注册均在创建账号的事务中生成默认密钥。
	for _, transactional := range []bool{false, true} {
		fresh := &model.User{Username: fmt.Sprintf("media-register-%t", transactional), Group: "default", Status: common.UserStatusEnabled}
		if transactional {
			require.NoError(t, model.DB.Transaction(func(tx *gorm.DB) error { return fresh.InsertWithTx(tx, 0) }))
		} else {
			require.NoError(t, fresh.Insert(0))
		}
		var defaults int64
		require.NoError(t, model.DB.Model(&model.Token{}).Where("default_for_user = ?", fresh.Id).Count(&defaults).Error)
		assert.EqualValues(t, 1, defaults)
	}
	// 密钥写入失败必须回滚账号，不能出现注册成功但没有默认 Key。
	require.NoError(t, model.DB.Callback().Create().Before("gorm:create").Register("media-default:fail", func(tx *gorm.DB) {
		if tx.Statement.Table == "tokens" {
			_ = tx.AddError(fmt.Errorf("fixture storage failure"))
		}
	}))
	t.Cleanup(func() { require.NoError(t, model.DB.Callback().Create().Remove("media-default:fail")) })
	fresh := &model.User{Username: "media-register-fail"}
	require.Error(t, fresh.Insert(0))
	var users int64
	require.NoError(t, model.DB.Model(&model.User{}).Where("username = ?", fresh.Username).Count(&users).Error)
	assert.Zero(t, users)
}
