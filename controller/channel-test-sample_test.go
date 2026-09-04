package controller

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/perf_metrics_setting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecordChannelTestSampleRespectsSetting(t *testing.T) {
	prev := perf_metrics_setting.GetSetting()
	t.Cleanup(func() {
		perf_metrics_setting.RestoreSettingForTest(prev)
	})

	info := &relaycommon.RelayInfo{
		OriginModelName: "gpt-test-sample",
		UsingGroup:      "ignored-user-group",
		StartTime:       time.Now().Add(-2 * time.Second),
	}
	channel := &model.Channel{Group: "default"}

	perf_metrics_setting.RestoreSettingForTest(perf_metrics_setting.PerfMetricsSetting{
		Enabled:            true,
		IncludeChannelTest: false,
		FlushInterval:      5,
		BucketTime:         "hour",
	})
	beforeReq, beforeOK := perfmetrics.HotCountersForTest("gpt-test-sample", "default")
	recordChannelTestSampleSync(channel, info, true, 11)
	afterReq, afterOK := perfmetrics.HotCountersForTest("gpt-test-sample", "default")
	assert.Equal(t, beforeReq, afterReq)
	assert.Equal(t, beforeOK, afterOK)

	perf_metrics_setting.RestoreSettingForTest(perf_metrics_setting.PerfMetricsSetting{
		Enabled:            true,
		IncludeChannelTest: true,
		FlushInterval:      5,
		BucketTime:         "hour",
	})
	beforeReq, beforeOK = perfmetrics.HotCountersForTest("gpt-test-sample", "default")
	recordChannelTestSampleSync(channel, info, false, 0)
	afterFailReq, afterFailOK := perfmetrics.HotCountersForTest("gpt-test-sample", "default")
	require.Greater(t, afterFailReq, beforeReq)
	assert.Equal(t, beforeOK, afterFailOK)

	recordChannelTestSampleSync(channel, info, true, 13)
	afterOKReq, afterOKCount := perfmetrics.HotCountersForTest("gpt-test-sample", "default")
	require.Greater(t, afterOKReq, afterFailReq)
	require.Greater(t, afterOKCount, afterFailOK)
}

func TestRecordChannelTestSampleFansOutToChannelGroups(t *testing.T) {
	prev := perf_metrics_setting.GetSetting()
	t.Cleanup(func() {
		perf_metrics_setting.RestoreSettingForTest(prev)
	})
	perf_metrics_setting.RestoreSettingForTest(perf_metrics_setting.PerfMetricsSetting{
		Enabled:            true,
		IncludeChannelTest: true,
		FlushInterval:      5,
		BucketTime:         "hour",
	})

	info := &relaycommon.RelayInfo{
		OriginModelName: "gpt-test-fanout",
		UsingGroup:      "should-not-use",
		StartTime:       time.Now().Add(-time.Second),
	}
	channel := &model.Channel{Group: "vip, default, vip"}

	beforeVIPReq, beforeVIPOK := perfmetrics.HotCountersForTest("gpt-test-fanout", "vip")
	beforeDefaultReq, beforeDefaultOK := perfmetrics.HotCountersForTest("gpt-test-fanout", "default")
	beforeIgnoredReq, _ := perfmetrics.HotCountersForTest("gpt-test-fanout", "should-not-use")

	recordChannelTestSampleSync(channel, info, true, 7)

	afterVIPReq, afterVIPOK := perfmetrics.HotCountersForTest("gpt-test-fanout", "vip")
	afterDefaultReq, afterDefaultOK := perfmetrics.HotCountersForTest("gpt-test-fanout", "default")
	afterIgnoredReq, _ := perfmetrics.HotCountersForTest("gpt-test-fanout", "should-not-use")

	assert.Equal(t, beforeVIPReq+1, afterVIPReq)
	assert.Equal(t, beforeVIPOK+1, afterVIPOK)
	assert.Equal(t, beforeDefaultReq+1, afterDefaultReq)
	assert.Equal(t, beforeDefaultOK+1, afterDefaultOK)
	assert.Equal(t, beforeIgnoredReq, afterIgnoredReq)
}

func TestRecordChannelTestSampleEmptyChannelGroupsDefaults(t *testing.T) {
	prev := perf_metrics_setting.GetSetting()
	t.Cleanup(func() {
		perf_metrics_setting.RestoreSettingForTest(prev)
	})
	perf_metrics_setting.RestoreSettingForTest(perf_metrics_setting.PerfMetricsSetting{
		Enabled:            true,
		IncludeChannelTest: true,
		FlushInterval:      5,
		BucketTime:         "hour",
	})

	info := &relaycommon.RelayInfo{
		OriginModelName: "gpt-test-empty-groups",
		UsingGroup:      "vip",
		StartTime:       time.Now().Add(-time.Second),
	}
	channel := &model.Channel{Group: ""}

	beforeDefaultReq, beforeDefaultOK := perfmetrics.HotCountersForTest("gpt-test-empty-groups", "default")
	beforeVIPReq, _ := perfmetrics.HotCountersForTest("gpt-test-empty-groups", "vip")

	recordChannelTestSampleSync(channel, info, true, 3)

	afterDefaultReq, afterDefaultOK := perfmetrics.HotCountersForTest("gpt-test-empty-groups", "default")
	afterVIPReq, _ := perfmetrics.HotCountersForTest("gpt-test-empty-groups", "vip")

	assert.Equal(t, beforeDefaultReq+1, afterDefaultReq)
	assert.Equal(t, beforeDefaultOK+1, afterDefaultOK)
	assert.Equal(t, beforeVIPReq, afterVIPReq)
}

func TestRecordChannelTestSampleRespectsExcludedModels(t *testing.T) {
	prev := perf_metrics_setting.GetSetting()
	t.Cleanup(func() {
		perf_metrics_setting.RestoreSettingForTest(prev)
	})
	perf_metrics_setting.RestoreSettingForTest(perf_metrics_setting.PerfMetricsSetting{
		Enabled:            true,
		IncludeChannelTest: true,
		FlushInterval:      5,
		BucketTime:         "hour",
	})

	info := &relaycommon.RelayInfo{
		OriginModelName: "gpt-test-excluded",
		UsingGroup:      "default",
		StartTime:       time.Now().Add(-time.Second),
	}
	channel := &model.Channel{Group: "default"}
	channel.SetOtherSettings(dto.ChannelOtherSettings{
		ExcludeFromSamplingModels: []string{"gpt-test-excluded"},
	})

	beforeReq, beforeOK := perfmetrics.HotCountersForTest("gpt-test-excluded", "default")
	recordChannelTestSampleSync(channel, info, true, 9)
	afterReq, afterOK := perfmetrics.HotCountersForTest("gpt-test-excluded", "default")
	assert.Equal(t, beforeReq, afterReq)
	assert.Equal(t, beforeOK, afterOK)
}

func recordChannelTestSampleSync(channel *model.Channel, info *relaycommon.RelayInfo, success bool, outputTokens int64) {
	if !perf_metrics_setting.IncludeChannelTestEnabled() || info == nil {
		return
	}
	var groups []string
	if channel != nil {
		groups = channel.GetGroups()
		if channel.GetOtherSettings().IsModelExcludedFromSampling(info.OriginModelName) {
			return
		}
	}
	perfmetrics.RecordRelaySampleToGroups(info, groups, success, outputTokens)
}
