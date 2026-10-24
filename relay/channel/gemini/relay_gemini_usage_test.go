package gemini

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestForcedGeminiStreamRequestAndBufferedResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, format := range []types.RelayFormat{types.RelayFormatGemini, types.RelayFormatOpenAI, types.RelayFormatClaude, types.RelayFormatOpenAIResponses} {
		t.Run(fmt.Sprint(format), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/v1beta/models/gemini-3.8-flash:streamGenerateContent", r.URL.Path)
				require.Equal(t, "sse", r.URL.Query().Get("alt"))
				require.Equal(t, "text/event-stream", r.Header.Get("Accept"))
				body, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				require.False(t, gjson.GetBytes(body, "stream").Exists())
				require.False(t, gjson.GetBytes(body, "generationConfig.streamFunctionCallArguments").Bool())
				w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
				fmt.Fprint(w, "data: {\"candidates\":[{\"index\":0,\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"O\"}]}}],\"usageMetadata\":{\"promptTokenCount\":10,\"candidatesTokenCount\":1,\"totalTokenCount\":11,\"cachedContentTokenCount\":3,\"promptTokensDetails\":[{\"modality\":\"TEXT\",\"tokenCount\":10}]}}\n\n")
				fmt.Fprint(w, "data: {\"candidates\":[{\"index\":0,\"content\":{\"parts\":[{\"text\":\"K\"},{\"text\":\"\",\"thoughtSignature\":\"signature\"}]},\"finishReason\":\"STOP\"}],\"modelVersion\":\"gemini-3.8-flash\",\"usageMetadata\":{\"promptTokenCount\":10,\"candidatesTokenCount\":2,\"totalTokenCount\":12,\"promptTokensDetails\":[{\"modality\":\"TEXT\",\"tokenCount\":10}]}}\n\n")
			}))
			defer server.Close()
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-3.8-flash:generateContent", strings.NewReader("{}"))
			c.Request.Header.Set("Content-Type", "application/json")
			info := &relaycommon.RelayInfo{RelayFormat: format, StartTime: time.Now(), ChannelMeta: &relaycommon.ChannelMeta{
				ChannelBaseUrl: server.URL, UpstreamModelName: "gemini-3.8-flash", ParamOverride: map[string]any{"stream": true},
			}}
			if format == types.RelayFormatGemini {
				info.RelayMode = relayconstant.RelayModeGemini
			}
			if format == types.RelayFormatOpenAIResponses {
				info.RelayMode = relayconstant.RelayModeResponses
			}
			adaptor := &Adaptor{}
			resp, err := adaptor.doGeminiRequest(c, info, strings.NewReader(`{"stream":true,"contents":[],"generationConfig":{"streamFunctionCallArguments":true}}`))
			require.NoError(t, err)
			require.False(t, info.IsStream)
			require.False(t, c.Writer.Written())
			require.Empty(t, recorder.Header().Get("Content-Type"))
			payload, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			require.Equal(t, "OK", gjson.GetBytes(payload, "candidates.0.content.parts.0.text").String())
			require.Equal(t, "signature", gjson.GetBytes(payload, "candidates.0.content.parts.1.thoughtSignature").String())
			require.EqualValues(t, 10, gjson.GetBytes(payload, "usageMetadata.promptTokensDetails.0.tokenCount").Int())
			require.EqualValues(t, 3, gjson.GetBytes(payload, "usageMetadata.cachedContentTokenCount").Int())
			resp.Body = io.NopCloser(bytes.NewReader(payload))
			result, apiErr := adaptor.DoResponse(c, resp, info)
			require.Nil(t, apiErr)
			usage, ok := result.(*dto.Usage)
			require.True(t, ok)
			require.Equal(t, 10, usage.PromptTokens)
			require.Equal(t, 2, usage.CompletionTokens)
			require.Equal(t, 12, usage.TotalTokens)
			require.Contains(t, recorder.Body.String(), "OK")
			require.NotContains(t, recorder.Header().Get("Content-Type"), "event-stream")
		})
	}
}

