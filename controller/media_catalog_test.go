package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestMediaCatalogFiltersModelsAndGroups(t *testing.T) {
	pricing := []model.Pricing{
		{ModelName: "allowed", ModelPrice: 0.02, EnableGroup: []string{"private", "adobe", "az"}},
		{ModelName: "denied", EnableGroup: []string{"az"}},
		{ModelName: "all-model", EnableGroup: []string{"all"}},
	}
	groups := []string{"az", "adobe"}
	ratios := map[string]float64{"az": 3.6, "adobe": 4}
	result := buildMediaCatalog(pricing, []string{"allowed", "all-model"}, groups, ratios)
	rows := result["data"].([]model.Pricing)
	require.Len(t, rows, 2)
	require.Equal(t, groups, rows[0].EnableGroup)
	require.Equal(t, groups, rows[1].EnableGroup)
	require.Equal(t, 0.02, rows[0].ModelPrice)
	// 目录过滤不能修改全站共享的价格缓存。
	require.Equal(t, []string{"private", "adobe", "az"}, pricing[0].EnableGroup)
	require.Empty(t, buildMediaCatalog(pricing, nil, groups, ratios)["data"])
}

func TestMediaCatalogIncludedOnlyForMediaKey(t *testing.T) {
	withSelfUseModeDisabled(t)
	withTieredBillingConfig(t, map[string]string{"catalog-image": "tiered_expr"}, map[string]string{"catalog-image": `tier("base", p * 5 + img_o * 30)`})
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.Create(&model.User{Id: 1001, Username: "catalog-user", Password: "password", Group: "default", Status: common.UserStatusEnabled}).Error)
	require.NoError(t, db.Create(&model.Ability{Group: "default", Model: "catalog-image", ChannelId: 1, Enabled: true}).Error)
	for _, media := range []bool{false, true} {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
		ctx.Set("id", 1001)
		if media {
			ctx.Set("media_key_groups", []string{"default"})
			common.SetContextKey(ctx, constant.ContextKeyTokenGroup, "auto")
		}
		ListModels(ctx, constant.ChannelTypeOpenAI)
		require.Equal(t, http.StatusOK, recorder.Code)
		var response map[string]any
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
		if !media {
			require.NotContains(t, response, "media_catalog")
			continue
		}
		catalog := response["media_catalog"].(map[string]any)
		require.Equal(t, []any{"default"}, catalog["groups"])
		require.Len(t, catalog["data"], 1)
		row := catalog["data"].([]any)[0].(map[string]any)
		require.Equal(t, "catalog-image", row["model_name"])
		require.Equal(t, "tiered_expr", row["billing_mode"])
		require.Contains(t, catalog, "status")
	}
}
