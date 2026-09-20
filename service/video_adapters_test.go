package service

import (
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/video_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestVideoAdaptersFilterChannelCombinationsAndPermissions(t *testing.T) {
	db := setupChannelSelectAutoGroupsTest(t)
	createChannelSelectAutoGroupsChannel(t, db, 1, "default", "shared")
	createChannelSelectAutoGroupsChannel(t, db, 2, "default", "shared")
	createChannelSelectAutoGroupsChannel(t, db, 3, "default", "shared")
	mapping := `{"shared":"actual"}`
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 1).Update("model_mapping", mapping).Error)
	a := video_setting.BuiltinTemplates()["wan-3.0"]
	b := video_setting.BuiltinTemplates()["seedance-2-pro"]
	legacy := video_setting.BuiltinTemplates()["seedance-2-pro"]
	legacy.ID = "legacy"
	legacy.Protocol.Capabilities = nil
	registry := video_setting.Registry{Templates: []video_setting.Template{a, b, legacy}, Rules: []video_setting.Rule{
		{ID: "a", Enabled: true, ChannelIDs: []int{1}, Models: []string{"actual"}, TemplateID: a.ID},
		{ID: "b", Enabled: true, ChannelIDs: []int{2}, Models: []string{"shared"}, TemplateID: b.ID},
		{ID: "legacy", Enabled: true, ChannelIDs: []int{3}, TemplateID: legacy.ID},
	}}
	data, err := common.Marshal(registry)
	require.NoError(t, err)
	common.OptionMapRWMutex.Lock()
	old := common.OptionMap
	common.OptionMap = map[string]string{video_setting.RegistryKey: string(data)}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() { common.OptionMapRWMutex.Lock(); common.OptionMap = old; common.OptionMapRWMutex.Unlock() })
	model.InitChannelCache()
	for _, cache := range []bool{false, true} {
		t.Run(fmt.Sprint(cache), func(t *testing.T) {
			common.MemoryCacheEnabled = cache
			for _, tc := range []struct {
				seconds int
				res     string
				channel int
			}{{30, "720p", 1}, {5, "4k", 2}, {30, "4k", 0}} {
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest("POST", "/v1/videos", strings.NewReader(fmt.Sprintf(`{"model":"shared","prompt":"测试","duration":%d,"resolution":"%s"}`, tc.seconds, tc.res)))
				c.Request.Header.Set("Content-Type", "application/json")
				common.SetContextKey(c, constant.ContextKeyUserGroup, "default")
				common.SetContextKey(c, constant.ContextKeyUsingGroup, "default")
				defer common.CleanupBodyStorage(c)
				err := PrepareVideoChannelSelection(c, "shared")
				if tc.channel == 0 {
					require.Error(t, err)
					continue
				}
				require.NoError(t, err)
				assert.True(t, VideoChannelAllowed(c, tc.channel))
				assert.False(t, VideoChannelAllowed(c, 3))
				for _, retry := range []int{0, 1} {
					selected, _, err := CacheGetRandomSatisfiedChannel(&RetryParam{Ctx: c, TokenGroup: "default", ModelName: "shared", RequestPath: "/v1/videos", Retry: &retry})
					require.NoError(t, err)
					require.NotNil(t, selected)
					assert.Equal(t, tc.channel, selected.Id)
				}
				// 指定渠道令牌不能因为另一个渠道支持而越过绑定渠道。
				c2, _ := gin.CreateTestContext(httptest.NewRecorder())
				c2.Request = httptest.NewRequest("POST", "/v1/videos", strings.NewReader(`{"model":"shared","prompt":"测试","duration":30,"resolution":"4k"}`))
				c2.Request.Header.Set("Content-Type", "application/json")
				common.SetContextKey(c2, constant.ContextKeyUserGroup, "default")
				common.SetContextKey(c2, constant.ContextKeyTokenSpecificChannelId, "1")
				require.Error(t, PrepareVideoChannelSelection(c2, "shared"))
				common.CleanupBodyStorage(c2)
				common.SetContextKey(c, constant.ContextKeyTokenModelLimitEnabled, true)
				common.SetContextKey(c, constant.ContextKeyTokenModelLimit, map[string]bool{"other": true})
				_, err = VideoCandidates(c, "shared")
				require.Error(t, err)
			}
		})
	}
}

func TestUnconfiguredVideoKeepsLegacyBodyFormats(t *testing.T) {
	db := setupChannelSelectAutoGroupsTest(t)
	createChannelSelectAutoGroupsChannel(t, db, 1, "default", "legacy")
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/videos", strings.NewReader("not-json"))
	c.Request.Header.Set("Content-Type", "multipart/form-data; boundary=legacy")
	common.SetContextKey(c, constant.ContextKeyUserGroup, "default")
	require.NoError(t, PrepareVideoChannelSelection(c, "legacy"))
	assert.True(t, VideoChannelAllowed(c, 1))
}