func TestForcedGeminiStreamHonorsChannelAndOriginalResponse(t *testing.T) {
	for _, tc := range []struct {
		name, model, payload, action string
		override, stream             bool
		status                       int
	}{
		{"no_override", "gemini-3.8-flash", `{"stream":true}`, "generateContent", false, false, 200},
		{"disabled", "gemini-3.8-flash", `{"stream":false}`, "generateContent", true, false, 200},
		{"embedding", "gemini-embedding-001", `{"stream":true}`, "embedContent", true, false, 200},
		{"already_stream", "gemini-3.8-flash", `{"stream":true}`, "streamGenerateContent", true, true, 200},
		{"rate_limit", "gemini-3.8-flash", `{"stream":true}`, "streamGenerateContent", true, false, 429},
		{"unavailable", "gemini-3.8-flash", `{"stream":true}`, "streamGenerateContent", true, false, 503},
		{"json_fallback", "gemini-3.8-flash", `{"stream":true}`, "streamGenerateContent", true, false, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.True(t, strings.HasSuffix(r.URL.Path, ":"+tc.action))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				fmt.Fprint(w, `{"unchanged":true}`)
			}))
			defer server.Close()
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/", strings.NewReader("{}"))
			info := &relaycommon.RelayInfo{IsStream: tc.stream, ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: server.URL, UpstreamModelName: tc.model}}
			if tc.override {
				info.ParamOverride = map[string]any{"stream": true}
			}
			resp, err := (&Adaptor{}).doGeminiRequest(c, info, strings.NewReader(tc.payload))
			require.NoError(t, err)
			defer resp.Body.Close()
			require.Equal(t, tc.status, resp.StatusCode)
			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			require.JSONEq(t, `{"unchanged":true}`, string(body))
			require.Equal(t, tc.stream, info.IsStream)
		})
	}
}

func TestBufferedGeminiStreamFailureAndComplexParts(t *testing.T) {
	for _, tc := range []struct {
		name, events string
		status       int
		fails        bool
	}{
		{"empty", "", 0, true},
		{"malformed", "data: {invalid}\n\n", 0, true},
		{"truncated", "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"partial\"}]}}]}\n\ndata: [DONE]\n\n", 0, true},
		{"stream_error", "data: {\"error\":{\"code\":429,\"message\":\"limited\"}}\n\n", 429, false},
		{"blocked", "data: {\"promptFeedback\":{\"blockReason\":\"SAFETY\"}}\n\n", 200, false},
		{"parts", "data: {\"candidates\":[{\"index\":1,\"content\":{\"role\":\"model\",\"parts\":[{\"functionCall\":{\"name\":\"search\",\"args\":{\"q\":\"x\"}},\"thoughtSignature\":\"sig\"},{\"inlineData\":{\"mimeType\":\"image/png\",\"data\":\"YQ==\"}}]},\"finishReason\":\"STOP\"},{\"index\":0,\"content\":{\"parts\":[{\"text\":\"hello\"}]},\"finishReason\":\"STOP\"}],\"responseId\":\"retained\"}\n\n", 200, false},
		{"partial_tool", "data: {\"candidates\":[{\"content\":{\"parts\":[{\"functionCall\":{\"name\":\"search\",\"partialArgs\":[{}],\"willContinue\":true}}]},\"finishReason\":\"STOP\"}]}\n\n", 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/", nil)
			info := &relaycommon.RelayInfo{StartTime: time.Now()}
			resp, err := bufferGeminiStream(c, info, &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(tc.events))})
			if tc.fails {
				require.Error(t, err)
				require.Nil(t, resp)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.status, resp.StatusCode)
			payload, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			if tc.name == "parts" {
				require.Equal(t, "sig", gjson.GetBytes(payload, "candidates.0.content.parts.0.thoughtSignature").String())
				require.Equal(t, "YQ==", gjson.GetBytes(payload, "candidates.0.content.parts.1.inlineData.data").String())
				require.Equal(t, "retained", gjson.GetBytes(payload, "responseId").String())
				require.Len(t, gjson.GetBytes(payload, "candidates").Array(), 2)
			}
		})
	}
	t.Run("cancel", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx, cancel := context.WithCancel(context.Background())
		c.Request = httptest.NewRequest("POST", "/", nil).WithContext(ctx)
		reader, writer := io.Pipe()
		defer writer.Close()
		cancel()
		_, err := bufferGeminiStream(c, &relaycommon.RelayInfo{}, &http.Response{Body: reader})
		require.Error(t, err)
	})
	t.Run("idle_timeout", func(t *testing.T) {
		previous := constant.StreamingTimeout
		constant.StreamingTimeout = 1
		defer func() { constant.StreamingTimeout = previous }()
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", "/", nil)
		reader, writer := io.Pipe()
		defer writer.Close()
		_, err := bufferGeminiStream(c, &relaycommon.RelayInfo{}, &http.Response{Body: reader})
		require.ErrorContains(t, err, "idle timeout")
	})
	t.Run("broken_body", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", "/", nil)
		reader, writer := io.Pipe()
		require.NoError(t, writer.CloseWithError(io.ErrUnexpectedEOF))
		_, err := bufferGeminiStream(c, &relaycommon.RelayInfo{}, &http.Response{Body: reader})
		require.ErrorIs(t, err, io.ErrUnexpectedEOF)
	})
}

