package service

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/stretchr/testify/assert"
)

func TestShouldNotDisableChannelForResponsesStreamTerminalError(t *testing.T) {
	previous := common.AutomaticDisableChannelEnabled
	common.AutomaticDisableChannelEnabled = true
	t.Cleanup(func() { common.AutomaticDisableChannelEnabled = previous })

	err := types.NewError(errors.New("upstream responses stream terminated"), types.ErrorCodeChannelUpstreamStreamTerminated)

	assert.False(t, ShouldDisableChannel(err))
}

func TestResponsesStreamTerminalErrorIgnoresDisableStatusAndKeywords(t *testing.T) {
	previousEnabled := common.AutomaticDisableChannelEnabled
	previousStatuses := operation_setting.AutomaticDisableStatusCodeRanges
	previousKeywords := operation_setting.AutomaticDisableKeywords
	common.AutomaticDisableChannelEnabled = true
	operation_setting.AutomaticDisableStatusCodeRanges = []operation_setting.StatusCodeRange{{Start: 500, End: 599}}
	operation_setting.AutomaticDisableKeywords = []string{"stream terminated"}
	t.Cleanup(func() {
		common.AutomaticDisableChannelEnabled = previousEnabled
		operation_setting.AutomaticDisableStatusCodeRanges = previousStatuses
		operation_setting.AutomaticDisableKeywords = previousKeywords
	})

	err := types.NewErrorWithStatusCode(
		errors.New("upstream responses stream terminated"),
		types.ErrorCodeChannelUpstreamStreamTerminated,
		502,
	)

	assert.False(t, ShouldDisableChannel(err))
}

func TestShouldDisableChannelRequiresGlobalSwitchForResponsesStreamTerminalError(t *testing.T) {
	previous := common.AutomaticDisableChannelEnabled
	common.AutomaticDisableChannelEnabled = false
	t.Cleanup(func() { common.AutomaticDisableChannelEnabled = previous })

	err := types.NewError(errors.New("upstream responses stream terminated"), types.ErrorCodeChannelUpstreamStreamTerminated)

	assert.False(t, ShouldDisableChannel(err))
}
