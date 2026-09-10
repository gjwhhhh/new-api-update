package perfmetrics

import (
	"errors"
	"math"
	"sort"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/perf_metrics_setting"
)

const (
	// GroupHours48 is the default channel-status group metric window.
	GroupHours48 = 48
	// GroupHours7Days is the seven-day channel-status group metric window.
	GroupHours7Days = 168

	maxGroupSeriesLen = 48
)

// ErrUnsupportedGroupHours indicates a request outside the supported group
// metric windows.
var ErrUnsupportedGroupHours = errors.New("hours must be 48 or 168")

// ValidateGroupHours rejects group metric windows other than 48 hours or 7 days.
func ValidateGroupHours(hours int) error {
	if hours == GroupHours48 || hours == GroupHours7Days {
		return nil
	}
	return ErrUnsupportedGroupHours
}

func QueryGroups(hours int, groups []string) (GroupsQueryResult, error) {
	if err := ValidateGroupHours(hours); err != nil {
		return GroupsQueryResult{}, err
	}
	bucketSeconds := perf_metrics_setting.GetBucketSeconds()
	if bucketSeconds <= 0 {
		bucketSeconds = 3600
	}
	startTs, endTs := groupTimeWindow(hours, time.Now().Unix(), bucketSeconds)

	result := GroupsQueryResult{
		BucketSeconds: bucketSeconds,
		StartTs:       startTs,
		EndTs:         endTs,
		Groups:        []GroupMetric{},
	}
	if groups != nil && len(groups) == 0 {
		return result, nil
	}

	rows, err := model.GetPerfMetricsGroupBuckets(startTs, endTs, groups)
	if err != nil {
		return GroupsQueryResult{}, err
	}

	merged := map[bucketKey]counters{}
	for _, row := range rows {
		mergeCounters(merged, bucketKey{
			model:    row.ModelName,
			group:    row.Group,
			bucketTs: row.BucketTs,
		}, counters{
			requestCount:   row.RequestCount,
			successCount:   row.SuccessCount,
			totalLatencyMs: row.TotalLatencyMs,
			ttftSumMs:      row.TtftSumMs,
			ttftCount:      row.TtftCount,
			outputTokens:   row.OutputTokens,
			generationMs:   row.GenerationMs,
		})
	}
	generations, err := model.GetPerfMetricGroupGenerations(groups)
	if err != nil {
		return GroupsQueryResult{}, err
	}

	allowed := allowedGroupSet(groups)
	hotBuckets.Range(func(key, value any) bool {
		k := key.(bucketKey)
		if k.bucketTs < startTs || k.bucketTs > endTs {
			return true
		}
		if allowed != nil {
			if _, ok := allowed[k.group]; !ok {
				return true
			}
		}
		bucket := value.(*atomicBucket)
		if !matchesAuthoritativeGeneration(k.group, bucket, generations) {
			return true
		}
		snap := bucket.snapshot()
		if snap.requestCount == 0 {
			return true
		}
		mergeCounters(merged, k, snap)
		return true
	})

	bucketRows := make([]groupBucketRow, 0, len(merged))
	for key, value := range merged {
		bucketRows = append(bucketRows, groupBucketRow{
			Group:    key.group,
			Model:    key.model,
			BucketTs: key.bucketTs,
			Value:    value,
		})
	}
	result.Groups = buildGroupMetrics(bucketRows, groups, startTs, endTs, bucketSeconds)
	return result, nil
}

// groupTimeWindow returns the most recent configured metric buckets, including
// the bucket that is currently being written. Using bucket boundaries keeps
// every displayed bar backed by a distinct stored sample.
func groupTimeWindow(hours int, nowTs int64, bucketSeconds int64) (int64, int64) {
	if bucketSeconds <= 0 {
		bucketSeconds = 3600
	}
	endTs := nowTs - (nowTs % bucketSeconds)
	bucketCount := int64(hours) * 3600 / bucketSeconds
	if bucketCount < 1 {
		bucketCount = 1
	}
	return endTs - (bucketCount-1)*bucketSeconds, endTs
}

