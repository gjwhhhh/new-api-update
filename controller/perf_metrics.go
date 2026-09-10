package controller

import (
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/perf_metrics_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
)

const (
	perfGroupsSortCustom  = "custom"
	perfGroupsSortTraffic = "traffic"
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
	hours := perfmetrics.GroupHours48
	if rawHours, exists := c.GetQuery("hours"); exists {
		parsed, err := strconv.Atoi(rawHours)
		if err != nil || perfmetrics.ValidateGroupHours(parsed) != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"message": perfmetrics.ErrUnsupportedGroupHours.Error(),
			})
			return
		}
		hours = parsed
	}
	sortMode := normalizePerfGroupsSort(c.Query("sort"))

	userGroup, _ := model.GetUserGroup(c.GetInt("id"), false)
	usable := service.GetUserUsableGroups(userGroup)
	groupNames := make([]string, 0, len(usable))
	for name := range usable {
		groupNames = append(groupNames, name)
	}

	isAdmin := c.GetInt("role") >= common.RoleAdminUser
	if !isAdmin {
		groupNames = lo.Filter(groupNames, func(name string, _ int) bool {
			return !perf_metrics_setting.IsGroupHidden(name)
		})
	}

	metrics, err := perfmetrics.QueryGroups(hours, groupNames)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	metrics.Groups = sortPerfMetricGroups(metrics.Groups, sortMode)

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
		if isAdmin {
			item["visible_to_users"] = !perf_metrics_setting.IsGroupHidden(group.Group)
		}
		items = append(items, item)
	}

	data := gin.H{
		"bucket_seconds": metrics.BucketSeconds,
		"start_ts":       metrics.StartTs,
		"end_ts":         metrics.EndTs,
		"sort":           sortMode,
		"groups":         items,
	}
	if isAdmin {
		data["display_order"] = perf_metrics_setting.GetGroupDisplayOrder()
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    data,
	})
}

func normalizePerfGroupsSort(raw string) string {
	if strings.EqualFold(strings.TrimSpace(raw), perfGroupsSortTraffic) {
		return perfGroupsSortTraffic
	}
	return perfGroupsSortCustom
}

func sortPerfMetricGroups(groups []perfmetrics.GroupMetric, sortMode string) []perfmetrics.GroupMetric {
	if len(groups) <= 1 {
		return groups
	}
	out := append([]perfmetrics.GroupMetric(nil), groups...)
	if sortMode == perfGroupsSortTraffic {
		sort.SliceStable(out, func(i, j int) bool {
			if out[i].RequestCount == out[j].RequestCount {
				return out[i].Group < out[j].Group
			}
			return out[i].RequestCount > out[j].RequestCount
		})
		return out
	}

	order := perf_metrics_setting.GetGroupDisplayOrder()
	index := make(map[string]int, len(order))
	for i, name := range order {
		index[name] = i
	}
	ratioGroups := ratio_setting.GetGroupRatioCopy()

	sort.SliceStable(out, func(i, j int) bool {
		iName, jName := out[i].Group, out[j].Group
		iIdx, iKnown := index[iName]
		jIdx, jKnown := index[jName]
		if iKnown && jKnown {
			return iIdx < jIdx
		}
		if iKnown != jKnown {
			return iKnown
		}
		_, iInRatio := ratioGroups[iName]
		_, jInRatio := ratioGroups[jName]
		if iInRatio != jInRatio {
			return iInRatio
		}
		return iName < jName
	})
	return out
}

type clearPerfMetricGroupRequest struct {
	Group string `json:"group"`
	Hours int    `json:"hours"`
}

func ClearPerfMetricGroupSamples(c *gin.Context) {
	var req clearPerfMetricGroupRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "invalid request body",
		})
		return
	}
	req.Group = strings.TrimSpace(req.Group)
	if req.Group == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "group is required",
		})
		return
	}

	result, err := perfmetrics.ClearGroupRecent(req.Group, req.Hours)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "ok",
		"data":    result,
	})
}

type updatePerfMetricGroupVisibilityRequest struct {
	Group          string `json:"group"`
	VisibleToUsers bool   `json:"visible_to_users"`
}

func UpdatePerfMetricGroupVisibility(c *gin.Context) {
	var req updatePerfMetricGroupVisibilityRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "invalid request body",
		})
		return
	}
	req.Group = strings.TrimSpace(req.Group)
	if req.Group == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "group is required",
		})
		return
	}

	hidden := make([]string, 0)
	seen := map[string]struct{}{}
	for _, name := range perf_metrics_setting.GetSetting().HiddenGroups {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if name == req.Group {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		hidden = append(hidden, name)
	}
	if !req.VisibleToUsers {
		if _, ok := seen[req.Group]; !ok {
			hidden = append(hidden, req.Group)
		}
	}

	encoded, err := common.Marshal(hidden)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	if err := model.UpdateOption("perf_metrics_setting.hidden_groups", string(encoded)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "ok",
		"data": gin.H{
			"group":            req.Group,
			"visible_to_users": req.VisibleToUsers,
		},
	})
}

type updatePerfMetricGroupDisplayOrderRequest struct {
	Groups []string `json:"groups"`
}

func UpdatePerfMetricGroupDisplayOrder(c *gin.Context) {
	var req updatePerfMetricGroupDisplayOrderRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "invalid request body",
		})
		return
	}
	order := perf_metrics_setting.NormalizeGroupDisplayOrder(req.Groups)
	encoded, err := common.Marshal(order)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	if err := model.UpdateOption("perf_metrics_setting.group_display_order", string(encoded)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "ok",
		"data": gin.H{
			"groups": order,
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
