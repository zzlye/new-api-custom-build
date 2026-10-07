package controller

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func enqueueDetailFixture(t *testing.T, body []byte, contentType string, video ...bool) *model.AsyncRelayTask {
	t.Helper()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	path, format := "/v1/images/edits?async=true", relaytypes.RelayFormat(relaytypes.RelayFormatOpenAIImage)
	if len(video) > 0 && video[0] {
		path, format = "/v1/videos?async=true", relaytypes.RelayFormatTask
	}
	c.Request = httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", contentType)
	c.Request.Header.Set("Authorization", "Bearer private-header")
	c.Set("id", 31)
	c.Set("token_id", 9)
	defer common.CleanupBodyStorage(c)
	require.NoError(t, EnqueueAsyncRelayRequest(c, format))
	var task model.AsyncRelayTask
	require.NoError(t, model.DB.Order("id desc").First(&task).Error)
	return &task
}

func TestAsyncTaskDetailsKeepPromptReferencesAndPermissionBoundary(t *testing.T) {
	prepareAsyncMediaController(t)
	body := fmt.Sprintf(`{"model":"gpt-image-2","prompt":"保留人物，改成晴天","size":"1280x720","seed":9007199254740993,"background":false,"image":["data:image/png;base64,%s"],"api_key":"private-body"}`, asyncFixturePNG)
	task := enqueueDetailFixture(t, []byte(body), "application/json")
	var saved model.AsyncRelayRequestDetails
	require.NoError(t, common.UnmarshalJsonStr(task.RequestDetails, &saved))
	require.Len(t, saved.References, 1)
	require.FileExists(t, saved.References[0].Path)
	assert.Equal(t, "9007199254740993", saved.Parameters["seed"])
	assert.Equal(t, "false", saved.Parameters["background"])
	params := gin.Params{{Key: "task_id", Value: task.TaskID}}
	response := asyncControllerRequest(GetAsyncRelayTaskDetails, http.MethodGet, "/details", 31, common.RoleCommonUser, params, "")
	require.Equal(t, http.StatusOK, response.Code)
	assert.Contains(t, response.Body.String(), "保留人物，改成晴天")
	assert.Contains(t, response.Body.String(), "/v1/images/edits")
	assert.Contains(t, response.Body.String(), "/reference/0")
	assert.NotContains(t, response.Body.String(), "private-body")
	assert.NotContains(t, response.Body.String(), "private-header")
	assert.NotContains(t, response.Body.String(), saved.References[0].Path)
	assert.NotContains(t, response.Body.String(), asyncFixturePNG)
	hidden := asyncControllerRequest(GetAsyncRelayTaskDetails, http.MethodGet, "/details", 32, common.RoleCommonUser, params, "")
	assert.Equal(t, http.StatusNotFound, hidden.Code)
	referenceParams := append(append(gin.Params{}, params...), gin.Param{Key: "index", Value: "0"})
	reference := asyncControllerRequest(GetAsyncRelayReference, http.MethodGet, "/reference/0", 31, common.RoleCommonUser, referenceParams, "")
	require.Equal(t, http.StatusOK, reference.Code)
	png, err := base64.StdEncoding.DecodeString(asyncFixturePNG)
	require.NoError(t, err)
	assert.Equal(t, png, reference.Body.Bytes())
	assert.Equal(t, "image/png", reference.Header().Get("Content-Type"))
	other := asyncControllerRequest(GetAsyncRelayReference, http.MethodGet, "/reference/0", 32, common.RoleCommonUser, referenceParams, "")
	assert.Equal(t, http.StatusNotFound, other.Code)
	admin := asyncControllerRequest(GetAsyncRelayReference, http.MethodGet, "/reference/0", 32, common.RoleAdminUser, referenceParams, "")
	assert.Equal(t, http.StatusOK, admin.Code)
	// 输入快照和媒体共用保留期限；任务状态及路由日志仍保留。
	task.Status, task.FinishedAt, task.RequestQuery = model.AsyncRelayTaskStatusSucceeded, common.GetTimestamp()-7201, "seed=9007199254740993&temperature=0.4"
	require.NoError(t, model.DB.Save(task).Error)
	expired := asyncControllerRequest(GetAsyncRelayReference, http.MethodGet, "/reference/0", 31, common.RoleCommonUser, referenceParams, "")
	assert.Equal(t, http.StatusGone, expired.Code)
	require.NoError(t, model.ExpireAsyncRelayTaskFiles(task))
	assert.NoFileExists(t, saved.References[0].Path)
	require.NoError(t, model.DB.First(task, task.ID).Error)
	assert.Empty(t, task.RequestQuery)
	saved = model.AsyncRelayRequestDetails{}
	require.NoError(t, common.UnmarshalJsonStr(task.RequestDetails, &saved))
	assert.Empty(t, saved.Prompt)
	assert.Empty(t, saved.Parameters)
	assert.Empty(t, saved.References)
	assert.True(t, saved.InputsExpired)
	after := asyncControllerRequest(GetAsyncRelayTaskDetails, http.MethodGet, "/details", 31, common.RoleCommonUser, params, "")
	assert.NotContains(t, after.Body.String(), "保留人物，改成晴天")
	assert.Contains(t, after.Body.String(), `"inputs_expired":true`)
	assert.NotContains(t, after.Body.String(), "/reference/0")
}

