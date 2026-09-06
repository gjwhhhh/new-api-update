package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetNextTestKeyCanProbeAutoDisabledKey(t *testing.T) {
	channel := &Channel{
		Key: "auto-disabled\nmanual-disabled",
		ChannelInfo: ChannelInfo{
			IsMultiKey: true,
			MultiKeyStatusList: map[int]int{
				0: common.ChannelStatusAutoDisabled,
				1: common.ChannelStatusManuallyDisabled,
			},
		},
	}

	_, _, relayErr := channel.GetNextEnabledKey()
	require.NotNil(t, relayErr)

	key, index, testErr := channel.GetNextTestKey()
	require.Nil(t, testErr)
	assert.Equal(t, "auto-disabled", key)
	assert.Equal(t, 0, index)
}

func TestGetNextTestKeyDoesNotProbeManualDisabledKey(t *testing.T) {
	channel := &Channel{
		Key: "manual-disabled",
		ChannelInfo: ChannelInfo{
			IsMultiKey: true,
			MultiKeyStatusList: map[int]int{
				0: common.ChannelStatusManuallyDisabled,
			},
		},
	}

	_, _, err := channel.GetNextTestKey()
	require.NotNil(t, err)
}

func TestGetNextTestKeyPrefersEnabledKey(t *testing.T) {
	channel := &Channel{
		Key: "auto-disabled\nenabled",
		ChannelInfo: ChannelInfo{
			IsMultiKey: true,
			MultiKeyStatusList: map[int]int{
				0: common.ChannelStatusAutoDisabled,
			},
		},
	}

	key, index, err := channel.GetNextTestKey()
	require.Nil(t, err)
	assert.Equal(t, "enabled", key)
	assert.Equal(t, 1, index)
}

func TestGetNextHealthCheckKeyPrioritizesAndRotatesAutoDisabledKeys(t *testing.T) {
	channel := &Channel{
		Key: "auto-1\nenabled\nauto-2\nmanual",
		ChannelInfo: ChannelInfo{
			IsMultiKey: true,
			MultiKeyStatusList: map[int]int{
				0: common.ChannelStatusAutoDisabled,
				2: common.ChannelStatusAutoDisabled,
				3: common.ChannelStatusManuallyDisabled,
			},
		},
	}

	key, index, relayErr := channel.GetNextEnabledKey()
	require.Nil(t, relayErr)
	assert.Equal(t, "enabled", key)
	assert.Equal(t, 1, index)

	first, err := channel.GetNextHealthCheckKey()
	require.Nil(t, err)
	assert.Equal(t, "auto-1", first.Key)
	assert.Equal(t, 0, first.Index)
	assert.Equal(t, common.ChannelStatusAutoDisabled, first.OriginalStatus)

	second, err := channel.GetNextHealthCheckKey()
	require.Nil(t, err)
	assert.Equal(t, "auto-2", second.Key)
	assert.Equal(t, 2, second.Index)
	assert.Equal(t, common.ChannelStatusAutoDisabled, second.OriginalStatus)
	assert.Equal(t, 3, channel.ChannelInfo.MultiKeyRecoveryPollingIndex)
}

func TestReconcileMultiKeyStateFollowsUnchangedKeyValues(t *testing.T) {
	channel := &Channel{
		Key: "retained\nnew-key",
		ChannelInfo: ChannelInfo{
			IsMultiKey:                   true,
			MultiKeyPollingIndex:         4,
			MultiKeyRecoveryPollingIndex: 4,
			MultiKeyStatusList: map[int]int{
				0: common.ChannelStatusAutoDisabled,
				1: common.ChannelStatusManuallyDisabled,
				4: common.ChannelStatusAutoDisabled,
			},
			MultiKeyDisabledReason: map[int]string{
				0: "removed",
				1: "keep",
				4: "stale",
			},
			MultiKeyDisabledTime: map[int]int64{
				0: 10,
				1: 20,
				4: 40,
			},
		},
	}

	channel.ReconcileMultiKeyState([]string{"removed", "retained"})

	assert.Equal(t, 2, channel.ChannelInfo.MultiKeySize)
	assert.Equal(t, map[int]int{0: common.ChannelStatusManuallyDisabled}, channel.ChannelInfo.MultiKeyStatusList)
	assert.Equal(t, map[int]string{0: "keep"}, channel.ChannelInfo.MultiKeyDisabledReason)
	assert.Equal(t, map[int]int64{0: 20}, channel.ChannelInfo.MultiKeyDisabledTime)
	assert.Equal(t, 0, channel.ChannelInfo.MultiKeyPollingIndex)
	assert.Equal(t, 0, channel.ChannelInfo.MultiKeyRecoveryPollingIndex)
}

