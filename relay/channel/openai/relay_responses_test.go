package openai

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setResponsesStreamPreCommitTestConfig(t *testing.T, memoryKB, maxKB, diskBudgetMB, maxEvents int) {
	t.Helper()
	oldMemoryKB := constant.ResponsesStreamPreCommitMemoryKB
	oldMaxKB := constant.ResponsesStreamPreCommitMaxKB
	oldDiskBudgetMB := constant.ResponsesStreamPreCommitDiskBudgetMB
	oldMaxEvents := constant.ResponsesStreamPreCommitMaxEvents
	constant.ResponsesStreamPreCommitMemoryKB = memoryKB
	constant.ResponsesStreamPreCommitMaxKB = maxKB
	constant.ResponsesStreamPreCommitDiskBudgetMB = diskBudgetMB
	constant.ResponsesStreamPreCommitMaxEvents = maxEvents
	t.Cleanup(func() {
		constant.ResponsesStreamPreCommitMemoryKB = oldMemoryKB
		constant.ResponsesStreamPreCommitMaxKB = oldMaxKB
		constant.ResponsesStreamPreCommitDiskBudgetMB = oldDiskBudgetMB
		constant.ResponsesStreamPreCommitMaxEvents = oldMaxEvents
	})
}

func responsesStreamEventWithLength(eventType string, length int) string {
	prefix := fmt.Sprintf(`{"type":%q,"padding":"`, eventType)
	suffix := `"}`
	if length < len(prefix)+len(suffix) {
		panic("responses stream event length is too small")
	}
	return prefix + strings.Repeat("x", length-len(prefix)-len(suffix)) + suffix
}

type responsesStreamScannerErrorReader struct {
	data []byte
	sent bool
}

func (reader *responsesStreamScannerErrorReader) Read(p []byte) (int, error) {
	if !reader.sent {
		reader.sent = true
		return copy(p, reader.data), nil
	}
	return 0, errors.New("upstream connection reset")
}

func newResponsesStreamTestContext(t *testing.T, body string) (*gin.Context, *httptest.ResponseRecorder, *http.Response, *relaycommon.RelayInfo) {
	t.Helper()
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set(common.RequestIdKey, "responses-stream-test")

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta:        &relaycommon.ChannelMeta{UpstreamModelName: "gpt-test"},
		IsStream:           true,
		DisablePing:        true,
		RelayFormat:        types.RelayFormatOpenAI,
		ShouldIncludeUsage: true,
	}

	return c, recorder, resp, info
}

func TestOaiResponsesStreamHandlerTreatsTopLevelErrorAsChannelFailure(t *testing.T) {
	body := "data: {\"type\":\"error\",\"error\":{\"message\":\"upstream stalled\",\"type\":\"server_error\",\"code\":\"upstream_stalled\"}}\n"
	c, recorder, resp, info := newResponsesStreamTestContext(t, body)

	usage, err := OaiResponsesStreamHandler(c, info, resp)

	require.Error(t, err)
	assert.Nil(t, usage)
	assert.Equal(t, types.ErrorCodeChannelUpstreamStreamTerminated, err.GetErrorCode())
	assert.Equal(t, http.StatusBadGateway, err.StatusCode)
	assert.True(t, types.IsChannelError(err))
	assert.False(t, types.IsSkipRetryError(err))
	assert.Empty(t, recorder.Body.String())
	require.NotNil(t, info.StreamStatus)
	assert.True(t, info.StreamStatus.HasErrors())
}

func TestOaiResponsesStreamHandlerTreatsResponseFailedAsChannelFailure(t *testing.T) {
	body := "data: {\"type\":\"response.failed\",\"response\":{\"error\":{\"message\":\"upstream stalled\",\"type\":\"server_error\",\"code\":\"upstream_stalled\"}}}\n"
	c, recorder, resp, info := newResponsesStreamTestContext(t, body)

	usage, err := OaiResponsesStreamHandler(c, info, resp)

	require.Error(t, err)
	assert.Nil(t, usage)
	assert.Equal(t, types.ErrorCodeChannelUpstreamStreamTerminated, err.GetErrorCode())
	assert.Empty(t, recorder.Body.String())
}

func TestOaiResponsesStreamHandlerTreatsUnstructuredErrorEventAsChannelFailure(t *testing.T) {
	body := "data: {\"type\":\"response.error\"}\n"
	c, recorder, resp, info := newResponsesStreamTestContext(t, body)

	usage, err := OaiResponsesStreamHandler(c, info, resp)

	require.Error(t, err)
	assert.Nil(t, usage)
	assert.Equal(t, types.ErrorCodeChannelUpstreamStreamTerminated, err.GetErrorCode())
	assert.Empty(t, recorder.Body.String())
}

