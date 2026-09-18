package controller

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

// 复现线上香蕉的结果结构，使用内嵌图片走完整归档流程，避免调用收费生成接口。
func TestAsyncRelayArchivesGeminiMarkdownImage(t *testing.T) {
	prepareAsyncMediaController(t)
	task := &model.AsyncRelayTask{UserID: 91, NodeID: common.NodeName}
	require.NoError(t, task.InsertWithLog("default", "IMAGE"))
	claimed, won, err := model.ClaimAsyncRelayTask(task.ID, "markdown-fixture")
	require.NoError(t, err)
	require.True(t, won)
	path, file, err := common.CreateAsyncMediaFile()
	require.NoError(t, err)
	_, err = fmt.Fprintf(file, `{"candidates":[{"content":{"parts":[{"text":"![image](data:image/png;base64,%s)"}]}}]}`, asyncFixturePNG)
	require.NoError(t, err)
	require.NoError(t, file.Close())
	require.True(t, completeAsyncRelayResult(context.Background(), claimed, path, "application/json"))
	require.Equal(t, model.AsyncRelayTaskStatusSucceeded, claimed.Status, claimed.Error)
	var media []model.AsyncRelayMedia
	require.NoError(t, common.Unmarshal([]byte(claimed.ResultFiles), &media))
	require.Len(t, media, 1)
	require.Equal(t, "image/png", media[0].ContentType)
}

func TestAsyncMarkdownResultExtraction(t *testing.T) {
	cases := []struct {
		name, body string
		urls       []string
	}{
		{"香蕉响应", `{"candidates":[{"content":{"parts":[{"text":"![image](https://cdn.example/a.png)"}]},"finishReason":"STOP"}]}`, []string{"https://cdn.example/a.png"}},
		{"聊天Markdown", `{"choices":[{"message":{"content":"图片：![a](https://cdn.example/a.png) ![b](<https://cdn.example/b.png> \"标题\")"}}]}`, []string{"https://cdn.example/a.png", "https://cdn.example/b.png"}},
		{"签名和括号", `{"candidates":[{"content":{"parts":[{"text":"![image](http://cdn.example:9090/a(1).png?sig=a%2Fb&x=1)"}]}}]}`, []string{"http://cdn.example:9090/a(1).png?sig=a%2Fb&x=1"}},
		{"忽略提示词和普通链接", `{"prompt":"![image](https://evil.example/p.png)","error":{"message":"![image](https://evil.example/e.png)"},"choices":[{"message":{"content":"[链接](https://evil.example/l.png)"}}]}`, nil},
		{"忽略其他协议", `{"data":[{"text":"![image](file:///etc/passwd) ![image](javascript:alert)"}]}`, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, stream := range []bool{false, true} {
				body, ct := tc.body, "application/json"
				if stream {
					body, ct = "data: "+body+"\n\ndata: [DONE]\n\n", "text/event-stream"
				}
				sources, err := readAsyncRelayMediaSources(strings.NewReader(body), ct)
				if len(tc.urls) > 0 {
					require.NoError(t, err)
				}
				require.Len(t, sources, len(tc.urls))
				for i, want := range tc.urls {
					require.Equal(t, want, sources[i].Value)
				}
			}
		})
	}
}
