package model

import (
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestUserChannelPrioritiesChangeRoutingAcrossCacheModes(t *testing.T) {
	truncateTables(t)
	user := User{Username: "priority-user", Password: "unused", Status: common.UserStatusEnabled, Group: "default"}
	require.NoError(t, DB.Create(&user).Error)

	priorities := []int64{30, 20, 10}
	channels := make([]Channel, 3)
	for i := range channels {
		weight := uint(1)
		channels[i] = Channel{
			Name:     "priority-channel",
			Key:      "test-key",
			Status:   common.ChannelStatusEnabled,
			Models:   "priority-model",
			Group:    "default",
			Priority: &priorities[i],
			Weight:   &weight,
		}
		require.NoError(t, channels[i].Insert())
	}

	require.NoError(t, PatchUserChannelPriorities(user.Id, []UserChannelPriority{
		{ChannelId: channels[2].Id, Priority: 100},
		{ChannelId: channels[1].Id, Priority: 80},
	}, nil))
	overrides, err := GetUserChannelPriorities(user.Id)
	require.NoError(t, err)
	assert.Equal(t, map[int]int64{channels[2].Id: 100, channels[1].Id: 80}, overrides)

	originalMemoryCache := common.MemoryCacheEnabled
	t.Cleanup(func() {
		common.MemoryCacheEnabled = originalMemoryCache
		InitChannelCache()
	})
	for _, memoryCacheEnabled := range []bool{false, true} {
		common.MemoryCacheEnabled = memoryCacheEnabled
		if memoryCacheEnabled {
			InitChannelCache()
		}
		selected, err := GetRandomSatisfiedChannel("default", "priority-model", 0, nil, overrides)
		require.NoError(t, err)
		require.NotNil(t, selected)
		assert.Equal(t, channels[2].Id, selected.Id)

		selected, err = GetRandomSatisfiedChannel("default", "priority-model", 1, nil, overrides)
		require.NoError(t, err)
		require.NotNil(t, selected)
		assert.Equal(t, channels[1].Id, selected.Id)

		selected, err = GetRandomSatisfiedChannel("default", "priority-model", 2, nil, overrides)
		require.NoError(t, err)
		require.NotNil(t, selected)
		assert.Equal(t, channels[0].Id, selected.Id)

		selected, err = GetRandomSatisfiedChannel("default", "priority-model", 0, []dto.ChannelFilter{{
			Kind: dto.FilterExcludedChannelIDs,
			ExcludedChannelIDs: map[int]struct{}{
				channels[2].Id: {},
			},
		}}, overrides)
		require.NoError(t, err)
		require.NotNil(t, selected)
		assert.Equal(t, channels[1].Id, selected.Id)
	}
}

func TestUserChannelPriorityPatchAndChannelDeletionCleanup(t *testing.T) {
	truncateTables(t)
	user := User{Username: "priority-cleanup-user", Password: "unused", Status: common.UserStatusEnabled, Group: "default"}
	require.NoError(t, DB.Create(&user).Error)
	priority := int64(10)
	channel := Channel{Name: "cleanup-channel", Key: "test-key", Status: common.ChannelStatusEnabled, Models: "cleanup-model", Group: "default", Priority: &priority}
	require.NoError(t, channel.Insert())

	require.NoError(t, PatchUserChannelPriorities(user.Id, []UserChannelPriority{{ChannelId: channel.Id, Priority: 99}}, nil))
	items, err := ListUserChannelPriorities(user.Id)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, int64(99), items[0].Priority)
	assert.Equal(t, int64(10), items[0].GlobalPriority)
	options, total, err := SearchUserChannelPriorityOptions("cleanup-channel", 0, 20)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	require.Len(t, options, 1)
	assert.Equal(t, channel.Id, options[0].ChannelId)

	require.NoError(t, channel.Delete())
	items, err = ListUserChannelPriorities(user.Id)
	require.NoError(t, err)
	assert.Empty(t, items)
	overrides, err := GetUserChannelPriorities(user.Id)
	require.NoError(t, err)
	assert.Empty(t, overrides)
}