func TestStreamResponseGeminiChat2OpenAIAttachesUsageMetadata(t *testing.T) {
	t.Parallel()

	withUsage, isStop := streamResponseGeminiChat2OpenAI(&dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{{
			Content: dto.GeminiChatContent{
				Role:  "model",
				Parts: []dto.GeminiPart{{Text: "hello"}},
			},
		}},
		UsageMetadata: dto.GeminiUsageMetadata{
			PromptTokenCount:     3868,
			CandidatesTokenCount: 0,
			TotalTokenCount:      3868,
		},
	})
	require.False(t, isStop)
	require.NotNil(t, withUsage)
	require.NotNil(t, withUsage.Usage)
	require.Equal(t, 3868, withUsage.Usage.PromptTokens)
	require.Equal(t, 3868, withUsage.Usage.TotalTokens)
	require.NotNil(t, withUsage.Usage.BillingUsage)
	require.Equal(t, dto.BillingUsageSourceGeminiChat, withUsage.Usage.BillingUsage.Source)
	require.Equal(t, dto.BillingUsageSemanticGemini, withUsage.Usage.BillingUsage.Semantic)
	require.NotNil(t, withUsage.Usage.BillingUsage.GeminiUsageMetadata)
	require.Equal(t, 3868, withUsage.Usage.BillingUsage.GeminiUsageMetadata.PromptTokenCount)
	require.False(t, withUsage.Usage.BillingUsage.Estimated)

	withoutUsage, _ := streamResponseGeminiChat2OpenAI(&dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{{
			Content: dto.GeminiChatContent{
				Role:  "model",
				Parts: []dto.GeminiPart{{Text: "hello"}},
			},
		}},
	})
	require.NotNil(t, withoutUsage)
	require.Nil(t, withoutUsage.Usage)
}

