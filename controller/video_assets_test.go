package controller

import (
	"bytes"
	"encoding/base64"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/QuantumNous/new-api/setting/video_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
)

func TestQueuedVideoBindsAssetsFromReferenceAliases(t *testing.T) {
	user, token := prepareAsyncCompatRelay(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("排队时不调用上游") }), "future")
	require.NoError(t, model.DB.AutoMigrate(&model.VideoAsset{}, &model.VideoAssetUse{}))
	png, err := base64.StdEncoding.DecodeString(asyncFixturePNG)
	require.NoError(t, err)
	asset, err := service.SaveVideoAsset(user.Id, bytes.NewReader(png))
	require.NoError(t, err)
	template := video_setting.BuiltinTemplates()["seedance-2-mini"]
	r := video_setting.Registry{Templates: []video_setting.Template{template}, Rules: []video_setting.Rule{{ID: "future", Enabled: true, ChannelIDs: []int{1}, Models: []string{"future"}, TemplateID: template.ID}}}
	require.NoError(t, model.SaveVideoAdapters(&r))
	body, err := common.Marshal(map[string]any{"model": "future", "prompt": "镜头", "duration": 5, "reference_images": []any{map[string]any{"asset_id": asset.ID}}})
	require.NoError(t, err)
	engine := gin.New()
	engine.POST("/v1/videos", middleware.TokenAuth(), middleware.Distribute(), RelayTask)
	request := httptest.NewRequest("POST", "/v1/videos", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer sk-"+token.Key)
	request.Header.Set("Prefer", "respond-async")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	require.Equal(t, 202, response.Code, response.Body.String())
	var use model.VideoAssetUse
	require.NoError(t, model.DB.Where("asset_id = ?", asset.ID).First(&use).Error)
	assert.True(t, strings.HasPrefix(use.TaskID, "async_"))
}

func TestVideoAssetOwnershipSignatureRetentionAndUpload(t *testing.T) {
	prepareAsyncMediaController(t)
	require.NoError(t, model.DB.AutoMigrate(&model.VideoAsset{}, &model.VideoAssetUse{}))
	old := system_setting.ServerAddress
	system_setting.ServerAddress = "https://assets.example"
	t.Cleanup(func() { system_setting.ServerAddress = old })
	png, err := base64.StdEncoding.DecodeString(asyncFixturePNG)
	require.NoError(t, err)
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("file", "image.png")
	require.NoError(t, err)
	_, err = part.Write(png)
	require.NoError(t, err)
	require.NoError(t, form.Close())
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Set("id", 11)
	c.Request = httptest.NewRequest("POST", "/v1/video/assets", &body)
	c.Request.Header.Set("Content-Type", form.FormDataContentType())
	UploadVideoAsset(c)
	require.Equal(t, 201, rec.Code, rec.Body.String())
	var asset model.VideoAsset
	require.NoError(t, common.Unmarshal(rec.Body.Bytes(), &asset))
	require.NoError(t, model.DB.First(&asset, "id = ?", asset.ID).Error)
	source := func() map[string]any {
		return map[string]any{"image_urls": []any{map[string]any{"asset_id": asset.ID}, "https://cdn.test/second.png"}}
	}
	require.Error(t, service.ValidateVideoAssets(source(), 12))
	task := model.AsyncRelayTask{TaskID: "async_asset", UserID: 11, Status: model.AsyncRelayTaskStatusWaiting}
	require.NoError(t, model.DB.Create(&task).Error)
	require.NoError(t, service.BindVideoAssets(source(), 11, task.TaskID))
	input := source()
	require.NoError(t, service.ResolveVideoMedia(input, 11, task.TaskID, true))
	refs := input["image_urls"].([]any)
	assert.Equal(t, "https://cdn.test/second.png", refs[1])
	signed, err := url.Parse(refs[0].(string))
	require.NoError(t, err)
	read := func(query string) *httptest.ResponseRecorder {
		r := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(r)
		c.Params = gin.Params{{Key: "asset_id", Value: asset.ID}}
		c.Request = httptest.NewRequest("GET", signed.Path+"?"+query, nil)
		GetVideoAssetContent(c)
		return r
	}
	response := read(signed.RawQuery)
	require.Equal(t, 200, response.Code)
	assert.Equal(t, png, response.Body.Bytes())
	query := signed.Query()
	query.Set("signature", "00")
	assert.Equal(t, 401, read(query.Encode()).Code)
	require.NoError(t, model.DB.Model(&asset).Update("created_at", common.GetTimestamp()-172800).Error)
	require.NoError(t, model.CleanupVideoAssets())
	_, err = os.Stat(asset.Path)
	require.NoError(t, err)
	require.NoError(t, service.ValidateVideoAssets(source(), 11))
	require.NoError(t, model.DB.Model(&task).Updates(map[string]any{"status": model.AsyncRelayTaskStatusSucceeded, "finished_at": common.GetTimestamp() - common.AsyncMediaRetentionSeconds() - 1}).Error)
	assert.Equal(t, 410, read(signed.RawQuery).Code)
	require.NoError(t, model.CleanupVideoAssets())
	_, err = os.Stat(asset.Path)
	assert.True(t, os.IsNotExist(err))
	// 未绑定素材按一天清理；内联素材落盘并保持图片、音频、视频各自顺序。
	orphan, err := service.SaveVideoAsset(11, bytes.NewReader(png))
	require.NoError(t, err)
	require.NoError(t, model.CleanupVideoAssets())
	_, err = os.Stat(orphan.Path)
	require.NoError(t, err)
	require.NoError(t, model.DB.Model(orphan).Update("created_at", common.GetTimestamp()-86401).Error)
	require.Error(t, service.ValidateVideoAssets(map[string]any{"first_frame": map[string]any{"asset_id": orphan.ID}}, 11))
	require.NoError(t, model.CleanupVideoAssets())
	_, err = os.Stat(orphan.Path)
	assert.True(t, os.IsNotExist(err))
	inline := map[string]any{"first_frame": "data:image/png;base64," + asyncFixturePNG}
	require.NoError(t, service.ValidateVideoAssets(inline, 11))
	require.NoError(t, service.ResolveVideoMedia(inline, 11, "pending-task", true))
	assert.True(t, strings.HasPrefix(inline["first_frame"].(string), "https://assets.example/v1/video/assets/"))
	assert.Error(t, service.ValidateVideoAssets(map[string]any{"audio_urls": []any{"data:audio/wav;base64," + asyncFixturePNG}}, 11))
}

func TestVideoAssetDetectsAudioAndVideoContent(t *testing.T) {
	prepareAsyncMediaController(t)
	require.NoError(t, model.DB.AutoMigrate(&model.VideoAsset{}, &model.VideoAssetUse{}))
	for _, tc := range []struct {
		kind string
		data []byte
	}{{"video", []byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'm', 'p', '4', '2', 0, 0, 0, 0, 'm', 'p', '4', '2', 'i', 's', 'o', 'm'}}, {"audio", append([]byte("RIFF\x00\x00\x00\x00WAVEfmt "), make([]byte, 32)...)}, {"audio", append([]byte("ID3\x04\x00\x00\x00\x00\x00\x00"), make([]byte, 32)...)}} {
		asset, err := service.SaveVideoAsset(11, bytes.NewReader(tc.data))
		require.NoError(t, err)
		assert.Equal(t, tc.kind, asset.Kind)
	}
}
