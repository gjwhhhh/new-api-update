package perfmetrics

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateGroupHours(t *testing.T) {
	for _, hours := range []int{GroupHours48, GroupHours7Days} {
		assert.NoError(t, ValidateGroupHours(hours))
	}

	for _, hours := range []int{-1, 0, 24, 720} {
		assert.ErrorIs(t, ValidateGroupHours(hours), ErrUnsupportedGroupHours)
	}
}

func TestGroupQueriesRejectUnsupportedHours(t *testing.T) {
	_, err := QueryGroups(24, nil)
	require.ErrorIs(t, err, ErrUnsupportedGroupHours)

	_, err = ClearGroupRecent("default", 24)
	require.ErrorIs(t, err, ErrUnsupportedGroupHours)
}

func TestQueryGroupsAllowsSupportedHours(t *testing.T) {
	for _, hours := range []int{GroupHours48, GroupHours7Days} {
		result, err := QueryGroups(hours, []string{})
		require.NoError(t, err)
		assert.Empty(t, result.Groups)
	}
}

func TestGroupTimeWindowUsesConfiguredBucketBoundaries(t *testing.T) {
	startTs, endTs := groupTimeWindow(48, 50*3600+123, 3600)
	assert.Equal(t, int64(3*3600), startTs)
	assert.Equal(t, int64(50*3600), endTs)
}

func TestBuildGroupMetricsMergesModelsInTheSameGroup(t *testing.T) {
	rows := []groupBucketRow{
		{
			Group:    "plus",
			Model:    "gpt-4.1",
			BucketTs: 100,
			Value: counters{
				requestCount:   80,
				successCount:   76,
				totalLatencyMs: 80000,
				ttftSumMs:      16000,
				ttftCount:      80,
				outputTokens:   800,
				generationMs:   40000,
			},
		},
		{
			Group:    "plus",
			Model:    "gpt-4.1-mini",
			BucketTs: 100,
			Value: counters{
				requestCount:   20,
				successCount:   14,
				totalLatencyMs: 10000,
				outputTokens:   200,
				generationMs:   10000,
			},
		},
	}

	metrics := buildGroupMetrics(rows, []string{"plus", "empty"}, 100, 100, 100)
	require.Len(t, metrics, 2)
	assert.Equal(t, "plus", metrics[0].Group)
	assert.Equal(t, int64(100), metrics[0].RequestCount)
	assert.Equal(t, int64(90), metrics[0].SuccessCount)
	assert.Equal(t, 90.0, metrics[0].SuccessRate)
	assert.Equal(t, int64(900), metrics[0].AvgLatencyMs)
	assert.Equal(t, int64(200), metrics[0].AvgTtftMs)
	require.Len(t, metrics[0].Models, 2)
	assert.Equal(t, "gpt-4.1", metrics[0].Models[0].ModelName)
	assert.Equal(t, 95.0, metrics[0].Models[0].SuccessRate)
	assert.Equal(t, int64(200), metrics[0].Models[0].AvgTtftMs)
	assert.Equal(t, "gpt-4.1-mini", metrics[0].Models[1].ModelName)
	assert.Equal(t, int64(0), metrics[0].Models[1].AvgTtftMs)

	assert.Equal(t, "empty", metrics[1].Group)
	assert.Equal(t, int64(0), metrics[1].RequestCount)
	assert.Equal(t, 0.0, metrics[1].SuccessRate)
	require.Len(t, metrics[1].Series, 1)
	assert.Nil(t, metrics[1].Series[0].SuccessRate)
}

func TestBuildGroupMetricsFillsEmptyBuckets(t *testing.T) {
	rows := []groupBucketRow{
		{
			Group:    "vip",
			Model:    "gpt-4.1",
			BucketTs: 200,
			Value:    counters{requestCount: 10, successCount: 10},
		},
	}
	metrics := buildGroupMetrics(rows, []string{"vip"}, 100, 300, 100)
	require.Len(t, metrics, 1)
	require.Len(t, metrics[0].Series, 3)
	assert.Equal(t, int64(100), metrics[0].Series[0].Ts)
	assert.Nil(t, metrics[0].Series[0].SuccessRate)
	assert.Equal(t, int64(200), metrics[0].Series[1].Ts)
	require.NotNil(t, metrics[0].Series[1].SuccessRate)
	assert.Equal(t, 100.0, *metrics[0].Series[1].SuccessRate)
	assert.Equal(t, int64(300), metrics[0].Series[2].Ts)
	assert.Nil(t, metrics[0].Series[2].SuccessRate)
}

func TestBuildGroupSeriesReturnsContinuousRealTimeRanges(t *testing.T) {
	slots := make(map[int64]counters, 168)
	for index := 0; index < 168; index++ {
		slots[int64(index*3600)] = counters{
			requestCount: 1,
			successCount: 1,
		}
	}

	series := buildGroupSeries(slots, 0, 167*3600, 3600)
	require.Len(t, series, 48)

	var requestCount int64
	var spanSeconds int64
	for index, point := range series {
		requestCount += point.RequestCount
		spanSeconds += point.SpanSeconds
		if index > 0 {
			assert.Equal(t, point.Ts, series[index-1].Ts+series[index-1].SpanSeconds)
		}
	}
	assert.Equal(t, int64(168), requestCount)
	assert.Equal(t, int64(168*3600), spanSeconds)
}

func TestBuildGroupMetricsSkipsBlankRequestedNames(t *testing.T) {
	metrics := buildGroupMetrics(nil, []string{"", "default", "default"}, 0, 0, 3600)
	require.Len(t, metrics, 1)
	assert.Equal(t, "default", metrics[0].Group)
}

func TestNormalizeSampleGroups(t *testing.T) {
	assert.Equal(t, []string{"default"}, normalizeSampleGroups(nil))
	assert.Equal(t, []string{"default"}, normalizeSampleGroups([]string{}))
	assert.Equal(t, []string{"default"}, normalizeSampleGroups([]string{"", "  "}))
	assert.Equal(t, []string{"vip", "default"}, normalizeSampleGroups([]string{"vip", " default ", "vip", ""}))
}
