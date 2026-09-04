package perfmetrics

import (
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/perf_metrics_setting"
)

type ClearGroupResult struct {
	Group   string `json:"group"`
	Hours   int    `json:"hours"`
	StartTs int64  `json:"start_ts"`
	EndTs   int64  `json:"end_ts"`
}

// ClearGroupRecent clears in-memory and persisted samples for group within the
// same hours window used by QueryGroups, then bumps the group sample generation
// so other nodes discard stale hot buckets on flush/query.
func ClearGroupRecent(group string, hours int) (ClearGroupResult, error) {
	group = strings.TrimSpace(group)
	if group == "" {
		return ClearGroupResult{}, fmt.Errorf("group is required")
	}
	hours = NormalizeGroupHours(hours)
	endTs := time.Now().Unix()
	startTs := endTs - int64(hours)*3600

	generation, err := model.ClearPerfMetricGroupInRange(group, startTs, endTs)
	if err != nil {
		return ClearGroupResult{}, err
	}
	clearHotBucketsInRange(group, startTs, endTs)

	persistGroupSampleGeneration(group, generation)

	return ClearGroupResult{
		Group:   group,
		Hours:   hours,
		StartTs: startTs,
		EndTs:   endTs,
	}, nil
}

func clearHotBucketsInRange(group string, startTs, endTs int64) {
	hotBuckets.Range(func(key, value any) bool {
		k := key.(bucketKey)
		if k.group != group {
			return true
		}
		if k.bucketTs < startTs || k.bucketTs > endTs {
			return true
		}
		hotBuckets.Delete(key)
		return true
	})
}

func persistGroupSampleGeneration(group string, generation int64) {
	setting := perf_metrics_setting.GetSetting()
	gens := map[string]int64{}
	for k, v := range setting.GroupSampleGeneration {
		gens[k] = v
	}
	gens[group] = generation
	encoded, err := common.Marshal(gens)
	if err != nil {
		common.SysError("failed to marshal group_sample_generation: " + err.Error())
		perf_metrics_setting.SetGroupSampleGenerationInMemory(group, generation)
		return
	}
	if err := model.UpdateOption("perf_metrics_setting.group_sample_generation", string(encoded)); err != nil {
		common.SysError("failed to persist group_sample_generation: " + err.Error())
		perf_metrics_setting.SetGroupSampleGenerationInMemory(group, generation)
		return
	}
	perf_metrics_setting.SetGroupSampleGenerationInMemory(group, generation)
}
