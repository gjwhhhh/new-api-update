package operation_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateRechargeCenterURL(t *testing.T) {
	testCases := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{name: "empty configuration", value: ""},
		{name: "HTTPS URL", value: "https://pay.example.com/recharge?plan=standard"},
		{name: "HTTP URL", value: "http://pay.example.com", wantErr: true},
		{name: "relative URL", value: "/recharge", wantErr: true},
		{name: "script URL", value: "javascript:alert(1)", wantErr: true},
		{name: "credentials", value: "https://user:password@pay.example.com", wantErr: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			err := ValidateRechargeCenterURL(testCase.value)
			if testCase.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestGetRechargeCenterSettingDisablesIncompleteConfiguration(t *testing.T) {
	previous := rechargeCenterSetting
	t.Cleanup(func() {
		rechargeCenterSetting = previous
	})

	rechargeCenterSetting = RechargeCenterSetting{
		Enabled:     true,
		URL:         "https://pay.example.com/recharge",
		DisplayMode: RechargeCenterDisplayModeEmbed,
	}
	assert.Equal(t, rechargeCenterSetting, GetRechargeCenterSetting())

	rechargeCenterSetting.URL = "https://user:password@pay.example.com"
	setting := GetRechargeCenterSetting()
	assert.False(t, setting.Enabled)
	assert.Empty(t, setting.URL)
	assert.Equal(t, RechargeCenterDisplayModeEmbed, setting.DisplayMode)

	rechargeCenterSetting = RechargeCenterSetting{
		Enabled:     true,
		URL:         "https://pay.example.com/recharge",
		DisplayMode: "unknown",
	}
	setting = GetRechargeCenterSetting()
	assert.True(t, setting.Enabled)
	assert.Equal(t, RechargeCenterDisplayModeRedirect, setting.DisplayMode)
}
