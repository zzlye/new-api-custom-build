package controller

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 详情中提供可再次访问的本地地址，并只记录真实存在的普通网络来源。
func TestAsyncTaskDetailsRecordMediaURLs(t *testing.T) {
	for _, tc := range []struct {
		name, alias, source string
		stream              bool
	}{
		{"内嵌图片的备用地址", `,"url":"http://127.0.0.1:9090/original.png"`, "http://127.0.0.1:9090/original.png", false},
		{"嵌套图片地址", `,"image_url":{"url":"https://images.example/original.png?signature=fixture"}`, "https://images.example/original.png?signature=fixture", false},
		{"只有内嵌图片", "", "", false},
		{"忽略脚本地址", `,"url":"javascript:alert(1)"`, "", false},
		{"忽略包含用户名密码的地址", `,"url":"https://name:password@images.example/original.png"`, "", false},
		{"事件流完成时补充来源", "", "https://images.example/stream.png", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prepareAsyncMediaController(t)
			task := &model.AsyncRelayTask{UserID: 31, NodeID: common.NodeName, RequestFormat: "openai-image"}
			require.NoError(t, task.InsertWithLog("default", "IMAGE"))
			claimed, won, err := model.ClaimAsyncRelayTask(task.ID, "url-fixture")
			require.NoError(t, err)
			require.True(t, won)
			path, file, err := common.CreateAsyncMediaFile()
			require.NoError(t, err)
			contentType := "application/json"
			payload := fmt.Sprintf(`{"data":[{"b64_json":"%s"%s}]}`, asyncFixturePNG, tc.alias)
			if tc.stream {
				contentType = "text/event-stream"
				payload = fmt.Sprintf("data: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"image_generation_call\",\"result\":\"%s\"}}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"output\":[{\"type\":\"image_generation_call\",\"result\":\"%s\",\"url\":\"%s\"}]}}\n\n", asyncFixturePNG, asyncFixturePNG, tc.source)
			}
			_, err = file.WriteString(payload)
			require.NoError(t, err)
			require.NoError(t, file.Close())
			require.True(t, completeAsyncRelayResult(context.Background(), claimed, path, contentType))
			require.Equal(t, model.AsyncRelayTaskStatusSucceeded, claimed.Status, claimed.Error)
			params := gin.Params{{Key: "task_id", Value: claimed.TaskID}}
			response := asyncControllerRequest(GetAsyncRelayTaskDetails, http.MethodGet, "/details", 31, common.RoleCommonUser, params, "")
			require.Equal(t, http.StatusOK, response.Code)
			var details struct {
				Data struct {
					Media []struct {
						URL        string `json:"url"`
						PreviewURL string `json:"preview_url"`
						SourceURL  string `json:"source_url"`
					} `json:"media"`
				} `json:"data"`
			}
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &details))
			require.Len(t, details.Data.Media, 1)
			assert.Equal(t, "/api/task/"+claimed.TaskID+"/media/0", details.Data.Media[0].URL)
			assert.Equal(t, "/task-media/"+claimed.TaskID+"/media/0", details.Data.Media[0].PreviewURL)
			assert.Equal(t, tc.source, details.Data.Media[0].SourceURL)
			assert.NotContains(t, response.Body.String(), path)
			assert.NotContains(t, response.Body.String(), asyncFixturePNG)
			if tc.source != "" {
				assert.Contains(t, claimed.ResultFiles, `"source_url":`)
			}
			hidden := asyncControllerRequest(GetAsyncRelayTaskDetails, http.MethodGet, "/details", 32, common.RoleCommonUser, params, "")
			assert.Equal(t, http.StatusNotFound, hidden.Code)
			// 到期后来源地址同样停止展示，仍保留任务文字记录。
			require.NoError(t, model.DB.Model(claimed).Update("finished_at", common.GetTimestamp()-7201).Error)
			expired := asyncControllerRequest(GetAsyncRelayTaskDetails, http.MethodGet, "/details", 31, common.RoleCommonUser, params, "")
			assert.NotContains(t, expired.Body.String(), `"preview_url"`)
			assert.NotContains(t, expired.Body.String(), `"source_url"`)
		})
	}
}

