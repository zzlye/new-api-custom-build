package appearance_setting

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeOverlayOpacity(t *testing.T) {
	cases := []struct {
		name  string
		input float64
		want  float64
	}{
		{name: "保留合法值", input: 0.65, want: 0.65},
		{name: "负数归零", input: -0.2, want: 0},
		{name: "大于一归一", input: 1.2, want: 1},
		{name: "NaN 使用默认值", input: math.NaN(), want: 0},
		{name: "无穷值使用默认值", input: math.Inf(1), want: 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setting := AppearanceSetting{
				ThemePreset:           "default",
				HomeBgOverlayOpacity:  tc.input,
				LoginBgOverlayOpacity: tc.input,
			}
			Normalize(&setting)
			assert.Equal(t, tc.want, setting.HomeBgOverlayOpacity)
			assert.Equal(t, tc.want, setting.LoginBgOverlayOpacity)
		})
	}
}

func TestNormalizeMissingOverlayOpacityKeepsLegacyDefault(t *testing.T) {
	setting := AppearanceSetting{
		ThemePreset: "default",
		HomeBgType:  BgTypeImage,
		LoginBgType: BgTypeVideo,
	}

	Normalize(&setting)

	require.Equal(t, 0.0, setting.HomeBgOverlayOpacity)
	require.Equal(t, 0.0, setting.LoginBgOverlayOpacity)
}

func TestPublicMapIncludesOverlayOpacity(t *testing.T) {
	original := appearanceSetting
	t.Cleanup(func() { appearanceSetting = original })
	appearanceSetting.HomeBgOverlayOpacity = 0.25
	appearanceSetting.LoginBgOverlayOpacity = 0.75

	public := PublicMap()
	assert.Equal(t, 0.25, public["home_bg_overlay_opacity"])
	assert.Equal(t, 0.75, public["login_bg_overlay_opacity"])
}
