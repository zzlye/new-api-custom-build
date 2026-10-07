package dto

import (
	"encoding/json"
)

type TaskError struct {
	// NoRetry prevents duplicate upstream work after a response has been accepted.
	NoRetry    bool   `json:"-"`
	Code       string `json:"code"`
	Message    string `json:"message"`
	Data       any    `json:"data"`
	StatusCode int    `json:"-"`
	LocalError bool   `json:"-"`
	Error      error  `json:"-"`
}

type TaskData interface {
	SunoDataResponse | []SunoDataResponse | string | any
}

const TaskSuccessCode = "success"

type TaskResponse[T TaskData] struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Data    T      `json:"data"`
}

func (t *TaskResponse[T]) IsSuccess() bool {
	return t.Code == TaskSuccessCode
}

// TaskMedia 是任务日志和轮询接口共用的媒体预览信息。
type TaskMedia struct {
	PreviewURL  string `json:"preview_url,omitempty"`
	Name        string `json:"name,omitempty"`
	Role        string `json:"role,omitempty"`
	Error       string `json:"error,omitempty"`
	URL         string `json:"url"`
	Kind        string `json:"kind"`
	ContentType string `json:"content_type"`
}

type TaskDto struct {
	DurationFinishTime   int64       `json:"duration_finish_time,omitempty"`
	RequestMethod        string      `json:"request_method,omitempty"`
	RequestPath          string      `json:"request_path,omitempty"`
	ModelName            string      `json:"model_name,omitempty"`
	MediaSaving          bool        `json:"media_saving,omitempty"`
	IsAsync              bool        `json:"is_async"`
	Media                []TaskMedia `json:"media,omitempty"`
	MediaExpired         bool        `json:"media_expired"`
	ExpiresAt            int64       `json:"expires_at,omitempty"`
	ID                   int64       `json:"id"`
	CreatedAt            int64       `json:"created_at"`
	UpdatedAt            int64       `json:"updated_at"`
	TaskID               string      `json:"task_id"`
	Platform             string      `json:"platform"`
	UserId               int         `json:"user_id"`
	Group                string      `json:"group"`
	ChannelId            int         `json:"channel_id"`
	ChannelName          string      `json:"channel_name,omitempty"` // 仅管理员任务列表补充渠道名称。
	Quota                int         `json:"quota"`
	Action               string      `json:"action"`
	Status               string      `json:"status"`
	FailReason           string      `json:"fail_reason"`
	ResultURL            string      `json:"result_url,omitempty"` // 任务结果 URL（视频地址等）
	LegacyVideoAvailable bool        `json:"legacy_video_available,omitempty"`
	// ResultDiscarded marks a synchronous result that was returned inline and
	// never persisted; the UI must not offer artifact retrieval for it.
	ResultDiscarded bool            `json:"result_discarded,omitempty"`
	SubmitTime      int64           `json:"submit_time"`
	StartTime       int64           `json:"start_time"`
	FinishTime      int64           `json:"finish_time"`
	Progress        string          `json:"progress"`
	Properties      any             `json:"properties"`
	Username        string          `json:"username,omitempty"`
	Data            json.RawMessage `json:"data"`
	AdminInfo       *TaskAdminInfo  `json:"admin_info,omitempty"`
	RootInfo        *TaskRootInfo   `json:"root_info,omitempty"`
}

type TaskPluginInfo struct {
	Key     string                `json:"key"`
	Name    string                `json:"name"`
	Version string                `json:"version,omitempty"`
	Author  *TaskPluginAuthorInfo `json:"author,omitempty"`
}

type TaskPluginAuthorInfo struct {
	Name string `json:"name"`
	URL  string `json:"url,omitempty"`
}

type TaskPluginRuntimeInfo struct {
	Key        string `json:"key"`
	Version    string `json:"version"`
	APIVersion int    `json:"api_version"`
	Generation uint64 `json:"generation"`
}

type TaskAdminInfo struct {
	RequestID   string          `json:"request_id,omitempty"`
	RequestPath string          `json:"request_path,omitempty"`
	TaskPlugin  *TaskPluginInfo `json:"task_plugin,omitempty"`
}

type TaskRootInfo struct {
	TaskPlugin     *TaskPluginRuntimeInfo `json:"task_plugin,omitempty"`
	UpstreamTaskID string                 `json:"upstream_task_id,omitempty"`
	NodeName       string                 `json:"node_name,omitempty"`
}