func TestAsyncTaskDetailsPreserveMultipartImageAndMask(t *testing.T) {
	prepareAsyncMediaController(t)
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	require.NoError(t, form.WriteField("prompt", "仅修改遮罩区域"))
	require.NoError(t, form.WriteField("model", "gpt-image-2"))
	require.NoError(t, form.WriteField("quality", "high"))
	png, err := base64.StdEncoding.DecodeString(asyncFixturePNG)
	require.NoError(t, err)
	for _, name := range []string{"image[]", "mask"} {
		part, err := form.CreateFormFile(name, "参考.png")
		require.NoError(t, err)
		_, err = part.Write(png)
		require.NoError(t, err)
	}
	require.NoError(t, form.Close())
	task := enqueueDetailFixture(t, body.Bytes(), form.FormDataContentType())
	var details model.AsyncRelayRequestDetails
	require.NoError(t, common.UnmarshalJsonStr(task.RequestDetails, &details))
	assert.Equal(t, "仅修改遮罩区域", details.Prompt)
	assert.Equal(t, "high", details.Parameters["quality"])
	require.Len(t, details.References, 2)
	assert.Equal(t, "reference", details.References[0].Role)
	assert.Equal(t, "mask", details.References[1].Role)
	for _, reference := range details.References {
		assert.FileExists(t, reference.Path)
		assert.Equal(t, "image/png", reference.ContentType)
	}
	require.NoError(t, common.RemoveAsyncMediaFile(task.RequestFilePath))
	// 原表单清理后仍可查看快照，不依赖前台连接或第三方客户端缓存。
	image := asyncControllerRequest(GetAsyncRelayReference, http.MethodGet, "/reference/1", 31, common.RoleCommonUser, gin.Params{{Key: "task_id", Value: task.TaskID}, {Key: "index", Value: "1"}}, "")
	assert.Equal(t, http.StatusOK, image.Code)
}

func TestAsyncTaskDetailsLegacyRevisedPromptIsNotClaimedAsOriginal(t *testing.T) {
	prepareAsyncMediaController(t)
	task := createCompletedAsyncImage(t, 31)
	payload := []byte(`{"data":[{"revised_prompt":"上游调整后的提示词"}]}`)
	require.NoError(t, os.WriteFile(task.ResultFilePath, payload, 0600))
	task.ResponseFilePath, task.ResponseContentType = task.ResultFilePath, "application/json"
	require.NoError(t, model.DB.Save(task).Error)
	response := asyncControllerRequest(GetAsyncRelayTaskDetails, http.MethodGet, "/details", 31, common.RoleCommonUser, gin.Params{{Key: "task_id", Value: task.TaskID}}, "")
	assert.Equal(t, http.StatusOK, response.Code)
	assert.Contains(t, response.Body.String(), `"prompt_source":"upstream_revised"`)
	assert.Contains(t, response.Body.String(), `"input_available":false`)
	assert.Contains(t, response.Body.String(), "上游调整后的提示词")
}

