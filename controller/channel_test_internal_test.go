package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSettleTestQuotaUsesTieredBilling(t *testing.T) {
	info := &relaycommon.RelayInfo{
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode:   "tiered_expr",
			ExprString:    `param("stream") == true ? tier("stream", p * 3) : tier("base", p * 2)`,
			ExprHash:      billingexpr.ExprHashString(`param("stream") == true ? tier("stream", p * 3) : tier("base", p * 2)`),
			GroupRatio:    1,
			EstimatedTier: "stream",
			QuotaPerUnit:  common.QuotaPerUnit,
			ExprVersion:   1,
		},
		BillingRequestInput: &billingexpr.RequestInput{
			Body: []byte(`{"stream":true}`),
		},
	}

	quota, result := settleTestQuota(info, types.PriceData{
		ModelRatio:      1,
		CompletionRatio: 2,
	}, &dto.Usage{
		PromptTokens: 1000,
	})

	require.Equal(t, 1500, quota)
	require.NotNil(t, result)
	require.Equal(t, "stream", result.MatchedTier)
}

func TestBuildTestLogOtherInjectsTieredInfo(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	info := &relaycommon.RelayInfo{
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode: "tiered_expr",
			ExprString:  `tier("base", p * 2)`,
		},
		ChannelMeta: &relaycommon.ChannelMeta{},
	}
	priceData := types.PriceData{
		GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1},
	}
	usage := &dto.Usage{
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 12,
		},
	}

	other := buildTestLogOther(ctx, info, priceData, usage, &billingexpr.TieredResult{
		MatchedTier: "base",
	})

	require.Equal(t, "tiered_expr", other["billing_mode"])
	require.Equal(t, "base", other["matched_tier"])
	require.NotEmpty(t, other["expr_b64"])
}

func TestResolveChannelTestUserIDUsesRequestUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("id", 2)

	userID, err := resolveChannelTestUserID(ctx)

	require.NoError(t, err)
	require.Equal(t, 2, userID)
}

func TestSelectChannelsForAutomaticTestPassiveRecoveryOnlyUsesAutoDisabled(t *testing.T) {
	channels := []*model.Channel{
		{Id: 1, Status: common.ChannelStatusEnabled},
		{Id: 2, Status: common.ChannelStatusAutoDisabled},
		{Id: 3, Status: common.ChannelStatusManuallyDisabled},
	}

	selected := selectChannelsForAutomaticTest(channels, operation_setting.ChannelTestModePassiveRecovery, 10*time.Minute, 1000)

	require.Len(t, selected, 1)
	require.Equal(t, 2, selected[0].Channel.Id)
	require.False(t, selected[0].AllowDisable)
	require.True(t, selected[0].RecordAutomaticTestTime)
}

func TestSelectChannelsForAutomaticTestScheduledSkipsManualDisabled(t *testing.T) {
	channels := []*model.Channel{
		{Id: 1, Status: common.ChannelStatusEnabled},
		{Id: 2, Status: common.ChannelStatusAutoDisabled},
		{Id: 3, Status: common.ChannelStatusManuallyDisabled},
	}

	selected := selectChannelsForAutomaticTest(channels, operation_setting.ChannelTestModeScheduledAll, 10*time.Minute, 1000)

	require.Len(t, selected, 2)
	require.Equal(t, 1, selected[0].Channel.Id)
	require.Equal(t, 2, selected[1].Channel.Id)
	require.True(t, selected[0].AllowDisable)
	require.True(t, selected[1].AllowDisable)
	require.True(t, selected[0].RecordAutomaticTestTime)
	require.True(t, selected[1].RecordAutomaticTestTime)
}