func TestAsyncTaskDetailsRecoverHistoricalURLsWithoutRegenerating(t *testing.T) {
	prepareAsyncMediaController(t)
	task := createCompletedAsyncImage(t, 31)
	// 模拟升级前仅保存文件路径的记录，原始响应仍在保存期内。
	var files []map[string]any
	require.NoError(t, common.UnmarshalJsonStr(task.ResultFiles, &files))
	delete(files[0], "source_url")
	delete(files[0], "source_checked")
	previousFiles, err := common.Marshal(files)
	require.NoError(t, err)
	task.ResultFiles = string(previousFiles)
	task.ResponseFilePath = task.ResultFilePath
	task.ResponseContentType = "application/json"
	task.ResponseCompletedAt = task.FinishedAt
	payload := fmt.Sprintf(`{"data":[{"b64_json":"%s","url":"https://images.example/old.png"}]}`, asyncFixturePNG)
	require.NoError(t, os.WriteFile(task.ResultFilePath, []byte(payload), 0600))
	require.NoError(t, model.DB.Save(task).Error)
	response := asyncControllerRequest(GetAsyncRelayTaskDetails, http.MethodGet, "/details", 31, common.RoleCommonUser, gin.Params{{Key: "task_id", Value: task.TaskID}}, "")
	require.Equal(t, http.StatusOK, response.Code)
	assert.Contains(t, response.Body.String(), `"source_url":"https://images.example/old.png"`)
	restored, err := model.GetAsyncRelayTaskByTaskID(task.TaskID)
	require.NoError(t, err)
	require.NotNil(t, restored)
	assert.Contains(t, restored.ResultFiles, `"source_url":"https://images.example/old.png"`)
	assert.Equal(t, task.FinishedAt, restored.FinishedAt)
	assert.Equal(t, task.ResponseCompletedAt, restored.ResponseCompletedAt)
	assert.Equal(t, task.UpdatedAt, restored.UpdatedAt)
	assert.Equal(t, task.Status, restored.Status)
	assert.Equal(t, task.MediaAttempts, restored.MediaAttempts)
	original, err := os.ReadFile(task.ResultFilePath)
	require.NoError(t, err)
	assert.Equal(t, payload, string(original))
}

// 历史记录若含有不同数量的结果，宁可保留本地地址，也不把来源误配到其他图片。
func TestAsyncTaskDetailsSkipAmbiguousHistoricalMediaURLs(t *testing.T) {
	prepareAsyncMediaController(t)
	task := createCompletedAsyncImage(t, 31)
	var files []map[string]any
	require.NoError(t, common.UnmarshalJsonStr(task.ResultFiles, &files))
	delete(files[0], "source_checked")
	encoded, err := common.Marshal(files)
	require.NoError(t, err)
	task.ResultFiles = string(encoded)
	require.NoError(t, model.DB.Save(task).Error)
	payload := fmt.Sprintf(`{"data":[{"b64_json":"%s","url":"https://images.example/first.png"},{"url":"https://images.example/second.png"}]}`, asyncFixturePNG)
	require.NoError(t, os.WriteFile(task.ResultFilePath, []byte(payload), 0600))
	response := asyncControllerRequest(GetAsyncRelayTaskDetails, http.MethodGet, "/details", 31, common.RoleCommonUser, gin.Params{{Key: "task_id", Value: task.TaskID}}, "")
	require.Equal(t, http.StatusOK, response.Code)
	assert.Contains(t, response.Body.String(), "/media/0")
	assert.NotContains(t, response.Body.String(), "images.example")
	saved, err := model.GetAsyncRelayTaskByTaskID(task.TaskID)
	require.NoError(t, err)
	require.NotNil(t, saved)
	assert.Equal(t, task.ResultFiles, saved.ResultFiles)
}