func TestAsyncTaskDetailsRecognizeChatResponsesAndGeminiInputs(t *testing.T) {
	fixtures := []struct{ name, body, prompt string }{
		{"纯 Base64", fmt.Sprintf(`{"prompt":"使用原图","image":"%s"}`, asyncFixturePNG), "使用原图"},
		{"聊天", fmt.Sprintf(`{"model":"gpt-image-2","messages":[{"role":"user","content":[{"type":"text","text":"改成水彩"},{"type":"image_url","image_url":{"url":"data:image/png;base64,%s"}}]}]}`, asyncFixturePNG), "改成水彩"},
		{"Responses", fmt.Sprintf(`{"model":"gpt-image-2","input":[{"role":"user","content":[{"type":"input_text","text":"增加云朵"},{"type":"input_image","image_url":"data:image/png;base64,%s"}]}],"tools":[{"type":"image_generation"}]}`, asyncFixturePNG), "增加云朵"},
		{"Gemini", fmt.Sprintf(`{"contents":[{"role":"user","parts":[{"text":"保留原色"},{"inlineData":{"mimeType":"image/png","data":"%s"}}]}],"generationConfig":{"imageConfig":{"aspectRatio":"16:9","imageSize":"2K"}}}`, asyncFixturePNG), "保留原色"},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			prepareAsyncMediaController(t)
			task := enqueueDetailFixture(t, []byte(fixture.body), "application/json")
			var details model.AsyncRelayRequestDetails
			require.NoError(t, common.UnmarshalJsonStr(task.RequestDetails, &details))
			assert.Equal(t, fixture.prompt, details.Prompt)
			require.Len(t, details.References, 1)
			assert.FileExists(t, details.References[0].Path)
			assert.Equal(t, "image/png", details.References[0].ContentType)
			if fixture.name == "Gemini" {
				assert.Equal(t, "16:9", details.Parameters["aspect_ratio"])
				assert.Equal(t, "2K", details.Parameters["resolution"])
			}
		})
	}
}

func TestAsyncTaskDetailsHideRemoteReferenceAddressAndExpireIt(t *testing.T) {
	prepareAsyncMediaController(t)
	task := enqueueDetailFixture(t, []byte(`{"model":"gpt-image-2","prompt":"参考这张图","image":"http://127.0.0.1:1/reference.png?token=private-reference"}`), "application/json")
	var input model.AsyncRelayRequestDetails
	require.NoError(t, common.UnmarshalJsonStr(task.RequestDetails, &input))
	require.Len(t, input.References, 1)
	assert.NotEmpty(t, input.References[0].Source)
	assert.Empty(t, input.References[0].Error)
	// 一个不可连接的地址仍能立即入队；展示接口只给出自身的鉴权地址。
	response := asyncControllerRequest(GetAsyncRelayTaskDetails, http.MethodGet, "/details", 31, common.RoleCommonUser, gin.Params{{Key: "task_id", Value: task.TaskID}}, "")
	assert.Equal(t, http.StatusOK, response.Code)
	assert.Contains(t, response.Body.String(), "/reference/0")
	assert.NotContains(t, response.Body.String(), "private-reference")
	assert.NotContains(t, response.Body.String(), "127.0.0.1")
	task.Status, task.FinishedAt = model.AsyncRelayTaskStatusFailed, common.GetTimestamp()-7201
	require.NoError(t, model.DB.Save(task).Error)
	require.NoError(t, model.ExpireAsyncRelayTaskFiles(task))
	fresh, err := model.GetAsyncRelayTaskByTaskID(task.TaskID)
	require.NoError(t, err)
	assert.NotContains(t, fresh.RequestDetails, "private-reference")
	assert.NotContains(t, fresh.RequestDetails, "参考这张图")
	assert.NotContains(t, fresh.RequestDetails, "private-reference")
	var expired model.AsyncRelayRequestDetails
	require.NoError(t, common.UnmarshalJsonStr(fresh.RequestDetails, &expired))
	assert.True(t, expired.InputsExpired)
}

