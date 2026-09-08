package openai

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
)

const (
	defaultResponsesStreamPreCommitMemoryBytes     = 256 << 10
	defaultResponsesStreamPreCommitMaxBytes        = 4 << 20
	defaultResponsesStreamPreCommitDiskBudgetBytes = 512 << 20
	defaultResponsesStreamPreCommitMaxEvents       = 8
	defaultResponsesStreamPreCommitFileTTL         = 24 * time.Hour

	minResponsesStreamPreCommitMemoryBytes = 64 << 10
	maxResponsesStreamPreCommitMemoryBytes = 1 << 20
	minResponsesStreamPreCommitMaxBytes    = 256 << 10
	maxResponsesStreamPreCommitMaxBytes    = 64 << 20
	maxResponsesStreamPreCommitDiskBytes   = 4 << 30
	minResponsesStreamPreCommitEvents      = 1
	maxResponsesStreamPreCommitEvents      = 64
	minResponsesStreamPreCommitFileTTL     = time.Hour
	maxResponsesStreamPreCommitFileTTL     = 7 * 24 * time.Hour

	responsesStreamPreCommitTempFilePrefix   = "newapi-responses-precommit-"
	responsesStreamPreCommitRecordHeaderSize = 4
)

var (
	errResponsesStreamPreCommitEventLimit = errors.New("responses stream pre-commit event limit reached")
	errResponsesStreamPreCommitByteLimit  = errors.New("responses stream pre-commit byte limit reached")
	errResponsesStreamPreCommitDiskBudget = errors.New("responses stream pre-commit disk budget reached")

	responsesStreamPreCommitDiskInUse atomic.Int64
	responsesStreamPreCommitCleanup   sync.Once
)

type responsesStreamPreCommitBufferConfig struct {
	memoryBytes int64
	maxBytes    int64
	diskBudget  int64
	maxEvents   int
	fileTTL     time.Duration
}

func responsesStreamPreCommitConfig() responsesStreamPreCommitBufferConfig {
	memoryBytes := normalizedResponsesStreamPreCommitBytes(
		constant.ResponsesStreamPreCommitMemoryKB,
		defaultResponsesStreamPreCommitMemoryBytes,
		minResponsesStreamPreCommitMemoryBytes,
		maxResponsesStreamPreCommitMemoryBytes,
		10,
	)
	maxBytes := normalizedResponsesStreamPreCommitBytes(
		constant.ResponsesStreamPreCommitMaxKB,
		defaultResponsesStreamPreCommitMaxBytes,
		minResponsesStreamPreCommitMaxBytes,
		maxResponsesStreamPreCommitMaxBytes,
		10,
	)
	if maxBytes < memoryBytes {
		maxBytes = memoryBytes
	}
	diskBudget := normalizedResponsesStreamPreCommitBytes(
		constant.ResponsesStreamPreCommitDiskBudgetMB,
		defaultResponsesStreamPreCommitDiskBudgetBytes,
		0,
		maxResponsesStreamPreCommitDiskBytes,
		20,
	)
	maxEvents := constant.ResponsesStreamPreCommitMaxEvents
	if maxEvents <= 0 {
		maxEvents = defaultResponsesStreamPreCommitMaxEvents
	}
	if maxEvents < minResponsesStreamPreCommitEvents {
		maxEvents = minResponsesStreamPreCommitEvents
	}
	if maxEvents > maxResponsesStreamPreCommitEvents {
		maxEvents = maxResponsesStreamPreCommitEvents
	}
	fileTTL := normalizedResponsesStreamPreCommitFileTTL(constant.ResponsesStreamPreCommitFileTTLMinutes)
	return responsesStreamPreCommitBufferConfig{
		memoryBytes: memoryBytes,
		maxBytes:    maxBytes,
		diskBudget:  diskBudget,
		maxEvents:   maxEvents,
		fileTTL:     fileTTL,
	}
}

func normalizedResponsesStreamPreCommitBytes(value int, fallback, minValue, maxValue int64, shift uint) int64 {
	if value <= 0 {
		return fallback
	}
	if int64(value) > maxValue>>shift {
		return maxValue
	}
	bytes := int64(value) << shift
	if bytes < minValue {
		return minValue
	}
	if bytes > maxValue {
		return maxValue
	}
	return bytes
}

