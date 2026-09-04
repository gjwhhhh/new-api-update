package model

import (
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// PerfMetric stores aggregated relay performance metrics for the model square.
type PerfMetric struct {
	Id             int    `json:"id" gorm:"primaryKey"`
	ModelName      string `json:"model_name" gorm:"size:128;uniqueIndex:idx_perf_model_group_bucket,priority:1"`
	Group          string `json:"group" gorm:"column:group;size:64;uniqueIndex:idx_perf_model_group_bucket,priority:2"`
	BucketTs       int64  `json:"bucket_ts" gorm:"uniqueIndex:idx_perf_model_group_bucket,priority:3;index:idx_perf_bucket_ts"`
	RequestCount   int64  `json:"-" gorm:"default:0"`
	SuccessCount   int64  `json:"-" gorm:"default:0"`
	TotalLatencyMs int64  `json:"-" gorm:"default:0"`
	TtftSumMs      int64  `json:"-" gorm:"default:0"`
	TtftCount      int64  `json:"-" gorm:"default:0"`
	OutputTokens   int64  `json:"-" gorm:"default:0"`
	GenerationMs   int64  `json:"-" gorm:"default:0"`
}

func (PerfMetric) TableName() string {
	return "perf_metrics"
}

// PerfMetricGroupState is the shared generation fence for one performance
// metrics group. Clear and flush operations lock this row so a bucket that
// predates a clear can never be persisted after that clear commits.
type PerfMetricGroupState struct {
	Group      string `json:"group" gorm:"column:group;size:64;primaryKey"`
	Generation int64  `json:"generation" gorm:"not null"`
}

func (PerfMetricGroupState) TableName() string {
	return "perf_metric_group_states"
}

func UpsertPerfMetric(metric *PerfMetric) error {
	if metric == nil || metric.RequestCount == 0 {
		return nil
	}
	return upsertPerfMetric(DB, metric)
}

func upsertPerfMetric(tx *gorm.DB, metric *PerfMetric) error {
	return tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "model_name"},
			{Name: "group"},
			{Name: "bucket_ts"},
		},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"request_count":    gorm.Expr("perf_metrics.request_count + ?", metric.RequestCount),
			"success_count":    gorm.Expr("perf_metrics.success_count + ?", metric.SuccessCount),
			"total_latency_ms": gorm.Expr("perf_metrics.total_latency_ms + ?", metric.TotalLatencyMs),
			"ttft_sum_ms":      gorm.Expr("perf_metrics.ttft_sum_ms + ?", metric.TtftSumMs),
			"ttft_count":       gorm.Expr("perf_metrics.ttft_count + ?", metric.TtftCount),
			"output_tokens":    gorm.Expr("perf_metrics.output_tokens + ?", metric.OutputTokens),
			"generation_ms":    gorm.Expr("perf_metrics.generation_ms + ?", metric.GenerationMs),
		}),
	}).Create(metric).Error
}

// UpsertPerfMetricIfCurrentGeneration persists metric only when its local
// bucket generation matches the group generation held in the shared database.
// The generation row lock serializes this decision with group clears.
func UpsertPerfMetricIfCurrentGeneration(metric *PerfMetric, generation int64) (bool, error) {
	if metric == nil || metric.RequestCount == 0 {
		return false, nil
	}
	if metric.Group == "" {
		return false, fmt.Errorf("perf metric group is required")
	}

	persisted := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		state, err := lockPerfMetricGroupState(tx, metric.Group)
		if err != nil {
			return err
		}
		if generation != state.Generation {
			return nil
		}
		if err := upsertPerfMetric(tx, metric); err != nil {
			return err
		}
		persisted = true
		return nil
	})
	return persisted, err
}

// ClearPerfMetricGroupInRange advances the group's shared generation and
// deletes metrics in one transaction. A concurrent flush either commits before
// this delete or observes the new generation and discards its stale bucket.
func ClearPerfMetricGroupInRange(group string, startTs int64, endTs int64) (int64, error) {
	if group == "" {
		return 0, fmt.Errorf("perf metric group is required")
	}
	if startTs <= 0 || endTs < startTs {
		return 0, fmt.Errorf("invalid perf metric time range")
	}

	var generation int64
	err := DB.Transaction(func(tx *gorm.DB) error {
		state, err := lockPerfMetricGroupState(tx, group)
		if err != nil {
			return err
		}
		state.Generation++
		if err := tx.Model(state).Update("generation", state.Generation).Error; err != nil {
			return err
		}
		if err := tx.Where(commonGroupCol+" = ? AND bucket_ts >= ? AND bucket_ts <= ?", group, startTs, endTs).
			Delete(&PerfMetric{}).Error; err != nil {
			return err
		}
		generation = state.Generation
		return nil
	})
	return generation, err
}

func lockPerfMetricGroupState(tx *gorm.DB, group string) (*PerfMetricGroupState, error) {
	state := PerfMetricGroupState{Group: group}
	if err := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "group"}},
		DoNothing: true,
	}).Create(&state).Error; err != nil {
		return nil, err
	}
	if err := lockForUpdate(tx).Where(commonGroupCol+" = ?", group).First(&state).Error; err != nil {
		return nil, err
	}
	return &state, nil
}

func GetPerfMetrics(modelName string, group string, startTs int64, endTs int64) ([]PerfMetric, error) {
	var metrics []PerfMetric
	query := DB.Model(&PerfMetric{}).
		Where("model_name = ? AND bucket_ts >= ? AND bucket_ts <= ?", modelName, startTs, endTs)
	if group != "" {
		query = query.Where(commonGroupCol+" = ?", group)
	}
	err := query.Order("bucket_ts ASC").Find(&metrics).Error
	return metrics, err
}

