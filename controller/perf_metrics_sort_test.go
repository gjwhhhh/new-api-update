package controller

import (
	"testing"

	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	"github.com/QuantumNous/new-api/setting/perf_metrics_setting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizePerfGroupsSort(t *testing.T) {
	assert.Equal(t, perfGroupsSortCustom, normalizePerfGroupsSort(""))
	assert.Equal(t, perfGroupsSortCustom, normalizePerfGroupsSort("custom"))
	assert.Equal(t, perfGroupsSortTraffic, normalizePerfGroupsSort("traffic"))
	assert.Equal(t, perfGroupsSortTraffic, normalizePerfGroupsSort("TRAFFIC"))
}

func TestSortPerfMetricGroupsCustomUsesDisplayOrder(t *testing.T) {
	prev := perf_metrics_setting.GetSetting()
	t.Cleanup(func() {
		perf_metrics_setting.RestoreSettingForTest(prev)
	})
	perf_metrics_setting.RestoreSettingForTest(perf_metrics_setting.PerfMetricsSetting{
		Enabled:           true,
		GroupDisplayOrder: []string{"vip", "default"},
	})

	groups := []perfmetrics.GroupMetric{
		{Group: "default", RequestCount: 100},
		{Group: "auto", RequestCount: 50},
		{Group: "vip", RequestCount: 1},
		{Group: "alpha", RequestCount: 10},
	}
	sorted := sortPerfMetricGroups(groups, perfGroupsSortCustom)
	require.Len(t, sorted, 4)
	assert.Equal(t, []string{"vip", "default", "alpha", "auto"}, []string{
		sorted[0].Group, sorted[1].Group, sorted[2].Group, sorted[3].Group,
	})
}

func TestSortPerfMetricGroupsTrafficByRequestCount(t *testing.T) {
	groups := []perfmetrics.GroupMetric{
		{Group: "b", RequestCount: 10},
		{Group: "a", RequestCount: 10},
		{Group: "c", RequestCount: 30},
	}
	sorted := sortPerfMetricGroups(groups, perfGroupsSortTraffic)
	require.Len(t, sorted, 3)
	assert.Equal(t, "c", sorted[0].Group)
	assert.Equal(t, "a", sorted[1].Group)
	assert.Equal(t, "b", sorted[2].Group)
}

func TestNormalizeGroupDisplayOrder(t *testing.T) {
	assert.Equal(t, []string{"vip", "default"}, perf_metrics_setting.NormalizeGroupDisplayOrder([]string{
		" vip ", "default", "", "vip", "default",
	}))
}
