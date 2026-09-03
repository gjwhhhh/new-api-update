package controller

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/model"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
)

func GetPerfMetricsSummary(c *gin.Context) {
	hours := 24
	if rawHours := c.Query("hours"); rawHours != "" {
		if parsed, err := strconv.Atoi(rawHours); err == nil {
			hours = parsed
		}
	}

	activeGroups := append(lo.Keys(ratio_setting.GetGroupRatioCopy()), "auto")
	result, err := perfmetrics.QuerySummaryAll(hours, activeGroups)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    result,
	})
}

func GetPerfMetricsGroups(c *gin.Context) {
	hours := 24
	if rawHours := c.Query("hours"); rawHours != "" {
		if parsed, err := strconv.Atoi(rawHours); err == nil {
			hours = parsed
		}
	}

	userGroup, _ := model.GetUserGroup(c.GetInt("id"), false)
	usable := service.GetUserUsableGroups(userGroup)
	groupNames := make([]string, 0, len(usable))
	for name := range usable {
		groupNames = append(groupNames, name)
	}

	metrics, err := perfmetrics.QueryGroups(hours, groupNames)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	items := make([]gin.H, 0, len(metrics.Groups))
	for _, group := range metrics.Groups {
		desc := usable[group.Group]
		if desc == "" {
			desc = setting.GetUsableGroupDescription(group.Group)
		}
		item := gin.H{
			"group":          group.Group,
			"name":           group.Group,
			"description":    desc,
			"request_count":  group.RequestCount,
			"success_count":  group.SuccessCount,
			"success_rate":   group.SuccessRate,
			"avg_ttft_ms":    group.AvgTtftMs,
			"avg_latency_ms": group.AvgLatencyMs,
			"avg_tps":        group.AvgTps,
			"series":         group.Series,
			"models":         group.Models,
		}
		if group.Group == "auto" {
			item["ratio"] = "auto"
		} else {
			item["ratio"] = service.GetUserGroupRatio(userGroup, group.Group)
		}
		items = append(items, item)
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"bucket_seconds": metrics.BucketSeconds,
			"start_ts":       metrics.StartTs,
			"end_ts":         metrics.EndTs,
			"groups":         items,
		},
	})
}

func GetPerfMetrics(c *gin.Context) {
	modelName := c.Query("model")
	if modelName == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "model is required",
		})
		return
	}

	hours := 24
	if rawHours := c.Query("hours"); rawHours != "" {
		if parsed, err := strconv.Atoi(rawHours); err == nil {
			hours = parsed
		}
	}

	result, err := perfmetrics.Query(perfmetrics.QueryParams{
		Model: modelName,
		Group: c.Query("group"),
		Hours: hours,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	result.Groups = filterActiveGroups(result.Groups)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    result,
	})
}

func filterActiveGroups(groups []perfmetrics.GroupResult) []perfmetrics.GroupResult {
	activeRatios := ratio_setting.GetGroupRatioCopy()
	return lo.Filter(groups, func(g perfmetrics.GroupResult, _ int) bool {
		_, ok := activeRatios[g.Group]
		return ok || g.Group == "auto"
	})
}
