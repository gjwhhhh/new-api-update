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
	perfGroupsSortCustom          = "custom"
	perfGroupsSortTraffic         = "traffic"
	channelMetricsSortID          = "id"
	channelMetricsSortTraffic     = "traffic"
	channelMetricsSortSuccessRate = "success_rate"
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

type channelStatusMetricItem struct {
	ChannelID     int                            `json:"channel_id"`
	ChannelName   string                         `json:"channel_name"`
	ChannelType   int                            `json:"channel_type"`
	ChannelStatus int                            `json:"channel_status"`
	Groups        []string                       `json:"groups"`
	Health        string                         `json:"health"`
	RequestCount  int64                          `json:"request_count"`
	SuccessCount  int64                          `json:"success_count"`
	SuccessRate   float64                        `json:"success_rate"`
	AvgTtftMs     int64                          `json:"avg_ttft_ms"`
	AvgLatencyMs  int64                          `json:"avg_latency_ms"`
	AvgTps        float64                        `json:"avg_tps"`
	Series        []perfmetrics.GroupBucketPoint `json:"series"`
}

func GetPerfMetricsChannels(c *gin.Context) {
	hours, ok := getPerfMetricGroupHours(c)
	if !ok {
		return
	}

	search := strings.ToLower(strings.TrimSpace(c.Query("search")))
	groupFilter := model.NormalizeChannelGroupFilter(c.Query("group"))
	channelStatus := strings.ToLower(strings.TrimSpace(c.Query("channel_status")))
	if !isValidChannelStatusFilter(channelStatus) {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "invalid channel_status",
		})
		return
	}
	var configuredStatus *int
	switch channelStatus {
	case "enabled":
		value := common.ChannelStatusEnabled
		configuredStatus = &value
	case "auto_disabled":
		value := common.ChannelStatusAutoDisabled
		configuredStatus = &value
	case "manually_disabled":
		value := common.ChannelStatusManuallyDisabled
		configuredStatus = &value
	}

	var channelType *int
	if rawType := strings.TrimSpace(c.Query("type")); rawType != "" {
		parsed, parseErr := strconv.Atoi(rawType)
		if parseErr != nil || parsed < 0 {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"message": "invalid type",
			})
			return
		}
		channelType = &parsed
	}

	healthFilter := strings.ToLower(strings.TrimSpace(c.Query("health")))
	if !isValidChannelHealthFilter(healthFilter) {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "invalid health",
		})
		return
	}
	pageInfo := common.GetPageQuery(c)
	if pageInfo.PageSize < 1 {
		pageInfo.PageSize = 24
	}
	sortBy := normalizeChannelMetricsSort(c.Query("sort"))
	window, err := perfmetrics.GetChannelMetricWindow(hours)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	page, err := model.GetChannelStatusMetricPage(model.ChannelStatusMetricPageParams{
		StartTs:       window.StartTs,
		EndTs:         window.EndTs,
		Search:        search,
		Group:         groupFilter,
		ChannelStatus: configuredStatus,
		ChannelType:   channelType,
		Health:        healthFilter,
		Sort:          sortBy,
		Desc:          !channelMetricsAscending(sortBy, c.Query("order")),
		Offset:        pageInfo.GetStartIdx(),
		Limit:         pageInfo.GetPageSize(),
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	channelIDs := make([]int, 0, len(page.Items))
	for _, item := range page.Items {
		channelIDs = append(channelIDs, item.ChannelID)
	}
	metrics := perfmetrics.ChannelsQueryResult{
		BucketSeconds: window.BucketSeconds,
		StartTs:       window.StartTs,
		EndTs:         window.EndTs,
		Channels:      []perfmetrics.ChannelMetric{},
	}
	if len(channelIDs) > 0 {
		metrics, err = perfmetrics.QueryChannelsInWindow(window, channelIDs)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
	}
	metricsByChannel := make(map[int]perfmetrics.ChannelMetric, len(metrics.Channels))
	for _, metric := range metrics.Channels {
		metricsByChannel[metric.ChannelID] = metric
	}

	healthCounts := map[string]int{
		perfmetrics.ChannelHealthRunning:     0,
		perfmetrics.ChannelHealthFluctuating: 0,
		perfmetrics.ChannelHealthAbnormal:    0,
		perfmetrics.ChannelHealthNoData:      0,
	}
	for health, count := range page.HealthCounts {
		healthCounts[health] = int(count)
	}
	items := make([]channelStatusMetricItem, 0, len(page.Items))
	for _, summary := range page.Items {
		metric := metricsByChannel[summary.ChannelID]
		health := perfmetrics.ChannelHealth(metric.RequestCount, metric.SuccessRate)
		items = append(items, newChannelStatusMetricItem(&model.Channel{
			Id:     summary.ChannelID,
			Name:   summary.ChannelName,
			Type:   summary.ChannelType,
			Status: summary.ChannelStatus,
			Group:  summary.ChannelGroup,
		}, metric, health))
	}
	providerTypeCounts, err := model.CountChannelsGroupByType()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	providerTypes := make([]int, 0, len(providerTypeCounts))
	for providerType := range providerTypeCounts {
		if providerType >= 0 {
			providerTypes = append(providerTypes, int(providerType))
		}
	}
	sort.Ints(providerTypes)

	common.ApiSuccess(c, gin.H{
		"items":          items,
		"total":          page.Total,
		"page":           pageInfo.GetPage(),
		"page_size":      pageInfo.GetPageSize(),
		"health_counts":  healthCounts,
		"bucket_seconds": metrics.BucketSeconds,
		"start_ts":       metrics.StartTs,
		"end_ts":         metrics.EndTs,
		"provider_types": providerTypes,
	})
}

func GetPerfMetricsChannelDetail(c *gin.Context) {
	channelID, err := strconv.Atoi(c.Param("id"))
	if err != nil || channelID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "invalid channel id",
		})
		return
	}
	hours, ok := getPerfMetricGroupHours(c)
	if !ok {
		return
	}
	channel, err := model.GetChannelStatusByID(channelID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"message": "channel not found",
		})
		return
	}
	metrics, err := perfmetrics.QueryChannels(hours, []int{channelID})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	metric := perfmetrics.ChannelMetric{ChannelID: channelID, Series: []perfmetrics.GroupBucketPoint{}, Models: []perfmetrics.GroupModelStat{}}
	if len(metrics.Channels) > 0 {
		metric = metrics.Channels[0]
	}
	health := perfmetrics.ChannelHealth(metric.RequestCount, metric.SuccessRate)
	item := newChannelStatusMetricItem(channel, metric, health)
	common.ApiSuccess(c, gin.H{
		"channel":        item,
		"models":         metric.Models,
		"bucket_seconds": metrics.BucketSeconds,
		"start_ts":       metrics.StartTs,
		"end_ts":         metrics.EndTs,
	})
}

