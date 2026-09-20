package controller

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/video_setting"
	"github.com/gin-gonic/gin"
)

func requireVideoProtocolRoot(c *gin.Context) bool {
	// 处理器再次验证根角色，普通管理员直接调用接口也不能读取或保存规则。
	if c.GetInt("role") != common.RoleRootUser {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"success": false, "message": "仅根用户可以管理视频渠道协议"})
		return false
	}
	return true
}

func ListVideoProtocols(c *gin.Context) {
	if !requireVideoProtocolRoot(c) {
		return
	}
	channels := make([]struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}, 0)
	if err := model.DB.Model(&model.Channel{}).Select("id", "name").Order("id").Find(&channels).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"channels": channels, "presets": video_setting.Presets()})
}

func GetVideoProtocol(c *gin.Context) {
	if !requireVideoProtocolRoot(c) {
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "渠道编号无效"})
		return
	}
	if _, err = model.GetChannelById(id, false); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "渠道不存在"})
		return
	}
	p, err := video_setting.Load(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if p == nil {
		preset := video_setting.Presets()["standard"]
		p = &preset
	}
	common.ApiSuccess(c, p)
}

func SaveVideoProtocol(c *gin.Context) {
	if !requireVideoProtocolRoot(c) {
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "渠道编号无效"})
		return
	}
	if _, err = model.GetChannelById(id, false); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "渠道不存在"})
		return
	}
	var p video_setting.Protocol
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	if err = common.DecodeJson(c.Request.Body, &p); err == nil {
		err = p.Validate()
	}
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	registry, err := video_setting.LoadRegistry()
	if err == nil {
		registry.SetDefaultProtocol(id, p)
		err = model.SaveVideoAdapters(registry)
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, p)
}