func buildGroupMetrics(
	rows []groupBucketRow,
	requestedGroups []string,
	startTs int64,
	endTs int64,
	bucketSeconds int64,
) []GroupMetric {
	if bucketSeconds <= 0 {
		bucketSeconds = 3600
	}

	type groupAcc struct {
		total  counters
		models map[string]counters
		slots  map[int64]counters
	}

	byGroup := map[string]*groupAcc{}
	ensureGroup := func(name string) *groupAcc {
		acc, ok := byGroup[name]
		if ok {
			return acc
		}
		acc = &groupAcc{
			models: map[string]counters{},
			slots:  map[int64]counters{},
		}
		byGroup[name] = acc
		return acc
	}

	for _, row := range rows {
		if row.Group == "" || row.Value.requestCount == 0 {
			continue
		}
		acc := ensureGroup(row.Group)
		acc.total = addCounters(acc.total, row.Value)
		acc.models[row.Model] = addCounters(acc.models[row.Model], row.Value)
		acc.slots[row.BucketTs] = addCounters(acc.slots[row.BucketTs], row.Value)
	}

	names := requestedGroups
	if names == nil {
		names = make([]string, 0, len(byGroup))
		for name := range byGroup {
			names = append(names, name)
		}
	}

	metrics := make([]GroupMetric, 0, len(names))
	seen := map[string]struct{}{}
	for _, name := range names {
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		acc := byGroup[name]
		if acc == nil {
			acc = &groupAcc{
				models: map[string]counters{},
				slots:  map[int64]counters{},
			}
		}
		metrics = append(metrics, GroupMetric{
			Group:        name,
			RequestCount: acc.total.requestCount,
			SuccessCount: acc.total.successCount,
			SuccessRate:  roundRate(successRate(acc.total)),
			AvgTtftMs:    avg(acc.total.ttftSumMs, acc.total.ttftCount),
			AvgLatencyMs: avg(acc.total.totalLatencyMs, acc.total.requestCount),
			AvgTps:       roundRate(avgTps(acc.total)),
			Series:       buildGroupSeries(acc.slots, startTs, endTs, bucketSeconds),
			Models:       buildGroupModelStats(acc.models),
		})
	}

	return metrics
}

func buildGroupSeries(
	slots map[int64]counters,
	startTs int64,
	endTs int64,
	bucketSeconds int64,
) []GroupBucketPoint {
	alignedStart := startTs - (startTs % bucketSeconds)
	alignedEnd := endTs - (endTs % bucketSeconds)
	if alignedEnd < alignedStart {
		return []GroupBucketPoint{}
	}

	bucketCount := int((alignedEnd-alignedStart)/bucketSeconds) + 1
	seriesCount := min(bucketCount, maxGroupSeriesLen)
	points := make([]GroupBucketPoint, 0, seriesCount)
	for index := 0; index < seriesCount; index++ {
		startIndex := index * bucketCount / seriesCount
		endIndex := (index + 1) * bucketCount / seriesCount
		ts := alignedStart + int64(startIndex)*bucketSeconds
		value := counters{}
		for bucketIndex := startIndex; bucketIndex < endIndex; bucketIndex++ {
			bucketTs := alignedStart + int64(bucketIndex)*bucketSeconds
			value = addCounters(value, slots[bucketTs])
		}
		point := groupBucketPoint(ts, value)
		point.SpanSeconds = int64(endIndex-startIndex) * bucketSeconds
		points = append(points, point)
	}
	return points
}

func buildGroupModelStats(models map[string]counters) []GroupModelStat {
	stats := make([]GroupModelStat, 0, len(models))
	for name, value := range models {
		if name == "" || value.requestCount == 0 {
			continue
		}
		stats = append(stats, GroupModelStat{
			ModelName:    name,
			RequestCount: value.requestCount,
			SuccessCount: value.successCount,
			SuccessRate:  roundRate(successRate(value)),
			AvgTtftMs:    avg(value.ttftSumMs, value.ttftCount),
			AvgLatencyMs: avg(value.totalLatencyMs, value.requestCount),
			AvgTps:       roundRate(avgTps(value)),
		})
	}
	sort.SliceStable(stats, func(i, j int) bool {
		if stats[i].RequestCount == stats[j].RequestCount {
			return stats[i].ModelName < stats[j].ModelName
		}
		return stats[i].RequestCount > stats[j].RequestCount
	})
	return stats
}

func groupBucketPoint(ts int64, value counters) GroupBucketPoint {
	point := GroupBucketPoint{
		Ts:           ts,
		AvgTtftMs:    avg(value.ttftSumMs, value.ttftCount),
		AvgLatencyMs: avg(value.totalLatencyMs, value.requestCount),
		AvgTps:       roundRate(avgTps(value)),
		RequestCount: value.requestCount,
	}
	if value.requestCount > 0 {
		rate := roundRate(successRate(value))
		point.SuccessRate = &rate
	}
	return point
}

func addCounters(current counters, value counters) counters {
	current.requestCount += value.requestCount
	current.successCount += value.successCount
	current.totalLatencyMs += value.totalLatencyMs
	current.ttftSumMs += value.ttftSumMs
	current.ttftCount += value.ttftCount
	current.outputTokens += value.outputTokens
	current.generationMs += value.generationMs
	return current
}

func roundRate(value float64) float64 {
	if value == 0 {
		return 0
	}
	return math.Round(value*100) / 100
}
