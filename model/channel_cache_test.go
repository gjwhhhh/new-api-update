package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetRandomSatisfiedChannelExcludingSkipsPreviouslyTriedChannel(t *testing.T) {
	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	channelSyncLock.Lock()
	oldGroup2Model2Channels := group2model2channels
	oldChannelsIDM := channelsIDM
	oldChannel2AdvancedCustomConfig := channel2advancedCustomConfig

	priority := int64(0)
	weight := uint(1)
	group2model2channels = map[string]map[string][]int{
		"test-group": {
			"test-model": {101, 102},
		},
	}
	channelsIDM = map[int]*Channel{
		101: {Id: 101, Priority: &priority, Weight: &weight},
		102: {Id: 102, Priority: &priority, Weight: &weight},
	}
	channel2advancedCustomConfig = map[int]*dto.AdvancedCustomConfig{}
	common.MemoryCacheEnabled = true
	channelSyncLock.Unlock()

	t.Cleanup(func() {
		channelSyncLock.Lock()
		group2model2channels = oldGroup2Model2Channels
		channelsIDM = oldChannelsIDM
		channel2advancedCustomConfig = oldChannel2AdvancedCustomConfig
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
		channelSyncLock.Unlock()
	})

	channel, err := GetRandomSatisfiedChannelExcluding("test-group", "test-model", 0, "", map[int]struct{}{101: {}})

	require.NoError(t, err)
	require.NotNil(t, channel)
	assert.Equal(t, 102, channel.Id)
}