func TestOaiResponsesStreamHandlerRetriesAfterOnlyPreCommitEvents(t *testing.T) {
	body := strings.Join([]string{
		`data: {"type":"codex.rate_limits"}`,
		`data: {"type":"codex.response.metadata"}`,
		`data: {"type":"response.created","response":{"id":"resp_1","model":"gpt-test"}}`,
		`data: {"type":"response.in_progress","response":{"id":"resp_1"}}`,
		`data: {"type":"response.failed","response":{"error":{"message":"upstream stalled","type":"server_error","code":"upstream_stalled"}}}`,
		``,
	}, "\n")
	c, recorder, resp, info := newResponsesStreamTestContext(t, body)

	usage, err := OaiResponsesStreamHandler(c, info, resp)

	require.Error(t, err)
	assert.Nil(t, usage)
	assert.False(t, types.IsSkipRetryError(err))
	assert.False(t, c.GetBool(string(constant.ContextKeyStreamTerminalErrorSent)))
	assert.Empty(t, recorder.Body.String())
	assert.Empty(t, recorder.Header().Get("Content-Type"))
}

func TestOaiResponsesStreamHandlerPreCommitFailureLeavesJSONErrorAvailable(t *testing.T) {
	body := "data: {\"type\":\"error\",\"error\":{\"message\":\"upstream stalled\",\"type\":\"server_error\"}}\n"
	c, recorder, resp, info := newResponsesStreamTestContext(t, body)

	_, err := OaiResponsesStreamHandler(c, info, resp)

	require.Error(t, err)
	c.JSON(err.StatusCode, gin.H{"error": err.ToOpenAIError()})
	assert.Contains(t, recorder.Header().Get("Content-Type"), "application/json")
	assert.NotContains(t, recorder.Body.String(), "text/event-stream")
}

func TestOaiResponsesStreamHandlerRetriesAfterRealSizedPreCommitPreamble(t *testing.T) {
	setResponsesStreamPreCommitTestConfig(t, 64, 4096, 512, defaultResponsesStreamPreCommitMaxEvents)
	prelude := []string{
		responsesStreamEventWithLength("codex.rate_limits", 383),
		responsesStreamEventWithLength("codex.response.metadata", 580),
		responsesStreamEventWithLength("response.created", 47886),
		responsesStreamEventWithLength("response.in_progress", 47890),
	}
	preludeBytes := 0
	for _, event := range prelude {
		preludeBytes += len(event)
	}
	require.Equal(t, 96739, preludeBytes)

	lines := make([]string, 0, len(prelude)+2)
	for _, event := range prelude {
		lines = append(lines, "data: "+event)
	}
	lines = append(lines,
		`data: {"type":"response.failed","response":{"error":{"message":"upstream stalled","type":"server_error","code":"upstream_stalled"}}}`,
		``,
	)
	c, recorder, resp, info := newResponsesStreamTestContext(t, strings.Join(lines, "\n"))

	usage, err := OaiResponsesStreamHandler(c, info, resp)

	require.Error(t, err)
	assert.Nil(t, usage)
	assert.False(t, types.IsSkipRetryError(err))
	assert.Empty(t, recorder.Body.String())
	assert.Empty(t, recorder.Header().Get("Content-Type"))
	assert.Equal(t, int64(0), responsesStreamPreCommitDiskInUse.Load())
}

