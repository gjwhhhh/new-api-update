package controller

import (
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestShouldRetryHonorsSkipRetryForChannelErrors(t *testing.T) {
	t.Parallel()

	for _, errorCode := range []types.ErrorCode{
		types.ErrorCodeChannelUpstreamStreamTerminated,
		types.ErrorCodeChannelModelMappedError,
	} {
		errorCode := errorCode
		t.Run(string(errorCode), func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest("POST", "/v1/responses", nil)

			channelErr := types.NewError(
				errors.New("channel error"),
				errorCode,
				types.ErrOptionWithSkipRetry(),
			)

			assert.Equal(t, "stop", service.DecideRelayRetry(c, channelErr, 1).Action)
		})
	}
}

func TestShouldRetryRetriesChannelErrorsWithoutSkipRetry(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)

	channelErr := types.NewError(
		errors.New("upstream stream terminated"),
		types.ErrorCodeChannelUpstreamStreamTerminated,
	)

	assert.Equal(t, "retry", service.DecideRelayRetry(c, channelErr, 0).Action)
}

func TestShouldRetryDoesNotRetryPinnedChannelErrors(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	service.GetChannelConstraints(c).AddPin(dto.ChannelPin{ChannelId: 51, Source: dto.PinSourceToken, Rank: dto.PinRankToken, RetryMode: dto.PinRetrySingleAttempt})

	channelErr := types.NewError(
		errors.New("upstream stream terminated"),
		types.ErrorCodeChannelUpstreamStreamTerminated,
	)

	assert.Equal(t, "stop", service.DecideRelayRetry(c, channelErr, 1).Action)
}