func TestGeminiChatStreamHandlerClaudeFirstFrameUsesUpstreamUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	oldStreamingTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 300
	t.Cleanup(func() {
		constant.StreamingTimeout = oldStreamingTimeout
	})

	info := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatClaude,
		OriginModelName: "gemini-2.5-flash",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gemini-2.5-flash",
		},
		ClaudeConvertInfo: &relaycommon.ClaudeConvertInfo{
			LastMessagesType: relaycommon.LastMessageTypeNone,
		},
	}
	info.SetEstimatePromptTokens(4994)

	chunkData, err := common.Marshal(dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{{
			Content: dto.GeminiChatContent{
				Role:  "model",
				Parts: []dto.GeminiPart{{Text: "hello"}},
			},
		}},
		UsageMetadata: dto.GeminiUsageMetadata{
			PromptTokenCount: 3868,
			TotalTokenCount:  3868,
		},
	})
	require.NoError(t, err)
	resp := &http.Response{
		Body: io.NopCloser(bytes.NewReader([]byte("data: " + string(chunkData) + "\n" + "data: [DONE]\n"))),
	}

	usage, newAPIError := GeminiChatStreamHandler(c, info, resp)
	require.Nil(t, newAPIError)
	require.NotNil(t, usage)
	require.Equal(t, 3868, usage.PromptTokens)

	var startUsage, deltaUsage *dto.ClaudeUsage
	for line := range strings.SplitSeq(recorder.Body.String(), "\n") {
		payload, ok := strings.CutPrefix(strings.TrimSpace(line), "data: ")
		if !ok {
			continue
		}
		var event dto.ClaudeResponse
		if err := common.UnmarshalJsonStr(payload, &event); err != nil {
			continue
		}
		switch event.Type {
		case "message_start":
			if event.Message != nil {
				startUsage = event.Message.Usage
			}
		case "message_delta":
			deltaUsage = event.Usage
		}
	}

	require.NotNil(t, startUsage)
	require.Equal(t, 3868, startUsage.InputTokens)
	require.NotNil(t, startUsage.BillingUsage)
	require.Equal(t, dto.BillingUsageSourceGeminiChat, startUsage.BillingUsage.Source)
	require.Equal(t, dto.BillingUsageSemanticGemini, startUsage.BillingUsage.Semantic)
	require.NotNil(t, startUsage.BillingUsage.GeminiUsageMetadata)
	require.Equal(t, 3868, startUsage.BillingUsage.GeminiUsageMetadata.PromptTokenCount)
	require.False(t, startUsage.BillingUsage.Estimated)

	require.NotNil(t, deltaUsage)
	require.Equal(t, 3868, deltaUsage.InputTokens)
	require.NotNil(t, deltaUsage.BillingUsage)
	require.Equal(t, dto.BillingUsageSourceGeminiChat, deltaUsage.BillingUsage.Source)
	require.Equal(t, dto.BillingUsageSemanticGemini, deltaUsage.BillingUsage.Semantic)
	require.NotNil(t, deltaUsage.BillingUsage.GeminiUsageMetadata)
	require.Equal(t, 3868, deltaUsage.BillingUsage.GeminiUsageMetadata.PromptTokenCount)
}

func TestGeminiChatHandlerCompletionTokensExcludeToolUsePromptTokens(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	info := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatGemini,
		OriginModelName: "gemini-3-flash-preview",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gemini-3-flash-preview",
		},
	}

	payload := dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{
			{
				Content: dto.GeminiChatContent{
					Role: "model",
					Parts: []dto.GeminiPart{
						{Text: "ok"},
					},
				},
			},
		},
		UsageMetadata: dto.GeminiUsageMetadata{
			PromptTokenCount:        151,
			ToolUsePromptTokenCount: 18329,
			CandidatesTokenCount:    1089,
			ThoughtsTokenCount:      1120,
			TotalTokenCount:         20689,
		},
	}

	body, err := common.Marshal(payload)
	require.NoError(t, err)

	resp := &http.Response{
		Body: io.NopCloser(bytes.NewReader(body)),
	}

	usage, newAPIError := GeminiChatHandler(c, info, resp)
	require.Nil(t, newAPIError)
	require.NotNil(t, usage)
	require.Equal(t, 18480, usage.PromptTokens)
	require.Equal(t, 2209, usage.CompletionTokens)
	require.Equal(t, 20689, usage.TotalTokens)
	require.Equal(t, 1120, usage.CompletionTokenDetails.ReasoningTokens)
}

func TestGeminiStreamHandlerCompletionTokensExcludeToolUsePromptTokens(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	oldStreamingTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 300
	t.Cleanup(func() {
		constant.StreamingTimeout = oldStreamingTimeout
	})

	info := &relaycommon.RelayInfo{
		OriginModelName: "gemini-3-flash-preview",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gemini-3-flash-preview",
		},
	}

	chunk := dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{
			{
				Content: dto.GeminiChatContent{
					Role: "model",
					Parts: []dto.GeminiPart{
						{Text: "partial"},
					},
				},
			},
		},
		UsageMetadata: dto.GeminiUsageMetadata{
			PromptTokenCount:        151,
			ToolUsePromptTokenCount: 18329,
			CandidatesTokenCount:    1089,
			ThoughtsTokenCount:      1120,
			TotalTokenCount:         20689,
		},
	}

	chunkData, err := common.Marshal(chunk)
	require.NoError(t, err)

	streamBody := []byte("data: " + string(chunkData) + "\n" + "data: [DONE]\n")
	resp := &http.Response{
		Body: io.NopCloser(bytes.NewReader(streamBody)),
	}

	usage, newAPIError := geminiStreamHandler(c, info, resp, func(_ string, _ *dto.GeminiChatResponse) bool {
		return true
	})
	require.Nil(t, newAPIError)
	require.NotNil(t, usage)
	require.Equal(t, 18480, usage.PromptTokens)
	require.Equal(t, 2209, usage.CompletionTokens)
	require.Equal(t, 20689, usage.TotalTokens)
	require.Equal(t, 1120, usage.CompletionTokenDetails.ReasoningTokens)
}

