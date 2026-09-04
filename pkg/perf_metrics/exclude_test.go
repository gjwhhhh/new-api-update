package perfmetrics

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/perf_metrics_setting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecordRelaySampleSkipsExcludedModels(t *testing.T) {
	prev := perf_metrics_setting.GetSetting()
	t.Cleanup(func() {
		perf_metrics_setting.RestoreSettingForTest(prev)
	})
	perf_metrics_setting.RestoreSettingForTest(perf_metrics_setting.PerfMetricsSetting{
		Enabled:               true,
		IncludeChannelTest:    true,
		FlushInterval:         5,
		BucketTime:            "hour",
		HiddenGroups:          []string{},
		GroupSampleGeneration: map[string]int64{},
	})

	excluded := &relaycommon.RelayInfo{
		OriginModelName: "gpt-excluded",
		UsingGroup:      "default",
		StartTime:       time.Now().Add(-time.Second),
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelOtherSettings: dto.ChannelOtherSettings{
				ExcludeFromSamplingModels: []string{"gpt-excluded"},
			},
		},
	}
	included := &relaycommon.RelayInfo{
		OriginModelName: "gpt-included",
		UsingGroup:      "default",
		StartTime:       time.Now().Add(-time.Second),
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelOtherSettings: dto.ChannelOtherSettings{
				ExcludeFromSamplingModels: []string{"gpt-excluded"},
			},
		},
	}

	beforeExcluded, _ := HotCountersForTest("gpt-excluded", "default")
	beforeIncluded, beforeIncludedOK := HotCountersForTest("gpt-included", "default")

	RecordRelaySample(excluded, true, 3)
	RecordRelaySample(included, true, 5)

	afterExcluded, _ := HotCountersForTest("gpt-excluded", "default")
	afterIncluded, afterIncludedOK := HotCountersForTest("gpt-included", "default")

	assert.Equal(t, beforeExcluded, afterExcluded)
	assert.Equal(t, beforeIncluded+1, afterIncluded)
	assert.Equal(t, beforeIncludedOK+1, afterIncludedOK)
}

func TestRecordRelaySampleNilChannelMetaStillRecords(t *testing.T) {
	prev := perf_metrics_setting.GetSetting()
	t.Cleanup(func() {
		perf_metrics_setting.RestoreSettingForTest(prev)
	})
	perf_metrics_setting.RestoreSettingForTest(perf_metrics_setting.PerfMetricsSetting{
		Enabled:               true,
		IncludeChannelTest:    true,
		FlushInterval:         5,
		BucketTime:            "hour",
		HiddenGroups:          []string{},
		GroupSampleGeneration: map[string]int64{},
	})

	info := &relaycommon.RelayInfo{
		OriginModelName: "gpt-nil-meta",
		UsingGroup:      "default",
		StartTime:       time.Now().Add(-time.Second),
	}
	before, beforeOK := HotCountersForTest("gpt-nil-meta", "default")
	require.NotPanics(t, func() {
		RecordRelaySample(info, true, 2)
	})
	after, afterOK := HotCountersForTest("gpt-nil-meta", "default")
	assert.Equal(t, before+1, after)
	assert.Equal(t, beforeOK+1, afterOK)
}

func TestStaleHotBucketGenerationIsIgnored(t *testing.T) {
	prev := perf_metrics_setting.GetSetting()
	t.Cleanup(func() {
		perf_metrics_setting.RestoreSettingForTest(prev)
	})
	perf_metrics_setting.RestoreSettingForTest(perf_metrics_setting.PerfMetricsSetting{
		Enabled:               true,
		IncludeChannelTest:    true,
		FlushInterval:         5,
		BucketTime:            "hour",
		HiddenGroups:          []string{},
		GroupSampleGeneration: map[string]int64{"clear-gen": 0},
	})

	info := &relaycommon.RelayInfo{
		OriginModelName: "gpt-clear-gen",
		UsingGroup:      "clear-gen",
		StartTime:       time.Now().Add(-time.Second),
	}
	RecordRelaySample(info, true, 2)
	before, _ := HotCountersForTest("gpt-clear-gen", "clear-gen")
	require.Greater(t, before, int64(0))

	perf_metrics_setting.BumpGroupSampleGenerationInMemory("clear-gen")
	afterBump, _ := HotCountersForTest("gpt-clear-gen", "clear-gen")
	assert.Equal(t, int64(0), afterBump)

	RecordRelaySample(info, true, 4)
	afterNew, afterNewOK := HotCountersForTest("gpt-clear-gen", "clear-gen")
	assert.Equal(t, int64(1), afterNew)
	assert.Equal(t, int64(1), afterNewOK)
}
