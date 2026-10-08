package controller

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 图片编辑经过真实队列重放，原渠道重试和切换都必须保留参考图与表单参数。
func TestAsyncMediaRoutingSameThenUnselectedEdits(t *testing.T) {
	reference, err := base64.StdEncoding.DecodeString(asyncFixturePNG)
	require.NoError(t, err)
	var mu sync.Mutex
	var order []int
	handler := func(id int) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, "/v1/images/edits", r.URL.Path)
			require.NoError(t, r.ParseMultipartForm(1<<20))
			defer r.MultipartForm.RemoveAll()
			require.Equal(t, "gpt-image-2", r.FormValue("model"))
			require.Equal(t, "保留参考图和参数", r.FormValue("prompt"))
			file, _, err := r.FormFile("image")
			require.NoError(t, err)
			defer file.Close()
			data, err := io.ReadAll(file)
			require.NoError(t, err)
			require.Equal(t, reference, data)
			mu.Lock()
			order = append(order, id)
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			if id == 1 {
				w.WriteHeader(503)
				fmt.Fprint(w, `{"error":{"message":"busy","type":"server_error"}}`)
				return
			}
			fmt.Fprintf(w, `{"data":[{"b64_json":"%s"}]}`, asyncFixturePNG)
		}
	}
	user, token := prepareAsyncCompatRelay(t, handler(1), "gpt-image-2")
	backup := httptest.NewServer(handler(2))
	t.Cleanup(backup.Close)
	priority := int64(10)
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = 1").Update("priority", priority).Error)
	require.NoError(t, model.DB.Model(&model.Ability{}).Where("channel_id = 1").Update("priority", priority).Error)
	channel := &model.Channel{Type: constant.ChannelTypeOpenAI, Key: "fixture", Status: common.ChannelStatusEnabled, Name: "未勾选备用", Models: "gpt-image-2", Group: "default", BaseURL: &backup.URL}
	require.NoError(t, model.DB.Create(channel).Error)
	require.NoError(t, model.DB.Create(&model.Ability{Group: "default", Model: "gpt-image-2", ChannelId: channel.Id, Enabled: true}).Error)
	require.NoError(t, model.UpdateOption(operation_setting.AsyncMediaRetryOption, `{"enabled":true,"max_retries":1,"same_channel_retries":1,"selected_channels_only":true,"channel_ids":[1],"channel_status_codes":{"1":"503"}}`))
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	require.NoError(t, form.WriteField("model", "gpt-image-2"))
	require.NoError(t, form.WriteField("prompt", "保留参考图和参数"))
	file, err := form.CreateFormFile("image", "reference.png")
	require.NoError(t, err)
	_, err = file.Write(reference)
	require.NoError(t, err)
	require.NoError(t, form.Close())
	response := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(response)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", &body)
	c.Request.Header.Set("Content-Type", form.FormDataContentType())
	c.Set("id", user.Id)
	c.Set("token_id", token.Id)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer common.CleanupBodyStorage(c)
		Relay(c, relaytypes.RelayFormatOpenAIImage)
	}()
	select {
	case <-asyncRelayWakeup:
	case <-time.After(5 * time.Second):
		t.Fatal("图片编辑没有进入队列")
	}
	_, err = ProcessAsyncRelayTasks(context.Background(), 1)
	require.NoError(t, err)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("客户端没有收到最终响应")
	}
	mu.Lock()
	require.Equal(t, []int{1, 1, 2}, order)
	mu.Unlock()
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var task model.AsyncRelayTask
	require.NoError(t, model.DB.First(&task).Error)
	require.Equal(t, model.AsyncRelayTaskStatusSucceeded, task.Status)
	var details model.AsyncRelayRequestDetails
	require.NoError(t, common.UnmarshalJsonStr(task.RequestDetails, &details))
	reasons := make([]string, 0, len(details.RoutingEvents))
	for _, event := range details.RoutingEvents {
		reasons = append(reasons, event.Decision.Reason)
	}
	require.Contains(t, reasons, "retry_same_channel")
	require.Contains(t, reasons, "retry_status_matched")
	var after model.User
	require.NoError(t, model.DB.First(&after, user.Id).Error)
	var log model.Task
	require.NoError(t, model.DB.First(&log, task.LogID).Error)
	require.Equal(t, user.Quota-log.Quota, after.Quota, "只结算最终成功的一次图片编辑")
}
