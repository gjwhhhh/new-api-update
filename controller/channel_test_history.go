package controller

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type channelTestRunProjection struct {
	TaskID    string                 `json:"task_id"`
	Status    model.SystemTaskStatus `json:"status"`
	Processed int                    `json:"processed"`
	Total     int                    `json:"total"`
	Result    *channelTestSummary    `json:"result,omitempty"`
}

func ListChannelTestHistory(c *gin.Context) {
	page := common.GetPageQuery(c)
	filter, ok := parseChannelTestResultFilter(c)
	if !ok {
		return
	}
	filter.Offset = page.GetStartIdx()
	filter.Limit = page.GetPageSize()
	items, total, summary, err := model.ListChannelTestResults(filter)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var run *channelTestRunProjection
	if filter.TaskID != "" {
		run, err = getChannelTestRunProjection(filter.TaskID)
		if err != nil {
			common.ApiError(c, err)
			return
		}
	}
	common.ApiSuccess(c, gin.H{
		"items":             items,
		"total":             total,
		"summary":           summary,
		"recording_enabled": operation_setting.GetChannelTestHistorySetting().Enabled,
		"run":               run,
	})
}

func GetChannelTestHistory(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid channel test result id"})
		return
	}
	result, err := model.GetChannelTestResultByID(id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "channel test result not found"})
		return
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}

func parseChannelTestResultFilter(c *gin.Context) (model.ChannelTestResultFilter, bool) {
	filter := model.ChannelTestResultFilter{
		RunID:     strings.TrimSpace(c.Query("run_id")),
		TaskID:    strings.TrimSpace(c.Query("task_id")),
		Source:    strings.TrimSpace(c.Query("source")),
		Status:    strings.TrimSpace(c.Query("status")),
		ModelName: strings.TrimSpace(c.Query("model_name")),
	}
	if raw := strings.TrimSpace(c.Query("channel_id")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid channel_id"})
			return filter, false
		}
		filter.ChannelID = value
	}
	for _, pair := range []struct {
		raw    string
		target *int64
		name   string
	}{
		{c.Query("start_at"), &filter.StartAt, "start_at"},
		{c.Query("end_at"), &filter.EndAt, "end_at"},
	} {
		if strings.TrimSpace(pair.raw) == "" {
			continue
		}
		value, err := strconv.ParseInt(pair.raw, 10, 64)
		if err != nil || value <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid " + pair.name})
			return filter, false
		}
		*pair.target = value
	}
	if filter.EndAt > 0 && filter.StartAt > filter.EndAt {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid time range"})
		return filter, false
	}
	if filter.Status != "" && filter.Status != service.ChannelTestStatusSucceeded && filter.Status != service.ChannelTestStatusFailed && filter.Status != service.ChannelTestStatusCancelled {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid status"})
		return filter, false
	}
	if filter.Source != "" && filter.Source != "scheduled" && filter.Source != "manual_batch" && filter.Source != "manual_single" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid source"})
		return filter, false
	}
	return filter, true
}

func getChannelTestRunProjection(taskID string) (*channelTestRunProjection, error) {
	task, err := model.GetSystemTaskByTaskID(taskID)
	if err != nil || task == nil || task.Type != model.SystemTaskTypeChannelTest {
		return nil, err
	}
	state := service.SystemTaskProgress{}
	if err := task.DecodeState(&state); err != nil {
		return nil, err
	}
	projection := &channelTestRunProjection{
		TaskID:    task.TaskID,
		Status:    task.Status,
		Processed: state.Processed,
		Total:     state.Total,
	}
	if task.Status == model.SystemTaskStatusSucceeded {
		result := channelTestSummary{}
		if err := common.UnmarshalJsonStr(task.Result, &result); err != nil {
			return nil, err
		}
		projection.Result = &result
	}
	return projection, nil
}
