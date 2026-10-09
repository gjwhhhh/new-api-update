package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetProviderPricingCapabilitiesAggregatesChannelState(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&Model{}))
	require.NoError(t, DB.Exec("DELETE FROM abilities").Error)
	require.NoError(t, DB.Exec("DELETE FROM channels").Error)
	require.NoError(t, DB.Exec("DELETE FROM models").Error)
	t.Cleanup(func() {
		require.NoError(t, DB.Exec("DELETE FROM abilities").Error)
		require.NoError(t, DB.Exec("DELETE FROM channels").Error)
		require.NoError(t, DB.Exec("DELETE FROM models").Error)
	})

	channels := []Channel{
		{Id: 9101, Type: constant.ChannelTypeAnthropic, Key: "key-1", Status: common.ChannelStatusEnabled, Name: "anthropic-enabled"},
		{Id: 9102, Type: constant.ChannelTypeOpenAI, Key: "key-2", Status: common.ChannelStatusManuallyDisabled, Name: "openai-disabled"},
		{Id: 9103, Type: constant.ChannelTypeOpenAI, Key: "key-3", Status: common.ChannelStatusManuallyDisabled, Name: "disabled-only"},
		{Id: 9104, Type: constant.ChannelTypeOpenAI, Key: "key-4", Status: common.ChannelStatusEnabled, Name: "metadata-disabled"},
	}
	require.NoError(t, DB.Create(&channels).Error)
	require.NoError(t, DB.Create(&[]Ability{
		{Group: "default", Model: "claude-test", ChannelId: 9101, Enabled: true},
		{Group: "default", Model: "claude-test", ChannelId: 9102, Enabled: false},
		{Group: "vip", Model: "disabled-test", ChannelId: 9103, Enabled: false},
		{Group: "default", Model: "metadata-disabled-test", ChannelId: 9104, Enabled: true},
	}).Error)
	require.NoError(t, DB.Create(&Model{ModelName: "metadata-disabled-test", Status: 1}).Error)
	require.NoError(t, DB.Model(&Model{}).Where("model_name = ?", "metadata-disabled-test").Update("status", 0).Error)

	capabilities, err := GetProviderPricingCapabilities()
	require.NoError(t, err)
	require.Len(t, capabilities, 3)

	assert.Equal(t, ProviderPricingCapability{
		Model:             "claude-test",
		Group:             "default",
		Enabled:           true,
		SupportsAnthropic: true,
	}, capabilities[0])
	assert.Equal(t, ProviderPricingCapability{
		Model:             "disabled-test",
		Group:             "vip",
		Enabled:           false,
		SupportsAnthropic: false,
	}, capabilities[1])
	assert.Equal(t, ProviderPricingCapability{
		Model:             "metadata-disabled-test",
		Group:             "default",
		Enabled:           false,
		SupportsAnthropic: false,
	}, capabilities[2])
}
