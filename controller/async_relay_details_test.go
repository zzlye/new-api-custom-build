package controller

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func enqueueDetailFixture(t *testing.T, body []byte, contentType string) *model.AsyncRelayTask {
	t.Helper()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits?async=true", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", contentType)
	c.Request.Header.Set("Authorization", "Bearer private-header")
	c.Set("id", 31)
	c.Set("token_id", 9)
	defer common.CleanupBodyStorage(c)
	require.NoError(t, EnqueueAsyncRelayRequest(c, relaytypes.RelayFormatOpenAIImage))
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
	// 文件过期只清理媒体和远程地址；原提示词和时间记录继续可查。
	task.Status, task.FinishedAt = model.AsyncRelayTaskStatusSucceeded, common.GetTimestamp()-7201
	require.NoError(t, model.DB.Save(task).Error)
	expired := asyncControllerRequest(GetAsyncRelayReference, http.MethodGet, "/reference/0", 31, common.RoleCommonUser, referenceParams, "")
	assert.Equal(t, http.StatusGone, expired.Code)
	require.NoError(t, model.ExpireAsyncRelayTaskFiles(task))
	assert.NoFileExists(t, saved.References[0].Path)
	require.NoError(t, model.DB.First(task, task.ID).Error)
	saved = model.AsyncRelayRequestDetails{}
	require.NoError(t, common.UnmarshalJsonStr(task.RequestDetails, &saved))
	assert.Equal(t, "保留人物，改成晴天", saved.Prompt)
	assert.Empty(t, saved.References[0].Path)
	after := asyncControllerRequest(GetAsyncRelayTaskDetails, http.MethodGet, "/details", 31, common.RoleCommonUser, params, "")
	assert.Contains(t, after.Body.String(), "保留人物，改成晴天")
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
	assert.Contains(t, fresh.RequestDetails, "参考这张图")
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
	items := tasksToDto([]*model.Task{&log}, false)
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