// GetPerfMetricGroupGenerations returns the authoritative clear generation for
// each requested group. Groups without a state row are at generation zero.
func GetPerfMetricGroupGenerations(groups []string) (map[string]int64, error) {
	generations := make(map[string]int64)
	if groups != nil && len(groups) == 0 {
		return generations, nil
	}

	var states []PerfMetricGroupState
	query := DB.Model(&PerfMetricGroupState{})
	if groups != nil {
		query = query.Where(commonGroupCol+" IN ?", groups)
	}
	if err := query.Find(&states).Error; err != nil {
		return nil, err
	}
	for _, state := range states {
		generations[state.Group] = state.Generation
	}
	return generations, nil
}

type PerfMetricSummary struct {
	ModelName      string `json:"model_name"`
	RequestCount   int64  `json:"request_count"`
	SuccessCount   int64  `json:"success_count"`
	TotalLatencyMs int64  `json:"total_latency_ms"`
	OutputTokens   int64  `json:"output_tokens"`
	GenerationMs   int64  `json:"generation_ms"`
}

type PerfMetricSummaryBucket struct {
	ModelName      string `json:"model_name"`
	BucketTs       int64  `json:"bucket_ts"`
	RequestCount   int64  `json:"request_count"`
	SuccessCount   int64  `json:"success_count"`
	TotalLatencyMs int64  `json:"total_latency_ms"`
	OutputTokens   int64  `json:"output_tokens"`
	GenerationMs   int64  `json:"generation_ms"`
}

func GetPerfMetricsSummaryAll(startTs int64, endTs int64, groups []string) ([]PerfMetricSummary, error) {
	var summaries []PerfMetricSummary
	query := DB.Model(&PerfMetric{}).
		Select("model_name, SUM(request_count) as request_count, SUM(success_count) as success_count, SUM(total_latency_ms) as total_latency_ms, SUM(output_tokens) as output_tokens, SUM(generation_ms) as generation_ms").
		Where("bucket_ts >= ? AND bucket_ts <= ?", startTs, endTs)
	if groups != nil {
		if len(groups) == 0 {
			return summaries, nil
		}
		query = query.Where(commonGroupCol+" IN ?", groups)
	}
	err := query.
		Group("model_name").
		Having("SUM(request_count) > 0").
		Find(&summaries).Error
	return summaries, err
}

type PerfMetricGroupBucket struct {
	Group          string `json:"group"`
	ModelName      string `json:"model_name"`
	BucketTs       int64  `json:"bucket_ts"`
	RequestCount   int64  `json:"request_count"`
	SuccessCount   int64  `json:"success_count"`
	TotalLatencyMs int64  `json:"total_latency_ms"`
	TtftSumMs      int64  `json:"ttft_sum_ms"`
	TtftCount      int64  `json:"ttft_count"`
	OutputTokens   int64  `json:"output_tokens"`
	GenerationMs   int64  `json:"generation_ms"`
}

func GetPerfMetricsGroupBuckets(startTs int64, endTs int64, groups []string) ([]PerfMetricGroupBucket, error) {
	var rows []PerfMetricGroupBucket
	if groups != nil && len(groups) == 0 {
		return rows, nil
	}
	query := DB.Model(&PerfMetric{}).
		Select(commonGroupCol+", model_name, bucket_ts, SUM(request_count) as request_count, SUM(success_count) as success_count, SUM(total_latency_ms) as total_latency_ms, SUM(ttft_sum_ms) as ttft_sum_ms, SUM(ttft_count) as ttft_count, SUM(output_tokens) as output_tokens, SUM(generation_ms) as generation_ms").
		Where("bucket_ts >= ? AND bucket_ts <= ?", startTs, endTs)
	if groups != nil {
		query = query.Where(commonGroupCol+" IN ?", groups)
	}
	err := query.
		Group(commonGroupCol + ", model_name, bucket_ts").
		Having("SUM(request_count) > 0").
		Order("bucket_ts ASC").
		Find(&rows).Error
	return rows, err
}

func GetPerfMetricsSummaryBucketsAll(startTs int64, endTs int64, groups []string) ([]PerfMetricSummaryBucket, error) {
	var summaries []PerfMetricSummaryBucket
	query := DB.Model(&PerfMetric{}).
		Select("model_name, bucket_ts, SUM(request_count) as request_count, SUM(success_count) as success_count, SUM(total_latency_ms) as total_latency_ms, SUM(output_tokens) as output_tokens, SUM(generation_ms) as generation_ms").
		Where("bucket_ts >= ? AND bucket_ts <= ?", startTs, endTs)
	if groups != nil {
		if len(groups) == 0 {
			return summaries, nil
		}
		query = query.Where(commonGroupCol+" IN ?", groups)
	}
	err := query.
		Group("model_name, bucket_ts").
		Having("SUM(request_count) > 0").
		Order("bucket_ts ASC").
		Find(&summaries).Error
	return summaries, err
}

func DeletePerfMetricsBefore(cutoffTs int64) error {
	if cutoffTs <= 0 {
		return nil
	}
	return DB.Where("bucket_ts < ?", cutoffTs).Delete(&PerfMetric{}).Error
}

func PerfMetricStartTime(hours int) int64 {
	if hours <= 0 {
		hours = 24
	}
	return time.Now().Add(-time.Duration(hours) * time.Hour).Unix()
}
