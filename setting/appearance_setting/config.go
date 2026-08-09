package appearance_setting

import (
	"math"
	"path/filepath"
	"strings"

	"github.com/QuantumNous/new-api/setting/config"
)

// AppearanceSetting 站点外观（仅根用户可改，全站生效）
// 分类：
//  1. 整站配色方案 theme_preset
//  2. 全局背景 global_bg_*（公开页面、登录页和后台控制台共用）
//  3. 主页背景 home_bg_*
//  4. 登录页背景 login_bg_*（登录/注册等鉴权页共用）
//  5. 卡片毛玻璃效果 glass_*
type AppearanceSetting struct {
	ThemePreset            string  `json:"theme_preset"`
	GlobalBgType           string  `json:"global_bg_type"`
	GlobalBgColor          string  `json:"global_bg_color"`
	GlobalBgMedia          string  `json:"global_bg_media"`
	GlobalBgOverlayOpacity float64 `json:"global_bg_overlay_opacity"`
	HomeBgType             string  `json:"home_bg_type"`
	HomeBgColor            string  `json:"home_bg_color"`
	HomeBgMedia            string  `json:"home_bg_media"`
	HomeBgOverlayOpacity   float64 `json:"home_bg_overlay_opacity"`
	LoginBgType            string  `json:"login_bg_type"`
	LoginBgColor           string  `json:"login_bg_color"`
	LoginBgMedia           string  `json:"login_bg_media"`
	LoginBgOverlayOpacity  float64 `json:"login_bg_overlay_opacity"`
	GlassOpacity           float64 `json:"glass_opacity"`
	GlassBlur              float64 `json:"glass_blur"`
	GlassBorderOpacity     float64 `json:"glass_border_opacity"`
	GlassShadowOpacity     float64 `json:"glass_shadow_opacity"`
}

const (
	BgTypeNone  = "none"
	BgTypeSolid = "solid"
	BgTypeImage = "image"
	BgTypeVideo = "video"

	// 遮罩透明度的合法范围，0 表示完全不叠加黑色遮罩，1 表示完全不透明。
	MinBgOverlayOpacity = 0.0
	MaxBgOverlayOpacity = 1.0

	// 毛玻璃透明度、边框和阴影透明度均使用 0 到 1 的范围。
	MinGlassOpacity       = 0.0
	MaxGlassOpacity       = 1.0
	MinGlassBorderOpacity = 0.0
	MaxGlassBorderOpacity = 1.0
	MinGlassShadowOpacity = 0.0
	MaxGlassShadowOpacity = 1.0

	// 毛玻璃模糊半径使用像素，限制范围避免过大的渲染开销。
	MinGlassBlur = 0.0
	MaxGlassBlur = 40.0

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
	ThemePreset:            "default",
	GlobalBgType:           BgTypeNone,
	GlobalBgColor:          "#0f172a",
	GlobalBgMedia:          "",
	GlobalBgOverlayOpacity: 0,
	HomeBgType:             BgTypeNone,
	HomeBgColor:            "#0f172a",
	HomeBgMedia:            "",
	HomeBgOverlayOpacity:   0,
	LoginBgType:            BgTypeNone,
	LoginBgColor:           "#0f172a",
	LoginBgMedia:           "",
	LoginBgOverlayOpacity:  0,
	GlassOpacity:           0.72,
	GlassBlur:              16,
	GlassBorderOpacity:     0.35,
	GlassShadowOpacity:     0.35,
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

// normalizeOverlayOpacity 将遮罩透明度限制在 0 到 1，非法浮点值回退到默认值。
func normalizeOverlayOpacity(v, fallback float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return fallback
	}
	if v < MinBgOverlayOpacity {
		return MinBgOverlayOpacity
	}
	if v > MaxBgOverlayOpacity {
		return MaxBgOverlayOpacity
	}
	return v
}

// normalizeGlassBlur 将毛玻璃模糊半径限制在合法像素范围内。
func normalizeGlassBlur(v, fallback float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return fallback
	}
	if v < MinGlassBlur {
		return MinGlassBlur
	}
	if v > MaxGlassBlur {
		return MaxGlassBlur
	}
	return v
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

	s.GlobalBgType = normalizeBgType(s.GlobalBgType)
	s.GlobalBgColor = strings.TrimSpace(s.GlobalBgColor)
	if s.GlobalBgColor == "" {
		s.GlobalBgColor = defaultAppearanceSetting.GlobalBgColor
	}
	s.GlobalBgMedia = strings.TrimSpace(s.GlobalBgMedia)
	s.GlobalBgOverlayOpacity = normalizeOverlayOpacity(
		s.GlobalBgOverlayOpacity,
		defaultAppearanceSetting.GlobalBgOverlayOpacity,
	)

	s.HomeBgType = normalizeBgType(s.HomeBgType)
	s.HomeBgColor = strings.TrimSpace(s.HomeBgColor)
	if s.HomeBgColor == "" {
		s.HomeBgColor = defaultAppearanceSetting.HomeBgColor
	}
	s.HomeBgMedia = strings.TrimSpace(s.HomeBgMedia)
	s.HomeBgOverlayOpacity = normalizeOverlayOpacity(
		s.HomeBgOverlayOpacity,
		defaultAppearanceSetting.HomeBgOverlayOpacity,
	)

	s.LoginBgType = normalizeBgType(s.LoginBgType)
	s.LoginBgColor = strings.TrimSpace(s.LoginBgColor)
	if s.LoginBgColor == "" {
		s.LoginBgColor = defaultAppearanceSetting.LoginBgColor
	}
	s.LoginBgMedia = strings.TrimSpace(s.LoginBgMedia)
	s.LoginBgOverlayOpacity = normalizeOverlayOpacity(
		s.LoginBgOverlayOpacity,
		defaultAppearanceSetting.LoginBgOverlayOpacity,
	)

	s.GlassOpacity = normalizeOverlayOpacity(s.GlassOpacity, defaultAppearanceSetting.GlassOpacity)
	s.GlassBlur = normalizeGlassBlur(s.GlassBlur, defaultAppearanceSetting.GlassBlur)
	s.GlassBorderOpacity = normalizeOverlayOpacity(
		s.GlassBorderOpacity,
		defaultAppearanceSetting.GlassBorderOpacity,
	)
	s.GlassShadowOpacity = normalizeOverlayOpacity(
		s.GlassShadowOpacity,
		defaultAppearanceSetting.GlassShadowOpacity,
	)
}

// PublicMap 供 /api/status 暴露
func PublicMap() map[string]any {
	s := *GetAppearanceSetting()
	Normalize(&s)
	return map[string]any{
		"theme_preset":              s.ThemePreset,
		"global_bg_type":            s.GlobalBgType,
		"global_bg_color":           s.GlobalBgColor,
		"global_bg_media":           s.GlobalBgMedia,
		"global_bg_overlay_opacity": s.GlobalBgOverlayOpacity,
		"home_bg_type":              s.HomeBgType,
		"home_bg_color":             s.HomeBgColor,
		"home_bg_media":             s.HomeBgMedia,
		"home_bg_overlay_opacity":   s.HomeBgOverlayOpacity,
		"login_bg_type":             s.LoginBgType,
		"login_bg_color":            s.LoginBgColor,
		"login_bg_media":            s.LoginBgMedia,
		"login_bg_overlay_opacity":  s.LoginBgOverlayOpacity,
		"glass_opacity":             s.GlassOpacity,
		"glass_blur":                s.GlassBlur,
		"glass_border_opacity":      s.GlassBorderOpacity,
		"glass_shadow_opacity":      s.GlassShadowOpacity,
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
