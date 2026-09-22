package service

import (
	"errors"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/assert"
)

func TestChannelTestFailureKindPreservesStreamTerminationAfterHTTP200(t *testing.T) {
	err := types.NewOpenAIError(errors.New("stream ended"), types.ErrorCodeChannelUpstreamStreamTerminated, http.StatusBadGateway)
	assert.Equal(t, ChannelTestFailureKindStreamTerminated, ChannelTestFailureKind(err, err, http.StatusOK))
}

func TestChannelTestFailureKindDistinguishesTransportAndUpstreamHTTP(t *testing.T) {
	transportErr := types.NewOpenAIError(errors.New("connect failed"), types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	assert.Equal(t, ChannelTestFailureKindTransport, ChannelTestFailureKind(transportErr, transportErr, 0))
	assert.Equal(t, ChannelTestFailureKindUpstreamHTTP, ChannelTestFailureKind(errors.New("bad response"), nil, http.StatusGatewayTimeout))
}
