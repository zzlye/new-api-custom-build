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

// UploadAppearanceMedia 根用户上传主页背景图片/短视频（最大 200MB）
func UploadAppearanceMedia(c *gin.Context) {
	// 限制请求体，略大于 200MB 以容纳 multipart 边界
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

	if err := os.MkdirAll(appearance_setting.UploadDir, 0o755); err != nil {
		common.ApiErrorMsg(c, "创建上传目录失败")
		return
	}

	ext := strings.ToLower(filepath.Ext(file.Filename))
	name := fmt.Sprintf("%s_%d%s", strings.ReplaceAll(uuid.NewString(), "-", ""), time.Now().Unix(), ext)
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
	defer dst.Close()

	written, err := io.Copy(dst, io.LimitReader(src, appearance_setting.MaxUploadBytes+1))
	if err != nil {
		_ = os.Remove(destPath)
		common.ApiErrorMsg(c, "写入文件失败")
		return
	}
	if written > appearance_setting.MaxUploadBytes {
		_ = os.Remove(destPath)
		common.ApiErrorMsg(c, "文件不能超过 200MB")
		return
	}

	kind := appearance_setting.MediaKindFromExt(file.Filename)
	url := appearance_setting.UploadURLPrefix + "/" + name

	// 上传后同步写回外观配置，便于立即预览
	bgTypeKey := "appearance_setting.home_bg_type"
	mediaKey := "appearance_setting.home_bg_media"
	if err := model.UpdateOptionsBulk(map[string]string{
		bgTypeKey: kind,
		mediaKey:  url,
	}); err != nil {
		common.SysError("update appearance after upload failed: " + err.Error())
		// 文件已落盘，仍返回 URL，前端可再手动保存
	}

	common.ApiSuccess(c, gin.H{
		"url":  url,
		"type": kind,
		"size": written,
		"name": name,
	})
}
