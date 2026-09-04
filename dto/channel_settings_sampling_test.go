package dto

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestChannelOtherSettingsIsModelExcludedFromSampling(t *testing.T) {
	settings := ChannelOtherSettings{
		ExcludeFromSamplingModels: []string{" gpt-4 ", "claude-3"},
	}
	assert.True(t, settings.IsModelExcludedFromSampling("gpt-4"))
	assert.True(t, settings.IsModelExcludedFromSampling("claude-3"))
	assert.False(t, settings.IsModelExcludedFromSampling("gpt-5"))
	assert.False(t, settings.IsModelExcludedFromSampling(""))
	assert.False(t, ChannelOtherSettings{}.IsModelExcludedFromSampling("gpt-4"))
}
