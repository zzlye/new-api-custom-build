package model

// AsyncRelayRequestDetails 只保留展示所需的输入信息，鉴权头和未列入白名单的字段不进入详情。
type AsyncRelayRequestDetails struct {
	Prompt       string                `json:"prompt,omitempty"`
	PromptSource string                `json:"prompt_source,omitempty"`
	Parameters   map[string]string     `json:"parameters,omitempty"`
	References   []AsyncRelayReference `json:"references,omitempty"`
	CaptureError string                `json:"capture_error,omitempty"`
}

// AsyncRelayReference 的文件路径和远程地址只供服务器读取，接口仅返回经过鉴权的预览地址。
type AsyncRelayReference struct {
	Path        string `json:"path,omitempty"`
	Source      string `json:"source,omitempty"`
	Name        string `json:"name,omitempty"`
	Role        string `json:"role"`
	ContentType string `json:"content_type,omitempty"`
	Kind        string `json:"kind,omitempty"`
	Error       string `json:"error,omitempty"`
}
