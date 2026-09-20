package video_setting

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

const OptionPrefix = "VideoProtocol."
const Platform = "configured_video"

// 渠道协议只保存字段规则，不保存密钥；鉴权始终使用实际选中的渠道密钥。
type Protocol struct {
	Enabled     bool              `json:"enabled"`
	SubmitPath  string            `json:"submit_path"`
	PollPath    string            `json:"poll_path"`
	PollMethod  string            `json:"poll_method"`
	PollIDField string            `json:"poll_id_field"`
	ContentPath string            `json:"content_path"`
	Encoding    string            `json:"encoding"`
	AuthMode    string            `json:"auth_mode"`
	AuthName    string            `json:"auth_name"`
	AuthPrefix  string            `json:"auth_prefix"`
	Fields      []Field           `json:"fields"`
	Defaults    map[string]any    `json:"defaults"`
	Headers     map[string]string `json:"headers"`
	Response    Response          `json:"response"`
}

type Field struct {
	Source  string `json:"source"`
	Target  string `json:"target"`
	Format  string `json:"format"`
	ItemKey string `json:"item_key,omitempty"`
}

type Response struct {
	ID       string            `json:"id"`
	Status   string            `json:"status"`
	URL      string            `json:"url"`
	Error    string            `json:"error"`
	Progress string            `json:"progress"`
	States   map[string]string `json:"states"`
}

