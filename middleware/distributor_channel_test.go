package middleware

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelTestContextsCanProbeAutoDisabledKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	channel := &model.Channel{
		Id:   36,
		Key:  "recoverable",
		Type: constant.ChannelTypeOpenAI,
		ChannelInfo: model.ChannelInfo{
			IsMultiKey: true,
			MultiKeyStatusList: map[int]int{
				0: common.ChannelStatusAutoDisabled,
			},
		},
	}

	relayContext, _ := gin.CreateTestContext(httptest.NewRecorder())
	relayErr := SetupContextForSelectedChannel(relayContext, channel, "gpt-4o-mini")
	require.NotNil(t, relayErr)

	testContext, _ := gin.CreateTestContext(httptest.NewRecorder())
	testErr := SetupContextForChannelTest(testContext, channel, "gpt-4o-mini")
	require.Nil(t, testErr)
	assert.Equal(t, "recoverable", common.GetContextKeyString(testContext, constant.ContextKeyChannelKey))
	assert.Equal(t, 0, common.GetContextKeyInt(testContext, constant.ContextKeyChannelMultiKeyIndex))

	healthContext, _ := gin.CreateTestContext(httptest.NewRecorder())
	healthErr := SetupContextForChannelHealthCheck(healthContext, channel, "gpt-4o-mini")
	require.Nil(t, healthErr)
	assert.Equal(t, "recoverable", common.GetContextKeyString(healthContext, constant.ContextKeyChannelKey))
	assert.Equal(t, common.ChannelStatusAutoDisabled, common.GetContextKeyInt(healthContext, constant.ContextKeyChannelMultiKeyStatus))
}
