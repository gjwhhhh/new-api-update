package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplySingleKeyHealthCheckResultRecordsOnlyCommittedTransition(t *testing.T) {
	truncateTables(t)
	channel := &Channel{Name: "single", Key: "secret", Status: common.ChannelStatusEnabled}
	require.NoError(t, DB.Create(channel).Error)

	result, err := ApplySingleKeyHealthCheckResult(
		channel.Id,
		common.ChannelStatusEnabled,
		false,
		true,
		true,
		"status_code=502",
	)
	require.NoError(t, err)
	assert.True(t, result.Disabled)
	assert.True(t, result.StatusChanged)

	updated, err := GetChannelById(channel.Id, true)
	require.NoError(t, err)
	assert.Equal(t, common.ChannelStatusAutoDisabled, updated.Status)

	stale, err := ApplySingleKeyHealthCheckResult(
		channel.Id,
		common.ChannelStatusEnabled,
		false,
		true,
		true,
		"status_code=504",
	)
	require.NoError(t, err)
	assert.True(t, stale.Stale)
}

func TestChannelTestResultListUsesStableOrderAndStatusIndependentSummary(t *testing.T) {
	truncateTables(t)
	keyIndex := 0
	results := []*ChannelTestResult{
		{RunID: "run-1", RequestID: "req-1", ChannelID: 36, ChannelName: "test", Source: "scheduled", Status: "succeeded", FailureKind: "none", StateAction: "none", KeyIndex: &keyIndex, CreatedAt: 100},
		{RunID: "run-1", RequestID: "req-2", ChannelID: 36, ChannelName: "test", Source: "scheduled", Status: "failed", FailureKind: "upstream_http", StateAction: "channel_disabled", CreatedAt: 100},
		{RunID: "run-1", RequestID: "req-3", ChannelID: 51, ChannelName: "other", Source: "scheduled", Status: "cancelled", FailureKind: "cancelled", StateAction: "skipped_cancelled", CreatedAt: 100},
	}
	for _, result := range results {
		require.NoError(t, CreateChannelTestResult(result))
	}

	items, total, summary, err := ListChannelTestResults(ChannelTestResultFilter{
		RunID:  "run-1",
		Status: "failed",
		Limit:  20,
	})
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, int64(1), total)
	assert.Equal(t, int64(3), summary.Tested)
	assert.Equal(t, int64(1), summary.Succeeded)
	assert.Equal(t, int64(1), summary.Failed)
	assert.Equal(t, int64(1), summary.Cancelled)
	assert.Equal(t, results[1].ID, items[0].ID)

	allItems, _, _, err := ListChannelTestResults(ChannelTestResultFilter{RunID: "run-1", Limit: 20})
	require.NoError(t, err)
	require.Len(t, allItems, 3)
	assert.Greater(t, allItems[0].ID, allItems[1].ID)
	assert.Greater(t, allItems[1].ID, allItems[2].ID)
	require.NotNil(t, allItems[2].KeyIndex)
	assert.Equal(t, 0, *allItems[2].KeyIndex)
}

func TestDeleteChannelTestResultsBeforeDeletesInBatches(t *testing.T) {
	truncateTables(t)
	for i := 0; i < 3; i++ {
		require.NoError(t, CreateChannelTestResult(&ChannelTestResult{
			RequestID: "old",
			ChannelID: 1,
			Source:    "scheduled",
			Status:    "failed",
			CreatedAt: int64(10 + i),
		}))
	}
	require.NoError(t, CreateChannelTestResult(&ChannelTestResult{
		RequestID: "new",
		ChannelID: 1,
		Source:    "scheduled",
		Status:    "succeeded",
		CreatedAt: 100,
	}))

	deleted, err := DeleteChannelTestResultsBefore(50, 2)
	require.NoError(t, err)
	assert.Equal(t, int64(2), deleted)
	deleted, err = DeleteChannelTestResultsBefore(50, 2)
	require.NoError(t, err)
	assert.Equal(t, int64(1), deleted)

	var remaining int64
	require.NoError(t, DB.Model(&ChannelTestResult{}).Count(&remaining).Error)
	assert.Equal(t, int64(1), remaining)
}
