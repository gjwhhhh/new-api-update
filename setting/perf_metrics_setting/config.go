package perf_metrics_setting

import (
	"strings"

	"github.com/QuantumNous/new-api/setting/config"
)

type PerfMetricsSetting struct {
	Enabled               bool             `json:"enabled"`
	IncludeChannelTest    bool             `json:"include_channel_test"`
	FlushInterval         int              `json:"flush_interval"`
	BucketTime            string           `json:"bucket_time"`
	RetentionDays         int              `json:"retention_days"`
	HiddenGroups          []string         `json:"hidden_groups"`
	GroupSampleGeneration map[string]int64 `json:"group_sample_generation"`
	GroupDisplayOrder     []string         `json:"group_display_order"`
}

var perfMetricsSetting = PerfMetricsSetting{
	Enabled:               true,
	IncludeChannelTest:    true,
	FlushInterval:         5,
	BucketTime:            "hour",
	RetentionDays:         0,
	HiddenGroups:          []string{},
	GroupSampleGeneration: map[string]int64{},
	GroupDisplayOrder:     []string{},
}

func init() {
	config.GlobalConfig.Register("perf_metrics_setting", &perfMetricsSetting)
}

func GetSetting() PerfMetricsSetting {
	return perfMetricsSetting
}

func RestoreSettingForTest(setting PerfMetricsSetting) {
	if setting.HiddenGroups == nil {
		setting.HiddenGroups = []string{}
	}
	if setting.GroupSampleGeneration == nil {
		setting.GroupSampleGeneration = map[string]int64{}
	}
	if setting.GroupDisplayOrder == nil {
		setting.GroupDisplayOrder = []string{}
	}
	perfMetricsSetting = setting
}

func IncludeChannelTestEnabled() bool {
	return perfMetricsSetting.Enabled && perfMetricsSetting.IncludeChannelTest
}

func GetBucketSeconds() int64 {
	switch perfMetricsSetting.BucketTime {
	case "minute":
		return 60
	case "5min":
		return 300
	case "hour":
		return 3600
	default:
		return 3600
	}
}

func GetFlushIntervalMinutes() int {
	if perfMetricsSetting.FlushInterval < 1 {
		return 1
	}
	return perfMetricsSetting.FlushInterval
}

func IsGroupHidden(group string) bool {
	group = strings.TrimSpace(group)
	if group == "" {
		return false
	}
	for _, hidden := range perfMetricsSetting.HiddenGroups {
		if strings.TrimSpace(hidden) == group {
			return true
		}
	}
	return false
}

func GetGroupSampleGeneration(group string) int64 {
	group = strings.TrimSpace(group)
	if group == "" || perfMetricsSetting.GroupSampleGeneration == nil {
		return 0
	}
	return perfMetricsSetting.GroupSampleGeneration[group]
}

// SetHiddenGroupsInMemory updates the runtime hidden-groups list (tests / apply after persist).
func SetHiddenGroupsInMemory(groups []string) {
	if groups == nil {
		groups = []string{}
	}
	perfMetricsSetting.HiddenGroups = groups
}

// BumpGroupSampleGenerationInMemory increments the generation for group and returns the new value.
func BumpGroupSampleGenerationInMemory(group string) int64 {
	group = strings.TrimSpace(group)
	if group == "" {
		return 0
	}
	if perfMetricsSetting.GroupSampleGeneration == nil {
		perfMetricsSetting.GroupSampleGeneration = map[string]int64{}
	}
	perfMetricsSetting.GroupSampleGeneration[group]++
	return perfMetricsSetting.GroupSampleGeneration[group]
}

func GetGroupDisplayOrder() []string {
	return NormalizeGroupDisplayOrder(perfMetricsSetting.GroupDisplayOrder)
}

// NormalizeGroupDisplayOrder trims, drops empties, and de-duplicates while preserving order.
func NormalizeGroupDisplayOrder(names []string) []string {
	if len(names) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}
