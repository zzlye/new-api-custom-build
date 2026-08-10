package controller

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/appearance_setting"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// UploadAppearanceMedia 根用户上传外观媒体（全局/主页/登录页背景，最大 200MB）
// 表单字段：file 必填；target 可选 global|home|login，默认 home
func UploadAppearanceMedia(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, appearance_setting.MaxUploadBytes+2<<20)

	file, err := c.FormFile("file")
	if err != nil {
		common.ApiErrorMsg(c, "请选择要上传的文件")
		return
	}
	if file.Size <= 0 {
		common.ApiErrorMsg(c, "文件为空")
		return
	}
	if file.Size > appearance_setting.MaxUploadBytes {
		common.ApiErrorMsg(c, "文件不能超过 200MB")
		return
	}
	if !appearance_setting.IsAllowedUploadExt(file.Filename) {
		common.ApiErrorMsg(c, "仅支持图片（jpg/png/webp/gif）或短视频（mp4/webm/mov）")
		return
	}

	// 上传目标：全局、主页或登录页
	target := strings.ToLower(strings.TrimSpace(c.PostForm("target")))
	if target != "global" && target != "login" {
		target = "home"
	}

	if err := os.MkdirAll(appearance_setting.UploadDir, 0o755); err != nil {
		common.ApiErrorMsg(c, "创建上传目录失败")
		return
	}

	ext := strings.ToLower(filepath.Ext(file.Filename))
	kind := appearance_setting.MediaKindFromExt(file.Filename)
	baseName := fmt.Sprintf("%s_%d", strings.ReplaceAll(uuid.NewString(), "-", ""), time.Now().Unix())
	name := baseName + ext
	if kind == appearance_setting.BgTypeVideo {
		name = baseName + ".source" + ext
	}
	destPath := filepath.Join(appearance_setting.UploadDir, name)

	src, err := file.Open()
	if err != nil {
		common.ApiErrorMsg(c, "读取上传文件失败")
		return
	}
	defer src.Close()

	dst, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		common.ApiErrorMsg(c, "保存文件失败")
		return
	}

	written, copyErr := io.Copy(dst, io.LimitReader(src, appearance_setting.MaxUploadBytes+1))
	closeErr := dst.Close()
	if copyErr != nil {
		_ = os.Remove(destPath)
		common.ApiErrorMsg(c, "写入文件失败")
		return
	}
	if written > appearance_setting.MaxUploadBytes {
		_ = os.Remove(destPath)
		common.ApiErrorMsg(c, "文件不能超过 200MB")
		return
	}
	if closeErr != nil {
		_ = os.Remove(destPath)
		common.ApiErrorMsg(c, "保存文件失败")
		return
	}

	originalSize := written
	optimized := false
	if kind == appearance_setting.BgTypeVideo {
		// 视频统一压缩为网页背景规格，避免高帧率素材持续占用解码资源。
		optimizedName := baseName + ".mp4"
		optimizedPath := filepath.Join(appearance_setting.UploadDir, optimizedName)
		optimizedSize, optimizeErr := appearance_setting.OptimizeBackgroundVideo(
			c.Request.Context(),
			destPath,
			optimizedPath,
		)
		_ = os.Remove(destPath)
		if optimizeErr != nil {
			_ = os.Remove(optimizedPath)
			common.SysError("optimize appearance video failed: " + optimizeErr.Error())
			common.ApiErrorMsg(c, "视频优化失败，请检查服务器 FFmpeg 配置")
			return
		}
		name = optimizedName
		destPath = optimizedPath
		written = optimizedSize
		optimized = true
	}

	url := appearance_setting.UploadURLPrefix + "/" + name

	bgTypeKey := "appearance_setting.home_bg_type"
	mediaKey := "appearance_setting.home_bg_media"
	if target == "global" {
		bgTypeKey = "appearance_setting.global_bg_type"
		mediaKey = "appearance_setting.global_bg_media"
	} else if target == "login" {
		bgTypeKey = "appearance_setting.login_bg_type"
		mediaKey = "appearance_setting.login_bg_media"
	}

	if err := model.UpdateOptionsBulk(map[string]string{
		bgTypeKey: kind,
		mediaKey:  url,
	}); err != nil {
		_ = os.Remove(destPath)
		common.SysError("update appearance after upload failed: " + err.Error())
		common.ApiErrorMsg(c, "更新外观设置失败")
		return
	}

	common.ApiSuccess(c, gin.H{
		"url":           url,
		"type":          kind,
		"size":          written,
		"original_size": originalSize,
		"optimized":     optimized,
		"name":          name,
		"target":        target,
	})
}
