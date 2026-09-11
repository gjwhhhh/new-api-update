package perf_metrics_setting

import (
	"strconv"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/setting/config"
)

type PerfMetricsSetting struct {
	Enabled                 bool             `json:"enabled"`
	IncludeChannelTest      bool             `json:"include_channel_test"`
	FlushInterval           int              `json:"flush_interval"`
	BucketTime              string           `json:"bucket_time"`
	RetentionDays           int              `json:"retention_days"`
	HiddenGroups            []string         `json:"hidden_groups"`
	GroupSampleGeneration   map[string]int64 `json:"group_sample_generation"`
	ChannelSampleGeneration map[string]int64 `json:"channel_sample_generation"`
	GroupDisplayOrder       []string         `json:"group_display_order"`
}

var perfMetricsSetting = PerfMetricsSetting{
	Enabled:                 true,
	IncludeChannelTest:      true,
	FlushInterval:           5,
	BucketTime:              "hour",
	RetentionDays:           0,
	HiddenGroups:            []string{},
	GroupSampleGeneration:   map[string]int64{},
	ChannelSampleGeneration: map[string]int64{},
	GroupDisplayOrder:       []string{},
}

var perfMetricsSettingMu sync.RWMutex

// perfMetricsSettingConfig deliberately has no methods. It lets the generic
// config codec parse a detached copy without bypassing perfMetricsSettingMu.
type perfMetricsSettingConfig PerfMetricsSetting

func init() {
	config.GlobalConfig.Register("perf_metrics_setting", &perfMetricsSetting)
}

func GetSetting() PerfMetricsSetting {
	perfMetricsSettingMu.RLock()
	defer perfMetricsSettingMu.RUnlock()
	return clonePerfMetricsSetting(perfMetricsSetting)
}

func RestoreSettingForTest(setting PerfMetricsSetting) {
	perfMetricsSettingMu.Lock()
	defer perfMetricsSettingMu.Unlock()
	perfMetricsSetting = clonePerfMetricsSetting(setting)
}

func clonePerfMetricsSetting(setting PerfMetricsSetting) PerfMetricsSetting {
	setting = normalizePerfMetricsSetting(setting)
	setting.HiddenGroups = append([]string(nil), setting.HiddenGroups...)
	setting.GroupSampleGeneration = cloneSampleGenerations(setting.GroupSampleGeneration)
	setting.ChannelSampleGeneration = cloneSampleGenerations(setting.ChannelSampleGeneration)
	setting.GroupDisplayOrder = append([]string(nil), setting.GroupDisplayOrder...)
	return setting
}