func TestAsyncTaskDetailsCaptureFullSafeParameterSnapshot(t *testing.T) {
	prepareAsyncMediaController(t)
	body := []byte(`{"model":"nano-banana-pro","prompt":"生成一张图","candidateCount":2,"temperature":0.4,"generationConfig":{"imageConfig":{"aspectRatio":"3:2","imageSize":"2K"},"responseModalities":["IMAGE"],"seed":9007199254740993,"systemInstruction":"不要记录这段正文"},"api_key":"private-secret","callback_url":"https://private.example/callback"}`)
	task := enqueueDetailFixture(t, body, "application/json")
	var details model.AsyncRelayRequestDetails
	require.NoError(t, common.UnmarshalJsonStr(task.RequestDetails, &details))
	assert.Equal(t, "2", details.Parameters["candidateCount"])
	assert.Equal(t, "0.4", details.Parameters["temperature"])
	assert.Equal(t, `{"imageConfig":{"aspectRatio":"3:2","imageSize":"2K"},"responseModalities":["IMAGE"],"seed":9007199254740993}`, details.Parameters["generationConfig"])
	assert.Equal(t, "3:2", details.Parameters["aspect_ratio"])
	assert.Equal(t, "2K", details.Parameters["resolution"])
	assert.NotContains(t, task.RequestDetails, "private-secret")
	assert.NotContains(t, task.RequestDetails, "private.example")
	assert.NotContains(t, task.RequestDetails, "不要记录这段正文")
}

func TestAsyncTaskReferenceIsProtectedFromOrphanCleanup(t *testing.T) {
	prepareAsyncMediaController(t)
	task := enqueueDetailFixture(t, []byte(fmt.Sprintf(`{"prompt":"参考图","image":"data:image/png;base64,%s"}`, asyncFixturePNG)), "application/json")
	var input model.AsyncRelayRequestDetails
	require.NoError(t, common.UnmarshalJsonStr(task.RequestDetails, &input))
	require.Len(t, input.References, 1)
	oldTime := time.Now().Add(-3 * time.Hour)
	require.NoError(t, os.Chtimes(input.References[0].Path, oldTime, oldTime))
	orphan, file, err := common.CreateAsyncMediaFile()
	require.NoError(t, err)
	require.NoError(t, file.Close())
	require.NoError(t, os.Chtimes(orphan, oldTime, oldTime))
	require.NoError(t, model.CleanupOrphanAsyncMediaFiles())
	assert.FileExists(t, input.References[0].Path)
	assert.NoFileExists(t, orphan)
}

func TestAsyncTaskListShowsInterfaceAndGenerationDurationWithoutInputBody(t *testing.T) {
	prepareAsyncMediaController(t)
	task := createCompletedAsyncImage(t, 31)
	task.ModelName, task.RequestMethod, task.RequestPath = "gpt-image-2", http.MethodPost, "/v1/images/generations"
	task.ResponseCompletedAt = task.CreatedAt + 50
	task.FinishedAt = task.CreatedAt + 300
	task.RequestDetails = `{"prompt":"只在详情中显示的长提示词"}`
	require.NoError(t, model.DB.Save(task).Error)
	require.NoError(t, task.SyncLog(model.DB))
	var log model.Task
	require.NoError(t, model.DB.First(&log, task.LogID).Error)
	items := tasksToDto([]*model.Task{&log}, false, common.RoleCommonUser)
	require.Len(t, items, 1)
	assert.Equal(t, task.RequestPath, items[0].RequestPath)
	assert.Equal(t, "gpt-image-2", items[0].ModelName)
	assert.Equal(t, task.CreatedAt+50, items[0].DurationFinishTime)
	assert.Equal(t, task.CreatedAt+300, items[0].FinishTime)
	require.Len(t, items[0].Media, 1)
	encoded, err := common.Marshal(items)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "只在详情中显示的长提示词")
}

