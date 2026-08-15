package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestRenameGroupInTransactionMigratesMutableReferences(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.AutoMigrate(&GroupRenameAlias{}, &Option{}))
	t.Cleanup(func() {
		DB.Exec("DELETE FROM group_rename_aliases")
		DB.Exec("DELETE FROM options")
		_ = RefreshGroupRenameAliasCache()
	})

	user := User{Username: "group-rename-user", Group: "vip", Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(&user).Error)
	token := Token{UserId: user.Id, Key: "group-rename-token", Group: "vip", Status: common.TokenStatusEnabled}
	require.NoError(t, DB.Create(&token).Error)
	channel := Channel{Type: 1, Key: "channel-key", Name: "group-rename-channel", Group: "vip,vip-plus,vip", Models: "gpt-test", Status: common.ChannelStatusEnabled}
	require.NoError(t, DB.Create(&channel).Error)
	require.NoError(t, channel.AddAbilities(nil))
	plan := SubscriptionPlan{Title: "group-rename-plan", UpgradeGroup: "vip", DowngradeGroup: "vip"}
	require.NoError(t, DB.Create(&plan).Error)
	subscription := UserSubscription{UserId: user.Id, PlanId: plan.Id, Status: "active", UpgradeGroup: "vip", PrevUserGroup: "vip", DowngradeGroup: "vip"}
	require.NoError(t, DB.Create(&subscription).Error)
	task := Task{TaskID: "group-rename-task", Platform: "test", UserId: user.Id, Group: "vip", Status: TaskStatusQueued}
	require.NoError(t, DB.Create(&task).Error)
	completedTask := Task{TaskID: "group-rename-complete", Platform: "test", UserId: user.Id, Group: "vip", Status: TaskStatusSuccess}
	require.NoError(t, DB.Create(&completedTask).Error)

	var affectedUsers []int
	err := DB.Transaction(func(tx *gorm.DB) error {
		affected, invalidations, err := RenameGroupInTransaction(tx, "vip", "premium", map[string]string{"GroupRatio": `{"default":1,"premium":1}`}, 1, "test-version")
		require.NoError(t, err)
		assert.Equal(t, int64(1), affected.Users)
		assert.Equal(t, int64(1), affected.Tokens)
		assert.Equal(t, int64(1), affected.Channels)
		assert.Equal(t, int64(1), affected.SubscriptionPlans)
		assert.Equal(t, int64(1), affected.ActiveSubscriptions)
		assert.Equal(t, int64(1), affected.ActiveTasks)
		affectedUsers = invalidations.UserIDs
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, []int{user.Id}, affectedUsers)

	var migratedUser User
	require.NoError(t, DB.First(&migratedUser, user.Id).Error)
	assert.Equal(t, "premium", migratedUser.Group)
	var migratedToken Token
	require.NoError(t, DB.First(&migratedToken, token.Id).Error)
	assert.Equal(t, "premium", migratedToken.Group)
	var migratedChannel Channel
	require.NoError(t, DB.First(&migratedChannel, channel.Id).Error)
	assert.Equal(t, "premium,vip-plus", migratedChannel.Group)
	var abilities []Ability
	require.NoError(t, DB.Where("channel_id = ?", channel.Id).Find(&abilities).Error)
	require.Len(t, abilities, 2)
	assert.ElementsMatch(t, []string{"premium", "vip-plus"}, []string{abilities[0].Group, abilities[1].Group})
	var migratedPlan SubscriptionPlan
	require.NoError(t, DB.First(&migratedPlan, plan.Id).Error)
	assert.Equal(t, "premium", migratedPlan.UpgradeGroup)
	assert.Equal(t, "premium", migratedPlan.DowngradeGroup)
	var migratedSubscription UserSubscription
	require.NoError(t, DB.First(&migratedSubscription, subscription.Id).Error)
	assert.Equal(t, "premium", migratedSubscription.UpgradeGroup)
	assert.Equal(t, "premium", migratedSubscription.PrevUserGroup)
	assert.Equal(t, "premium", migratedSubscription.DowngradeGroup)
	var migratedTask Task
	require.NoError(t, DB.First(&migratedTask, task.ID).Error)
	assert.Equal(t, "premium", migratedTask.Group)
	var historicalTask Task
	require.NoError(t, DB.First(&historicalTask, completedTask.ID).Error)
	assert.Equal(t, "vip", historicalTask.Group)

	require.NoError(t, RefreshGroupRenameAliasCache())
	alias, found, err := ResolveGroupRenameAlias("vip")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "premium", alias)
}
