package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMultiKeyHealthChecksRecoverEachAutoDisabledKey(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	previousMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = previousMemoryCacheEnabled })

	channel := &model.Channel{
		Name:   "multi-key-recovery",
		Key:    "key-a\nkey-b\nkey-c",
		Status: common.ChannelStatusAutoDisabled,
		Models: "gpt-4o-mini",
		Group:  "default",
		ChannelInfo: model.ChannelInfo{
			IsMultiKey: true,
			MultiKeyStatusList: map[int]int{
				0: common.ChannelStatusAutoDisabled,
				1: common.ChannelStatusAutoDisabled,
				2: common.ChannelStatusAutoDisabled,
			},
		},
	}
	require.NoError(t, db.Create(channel).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group:     "default",
		Model:     "gpt-4o-mini",
		ChannelId: channel.Id,
		Enabled:   false,
	}).Error)

	for expectedIndex := 0; expectedIndex < 3; expectedIndex++ {
		stored, err := model.GetChannelById(channel.Id, true)
		require.NoError(t, err)
		selection, relayErr := stored.GetNextHealthCheckKey()
		require.Nil(t, relayErr)
		assert.Equal(t, expectedIndex, selection.Index)
		assert.Equal(t, common.ChannelStatusAutoDisabled, selection.OriginalStatus)

		result, err := model.ApplyMultiKeyHealthCheckResult(channel.Id, selection, true, false, true, "")
		require.NoError(t, err)
		assert.True(t, result.Enabled)
		if expectedIndex == 0 {
			ability := &model.Ability{}
			require.NoError(t, db.Where("channel_id = ?", channel.Id).First(ability).Error)
			assert.True(t, ability.Enabled)
		}
	}

	stored, err := model.GetChannelById(channel.Id, true)
	require.NoError(t, err)
	assert.Equal(t, common.ChannelStatusEnabled, stored.Status)
	assert.Empty(t, stored.ChannelInfo.MultiKeyStatusList)
}

func TestMultiKeyHealthCheckRejectsStaleKeyResult(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	previousMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = previousMemoryCacheEnabled })

	channel := &model.Channel{
		Name:   "stale-multi-key-recovery",
		Key:    "old-key\nrecoverable",
		Status: common.ChannelStatusAutoDisabled,
		Models: "gpt-4o-mini",
		Group:  "default",
		ChannelInfo: model.ChannelInfo{
			IsMultiKey:         true,
			MultiKeyStatusList: map[int]int{0: common.ChannelStatusAutoDisabled},
		},
	}
	require.NoError(t, db.Create(channel).Error)

	stored, err := model.GetChannelById(channel.Id, true)
	require.NoError(t, err)
	selection, relayErr := stored.GetNextHealthCheckKey()
	require.Nil(t, relayErr)
	require.Equal(t, 0, selection.Index)

	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", channel.Id).Update("key", "replacement\nrecoverable").Error)
	result, err := model.ApplyMultiKeyHealthCheckResult(channel.Id, selection, true, false, true, "")
	require.NoError(t, err)
	assert.True(t, result.Stale)

	stored, err = model.GetChannelById(channel.Id, true)
	require.NoError(t, err)
	assert.Equal(t, common.ChannelStatusAutoDisabled, stored.Status)
	assert.Equal(t, common.ChannelStatusAutoDisabled, stored.ChannelInfo.MultiKeyStatusList[0])
}

func TestMultiKeyStatusUpdateUsesSelectedIndexForDuplicateKeys(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	previousMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = previousMemoryCacheEnabled })

	channel := &model.Channel{
		Name:   "duplicate-multi-key",
		Key:    "same-key\nsame-key",
		Status: common.ChannelStatusEnabled,
		ChannelInfo: model.ChannelInfo{
			IsMultiKey: true,
		},
	}
	require.NoError(t, db.Create(channel).Error)

	require.True(t, model.UpdateChannelStatusAtKeyIndex(channel.Id, 1, common.ChannelStatusAutoDisabled, "upstream error"))
	stored, err := model.GetChannelById(channel.Id, true)
	require.NoError(t, err)
	assert.Equal(t, common.ChannelStatusEnabled, stored.Status)
	assert.Equal(t, map[int]int{1: common.ChannelStatusAutoDisabled}, stored.ChannelInfo.MultiKeyStatusList)

	require.True(t, model.UpdateChannelStatusAtKeyIndex(channel.Id, 0, common.ChannelStatusAutoDisabled, "upstream error"))
	stored, err = model.GetChannelById(channel.Id, true)
	require.NoError(t, err)
	assert.Equal(t, common.ChannelStatusAutoDisabled, stored.Status)
	assert.Equal(t, map[int]int{0: common.ChannelStatusAutoDisabled, 1: common.ChannelStatusAutoDisabled}, stored.ChannelInfo.MultiKeyStatusList)
}