func TestGeminiTextGenerationHandlerPromptTokensIncludeToolUsePromptTokens(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-3-flash-preview:generateContent", nil)

	info := &relaycommon.RelayInfo{
		OriginModelName: "gemini-3-flash-preview",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gemini-3-flash-preview",
		},
	}

	payload := dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{
			{
				Content: dto.GeminiChatContent{
					Role: "model",
					Parts: []dto.GeminiPart{
						{Text: "ok"},
					},
				},
			},
		},
		UsageMetadata: dto.GeminiUsageMetadata{
			PromptTokenCount:        151,
			ToolUsePromptTokenCount: 18329,
			CandidatesTokenCount:    1089,
			ThoughtsTokenCount:      1120,
			TotalTokenCount:         20689,
		},
	}

	body, err := common.Marshal(payload)
	require.NoError(t, err)

	resp := &http.Response{
		Body: io.NopCloser(bytes.NewReader(body)),
	}

	usage, newAPIError := GeminiTextGenerationHandler(c, info, resp)
	require.Nil(t, newAPIError)
	require.NotNil(t, usage)
	require.Equal(t, 18480, usage.PromptTokens)
	require.Equal(t, 2209, usage.CompletionTokens)
	require.Equal(t, 20689, usage.TotalTokens)
	require.Equal(t, 1120, usage.CompletionTokenDetails.ReasoningTokens)
}

func TestGeminiChatHandlerUsesEstimatedPromptTokensWhenUsagePromptMissing(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	info := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatGemini,
		OriginModelName: "gemini-3-flash-preview",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gemini-3-flash-preview",
		},
	}
	info.SetEstimatePromptTokens(20)

	payload := dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{
			{
				Content: dto.GeminiChatContent{
					Role: "model",
					Parts: []dto.GeminiPart{
						{Text: "ok"},
					},
				},
			},
		},
		UsageMetadata: dto.GeminiUsageMetadata{
			PromptTokenCount:        0,
			ToolUsePromptTokenCount: 0,
			CandidatesTokenCount:    90,
			ThoughtsTokenCount:      10,
			TotalTokenCount:         110,
		},
	}

	body, err := common.Marshal(payload)
	require.NoError(t, err)

	resp := &http.Response{
		Body: io.NopCloser(bytes.NewReader(body)),
	}

	usage, newAPIError := GeminiChatHandler(c, info, resp)
	require.Nil(t, newAPIError)
	require.NotNil(t, usage)
	require.Equal(t, 20, usage.PromptTokens)
	require.Equal(t, 100, usage.CompletionTokens)
	require.Equal(t, 110, usage.TotalTokens)
}

