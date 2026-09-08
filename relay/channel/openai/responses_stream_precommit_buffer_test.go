package openai

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResponsesStreamPreCommitBufferSpillsReplaysAndCleansUp(t *testing.T) {
	baselineDiskUse := responsesStreamPreCommitDiskInUse.Load()
	buffer := newResponsesStreamPreCommitBuffer(responsesStreamPreCommitBufferConfig{
		memoryBytes: 64 << 10,
		maxBytes:    4 << 20,
		diskBudget:  4 << 20,
		maxEvents:   8,
	})
	first := strings.Repeat("a", 40<<10)
	second := strings.Repeat("b", 40<<10)
	require.NoError(t, buffer.Append(first))
	require.NoError(t, buffer.Append(second))
	require.NotNil(t, buffer.file)
	require.NotEmpty(t, buffer.filePath)

	fileInfo, err := os.Stat(buffer.filePath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), fileInfo.Mode().Perm())

	var replayed []string
	require.NoError(t, buffer.Replay(func(data string) error {
		replayed = append(replayed, data)
		return nil
	}))
	require.Equal(t, []string{first, second}, replayed)

	path := buffer.filePath
	buffer.Close()
	_, err = os.Stat(path)
	assert.True(t, errors.Is(err, os.ErrNotExist))
	assert.Equal(t, baselineDiskUse, responsesStreamPreCommitDiskInUse.Load())
}

func TestResponsesStreamPreCommitBufferKeepsMemoryWhenDiskBudgetIsExhausted(t *testing.T) {
	buffer := newResponsesStreamPreCommitBuffer(responsesStreamPreCommitBufferConfig{
		memoryBytes: 64 << 10,
		maxBytes:    4 << 20,
		diskBudget:  0,
		maxEvents:   8,
	})
	defer buffer.Close()
	first := strings.Repeat("a", 40<<10)
	second := strings.Repeat("b", 40<<10)
	require.NoError(t, buffer.Append(first))
	require.ErrorIs(t, buffer.Append(second), errResponsesStreamPreCommitDiskBudget)
	require.Nil(t, buffer.file)
	assert.Equal(t, int64(len(first)), buffer.Bytes())

	var replayed []string
	require.NoError(t, buffer.Replay(func(data string) error {
		replayed = append(replayed, data)
		return nil
	}))
	assert.Equal(t, []string{first}, replayed)
}

func TestResponsesStreamPreCommitBufferEnforcesEventAndByteLimits(t *testing.T) {
	buffer := newResponsesStreamPreCommitBuffer(responsesStreamPreCommitBufferConfig{
		memoryBytes: 64 << 10,
		maxBytes:    64 << 10,
		diskBudget:  1 << 20,
		maxEvents:   1,
	})
	defer buffer.Close()
	require.NoError(t, buffer.Append(`{"type":"response.created"}`))
	assert.ErrorIs(t, buffer.Append(`{"type":"response.in_progress"}`), errResponsesStreamPreCommitEventLimit)

	byteLimited := newResponsesStreamPreCommitBuffer(responsesStreamPreCommitBufferConfig{
		memoryBytes: 64 << 10,
		maxBytes:    64 << 10,
		diskBudget:  1 << 20,
		maxEvents:   8,
	})
	defer byteLimited.Close()
	assert.ErrorIs(t, byteLimited.Append(strings.Repeat("x", 64<<10+1)), errResponsesStreamPreCommitByteLimit)
}

func TestResponsesStreamPreCommitConfigClampsResourceBounds(t *testing.T) {
	oldMemoryKB := constant.ResponsesStreamPreCommitMemoryKB
	oldMaxKB := constant.ResponsesStreamPreCommitMaxKB
	oldDiskBudgetMB := constant.ResponsesStreamPreCommitDiskBudgetMB
	oldMaxEvents := constant.ResponsesStreamPreCommitMaxEvents
	oldTTLMinutes := constant.ResponsesStreamPreCommitFileTTLMinutes
	t.Cleanup(func() {
		constant.ResponsesStreamPreCommitMemoryKB = oldMemoryKB
		constant.ResponsesStreamPreCommitMaxKB = oldMaxKB
		constant.ResponsesStreamPreCommitDiskBudgetMB = oldDiskBudgetMB
		constant.ResponsesStreamPreCommitMaxEvents = oldMaxEvents
		constant.ResponsesStreamPreCommitFileTTLMinutes = oldTTLMinutes
	})

	constant.ResponsesStreamPreCommitMemoryKB = 1
	constant.ResponsesStreamPreCommitMaxKB = 1
	constant.ResponsesStreamPreCommitDiskBudgetMB = int(^uint(0) >> 1)
	constant.ResponsesStreamPreCommitMaxEvents = 1000
	constant.ResponsesStreamPreCommitFileTTLMinutes = 1

	config := responsesStreamPreCommitConfig()
	assert.Equal(t, int64(64<<10), config.memoryBytes)
	assert.Equal(t, int64(256<<10), config.maxBytes)
	assert.Equal(t, int64(4<<30), config.diskBudget)
	assert.Equal(t, 64, config.maxEvents)
	assert.Equal(t, minResponsesStreamPreCommitFileTTL, config.fileTTL)
}
