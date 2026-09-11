package model

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const perfChannelMetricQueryChunkSize = 500

// PerfChannelMetric stores aggregated relay performance metrics for one
// channel and model. It is deliberately separate from PerfMetric: group
// metrics represent a user's final request, while channel metrics identify the
// upstream channel that served that request.
type PerfChannelMetric struct {
	Id             int    `json:"id" gorm:"primaryKey"`
	ChannelId      int    `json:"channel_id" gorm:"uniqueIndex:idx_perf_channel_model_bucket,priority:1;index:idx_perf_channel_bucket,priority:1"`
	ModelName      string `json:"model_name" gorm:"size:128;uniqueIndex:idx_perf_channel_model_bucket,priority:2"`
	BucketTs       int64  `json:"bucket_ts" gorm:"uniqueIndex:idx_perf_channel_model_bucket,priority:3;index:idx_perf_channel_bucket,priority:2;index:idx_perf_channel_bucket_ts"`
	RequestCount   int64  `json:"-" gorm:"default:0"`
	SuccessCount   int64  `json:"-" gorm:"default:0"`
	TotalLatencyMs int64  `json:"-" gorm:"default:0"`
	TtftSumMs      int64  `json:"-" gorm:"default:0"`
	TtftCount      int64  `json:"-" gorm:"default:0"`
	OutputTokens   int64  `json:"-" gorm:"default:0"`
	GenerationMs   int64  `json:"-" gorm:"default:0"`
}

func (PerfChannelMetric) TableName() string {
	return "perf_channel_metrics"
}

func UpsertPerfChannelMetric(metric *PerfChannelMetric) error {
	if metric == nil || metric.ChannelId <= 0 || metric.ModelName == "" || metric.RequestCount == 0 {
		return nil
	}
	return upsertPerfChannelMetric(DB, metric)
}

func upsertPerfChannelMetric(tx *gorm.DB, metric *PerfChannelMetric) error {
	return tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "channel_id"},
			{Name: "model_name"},
			{Name: "bucket_ts"},
		},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"request_count":    gorm.Expr("perf_channel_metrics.request_count + ?", metric.RequestCount),
			"success_count":    gorm.Expr("perf_channel_metrics.success_count + ?", metric.SuccessCount),
			"total_latency_ms": gorm.Expr("perf_channel_metrics.total_latency_ms + ?", metric.TotalLatencyMs),
			"ttft_sum_ms":      gorm.Expr("perf_channel_metrics.ttft_sum_ms + ?", metric.TtftSumMs),
			"ttft_count":       gorm.Expr("perf_channel_metrics.ttft_count + ?", metric.TtftCount),
			"output_tokens":    gorm.Expr("perf_channel_metrics.output_tokens + ?", metric.OutputTokens),
			"generation_ms":    gorm.Expr("perf_channel_metrics.generation_ms + ?", metric.GenerationMs),
		}),
	}).Create(metric).Error
}

// PerfChannelMetricState is the shared generation fence for one channel's
// metrics. It prevents an old in-memory bucket on any instance from being
// persisted after an administrator clears that channel's recent samples.
type PerfChannelMetricState struct {
	ChannelId  int   `json:"channel_id" gorm:"column:channel_id;primaryKey"`
	Generation int64 `json:"generation" gorm:"not null"`
}

func (PerfChannelMetricState) TableName() string {
	return "perf_channel_metric_states"
}

// UpsertPerfChannelMetricIfCurrentGeneration persists a bucket only when its
// generation matches the channel generation held in the shared database.
func UpsertPerfChannelMetricIfCurrentGeneration(metric *PerfChannelMetric, generation int64) (bool, error) {
	if metric == nil || metric.RequestCount == 0 {
		return false, nil
	}
	if metric.ChannelId <= 0 {
		return false, fmt.Errorf("perf channel metric channel id is required")
	}
	if metric.ModelName == "" {
		return false, fmt.Errorf("perf channel metric model name is required")
	}

	persisted := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		state, err := lockPerfChannelMetricState(tx, metric.ChannelId)
		if err != nil {
			return err
		}
		if generation != state.Generation {
			return nil
		}
		if err := upsertPerfChannelMetric(tx, metric); err != nil {
			return err
		}
		persisted = true
		return nil
	})
	return persisted, err
}

