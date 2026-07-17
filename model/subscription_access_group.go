package model

import (
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// SubscriptionAccessGroup controls which users can discover and purchase a subscription plan.
// It is intentionally independent from the user's model-routing group.
type SubscriptionAccessGroup struct {
	Id          int    `json:"id"`
	Name        string `json:"name" gorm:"type:varchar(64);not null;uniqueIndex"`
	Description string `json:"description" gorm:"type:varchar(255);default:''"`
	Enabled     bool   `json:"enabled" gorm:"not null"`
	CreatedAt   int64  `json:"created_at" gorm:"bigint"`
	UpdatedAt   int64  `json:"updated_at" gorm:"bigint"`
}

func (g *SubscriptionAccessGroup) BeforeCreate(tx *gorm.DB) error {
	now := common.GetTimestamp()
	g.CreatedAt = now
	g.UpdatedAt = now
	return nil
}

func (g *SubscriptionAccessGroup) BeforeUpdate(tx *gorm.DB) error {
	g.UpdatedAt = common.GetTimestamp()
	return nil
}

type SubscriptionAccessGroupUser struct {
	Id                        int `json:"id"`
	SubscriptionAccessGroupId int `json:"subscription_access_group_id" gorm:"not null;uniqueIndex:idx_subscription_access_group_user,priority:1;index"`
	UserId                    int `json:"user_id" gorm:"not null;uniqueIndex:idx_subscription_access_group_user,priority:2;index"`
}

type SubscriptionPlanAccessGroup struct {
	Id                        int `json:"id"`
	SubscriptionAccessGroupId int `json:"subscription_access_group_id" gorm:"not null;uniqueIndex:idx_subscription_plan_access_group,priority:1;index"`
	PlanId                    int `json:"plan_id" gorm:"not null;uniqueIndex:idx_subscription_plan_access_group,priority:2;index"`
}

func normalizeSubscriptionAccessGroupIDs(ids []int) []int {
	seen := make(map[int]struct{}, len(ids))
	result := make([]int, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result
}

func validateSubscriptionAccessGroupIDsTx(tx *gorm.DB, ids []int) error {
	if len(ids) == 0 {
		return nil
	}
	var count int64
	if err := tx.Model(&SubscriptionAccessGroup{}).Where("id IN ?", ids).Count(&count).Error; err != nil {
		return err
	}
	if count != int64(len(ids)) {
		return errors.New("subscription access group does not exist")
	}
	return nil
}

func ListSubscriptionAccessGroups() ([]SubscriptionAccessGroup, error) {
	var groups []SubscriptionAccessGroup
	if err := DB.Order("name asc, id asc").Find(&groups).Error; err != nil {
		return nil, err
	}
	return groups, nil
}

func CreateSubscriptionAccessGroup(group *SubscriptionAccessGroup) error {
	if group == nil {
		return errors.New("subscription access group is nil")
	}
	group.Name = strings.TrimSpace(group.Name)
	group.Description = strings.TrimSpace(group.Description)
	if group.Name == "" {
		return errors.New("subscription access group name is empty")
	}
	return DB.Create(group).Error
}

func UpdateSubscriptionAccessGroup(group *SubscriptionAccessGroup) error {
	if group == nil || group.Id <= 0 {
		return errors.New("invalid subscription access group")
	}
	group.Name = strings.TrimSpace(group.Name)
	group.Description = strings.TrimSpace(group.Description)
	if group.Name == "" {
		return errors.New("subscription access group name is empty")
	}
	result := DB.Model(&SubscriptionAccessGroup{}).Where("id = ?", group.Id).Updates(map[string]interface{}{
		"name":        group.Name,
		"description": group.Description,
		"enabled":     group.Enabled,
		"updated_at":  common.GetTimestamp(),
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func DeleteSubscriptionAccessGroup(id int) error {
	if id <= 0 {
		return errors.New("invalid subscription access group id")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("subscription_access_group_id = ?", id).Delete(&SubscriptionAccessGroupUser{}).Error; err != nil {
			return err
		}
		if err := tx.Where("subscription_access_group_id = ?", id).Delete(&SubscriptionPlanAccessGroup{}).Error; err != nil {
			return err
		}
		result := tx.Delete(&SubscriptionAccessGroup{}, id)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
}

func GetUserSubscriptionAccessGroupIDs(userId int) ([]int, error) {
	if userId <= 0 {
		return nil, errors.New("invalid user id")
	}
	var ids []int
	if err := DB.Model(&SubscriptionAccessGroupUser{}).
		Where("user_id = ?", userId).
		Order("subscription_access_group_id asc").
		Pluck("subscription_access_group_id", &ids).Error; err != nil {
		return nil, err
	}
	return ids, nil
}

func SetUserSubscriptionAccessGroups(userId int, ids []int) error {
	if userId <= 0 {
		return errors.New("invalid user id")
	}
	ids = normalizeSubscriptionAccessGroupIDs(ids)
	return DB.Transaction(func(tx *gorm.DB) error {
		var user User
		if err := tx.Select("id").First(&user, userId).Error; err != nil {
			return err
		}
		if err := validateSubscriptionAccessGroupIDsTx(tx, ids); err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", userId).Delete(&SubscriptionAccessGroupUser{}).Error; err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		memberships := make([]SubscriptionAccessGroupUser, 0, len(ids))
		for _, id := range ids {
			memberships = append(memberships, SubscriptionAccessGroupUser{
				SubscriptionAccessGroupId: id,
				UserId:                    userId,
			})
		}
		return tx.Create(&memberships).Error
	})
}

func GetSubscriptionPlanAccessGroupIDs(planId int) ([]int, error) {
	if planId <= 0 {
		return nil, errors.New("invalid subscription plan id")
	}
	var ids []int
	if err := DB.Model(&SubscriptionPlanAccessGroup{}).
		Where("plan_id = ?", planId).
		Order("subscription_access_group_id asc").
		Pluck("subscription_access_group_id", &ids).Error; err != nil {
		return nil, err
	}
	return ids, nil
}

func ReplaceSubscriptionPlanAccessGroupsTx(tx *gorm.DB, planId int, ids []int) error {
	if tx == nil || planId <= 0 {
		return errors.New("invalid subscription plan access group update")
	}
	ids = normalizeSubscriptionAccessGroupIDs(ids)
	if err := validateSubscriptionAccessGroupIDsTx(tx, ids); err != nil {
		return err
	}
	if err := tx.Where("plan_id = ?", planId).Delete(&SubscriptionPlanAccessGroup{}).Error; err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	bindings := make([]SubscriptionPlanAccessGroup, 0, len(ids))
	for _, id := range ids {
		bindings = append(bindings, SubscriptionPlanAccessGroup{
			SubscriptionAccessGroupId: id,
			PlanId:                    planId,
		})
	}
	return tx.Create(&bindings).Error
}

func CanUserAccessSubscriptionPlan(userId int, planId int) (bool, error) {
	if userId <= 0 || planId <= 0 {
		return false, errors.New("invalid subscription plan access arguments")
	}
	var bindings []SubscriptionPlanAccessGroup
	if err := DB.Where("plan_id = ?", planId).Find(&bindings).Error; err != nil {
		return false, err
	}
	if len(bindings) == 0 {
		return true, nil
	}
	groupIds := make([]int, 0, len(bindings))
	for _, binding := range bindings {
		groupIds = append(groupIds, binding.SubscriptionAccessGroupId)
	}
	var count int64
	if err := DB.Model(&SubscriptionAccessGroupUser{}).
		Joins("JOIN subscription_access_groups ON subscription_access_groups.id = subscription_access_group_users.subscription_access_group_id").
		Where("subscription_access_group_users.user_id = ? AND subscription_access_group_users.subscription_access_group_id IN ? AND subscription_access_groups.enabled = ?", userId, groupIds, true).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func ListVisibleSubscriptionPlans(userId int) ([]SubscriptionPlan, error) {
	if userId <= 0 {
		return nil, errors.New("invalid user id")
	}
	var plans []SubscriptionPlan
	if err := DB.Where("enabled = ?", true).Order("sort_order desc, id desc").Find(&plans).Error; err != nil {
		return nil, err
	}
	if len(plans) == 0 {
		return plans, nil
	}
	planIds := make([]int, 0, len(plans))
	for _, plan := range plans {
		planIds = append(planIds, plan.Id)
	}
	var bindings []SubscriptionPlanAccessGroup
	if err := DB.Where("plan_id IN ?", planIds).Find(&bindings).Error; err != nil {
		return nil, err
	}
	if len(bindings) == 0 {
		return plans, nil
	}
	var memberships []SubscriptionAccessGroupUser
	if err := DB.Where("user_id = ?", userId).Find(&memberships).Error; err != nil {
		return nil, err
	}
	userGroupIds := make(map[int]struct{}, len(memberships))
	for _, membership := range memberships {
		userGroupIds[membership.SubscriptionAccessGroupId] = struct{}{}
	}
	var groups []SubscriptionAccessGroup
	if err := DB.Where("enabled = ?", true).Find(&groups).Error; err != nil {
		return nil, err
	}
	enabledGroupIds := make(map[int]struct{}, len(groups))
	for _, group := range groups {
		enabledGroupIds[group.Id] = struct{}{}
	}
	planGroupIds := make(map[int][]int)
	for _, binding := range bindings {
		planGroupIds[binding.PlanId] = append(planGroupIds[binding.PlanId], binding.SubscriptionAccessGroupId)
	}
	visible := make([]SubscriptionPlan, 0, len(plans))
	for _, plan := range plans {
		groupsForPlan := planGroupIds[plan.Id]
		if len(groupsForPlan) == 0 {
			visible = append(visible, plan)
			continue
		}
		for _, groupId := range groupsForPlan {
			if _, enabled := enabledGroupIds[groupId]; !enabled {
				continue
			}
			if _, member := userGroupIds[groupId]; member {
				visible = append(visible, plan)
				break
			}
		}
	}
	return visible, nil
}
