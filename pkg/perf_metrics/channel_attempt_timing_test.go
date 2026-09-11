package perfmetrics

import (
	"testing"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/perf_metrics_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelRelaySampleUsesItsOwnAttemptTiming(t *testing.T) {
	previous := perf_metrics_setting.GetSetting()
	t.Cleanup(func() {
		perf_metrics_setting.RestoreSettingForTest(previous)
	})
	perf_metrics_setting.RestoreSettingForTest(perf_metrics_setting.PerfMetricsSetting{
		Enabled:                 true,
		BucketTime:              "hour",
		ChannelSampleGeneration: map[string]int64{},
	})

	const channelID = 920002
	const modelName = "channel-attempt-timing"
	channelHotBuckets.Range(func(key, _ any) bool {
		bucketKey := key.(channelBucketKey)
		if bucketKey.channelID == channelID && bucketKey.model == modelName {
			channelHotBuckets.Delete(key)
		}
		return true
	})
	t.Cleanup(func() {
		channelHotBuckets.Range(func(key, _ any) bool {
			bucketKey := key.(channelBucketKey)
			if bucketKey.channelID == channelID && bucketKey.model == modelName {
				channelHotBuckets.Delete(key)
			}
			return true
		})
	})

	requestStartedAt := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	attemptStartedAt := requestStartedAt.Add(2 * time.Second)
	attemptFirstResponseAt := attemptStartedAt.Add(50 * time.Millisecond)
	attemptFinishedAt := attemptStartedAt.Add(200 * time.Millisecond)
	info := &relaycommon.RelayInfo{
		OriginModelName:               modelName,
		StartTime:                     requestStartedAt,
		FirstResponseTime:             requestStartedAt.Add(500 * time.Millisecond),
		IsStream:                      true,
		ChannelAttemptStartedAt:       attemptStartedAt,
		ChannelAttemptFirstResponseAt: attemptFirstResponseAt,
		ChannelMeta:                   &relaycommon.ChannelMeta{ChannelId: channelID},
	}

	RecordChannelRelaySampleAt(info, true, 300, attemptFinishedAt)

	var snapshot counters
	var found bool
	channelHotBuckets.Range(func(key, value any) bool {
		bucketKey := key.(channelBucketKey)
		if bucketKey.channelID == channelID && bucketKey.model == modelName {
			snapshot = value.(*atomicBucket).snapshot()
			found = true
			return false
		}
		return true
	})
	require.True(t, found)
	assert.Equal(t, int64(1), snapshot.requestCount)
	assert.Equal(t, int64(1), snapshot.successCount)
	assert.Equal(t, int64(200), snapshot.totalLatencyMs)
	assert.Equal(t, int64(50), snapshot.ttftSumMs)
	assert.Equal(t, int64(1), snapshot.ttftCount)
	assert.Equal(t, int64(150), snapshot.generationMs)
}