func getPerfMetricGroupHours(c *gin.Context) (int, bool) {
	hours := perfmetrics.GroupHours48
	if rawHours, exists := c.GetQuery("hours"); exists {
		parsed, err := strconv.Atoi(rawHours)
		if err != nil || perfmetrics.ValidateGroupHours(parsed) != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"message": perfmetrics.ErrUnsupportedGroupHours.Error(),
			})
			return 0, false
		}
		hours = parsed
	}
	return hours, true
}

func isValidChannelStatusFilter(status string) bool {
	return status == "" || status == "enabled" || status == "auto_disabled" || status == "manually_disabled"
}

func isValidChannelHealthFilter(health string) bool {
	switch health {
	case "", perfmetrics.ChannelHealthRunning, perfmetrics.ChannelHealthFluctuating, perfmetrics.ChannelHealthAbnormal, perfmetrics.ChannelHealthNoData:
		return true
	default:
		return false
	}
}

func newChannelStatusMetricItem(channel *model.Channel, metric perfmetrics.ChannelMetric, health string) channelStatusMetricItem {
	return channelStatusMetricItem{
		ChannelID:     channel.Id,
		ChannelName:   channel.Name,
		ChannelType:   channel.Type,
		ChannelStatus: channel.Status,
		Groups:        channel.GetGroups(),
		Health:        health,
		RequestCount:  metric.RequestCount,
		SuccessCount:  metric.SuccessCount,
		SuccessRate:   metric.SuccessRate,
		AvgTtftMs:     metric.AvgTtftMs,
		AvgLatencyMs:  metric.AvgLatencyMs,
		AvgTps:        metric.AvgTps,
		Series:        metric.Series,
	}
}

func normalizeChannelMetricsSort(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case channelMetricsSortTraffic, channelMetricsSortSuccessRate:
		return strings.ToLower(strings.TrimSpace(raw))
	default:
		return channelMetricsSortID
	}
}

func channelMetricsAscending(sortBy string, rawOrder string) bool {
	ascending := sortBy == channelMetricsSortID
	if strings.EqualFold(strings.TrimSpace(rawOrder), "asc") {
		return true
	} else if strings.EqualFold(strings.TrimSpace(rawOrder), "desc") {
		return false
	}
	return ascending
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
	if err := perfmetrics.ValidateGroupHours(req.Hours); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	result, err := perfmetrics.ClearGroupRecent(req.Group, req.Hours)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
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

type clearPerfMetricChannelRequest struct {
	Hours int `json:"hours"`
}

func ClearPerfMetricChannelSamples(c *gin.Context) {
	channelID, err := strconv.Atoi(c.Param("id"))
	if err != nil || channelID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "invalid channel id",
		})
		return
	}

	var req clearPerfMetricChannelRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "invalid request body",
		})
		return
	}
	if err := perfmetrics.ValidateGroupHours(req.Hours); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	if _, err := model.GetChannelStatusByID(channelID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"message": "channel not found",
		})
		return
	}

	result, err := perfmetrics.ClearChannelRecent(channelID, req.Hours)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	common.ApiSuccess(c, result)
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
