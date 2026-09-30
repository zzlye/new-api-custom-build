package service

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/setting/video_setting"
	"github.com/gin-gonic/gin"
)

const VideoAllowedChannelsKey = "video_adapter_allowed_channels"
const VideoAdapterSnapshotKey = "video_adapter_snapshot"

type VideoAdapterSnapshot struct {
	Model    string                  `json:"model"`
	Protocol *video_setting.Protocol `json:"protocol"`
}

// 排队时冻结候选协议及模型映射，发布新模板不改变已经受理的任务。
func VideoProtocolForRequest(c *gin.Context, channelID int, upstream string) (*video_setting.Protocol, string, error) {
	if value, ok := c.Get(VideoAdapterSnapshotKey); ok {
		snapshots, _ := value.(map[int]VideoAdapterSnapshot)
		if snapshot, ok := snapshots[channelID]; ok {
			return snapshot.Protocol, snapshot.Model, nil
		}
		return nil, upstream, fmt.Errorf("该渠道不在任务受理时的候选范围内")
	}
	p, err := video_setting.Resolve(channelID, upstream)
	return p, upstream, err
}

func IsVideoAdapterPath(path string) bool {
	return path == "/v1/videos" || path == "/v1/video" || path == "/v1/video/generations"
}

// 渠道候选与实际请求使用相同的链式模型映射，禁止按名字相似程度猜测协议。
func VideoUpstreamModel(mapping, name string) (string, error) {
	if mapping == "" || mapping == "{}" {
		return name, nil
	}
	var names map[string]string
	if err := common.UnmarshalJsonStr(mapping, &names); err != nil {
		return "", err
	}
	seen := map[string]bool{}
	for {
		if seen[name] {
			return "", fmt.Errorf("模型映射存在循环")
		}
		seen[name] = true
		next := names[name]
		if next == "" || next == name {
			return name, nil
		}
		name = next
	}
}

func VideoRequestGroups(c *gin.Context) []string {
	group := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	if group == "" {
		group = common.GetContextKeyString(c, constant.ContextKeyUserGroup)
	}
	if group == "auto" {
		return GetRequestAutoGroups(c, common.GetContextKeyString(c, constant.ContextKeyUserGroup))
	}
	return []string{group}
}

func VideoCandidates(c *gin.Context, name string) ([]*model.Channel, error) {
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("缺少模型名")
	}
	if common.GetContextKeyBool(c, constant.ContextKeyTokenModelLimitEnabled) {
		value, _ := common.GetContextKey(c, constant.ContextKeyTokenModelLimit)
		limits, _ := value.(map[string]bool)
		if !limits[name] && !limits[ratio_setting.FormatMatchingModelName(name)] {
			return nil, fmt.Errorf("当前令牌无权使用此模型")
		}
	}
	channels, err := model.VideoChannelCandidates(VideoRequestGroups(c), name)
	if err != nil {
		return nil, err
	}
	if value, ok := common.GetContextKey(c, constant.ContextKeyTokenSpecificChannelId); ok {
		id, e := strconv.Atoi(fmt.Sprint(value))
		if e != nil {
			return nil, e
		}
		out := channels[:0]
		for _, ch := range channels {
			if ch.Id == id {
				out = append(out, ch)
			}
		}
		channels = out
	}
	return channels, nil
}

// 在选择渠道和预扣之前检查完整组合；能力未知的渠道不会被当作已知能力的万能备用。
func PrepareVideoChannelSelection(c *gin.Context, name string) error {
	if c == nil || c.Request == nil || c.Request.Method != http.MethodPost || !IsVideoAdapterPath(c.Request.URL.Path) {
		return nil
	}
	if _, ok := c.Get(VideoAllowedChannelsKey); ok {
		return nil
	}
	registry, err := video_setting.LoadRegistry()
	if err != nil {
		return err
	}
	// 没有定制规则时让官方插件直接接管；冻结任务仍必须使用受理时的候选。
	if _, frozen := c.Get(VideoAdapterSnapshotKey); !frozen && len(registry.Rules) == 0 {
		return nil
	}
	channels, err := VideoCandidates(c, name)
	if err != nil {
		return err
	}
	var input map[string]any
	allowed := map[int]bool{}
	snapshots := map[int]VideoAdapterSnapshot{}
	managed := false
	knownCapabilities := false
	var invalid error
	for _, ch := range channels {
		upstream, e := VideoUpstreamModel(ch.GetModelMapping(), name)
		if e != nil {
			return e
		}
		var p *video_setting.Protocol
		if _, frozen := c.Get(VideoAdapterSnapshotKey); frozen {
			p, upstream, e = VideoProtocolForRequest(c, ch.Id, upstream)
			if e != nil {
				continue
			}
		} else {
			p, e = registry.Resolve(ch.Id, upstream)
		}
		if e != nil {
			return e
		}
		snapshots[ch.Id] = VideoAdapterSnapshot{Model: upstream, Protocol: p}
		if p == nil {
			continue
		}
		managed = true
		knownCapabilities = knownCapabilities || p.Capabilities != nil
		// 完全没有适配规则时不读取请求体，保留历史 multipart 等入口行为。
		if input == nil {
			if e = common.UnmarshalBodyReusable(c, &input); e != nil {
				return e
			}
		}
		normalized, normalizeErr := p.Normalize(input)
		if normalizeErr == nil {
			_, normalizeErr = p.MapRequest(normalized)
		}
		if e = normalizeErr; e != nil {
			invalid = e
			continue
		}
		allowed[ch.Id] = true
	}
	if !managed {
		return nil
	}
	// 存在明确能力规则时，历史默认协议不能作为能力未知的万能备用渠道。
	if knownCapabilities {
		for id := range allowed {
			if snapshots[id].Protocol.Capabilities == nil {
				delete(allowed, id)
			}
		}
	}
	if len(allowed) == 0 {
		if invalid != nil {
			return fmt.Errorf("没有兼容本次参数的渠道：%w", invalid)
		}
		return fmt.Errorf("没有兼容本次参数的渠道")
	}
	// 在选优先级之前与插件、协议等官方约束求交集。
	GetChannelConstraints(c).AddFilter(dto.ChannelFilter{Kind: dto.FilterAllowedChannels, AllowedChannelIDs: allowed})
	c.Set(VideoAllowedChannelsKey, allowed)
	c.Set(VideoAdapterSnapshotKey, snapshots)
	return nil
}

func VideoChannelAllowed(c *gin.Context, id int) bool {
	value, ok := c.Get(VideoAllowedChannelsKey)
	if !ok {
		return true
	}
	allowed, _ := value.(map[int]bool)
	return allowed[id]
}
