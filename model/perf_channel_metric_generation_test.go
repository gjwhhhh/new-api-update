package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClearPerfChannelMetricInRangeFencesStaleFlush(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&PerfChannelMetric{}, &PerfChannelMetricState{}))
	const channelID = 930002
	const modelName = "perf-channel-generation-fence"
	require.NoError(t, DB.Where("channel_id = ?", channelID).Delete(&PerfChannelMetric{}).Error)
	require.NoError(t, DB.Where("channel_id = ?", channelID).Delete(&PerfChannelMetricState{}).Error)
	t.Cleanup(func() {
		_ = DB.Where("channel_id = ?", channelID).Delete(&PerfChannelMetric{}).Error
		_ = DB.Where("channel_id = ?", channelID).Delete(&PerfChannelMetricState{}).Error
	})

	stale := &PerfChannelMetric{
		ChannelId:    channelID,
		ModelName:    modelName,
		BucketTs:     1000,
		RequestCount: 1,
		SuccessCount: 1,
	}
	persisted, err := UpsertPerfChannelMetricIfCurrentGeneration(stale, 0)
	require.NoError(t, err)
	require.True(t, persisted)

	generation, err := ClearPerfChannelMetricInRange(channelID, 900, 1100)
	require.NoError(t, err)
	require.Equal(t, int64(1), generation)

	persisted, err = UpsertPerfChannelMetricIfCurrentGeneration(stale, 0)
	require.NoError(t, err)
	assert.False(t, persisted)

	var rows []PerfChannelMetric
	require.NoError(t, DB.Where("channel_id = ? AND model_name = ?", channelID, modelName).Find(&rows).Error)
	require.Empty(t, rows)

	fresh := *stale
	fresh.RequestCount = 2
	fresh.SuccessCount = 2
	persisted, err = UpsertPerfChannelMetricIfCurrentGeneration(&fresh, generation)
	require.NoError(t, err)
	assert.True(t, persisted)

	require.NoError(t, DB.Where("channel_id = ? AND model_name = ?", channelID, modelName).Find(&rows).Error)
	require.Len(t, rows, 1)
	assert.Equal(t, int64(2), rows[0].RequestCount)
	assert.Equal(t, int64(2), rows[0].SuccessCount)
}