func normalizedResponsesStreamPreCommitFileTTL(value int) time.Duration {
	if value <= 0 {
		return defaultResponsesStreamPreCommitFileTTL
	}
	minMinutes := int(minResponsesStreamPreCommitFileTTL / time.Minute)
	maxMinutes := int(maxResponsesStreamPreCommitFileTTL / time.Minute)
	if value < minMinutes {
		value = minMinutes
	}
	if value > maxMinutes {
		value = maxMinutes
	}
	return time.Duration(value) * time.Minute
}

// CleanupStaleResponsesStreamPreCommitFiles removes only old private spill
// files created by this feature. It is safe to call more than once.
func CleanupStaleResponsesStreamPreCommitFiles() {
	responsesStreamPreCommitCleanup.Do(func() {
		config := responsesStreamPreCommitConfig()
		entries, err := os.ReadDir(os.TempDir())
		if err != nil {
			common.SysError("failed to scan responses stream pre-commit temp directory: " + err.Error())
			return
		}
		cutoff := time.Now().Add(-config.fileTTL)
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasPrefix(entry.Name(), responsesStreamPreCommitTempFilePrefix) {
				continue
			}
			info, err := entry.Info()
			if err != nil || info.ModTime().After(cutoff) {
				continue
			}
			_ = os.Remove(filepath.Join(os.TempDir(), entry.Name()))
		}
	})
}

type responsesStreamPreCommitBuffer struct {
	config responsesStreamPreCommitBufferConfig

	memory     [][]byte
	memorySize int64
	totalSize  int64
	eventCount int

	file         *os.File
	filePath     string
	diskReserved int64
	closed       bool
}

func newResponsesStreamPreCommitBuffer(config responsesStreamPreCommitBufferConfig) *responsesStreamPreCommitBuffer {
	return &responsesStreamPreCommitBuffer{config: config}
}

func (buffer *responsesStreamPreCommitBuffer) Append(data string) error {
	if buffer == nil || buffer.closed {
		return errors.New("responses stream pre-commit buffer is closed")
	}
	dataBytes := []byte(data)
	dataSize := int64(len(dataBytes))
	if buffer.eventCount+1 > buffer.config.maxEvents {
		return errResponsesStreamPreCommitEventLimit
	}
	if dataSize > buffer.config.maxBytes-buffer.totalSize {
		return errResponsesStreamPreCommitByteLimit
	}

	if buffer.file == nil && buffer.totalSize+dataSize <= buffer.config.memoryBytes {
		buffer.memory = append(buffer.memory, dataBytes)
		buffer.memorySize += dataSize
		buffer.totalSize += dataSize
		buffer.eventCount++
		return nil
	}

	if buffer.file == nil {
		return buffer.promoteAndAppend(dataBytes)
	}
	reservedSize := dataSize + responsesStreamPreCommitRecordHeaderSize
	if !reserveResponsesStreamPreCommitDisk(reservedSize, buffer.config.diskBudget) {
		return errResponsesStreamPreCommitDiskBudget
	}
	if err := appendResponsesStreamPreCommitRecord(buffer.file, dataBytes); err != nil {
		responsesStreamPreCommitDiskInUse.Add(-reservedSize)
		return fmt.Errorf("append pre-commit spill file: %w", err)
	}
	buffer.diskReserved += reservedSize
	buffer.totalSize += dataSize
	buffer.eventCount++
	return nil
}

func (buffer *responsesStreamPreCommitBuffer) promoteAndAppend(data []byte) error {
	reserveBytes := buffer.memorySize + int64(len(data)) + int64(len(buffer.memory)+1)*responsesStreamPreCommitRecordHeaderSize
	if !reserveResponsesStreamPreCommitDisk(reserveBytes, buffer.config.diskBudget) {
		return errResponsesStreamPreCommitDiskBudget
	}

	file, err := os.CreateTemp(os.TempDir(), responsesStreamPreCommitTempFilePrefix+"*")
	if err != nil {
		responsesStreamPreCommitDiskInUse.Add(-reserveBytes)
		return fmt.Errorf("create pre-commit spill file: %w", err)
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		_ = os.Remove(file.Name())
		responsesStreamPreCommitDiskInUse.Add(-reserveBytes)
		return fmt.Errorf("restrict pre-commit spill file permissions: %w", err)
	}

	for _, pending := range buffer.memory {
		if err := appendResponsesStreamPreCommitRecord(file, pending); err != nil {
			_ = file.Close()
			_ = os.Remove(file.Name())
			responsesStreamPreCommitDiskInUse.Add(-reserveBytes)
			return fmt.Errorf("write pre-commit spill file: %w", err)
		}
	}
	if err := appendResponsesStreamPreCommitRecord(file, data); err != nil {
		_ = file.Close()
		_ = os.Remove(file.Name())
		responsesStreamPreCommitDiskInUse.Add(-reserveBytes)
		return fmt.Errorf("write pre-commit spill file: %w", err)
	}

	buffer.file = file
	buffer.filePath = file.Name()
	buffer.diskReserved = reserveBytes
	buffer.memory = nil
	buffer.memorySize = 0
	buffer.totalSize += int64(len(data))
	buffer.eventCount++
	return nil
}

