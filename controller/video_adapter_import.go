package controller

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/video_setting"
	"github.com/gin-gonic/gin"
)

// 导入预览不发起上游请求、不写数据库，发布仍使用原有版本校验接口。
func PreviewVideoAdapterImport(c *gin.Context) {
	if !requireVideoProtocolRoot(c) {
		return
	}
	var request struct {
		Registry   *video_setting.Registry    `json:"registry"`
		Bundle     video_setting.ImportBundle `json:"bundle"`
		ChannelIDs []int                      `json:"channel_ids"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4<<20)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": fmt.Sprintf("配置文件解析失败：%v", err)})
		return
	}
	// 禁止尾随第二份 JSON，防止预览与发布对同一输入产生不同理解。
	if err := decoder.Decode(new(any)); err != io.EOF {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "配置文件只能包含一个 JSON 对象"})
		return
	}
	if request.Registry == nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "缺少当前配置草稿"})
		return
	}
	next, err := video_setting.MergeImport(*request.Registry, request.Bundle, request.ChannelIDs)
	if err == nil {
		ids := map[int]bool{}
		for _, rule := range next.Rules {
			for _, id := range rule.ChannelIDs {
				ids[id] = true
			}
		}
		for id := range ids {
			if _, channelErr := model.GetChannelById(id, false); channelErr != nil {
				err = fmt.Errorf("渠道 %d 不存在", id)
				break
			}
		}
	}
	if err == nil {
		bytes, marshalErr := common.Marshal(next)
		err = marshalErr
		if len(bytes) > 2<<20 {
			err = fmt.Errorf("合并后的配置超过发布大小上限，请减少导入内容")
		}
	}
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	common.ApiSuccess(c, next)
}

func DownloadVideoAdapterTemplate(c *gin.Context) {
	if !requireVideoProtocolRoot(c) {
		return
	}
	c.Header("Content-Disposition", `attachment; filename="video-adapters-template.json"`)
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "application/json; charset=utf-8", video_setting.AdapterImportTemplate)
}

func DownloadVideoAdapterGuide(c *gin.Context) {
	if !requireVideoProtocolRoot(c) {
		return
	}
	c.Header("Content-Disposition", `attachment; filename="video-adapters-guide.md"`)
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "text/markdown; charset=utf-8", video_setting.AdapterImportGuide)
}
