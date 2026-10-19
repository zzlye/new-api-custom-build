package controller

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type registerResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

func setupUserRegisterTestDB(t *testing.T, dialect string) *gorm.DB {
	t.Helper()

	gin.SetMode(gin.TestMode)
	originalDB, originalLogDB, originalRedis := model.DB, model.LOG_DB, common.RedisEnabled
	originalMainType, originalLogType := common.MainDatabaseType(), common.LogDatabaseType()
	common.SetDatabaseTypes(common.DatabaseType(dialect), common.DatabaseType(dialect))
	common.RedisEnabled = false

	dsn := os.Getenv("TEST_" + strings.ToUpper(dialect) + "_DSN")
	db, _ := newAuditTestDatabase(t, dialect, dsn)

	model.DB = db
	model.LOG_DB = db
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Option{}, &model.Log{}))

	t.Cleanup(func() {
		model.DB, model.LOG_DB, common.RedisEnabled = originalDB, originalLogDB, originalRedis
		common.SetDatabaseTypes(originalMainType, originalLogType)
	})

	return db
}

func withRegisterTestOptions(t *testing.T) {
	t.Helper()

	originalRegisterEnabled := common.RegisterEnabled
	originalInviteCodeRequired := common.RegisterInviteCodeRequired
	originalOptions := common.OptionMap
	originalPasswordRegisterEnabled := common.PasswordRegisterEnabled
	originalEmailVerificationEnabled := common.EmailVerificationEnabled
	originalQuotaForNewUser := common.QuotaForNewUser
	originalQuotaForInviter := common.QuotaForInviter
	originalQuotaForInvitee := common.QuotaForInvitee
	originalGenerateDefaultToken := constant.GenerateDefaultToken

	common.RegisterEnabled = true
	common.RegisterInviteCodeRequired = true
	common.OptionMap = maps.Clone(originalOptions)
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string)
	}
	common.PasswordRegisterEnabled = true
	common.EmailVerificationEnabled = false
	common.QuotaForNewUser = 0
	common.QuotaForInviter = 0
	common.QuotaForInvitee = 0
	constant.GenerateDefaultToken = false

	t.Cleanup(func() {
		common.RegisterEnabled = originalRegisterEnabled
		common.RegisterInviteCodeRequired = originalInviteCodeRequired
		common.OptionMap = originalOptions
		common.PasswordRegisterEnabled = originalPasswordRegisterEnabled
		common.EmailVerificationEnabled = originalEmailVerificationEnabled
		common.QuotaForNewUser = originalQuotaForNewUser
		common.QuotaForInviter = originalQuotaForInviter
		common.QuotaForInvitee = originalQuotaForInvitee
		constant.GenerateDefaultToken = originalGenerateDefaultToken
	})
}

func callRegister(t *testing.T, payload string) (*httptest.ResponseRecorder, registerResponse) {
	t.Helper()

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/user/register", bytes.NewBufferString(payload))
	ctx.Request.Header.Set("Content-Type", "application/json")

	Register(ctx)

	var response registerResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	return recorder, response
}

func TestRegisterRejectsInvalidAffCodeWithoutCreatingUser(t *testing.T) {
	db := setupUserRegisterTestDB(t, "sqlite")
	withRegisterTestOptions(t)

	recorder, response := callRegister(t, `{
		"username": "badinvite",
		"password": "password123",
		"aff_code": "not-exist"
	}`)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.False(t, response.Success)
	require.Equal(t, "邀请码无效", response.Message)

	var count int64
	require.NoError(t, db.Unscoped().Model(&model.User{}).Where("username = ?", "badinvite").Count(&count).Error)
	require.Zero(t, count)
}

func TestRegisterWithValidAffCodeCreatesUserWithInviter(t *testing.T) {
	db := setupUserRegisterTestDB(t, "sqlite")
	withRegisterTestOptions(t)

	inviter := model.User{
		Username:    "inviter",
		Password:    "password123",
		DisplayName: "inviter",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		AffCode:     "GOOD",
	}
	require.NoError(t, db.Create(&inviter).Error)

	recorder, response := callRegister(t, `{
		"username": "invitee",
		"password": "password123",
		"aff_code": "GOOD"
	}`)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.True(t, response.Success)

	var created model.User
	require.NoError(t, db.Where("username = ?", "invitee").First(&created).Error)
	require.Equal(t, inviter.Id, created.InviterId)

	var updatedInviter model.User
	require.NoError(t, db.First(&updatedInviter, inviter.Id).Error)
	require.Equal(t, 1, updatedInviter.AffCount)
}

