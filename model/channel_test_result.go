package model

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

type ChannelTestResult struct {
	ID                 int64  `json:"id" gorm:"primaryKey;index:idx_channel_test_time,priority:2;index:idx_channel_test_channel_time,priority:3;index:idx_channel_test_task_time,priority:3;index:idx_channel_test_status_time,priority:3"`
	RunID              string `json:"run_id" gorm:"type:varchar(64);index:idx_channel_test_run"`
	RequestID          string `json:"request_id" gorm:"type:varchar(64);index:idx_channel_test_request"`
	TaskID             string `json:"task_id,omitempty" gorm:"type:varchar(64);index:idx_channel_test_task_time,priority:1"`
	ChannelID          int    `json:"channel_id" gorm:"index:idx_channel_test_channel_time,priority:1"`
	ChannelName        string `json:"channel_name" gorm:"type:varchar(255)"`
	ChannelType        int    `json:"channel_type"`
	ChannelGroups      string `json:"channel_groups" gorm:"type:varchar(4096)"`
	Source             string `json:"source" gorm:"type:varchar(32);index"`
	HealthCheckMode    string `json:"health_check_mode,omitempty" gorm:"type:varchar(32)"`
	ModelName          string `json:"model_name" gorm:"type:varchar(255);index"`
	EndpointType       string `json:"endpoint_type,omitempty" gorm:"type:varchar(64)"`
	RequestPath        string `json:"request_path" gorm:"type:varchar(255)"`
	IsStream           bool   `json:"is_stream"`
	Status             string `json:"status" gorm:"type:varchar(32);index:idx_channel_test_status_time,priority:1"`
	UpstreamHTTPStatus int    `json:"upstream_http_status"`
	ResultStatusCode   int    `json:"result_status_code"`
	FailureKind        string `json:"failure_kind" gorm:"type:varchar(64);index"`
	KeyIndex           *int   `json:"key_index,omitempty"`
	StateAction        string `json:"state_action" gorm:"type:varchar(32)"`
	DurationMs         int64  `json:"duration_ms"`
	CreatedAt          int64  `json:"created_at" gorm:"bigint;index:idx_channel_test_time,priority:1;index:idx_channel_test_channel_time,priority:2;index:idx_channel_test_task_time,priority:2;index:idx_channel_test_status_time,priority:2"`
}

func (result *ChannelTestResult) BeforeCreate(_ *gorm.DB) error {
	if result.CreatedAt == 0 {
		result.CreatedAt = common.GetTimestamp()
	}
	return nil
}

type ChannelTestResultFilter struct {
	ChannelID int
	Group     string
	RunID     string
	TaskID    string
	Source    string
	Status    string
	ModelName string
	StartAt   int64
	EndAt     int64
	Offset    int
	Limit     int
}

type ChannelTestFilterOption struct {
	ID            int      `json:"id"`
	Name          string   `json:"name"`
	Status        int      `json:"status"`
	Groups        []string `json:"groups" gorm:"-"`
	ChannelGroups string   `json:"-" gorm:"column:channel_groups"`
}

type ChannelTestResultSummary struct {
	Tested    int64 `json:"tested"`
	Succeeded int64 `json:"succeeded"`
	Failed    int64 `json:"failed"`
	Cancelled int64 `json:"cancelled"`
}

func CreateChannelTestResult(result *ChannelTestResult) error {
	return DB.Create(result).Error
}

func GetChannelTestResultByID(id int64) (*ChannelTestResult, error) {
	result := &ChannelTestResult{}
	err := DB.First(result, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return result, nil
}

func ListChannelTestFilterOptions() ([]ChannelTestFilterOption, error) {
	options := make([]ChannelTestFilterOption, 0)
	if err := DB.Model(&Channel{}).
		Select("id, name, status, " + commonGroupCol + " AS channel_groups").
		Order("id DESC").
		Scan(&options).Error; err != nil {
		return nil, err
	}
	for i := range options {
		channel := Channel{Group: options[i].ChannelGroups}
		options[i].Groups = channel.GetGroups()
	}
	return options, nil
}

func ListChannelTestResults(filter ChannelTestResultFilter) ([]ChannelTestResult, int64, ChannelTestResultSummary, error) {
	items := make([]ChannelTestResult, 0)
	var total int64
	countQuery := applyChannelTestResultFilter(DB.Model(&ChannelTestResult{}), filter, false)
	if err := countQuery.Count(&total).Error; err != nil {
		return nil, 0, ChannelTestResultSummary{}, err
	}
	if filter.Limit > 0 && total > 0 {
		pageQuery := applyChannelTestResultFilter(DB.Model(&ChannelTestResult{}), filter, false)
		if err := pageQuery.Order("created_at DESC").Order("id DESC").Offset(filter.Offset).Limit(filter.Limit).Find(&items).Error; err != nil {
			return nil, 0, ChannelTestResultSummary{}, err
		}
	}

	summary := ChannelTestResultSummary{}
	var rows []struct {
		Status string
		Count  int64
	}
	summaryQuery := applyChannelTestResultFilter(DB.Model(&ChannelTestResult{}), filter, true)
	if err := summaryQuery.Select("status, COUNT(*) AS count").Group("status").Scan(&rows).Error; err != nil {
		return nil, 0, ChannelTestResultSummary{}, err
	}
	for _, row := range rows {
		summary.Tested += row.Count
		switch row.Status {
		case "succeeded":
			summary.Succeeded = row.Count
		case "failed":
			summary.Failed = row.Count
		case "cancelled":
			summary.Cancelled = row.Count
		}
	}
	return items, total, summary, nil
}

func applyChannelTestResultFilter(query *gorm.DB, filter ChannelTestResultFilter, ignoreStatus bool) *gorm.DB {
	if filter.ChannelID > 0 {
		query = query.Where("channel_id = ?", filter.ChannelID)
	}
	query = applyChannelGroupFilter(query, filter.Group, "channel_groups")
	if value := strings.TrimSpace(filter.RunID); value != "" {
		query = query.Where("run_id = ?", value)
	}
	if value := strings.TrimSpace(filter.TaskID); value != "" {
		query = query.Where("task_id = ?", value)
	}
	if value := strings.TrimSpace(filter.Source); value != "" {
		query = query.Where("source = ?", value)
	}
	if !ignoreStatus {
		if value := strings.TrimSpace(filter.Status); value != "" {
			query = query.Where("status = ?", value)
		}
	}
	if value := strings.TrimSpace(filter.ModelName); value != "" {
		query = query.Where("model_name = ?", value)
	}
	if filter.StartAt > 0 {
		query = query.Where("created_at >= ?", filter.StartAt)
	}
	if filter.EndAt > 0 {
		query = query.Where("created_at <= ?", filter.EndAt)
	}
	return query
}

func DeleteChannelTestResultsBefore(cutoff int64, limit int) (int64, error) {
	if cutoff <= 0 || limit <= 0 {
		return 0, nil
	}
	ids := make([]int64, 0, limit)
	if err := DB.Model(&ChannelTestResult{}).Where("created_at < ?", cutoff).Order("created_at ASC").Order("id ASC").Limit(limit).Pluck("id", &ids).Error; err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	result := DB.Where("id IN ?", ids).Delete(&ChannelTestResult{})
	return result.RowsAffected, result.Error
}
