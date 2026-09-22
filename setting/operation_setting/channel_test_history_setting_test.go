package operation_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelTestHistorySettingValidatesRetention(t *testing.T) {
	setting := ChannelTestHistorySetting{Enabled: false, RetentionDays: 7}
	require.NoError(t, setting.UpdateConfigFromMap(map[string]string{
		"enabled":        "true",
		"retention_days": "30",
	}))
	assert.True(t, setting.Enabled)
	assert.Equal(t, 30, setting.RetentionDays)

	err := setting.UpdateConfigFromMap(map[string]string{"retention_days": "0"})
	require.Error(t, err)
	assert.Equal(t, 30, setting.RetentionDays)

	err = setting.UpdateConfigFromMap(map[string]string{"retention_days": "91"})
	require.Error(t, err)
	assert.Equal(t, 30, setting.RetentionDays)
}
