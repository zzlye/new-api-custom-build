package dto

import (
	"encoding/json"
)

type TaskError struct {
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
	Name        string `json:"name,omitempty"`
	Role        string `json:"role,omitempty"`
	Error       string `json:"error,omitempty"`
	URL         string `json:"url"`
	Kind        string `json:"kind"`
	ContentType string `json:"content_type"`
}

type TaskDto struct {
	DurationFinishTime int64           `json:"duration_finish_time,omitempty"`
	RequestMethod      string          `json:"request_method,omitempty"`
	RequestPath        string          `json:"request_path,omitempty"`
	ModelName          string          `json:"model_name,omitempty"`
	MediaSaving        bool            `json:"media_saving,omitempty"`
	IsAsync            bool            `json:"is_async"`
	Media              []TaskMedia     `json:"media,omitempty"`
	MediaExpired       bool            `json:"media_expired"`
	ExpiresAt          int64           `json:"expires_at,omitempty"`
	ID                 int64           `json:"id"`
	CreatedAt          int64           `json:"created_at"`
	UpdatedAt          int64           `json:"updated_at"`
	TaskID             string          `json:"task_id"`
	Platform           string          `json:"platform"`
	UserId             int             `json:"user_id"`
	Group              string          `json:"group"`
	ChannelId          int             `json:"channel_id"`
	Quota              int             `json:"quota"`
	Action             string          `json:"action"`
	Status             string          `json:"status"`
	FailReason         string          `json:"fail_reason"`
	ResultURL          string          `json:"result_url,omitempty"` // 任务结果 URL（视频地址等）
	SubmitTime         int64           `json:"submit_time"`
	StartTime          int64           `json:"start_time"`
	FinishTime         int64           `json:"finish_time"`
	Progress           string          `json:"progress"`
	Properties         any             `json:"properties"`
	Username           string          `json:"username,omitempty"`
	Data               json.RawMessage `json:"data"`
}

type FetchReq struct {
	IDs []string `json:"ids"`
}

// AsyncTaskDetails 是任务日志的完整记录，不包含上游密钥、内部路径或原始请求头。
type AsyncTaskDetails struct {
	TaskID             string            `json:"task_id"`
	ModelName          string            `json:"model_name"`
	RequestMethod      string            `json:"request_method"`
	RequestPath        string            `json:"request_path"`
	RequestFormat      string            `json:"request_format"`
	Status             string            `json:"status"`
	Error              string            `json:"error,omitempty"`
	Prompt             string            `json:"prompt"`
	PromptSource       string            `json:"prompt_source,omitempty"`
	InputAvailable     bool              `json:"input_available"`
	InputError         string            `json:"input_error,omitempty"`
	Parameters         map[string]string `json:"parameters"`
	References         []TaskMedia       `json:"references"`
	Media              []TaskMedia       `json:"media"`
	MediaExpired       bool              `json:"media_expired"`
	ExpiresAt          int64             `json:"expires_at,omitempty"`
	SubmitTime         int64             `json:"submit_time"`
	StartTime          int64             `json:"start_time"`
	ResponseTime       int64             `json:"response_time"`
	FinishTime         int64             `json:"finish_time"`
	ResponseStatusCode int               `json:"response_status_code"`
}
