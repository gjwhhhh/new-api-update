package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func seedSubscriptionAccessUser(t *testing.T, id int, username string, quota int) {
	t.Helper()
	require.NoError(t, DB.Create(&User{
		Id:       id,
		Username: username,
		Status:   common.UserStatusEnabled,
		Quota:    quota,
		AffCode:  username,
	}).Error)
}

func seedSubscriptionAccessPlan(t *testing.T, id int, title string) *SubscriptionPlan {
	t.Helper()
	plan := &SubscriptionPlan{
		Id:            id,
		Title:         title,
		PriceAmount:   1,
		Currency:      "USD",
		DurationUnit:  SubscriptionDurationMonth,
		DurationValue: 1,
		Enabled:       true,
		TotalAmount:   100,
	}
	require.NoError(t, DB.Create(plan).Error)
	return plan
}

func subscriptionPlanIDs(plans []SubscriptionPlan) map[int]struct{} {
	ids := make(map[int]struct{}, len(plans))
	for _, plan := range plans {
		ids[plan.Id] = struct{}{}
	}
	return ids
}

func TestSubscriptionAccessGroupsLimitPlanVisibilityAndBalancePurchase(t *testing.T) {
	truncateTables(t)

	allowedUserID := 8801
	blockedUserID := 8802
	seedSubscriptionAccessUser(t, allowedUserID, "subscription_access_allowed", 1000)
	seedSubscriptionAccessUser(t, blockedUserID, "subscription_access_blocked", 1000)
	publicPlan := seedSubscriptionAccessPlan(t, 8901, "Public")
	restrictedPlan := seedSubscriptionAccessPlan(t, 8902, "Restricted")
	group := &SubscriptionAccessGroup{Name: "subscription-access-test", Enabled: true}
	require.NoError(t, CreateSubscriptionAccessGroup(group))
	require.NoError(t, SetUserSubscriptionAccessGroups(allowedUserID, []int{group.Id}))
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		return ReplaceSubscriptionPlanAccessGroupsTx(tx, restrictedPlan.Id, []int{group.Id})
	}))

	allowedPlans, err := ListVisibleSubscriptionPlans(allowedUserID)
	require.NoError(t, err)
	assert.Equal(t, map[int]struct{}{publicPlan.Id: {}, restrictedPlan.Id: {}}, subscriptionPlanIDs(allowedPlans))

	blockedPlans, err := ListVisibleSubscriptionPlans(blockedUserID)
	require.NoError(t, err)
	assert.Equal(t, map[int]struct{}{publicPlan.Id: {}}, subscriptionPlanIDs(blockedPlans))

	allowed, err := CanUserAccessSubscriptionPlan(allowedUserID, restrictedPlan.Id)
	require.NoError(t, err)
	assert.True(t, allowed)

	allowed, err = CanUserAccessSubscriptionPlan(blockedUserID, restrictedPlan.Id)
	require.NoError(t, err)
	assert.False(t, allowed)

	require.Error(t, PurchaseSubscriptionWithBalance(blockedUserID, restrictedPlan.Id))
	var blockedUser User
	require.NoError(t, DB.First(&blockedUser, blockedUserID).Error)
	assert.Equal(t, 1000, blockedUser.Quota)
}

func TestDeletingSubscriptionAccessGroupRestoresPlanVisibility(t *testing.T) {
	truncateTables(t)

	userID := 8811
	seedSubscriptionAccessUser(t, userID, "subscription_access_cleanup", 1000)
	plan := seedSubscriptionAccessPlan(t, 8911, "Restricted")
	group := &SubscriptionAccessGroup{Name: "subscription-access-cleanup", Enabled: true}
	require.NoError(t, CreateSubscriptionAccessGroup(group))
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		return ReplaceSubscriptionPlanAccessGroupsTx(tx, plan.Id, []int{group.Id})
	}))

	plans, err := ListVisibleSubscriptionPlans(userID)
	require.NoError(t, err)
	assert.Empty(t, plans)

	require.NoError(t, DeleteSubscriptionAccessGroup(group.Id))
	plans, err = ListVisibleSubscriptionPlans(userID)
	require.NoError(t, err)
	assert.Equal(t, map[int]struct{}{plan.Id: {}}, subscriptionPlanIDs(plans))
}