func TestGeminiStreamHandlerUsesEstimatedPromptTokensWhenUsagePromptMissing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	oldStreamingTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 300
	t.Cleanup(func() {
		constant.StreamingTimeout = oldStreamingTimeout
	})

	info := &relaycommon.RelayInfo{
		OriginModelName: "gemini-3-flash-preview",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gemini-3-flash-preview",
		},
	}
	info.SetEstimatePromptTokens(20)

	chunk := dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{
			{
				Content: dto.GeminiChatContent{
					Role: "model",
					Parts: []dto.GeminiPart{
						{Text: "partial"},
					},
				},
			},
		},
		UsageMetadata: dto.GeminiUsageMetadata{
			PromptTokenCount:        0,
			ToolUsePromptTokenCount: 0,
			CandidatesTokenCount:    90,
			ThoughtsTokenCount:      10,
			TotalTokenCount:         110,
		},
	}

	chunkData, err := common.Marshal(chunk)
	require.NoError(t, err)

	streamBody := []byte("data: " + string(chunkData) + "\n" + "data: [DONE]\n")
	resp := &http.Response{
		Body: io.NopCloser(bytes.NewReader(streamBody)),
	}

	usage, newAPIError := geminiStreamHandler(c, info, resp, func(_ string, _ *dto.GeminiChatResponse) bool {
		return true
	})
	require.Nil(t, newAPIError)
	require.NotNil(t, usage)
	require.Equal(t, 20, usage.PromptTokens)
	require.Equal(t, 100, usage.CompletionTokens)
	require.Equal(t, 110, usage.TotalTokens)
}

func TestGeminiTextGenerationHandlerUsesEstimatedPromptTokensWhenUsagePromptMissing(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-3-flash-preview:generateContent", nil)

	info := &relaycommon.RelayInfo{
		OriginModelName: "gemini-3-flash-preview",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gemini-3-flash-preview",
		},
	}
	info.SetEstimatePromptTokens(20)

	payload := dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{
			{
				Content: dto.GeminiChatContent{
					Role: "model",
					Parts: []dto.GeminiPart{
						{Text: "ok"},
					},
				},
			},
		},
		UsageMetadata: dto.GeminiUsageMetadata{
			PromptTokenCount:        0,
			ToolUsePromptTokenCount: 0,
			CandidatesTokenCount:    90,
			ThoughtsTokenCount:      10,
			TotalTokenCount:         110,
		},
	}

	body, err := common.Marshal(payload)
	require.NoError(t, err)

	resp := &http.Response{
		Body: io.NopCloser(bytes.NewReader(body)),
	}

	usage, newAPIError := GeminiTextGenerationHandler(c, info, resp)
	require.Nil(t, newAPIError)
	require.NotNil(t, usage)
	require.Equal(t, 20, usage.PromptTokens)
	require.Equal(t, 100, usage.CompletionTokens)
	require.Equal(t, 110, usage.TotalTokens)
}

func TestGeminiChatHandlerMissingUsageMetadataBuildsEstimatedBillingUsage(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	info := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatGemini,
		OriginModelName: "gemini-3-flash-preview",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gemini-3-flash-preview",
		},
	}
	info.SetEstimatePromptTokens(20)

	body := []byte(`{"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]}}]}`)
	resp := &http.Response{
		Body: io.NopCloser(bytes.NewReader(body)),
	}

	usage, newAPIError := GeminiChatHandler(c, info, resp)
	require.Nil(t, newAPIError)
	require.NotNil(t, usage)
	require.Equal(t, 20, usage.PromptTokens)
	require.NotNil(t, usage.BillingUsage)
	require.True(t, usage.BillingUsage.Estimated)
	require.Equal(t, dto.BillingUsageSourceGeminiChat, usage.BillingUsage.Source)
	require.Equal(t, dto.BillingUsageSemanticGemini, usage.BillingUsage.Semantic)
	require.NotNil(t, usage.BillingUsage.GeminiUsageMetadata)
	require.Equal(t, usage.PromptTokens, usage.BillingUsage.GeminiUsageMetadata.PromptTokenCount)
	require.Equal(t, usage.CompletionTokens, usage.BillingUsage.GeminiUsageMetadata.CandidatesTokenCount)
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyLocalCountTokens))
}

