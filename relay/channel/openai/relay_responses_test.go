package openai

import (
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

func TestOaiResponsesStreamHandlerStopsWithoutRetryAfterForwardingEvent(t *testing.T) {
	body := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_1","model":"gpt-test"}}`,
		`data: {"type":"error","error":{"message":"upstream stalled","type":"server_error","code":"upstream_stalled"}}`,
		``,
	}, "\n")
	c, recorder, resp, info := newResponsesStreamTestContext(t, body)

	usage, err := OaiResponsesStreamHandler(c, info, resp)

	require.Error(t, err)
	assert.Nil(t, usage)
	assert.True(t, types.IsSkipRetryError(err))
	assert.True(t, c.GetBool(string(constant.ContextKeyStreamTerminalErrorSent)))
	assert.Contains(t, recorder.Body.String(), "event: response.created")
	assert.Contains(t, recorder.Body.String(), "event: error")
	assert.NotContains(t, recorder.Body.String(), "upstream stalled")
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
