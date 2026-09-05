package controller

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 同一张图片同时提供内嵌数据和下载地址时，只需保存已经完整返回的数据。
func TestAsyncRelayStoresInlineImageWithoutDownloadingDuplicateURL(t *testing.T) {
	user, token := prepareAsyncCompatRelay(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, err := fmt.Fprintf(w, `{"data":[{"b64_json":"%s","url":"http://127.0.0.1:9090/image.png"}]}`, asyncFixturePNG)
		assert.NoError(t, err)
	}))
	response, done, _ := beginAsyncCompatRequest(t, user, token, "/v1/images/generations", `{"model":"dall-e-3","prompt":"一只猫","n":1,"size":"1024x1024"}`)
	processed, err := ProcessAsyncRelayTasks(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, 1, processed)
	<-done
	require.Equal(t, http.StatusOK, response.Code)
	var task model.AsyncRelayTask
	require.NoError(t, model.DB.First(&task).Error)
	require.Equal(t, model.AsyncRelayTaskStatusSucceeded, task.Status, task.Error)
	media := asyncRelayMediaLinks(&task, "/api/task/")
	require.Len(t, media, 1)
	assert.Equal(t, "image/png", media[0].ContentType)
}