func TestSelectChannelsForAutomaticTestHonorsChannelHealthCheckOverrides(t *testing.T) {
	tenMinutes := 10
	thirtyMinutes := 30
	channels := []*model.Channel{
		{Id: 1, Status: common.ChannelStatusEnabled},
		{Id: 2, Status: common.ChannelStatusEnabled, LastAutoTestTime: 100},
		{Id: 3, Status: common.ChannelStatusEnabled},
		{Id: 4, Status: common.ChannelStatusAutoDisabled},
		{Id: 5, Status: common.ChannelStatusAutoDisabled},
		{Id: 6, Status: common.ChannelStatusEnabled},
	}
	channels[1].SetOtherSettings(dto.ChannelOtherSettings{
		HealthCheck: &dto.ChannelHealthCheckSettings{
			Mode:            dto.ChannelHealthCheckModeScheduled,
			IntervalMinutes: &thirtyMinutes,
		},
	})
	channels[2].SetOtherSettings(dto.ChannelOtherSettings{
		HealthCheck: &dto.ChannelHealthCheckSettings{Mode: dto.ChannelHealthCheckModeExcluded},
	})
	channels[3].SetOtherSettings(dto.ChannelOtherSettings{
		HealthCheck: &dto.ChannelHealthCheckSettings{
			Mode:            dto.ChannelHealthCheckModePassiveRecovery,
			IntervalMinutes: &tenMinutes,
		},
	})
	channels[4].SetOtherSettings(dto.ChannelOtherSettings{
		HealthCheck: &dto.ChannelHealthCheckSettings{
			Mode:            dto.ChannelHealthCheckModePassiveRecovery,
			IntervalMinutes: &thirtyMinutes,
		},
	})
	channels[4].LastAutoTestTime = 900
	channels[5].SetOtherSettings(dto.ChannelOtherSettings{
		HealthCheck: &dto.ChannelHealthCheckSettings{Mode: dto.ChannelHealthCheckModeScheduled},
	})

	selected := selectChannelsForAutomaticTest(
		channels,
		operation_setting.ChannelTestModeScheduledAll,
		20*time.Minute,
		1000,
	)

	require.Len(t, selected, 3)
	require.Equal(t, 1, selected[0].Channel.Id)
	require.Equal(t, 4, selected[1].Channel.Id)
	require.Equal(t, 6, selected[2].Channel.Id)
	require.True(t, selected[0].AllowDisable)
	require.False(t, selected[1].AllowDisable)
	require.True(t, selected[2].AllowDisable)
	require.True(t, selected[0].RecordAutomaticTestTime)
	require.True(t, selected[1].RecordAutomaticTestTime)
	require.True(t, selected[2].RecordAutomaticTestTime)
}

func TestSelectChannelsForManualTestIgnoresChannelHealthCheckOverrides(t *testing.T) {
	channels := []*model.Channel{
		{Id: 1, Status: common.ChannelStatusEnabled},
		{Id: 2, Status: common.ChannelStatusAutoDisabled},
		{Id: 3, Status: common.ChannelStatusManuallyDisabled},
	}
	channels[0].SetOtherSettings(dto.ChannelOtherSettings{
		HealthCheck: &dto.ChannelHealthCheckSettings{Mode: dto.ChannelHealthCheckModeExcluded},
	})

	selected := selectChannelsForManualTest(channels)

	require.Len(t, selected, 2)
	require.Equal(t, 1, selected[0].Channel.Id)
	require.Equal(t, 2, selected[1].Channel.Id)
	require.False(t, selected[0].RecordAutomaticTestTime)
	require.False(t, selected[1].RecordAutomaticTestTime)
}

func TestTestAllChannelsRejectsExistingActiveTask(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.SystemTask{}, &model.SystemTaskLock{}))

	existing, err := model.CreateSystemTask(model.SystemTaskTypeChannelTest, nil, nil)
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/channel/test", nil)

	TestAllChannels(ctx)

	require.Equal(t, http.StatusConflict, recorder.Code)
	require.Contains(t, recorder.Body.String(), existing.TaskID)
	require.Contains(t, recorder.Body.String(), "已有通道测试任务正在运行或等待中")
}