func cloneSampleGenerations(values map[string]int64) map[string]int64 {
	cloned := make(map[string]int64, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func normalizePerfMetricsSetting(setting PerfMetricsSetting) PerfMetricsSetting {
	if setting.HiddenGroups == nil {
		setting.HiddenGroups = []string{}
	}
	if setting.GroupSampleGeneration == nil {
		setting.GroupSampleGeneration = map[string]int64{}
	}
	if setting.ChannelSampleGeneration == nil {
		setting.ChannelSampleGeneration = map[string]int64{}
	}
	if setting.GroupDisplayOrder == nil {
		setting.GroupDisplayOrder = []string{}
	}
	return setting
}

// ConfigToMap implements config.ConfigMapExporter. The global config manager
// uses it instead of reflecting over live maps while request goroutines read
// their generation values.
func (setting *PerfMetricsSetting) ConfigToMap() (map[string]string, error) {
	perfMetricsSettingMu.RLock()
	snapshot := clonePerfMetricsSetting(*setting)
	perfMetricsSettingMu.RUnlock()
	configSnapshot := perfMetricsSettingConfig(snapshot)
	return config.ConfigToMap(&configSnapshot)
}

// UpdateConfigFromMap implements config.ConfigMapUpdater. Configuration reload
// writes a complete detached value under the same lock as runtime accessors.
func (setting *PerfMetricsSetting) UpdateConfigFromMap(values map[string]string) error {
	perfMetricsSettingMu.Lock()
	defer perfMetricsSettingMu.Unlock()

	next := perfMetricsSettingConfig(clonePerfMetricsSetting(*setting))
	if err := config.UpdateConfigFromMap(&next, values); err != nil {
		return err
	}
	*setting = normalizePerfMetricsSetting(PerfMetricsSetting(next))
	return nil
}

func IncludeChannelTestEnabled() bool {
	perfMetricsSettingMu.RLock()
	defer perfMetricsSettingMu.RUnlock()
	return perfMetricsSetting.Enabled && perfMetricsSetting.IncludeChannelTest
}

func IsEnabled() bool {
	perfMetricsSettingMu.RLock()
	defer perfMetricsSettingMu.RUnlock()
	return perfMetricsSetting.Enabled
}

func GetBucketSeconds() int64 {
	perfMetricsSettingMu.RLock()
	defer perfMetricsSettingMu.RUnlock()
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
	perfMetricsSettingMu.RLock()
	defer perfMetricsSettingMu.RUnlock()
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
	perfMetricsSettingMu.RLock()
	defer perfMetricsSettingMu.RUnlock()
	for _, hidden := range perfMetricsSetting.HiddenGroups {
		if strings.TrimSpace(hidden) == group {
			return true
		}
	}
	return false
}

func GetGroupSampleGeneration(group string) int64 {
	group = strings.TrimSpace(group)
	if group == "" {
		return 0
	}
	perfMetricsSettingMu.RLock()
	defer perfMetricsSettingMu.RUnlock()
	if perfMetricsSetting.GroupSampleGeneration == nil {
		return 0
	}
	return perfMetricsSetting.GroupSampleGeneration[group]
}

// SetHiddenGroupsInMemory updates the runtime hidden-groups list (tests / apply after persist).
func SetHiddenGroupsInMemory(groups []string) {
	if groups == nil {
		groups = []string{}
	}
	perfMetricsSettingMu.Lock()
	defer perfMetricsSettingMu.Unlock()
	perfMetricsSetting.HiddenGroups = append([]string(nil), groups...)
}

// BumpGroupSampleGenerationInMemory increments the generation for group and returns the new value.
func BumpGroupSampleGenerationInMemory(group string) int64 {
	group = strings.TrimSpace(group)
	if group == "" {
		return 0
	}
	perfMetricsSettingMu.Lock()
	defer perfMetricsSettingMu.Unlock()
	if perfMetricsSetting.GroupSampleGeneration == nil {
		perfMetricsSetting.GroupSampleGeneration = map[string]int64{}
	}
	perfMetricsSetting.GroupSampleGeneration[group]++
	return perfMetricsSetting.GroupSampleGeneration[group]
}

// SetGroupSampleGenerationInMemory advances the local generation cache after
// a successful clear. Generations never move backward during concurrent
// clears; the database state remains authoritative during flush.
func SetGroupSampleGenerationInMemory(group string, generation int64) {
	group = strings.TrimSpace(group)
	if group == "" || generation < 0 {
		return
	}
	perfMetricsSettingMu.Lock()
	defer perfMetricsSettingMu.Unlock()
	if perfMetricsSetting.GroupSampleGeneration == nil {
		perfMetricsSetting.GroupSampleGeneration = map[string]int64{}
	}
	if generation > perfMetricsSetting.GroupSampleGeneration[group] {
		perfMetricsSetting.GroupSampleGeneration[group] = generation
	}
}

func GetChannelSampleGeneration(channelID int) int64 {
	if channelID <= 0 {
		return 0
	}
	perfMetricsSettingMu.RLock()
	defer perfMetricsSettingMu.RUnlock()
	if perfMetricsSetting.ChannelSampleGeneration == nil {
		return 0
	}
	return perfMetricsSetting.ChannelSampleGeneration[strconv.Itoa(channelID)]
}

// SetChannelSampleGenerationInMemory advances the local generation cache after
// a successful channel-sample clear. Generations never move backward during
// concurrent clears; the database state remains authoritative during query and
// flush.
func SetChannelSampleGenerationInMemory(channelID int, generation int64) {
	if channelID <= 0 || generation < 0 {
		return
	}
	perfMetricsSettingMu.Lock()
	defer perfMetricsSettingMu.Unlock()
	if perfMetricsSetting.ChannelSampleGeneration == nil {
		perfMetricsSetting.ChannelSampleGeneration = map[string]int64{}
	}
	key := strconv.Itoa(channelID)
	if generation > perfMetricsSetting.ChannelSampleGeneration[key] {
		perfMetricsSetting.ChannelSampleGeneration[key] = generation
	}
}

func GetGroupDisplayOrder() []string {
	return NormalizeGroupDisplayOrder(GetSetting().GroupDisplayOrder)
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
