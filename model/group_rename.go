package model

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"gorm.io/gorm"
)

// GroupRenameCacheInvalidations contains cache keys captured in the same
// transaction as the rename. They are consumed only after commit.
type GroupRenameCacheInvalidations struct {
	UserIDs             []int
	TokenKeys           []string
	SubscriptionPlanIDs []int
}

func groupNameInChannelGroups(groups string, name string) bool {
	for _, group := range strings.Split(groups, ",") {
		if strings.TrimSpace(group) == name {
			return true
		}
	}
	return false
}

func renameChannelGroups(groups string, oldName string, newName string) (string, bool) {
	items := strings.Split(groups, ",")
	changed := false
	seen := make(map[string]struct{}, len(items))
	result := make([]string, 0, len(items))
	for _, item := range items {
		group := strings.TrimSpace(item)
		if group == oldName {
			group = newName
			changed = true
		}
		if group == "" {
			continue
		}
		if _, ok := seen[group]; ok {
			changed = true
			continue
		}
		seen[group] = struct{}{}
		result = append(result, group)
	}
	return strings.Join(result, ","), changed
}

func CountGroupRenameAffected(oldName string) (dto.GroupRenameAffected, error) {
	affected := dto.GroupRenameAffected{}
	if err := DB.Model(&User{}).Where(commonGroupCol+" = ?", oldName).Count(&affected.Users).Error; err != nil {
		return affected, err
	}
	if err := DB.Model(&Token{}).Where(commonGroupCol+" = ?", oldName).Count(&affected.Tokens).Error; err != nil {
		return affected, err
	}
	var channels []Channel
	if err := DB.Select("id", commonGroupCol).Find(&channels).Error; err != nil {
		return affected, err
	}
	channelIDs := make([]int, 0)
	for _, channel := range channels {
		if groupNameInChannelGroups(channel.Group, oldName) {
			channelIDs = append(channelIDs, channel.Id)
		}
	}
	affected.Channels = int64(len(channelIDs))
	if err := DB.Model(&Ability{}).Where(commonGroupCol+" = ?", oldName).Count(&affected.Abilities).Error; err != nil {
		return affected, err
	}
	if err := DB.Model(&SubscriptionPlan{}).
		Where("upgrade_group = ? OR downgrade_group = ?", oldName, oldName).
		Count(&affected.SubscriptionPlans).Error; err != nil {
		return affected, err
	}
	if err := DB.Model(&UserSubscription{}).
		Where("status = ? AND (upgrade_group = ? OR prev_user_group = ? OR downgrade_group = ?)", "active", oldName, oldName, oldName).
		Count(&affected.ActiveSubscriptions).Error; err != nil {
		return affected, err
	}
	if err := DB.Model(&Task{}).
		Where(commonGroupCol+" = ? AND status NOT IN ?", oldName, []TaskStatus{TaskStatusSuccess, TaskStatusFailure}).
		Count(&affected.ActiveTasks).Error; err != nil {
		return affected, err
	}
	return affected, nil
}

func ValidateGroupRenameChannelCapacity(oldName string, newName string) error {
	var channels []Channel
	if err := DB.Select("id", commonGroupCol).Find(&channels).Error; err != nil {
		return err
	}
	for _, channel := range channels {
		updatedGroups, changed := renameChannelGroups(channel.Group, oldName, newName)
		if changed && utf8.RuneCountInString(updatedGroups) > 4096 {
			return fmt.Errorf("channel %d group list exceeds 4096 characters after rename", channel.Id)
		}
	}
	return nil
}

func CountActiveSystemInstances() (int64, error) {
	var count int64
	err := DB.Model(&SystemInstance{}).
		Where("last_seen_at >= ?", common.GetTimestamp()-SystemInstanceStaleAfterSeconds).
		Count(&count).Error
	return count, err
}

