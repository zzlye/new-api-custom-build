package appearance_setting

import (
	"path/filepath"
	"strings"

	"github.com/QuantumNous/new-api/setting/config"
)

// AppearanceSetting 站点外观（仅根用户可改，全站生效）
// 分类：
//  1. 整站配色方案 theme_preset
//  2. 主页背景 home_bg_*
//  3. 登录页背景 login_bg_*（登录/注册等鉴权页共用）
type AppearanceSetting struct {
	ThemePreset  string `json:"theme_preset"`
	HomeBgType   string `json:"home_bg_type"`
	HomeBgColor  string `json:"home_bg_color"`
	HomeBgMedia  string `json:"home_bg_media"`
	LoginBgType  string `json:"login_bg_type"`
	LoginBgColor string `json:"login_bg_color"`
	LoginBgMedia string `json:"login_bg_media"`
}

const (
	BgTypeNone  = "none"
	BgTypeSolid = "solid"
	BgTypeImage = "image"
	BgTypeVideo = "video"

	// 兼容旧命名
	HomeBgTypeNone  = BgTypeNone
	HomeBgTypeSolid = BgTypeSolid
	HomeBgTypeImage = BgTypeImage
	HomeBgTypeVideo = BgTypeVideo

	MaxUploadBytes  int64  = 200 << 20
	UploadDir       string = "data/appearance"
	UploadURLPrefix string = "/uploads/appearance"
)

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
	ThemePreset:  "default",
	HomeBgType:   BgTypeNone,
	HomeBgColor:  "#0f172a",
	HomeBgMedia:  "",
	LoginBgType:  BgTypeNone,
	LoginBgColor: "#0f172a",
	LoginBgMedia: "",
}

var appearanceSetting = defaultAppearanceSetting

func init() {
	config.GlobalConfig.Register("appearance_setting", &appearanceSetting)
}

// GetAppearanceSetting 返回当前外观配置
func GetAppearanceSetting() *AppearanceSetting {
	return &appearanceSetting
}

func normalizeBgType(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case BgTypeSolid, BgTypeImage, BgTypeVideo:
		return strings.ToLower(strings.TrimSpace(v))
	default:
		return BgTypeNone
	}
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

	s.HomeBgType = normalizeBgType(s.HomeBgType)
	if strings.TrimSpace(s.HomeBgColor) == "" {
		s.HomeBgColor = defaultAppearanceSetting.HomeBgColor
	}
	s.HomeBgMedia = strings.TrimSpace(s.HomeBgMedia)

	s.LoginBgType = normalizeBgType(s.LoginBgType)
	if strings.TrimSpace(s.LoginBgColor) == "" {
		s.LoginBgColor = defaultAppearanceSetting.LoginBgColor
	}
	s.LoginBgMedia = strings.TrimSpace(s.LoginBgMedia)
}

// PublicMap 供 /api/status 暴露
func PublicMap() map[string]any {
	s := *GetAppearanceSetting()
	Normalize(&s)
	return map[string]any{
		"theme_preset":   s.ThemePreset,
		"home_bg_type":   s.HomeBgType,
		"home_bg_color":  s.HomeBgColor,
		"home_bg_media":  s.HomeBgMedia,
		"login_bg_type":  s.LoginBgType,
		"login_bg_color": s.LoginBgColor,
		"login_bg_media": s.LoginBgMedia,
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
		return BgTypeVideo
	default:
		return BgTypeImage
	}
}