// ClearPerfChannelMetricInRange advances the channel generation and removes
// persisted metric buckets in one transaction. A concurrent flush either
// commits before this deletion or observes the new generation and discards
// its stale bucket.
func ClearPerfChannelMetricInRange(channelID int, startTs int64, endTs int64) (int64, error) {
	if channelID <= 0 {
		return 0, fmt.Errorf("perf channel metric channel id is required")
	}
	if startTs <= 0 || endTs < startTs {
		return 0, fmt.Errorf("invalid perf channel metric time range")
	}

	var generation int64
	err := DB.Transaction(func(tx *gorm.DB) error {
		state, err := lockPerfChannelMetricState(tx, channelID)
		if err != nil {
			return err
		}
		state.Generation++
		if err := tx.Model(state).Update("generation", state.Generation).Error; err != nil {
			return err
		}
		if err := tx.Where("channel_id = ? AND bucket_ts >= ? AND bucket_ts <= ?", channelID, startTs, endTs).
			Delete(&PerfChannelMetric{}).Error; err != nil {
			return err
		}
		generation = state.Generation
		return nil
	})
	return generation, err
}

func lockPerfChannelMetricState(tx *gorm.DB, channelID int) (*PerfChannelMetricState, error) {
	state := PerfChannelMetricState{ChannelId: channelID}
	if err := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "channel_id"}},
		DoNothing: true,
	}).Create(&state).Error; err != nil {
		return nil, err
	}
	if err := lockForUpdate(tx).Where("channel_id = ?", channelID).First(&state).Error; err != nil {
		return nil, err
	}
	return &state, nil
}

type PerfChannelMetricBucket struct {
	ChannelId      int    `json:"channel_id"`
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

func GetPerfChannelMetricBuckets(startTs int64, endTs int64, channelIDs []int) ([]PerfChannelMetricBucket, error) {
	rows := make([]PerfChannelMetricBucket, 0)
	if len(channelIDs) == 0 {
		return rows, nil
	}
	for start := 0; start < len(channelIDs); start += perfChannelMetricQueryChunkSize {
		end := start + perfChannelMetricQueryChunkSize
		if end > len(channelIDs) {
			end = len(channelIDs)
		}
		chunkRows := make([]PerfChannelMetricBucket, 0)
		if err := DB.Model(&PerfChannelMetric{}).
			Select("channel_id, model_name, bucket_ts, SUM(request_count) as request_count, SUM(success_count) as success_count, SUM(total_latency_ms) as total_latency_ms, SUM(ttft_sum_ms) as ttft_sum_ms, SUM(ttft_count) as ttft_count, SUM(output_tokens) as output_tokens, SUM(generation_ms) as generation_ms").
			Where("bucket_ts >= ? AND bucket_ts <= ? AND channel_id IN ?", startTs, endTs, channelIDs[start:end]).
			Group("channel_id, model_name, bucket_ts").
			Having("SUM(request_count) > 0").
			Find(&chunkRows).Error; err != nil {
			return nil, err
		}
		rows = append(rows, chunkRows...)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].BucketTs != rows[j].BucketTs {
			return rows[i].BucketTs < rows[j].BucketTs
		}
		if rows[i].ChannelId != rows[j].ChannelId {
			return rows[i].ChannelId < rows[j].ChannelId
		}
		return rows[i].ModelName < rows[j].ModelName
	})
	return rows, nil
}