func TestPatchUserChannelPrioritiesAppliesDifferentialChangesAtomically(t *testing.T) {
	truncateTables(t)
	user := User{Username: "priority-patch-user", Password: "unused", Status: common.UserStatusEnabled, Group: "default"}
	require.NoError(t, DB.Create(&user).Error)

	channels := make([]Channel, 3)
	for i := range channels {
		priority := int64(i + 1)
		channels[i] = Channel{
			Name:     "priority-patch-channel",
			Key:      "test-key",
			Status:   common.ChannelStatusEnabled,
			Models:   "priority-patch-model",
			Group:    "default",
			Priority: &priority,
		}
		require.NoError(t, channels[i].Insert())
	}

	require.NoError(t, PatchUserChannelPriorities(user.Id, []UserChannelPriority{
		{ChannelId: channels[0].Id, Priority: 100},
		{ChannelId: channels[1].Id, Priority: 90},
	}, nil))
	_, err := GetUserChannelPriorities(user.Id)
	require.NoError(t, err)

	require.NoError(t, PatchUserChannelPriorities(user.Id, []UserChannelPriority{
		{ChannelId: channels[1].Id, Priority: 80},
		{ChannelId: channels[2].Id, Priority: 70},
	}, []int{channels[0].Id}))
	overrides, err := GetUserChannelPriorities(user.Id)
	require.NoError(t, err)
	assert.Equal(t, map[int]int64{
		channels[1].Id: 80,
		channels[2].Id: 70,
	}, overrides)

	err = PatchUserChannelPriorities(user.Id, []UserChannelPriority{
		{ChannelId: channels[1].Id, Priority: 60},
		{ChannelId: channels[2].Id + 10_000, Priority: 50},
	}, []int{channels[2].Id})
	require.Error(t, err)
	overrides, err = GetUserChannelPriorities(user.Id)
	require.NoError(t, err)
	assert.Equal(t, map[int]int64{
		channels[1].Id: 80,
		channels[2].Id: 70,
	}, overrides)
}

func TestUserChannelPrioritySQLiteMigrationIsFreshUpgradeSafeAndIdempotent(t *testing.T) {
	openDatabase := func(t *testing.T, name string) *gorm.DB {
		t.Helper()
		db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), name)))
		require.NoError(t, err)
		sqlDB, err := db.DB()
		require.NoError(t, err)
		t.Cleanup(func() { _ = sqlDB.Close() })
		return db
	}

	t.Run("fresh", func(t *testing.T) {
		db := openDatabase(t, "fresh.db")
		require.NoError(t, db.AutoMigrate(&UserChannelPriority{}))
		require.NoError(t, db.AutoMigrate(&UserChannelPriority{}))
		rule := UserChannelPriority{UserId: 1, ChannelId: 2, Priority: 30}
		require.NoError(t, db.Create(&rule).Error)
		var saved UserChannelPriority
		require.NoError(t, db.First(&saved, "user_id = ? AND channel_id = ?", 1, 2).Error)
		assert.Equal(t, int64(30), saved.Priority)
	})

	t.Run("upgrade", func(t *testing.T) {
		db := openDatabase(t, "upgrade.db")
		require.NoError(t, db.AutoMigrate(&User{}, &Channel{}))
		user := User{Username: "existing-user", Password: "unused", Status: common.UserStatusEnabled, Group: "default"}
		require.NoError(t, db.Create(&user).Error)
		priority := int64(10)
		channel := Channel{Name: "existing-channel", Key: "key", Status: common.ChannelStatusEnabled, Group: "default", Priority: &priority}
		require.NoError(t, db.Create(&channel).Error)

		require.NoError(t, db.AutoMigrate(&UserChannelPriority{}))
		require.NoError(t, db.AutoMigrate(&UserChannelPriority{}))
		require.NoError(t, db.Create(&UserChannelPriority{UserId: user.Id, ChannelId: channel.Id, Priority: 40}).Error)
		var existingUser User
		require.NoError(t, db.First(&existingUser, user.Id).Error)
		assert.Equal(t, "existing-user", existingUser.Username)
		var existingChannel Channel
		require.NoError(t, db.First(&existingChannel, channel.Id).Error)
		assert.Equal(t, "existing-channel", existingChannel.Name)
	})
}
