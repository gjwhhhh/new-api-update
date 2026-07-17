package controller

import (
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

type SubscriptionAccessGroupRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Enabled     *bool  `json:"enabled"`
}

type SubscriptionAccessGroupMembershipRequest struct {
	AccessGroupIds []int `json:"access_group_ids"`
}

func requireSubscriptionPlanAccess(c *gin.Context, userId int, planId int) bool {
	allowed, err := model.CanUserAccessSubscriptionPlan(userId, planId)
	if err != nil {
		common.ApiError(c, err)
		return false
	}
	if !allowed {
		common.ApiErrorMsg(c, "当前用户无权购买该套餐")
		return false
	}
	return true
}

func AdminListSubscriptionAccessGroups(c *gin.Context) {
	groups, err := model.ListSubscriptionAccessGroups()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, groups)
}

func AdminCreateSubscriptionAccessGroup(c *gin.Context) {
	if !requirePaymentCompliance(c) {
		return
	}
	var req SubscriptionAccessGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		common.ApiErrorMsg(c, "套餐分组名称不能为空")
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	group := &model.SubscriptionAccessGroup{
		Name:        req.Name,
		Description: req.Description,
		Enabled:     enabled,
	}
	if err := model.CreateSubscriptionAccessGroup(group); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, group)
}

func AdminUpdateSubscriptionAccessGroup(c *gin.Context) {
	if !requirePaymentCompliance(c) {
		return
	}
	id, _ := strconv.Atoi(c.Param("id"))
	if id <= 0 {
		common.ApiErrorMsg(c, "无效的套餐分组ID")
		return
	}
	var req SubscriptionAccessGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	if strings.TrimSpace(req.Name) == "" || req.Enabled == nil {
		common.ApiErrorMsg(c, "套餐分组名称和状态不能为空")
		return
	}
	group := &model.SubscriptionAccessGroup{
		Id:          id,
		Name:        req.Name,
		Description: req.Description,
		Enabled:     *req.Enabled,
	}
	if err := model.UpdateSubscriptionAccessGroup(group); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, group)
}

func AdminDeleteSubscriptionAccessGroup(c *gin.Context) {
	if !requirePaymentCompliance(c) {
		return
	}
	id, _ := strconv.Atoi(c.Param("id"))
	if id <= 0 {
		common.ApiErrorMsg(c, "无效的套餐分组ID")
		return
	}
	if err := model.DeleteSubscriptionAccessGroup(id); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

func AdminGetUserSubscriptionAccessGroups(c *gin.Context) {
	userId, _ := strconv.Atoi(c.Param("id"))
	if userId <= 0 {
		common.ApiErrorMsg(c, "无效的用户ID")
		return
	}
	ids, err := model.GetUserSubscriptionAccessGroupIDs(userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"access_group_ids": ids})
}

func AdminSetUserSubscriptionAccessGroups(c *gin.Context) {
	if !requirePaymentCompliance(c) {
		return
	}
	userId, _ := strconv.Atoi(c.Param("id"))
	if userId <= 0 {
		common.ApiErrorMsg(c, "无效的用户ID")
		return
	}
	var req SubscriptionAccessGroupMembershipRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	if err := model.SetUserSubscriptionAccessGroups(userId, req.AccessGroupIds); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}
