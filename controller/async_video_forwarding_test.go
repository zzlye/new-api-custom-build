package controller

import (
	"context"
	"io"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 从持久化队列回放到真实上游 HTTP 替身，防止只检查入站快照却漏掉实际丢参。
func TestAsyncVideoForwardsCompletePluginRequest(t *testing.T) {
	for _, path := range []string{"/v1/videos", "/v1/videos?async=true"} {
		t.Run(path, func(t *testing.T) {
			var submits atomic.Int32
			outbound := make(chan string, 1)
			user, token := prepareAsyncCompatRelay(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				submits.Add(1)
				assert.Equal(t, "/v1/videos", r.URL.Path)
				data, err := io.ReadAll(r.Body)
				if !assert.NoError(t, err) {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				select {
				case outbound <- string(data):
				default:
				}
				w.Header().Set("Content-Type", "application/json")
				_, err = w.Write([]byte(`{"id":"wan-fixture","status":"queued","model":"wan-3.0-1080p"}`))
				assert.NoError(t, err)
			}), "wan-3.0-1080p")
			request := `{"model":"wan-3.0-1080p","prompt":"镜头缓慢前进","duration":8,"resolution":"1080p","aspect_ratio":"16:9","generate_audio":false,"seed":0,"input_reference":{"image_url":"https://example.test/reference.png"},"provider_options":{"strength":0.5}}`
			response, done, _ := beginAsyncCompatRequest(t, user, token, path, request, relaytypes.RelayFormatTask)
			processed, err := ProcessAsyncRelayTasks(context.Background(), 1)
			require.NoError(t, err)
			require.Equal(t, 1, processed)
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("视频提交未返回")
			}
			if path == "/v1/videos" {
				require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			} else {
				require.Equal(t, http.StatusAccepted, response.Code, response.Body.String())
			}
			select {
			case actual := <-outbound:
				assert.JSONEq(t, request, actual)
			default:
				t.Fatal("未收到实际出站请求")
			}
			assert.EqualValues(t, 1, submits.Load())
			var parent model.AsyncRelayTask
			require.NoError(t, model.DB.First(&parent).Error)
			assert.Equal(t, model.AsyncRelayTaskStatusWaiting, parent.Status)
			assert.NotEmpty(t, parent.LinkedTaskID)
		})
	}
}