// RenameGroupInTransaction migrates every mutable database reference and
// replaces the related option rows. Runtime configuration and cache refresh are
// intentionally left to the service layer after the transaction commits.
func RenameGroupInTransaction(tx *gorm.DB, oldName string, newName string, optionValues map[string]string, createdBy int, migrationVersion string) (dto.GroupRenameAffected, GroupRenameCacheInvalidations, error) {
	affected := dto.GroupRenameAffected{}
	invalidations := GroupRenameCacheInvalidations{}

	var users []User
	if err := lockForUpdate(tx).Select("id").Where(commonGroupCol+" = ?", oldName).Find(&users).Error; err != nil {
		return affected, invalidations, err
	}
	for _, user := range users {
		invalidations.UserIDs = append(invalidations.UserIDs, user.Id)
	}
	affected.Users = int64(len(users))
	if len(users) > 0 {
		if err := tx.Model(&User{}).Where(commonGroupCol+" = ?", oldName).Update("group", newName).Error; err != nil {
			return affected, invalidations, err
		}
	}

	var tokens []Token
	if err := lockForUpdate(tx).Select("id", commonKeyCol).Where(commonGroupCol+" = ?", oldName).Find(&tokens).Error; err != nil {
		return affected, invalidations, err
	}
	for _, token := range tokens {
		invalidations.TokenKeys = append(invalidations.TokenKeys, token.Key)
	}
	affected.Tokens = int64(len(tokens))
	if len(tokens) > 0 {
		if err := tx.Model(&Token{}).Where(commonGroupCol+" = ?", oldName).Update("group", newName).Error; err != nil {
			return affected, invalidations, err
		}
	}

	var channels []Channel
	if err := lockForUpdate(tx).Find(&channels).Error; err != nil {
		return affected, invalidations, err
	}
	affectedChannelIDs := make([]int, 0)
	for i := range channels {
		updatedGroups, changed := renameChannelGroups(channels[i].Group, oldName, newName)
		if !changed {
			continue
		}
		if utf8.RuneCountInString(updatedGroups) > 4096 {
			return affected, invalidations, fmt.Errorf("channel %d group list exceeds 4096 characters after rename", channels[i].Id)
		}
		channels[i].Group = updatedGroups
		affectedChannelIDs = append(affectedChannelIDs, channels[i].Id)
		if err := tx.Model(&Channel{}).Where("id = ?", channels[i].Id).Update("group", updatedGroups).Error; err != nil {
			return affected, invalidations, err
		}
	}
	affected.Channels = int64(len(affectedChannelIDs))
	if err := lockForUpdate(tx).Model(&Ability{}).Where(commonGroupCol+" = ?", oldName).Count(&affected.Abilities).Error; err != nil {
		return affected, invalidations, err
	}
	if len(affectedChannelIDs) > 0 {
		if err := tx.Where("channel_id IN ?", affectedChannelIDs).Delete(&Ability{}).Error; err != nil {
			return affected, invalidations, err
		}
		affectedChannelSet := make(map[int]struct{}, len(affectedChannelIDs))
		for _, channelID := range affectedChannelIDs {
			affectedChannelSet[channelID] = struct{}{}
		}
		for i := range channels {
			if _, affectedChannel := affectedChannelSet[channels[i].Id]; affectedChannel {
				if err := channels[i].AddAbilities(tx); err != nil {
					return affected, invalidations, err
				}
			}
		}
	}

	var plans []SubscriptionPlan
	if err := lockForUpdate(tx).Where("upgrade_group = ? OR downgrade_group = ?", oldName, oldName).Find(&plans).Error; err != nil {
		return affected, invalidations, err
	}
	affected.SubscriptionPlans = int64(len(plans))
	for _, plan := range plans {
		updates := map[string]interface{}{}
		if plan.UpgradeGroup == oldName {
			updates["upgrade_group"] = newName
		}
		if plan.DowngradeGroup == oldName {
			updates["downgrade_group"] = newName
		}
		if len(updates) > 0 {
			if err := tx.Model(&SubscriptionPlan{}).Where("id = ?", plan.Id).Updates(updates).Error; err != nil {
				return affected, invalidations, err
			}
			invalidations.SubscriptionPlanIDs = append(invalidations.SubscriptionPlanIDs, plan.Id)
		}
	}

	var subscriptions []UserSubscription
	if err := lockForUpdate(tx).Where("upgrade_group = ? OR prev_user_group = ? OR downgrade_group = ?", oldName, oldName, oldName).Find(&subscriptions).Error; err != nil {
		return affected, invalidations, err
	}
	for _, subscription := range subscriptions {
		updates := map[string]interface{}{}
		if subscription.UpgradeGroup == oldName {
			updates["upgrade_group"] = newName
		}
		if subscription.PrevUserGroup == oldName {
			updates["prev_user_group"] = newName
		}
		if subscription.DowngradeGroup == oldName {
			updates["downgrade_group"] = newName
		}
		if len(updates) > 0 {
			if err := tx.Model(&UserSubscription{}).Where("id = ?", subscription.Id).Updates(updates).Error; err != nil {
				return affected, invalidations, err
			}
			if subscription.Status == "active" {
				affected.ActiveSubscriptions++
			}
		}
	}

	var tasks []Task
	if err := lockForUpdate(tx).Select("id").Where(commonGroupCol+" = ? AND status NOT IN ?", oldName, []TaskStatus{TaskStatusSuccess, TaskStatusFailure}).Find(&tasks).Error; err != nil {
		return affected, invalidations, err
	}
	affected.ActiveTasks = int64(len(tasks))
	if len(tasks) > 0 {
		taskIDs := make([]int64, 0, len(tasks))
		for _, task := range tasks {
			taskIDs = append(taskIDs, task.ID)
		}
		if err := tx.Model(&Task{}).Where("id IN ?", taskIDs).Update("group", newName).Error; err != nil {
			return affected, invalidations, err
		}
	}

	var priorAliases []GroupRenameAlias
	if err := lockForUpdate(tx).Where("new_name = ?", oldName).Find(&priorAliases).Error; err != nil {
		return affected, invalidations, err
	}
	if err := tx.Model(&GroupRenameAlias{}).Where("new_name = ?", oldName).Update("new_name", newName).Error; err != nil {
		return affected, invalidations, err
	}
	if err := tx.Where("old_name = ?", oldName).Delete(&GroupRenameAlias{}).Error; err != nil {
		return affected, invalidations, err
	}
	alias := GroupRenameAlias{
		OldName:          oldName,
		NewName:          newName,
		ExpiresAt:        common.GetTimestamp() + GroupRenameAliasTTLSeconds,
		CreatedBy:        createdBy,
		MigrationVersion: migrationVersion,
	}
	if err := tx.Create(&alias).Error; err != nil {
		return affected, invalidations, err
	}

	if err := UpdateOptionsBulkWithTx(tx, optionValues); err != nil {
		return affected, invalidations, err
	}
	return affected, invalidations, nil
}
