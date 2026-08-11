package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type tokenUsageAccountResponse struct {
	Code bool `json:"code"`
	Data struct {
		Account struct {
			Quota     int `json:"quota"`
			UsedQuota int `json:"used_quota"`
		} `json:"account"`
	} `json:"data"`
}

func TestGetTokenUsageReturnsOwningAccountBalance(t *testing.T) {
	db := setupTokenControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.User{}))

	user := &model.User{
		Id:        42,
		Username:  "token-usage-owner",
		Password:  "password",
		Status:    common.UserStatusEnabled,
		Quota:     3_420_000,
		UsedQuota: 1_580_000,
		Group:     "default",
		AffCode:   "usage-owner-code",
	}
	require.NoError(t, db.Create(user).Error)
	token := seedToken(t, db, user.Id, "account-balance-token", "boundaccountkey123456")

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/usage/token/", nil)
	ctx.Request.Header.Set("Authorization", "Bearer sk-boundaccountkey123456")
	ctx.Set("token_id", token.Id)

	GetTokenUsage(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response tokenUsageAccountResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.True(t, response.Code)
	assert.Equal(t, user.Quota, response.Data.Account.Quota)
	assert.Equal(t, user.UsedQuota, response.Data.Account.UsedQuota)
}
