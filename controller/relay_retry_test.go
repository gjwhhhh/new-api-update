package controller

import (
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/types"

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

			assert.False(t, shouldRetry(c, channelErr, 1))
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

	assert.True(t, shouldRetry(c, channelErr, 0))
}