// 三类素材经过受理、详情读取和清理，原始请求始终保持原样。
func TestAsyncTaskDetailsMixedReferences(t *testing.T) {
	png, err := base64.StdEncoding.DecodeString(asyncFixturePNG)
	require.NoError(t, err)
	video := []byte("\x00\x00\x00\x18ftypmp42\x00\x00\x00\x00mp42isom")
	audio := append([]byte("fLaC\x80\x00\x00\x22"), make([]byte, 34)...)
	inline := []string{"data:image/png;base64," + asyncFixturePNG, "data:video/mp4;base64," + base64.StdEncoding.EncodeToString(video), "data:audio/flac;base64," + base64.StdEncoding.EncodeToString(audio)}
	for _, format := range []string{"标准字段", "参考别名", "内容数组", "表单文件", "表单地址"} {
		t.Run(format, func(t *testing.T) {
			prepareAsyncMediaController(t)
			input := map[string]any{"model": "sd-2.0", "prompt": "起身看向外面，音乐参考", "resolution": "720p"}
			keys := []string{"image_urls", "video_urls", "audio_urls"}
			if format == "参考别名" {
				keys = []string{"reference_images", "reference_videos", "reference_audios"}
			}
			for i, key := range keys {
				input[key] = []string{inline[i]}
			}
			if format == "内容数组" {
				for _, key := range keys {
					delete(input, key)
				}
				input["content"] = []any{map[string]any{"type": "image_url", "image_url": map[string]any{"url": inline[0]}}, map[string]any{"type": "video_url", "video_url": map[string]any{"url": inline[1]}}, map[string]any{"type": "audio_url", "audio_url": map[string]any{"url": inline[2]}}}
			}
			body, err := common.Marshal(input)
			require.NoError(t, err)
			contentType := "application/json"
			if format == "表单文件" || format == "表单地址" {
				var buffer bytes.Buffer
				form := multipart.NewWriter(&buffer)
				require.NoError(t, form.WriteField("prompt", input["prompt"].(string)))
				for i, key := range keys {
					if format == "表单文件" {
						part, err := form.CreateFormFile(key+"[]", []string{"参考.png", "动作.mp4", "音乐.flac"}[i])
						require.NoError(t, err)
						_, err = part.Write([][]byte{png, video, audio}[i])
						require.NoError(t, err)
					} else {
						value, err := common.Marshal([]string{inline[i]})
						require.NoError(t, err)
						require.NoError(t, form.WriteField(key, string(value)))
					}
				}
				require.NoError(t, form.Close())
				body, contentType = buffer.Bytes(), form.FormDataContentType()
			}
			task := enqueueDetailFixture(t, body, contentType, true)
			original, err := os.ReadFile(task.RequestFilePath)
			require.NoError(t, err)
			assert.Equal(t, body, original)
			var saved model.AsyncRelayRequestDetails
			require.NoError(t, common.UnmarshalJsonStr(task.RequestDetails, &saved))
			require.Len(t, saved.References, 3)
			params := gin.Params{{Key: "task_id", Value: task.TaskID}}
			for i, kind := range []string{"image", "video", "audio"} {
				assert.Empty(t, saved.References[i].Error)
				assert.Equal(t, kind, saved.References[i].Kind)
				reference := asyncControllerRequest(GetAsyncRelayReference, http.MethodGet, "/reference", 31, common.RoleCommonUser, append(append(gin.Params{}, params...), gin.Param{Key: "index", Value: fmt.Sprint(i)}), "")
				require.Equal(t, http.StatusOK, reference.Code)
				assert.Equal(t, [][]byte{png, video, audio}[i], reference.Body.Bytes())
			}
			details := asyncControllerRequest(GetAsyncRelayTaskDetails, http.MethodGet, "/details", 31, common.RoleCommonUser, params, "")
			assert.Contains(t, details.Body.String(), `"kind":"audio"`)
			assert.Contains(t, details.Body.String(), "/reference/2")
			task.Status, task.FinishedAt = model.AsyncRelayTaskStatusSucceeded, common.GetTimestamp()-7201
			require.NoError(t, model.DB.Save(task).Error)
			require.NoError(t, model.ExpireAsyncRelayTaskFiles(task))
			for _, reference := range saved.References {
				assert.NoFileExists(t, reference.Path)
			}
			details = asyncControllerRequest(GetAsyncRelayTaskDetails, http.MethodGet, "/details", 31, common.RoleCommonUser, params, "")
			assert.NotContains(t, details.Body.String(), "起身看向外面，音乐参考")
			assert.Contains(t, details.Body.String(), `"inputs_expired":true`)
			assert.NotContains(t, details.Body.String(), "/reference/")
		})
	}
}