func TestRegisterInviterRewardCreditsBalanceImmediately(t *testing.T) {
	db := setupUserRegisterTestDB(t, "sqlite")
	withRegisterTestOptions(t)

	originalPaymentSetting := *operation_setting.GetPaymentSetting()
	operation_setting.GetPaymentSetting().ComplianceConfirmed = true
	operation_setting.GetPaymentSetting().ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
	common.QuotaForInviter = 100
	t.Cleanup(func() {
		*operation_setting.GetPaymentSetting() = originalPaymentSetting
	})

	inviter := model.User{
		Username:    "reward-inviter",
		Password:    "password123",
		DisplayName: "reward-inviter",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		AffCode:     "REWARD",
	}
	require.NoError(t, db.Create(&inviter).Error)

	recorder, response := callRegister(t, `{
		"username": "reward-invitee",
		"password": "password123",
		"aff_code": "REWARD"
	}`)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.True(t, response.Success)

	var updatedInviter model.User
	require.NoError(t, db.First(&updatedInviter, inviter.Id).Error)
	require.Equal(t, 1, updatedInviter.AffCount)
	require.Equal(t, 100, updatedInviter.Quota)
	require.Zero(t, updatedInviter.AffQuota)
	require.Equal(t, 100, updatedInviter.AffHistoryQuota)
}

