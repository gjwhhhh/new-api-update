package perf_metrics_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIncludeChannelTestEnabled(t *testing.T) {
	prev := GetSetting()
	t.Cleanup(func() {
		RestoreSettingForTest(prev)
	})

	RestoreSettingForTest(PerfMetricsSetting{
		Enabled:            true,
		IncludeChannelTest: true,
	})
	assert.True(t, IncludeChannelTestEnabled())

	RestoreSettingForTest(PerfMetricsSetting{
		Enabled:            true,
		IncludeChannelTest: false,
	})
	assert.False(t, IncludeChannelTestEnabled())

	RestoreSettingForTest(PerfMetricsSetting{
		Enabled:            false,
		IncludeChannelTest: true,
	})
	assert.False(t, IncludeChannelTestEnabled())
}