type FetchReq struct {
	IDs []string `json:"ids"`
}

// AsyncTaskDetails 是任务日志的完整记录，不包含上游密钥、内部路径或原始请求头。
type AsyncTaskDetails struct {
	FullParameters     AsyncTaskParameterSnapshot `json:"full_parameters"`
	RoutingEvents      []TaskRoutingEvent         `json:"routing_events,omitempty"`
	TaskID             string                     `json:"task_id"`
	ModelName          string                     `json:"model_name"`
	RequestMethod      string                     `json:"request_method"`
	RequestPath        string                     `json:"request_path"`
	RequestFormat      string                     `json:"request_format"`
	Status             string                     `json:"status"`
	Error              string                     `json:"error,omitempty"`
	Prompt             string                     `json:"prompt"`
	PromptSource       string                     `json:"prompt_source,omitempty"`
	InputAvailable     bool                       `json:"input_available"`
	InputError         string                     `json:"input_error,omitempty"`
	Parameters         map[string]string          `json:"parameters"`
	References         []TaskMedia                `json:"references"`
	Media              []TaskMedia                `json:"media"`
	MediaExpired       bool                       `json:"media_expired"`
	ExpiresAt          int64                      `json:"expires_at,omitempty"`
	SubmitTime         int64                      `json:"submit_time"`
	StartTime          int64                      `json:"start_time"`
	ResponseTime       int64                      `json:"response_time"`
	FinishTime         int64                      `json:"finish_time"`
	ResponseStatusCode int                        `json:"response_status_code"`
}

// AsyncTaskParameterSnapshot 是任务详情中的完整参数快照，字段顺序与任务日志展示保持一致。
// 请求头、服务器路径和原始请求体不会写入快照，避免把鉴权信息带到前端。
type AsyncTaskParameterSnapshot struct {
	TaskID             string                   `json:"task_id"`
	ModelName          string                   `json:"model_name"`
	UpstreamModelName  string                   `json:"upstream_model_name"`
	ChannelID          int                      `json:"channel_id"`
	RequestMethod      string                   `json:"request_method"`
	RequestPath        string                   `json:"request_path"`
	RequestQuery       string                   `json:"request_query"`
	RequestContentType string                   `json:"request_content_type"`
	RequestFormat      string                   `json:"request_format"`
	RequestConversion  []string                 `json:"request_conversion"`
	RequestDetails     AsyncTaskRequestSnapshot `json:"request_details"`
	RequestBody        string                   `json:"request_body"`
	RequestFiles       string                   `json:"request_files"`
	CreatedAt          int64                    `json:"created_at"`
	StartedAt          int64                    `json:"started_at"`
	FinishedAt         int64                    `json:"finished_at"`
	CreatedBeijing     string                   `json:"created_beijing"`
	Status             string                   `json:"status"`
	ResponseStatusCode int                      `json:"response_status_code"`
	ResultExpiredAt    int64                    `json:"result_expired_at"`
}

// AsyncTaskRequestSnapshot 是脱敏后的请求详情，只保留用户需要核对的字段和值。
type AsyncTaskRequestSnapshot struct {
	Prompt       string                       `json:"prompt,omitempty"`
	PromptSource string                       `json:"prompt_source,omitempty"`
	Parameters   map[string]string            `json:"parameters,omitempty"`
	References   []AsyncTaskReferenceSnapshot `json:"references,omitempty"`
	CaptureError string                       `json:"capture_error,omitempty"`
}

// AsyncTaskReferenceSnapshot 只记录参考素材的类型，不暴露本地路径或远程签名地址。
type AsyncTaskReferenceSnapshot struct {
	Role        string `json:"role"`
	ContentType string `json:"content_type,omitempty"`
	Kind        string `json:"kind,omitempty"`
}

// TaskRoutingEvent 仅保存编号、错误分类和决策，不保留上游地址、密钥或原始响应。
type TaskRoutingEvent struct {
	Attempt   int    `json:"attempt"`
	ChannelID int    `json:"channel_id,omitempty"`
	Group     string `json:"group,omitempty"`
	Status    int    `json:"status,omitempty"`
	ErrorCode string `json:"error_code,omitempty"`
	ElapsedMS int64  `json:"elapsed_ms"`
	Decision  struct {
		Action string `json:"action"`
		Reason string `json:"reason"`
		Source string `json:"source"`
	} `json:"decision"`
}
