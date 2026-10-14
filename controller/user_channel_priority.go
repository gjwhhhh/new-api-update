package controller

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

type patchUserChannelPrioritiesRequest struct {
	Upserts []patchUserChannelPriorityItem `json:"upserts"`
	Deletes []int                          `json:"deletes"`
}

type patchUserChannelPriorityItem struct {
	ChannelId int   `json:"channel_id"`
	Priority  int64 `json:"priority"`
}

func GetUserChannelPriorities(c *gin.Context) {
	userId, ok := userIdParam(c)
	if !ok {
		return
	}
	if _, err := model.GetUserById(userId, false); err != nil {
		common.ApiError(c, err)
		return
	}
	items, err := model.ListUserChannelPriorities(userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"items": items})
}

func GetUserChannelPriorityOptions(c *gin.Context) {
	userId, ok := userIdParam(c)
	if !ok {
		return
	}
	if _, err := model.GetUserById(userId, false); err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo := common.GetPageQuery(c)
	items, total, err := model.SearchUserChannelPriorityOptions(c.Query("keyword"), pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{
		"items":     items,
		"total":     total,
		"page":      pageInfo.GetPage(),
		"page_size": pageInfo.GetPageSize(),
	})
}

func PatchUserChannelPriorities(c *gin.Context) {
	userId, ok := userIdParam(c)
	if !ok {
		return
	}
	var request patchUserChannelPrioritiesRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		common.ApiError(c, fmt.Errorf("invalid request: %w", err))
		return
	}
	if len(request.Upserts)+len(request.Deletes) > model.MaxUserChannelPriorities {
		common.ApiErrorMsg(c, fmt.Sprintf("a request can change at most %d channel priorities", model.MaxUserChannelPriorities))
		return
	}

	seen := make(map[int]struct{}, len(request.Upserts)+len(request.Deletes))
	upserts := make([]model.UserChannelPriority, 0, len(request.Upserts))
	for _, item := range request.Upserts {
		if item.ChannelId <= 0 {
			common.ApiErrorMsg(c, "channel_id must be positive")
			return
		}
		if item.Priority < model.MinUserChannelPriority || item.Priority > model.MaxUserChannelPriority {
			common.ApiErrorMsg(c, fmt.Sprintf("priority must be between %d and %d", model.MinUserChannelPriority, model.MaxUserChannelPriority))
			return
		}
		if _, duplicate := seen[item.ChannelId]; duplicate {
			common.ApiErrorMsg(c, fmt.Sprintf("channel %d appears more than once", item.ChannelId))
			return
		}
		seen[item.ChannelId] = struct{}{}
		upserts = append(upserts, model.UserChannelPriority{ChannelId: item.ChannelId, Priority: item.Priority})
	}
	for _, channelId := range request.Deletes {
		if channelId <= 0 {
			common.ApiErrorMsg(c, "channel_id must be positive")
			return
		}
		if _, duplicate := seen[channelId]; duplicate {
			common.ApiErrorMsg(c, fmt.Sprintf("channel %d appears more than once", channelId))
			return
		}
		seen[channelId] = struct{}{}
	}

	if err := model.PatchUserChannelPriorities(userId, upserts, request.Deletes); err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAuditFor(c, userId, "user.channel_priorities.update", map[string]any{
		"target_user_id": userId,
		"upserts":        len(upserts),
		"deletes":        len(request.Deletes),
	})
	items, err := model.ListUserChannelPriorities(userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"items": items})
}

func userIdParam(c *gin.Context) (int, bool) {
	userId, err := strconv.Atoi(c.Param("id"))
	if err != nil || userId <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid user id"})
		return 0, false
	}
	return userId, true
}
