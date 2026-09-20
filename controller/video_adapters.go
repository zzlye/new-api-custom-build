package controller

import (
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/video_setting"
	"github.com/gin-gonic/gin"
)

func GetVideoAdapters(c *gin.Context) {
	if !requireVideoProtocolRoot(c) {
		return
	}
	r, err := video_setting.LoadRegistry()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	channels := []struct {
		ID           int     `json:"id"`
		Name         string  `json:"name"`
		Models       string  `json:"models"`
		ModelMapping *string `json:"model_mapping"`
	}{}
	if err = model.DB.Model(&model.Channel{}).Select("id,name,models,model_mapping").Order("id").Find(&channels).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"registry": r, "channels": channels})
}

func SaveVideoAdapters(c *gin.Context) {
	if !requireVideoProtocolRoot(c) {
		return
	}
	var r video_setting.Registry
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 2<<20)
	if err := common.DecodeJson(c.Request.Body, &r); err != nil {
		c.JSON(400, gin.H{"success": false, "message": err.Error()})
		return
	}
	ids := map[int]bool{}
	for _, rule := range r.Rules {
		for _, id := range rule.ChannelIDs {
			ids[id] = true
		}
	}
	for id := range ids {
		if _, err := model.GetChannelById(id, false); err != nil {
			c.JSON(400, gin.H{"success": false, "message": fmt.Sprintf("渠道 %d 不存在", id)})
			return
		}
	}
	if err := model.SaveVideoAdapters(&r); err != nil {
		c.JSON(409, gin.H{"success": false, "message": err.Error()})
		return
	}
	common.ApiSuccess(c, &r)
}

func PreviewVideoAdapter(c *gin.Context) {
	if !requireVideoProtocolRoot(c) {
		return
	}
	var request struct {
		Registry  *video_setting.Registry `json:"registry"`
		ChannelID int                     `json:"channel_id"`
		Protocol  video_setting.Protocol  `json:"protocol"`
		Input     map[string]any          `json:"input"`
		Response  map[string]any          `json:"response"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 2<<20)
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiError(c, err)
		return
	}
	result := gin.H{}
	if request.Registry != nil {
		if err := request.Registry.Validate(); err != nil {
			common.ApiError(c, err)
			return
		}
		channel, err := model.GetChannelById(request.ChannelID, false)
		if err != nil {
			common.ApiError(c, fmt.Errorf("请选择预览渠道"))
			return
		}
		name, _ := request.Input["model"].(string)
		actual, err := service.VideoUpstreamModel(channel.GetModelMapping(), name)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		p, err := request.Registry.Resolve(channel.Id, actual)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		if p == nil {
			common.ApiError(c, fmt.Errorf("当前渠道与模型没有命中启用规则"))
			return
		}
		request.Protocol = *p
		request.Input["model"] = actual
		result["match"] = gin.H{"channel_id": channel.Id, "model": actual, "version": request.Registry.Version}
	}
	if err := request.Protocol.Validate(); err != nil {
		common.ApiError(c, err)
		return
	}
	if request.Input != nil {
		input, err := request.Protocol.Normalize(request.Input)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		data, err := request.Protocol.MapRequest(input)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		var output any
		if err = common.Unmarshal(data, &output); err != nil {
			common.ApiError(c, err)
			return
		}
		result["input"] = input
		result["output"] = output
	}
	if request.Response != nil {
		data, _ := common.Marshal(request.Response)
		status, err := request.Protocol.ReadStatus(data)
		result["response"] = gin.H{"id": video_setting.Read(data, request.Protocol.Response.ID), "status": status, "url": video_setting.Read(data, request.Protocol.Response.URL), "error": video_setting.Read(data, request.Protocol.Response.Error), "progress": video_setting.Read(data, request.Protocol.Response.Progress)}
		if err != nil {
			result["status_error"] = err.Error()
		}
	}
	common.ApiSuccess(c, result)
}

func GetVideoCapabilities(c *gin.Context) {
	name := c.Query("model")
	channels, err := service.VideoCandidates(c, name)
	if err != nil {
		c.JSON(403, gin.H{"error": gin.H{"message": err.Error()}})
		return
	}
	r, err := video_setting.LoadRegistry()
	if err != nil {
		c.JSON(500, gin.H{"error": gin.H{"message": err.Error()}})
		return
	}
	variants := []*video_setting.Capabilities{}
	seen := map[string]bool{}
	for _, ch := range channels {
		upstream, e := service.VideoUpstreamModel(ch.GetModelMapping(), name)
		if e != nil {
			continue
		}
		p, e := r.Resolve(ch.Id, upstream)
		if e != nil {
			c.JSON(500, gin.H{"error": gin.H{"message": e.Error()}})
			return
		}
		if p == nil || p.Capabilities == nil {
			continue
		}
		// 客户端只需要可调整的参数定义，内部固定参数和默认值保留在服务端。
		public := *p.Capabilities
		public.Parameters = nil
		for _, parameter := range p.Capabilities.Parameters {
			if parameter.Editable {
				public.Parameters = append(public.Parameters, parameter)
			}
		}
		data, _ := common.Marshal(&public)
		if !seen[string(data)] {
			seen[string(data)] = true
			variants = append(variants, &public)
		}
	}
	c.Header("Cache-Control", "private, no-store")
	c.JSON(200, gin.H{"model": name, "version": r.Version, "configured": len(variants) > 0, "variants": variants})
}