func (buffer *responsesStreamPreCommitBuffer) Replay(handle func(data string) error) error {
	if buffer == nil || handle == nil {
		return nil
	}
	if buffer.file == nil {
		for _, pending := range buffer.memory {
			if err := handle(string(pending)); err != nil {
				return err
			}
		}
		return nil
	}

	if _, err := buffer.file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind pre-commit spill file: %w", err)
	}
	for {
		data, err := readResponsesStreamPreCommitRecord(buffer.file, buffer.config.maxBytes)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read pre-commit spill file: %w", err)
		}
		if err := handle(string(data)); err != nil {
			return err
		}
	}
}

func (buffer *responsesStreamPreCommitBuffer) Close() {
	if buffer == nil || buffer.closed {
		return
	}
	buffer.closed = true
	if buffer.file != nil {
		_ = buffer.file.Close()
		_ = os.Remove(buffer.filePath)
	}
	if buffer.diskReserved > 0 {
		responsesStreamPreCommitDiskInUse.Add(-buffer.diskReserved)
	}
}

func (buffer *responsesStreamPreCommitBuffer) EventCount() int {
	if buffer == nil {
		return 0
	}
	return buffer.eventCount
}

func (buffer *responsesStreamPreCommitBuffer) Bytes() int64 {
	if buffer == nil {
		return 0
	}
	return buffer.totalSize
}

func (buffer *responsesStreamPreCommitBuffer) SpilledBytes() int64 {
	if buffer == nil {
		return 0
	}
	return buffer.diskReserved
}

func reserveResponsesStreamPreCommitDisk(bytes, budget int64) bool {
	if bytes <= 0 {
		return true
	}
	for {
		used := responsesStreamPreCommitDiskInUse.Load()
		if bytes > budget-used {
			return false
		}
		if responsesStreamPreCommitDiskInUse.CompareAndSwap(used, used+bytes) {
			return true
		}
	}
}

func appendResponsesStreamPreCommitRecord(file *os.File, data []byte) error {
	offset, err := file.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}
	if err := writeResponsesStreamPreCommitRecord(file, data); err != nil {
		_ = file.Truncate(offset)
		_, _ = file.Seek(0, io.SeekEnd)
		return err
	}
	return nil
}

func writeResponsesStreamPreCommitRecord(writer io.Writer, data []byte) error {
	if uint64(len(data)) > uint64(^uint32(0)) {
		return errors.New("pre-commit event exceeds record size")
	}
	var header [responsesStreamPreCommitRecordHeaderSize]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(data)))
	if err := writeResponsesStreamPreCommitFull(writer, header[:]); err != nil {
		return err
	}
	return writeResponsesStreamPreCommitFull(writer, data)
}

func writeResponsesStreamPreCommitFull(writer io.Writer, data []byte) error {
	for len(data) > 0 {
		written, err := writer.Write(data)
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
		data = data[written:]
	}
	return nil
}

func readResponsesStreamPreCommitRecord(reader io.Reader, maxBytes int64) ([]byte, error) {
	var header [responsesStreamPreCommitRecordHeaderSize]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, io.EOF
		}
		return nil, err
	}
	size := int64(binary.BigEndian.Uint32(header[:]))
	if size > maxBytes {
		return nil, errors.New("pre-commit record exceeds configured limit")
	}
	data := make([]byte, size)
	if _, err := io.ReadFull(reader, data); err != nil {
		return nil, err
	}
	return data, nil
}
