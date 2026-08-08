package appearance_setting

import (
	"path/filepath"
	"strings"

	"github.com/QuantumNous/new-api/setting/config"
)

// AppearanceSetting 站点外观（仅根用户可改，全站生效）
// 分类：
//  1. 整站配色方案 theme_preset —— 一次切换主色/成功/警告等整套语义色
//  2. 主页背景 home_bg_* —— 纯色 / 图片 / 短视频
type AppearanceSetting struct {
	// ThemePreset 整站配色预设，对应前端 THEME_PRESETS
	ThemePreset string `json:"theme_preset"`
	// HomeBgType 主页背景：none / solid / image / video
	HomeBgType string `json:"home_bg_type"`
	// HomeBgColor 纯色背景
	HomeBgColor string `json:"home_bg_color"`
	// HomeBgMedia 图片或短视频 URL
	HomeBgMedia string `json:"home_bg_media"`
}

const (
	HomeBgTypeNone  = "none"
	HomeBgTypeSolid = "solid"
	HomeBgTypeImage = "image"
	HomeBgTypeVideo = "video"

	MaxUploadBytes  int64  = 200 << 20
	UploadDir       string = "data/appearance"
	UploadURLPrefix string = "/uploads/appearance"
)

// 与前端 THEME_PRESETS 对齐
var allowedThemePresets = map[string]struct{}{
	"default":        {},
	"anthropic":      {},
	"simple-large":   {},
	"underground":    {},
	"rose-garden":    {},
	"lake-view":      {},
	"sunset-glow":    {},
	"forest-whisper": {},
	"ocean-breeze":   {},
	"lavender-dream": {},
}

var defaultAppearanceSetting = AppearanceSetting{
	ThemePreset: "default",
	HomeBgType:  HomeBgTypeNone,
	HomeBgColor: "#0f172a",
	HomeBgMedia: "",
}

var appearanceSetting = defaultAppearanceSetting

func init() {
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
	preset := strings.ToLower(strings.TrimSpace(s.ThemePreset))
	if _, ok := allowedThemePresets[preset]; !ok {
		preset = "default"
	}
	s.ThemePreset = preset

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
}

// PublicMap 供 /api/status 暴露
func PublicMap() map[string]any {
	s := *GetAppearanceSetting()
	Normalize(&s)
	return map[string]any{
		"theme_preset":  s.ThemePreset,
		"home_bg_type":  s.HomeBgType,
		"home_bg_color": s.HomeBgColor,
		"home_bg_media": s.HomeBgMedia,
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
