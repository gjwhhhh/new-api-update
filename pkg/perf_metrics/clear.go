package perfmetrics

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/perf_metrics_setting"
)

// sampleGenerationPersistenceMu serializes snapshot-and-persist operations so
// two clears cannot overwrite each other's generation entry in the option map.
var sampleGenerationPersistenceMu sync.Mutex

type ClearGroupResult struct {
	Group   string `json:"group"`
	Hours   int    `json:"hours"`
	StartTs int64  `json:"start_ts"`
	EndTs   int64  `json:"end_ts"`
}

type ClearChannelResult struct {
	ChannelID int   `json:"channel_id"`
	Hours     int   `json:"hours"`
	StartTs   int64 `json:"start_ts"`
	EndTs     int64 `json:"end_ts"`
}

// ClearGroupRecent clears in-memory and persisted samples for group within the
// same hours window used by QueryGroups, then bumps the group sample generation
// so other nodes discard stale hot buckets on flush/query.
func ClearGroupRecent(group string, hours int) (ClearGroupResult, error) {
	group = strings.TrimSpace(group)
	if group == "" {
		return ClearGroupResult{}, fmt.Errorf("group is required")
	}
	if err := ValidateGroupHours(hours); err != nil {
		return ClearGroupResult{}, err
	}
	startTs, endTs := groupTimeWindow(
		hours,
		time.Now().Unix(),
		perf_metrics_setting.GetBucketSeconds(),
	)

	generation, err := model.ClearPerfMetricGroupInRange(group, startTs, endTs)
	if err != nil {
		return ClearGroupResult{}, err
	}
	perf_metrics_setting.SetGroupSampleGenerationInMemory(group, generation)
	clearHotBucketsInRange(group, startTs, endTs, generation)
	if err := persistGroupSampleGeneration(group, generation); err != nil {
		return ClearGroupResult{}, err
	}

	return ClearGroupResult{
		Group:   group,
		Hours:   hours,
		StartTs: startTs,
		EndTs:   endTs,
	}, nil
}

// ClearChannelRecent clears in-memory and persisted samples for one channel
// within the same hours window used by QueryChannels. The channel generation
// fences stale buckets held by other instances from reappearing on flush.
func ClearChannelRecent(channelID int, hours int) (ClearChannelResult, error) {
	if channelID <= 0 {
		return ClearChannelResult{}, fmt.Errorf("channel id is required")
	}
	if err := ValidateGroupHours(hours); err != nil {
		return ClearChannelResult{}, err
	}
	startTs, endTs := groupTimeWindow(
		hours,
		time.Now().Unix(),
		perf_metrics_setting.GetBucketSeconds(),
	)

	generation, err := model.ClearPerfChannelMetricInRange(channelID, startTs, endTs)
	if err != nil {
		return ClearChannelResult{}, err
	}
	perf_metrics_setting.SetChannelSampleGenerationInMemory(channelID, generation)
	clearChannelHotBucketsInRange(channelID, startTs, endTs, generation)
	if err := persistChannelSampleGeneration(channelID, generation); err != nil {
		return ClearChannelResult{}, err
	}

	return ClearChannelResult{
		ChannelID: channelID,
		Hours:     hours,
		StartTs:   startTs,
		EndTs:     endTs,
	}, nil
}

func clearHotBucketsInRange(group string, startTs, endTs, generation int64) {
	hotBuckets.Range(func(key, value any) bool {
		k := key.(bucketKey)
		if k.group != group {
			return true
		}
		if k.bucketTs < startTs || k.bucketTs > endTs {
			return true
		}
		bucket := value.(*atomicBucket)
		if bucket.sampleGeneration() >= generation {
			return true
		}
		hotBuckets.CompareAndDelete(key, value)
		return true
	})
}

func clearChannelHotBucketsInRange(channelID int, startTs, endTs, generation int64) {
	channelHotBuckets.Range(func(key, value any) bool {
		k := key.(channelBucketKey)
		if k.channelID != channelID {
			return true
		}
		if k.bucketTs < startTs || k.bucketTs > endTs {
			return true
		}
		bucket := value.(*atomicBucket)
		if bucket.sampleGeneration() >= generation {
			return true
		}
		channelHotBuckets.CompareAndDelete(key, value)
		return true
	})
}

func persistGroupSampleGeneration(group string, generation int64) error {
	sampleGenerationPersistenceMu.Lock()
	defer sampleGenerationPersistenceMu.Unlock()

	setting := perf_metrics_setting.GetSetting()
	gens := map[string]int64{}
	for k, v := range setting.GroupSampleGeneration {
		gens[k] = v
	}
	if generation > gens[group] {
		gens[group] = generation
	}
	encoded, err := common.Marshal(gens)
	if err != nil {
		return fmt.Errorf("marshal group sample generation: %w", err)
	}
	if err := model.UpdateOption("perf_metrics_setting.group_sample_generation", string(encoded)); err != nil {
		return fmt.Errorf("persist group sample generation: %w", err)
	}
	return nil
}

func persistChannelSampleGeneration(channelID int, generation int64) error {
	sampleGenerationPersistenceMu.Lock()
	defer sampleGenerationPersistenceMu.Unlock()

	setting := perf_metrics_setting.GetSetting()
	gens := map[string]int64{}
	for key, value := range setting.ChannelSampleGeneration {
		gens[key] = value
	}
	key := strconv.Itoa(channelID)
	if generation > gens[key] {
		gens[key] = generation
	}
	encoded, err := common.Marshal(gens)
	if err != nil {
		return fmt.Errorf("marshal channel sample generation: %w", err)
	}
	if err := model.UpdateOption("perf_metrics_setting.channel_sample_generation", string(encoded)); err != nil {
		return fmt.Errorf("persist channel sample generation: %w", err)
	}
	return nil
}