// GetPerfChannelMetricGenerations returns the authoritative clear generation
// for each requested channel. Channels without a state row are at generation
// zero.
func GetPerfChannelMetricGenerations(channelIDs []int) (map[int]int64, error) {
	generations := make(map[int]int64)
	if len(channelIDs) == 0 {
		return generations, nil
	}

	for start := 0; start < len(channelIDs); start += perfChannelMetricQueryChunkSize {
		end := start + perfChannelMetricQueryChunkSize
		if end > len(channelIDs) {
			end = len(channelIDs)
		}
		states := make([]PerfChannelMetricState, 0)
		if err := DB.Where("channel_id IN ?", channelIDs[start:end]).Find(&states).Error; err != nil {
			return nil, err
		}
		for _, state := range states {
			generations[state.ChannelId] = state.Generation
		}
	}
	return generations, nil
}

// ChannelStatusMetricPageParams describes the database-backed portion of the
// admin channel-status list. It intentionally contains only safe channel
// fields and aggregate counters, never channel credentials or settings.
type ChannelStatusMetricPageParams struct {
	StartTs       int64
	EndTs         int64
	Search        string
	ChannelStatus *int
	ChannelType   *int
	Health        string
	Sort          string
	Desc          bool
	Offset        int
	Limit         int
}

type ChannelStatusMetricSummary struct {
	ChannelID     int     `gorm:"column:channel_id"`
	ChannelName   string  `gorm:"column:channel_name"`
	ChannelType   int     `gorm:"column:channel_type"`
	ChannelStatus int     `gorm:"column:channel_status"`
	ChannelGroup  string  `gorm:"column:channel_group"`
	Health        string  `gorm:"column:health"`
	RequestCount  int64   `gorm:"column:request_count"`
	SuccessCount  int64   `gorm:"column:success_count"`
	SuccessRate   float64 `gorm:"column:success_rate"`
}

type ChannelStatusMetricPageResult struct {
	Items        []ChannelStatusMetricSummary
	Total        int64
	HealthCounts map[string]int64
}

// GetChannelStatusMetricPage computes channel health, sort order, counts, and
// pagination in the database. Detailed per-model series are loaded separately
// only for the returned page.
func GetChannelStatusMetricPage(params ChannelStatusMetricPageParams) (ChannelStatusMetricPageResult, error) {
	result := ChannelStatusMetricPageResult{
		Items: []ChannelStatusMetricSummary{},
		HealthCounts: map[string]int64{
			"running":     0,
			"fluctuating": 0,
			"abnormal":    0,
			"no_data":     0,
		},
	}
	if params.StartTs <= 0 || params.EndTs < params.StartTs {
		return result, fmt.Errorf("invalid channel status metric time range")
	}

	countsQuery, _, healthExpression, _, err := channelStatusMetricQuery(params)
	if err != nil {
		return result, err
	}
	var healthRows []struct {
		Health string `gorm:"column:health"`
		Count  int64  `gorm:"column:count"`
	}
	if err := countsQuery.
		Select(healthExpression + " AS health, COUNT(*) AS count").
		Group(healthExpression).
		Scan(&healthRows).Error; err != nil {
		return result, err
	}
	for _, row := range healthRows {
		result.HealthCounts[row.Health] = row.Count
		result.Total += row.Count
	}
	if params.Health != "" {
		result.Total = result.HealthCounts[params.Health]
	}
	if result.Total == 0 || params.Limit <= 0 {
		return result, nil
	}

	pageQuery, selectFields, pageHealthExpression, successRateExpression, err := channelStatusMetricQuery(params)
	if err != nil {
		return result, err
	}
	if params.Health != "" {
		pageQuery = pageQuery.Where(pageHealthExpression+" = ?", params.Health)
	}
	orderExpression := "channels.id"
	switch strings.ToLower(strings.TrimSpace(params.Sort)) {
	case "traffic":
		orderExpression = "COALESCE(metric_summary.request_count, 0)"
	case "success_rate":
		orderExpression = successRateExpression
	}
	direction := "ASC"
	if params.Desc {
		direction = "DESC"
	}
	if params.Offset < 0 {
		params.Offset = 0
	}
	if err := pageQuery.
		Select(selectFields).
		Order(orderExpression + " " + direction).
		Order("channels.id " + direction).
		Offset(params.Offset).
		Limit(params.Limit).
		Scan(&result.Items).Error; err != nil {
		return result, err
	}
	return result, nil
}

