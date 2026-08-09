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