func TestOaiResponsesStreamHandlerStopsWithoutRetryAfterCommittedEvent(t *testing.T) {
	body := strings.Join([]string{
		`data: {"type":"codex.rate_limits"}`,
		`data: {"type":"codex.response.metadata"}`,
		`data: {"type":"response.created","response":{"id":"resp_1","model":"gpt-test"}}`,
		`data: {"type":"response.in_progress","response":{"id":"resp_1"}}`,
		`data: {"type":"response.output_text.delta","delta":"hello"}`,
		`data: {"type":"response.failed","response":{"error":{"message":"upstream stalled","type":"server_error","code":"upstream_stalled"}}}`,
		``,
	}, "\n")
	c, recorder, resp, info := newResponsesStreamTestContext(t, body)

	usage, err := OaiResponsesStreamHandler(c, info, resp)

	require.Error(t, err)
	assert.Nil(t, usage)
	assert.True(t, types.IsSkipRetryError(err))
	assert.True(t, c.GetBool(string(constant.ContextKeyStreamTerminalErrorSent)))
	responseBody := recorder.Body.String()
	assert.Contains(t, responseBody, "event: codex.rate_limits")
	assert.Contains(t, responseBody, "event: codex.response.metadata")
	assert.Contains(t, responseBody, "event: response.created")
	assert.Contains(t, responseBody, "event: response.in_progress")
	assert.Contains(t, responseBody, "event: response.output_text.delta")
	assert.Contains(t, responseBody, "event: response.failed")
	assert.NotContains(t, responseBody, "upstream stalled")
	assert.Less(t, strings.Index(responseBody, "event: codex.rate_limits"), strings.Index(responseBody, "event: codex.response.metadata"))
	assert.Less(t, strings.Index(responseBody, "event: codex.response.metadata"), strings.Index(responseBody, "event: response.created"))
	assert.Less(t, strings.Index(responseBody, "event: response.created"), strings.Index(responseBody, "event: response.in_progress"))
	assert.Less(t, strings.Index(responseBody, "event: response.in_progress"), strings.Index(responseBody, "event: response.output_text.delta"))
}

func TestOaiResponsesStreamHandlerFlushesPreCommitEventsOnNormalCompletion(t *testing.T) {
	body := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_1","model":"gpt-test"}}`,
		`data: {"type":"response.in_progress","response":{"id":"resp_1"}}`,
		`data: [DONE]`,
		``,
	}, "\n")
	c, recorder, resp, info := newResponsesStreamTestContext(t, body)

	usage, err := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, err)
	require.NotNil(t, usage)
	responseBody := recorder.Body.String()
	assert.Contains(t, responseBody, "event: response.created")
	assert.Contains(t, responseBody, "event: response.in_progress")
	assert.Less(t, strings.Index(responseBody, "event: response.created"), strings.Index(responseBody, "event: response.in_progress"))
}

func TestOaiResponsesStreamHandlerTreatsUnknownEventAsCommitted(t *testing.T) {
	body := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_1","model":"gpt-test"}}`,
		`data: {"type":"response.future_extension"}`,
		`data: {"type":"error","error":{"message":"upstream stalled","type":"server_error","code":"upstream_stalled"}}`,
		``,
	}, "\n")
	c, recorder, resp, info := newResponsesStreamTestContext(t, body)

	usage, err := OaiResponsesStreamHandler(c, info, resp)

	require.Error(t, err)
	assert.Nil(t, usage)
	assert.True(t, types.IsSkipRetryError(err))
	assert.True(t, c.GetBool(string(constant.ContextKeyStreamTerminalErrorSent)))
	assert.Contains(t, recorder.Body.String(), "event: response.future_extension")
}

func TestOaiResponsesStreamHandlerCommitsWhenPreCommitBufferIsFull(t *testing.T) {
	setResponsesStreamPreCommitTestConfig(t, 256, 4096, 512, 2)
	events := make([]string, 0, 5)
	for range 3 {
		events = append(events, `data: {"type":"response.in_progress"}`)
	}
	events = append(events,
		`data: {"type":"response.failed","response":{"error":{"message":"upstream stalled","type":"server_error","code":"upstream_stalled"}}}`,
		``,
	)
	c, recorder, resp, info := newResponsesStreamTestContext(t, strings.Join(events, "\n"))

	usage, err := OaiResponsesStreamHandler(c, info, resp)

	require.Error(t, err)
	assert.Nil(t, usage)
	assert.True(t, types.IsSkipRetryError(err))
	assert.True(t, c.GetBool(string(constant.ContextKeyStreamTerminalErrorSent)))
	assert.Contains(t, recorder.Body.String(), "event: response.in_progress")
}

func TestOaiResponsesStreamHandlerRetriesAfterUnexpectedEOFBeforeCommit(t *testing.T) {
	body := `data: {"type":"response.created","response":{"id":"resp_1","model":"gpt-test"}}` + "\n"
	c, recorder, resp, info := newResponsesStreamTestContext(t, body)

	usage, err := OaiResponsesStreamHandler(c, info, resp)

	require.Error(t, err)
	assert.Nil(t, usage)
	assert.Equal(t, types.ErrorCodeChannelUpstreamStreamTerminated, err.GetErrorCode())
	assert.False(t, types.IsSkipRetryError(err))
	assert.Empty(t, recorder.Body.String())
	assert.Empty(t, recorder.Header().Get("Content-Type"))
}

