package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
)

func GetGroups(c *gin.Context) {
	groupNames := make([]string, 0)
	for groupName := range ratio_setting.GetGroupRatioCopy() {
		groupNames = append(groupNames, groupName)
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    groupNames,
	})
}

func GetUserGroups(c *gin.Context) {
	usableGroups := make(map[string]map[string]interface{})
	userGroup := ""
	userId := c.GetInt("id")
	userGroup, _ = model.GetUserGroup(userId, false)
	userUsableGroups := service.GetUserUsableGroups(userGroup)
	for groupName, _ := range ratio_setting.GetGroupRatioCopy() {
		// UserUsableGroups contains the groups that the user can use
		if desc, ok := userUsableGroups[groupName]; ok {
			usableGroups[groupName] = map[string]interface{}{
				"ratio": service.GetUserGroupRatio(userGroup, groupName),
				"desc":  desc,
			}
		}
	}
	if _, ok := userUsableGroups["auto"]; ok {
		usableGroups["auto"] = map[string]interface{}{
			"ratio": "自动",
			"desc":  setting.GetUsableGroupDescription("auto"),
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    usableGroups,
	})
}

func GetGroupConfig(c *gin.Context) {
	config, err := service.CurrentGroupConfig()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	revision, err := service.GroupConfigRevision(config)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"config": config, "revision": revision})
}

func UpdateGroupConfig(c *gin.Context) {
	var request dto.GroupConfigUpdateRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorMsg(c, "无效的分组配置")
		return
	}
	revision, err := service.UpdateGroupConfig(request)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "group.config_update", nil)
	common.ApiSuccess(c, gin.H{"revision": revision})
}

func PreviewGroupRename(c *gin.Context) {
	var request dto.GroupRenamePreviewRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorMsg(c, "无效的分组重命名参数")
		return
	}
	preview, err := service.PreviewGroupRename(request)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, preview)
}

func RenameGroup(c *gin.Context) {
	var request dto.GroupRenameRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorMsg(c, "无效的分组重命名参数")
		return
	}
	result, err := service.RenameGroup(request, c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "group.rename", map[string]interface{}{
		"old_name": request.OldName,
		"new_name": request.NewName,
		"users":    result.Affected.Users,
		"tokens":   result.Affected.Tokens,
		"channels": result.Affected.Channels,
	})
	common.ApiSuccess(c, result)
}

func GetUserGroupAliases(c *gin.Context) {
	userGroup, err := model.GetUserGroup(c.GetInt("id"), false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	aliases, err := service.GetUserGroupAliases(userGroup)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, aliases)
}
