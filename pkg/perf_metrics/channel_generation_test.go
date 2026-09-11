package perfmetrics

import (
	"testing"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/perf_metrics_setting"
	"github.com/stretchr/testify/assert"
)

func TestRecordChannelSampleReplacesClearedGeneration(t *testing.T) {
	previous := perf_metrics_setting.GetSetting()
	t.Cleanup(func() {
		perf_metrics_setting.RestoreSettingForTest(previous)
	})
	perf_metrics_setting.RestoreSettingForTest(perf_metrics_setting.PerfMetricsSetting{
		Enabled:                 true,
		BucketTime:              "hour",
		ChannelSampleGeneration: map[string]int64{},
	})

	const channelID = 920001
	const modelName = "channel-generation-replacement"
	key := channelBucketKey{
		channelID: channelID,
		model:     modelName,
		bucketTs:  bucketStart(time.Now().Unix()),
	}
	channelHotBuckets.Delete(key)
	t.Cleanup(func() {
		channelHotBuckets.Delete(key)
	})

	info := &relaycommon.RelayInfo{
		OriginModelName: modelName,
		StartTime:       time.Now().Add(-time.Second),
	}
	RecordChannelSampleAt(info, channelID, true, 0, time.Now())
	requestCount, successCount := HotChannelCountersForTest(channelID, modelName)
	assert.Equal(t, int64(1), requestCount)
	assert.Equal(t, int64(1), successCount)

	perf_metrics_setting.SetChannelSampleGenerationInMemory(channelID, 1)
	requestCount, successCount = HotChannelCountersForTest(channelID, modelName)
	assert.Equal(t, int64(0), requestCount)
	assert.Equal(t, int64(0), successCount)

	RecordChannelSampleAt(info, channelID, true, 0, time.Now())
	requestCount, successCount = HotChannelCountersForTest(channelID, modelName)
	assert.Equal(t, int64(1), requestCount)
	assert.Equal(t, int64(1), successCount)
}