func TestGeminiStreamHandlerPromptOnlyUsageMetadataEstimatesCompletionTokens(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	oldStreamingTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 300
	t.Cleanup(func() {
		constant.StreamingTimeout = oldStreamingTimeout
	})

	info := &relaycommon.RelayInfo{
		OriginModelName: "gemini-3-flash-preview",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gemini-3-flash-preview",
		},
	}
	info.SetEstimatePromptTokens(20)

	// Simulates a client aborting the stream before the final chunk: text was
	// streamed but the last observed usageMetadata only carries prompt tokens.
	chunk := dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{
			{
				Content: dto.GeminiChatContent{
					Role: "model",
					Parts: []dto.GeminiPart{
						{Text: "partial streamed answer before disconnect"},
					},
				},
			},
		},
		UsageMetadata: dto.GeminiUsageMetadata{
			PromptTokenCount: 151,
			TotalTokenCount:  151,
		},
	}

	chunkData, err := common.Marshal(chunk)
	require.NoError(t, err)

	streamBody := []byte("data: " + string(chunkData) + "\n" + "data: [DONE]\n")
	resp := &http.Response{
		Body: io.NopCloser(bytes.NewReader(streamBody)),
	}

	usage, newAPIError := geminiStreamHandler(c, info, resp, func(_ string, _ *dto.GeminiChatResponse) bool {
		return true
	})
	require.Nil(t, newAPIError)
	require.NotNil(t, usage)
	require.Equal(t, 151, usage.PromptTokens)
	require.Greater(t, usage.CompletionTokens, 0)
	require.Equal(t, usage.PromptTokens+usage.CompletionTokens, usage.TotalTokens)
	require.NotNil(t, usage.BillingUsage)
	require.True(t, usage.BillingUsage.Estimated)
	require.NotNil(t, usage.BillingUsage.GeminiUsageMetadata)
	require.Equal(t, usage.CompletionTokens, usage.BillingUsage.GeminiUsageMetadata.CandidatesTokenCount)
}

func TestGeminiChatHandlerPromptOnlyUsageMetadataEstimatesCompletionTokens(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	info := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatGemini,
		OriginModelName: "gemini-3-flash-preview",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gemini-3-flash-preview",
		},
	}

	payload := dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{
			{
				Content: dto.GeminiChatContent{
					Role: "model",
					Parts: []dto.GeminiPart{
						{Text: "answer text without candidate token count"},
					},
				},
			},
		},
		UsageMetadata: dto.GeminiUsageMetadata{
			PromptTokenCount: 151,
			TotalTokenCount:  151,
		},
	}

	body, err := common.Marshal(payload)
	require.NoError(t, err)

	resp := &http.Response{
		Body: io.NopCloser(bytes.NewReader(body)),
	}

	usage, newAPIError := GeminiChatHandler(c, info, resp)
	require.Nil(t, newAPIError)
	require.NotNil(t, usage)
	require.Equal(t, 151, usage.PromptTokens)
	require.Greater(t, usage.CompletionTokens, 0)
	require.Equal(t, usage.PromptTokens+usage.CompletionTokens, usage.TotalTokens)
	require.NotNil(t, usage.BillingUsage)
	require.True(t, usage.BillingUsage.Estimated)
}

func TestGeminiStreamHandlerEmptyUsageMetadataBuildsEstimatedBillingUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	oldStreamingTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 300
	t.Cleanup(func() {
		constant.StreamingTimeout = oldStreamingTimeout
	})

	info := &relaycommon.RelayInfo{
		OriginModelName: "gemini-3-flash-preview",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gemini-3-flash-preview",
		},
	}
	info.SetEstimatePromptTokens(20)

	streamBody := []byte("data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"partial\"}]}}],\"usageMetadata\":{}}\n" + "data: [DONE]\n")
	resp := &http.Response{
		Body: io.NopCloser(bytes.NewReader(streamBody)),
	}

	usage, newAPIError := geminiStreamHandler(c, info, resp, func(_ string, _ *dto.GeminiChatResponse) bool {
		return true
	})
	require.Nil(t, newAPIError)
	require.NotNil(t, usage)
	require.Equal(t, 20, usage.PromptTokens)
	require.NotNil(t, usage.BillingUsage)
	require.True(t, usage.BillingUsage.Estimated)
	require.Equal(t, dto.BillingUsageSourceGeminiChat, usage.BillingUsage.Source)
	require.NotNil(t, usage.BillingUsage.GeminiUsageMetadata)
	require.Equal(t, usage.PromptTokens, usage.BillingUsage.GeminiUsageMetadata.PromptTokenCount)
	require.Equal(t, usage.CompletionTokens, usage.BillingUsage.GeminiUsageMetadata.CandidatesTokenCount)
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyLocalCountTokens))
}
