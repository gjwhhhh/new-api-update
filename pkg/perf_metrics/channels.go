package perfmetrics

import (
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/perf_metrics_setting"
)

const (
	ChannelHealthRunning     = "running"
	ChannelHealthFluctuating = "fluctuating"
	ChannelHealthAbnormal    = "abnormal"
	ChannelHealthNoData      = "no_data"
)

type ChannelMetricWindow struct {
	BucketSeconds int64
	StartTs       int64
	EndTs         int64
}

// ChannelHealth returns the stable health label shared by the channel API and
// the frontend. The thresholds match the existing status-card color mapping.
func ChannelHealth(requestCount int64, successRate float64) string {
	if requestCount <= 0 {
		return ChannelHealthNoData
	}
	if successRate >= 90 {
		return ChannelHealthRunning
	}
	if successRate >= 70 {
		return ChannelHealthFluctuating
	}
	return ChannelHealthAbnormal
}

func QueryChannels(hours int, channelIDs []int) (ChannelsQueryResult, error) {
	window, err := GetChannelMetricWindow(hours)
	if err != nil {
		return ChannelsQueryResult{}, err
	}
	return QueryChannelsInWindow(window, channelIDs)
}

func GetChannelMetricWindow(hours int) (ChannelMetricWindow, error) {
	if err := ValidateGroupHours(hours); err != nil {
		return ChannelMetricWindow{}, err
	}
	bucketSeconds := perf_metrics_setting.GetBucketSeconds()
	if bucketSeconds <= 0 {
		bucketSeconds = 3600
	}
	startTs, endTs := groupTimeWindow(hours, time.Now().Unix(), bucketSeconds)
	return ChannelMetricWindow{
		BucketSeconds: bucketSeconds,
		StartTs:       startTs,
		EndTs:         endTs,
	}, nil
}

// QueryChannelsInWindow lets the paged list use the exact same time window
// for its database sort/count query and the returned detailed card metrics.
func QueryChannelsInWindow(window ChannelMetricWindow, channelIDs []int) (ChannelsQueryResult, error) {
	if window.BucketSeconds <= 0 || window.StartTs <= 0 || window.EndTs < window.StartTs {
		return ChannelsQueryResult{}, fmt.Errorf("invalid channel metric window")
	}
	result := ChannelsQueryResult{
		BucketSeconds: window.BucketSeconds,
		StartTs:       window.StartTs,
		EndTs:         window.EndTs,
		Channels:      []ChannelMetric{},
	}
	channelIDs = normalizeChannelIDs(channelIDs)
	if len(channelIDs) == 0 {
		return result, nil
	}

	rows, err := model.GetPerfChannelMetricBuckets(window.StartTs, window.EndTs, channelIDs)
	if err != nil {
		return ChannelsQueryResult{}, err
	}

	merged := make(map[channelBucketKey]counters, len(rows))
	for _, row := range rows {
		mergeChannelCounters(merged, channelBucketKey{
			channelID: row.ChannelId,
			model:     row.ModelName,
			bucketTs:  row.BucketTs,
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

	allowed := make(map[int]struct{}, len(channelIDs))
	for _, channelID := range channelIDs {
		allowed[channelID] = struct{}{}
	}
	generations, err := model.GetPerfChannelMetricGenerations(channelIDs)
	if err != nil {
		return ChannelsQueryResult{}, err
	}
	channelHotBuckets.Range(func(key, value any) bool {
		k := key.(channelBucketKey)
		if k.bucketTs < window.StartTs || k.bucketTs > window.EndTs {
			return true
		}
		if _, ok := allowed[k.channelID]; !ok {
			return true
		}
		bucket := value.(*atomicBucket)
		if !matchesAuthoritativeChannelGeneration(k.channelID, bucket, generations) {
			return true
		}
		snapshot := bucket.snapshot()
		if snapshot.requestCount > 0 {
			mergeChannelCounters(merged, k, snapshot)
		}
		return true
	})

	rowsByChannel := make([]channelBucketRow, 0, len(merged))
	for key, value := range merged {
		rowsByChannel = append(rowsByChannel, channelBucketRow{
			ChannelID: key.channelID,
			Model:     key.model,
			BucketTs:  key.bucketTs,
			Value:     value,
		})
	}
	result.Channels = buildChannelMetrics(
		rowsByChannel,
		channelIDs,
		window.StartTs,
		window.EndTs,
		window.BucketSeconds,
	)
	return result, nil
}

func normalizeChannelIDs(ids []int) []int {
	seen := make(map[int]struct{}, len(ids))
	result := make([]int, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result
}

func mergeChannelCounters(merged map[channelBucketKey]counters, key channelBucketKey, value counters) {
	if value.requestCount == 0 {
		return
	}
	merged[key] = addCounters(merged[key], value)
}

func buildChannelMetrics(
	rows []channelBucketRow,
	requestedChannelIDs []int,
	startTs int64,
	endTs int64,
	bucketSeconds int64,
) []ChannelMetric {
	type channelAcc struct {
		total  counters
		models map[string]counters
		slots  map[int64]counters
	}

	byChannel := make(map[int]*channelAcc, len(requestedChannelIDs))
	ensureChannel := func(channelID int) *channelAcc {
		acc, ok := byChannel[channelID]
		if ok {
			return acc
		}
		acc = &channelAcc{
			models: make(map[string]counters),
			slots:  make(map[int64]counters),
		}
		byChannel[channelID] = acc
		return acc
	}

	for _, row := range rows {
		if row.ChannelID <= 0 || row.Value.requestCount == 0 {
			continue
		}
		acc := ensureChannel(row.ChannelID)
		acc.total = addCounters(acc.total, row.Value)
		acc.models[row.Model] = addCounters(acc.models[row.Model], row.Value)
		acc.slots[row.BucketTs] = addCounters(acc.slots[row.BucketTs], row.Value)
	}

	metrics := make([]ChannelMetric, 0, len(requestedChannelIDs))
	for _, channelID := range requestedChannelIDs {
		acc := ensureChannel(channelID)
		metrics = append(metrics, ChannelMetric{
			ChannelID:    channelID,
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