func TestOaiResponsesStreamHandlerRetriesAfterScannerErrorBeforeCommit(t *testing.T) {
	reader := &responsesStreamScannerErrorReader{data: []byte(`data: {"type":"response.created","response":{"id":"resp_1","model":"gpt-test"}}` + "\n")}
	c, recorder, _, info := newResponsesStreamTestContext(t, "")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(reader),
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
	}

	usage, err := OaiResponsesStreamHandler(c, info, resp)

	require.Error(t, err)
	assert.Nil(t, usage)
	assert.Equal(t, types.ErrorCodeChannelUpstreamStreamTerminated, err.GetErrorCode())
	assert.False(t, types.IsSkipRetryError(err))
	assert.Empty(t, recorder.Body.String())
	assert.Empty(t, recorder.Header().Get("Content-Type"))
}

func TestOaiResponsesStreamHandlerRetriesAfterMalformedPreCommitEvent(t *testing.T) {
	body := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_1","model":"gpt-test"}}`,
		"data: {\"type\":",
		"",
	}, "\n")
	c, recorder, resp, info := newResponsesStreamTestContext(t, body)

	usage, err := OaiResponsesStreamHandler(c, info, resp)

	require.Error(t, err)
	assert.Nil(t, usage)
	assert.Equal(t, types.ErrorCodeChannelUpstreamStreamTerminated, err.GetErrorCode())
	assert.False(t, types.IsSkipRetryError(err))
	assert.Empty(t, recorder.Body.String())
	assert.Empty(t, recorder.Header().Get("Content-Type"))
}

func TestOaiResponsesStreamHandlerKeepsCompletedStreamBehavior(t *testing.T) {
	body := strings.Join([]string{
		`data: {"type":"response.output_text.delta","delta":"hello"}`,
		`data: {"type":"response.completed","response":{"usage":{"input_tokens":2,"output_tokens":3,"total_tokens":5}}}`,
		`data: [DONE]`,
		``,
	}, "\n")
	c, recorder, resp, info := newResponsesStreamTestContext(t, body)

	usage, err := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, err)
	require.NotNil(t, usage)
	assert.Equal(t, 2, usage.PromptTokens)
	assert.Equal(t, 3, usage.CompletionTokens)
	assert.Equal(t, 5, usage.TotalTokens)
	assert.Contains(t, recorder.Body.String(), "event: response.output_text.delta")
	assert.Contains(t, recorder.Body.String(), "event: response.completed")
	assert.False(t, c.GetBool(string(constant.ContextKeyStreamTerminalErrorSent)))
	assert.True(t, c.GetBool(string(constant.ContextKeyStreamResponseStarted)))
}

func TestResponsesStreamEventErrorSupportsRootAndResponseErrors(t *testing.T) {
	tests := []struct {
		name  string
		event dto.ResponsesStreamResponse
	}{
		{
			name: "root error",
			event: dto.ResponsesStreamResponse{
				Type:  "error",
				Error: &types.OpenAIError{Type: "server_error", Message: "failed"},
			},
		},
		{
			name: "root error without an event type",
			event: dto.ResponsesStreamResponse{
				Error: &types.OpenAIError{Type: "server_error", Message: "failed"},
			},
		},
		{
			name: "response failed error",
			event: dto.ResponsesStreamResponse{
				Type: "response.failed",
				Response: &dto.OpenAIResponsesResponse{
					Error: types.OpenAIError{Type: "server_error", Message: "failed"},
				},
			},
		},
		{
			name: "generic failure",
			event: dto.ResponsesStreamResponse{
				Type: "response.error",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upstreamError := responsesStreamEventError(&tt.event)
			require.NotNil(t, upstreamError)
			assert.NotEmpty(t, upstreamError.Type)
		})
	}
}

func TestResponsesStreamTerminalEventUsesMatchingProtocolShape(t *testing.T) {
	streamErr := newResponsesStreamChannelError("error", true)
	rootEvent := responsesStreamTerminalEvent("error", streamErr)
	require.NotNil(t, rootEvent.Error)
	assert.Nil(t, rootEvent.Response)
	assert.Equal(t, "error", rootEvent.Type)

	responseEvent := responsesStreamTerminalEvent("response.failed", streamErr)
	assert.Nil(t, responseEvent.Error)
	require.NotNil(t, responseEvent.Response)
	assert.Equal(t, "response.failed", responseEvent.Type)
}
