package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClearPerfMetricGroupInRangeFencesStaleFlush(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&PerfMetric{}, &PerfMetricGroupState{}))
	require.NoError(t, DB.Exec("DELETE FROM perf_metrics").Error)
	require.NoError(t, DB.Exec("DELETE FROM perf_metric_group_states").Error)
	t.Cleanup(func() {
		_ = DB.Exec("DELETE FROM perf_metrics").Error
		_ = DB.Exec("DELETE FROM perf_metric_group_states").Error
	})

	stale := &PerfMetric{
		ModelName:    "generation-fence-model",
		Group:        "generation-fence-group",
		BucketTs:     1000,
		RequestCount: 1,
		SuccessCount: 1,
	}
	persisted, err := UpsertPerfMetricIfCurrentGeneration(stale, 0)
	require.NoError(t, err)
	require.True(t, persisted)

	generation, err := ClearPerfMetricGroupInRange(stale.Group, 900, 1100)
	require.NoError(t, err)
	require.Equal(t, int64(1), generation)

	persisted, err = UpsertPerfMetricIfCurrentGeneration(stale, 0)
	require.NoError(t, err)
	assert.False(t, persisted)

	var rows []PerfMetric
	require.NoError(t, DB.Where("model_name = ?", stale.ModelName).Find(&rows).Error)
	require.Empty(t, rows)

	fresh := *stale
	fresh.RequestCount = 2
	fresh.SuccessCount = 2
	persisted, err = UpsertPerfMetricIfCurrentGeneration(&fresh, generation)
	require.NoError(t, err)
	assert.True(t, persisted)

	require.NoError(t, DB.Where("model_name = ?", stale.ModelName).Find(&rows).Error)
	require.Len(t, rows, 1)
	assert.Equal(t, int64(2), rows[0].RequestCount)
	assert.Equal(t, int64(2), rows[0].SuccessCount)

	generation, err = ClearPerfMetricGroupInRange(stale.Group, 900, 1100)
	require.NoError(t, err)
	assert.Equal(t, int64(2), generation)
	require.NoError(t, DB.Where("model_name = ?", stale.ModelName).Find(&rows).Error)
	assert.Empty(t, rows)
}
