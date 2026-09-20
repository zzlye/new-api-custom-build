package controller

import (
	"crypto/hmac"
	"encoding/hex"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func UploadVideoAsset(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, common.AsyncMediaMaxFileBytes+(1<<20))
	reader, err := c.Request.MultipartReader()
	if err != nil {
		c.JSON(400, gin.H{"error": gin.H{"message": "请使用文件上传表单"}})
		return
	}
	part, err := reader.NextPart()
	if err != nil || part.FileName() == "" {
		c.JSON(400, gin.H{"error": gin.H{"message": "请选择素材文件"}})
		return
	}
	defer part.Close()
	asset, err := service.SaveVideoAsset(c.GetInt("id"), part)
	if err != nil {
		c.JSON(400, gin.H{"error": gin.H{"message": err.Error()}})
		return
	}
	c.JSON(201, asset)
}

func GetVideoAssetContent(c *gin.Context) {
	id, task := c.Param("asset_id"), c.Query("task")
	actual, err := hex.DecodeString(c.Query("signature"))
	expected, _ := hex.DecodeString(common.GenerateHMACWithKey([]byte(common.SessionSecret), "video-asset-v1\n"+id+"\n"+task))
	if err != nil || !hmac.Equal(actual, expected) {
		c.AbortWithStatus(401)
		return
	}
	var use model.VideoAssetUse
	if err = model.DB.Where("asset_id = ? AND task_id = ?", id, task).First(&use).Error; err != nil {
		c.AbortWithStatus(404)
		return
	}
	active, err := model.VideoAssetUseActive(use, common.GetTimestamp())
	if err != nil {
		c.AbortWithStatus(500)
		return
	}
	if !active {
		c.AbortWithStatus(410)
		return
	}
	var asset model.VideoAsset
	if err = model.DB.Where("id = ? AND node_name = ?", id, common.NodeName).First(&asset).Error; err != nil {
		c.AbortWithStatus(404)
		return
	}
	c.Header("Content-Type", asset.ContentType)
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Cache-Control", "private, no-store")
	c.Header("Content-Security-Policy", "sandbox")
	c.File(asset.Path)
}