var fieldPath = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*(\.[A-Za-z0-9_-]+)*$`)
var headerName = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func Key(channelID int) string { return OptionPrefix + strconv.Itoa(channelID) }

func Load(channelID int) (*Protocol, error) {
	common.OptionMapRWMutex.RLock()
	raw := common.OptionMap[Key(channelID)]
	common.OptionMapRWMutex.RUnlock()
	if raw == "" {
		return nil, nil
	}
	var p Protocol
	if err := common.UnmarshalJsonStr(raw, &p); err != nil {
		return nil, err
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return &p, nil
}

func Presets() map[string]Protocol {
	p := Protocol{
		SubmitPath: "/v1/videos", PollPath: "/v1/videos/{id}", PollMethod: "GET", PollIDField: "task_id",
		ContentPath: "/v1/videos/{id}/content", Encoding: "json", AuthMode: "header", AuthName: "Authorization", AuthPrefix: "Bearer ",
		Defaults: map[string]any{}, Headers: map[string]string{},
		Response: Response{ID: "id|task_id|data.id|data.task_id|output.task_id", Status: "status|data.status|output.task_status", URL: "url|video_url|data.url|data.video_url|output.video_url|output.results.0.url", Error: "error.message|message|data.error.message", Progress: "progress|data.progress", States: map[string]string{
			"queued": "queued", "pending": "queued", "submitted": "queued", "processing": "in_progress", "running": "in_progress", "in_progress": "in_progress", "completed": "completed", "succeeded": "completed", "success": "completed", "done": "completed", "failed": "failed", "error": "failed", "cancelled": "failed", "canceled": "failed",
		}},
	}
	for _, source := range []string{"model", "prompt", "duration", "aspect_ratio", "resolution", "generate_audio", "image_urls", "audio_urls", "video_urls"} {
		p.Fields = append(p.Fields, Field{Source: source, Target: source, Format: "identity"})
	}
	// 每个预设独立分配字段，切换预设不会影响其他渠道或模型。
	sora := p
	sora.Fields = append([]Field(nil), p.Fields...)
	sora.Fields[2] = Field{Source: "duration", Target: "seconds", Format: "string"}
	sora.Fields[6] = Field{Source: "image_urls", Target: "input_reference", Format: "single_object", ItemKey: "image_url"}
	str := sora
	str.Fields = append([]Field(nil), sora.Fields...)
	str.Fields[6] = Field{Source: "image_urls", Target: "input_reference", Format: "single"}
	return map[string]Protocol{"standard": p, "reference_object": sora, "reference_string": str}
}

func (p *Protocol) Validate() error {
	if p == nil {
		return fmt.Errorf("视频协议不能为空")
	}
	if strings.Contains(p.SubmitPath, "{id}") {
		return fmt.Errorf("提交路径不能包含任务编号")
	}
	for name, value := range p.Headers {
		if !headerName.MatchString(name) || strings.ContainsAny(value, "\r\n") {
			return fmt.Errorf("附加请求头无效")
		}
		switch strings.ToLower(name) {
		case "host", "content-length", "content-type", "connection", "transfer-encoding":
			return fmt.Errorf("此请求头由服务自动设置：%s", name)
		}
	}
	for label, path := range map[string]string{"提交": p.SubmitPath, "查询": p.PollPath, "下载": p.ContentPath} {
		if path == "" && label == "下载" {
			continue
		}
		if _, err := Endpoint("https://channel.invalid", path, "task"); err != nil {
			return fmt.Errorf("%s路径：%w", label, err)
		}
	}
	if p.PollMethod != "GET" && p.PollMethod != "POST" {
		return fmt.Errorf("查询方式必须为 GET 或 POST")
	}
	if p.PollMethod == "GET" && !strings.Contains(p.PollPath, "{id}") {
		return fmt.Errorf("GET 查询路径需要包含 {id}")
	}
	if p.PollMethod == "POST" && !fieldPath.MatchString(p.PollIDField) {
		return fmt.Errorf("任务 ID 字段无效")
	}
	if p.Encoding != "json" && p.Encoding != "form" && p.Encoding != "multipart" {
		return fmt.Errorf("请求格式无效")
	}
	if p.AuthMode != "header" && p.AuthMode != "query" && p.AuthMode != "none" {
		return fmt.Errorf("鉴权方式无效")
	}
	if p.AuthMode != "none" && (!headerName.MatchString(p.AuthName) || strings.ContainsAny(p.AuthPrefix, "\r\n")) {
		return fmt.Errorf("鉴权字段无效")
	}
	if len(p.Fields) > 64 {
		return fmt.Errorf("字段规则最多 64 项")
	}
	seen := map[string]bool{}
	sources := map[string]bool{}
	for _, f := range p.Fields {
		if !fieldPath.MatchString(f.Source) || !fieldPath.MatchString(f.Target) {
			return fmt.Errorf("字段路径无效：%s → %s", f.Source, f.Target)
		}
		if seen[f.Target] {
			return fmt.Errorf("目标字段重复：%s", f.Target)
		}
		for target := range seen {
			if strings.HasPrefix(target, f.Target+".") || strings.HasPrefix(f.Target, target+".") {
				return fmt.Errorf("目标字段相互覆盖：%s", f.Target)
			}
		}
		seen[f.Target] = true
		sources[f.Source] = true
		for _, part := range strings.Split(f.Target, ".") {
			if (part == "seconds" || part == "duration") && f.Source != "duration" {
				return fmt.Errorf("时长必须来自已校验的 duration")
			}
		}
		if f.Source == "duration" && f.Format != "identity" && f.Format != "string" && f.Format != "number" {
			return fmt.Errorf("时长必须为数字或字符串")
		}
		switch f.Format {
		case "identity", "string", "number", "boolean", "single", "single_object", "objects", "frames":
		default:
			return fmt.Errorf("字段格式无效：%s", f.Format)
		}
		if (f.Format == "single_object" || f.Format == "objects") && !fieldPath.MatchString(f.ItemKey) {
			return fmt.Errorf("对象内字段不能为空")
		}
	}
	for _, required := range []string{"model", "prompt", "duration"} {
		if !sources[required] {
			return fmt.Errorf("缺少必要映射：%s", required)
		}
	}
	for key, value := range p.Defaults {
		if containsDuration(value) {
			return fmt.Errorf("时长请使用字段映射")
		}
		if !fieldPath.MatchString(key) {
			return fmt.Errorf("附加字段路径无效：%s", key)
		}
		for target := range seen {
			if key == target || strings.HasPrefix(key, target+".") || strings.HasPrefix(target, key+".") {
				return fmt.Errorf("附加字段覆盖参数：%s", key)
			}
		}
		// 时长只能来自通过计费校验的统一输入，不能用常量改变实际生成时长。
		for _, part := range strings.Split(key, ".") {
			if part == "duration" || part == "seconds" {
				return fmt.Errorf("时长请使用字段映射")
			}
		}
	}
	if p.Response.ID == "" || p.Response.Status == "" {
		return fmt.Errorf("任务 ID 和状态路径不能为空")
	}
	for _, paths := range []string{p.Response.ID, p.Response.Status, p.Response.URL, p.Response.Error, p.Response.Progress} {
		if paths == "" {
			continue
		}
		for _, path := range strings.Split(paths, "|") {
			if !fieldPath.MatchString(path) {
				return fmt.Errorf("响应路径无效：%s", path)
			}
		}
	}
	states := map[string]bool{}
	for name, state := range p.Response.States {
		normalized := strings.ToLower(strings.TrimSpace(name))
		if normalized == "" || states[normalized] {
			return fmt.Errorf("任务状态名称为空或重复")
		}
		states[normalized] = true
		switch state {
		case "queued", "in_progress", "completed", "failed":
		default:
			return fmt.Errorf("任务状态映射无效")
		}
	}
	return nil
}

// 路径始终限制在渠道地址下，任务 ID 按不透明值编码，避免改写主机或路径层级。
func Endpoint(base, path, id string) (string, error) {
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || strings.ContainsAny(path, "\\\r\n#") {
		return "", fmt.Errorf("请输入以 / 开头的相对路径")
	}
	if strings.Contains(strings.ReplaceAll(path, "{id}", ""), "{") || strings.Contains(strings.ReplaceAll(path, "{id}", ""), "}") {
		return "", fmt.Errorf("路径只支持 {id} 占位符")
	}
	decoded, err := url.PathUnescape(path)
	if err != nil {
		return "", err
	}
	for _, part := range strings.Split(strings.SplitN(decoded, "?", 2)[0], "/") {
		if part == ".." || part == "." {
			return "", fmt.Errorf("路径包含无效层级")
		}
	}
	baseURL, err := url.Parse(strings.TrimRight(base, "/"))
	if err != nil || baseURL.Host == "" || (baseURL.Scheme != "http" && baseURL.Scheme != "https") || baseURL.RawQuery != "" || baseURL.Fragment != "" || baseURL.User != nil {
		return "", fmt.Errorf("渠道地址无效")
	}
	parts := strings.SplitN(path, "?", 2)
	escaped := url.PathEscape(id)
	if id == "." || id == ".." {
		escaped = strings.ReplaceAll(id, ".", "%2E")
	}
	result := strings.TrimRight(base, "/") + strings.ReplaceAll(parts[0], "{id}", escaped)
	if len(parts) == 2 {
		result += "?" + strings.ReplaceAll(parts[1], "{id}", url.QueryEscape(id))
	}
	return result, nil
}

func (p *Protocol) Authorize(req *http.Request, key string) {
	for name, value := range p.Headers {
		req.Header.Set(name, value)
	}
	switch p.AuthMode {
	case "header":
		req.Header.Set(p.AuthName, p.AuthPrefix+key)
	case "query":
		q := req.URL.Query()
		q.Set(p.AuthName, p.AuthPrefix+key)
		req.URL.RawQuery = q.Encode()
	}
}

// 常量中的嵌套时长同样不能绕过请求校验和计费。
func containsDuration(value any) bool {
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			if key == "duration" || key == "seconds" || containsDuration(child) {
				return true
			}
		}
	case []any:
		for _, child := range v {
			if containsDuration(child) {
				return true
			}
		}
	}
	return false
}
