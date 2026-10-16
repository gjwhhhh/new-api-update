package controller

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type userSessionAffinityResponse struct {
	model.UserSessionAffinityDetail
	GlobalEnabled bool `json:"global_enabled"`
}

func GetUserSessionAffinity(c *gin.Context) {
	userId, ok := userIdParam(c)
	if !ok {
		return
	}
	detail, err := model.GetUserSessionAffinity(userId)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"success": false, "code": "USER_NOT_FOUND"})
			return
		}
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, userSessionAffinityResponse{detail, operation_setting.GetChannelAffinitySetting().Enabled})
}

func SearchUserSessionAffinities(c *gin.Context) {
	page := common.GetPageQuery(c)
	rows, total, err := model.SearchUserSessionAffinities(c.Query("keyword"), c.Query("disabled_only") != "false", page.GetStartIdx(), page.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"items": rows, "total": total, "page": page.GetPage(), "page_size": page.GetPageSize()})
}

func UpdateUserSessionAffinity(c *gin.Context) {
	userId, ok := userIdParam(c)
	if !ok {
		return
	}
	var request struct {
		Disabled *bool  `json:"disabled"`
		Version  *int64 `json:"version"`
	}
	if err := c.ShouldBindJSON(&request); err != nil || request.Disabled == nil || request.Version == nil || *request.Version < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "code": "INVALID_USER_SESSION_AFFINITY"})
		return
	}
	detail, previous, err := model.UpdateUserSessionAffinity(userId, *request.Disabled, *request.Version)
	if err != nil {
		switch {
		case errors.Is(err, model.ErrUserSessionAffinityConflict):
			c.JSON(http.StatusConflict, gin.H{"success": false, "code": "USER_SESSION_AFFINITY_CONFLICT"})
		case errors.Is(err, gorm.ErrRecordNotFound):
			c.JSON(http.StatusNotFound, gin.H{"success": false, "code": "USER_NOT_FOUND"})
		default:
			common.ApiError(c, err)
		}
		return
	}
	if previous != detail.Disabled {
		recordManageAuditFor(c, userId, "user.session_affinity.update", map[string]any{
			"target_user_id": userId, "previous_disabled": previous, "disabled": detail.Disabled,
		})
	}
	common.ApiSuccess(c, userSessionAffinityResponse{detail, operation_setting.GetChannelAffinitySetting().Enabled})
}
