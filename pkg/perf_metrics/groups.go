package perfmetrics

import (
	"math"
	"sort"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/perf_metrics_setting"
)

const (
	groupHours24      = 24
	groupHours7d      = 168
	maxGroupSeriesLen = 120
)

func NormalizeGroupHours(hours int) int {
	if hours == groupHours7d {
		return groupHours7d
	}
	return groupHours24
}

func QueryGroups(hours int, groups []string) (GroupsQueryResult, error) {
	hours = NormalizeGroupHours(hours)
	endTs := time.Now().Unix()
	startTs := endTs - int64(hours)*3600
	bucketSeconds := perf_metrics_setting.GetBucketSeconds()
	if bucketSeconds <= 0 {
		bucketSeconds = 3600
	}

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
		if !isCurrentHotBucket(k.group, bucket) {
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

	n := int((alignedEnd-alignedStart)/bucketSeconds) + 1
	points := make([]GroupBucketPoint, 0, n)
	for ts := alignedStart; ts <= alignedEnd; ts += bucketSeconds {
		points = append(points, groupBucketPoint(ts, slots[ts]))
	}
	return downsampleGroupSeries(points, maxGroupSeriesLen)
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

func downsampleGroupSeries(points []GroupBucketPoint, maxPoints int) []GroupBucketPoint {
	if maxPoints <= 0 || len(points) <= maxPoints {
		return points
	}
	out := make([]GroupBucketPoint, 0, maxPoints)
	for i := 0; i < maxPoints; i++ {
		start := i * len(points) / maxPoints
		end := (i + 1) * len(points) / maxPoints
		if end <= start {
			end = start + 1
		}
		out = append(out, mergeGroupBucketPoints(points[start:end]))
	}
	return out
}

func mergeGroupBucketPoints(points []GroupBucketPoint) GroupBucketPoint {
	if len(points) == 1 {
		return points[0]
	}
	total := counters{}
	for _, point := range points {
		total.requestCount += point.RequestCount
		if point.SuccessRate != nil && point.RequestCount > 0 {
			total.successCount += int64(math.Round(*point.SuccessRate / 100 * float64(point.RequestCount)))
		}
		total.totalLatencyMs += point.AvgLatencyMs * point.RequestCount
		if point.AvgTtftMs > 0 {
			total.ttftSumMs += point.AvgTtftMs * point.RequestCount
			total.ttftCount += point.RequestCount
		}
		if point.AvgTps > 0 && point.RequestCount > 0 {
			// Reconstruct a generation duration so avgTps stays consistent.
			total.outputTokens += int64(math.Round(point.AvgTps))
			total.generationMs += 1000
		}
	}
	merged := groupBucketPoint(points[0].Ts, total)
	if total.requestCount > 0 {
		merged.AvgLatencyMs = avg(total.totalLatencyMs, total.requestCount)
		merged.AvgTtftMs = avg(total.ttftSumMs, total.ttftCount)
		merged.AvgTps = 0
		for _, point := range points {
			if point.RequestCount > 0 && point.AvgTps > 0 {
				merged.AvgTps += point.AvgTps * float64(point.RequestCount)
			}
		}
		merged.AvgTps = roundRate(merged.AvgTps / float64(total.requestCount))
	}
	return merged
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
