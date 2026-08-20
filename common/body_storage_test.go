package common

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func withDiskSpillConfig(t *testing.T) {
	t.Helper()
	previous := GetDiskCacheConfig()
	t.Cleanup(func() { SetDiskCacheConfig(previous) })
	SetDiskCacheConfig(DiskCacheConfig{
		Enabled:     false,
		ThresholdMB: 1,
		MaxSizeMB:   64,
		Path:        t.TempDir(),
	})
}

func TestCreateBodyStorageFromReaderSpillsToDiskWhenCacheDisabled(t *testing.T) {
	withDiskSpillConfig(t)
	payload := bytes.Repeat([]byte("a"), 2<<20)
	storage, err := CreateBodyStorageFromReader(bytes.NewReader(payload), int64(len(payload)), 128<<20)
	require.NoError(t, err)
	t.Cleanup(func() { _ = storage.Close() })
	assert.True(t, storage.IsDisk())
	got, err := io.ReadAll(storage)
	require.NoError(t, err)
	assert.Equal(t, payload, got)
}

func TestCreateBodyStorageFromReaderKeepsSmallBodiesInMemory(t *testing.T) {
	withDiskSpillConfig(t)
	payload := []byte(`{"input":"hello"}`)
	storage, err := CreateBodyStorageFromReader(bytes.NewReader(payload), int64(len(payload)), 128<<20)
	require.NoError(t, err)
	t.Cleanup(func() { _ = storage.Close() })
	assert.False(t, storage.IsDisk())
	got, err := io.ReadAll(storage)
	require.NoError(t, err)
	assert.Equal(t, payload, got)
}

func TestCreateBodyStorageFromReaderSpillsUnknownLengthAfterThreshold(t *testing.T) {
	withDiskSpillConfig(t)
	payload := bytes.Repeat([]byte("b"), 2<<20)
	storage, err := CreateBodyStorageFromReader(bytes.NewReader(payload), -1, 128<<20)
	require.NoError(t, err)
	t.Cleanup(func() { _ = storage.Close() })
	assert.True(t, storage.IsDisk())
	got, err := io.ReadAll(storage)
	require.NoError(t, err)
	assert.Equal(t, payload, got)
}

func TestUnmarshalBodyReusableStreamDecodesDiskJSON(t *testing.T) {
	withDiskSpillConfig(t)
	gin.SetMode(gin.TestMode)
	payload := []byte(`{"model":"gpt-test","input":"` + strings.Repeat("x", 2<<20) + `"}`)
	storage, err := CreateBodyStorageFromReader(bytes.NewReader(payload), int64(len(payload)), 128<<20)
	require.NoError(t, err)
	require.True(t, storage.IsDisk())
	t.Cleanup(func() { _ = storage.Close() })

	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", http.NoBody)
	context.Request.Header.Set("Content-Type", "application/json")
	context.Set(KeyBodyStorage, storage)

	var decoded struct {
		Model string `json:"model"`
		Input string `json:"input"`
	}
	require.NoError(t, UnmarshalBodyReusable(context, &decoded))
	assert.Equal(t, "gpt-test", decoded.Model)
	assert.Equal(t, strings.Repeat("x", 2<<20), decoded.Input)
}

func TestPeekBodyStorageDoesNotReadRequestBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader("secret"))
	storage, ok := PeekBodyStorage(context)
	assert.False(t, ok)
	assert.Nil(t, storage)
}
