package model

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpsertPerfChannelMetricAccumulatesOneChannelModelBucket(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&PerfChannelMetric{}))
	const channelID = 930001
	const modelName = "perf-channel-upsert-model"
	const bucketTs = int64(3600)
	require.NoError(t, DB.Where("channel_id = ? AND model_name = ?", channelID, modelName).Delete(&PerfChannelMetric{}).Error)
	t.Cleanup(func() {
		_ = DB.Where("channel_id = ? AND model_name = ?", channelID, modelName).Delete(&PerfChannelMetric{}).Error
	})

	require.NoError(t, UpsertPerfChannelMetric(&PerfChannelMetric{
		ChannelId:      channelID,
		ModelName:      modelName,
		BucketTs:       bucketTs,
		RequestCount:   2,
		SuccessCount:   1,
		TotalLatencyMs: 300,
	}))
	require.NoError(t, UpsertPerfChannelMetric(&PerfChannelMetric{
		ChannelId:      channelID,
		ModelName:      modelName,
		BucketTs:       bucketTs,
		RequestCount:   3,
		SuccessCount:   3,
		TotalLatencyMs: 600,
	}))

	rows, err := GetPerfChannelMetricBuckets(bucketTs, bucketTs, []int{channelID})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, int64(5), rows[0].RequestCount)
	assert.Equal(t, int64(4), rows[0].SuccessCount)
	assert.Equal(t, int64(900), rows[0].TotalLatencyMs)
}

func TestGetChannelStatusMetricPageFiltersSortsAndPaginates(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&Channel{}, &PerfChannelMetric{}))
	const modelName = "channel-status-page-metric"
	channels := []Channel{
		{Id: 930101, Key: "channel-status-page-key-1", Name: "channel-status-page-fixture-alpha", Status: 1},
		{Id: 930102, Key: "channel-status-page-key-2", Name: "channel-status-page-fixture-beta", Status: 1},
		{Id: 930103, Key: "channel-status-page-key-3", Name: "channel-status-page-fixture-gamma", Status: 1},
		{Id: 930104, Key: "channel-status-page-key-4", Name: "channel-status-page-fixture-empty", Status: 1},
	}
	ids := []int{930101, 930102, 930103, 930104}
	require.NoError(t, DB.Where("id IN ?", ids).Delete(&Channel{}).Error)
	require.NoError(t, DB.Where("channel_id IN ? AND model_name = ?", ids, modelName).Delete(&PerfChannelMetric{}).Error)
	t.Cleanup(func() {
		_ = DB.Where("id IN ?", ids).Delete(&Channel{}).Error
		_ = DB.Where("channel_id IN ? AND model_name = ?", ids, modelName).Delete(&PerfChannelMetric{}).Error
	})
	require.NoError(t, DB.Create(&channels).Error)

	bucketTs := time.Now().Unix()
	bucketTs -= bucketTs % 3600
	for _, metric := range []PerfChannelMetric{
		{ChannelId: 930101, ModelName: modelName, BucketTs: bucketTs, RequestCount: 10, SuccessCount: 10},
		{ChannelId: 930102, ModelName: modelName, BucketTs: bucketTs, RequestCount: 20, SuccessCount: 14},
		{ChannelId: 930103, ModelName: modelName, BucketTs: bucketTs, RequestCount: 5, SuccessCount: 0},
	} {
		require.NoError(t, UpsertPerfChannelMetric(&metric))
	}

	page, err := GetChannelStatusMetricPage(ChannelStatusMetricPageParams{
		StartTs: bucketTs,
		EndTs:   bucketTs,
		Search:  "channel-status-page-fixture",
		Sort:    "traffic",
		Desc:    true,
		Limit:   2,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(4), page.Total)
	assert.Equal(t, int64(1), page.HealthCounts["running"])
	assert.Equal(t, int64(1), page.HealthCounts["fluctuating"])
	assert.Equal(t, int64(1), page.HealthCounts["abnormal"])
	assert.Equal(t, int64(1), page.HealthCounts["no_data"])
	require.Len(t, page.Items, 2)
	assert.Equal(t, 930102, page.Items[0].ChannelID)
	assert.Equal(t, 930101, page.Items[1].ChannelID)

	abnormalPage, err := GetChannelStatusMetricPage(ChannelStatusMetricPageParams{
		StartTs: bucketTs,
		EndTs:   bucketTs,
		Search:  "channel-status-page-fixture",
		Health:  "abnormal",
		Sort:    "id",
		Limit:   10,
	})
	require.NoError(t, err)
	require.Len(t, abnormalPage.Items, 1)
	assert.Equal(t, 930103, abnormalPage.Items[0].ChannelID)
}
