package controller

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type registerResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

func setupUserRegisterTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	gin.SetMode(gin.TestMode)
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)

	model.DB = db
	model.LOG_DB = db
	require.NoError(t, db.AutoMigrate(&model.User{}))

	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})

	return db
}

func withRegisterTestOptions(t *testing.T) {
	t.Helper()

	originalRegisterEnabled := common.RegisterEnabled
	originalPasswordRegisterEnabled := common.PasswordRegisterEnabled
	originalEmailVerificationEnabled := common.EmailVerificationEnabled
	originalQuotaForNewUser := common.QuotaForNewUser
	originalQuotaForInviter := common.QuotaForInviter
	originalQuotaForInvitee := common.QuotaForInvitee
	originalGenerateDefaultToken := constant.GenerateDefaultToken

	common.RegisterEnabled = true
	common.PasswordRegisterEnabled = true
	common.EmailVerificationEnabled = false
	common.QuotaForNewUser = 0
	common.QuotaForInviter = 0
	common.QuotaForInvitee = 0
	constant.GenerateDefaultToken = false

	t.Cleanup(func() {
		common.RegisterEnabled = originalRegisterEnabled
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
	db := setupUserRegisterTestDB(t)
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
	db := setupUserRegisterTestDB(t)
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
}
