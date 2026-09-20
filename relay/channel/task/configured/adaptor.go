package configured

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/task/sora"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/video_setting"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/sjson"
)

// 可配置适配器复用原视频计费约束，仅替换与渠道协议有关的请求和响应。
type TaskAdaptor struct {
	sora.TaskAdaptor
	protocol    *video_setting.Protocol
	baseURL     string
	key         string
	contentType string
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	a.TaskAdaptor.Init(info)
	a.protocol = info.VideoProtocol
	a.baseURL = info.ChannelBaseUrl
	a.key = info.ApiKey
}

func (a *TaskAdaptor) SetVideoProtocol(p *video_setting.Protocol) { a.protocol = p }

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	if a.protocol == nil {
		return service.TaskErrorWrapperLocal(fmt.Errorf("任务缺少视频协议快照"), "invalid_video_protocol", http.StatusBadRequest)
	}
	if !strings.HasPrefix(c.GetHeader("Content-Type"), "application/json") {
		return service.TaskErrorWrapperLocal(fmt.Errorf("可配置视频入口需要 JSON 请求"), "invalid_request", http.StatusBadRequest)
	}
	return a.TaskAdaptor.ValidateRequestAndSetAction(c, info)
}

func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	return video_setting.Endpoint(a.baseURL, a.protocol.SubmitPath, "")
}

func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	a.protocol.Authorize(req, a.key)
	req.Header.Set("Content-Type", a.contentType)
	return nil
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	var input map[string]any
	if err := common.UnmarshalBodyReusable(c, &input); err != nil {
		return nil, err
	}
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil, err
	}
	input["model"] = info.UpstreamModelName
	// 转换使用校验和计费后的秒数，避免 duration 与 seconds 冲突。
	input["duration"] = taskcommon.NormalizeVideoDurationSeconds(req.Duration, 4)
	delete(input, "seconds")
	if _, ok := input["image_urls"]; !ok && req.InputReference != "" {
		input["image_urls"] = []string{req.InputReference}
	}
	delete(input, "input_reference")
	data, err := a.protocol.MapRequest(input)
	if err != nil {
		return nil, err
	}
	if a.protocol.Encoding == "json" {
		a.contentType = "application/json"
		return bytes.NewReader(data), nil
	}
	var fields map[string]any
	if err = common.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	values := url.Values{}
	for k, v := range fields {
		if text, ok := v.(string); ok {
			values.Set(k, text)
		} else {
			encoded, e := common.Marshal(v)
			if e != nil {
				return nil, e
			}
			values.Set(k, string(encoded))
		}
	}
	if a.protocol.Encoding == "form" {
		a.contentType = "application/x-www-form-urlencoded"
		return strings.NewReader(values.Encode()), nil
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for key, vals := range values {
		if err = writer.WriteField(key, vals[0]); err != nil {
			return nil, err
		}
	}
	if err = writer.Close(); err != nil {
		return nil, err
	}
	a.contentType = writer.FormDataContentType()
	return &body, nil
}

func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, body io.Reader) (*http.Response, error) {
	return channel.DoTaskApiRequest(a, c, info, body)
}

func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (string, []byte, *dto.TaskError) {
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusBadGateway)
	}
	id := video_setting.Read(data, a.protocol.Response.ID)
	if id == "" {
		return "", nil, service.TaskErrorWrapperLocal(fmt.Errorf("渠道响应未包含配置的任务 ID，请核对提交结果后再操作"), "invalid_video_response", http.StatusBadGateway)
	}
	// 创建成功后只返回公开任务 ID；上游原始响应和协议快照留在服务端。
	c.JSON(http.StatusOK, gin.H{"id": info.PublicTaskID, "object": "video", "status": "queued"})
	return id, data, nil
}

func (a *TaskAdaptor) FetchTask(base, key string, body map[string]any, proxy string) (*http.Response, error) {
	a.baseURL = base
	if a.protocol == nil {
		return nil, fmt.Errorf("任务缺少视频协议快照")
	}
	id, _ := body["task_id"].(string)
	endpoint, err := video_setting.Endpoint(base, a.protocol.PollPath, id)
	if err != nil {
		return nil, err
	}
	var reader io.Reader
	if a.protocol.PollMethod == "POST" {
		data, e := sjson.Set(`{}`, a.protocol.PollIDField, id)
		if e != nil {
			return nil, e
		}
		reader = strings.NewReader(data)
	}
	req, err := http.NewRequest(a.protocol.PollMethod, endpoint, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	a.protocol.Authorize(req, key)
	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, err
	}
	// 只查询已有任务；临时 HTTP 错误交给下一轮查询，不触发重新创建或退款。
	bounded := *client
	bounded.Timeout = 60 * time.Second
	bounded.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := bounded.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		resp.Body.Close()
		return nil, fmt.Errorf("视频状态查询 HTTP %d", resp.StatusCode)
	}
	return resp, nil
}

func (a *TaskAdaptor) ParseTaskResult(data []byte) (*relaycommon.TaskInfo, error) {
	if a.protocol == nil {
		return nil, fmt.Errorf("任务缺少视频协议快照")
	}
	status, err := a.protocol.ReadStatus(data)
	if err != nil {
		return nil, err
	}
	states := map[string]model.TaskStatus{"queued": model.TaskStatusQueued, "in_progress": model.TaskStatusInProgress, "completed": model.TaskStatusSuccess, "failed": model.TaskStatusFailure}
	result := &relaycommon.TaskInfo{Status: string(states[status])}
	if status == "completed" {
		result.Url = video_setting.Read(data, a.protocol.Response.URL)
		if result.Url != "" {
			parsed, parseErr := url.Parse(result.Url)
			if parseErr == nil && !parsed.IsAbs() {
				base, baseErr := url.Parse(strings.TrimRight(a.baseURL, "/") + "/")
				if baseErr == nil {
					parsed = base.ResolveReference(parsed)
				}
			}
			if parseErr != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil {
				return nil, fmt.Errorf("渠道结果需要有效的 HTTP(S) 视频地址")
			}
			result.Url = parsed.String()
		}
		if result.Url == "" && a.protocol.ContentPath == "" {
			return nil, fmt.Errorf("完成响应缺少视频地址，请检查结果映射")
		}
	}
	if status == "failed" {
		result.Reason = video_setting.Read(data, a.protocol.Response.Error)
		if result.Reason == "" {
			result.Reason = "视频生成失败"
		}
	}
	if progress, err := strconv.ParseFloat(strings.TrimSuffix(video_setting.Read(data, a.protocol.Response.Progress), "%"), 64); err == nil && progress >= 0 && progress <= 100 {
		result.Progress = fmt.Sprintf("%.0f%%", progress)
	}
	return result, nil
}

func (a *TaskAdaptor) ConvertToOpenAIVideo(task *model.Task) ([]byte, error) {
	response := map[string]any{"id": task.TaskID, "object": "video", "status": task.Status.ToVideoStatus(), "progress": strings.TrimSuffix(task.Progress, "%")}
	if task.Status == model.TaskStatusSuccess {
		response["url"] = taskcommon.BuildProxyURL(task.TaskID)
	}
	if task.Status == model.TaskStatusFailure {
		response["error"] = map[string]string{"message": task.FailReason}
	}
	return common.Marshal(response)
}
