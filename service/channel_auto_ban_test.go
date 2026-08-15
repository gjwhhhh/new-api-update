package service

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/stretchr/testify/assert"
)

func TestShouldDisableChannelForResponsesStreamTerminalError(t *testing.T) {
	previous := common.AutomaticDisableChannelEnabled
	common.AutomaticDisableChannelEnabled = true
	t.Cleanup(func() { common.AutomaticDisableChannelEnabled = previous })

	err := types.NewError(errors.New("upstream responses stream terminated"), types.ErrorCodeChannelUpstreamStreamTerminated)

	assert.True(t, ShouldDisableChannel(err))
}

func TestShouldDisableChannelRequiresGlobalSwitchForResponsesStreamTerminalError(t *testing.T) {
	previous := common.AutomaticDisableChannelEnabled
	common.AutomaticDisableChannelEnabled = false
	t.Cleanup(func() { common.AutomaticDisableChannelEnabled = previous })

	err := types.NewError(errors.New("upstream responses stream terminated"), types.ErrorCodeChannelUpstreamStreamTerminated)

	assert.False(t, ShouldDisableChannel(err))
}
