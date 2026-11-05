package openai

import (
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 回包原文和两类用量分别保留，不能把作者推理 token 再累加到图片输出中。
func TestOpenAIImageSVGUsageRemainsSeparate(t *testing.T) {
	body := `{"data":[{"b64_json":"PHN2Zy8+"}],"usage":{"input_tokens":10,"output_tokens":20,"total_tokens":30},"svg_usage":{"model":"author","input_tokens":1000,"output_tokens":200,"reasoning_tokens":100,"total_tokens":1200},"output_format":"svg"}`
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("POST", "/v1/images/generations", nil)
	info := &relaycommon.RelayInfo{ImageSVGRequested: true, ChannelMeta: &relaycommon.ChannelMeta{}}
	response := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
	usage, err := OpenaiImageHandler(c, info, response)
	require.Nil(t, err)
	require.Equal(t, 10, usage.PromptTokens)
	require.Equal(t, 20, usage.CompletionTokens)
	require.NotNil(t, info.ImageSVGUsage)
	require.Equal(t, 200, info.ImageSVGUsage.OutputTokens)
	require.JSONEq(t, body, recorder.Body.String())
}
