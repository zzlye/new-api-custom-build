package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateOptionValueRejectsInvalidMaxTokenAutoGroups(t *testing.T) {
	for _, value := range []string{"", "0", "-1", "1.5", "invalid"} {
		t.Run(value, func(t *testing.T) {
			assert.Error(t, validateOptionValue("MaxTokenAutoGroups", value))
		})
	}
	require.NoError(t, validateOptionValue("MaxTokenAutoGroups", "999999"))
}

func TestValidateAppearanceOverlayOpacity(t *testing.T) {
	validValues := []string{"0", "0.3", "1", "1.000000"}
	for _, value := range validValues {
		t.Run("合法_"+value, func(t *testing.T) {
			require.NoError(t, validateOptionValue("appearance_setting.home_bg_overlay_opacity", value))
			require.NoError(t, validateOptionValue("appearance_setting.login_bg_overlay_opacity", value))
		})
	}

	invalidValues := []string{"", "-0.1", "1.1", "NaN", "+Inf", "-Inf", "not-a-number"}
	for _, value := range invalidValues {
		t.Run("非法_"+value, func(t *testing.T) {
			require.Error(t, validateOptionValue("appearance_setting.home_bg_overlay_opacity", value))
			require.Error(t, validateOptionValue("appearance_setting.login_bg_overlay_opacity", value))
		})
	}
}

func TestValidateAppearanceGlobalBackgroundAndGlassOptions(t *testing.T) {
	for _, key := range []string{
		"appearance_setting.global_bg_overlay_opacity",
		"appearance_setting.glass_opacity",
		"appearance_setting.glass_border_opacity",
		"appearance_setting.glass_shadow_opacity",
	} {
		t.Run(key+"_合法", func(t *testing.T) {
			require.NoError(t, validateOptionValue(key, "0.35"))
			require.NoError(t, validateOptionValue(key, "1"))
		})
		t.Run(key+"_非法", func(t *testing.T) {
			require.Error(t, validateOptionValue(key, "-0.1"))
			require.Error(t, validateOptionValue(key, "1.1"))
			require.Error(t, validateOptionValue(key, "NaN"))
		})
	}

	for _, value := range []string{"0", "16", "40", "12.5"} {
		require.NoError(t, validateOptionValue("appearance_setting.glass_blur", value))
	}
	for _, value := range []string{"-0.1", "40.1", "NaN", "not-a-number"} {
		require.Error(t, validateOptionValue("appearance_setting.glass_blur", value))
	}
}
