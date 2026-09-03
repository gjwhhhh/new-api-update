package perfmetrics

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeGroupHours(t *testing.T) {
	assert.Equal(t, 24, NormalizeGroupHours(0))
	assert.Equal(t, 24, NormalizeGroupHours(-1))
	assert.Equal(t, 24, NormalizeGroupHours(12))
	assert.Equal(t, 24, NormalizeGroupHours(24))
	assert.Equal(t, 168, NormalizeGroupHours(168))
	assert.Equal(t, 24, NormalizeGroupHours(720))
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

func TestDownsampleGroupSeriesCapsLength(t *testing.T) {
	points := make([]GroupBucketPoint, 0, 168)
	for i := 0; i < 168; i++ {
		rate := 90.0
		if i%10 == 0 {
			rate = 50.0
		}
		copied := rate
		points = append(points, GroupBucketPoint{
			Ts:           int64(i * 3600),
			RequestCount: 10,
			SuccessRate:  &copied,
		})
	}
	downsampled := downsampleGroupSeries(points, maxGroupSeriesLen)
	assert.Len(t, downsampled, maxGroupSeriesLen)
	assert.Equal(t, int64(0), downsampled[0].Ts)
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