func TestAsyncTaskDetailsUploadedReferenceSnapshot(t *testing.T) {
	prepareAsyncMediaController(t)
	// 同一套素材归属、快照与清理用例可在三种真实数据库上运行。
	db, dialect := openTaskDialectDatabase(t, &model.AsyncRelayTask{}, &model.Task{}, &model.VideoAsset{}, &model.VideoAssetUse{})
	model.DB = db
	common.SetDatabaseTypes(dialect, common.DatabaseTypeSQLite)
	png, err := base64.StdEncoding.DecodeString(asyncFixturePNG)
	require.NoError(t, err)
	asset, err := service.SaveVideoAsset(31, bytes.NewReader(png))
	require.NoError(t, err)
	for _, owner := range []bool{true, false} {
		if !owner {
			require.NoError(t, model.DB.Model(asset).Update("user_id", 32).Error)
		}
		body, err := common.Marshal(map[string]any{"reference_images": []any{map[string]any{"asset_id": asset.ID}}})
		require.NoError(t, err)
		task := enqueueDetailFixture(t, body, "application/json")
		var saved model.AsyncRelayRequestDetails
		require.NoError(t, common.UnmarshalJsonStr(task.RequestDetails, &saved))
		require.Len(t, saved.References, 1)
		if owner {
			assert.Empty(t, saved.References[0].Error)
			assert.FileExists(t, saved.References[0].Path)
			assert.NotEqual(t, asset.Path, saved.References[0].Path)
			task.Status, task.FinishedAt = model.AsyncRelayTaskStatusSucceeded, common.GetTimestamp()-7201
			require.NoError(t, model.DB.Save(task).Error)
			require.NoError(t, model.ExpireAsyncRelayTaskFiles(task))
			assert.FileExists(t, asset.Path)
		} else {
			assert.Empty(t, saved.References[0].Path)
			assert.NotEmpty(t, saved.References[0].Error)
		}
	}
}

func TestAsyncTaskDetailsRemoteAudioAndInvalidMedia(t *testing.T) {
	prepareAsyncMediaController(t)
	settings := system_setting.GetFetchSetting()
	previousProtection := settings.EnableSSRFProtection
	settings.EnableSSRFProtection = false
	t.Cleanup(func() { settings.EnableSSRFProtection = previousProtection })
	service.InitHttpClient()
	flac := append([]byte("fLaC\x80\x00\x00\x22"), make([]byte, 34)...)
	var requests atomic.Int32
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path == "/failed" {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_, _ = w.Write(flac)
	}))
	defer source.Close()
	for _, path := range []string{"/audio", "/failed"} {
		body, err := common.Marshal(map[string]any{"prompt": "只记录素材，https://example.test/not-a-reference", "audio_urls": []string{source.URL + path}, "callback_url": "https://example.test/callback"})
		require.NoError(t, err)
		before := requests.Load()
		task := enqueueDetailFixture(t, body, "application/json")
		assert.Equal(t, before, requests.Load(), "受理任务不下载远程素材")
		var saved model.AsyncRelayRequestDetails
		require.NoError(t, common.UnmarshalJsonStr(task.RequestDetails, &saved))
		require.Len(t, saved.References, 1)
		assert.Equal(t, "audio", saved.References[0].Kind)
		params := gin.Params{{Key: "task_id", Value: task.TaskID}, {Key: "index", Value: "0"}}
		response := asyncControllerRequest(GetAsyncRelayReference, http.MethodGet, "/reference/0", 31, common.RoleCommonUser, params, "")
		assert.Equal(t, before+1, requests.Load(), "查看仅下载一次，失败不自动重试")
		if path == "/audio" {
			assert.Equal(t, http.StatusOK, response.Code)
			assert.Equal(t, "audio/flac", response.Header().Get("Content-Type"))
			assert.Equal(t, flac, response.Body.Bytes())
		} else {
			assert.Equal(t, http.StatusBadGateway, response.Code)
		}
	}
	// 伪装为音频的网页不产生预览地址，也不保留无效快照文件。
	input := `{"audio_urls":["data:audio/flac;base64,PGh0bWw+YmFkPC9odG1sPg=="]}`
	task := enqueueDetailFixture(t, []byte(input), "application/json")
	var saved model.AsyncRelayRequestDetails
	require.NoError(t, common.UnmarshalJsonStr(task.RequestDetails, &saved))
	require.Len(t, saved.References, 1)
	assert.NotEmpty(t, saved.References[0].Error)
	assert.Empty(t, saved.References[0].Path)
	assert.Empty(t, saved.References[0].Source)
}
