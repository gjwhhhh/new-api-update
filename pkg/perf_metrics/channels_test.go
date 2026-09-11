package perfmetrics

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildChannelMetricsAggregatesByChannelAndKeepsNoDataChannels(t *testing.T) {
	metrics := buildChannelMetrics(
		[]channelBucketRow{
			{
				ChannelID: 11,
				Model:     "gpt-a",
				BucketTs:  3600,
				Value: counters{
					requestCount:   1,
					successCount:   1,
					totalLatencyMs: 100,
				},
			},
			{
				ChannelID: 11,
				Model:     "gpt-b",
				BucketTs:  3600,
				Value: counters{
					requestCount:   1,
					totalLatencyMs: 300,
				},
			},
		},
		[]int{11, 12},
		3600,
		7200,
		3600,
	)

	require.Len(t, metrics, 2)
	assert.Equal(t, 11, metrics[0].ChannelID)
	assert.Equal(t, int64(2), metrics[0].RequestCount)
	assert.Equal(t, int64(1), metrics[0].SuccessCount)
	assert.Equal(t, 50.0, metrics[0].SuccessRate)
	assert.Equal(t, int64(200), metrics[0].AvgLatencyMs)
	require.Len(t, metrics[0].Series, 2)
	require.NotNil(t, metrics[0].Series[0].SuccessRate)
	assert.Equal(t, 50.0, *metrics[0].Series[0].SuccessRate)
	require.Len(t, metrics[0].Models, 2)

	assert.Equal(t, 12, metrics[1].ChannelID)
	assert.Equal(t, int64(0), metrics[1].RequestCount)
	require.Len(t, metrics[1].Series, 2)
	assert.Nil(t, metrics[1].Series[0].SuccessRate)
	assert.Equal(t, ChannelHealthNoData, ChannelHealth(metrics[1].RequestCount, metrics[1].SuccessRate))
}

func TestChannelHealthMatchesStatusThresholds(t *testing.T) {
	tests := []struct {
		name         string
		requestCount int64
		successRate  float64
		expected     string
	}{
		{name: "no data", requestCount: 0, successRate: 100, expected: ChannelHealthNoData},
		{name: "running", requestCount: 1, successRate: 90, expected: ChannelHealthRunning},
		{name: "fluctuating", requestCount: 1, successRate: 70, expected: ChannelHealthFluctuating},
		{name: "abnormal", requestCount: 1, successRate: 69.99, expected: ChannelHealthAbnormal},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.expected, ChannelHealth(testCase.requestCount, testCase.successRate))
		})
	}
}
