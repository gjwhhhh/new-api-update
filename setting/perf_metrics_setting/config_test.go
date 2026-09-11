package perf_metrics_setting

import (
	"fmt"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/setting/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestSampleGenerationConfigReloadReplacesProtectedMaps(t *testing.T) {
	previous := GetSetting()
	t.Cleanup(func() {
		RestoreSettingForTest(previous)
	})
	RestoreSettingForTest(PerfMetricsSetting{
		GroupSampleGeneration:   map[string]int64{"old-group": 1},
		ChannelSampleGeneration: map[string]int64{"1": 1},
	})

	require.NoError(t, config.UpdateConfigFromMap(&perfMetricsSetting, map[string]string{
		"group_sample_generation":   `{"new-group":2}`,
		"channel_sample_generation": `{"9":3}`,
	}))

	assert.Zero(t, GetGroupSampleGeneration("old-group"))
	assert.Equal(t, int64(2), GetGroupSampleGeneration("new-group"))
	assert.Zero(t, GetChannelSampleGeneration(1))
	assert.Equal(t, int64(3), GetChannelSampleGeneration(9))
}

func TestSampleGenerationAccessIsSafeDuringConcurrentClearUpdates(t *testing.T) {
	previous := GetSetting()
	t.Cleanup(func() {
		RestoreSettingForTest(previous)
	})
	RestoreSettingForTest(PerfMetricsSetting{
		GroupSampleGeneration:   map[string]int64{},
		ChannelSampleGeneration: map[string]int64{},
	})

	start := make(chan struct{})
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			for range 64 {
				_ = GetGroupSampleGeneration("concurrent")
				_ = GetChannelSampleGeneration(9)
			}
		}()
	}
	wait.Add(1)
	go func() {
		defer wait.Done()
		<-start
		for generation := int64(1); generation <= 64; generation++ {
			SetGroupSampleGenerationInMemory("concurrent", generation)
			SetChannelSampleGenerationInMemory(9, generation)
		}
	}()
	close(start)
	wait.Wait()

	assert.Equal(t, int64(64), GetGroupSampleGeneration("concurrent"))
	assert.Equal(t, int64(64), GetChannelSampleGeneration(9))
}

func TestSampleGenerationDoesNotMoveBackwardAfterConcurrentClears(t *testing.T) {
	previous := GetSetting()
	t.Cleanup(func() {
		RestoreSettingForTest(previous)
	})
	RestoreSettingForTest(PerfMetricsSetting{
		GroupSampleGeneration:   map[string]int64{"group": 5},
		ChannelSampleGeneration: map[string]int64{"9": 5},
	})

	SetGroupSampleGenerationInMemory("group", 4)
	SetChannelSampleGenerationInMemory(9, 4)

	assert.Equal(t, int64(5), GetGroupSampleGeneration("group"))
	assert.Equal(t, int64(5), GetChannelSampleGeneration(9))
}

func TestSampleGenerationAccessIsSafeDuringConfigReload(t *testing.T) {
	previous := GetSetting()
	t.Cleanup(func() {
		RestoreSettingForTest(previous)
	})
	RestoreSettingForTest(PerfMetricsSetting{
		GroupSampleGeneration:   map[string]int64{},
		ChannelSampleGeneration: map[string]int64{},
	})

	start := make(chan struct{})
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			for range 64 {
				_ = GetGroupSampleGeneration("reload")
				_ = GetChannelSampleGeneration(9)
			}
		}()
	}
	errCh := make(chan error, 1)
	wait.Add(1)
	go func() {
		defer wait.Done()
		<-start
		for generation := int64(1); generation <= 64; generation++ {
			err := config.UpdateConfigFromMap(&perfMetricsSetting, map[string]string{
				"group_sample_generation":   fmt.Sprintf(`{"reload":%d}`, generation),
				"channel_sample_generation": fmt.Sprintf(`{"9":%d}`, generation),
			})
			if err != nil {
				errCh <- err
				return
			}
		}
	}()
	close(start)
	wait.Wait()
	select {
	case err := <-errCh:
		require.NoError(t, err)
	default:
	}

	assert.Equal(t, int64(64), GetGroupSampleGeneration("reload"))
	assert.Equal(t, int64(64), GetChannelSampleGeneration(9))
}
