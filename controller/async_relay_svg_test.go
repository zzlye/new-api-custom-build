package controller

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 验证实际归档与读取链路，SVG 和 GIF 均保留原始字节及既有访问边界。
func TestAsyncSVGAndGIFArchivePreview(t *testing.T) {
	for _, tc := range []struct{ name, content, mime string }{
		{"svg", `<svg xmlns="http://www.w3.org/2000/svg"><path d="M0 0h10v10z"/></svg>`, "image/svg+xml"},
		{"gif", "GIF89a\x01\x00\x01\x00\x80\x00\x00\x00\x00\x00\xff\xff\xff\x21\xf9\x04\x01\x00\x00\x00\x00\x2c\x00\x00\x00\x00\x01\x00\x01\x00\x00\x02\x02\x44\x01\x00\x3b", "image/gif"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prepareAsyncMediaController(t)
			task := &model.AsyncRelayTask{UserID: 41, NodeID: common.NodeName}
			require.NoError(t, task.InsertWithLog("default", "IMAGE"))
			claimed, won, err := model.ClaimAsyncRelayTask(task.ID, "svg-fixture")
			require.NoError(t, err)
			require.True(t, won)
			path, file, err := common.CreateAsyncMediaFile()
			require.NoError(t, err)
			_, err = fmt.Fprintf(file, `{"data":[{"b64_json":"%s"}]}`, base64.StdEncoding.EncodeToString([]byte(tc.content)))
			require.NoError(t, err)
			require.NoError(t, file.Close())
			require.True(t, completeAsyncRelayResult(context.Background(), claimed, path, "application/json"))
			params := gin.Params{{Key: "task_id", Value: task.TaskID}, {Key: "index", Value: "0"}}
			preview := asyncControllerRequest(GetAsyncRelayMedia, http.MethodGet, "/", 41, common.RoleCommonUser, params, "")
			require.Equal(t, http.StatusOK, preview.Code)
			require.Equal(t, tc.content, preview.Body.String())
			require.Equal(t, tc.mime, preview.Header().Get("Content-Type"))
			if tc.name == "svg" {
				require.Contains(t, preview.Header().Get("Content-Security-Policy"), "sandbox; default-src 'none'")
			}
			denied := asyncControllerRequest(GetAsyncRelayMedia, http.MethodGet, "/", 42, common.RoleCommonUser, params, "")
			require.Equal(t, http.StatusNotFound, denied.Code)
			require.NoError(t, model.DB.Model(task).Update("finished_at", common.GetTimestamp()-7200).Error)
			expired := asyncControllerRequest(GetAsyncRelayMedia, http.MethodGet, "/", 41, common.RoleCommonUser, params, "")
			require.Equal(t, http.StatusGone, expired.Code)
		})
	}
}

func TestAsyncSVGStaticArchiveAndUnsafeContent(t *testing.T) {
	valid := `<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><defs><linearGradient id="g"><stop offset="0" stop-color="red"/></linearGradient></defs><path fill="url(#g)" d="M0 0h10v10z"/><text x="1" y="5">图</text></svg>`
	require.NoError(t, validateAsyncSVG(strings.NewReader(valid)))
	path := filepath.Join(t.TempDir(), "result.svg")
	require.NoError(t, os.WriteFile(path, []byte(valid), 0600))
	mime, kind, err := inspectAsyncMedia(path)
	require.NoError(t, err)
	require.Equal(t, "image/svg+xml", mime)
	require.Equal(t, "image", kind)
	for _, content := range []string{
		`<svg><script>alert(1)</script></svg>`,
		`<svg onload="alert(1)"/>`,
		`<svg><foreignObject><div/></foreignObject></svg>`,
		`<svg><use href="https://example.test/x.svg#id"/></svg>`,
		`<svg><style>@import 'https://example.test/x.css';</style></svg>`,
		`<svg><style>path{fill:u<!-- -->rl(https://example.test)}</style></svg>`,
		`<!DOCTYPE svg><svg/>`,
		`<svg/><svg/>`,
		`<svg><path>`,
	} {
		require.Error(t, validateAsyncSVG(strings.NewReader(content)), content)
	}
}