func channelStatusMetricQuery(params ChannelStatusMetricPageParams) (*gorm.DB, string, string, string, error) {
	metricSummary := DB.Model(&PerfChannelMetric{}).
		Select("channel_id, SUM(request_count) AS request_count, SUM(success_count) AS success_count").
		Where("bucket_ts >= ? AND bucket_ts <= ?", params.StartTs, params.EndTs).
		Group("channel_id")

	requestCountExpression := "COALESCE(metric_summary.request_count, 0)"
	successCountExpression := "COALESCE(metric_summary.success_count, 0)"
	successRateExpression := fmt.Sprintf(
		"ROUND(CASE WHEN %s > 0 THEN 100.0 * %s / %s ELSE 0 END, 2)",
		requestCountExpression,
		successCountExpression,
		requestCountExpression,
	)
	healthExpression := fmt.Sprintf(
		"CASE WHEN %s <= 0 THEN 'no_data' WHEN %s >= 90 THEN 'running' WHEN %s >= 70 THEN 'fluctuating' ELSE 'abnormal' END",
		requestCountExpression,
		successRateExpression,
		successRateExpression,
	)
	selectFields := fmt.Sprintf(
		"channels.id AS channel_id, channels.name AS channel_name, channels.type AS channel_type, channels.status AS channel_status, %s AS channel_group, %s AS request_count, %s AS success_count, %s AS success_rate, %s AS health",
		channelStatusGroupColumn(),
		requestCountExpression,
		successCountExpression,
		successRateExpression,
		healthExpression,
	)
	query := DB.Table("channels AS channels").
		Joins("LEFT JOIN (?) AS metric_summary ON metric_summary.channel_id = channels.id", metricSummary)
	if params.ChannelStatus != nil {
		query = query.Where("channels.status = ?", *params.ChannelStatus)
	}
	if params.ChannelType != nil {
		query = query.Where("channels.type = ?", *params.ChannelType)
	}
	search := strings.TrimSpace(params.Search)
	if search != "" {
		pattern := "%" + escapeChannelStatusSearch(strings.ToLower(search)) + "%"
		if channelID, err := strconv.Atoi(search); err == nil && channelID > 0 {
			query = query.Where("(LOWER(channels.name) LIKE ? ESCAPE '!' OR channels.id = ?)", pattern, channelID)
		} else {
			query = query.Where("LOWER(channels.name) LIKE ? ESCAPE '!'", pattern)
		}
	}
	return query, selectFields, healthExpression, successRateExpression, nil
}

func channelStatusGroupColumn() string {
	if commonGroupCol != "" {
		return "channels." + commonGroupCol
	}
	if DB != nil && DB.Dialector.Name() == "postgres" {
		return `channels."group"`
	}
	return "channels.`group`"
}

func escapeChannelStatusSearch(value string) string {
	value = strings.ReplaceAll(value, "!", "!!")
	value = strings.ReplaceAll(value, "%", "!%")
	return strings.ReplaceAll(value, "_", "!_")
}

func DeletePerfChannelMetricsBefore(cutoffTs int64) error {
	if cutoffTs <= 0 {
		return nil
	}
	return DB.Where("bucket_ts < ?", cutoffTs).Delete(&PerfChannelMetric{}).Error
}

// GetChannelStatusCatalog returns only the channel fields needed by the
// status page. Sensitive channel configuration is never loaded for this view.
func GetChannelStatusCatalog() ([]*Channel, error) {
	channels := make([]*Channel, 0)
	err := DB.Select([]string{"Id", "Name", "Type", "Status", "Group"}).
		Order("id ASC").
		Find(&channels).Error
	if err != nil {
		return nil, fmt.Errorf("get channel status catalog: %w", err)
	}
	return channels, nil
}

// GetChannelStatusByID returns the safe channel fields displayed in the
// channel-status detail pane.
func GetChannelStatusByID(id int) (*Channel, error) {
	channel := &Channel{}
	err := DB.Select([]string{"Id", "Name", "Type", "Status", "Group"}).
		First(channel, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return channel, nil
}
