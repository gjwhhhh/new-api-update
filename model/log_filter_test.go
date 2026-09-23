package model

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSplitLogFilterValues(t *testing.T) {
	assert.Empty(t, splitLogFilterValues(""))
	assert.Empty(t, splitLogFilterValues(" , ， "))
	assert.Equal(t, []string{"alice"}, splitLogFilterValues("alice"))
	assert.Equal(t, []string{"alice", "bob"}, splitLogFilterValues("alice, bob"))
	assert.Equal(t, []string{"alice", "bob"}, splitLogFilterValues("alice，bob"))
}

func TestGetAllLogsExcludesUsernames(t *testing.T) {
	truncateTables(t)
	require.NoError(t, LOG_DB.Create([]*Log{
		{Username: "alice", ModelName: "gpt-4", CreatedAt: 100},
		{Username: "bob", ModelName: "gpt-4", CreatedAt: 101},
		{Username: "root", ModelName: "gpt-4", CreatedAt: 102},
	}).Error)

	logs, total, err := GetAllLogs(LogTypeUnknown, 0, 0, "", "", "", 0, 20, 0, "", "", "", "alice, bob")
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, logs, 1)
	assert.Equal(t, "root", logs[0].Username)
}

func TestGetAllLogsExcludesUsernameLikePattern(t *testing.T) {
	truncateTables(t)
	require.NoError(t, LOG_DB.Create([]*Log{
		{Username: "alice", ModelName: "gpt-4", CreatedAt: 100},
		{Username: "albert", ModelName: "gpt-4", CreatedAt: 101},
		{Username: "bob", ModelName: "gpt-4", CreatedAt: 102},
	}).Error)

	logs, total, err := GetAllLogs(LogTypeUnknown, 0, 0, "", "", "", 0, 20, 0, "", "", "", "%al%")
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, logs, 1)
	assert.Equal(t, "bob", logs[0].Username)
}

func TestSumUsedQuotaExcludesUsernames(t *testing.T) {
	truncateTables(t)
	require.NoError(t, LOG_DB.Create([]*Log{
		{Username: "alice", Type: LogTypeConsume, Quota: 10, PromptTokens: 1, CompletionTokens: 1, CreatedAt: 100},
		{Username: "bob", Type: LogTypeConsume, Quota: 20, PromptTokens: 1, CompletionTokens: 1, CreatedAt: 101},
		{Username: "root", Type: LogTypeConsume, Quota: 30, PromptTokens: 1, CompletionTokens: 1, CreatedAt: 102},
	}).Error)

	stat, err := SumUsedQuota(LogTypeUnknown, 0, 0, "", "", "", 0, "", "alice")
	require.NoError(t, err)
	assert.Equal(t, 50, stat.Quota)
}

func TestApplyExcludedLogTextFilterRejectsTooManyValues(t *testing.T) {
	values := make([]string, maxLogTextFilterValues+1)
	for i := range values {
		values[i] = "user"
	}
	_, err := applyExcludedLogTextFilter(LOG_DB, "username", strings.Join(values, ","))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "排除用户过多")
}
