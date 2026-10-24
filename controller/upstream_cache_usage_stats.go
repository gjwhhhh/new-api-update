package controller

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func parseUpstreamCacheWindows(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}

func parseUpstreamCacheChannelIDs(raw string) ([]int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	out := make([]int, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, err := strconv.Atoi(part)
		if err != nil {
			return nil, err
		}
		if id <= 0 {
			return nil, errors.New("channel id must be positive")
		}
		out = append(out, id)
	}
	return out, nil
}

func parseUpstreamCacheGroups(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}

// GetUpstreamCacheUsageStats returns 1h/24h upstream Prompt/KV cache stats for one key.
func GetUpstreamCacheUsageStats(c *gin.Context) {
	scope := strings.TrimSpace(c.Query("scope"))
	group := strings.TrimSpace(c.Query("group"))
	windows := parseUpstreamCacheWindows(c.Query("windows"))
	if len(windows) == 0 {
		windows = parseUpstreamCacheWindows(c.Query("window"))
	}

	channelID := 0
	if raw := strings.TrimSpace(c.Query("channel_id")); raw != "" {
		id, err := strconv.Atoi(raw)
		if err != nil || id <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"message": "invalid channel_id",
			})
			return
		}
		channelID = id
	}

	switch scope {
	case "group":
		if group == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"message": "missing param: group",
			})
			return
		}
	case "channel":
		if channelID <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"message": "missing param: channel_id",
			})
			return
		}
	case "group_channel":
		if group == "" || channelID <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"message": "missing param: group and channel_id",
			})
			return
		}
	default:
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "invalid scope: use group, channel, or group_channel",
		})
		return
	}

	stats := service.GetUpstreamCacheUsageStats(scope, group, channelID, windows)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    stats,
	})
}

// BatchGetUpstreamCacheUsageStats returns stats for many groups or channels in one call.
func BatchGetUpstreamCacheUsageStats(c *gin.Context) {
	scope := strings.TrimSpace(c.Query("scope"))
	windows := parseUpstreamCacheWindows(c.Query("windows"))
	if len(windows) == 0 {
		windows = parseUpstreamCacheWindows(c.Query("window"))
	}

	switch scope {
	case "channel":
		ids, err := parseUpstreamCacheChannelIDs(c.Query("ids"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"message": "invalid ids",
			})
			return
		}
		if len(ids) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"message": "missing param: ids",
			})
			return
		}
		data, err := service.BatchGetUpstreamCacheUsageStatsByChannels(ids, windows)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "",
			"data":    data,
		})
	case "group":
		groups := parseUpstreamCacheGroups(c.Query("groups"))
		if len(groups) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"message": "missing param: groups",
			})
			return
		}
		data, err := service.BatchGetUpstreamCacheUsageStatsByGroups(groups, windows)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "",
			"data":    data,
		})
	default:
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "invalid scope: use group or channel",
		})
	}
}

type clearUpstreamCacheUsageStatsRequest struct {
	Window    string `json:"window"`
	Scope     string `json:"scope"`
	Group     string `json:"group"`
	ChannelID int    `json:"channel_id"`
	All       bool   `json:"all"`
}

// ClearUpstreamCacheUsageStats clears upstream cache hit counters.
// Full purge requires all=true; otherwise scope and key are required.
func ClearUpstreamCacheUsageStats(c *gin.Context) {
	var req clearUpstreamCacheUsageStatsRequest
	_ = c.ShouldBindJSON(&req)

	if strings.TrimSpace(req.Window) == "" {
		req.Window = strings.TrimSpace(c.Query("window"))
	}
	if strings.TrimSpace(req.Scope) == "" {
		req.Scope = strings.TrimSpace(c.Query("scope"))
	}
	if strings.TrimSpace(req.Group) == "" {
		req.Group = strings.TrimSpace(c.Query("group"))
	}
	if !req.All {
		req.All = strings.EqualFold(strings.TrimSpace(c.Query("all")), "true")
	}
	if req.ChannelID <= 0 {
		if raw := strings.TrimSpace(c.Query("channel_id")); raw != "" {
			id, err := strconv.Atoi(raw)
			if err != nil || id <= 0 {
				c.JSON(http.StatusBadRequest, gin.H{
					"success": false,
					"message": "invalid channel_id",
				})
				return
			}
			req.ChannelID = id
		}
	}

	window := strings.TrimSpace(req.Window)
	if req.All && window == "" {
		window = "all"
	}
	scope := strings.TrimSpace(req.Scope)

	cleared, err := service.ClearUpstreamCacheUsageStats(window, req.All, scope, req.Group, req.ChannelID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"cleared": cleared,
			"window":  window,
			"scope":   scope,
			"all":     req.All,
		},
	})
}