// 同一组注册和设置行为覆盖三种真实数据库，不修改生产账号或已有奖励规则。
func TestRegisterInvitationPolicyDatabaseMatrix(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			if dialect != "sqlite" && os.Getenv("TEST_"+strings.ToUpper(dialect)+"_DSN") == "" {
				t.Skip("未配置真实数据库")
			}
			db := setupUserRegisterTestDB(t, dialect)
			withRegisterTestOptions(t)
			versionSQL := "SELECT VERSION()"
			if dialect == "sqlite" {
				versionSQL = "SELECT sqlite_version()"
			}
			var version string
			require.NoError(t, db.Raw(versionSQL).Scan(&version).Error)
			t.Log("数据库版本：", version)

			// 新增设置缺省为必填；无需数据库迁移或修改老账号。
			require.True(t, common.RegisterInviteCodeRequired)
			var optionCount int64
			require.NoError(t, db.Model(&model.Option{}).Count(&optionCount).Error)
			require.Zero(t, optionCount)
			_, missing := callRegister(t, `{"username":"default-missing","password":"password123"}`)
			require.False(t, missing.Success)

			t.Run("仅根用户可保存并公开当前规则", func(t *testing.T) {
				for _, role := range []int{0, common.RoleCommonUser, common.RoleAdminUser, common.RoleRootUser} {
					recorder := httptest.NewRecorder()
					ctx, _ := gin.CreateTestContext(recorder)
					ctx.Set("role", role)
					ctx.Request = httptest.NewRequest(http.MethodPut, "/api/option/", strings.NewReader(`{"key":"RegisterInviteCodeRequired","value":false}`))
					UpdateOption(ctx)
					if role != common.RoleRootUser {
						require.Equal(t, http.StatusForbidden, recorder.Code)
						require.True(t, common.RegisterInviteCodeRequired)
						continue
					}
					var response registerResponse
					require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
					require.True(t, response.Success)
					require.False(t, common.RegisterInviteCodeRequired)
				}
				for _, value := range []bool{false, true} {
					require.NoError(t, model.UpdateOption("RegisterInviteCodeRequired", strconv.FormatBool(value)))
					var saved model.Option
					require.NoError(t, db.Where(&model.Option{Key: "RegisterInviteCodeRequired"}).First(&saved).Error)
					require.Equal(t, strconv.FormatBool(value), saved.Value)
					recorder := httptest.NewRecorder()
					ctx, _ := gin.CreateTestContext(recorder)
					ctx.Request = httptest.NewRequest(http.MethodGet, "/api/status", nil)
					GetStatus(ctx)
					var status struct {
						Data map[string]any `json:"data"`
					}
					require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &status))
					require.Equal(t, value, status.Data["register_invite_code_required"])
				}
				for _, invalid := range []string{"", "0", "null", "disabled"} {
					require.Error(t, model.UpdateOption("RegisterInviteCodeRequired", invalid))
					require.True(t, common.RegisterInviteCodeRequired)
				}
			})

			t.Run("保存失败不放宽注册限制", func(t *testing.T) {
				const callback = "test:reject_invite_option"
				require.NoError(t, db.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
					tx.AddError(errors.New("模拟设置写入失败"))
				}))
				t.Cleanup(func() { _ = db.Callback().Update().Remove(callback) })
				require.Error(t, model.UpdateOption("RegisterInviteCodeRequired", "false"))
				require.True(t, common.RegisterInviteCodeRequired)
			})

			for index, tc := range []struct {
				name, code                                               string
				required, success, registerOff, passwordOff, verifyEmail bool
			}{
				{name: "必填时空白拒绝", required: true, code: "  "},
				{name: "必填时有效码成功", required: true, code: " GOOD ", success: true},
				{name: "必填时无效码拒绝", required: true, code: "INVALID"},
				{name: "选填时不填写成功", success: true},
				{name: "选填时空白成功", code: "  ", success: true},
				{name: "选填时有效码仍保留邀请奖励", code: " GOOD ", success: true},
				{name: "选填时无效码仍拒绝", code: "INVALID"},
				{name: "选填不绕过注册总开关", registerOff: true},
				{name: "选填不绕过密码注册开关", passwordOff: true},
				{name: "选填不绕过邮箱验证", verifyEmail: true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					withRegisterTestOptions(t)
					common.RegisterEnabled = !tc.registerOff
					common.PasswordRegisterEnabled = !tc.passwordOff
					common.EmailVerificationEnabled = tc.verifyEmail
					require.NoError(t, model.UpdateOption("RegisterInviteCodeRequired", strconv.FormatBool(tc.required)))
					paymentSetting := *operation_setting.GetPaymentSetting()
					t.Cleanup(func() { *operation_setting.GetPaymentSetting() = paymentSetting })
					operation_setting.GetPaymentSetting().ComplianceConfirmed = true
					operation_setting.GetPaymentSetting().ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
					common.QuotaForNewUser, common.QuotaForInvitee, common.QuotaForInviter = 20, 30, 40
					inviter := model.User{Username: fmt.Sprintf("inviter-%d", index), AffCode: fmt.Sprintf("GOOD-%d", index), Status: common.UserStatusEnabled}
					require.NoError(t, db.Create(&inviter).Error)
					code := strings.ReplaceAll(tc.code, "GOOD", inviter.AffCode)
					username := fmt.Sprintf("invitee-%d", index)
					payload := map[string]any{"username": username, "password": "password123", "role": common.RoleRootUser, "quota": 9999, "inviter_id": inviter.Id, "register_invite_code_required": false}
					if code != "" {
						payload["aff_code"] = code
					}
					body, err := common.Marshal(payload)
					require.NoError(t, err)
					_, response := callRegister(t, string(body))
					require.Equal(t, tc.success, response.Success, response.Message)
					var created model.User
					err = db.Where("username = ?", username).First(&created).Error
					if !tc.success {
						require.ErrorIs(t, err, gorm.ErrRecordNotFound)
						return
					}
					require.NoError(t, err)
					require.Equal(t, common.RoleCommonUser, created.Role)
					require.NoError(t, db.First(&inviter, inviter.Id).Error)
					if strings.TrimSpace(code) == "" {
						require.Zero(t, created.InviterId)
						require.Zero(t, inviter.AffCount)
						require.Zero(t, inviter.Quota)
						require.Equal(t, 20, created.Quota)
						require.Equal(t, 20, created.GiftQuota)
					} else {
						require.Equal(t, inviter.Id, created.InviterId)
						require.Equal(t, 1, inviter.AffCount)
						require.Equal(t, 40, inviter.Quota)
						require.Equal(t, 40, inviter.GiftQuota)
						require.Equal(t, 50, created.Quota)
						require.Equal(t, 50, created.GiftQuota)
					}
				})
			}
		})
	}
}