func TestManualChannelEnablePreservesDisabledKeyState(t *testing.T) {
	channel := &Channel{
		Status:    common.ChannelStatusAutoDisabled,
		Key:       "auto-disabled\nmanual-disabled",
		OtherInfo: `{"status_reason":"All keys are disabled","status_time":10}`,
		ChannelInfo: ChannelInfo{
			IsMultiKey: true,
			MultiKeyStatusList: map[int]int{
				0: common.ChannelStatusAutoDisabled,
				1: common.ChannelStatusManuallyDisabled,
			},
			MultiKeyDisabledReason: map[int]string{0: "auto", 1: "manual", 3: "stale"},
			MultiKeyDisabledTime:   map[int]int64{0: 10, 1: 20, 3: 30},
		},
	}

	handlerMultiKeyUpdate(channel, "", common.ChannelStatusEnabled, "manual operation")

	assert.Equal(t, common.ChannelStatusAutoDisabled, channel.Status)
	assert.Equal(t, map[int]int{0: common.ChannelStatusAutoDisabled, 1: common.ChannelStatusManuallyDisabled}, channel.ChannelInfo.MultiKeyStatusList)
	assert.Equal(t, map[int]string{0: "auto", 1: "manual"}, channel.ChannelInfo.MultiKeyDisabledReason)
	assert.Equal(t, map[int]int64{0: 10, 1: 20}, channel.ChannelInfo.MultiKeyDisabledTime)
	info := channel.GetOtherInfo()
	assert.Equal(t, "All keys are disabled", info["status_reason"])
	assert.Contains(t, info, "status_time")
}

func TestSuccessfulKeyRecoveryClearsDisabledMetadata(t *testing.T) {
	channel := &Channel{
		Status:    common.ChannelStatusAutoDisabled,
		Key:       "recoverable",
		OtherInfo: `{"status_reason":"All keys are disabled","status_time":10}`,
		ChannelInfo: ChannelInfo{
			IsMultiKey:             true,
			MultiKeyStatusList:     map[int]int{0: common.ChannelStatusAutoDisabled},
			MultiKeyDisabledReason: map[int]string{0: "timeout"},
			MultiKeyDisabledTime:   map[int]int64{0: 10},
		},
	}

	handlerMultiKeyUpdate(channel, "recoverable", common.ChannelStatusEnabled, "")

	assert.Equal(t, common.ChannelStatusEnabled, channel.Status)
	assert.Empty(t, channel.ChannelInfo.MultiKeyStatusList)
	assert.Empty(t, channel.ChannelInfo.MultiKeyDisabledReason)
	assert.Empty(t, channel.ChannelInfo.MultiKeyDisabledTime)
	info := channel.GetOtherInfo()
	assert.NotContains(t, info, "status_reason")
	assert.NotContains(t, info, "status_time")
}

func TestManualKeyDisableUpdatesChannelAvailability(t *testing.T) {
	channel := &Channel{
		Status: common.ChannelStatusEnabled,
		Key:    "first\nsecond",
		ChannelInfo: ChannelInfo{
			IsMultiKey: true,
		},
	}

	handlerMultiKeyUpdateAtIndex(channel, 0, common.ChannelStatusManuallyDisabled, "manual")
	assert.Equal(t, common.ChannelStatusEnabled, channel.Status)

	handlerMultiKeyUpdateAtIndex(channel, 1, common.ChannelStatusManuallyDisabled, "manual")
	assert.Equal(t, common.ChannelStatusManuallyDisabled, channel.Status)
}
