package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/perf_metrics_setting"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestGetPerfMetricsChannelsFiltersByGroupAndProviderWithoutLeakingCredentials(t *testing.T) {
	gin.SetMode(gin.TestMode)
	prev := perf_metrics_setting.GetSetting()
	t.Cleanup(func() {
		perf_metrics_setting.RestoreSettingForTest(prev)
	})
	perf_metrics_setting.RestoreSettingForTest(perf_metrics_setting.PerfMetricsSetting{
		Enabled:            true,
		IncludeChannelTest: true,
		FlushInterval:      5,
		BucketTime:         "hour",
	})

	previousDB := model.DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	t.Cleanup(func() {
		model.DB = previousDB
	})
	require.NoError(t, model.DB.AutoMigrate(
		&model.Channel{},
		&model.PerfChannelMetric{},
		&model.PerfChannelMetricState{},
	))
	const channelID = 940001
	const sameProviderDifferentGroupID = 940002
	const sameGroupDifferentProviderID = 940003
	const channelName = "channel-status-controller-test"
	channelIDs := []int{channelID, sameProviderDifferentGroupID, sameGroupDifferentProviderID}
	require.NoError(t, model.DB.Where("id IN ?", channelIDs).Delete(&model.Channel{}).Error)
	require.NoError(t, model.DB.Where("channel_id IN ?", channelIDs).Delete(&model.PerfChannelMetric{}).Error)
	t.Cleanup(func() {
		_ = model.DB.Where("id IN ?", channelIDs).Delete(&model.Channel{}).Error
		_ = model.DB.Where("channel_id IN ?", channelIDs).Delete(&model.PerfChannelMetric{}).Error
	})

	require.NoError(t, model.DB.Create([]model.Channel{
		{
			Id:     channelID,
			Key:    "must-not-be-returned",
			Name:   channelName,
			Type:   1,
			Status: common.ChannelStatusEnabled,
			Group:  "default,vip",
		},
		{
			Id:     sameProviderDifferentGroupID,
			Key:    "must-not-be-returned-other-group",
			Name:   "channel-status-controller-other-group",
			Type:   1,
			Status: common.ChannelStatusEnabled,
			Group:  "vip-plus",
		},
		{
			Id:     sameGroupDifferentProviderID,
			Key:    "must-not-be-returned-other-provider",
			Name:   "channel-status-controller-other-provider",
			Type:   24,
			Status: common.ChannelStatusEnabled,
			Group:  "vip",
		},
	}).Error)
	now := time.Now().Unix()
	bucketTs := now - now%3600
	for _, channelID := range channelIDs {
		require.NoError(t, model.UpsertPerfChannelMetric(&model.PerfChannelMetric{
			ChannelId:    channelID,
			ModelName:    "gpt-channel-status-test",
			BucketTs:     bucketTs,
			RequestCount: 2,
			SuccessCount: 2,
		}))
	}

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodGet,
		"/api/channel/status-metrics?hours=48&group=vip&type=1",
		nil,
	)

	GetPerfMetricsChannels(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	assert.NotContains(t, recorder.Body.String(), "must-not-be-returned")
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			Total         int   `json:"total"`
			ProviderTypes []int `json:"provider_types"`
			Items         []struct {
				ChannelID    int      `json:"channel_id"`
				ChannelName  string   `json:"channel_name"`
				Groups       []string `json:"groups"`
				Health       string   `json:"health"`
				RequestCount int64    `json:"request_count"`
			} `json:"items"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	require.Equal(t, 1, response.Data.Total)
	assert.Equal(t, []int{1, 24}, response.Data.ProviderTypes)
	require.Len(t, response.Data.Items, 1)
	assert.Equal(t, channelID, response.Data.Items[0].ChannelID)
	assert.Equal(t, channelName, response.Data.Items[0].ChannelName)
	assert.Equal(t, []string{"default", "vip"}, response.Data.Items[0].Groups)
	assert.Equal(t, "running", response.Data.Items[0].Health)
	assert.Equal(t, int64(2), response.Data.Items[0].RequestCount)
}

func TestClearPerfMetricChannelSamplesClearsOnlySelectedChannel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousSetting := perf_metrics_setting.GetSetting()
	t.Cleanup(func() {
		perf_metrics_setting.RestoreSettingForTest(previousSetting)
	})
	perf_metrics_setting.RestoreSettingForTest(perf_metrics_setting.PerfMetricsSetting{
		Enabled:                 true,
		BucketTime:              "hour",
		ChannelSampleGeneration: map[string]int64{},
	})
	common.OptionMapRWMutex.Lock()
	previousOptionMap := common.OptionMap
	common.OptionMap = make(map[string]string)
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previousOptionMap
		common.OptionMapRWMutex.Unlock()
	})

	previousDB := model.DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	t.Cleanup(func() {
		model.DB = previousDB
	})
	require.NoError(t, model.DB.AutoMigrate(
		&model.Channel{},
		&model.Option{},
		&model.PerfChannelMetric{},
		&model.PerfChannelMetricState{},
	))

	const selectedChannelID = 940002
	const otherChannelID = 940003
	require.NoError(t, model.DB.Create([]model.Channel{
		{Id: selectedChannelID, Name: "clear-selected-channel", Group: "default"},
		{Id: otherChannelID, Name: "keep-other-channel", Group: "default"},
	}).Error)
	bucketTs := time.Now().Unix()
	bucketTs -= bucketTs % 3600
	require.NoError(t, model.UpsertPerfChannelMetric(&model.PerfChannelMetric{
		ChannelId:    selectedChannelID,
		ModelName:    "clear-channel-test",
		BucketTs:     bucketTs,
		RequestCount: 2,
		SuccessCount: 1,
	}))
	require.NoError(t, model.UpsertPerfChannelMetric(&model.PerfChannelMetric{
		ChannelId:    otherChannelID,
		ModelName:    "keep-channel-test",
		BucketTs:     bucketTs,
		RequestCount: 2,
		SuccessCount: 2,
	}))

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = []gin.Param{{Key: "id", Value: "940002"}}
	ctx.Request = httptest.NewRequest(
		http.MethodPost,
		"/api/channel/940002/status-metrics/clear",
		bytes.NewBufferString(`{"hours":48}`),
	)

	ClearPerfMetricChannelSamples(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			ChannelID int `json:"channel_id"`
			Hours     int `json:"hours"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	assert.Equal(t, selectedChannelID, response.Data.ChannelID)
	assert.Equal(t, 48, response.Data.Hours)

	var selectedCount int64
	require.NoError(t, model.DB.Model(&model.PerfChannelMetric{}).Where("channel_id = ?", selectedChannelID).Count(&selectedCount).Error)
	assert.Zero(t, selectedCount)

	var otherCount int64
	require.NoError(t, model.DB.Model(&model.PerfChannelMetric{}).Where("channel_id = ?", otherChannelID).Count(&otherCount).Error)
	assert.Equal(t, int64(1), otherCount)
}
