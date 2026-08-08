package appearance_setting

import (
	"path/filepath"
	"strings"

	"github.com/QuantumNous/new-api/setting/config"
)

// AppearanceSetting 站点外观设置（仅根用户可改）
type AppearanceSetting struct {
	// HomeBgType 主页背景类型：none / solid / image / video
	HomeBgType string `json:"home_bg_type"`
	// HomeBgColor 纯色背景（CSS 颜色，如 #0f172a 或 oklch(...)）
	HomeBgColor string `json:"home_bg_color"`
	// HomeBgMedia 图片或短视频地址（本地上传或外链）
	HomeBgMedia string `json:"home_bg_media"`
	// SuccessTone 成功提示色预设（如欢迎回来 toast）
	SuccessTone string `json:"success_tone"`
}

const (
	HomeBgTypeNone  = "none"
	HomeBgTypeSolid = "solid"
	HomeBgTypeImage = "image"
	HomeBgTypeVideo = "video"

	// MaxUploadBytes 外观媒体最大上传体积 200MB
	MaxUploadBytes int64 = 200 << 20
	// UploadDir 本地上传目录（相对工作目录）
	UploadDir = "data/appearance"
	// UploadURLPrefix 对外访问前缀
	UploadURLPrefix = "/uploads/appearance"
)

// 允许的成功色预设（与前端 SUCCESS_TONES 对齐）
var allowedSuccessTones = map[string]struct{}{
	"default": {},
	"emerald": {},
	"teal":    {},
	"forest":  {},
	"blue":    {},
	"rose":    {},
	"amber":   {},
}

var defaultAppearanceSetting = AppearanceSetting{
	HomeBgType:  HomeBgTypeNone,
	HomeBgColor: "#0f172a",
	HomeBgMedia: "",
	SuccessTone: "default",
}

var appearanceSetting = defaultAppearanceSetting

func init() {
	// 注册分层配置，键前缀 appearance_setting.*
	config.GlobalConfig.Register("appearance_setting", &appearanceSetting)
}

// GetAppearanceSetting 返回当前外观配置
func GetAppearanceSetting() *AppearanceSetting {
	return &appearanceSetting
}

// Normalize 校正非法值
func Normalize(s *AppearanceSetting) {
	if s == nil {
		return
	}
	switch strings.ToLower(strings.TrimSpace(s.HomeBgType)) {
	case HomeBgTypeSolid, HomeBgTypeImage, HomeBgTypeVideo:
		s.HomeBgType = strings.ToLower(strings.TrimSpace(s.HomeBgType))
	default:
		s.HomeBgType = HomeBgTypeNone
	}
	if strings.TrimSpace(s.HomeBgColor) == "" {
		s.HomeBgColor = defaultAppearanceSetting.HomeBgColor
	}
	s.HomeBgMedia = strings.TrimSpace(s.HomeBgMedia)
	tone := strings.ToLower(strings.TrimSpace(s.SuccessTone))
	if _, ok := allowedSuccessTones[tone]; !ok {
		tone = "default"
	}
	s.SuccessTone = tone
}

// PublicMap 供 /api/status 暴露给所有访客
func PublicMap() map[string]any {
	s := *GetAppearanceSetting()
	Normalize(&s)
	return map[string]any{
		"home_bg_type":  s.HomeBgType,
		"home_bg_color": s.HomeBgColor,
		"home_bg_media": s.HomeBgMedia,
		"success_tone":  s.SuccessTone,
	}
}

// IsAllowedUploadExt 校验扩展名
func IsAllowedUploadExt(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".webp", ".gif", ".mp4", ".webm", ".mov":
		return true
	default:
		return false
	}
}

// MediaKindFromExt 根据扩展名返回 image 或 video
func MediaKindFromExt(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".mp4", ".webm", ".mov":
		return HomeBgTypeVideo
	default:
		return HomeBgTypeImage
	}
}
