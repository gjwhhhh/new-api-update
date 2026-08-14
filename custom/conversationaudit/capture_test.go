package conversationaudit

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractLastMessageTextStoresOnlyCurrentUserInput(t *testing.T) {
	raw := []byte(`{"messages":[{"role":"system","content":"private system"},{"role":"user","content":"old question"},{"role":"assistant","content":"old answer"},{"role":"user","content":[{"type":"text","text":"current question"},{"type":"image_url","image_url":{"url":"data:image/png;base64,secret"}}]}]}`)

	text, captureError := extractLatestUserText(protocolOpenAIChat, raw)
	require.Empty(t, captureError)
	assert.Equal(t, "current question", text)
	assert.NotContains(t, text, "private system")
	assert.NotContains(t, text, "old question")
	assert.NotContains(t, text, "secret")
}

func TestExtractLastMessageTextDoesNotRepeatHistoryForToolContinuation(t *testing.T) {
	testCases := []struct {
		name     string
		protocol conversationProtocol
		raw      string
	}{
		{
			name:     "openai chat tool message",
			protocol: protocolOpenAIChat,
			raw:      `{"messages":[{"role":"user","content":"do not repeat"},{"role":"tool","content":"tool output"}]}`,
		},
		{
			name:     "claude tool result",
			protocol: protocolClaude,
			raw:      `{"messages":[{"role":"user","content":"do not repeat"},{"role":"assistant","content":[{"type":"tool_use","id":"tool-1"}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool-1","content":"result"}]}]}`,
		},
		{
			name:     "responses function output",
			protocol: protocolOpenAIResponses,
			raw:      `{"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"do not repeat"}]},{"type":"function_call_output","call_id":"call-1","output":"result"}]}`,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			text, captureError := extractLatestUserText(testCase.protocol, []byte(testCase.raw))
			assert.Empty(t, text)
			assert.Equal(t, "no_new_user_text", captureError)
		})
	}
}

func TestExtractResponsesInputAfterLargeContext(t *testing.T) {
	raw := []byte(`{"model":"gpt-test","instructions":"` + strings.Repeat("s", 300*1024) + `","tools":[{"description":"` + strings.Repeat("t", 300*1024) + `"}],"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"latest input"}]}]}`)
	require.Greater(t, len(raw), defaultMaxContentBytes)
	require.Less(t, len(raw), defaultMaxParseBytes)

	text, captureError := extractLatestUserText(protocolOpenAIResponses, raw)
	require.Empty(t, captureError)
	assert.Equal(t, "latest input", text)
}

func TestExtractGeminiInputStoresOnlyFinalUserTextParts(t *testing.T) {
	raw := []byte(`{"systemInstruction":{"parts":[{"text":"private system"}]},"contents":[{"role":"user","parts":[{"text":"old input"}]},{"role":"model","parts":[{"text":"old response"}]},{"role":"user","parts":[{"text":"first block"},{"inlineData":{"mimeType":"image/png","data":"secret"}},{"functionResponse":{"name":"lookup","response":{"value":"secret"}}},{"text":"second block"}]}]}`)

	text, captureError := extractLatestUserText(protocolGemini, raw)
	require.Empty(t, captureError)
	assert.Equal(t, "first block\nsecond block", text)
	assert.NotContains(t, text, "private system")
	assert.NotContains(t, text, "secret")
}

func TestCaptureRequestRestoresBodyStorageAndDoesNotPersistRawJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	raw := []byte(`{"messages":[{"role":"system","content":"private"},{"role":"user","content":"current"}]}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(string(raw)))
	request.Header.Set("Content-Type", "application/json")
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = request
	storage, err := common.CreateBodyStorage(raw)
	require.NoError(t, err)
	t.Cleanup(func() { _ = storage.Close() })
	context.Set(common.KeyBodyStorage, storage)

	captured := captureRequest(context, protocolOpenAIChat, defaultMaxParseBytes, defaultMaxContentBytes, false)
	require.Empty(t, captured.captureError)
	assert.NotContains(t, captured.body, "private")
	forwarded, err := io.ReadAll(context.Request.Body)
	require.NoError(t, err)
	assert.Equal(t, raw, forwarded)
}

func TestCaptureRequestOverParseLimitStoresOnlyErrorCode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	raw := []byte(`{"messages":[{"role":"user","content":"` + strings.Repeat("x", 2048) + `"}]}`)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(string(raw)))
	context.Request.Header.Set("Content-Type", "application/json")
	storage, err := common.CreateBodyStorage(raw)
	require.NoError(t, err)
	t.Cleanup(func() { _ = storage.Close() })
	context.Set(common.KeyBodyStorage, storage)

	captured := captureRequest(context, protocolOpenAIChat, 1024, defaultMaxContentBytes, false)
	assert.Empty(t, captured.body)
	assert.Equal(t, "request_parse_limit_exceeded", captured.captureError)
}

func TestCaptureRequestFullPayloadPreservesRawJSONBeyondSummaryLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	raw := []byte(`{"messages":[{"role":"system","content":"private system"},{"role":"user","content":"current"}],"metadata":{"trace":"full request"}}`)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(string(raw)))
	context.Request.Header.Set("Content-Type", "application/json")
	storage, err := common.CreateBodyStorage(raw)
	require.NoError(t, err)
	t.Cleanup(func() { _ = storage.Close() })
	context.Set(common.KeyBodyStorage, storage)

	captured := captureRequest(context, protocolOpenAIChat, len(raw), 32, true)
	require.Empty(t, captured.captureError)
	assert.True(t, captured.fullPayload)
	assert.False(t, captured.truncated)
	assert.Equal(t, string(raw), captured.body)
	forwarded, err := io.ReadAll(context.Request.Body)
	require.NoError(t, err)
	assert.Equal(t, raw, forwarded)
}

func TestOpenAIChatSSECombinesCharacterDeltasAndKeepsWhitespace(t *testing.T) {
	collector := newResponseCollector(protocolOpenAIChat, defaultMaxContentBytes, defaultMaxParseBytes, false)
	stream := "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hello\"}}]}\n\n" +
		"data: {\"choices\":[{\"index\":1,\"delta\":{\"content\":\"ignored\"}}]}\n\n" +
		"data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\" \"}}]}\n\n" +
		"data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"world\"}}]}\n\n" +
		"data: [DONE]\n\n"
	for _, fragment := range []string{stream[:17], stream[17:53], stream[53:121], stream[121:]} {
		collector.Write([]byte(fragment), true)
	}

	body, truncated, _, captureError := collector.Finalize()
	require.Empty(t, captureError)
	assert.False(t, truncated)
	assert.Equal(t, "Hello world", decodedPayloadField(t, body, "assistant_text"))
}

func TestFullPayloadResponseCollectorPreservesRawSSE(t *testing.T) {
	stream := "data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"private\"}}\n\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"visible\"}}\n\n"
	collector := newResponseCollector(protocolClaude, len(stream), defaultMaxParseBytes, true)
	collector.Write([]byte(stream[:41]), true)
	collector.Write([]byte(stream[41:]), true)

	captured := collector.FinalizeCapture()
	assert.Equal(t, stream, captured.body)
	assert.False(t, captured.truncated)
	assert.Contains(t, captured.body, "thinking_delta")
}

func TestFullPayloadResponseCollectorMarksContentLimit(t *testing.T) {
	collector := newResponseCollector(protocolClaude, defaultMaxContentBytes, 8, true)
	collector.Write([]byte(`{"content":[{"type":"text","text":"response"}]}`), false)

	captured := collector.FinalizeCapture()
	assert.Equal(t, `{"conten`, captured.body)
	assert.True(t, captured.truncated)
}

func TestResponsesSSEOrdersOutputTextByIndexes(t *testing.T) {
	collector := newResponseCollector(protocolOpenAIResponses, defaultMaxContentBytes, defaultMaxParseBytes, false)
	collector.Write([]byte("event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"output_index\":1,\"content_index\":0,\"delta\":\"second\"}\n\n"), true)
	collector.Write([]byte("event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"output_index\":0,\"content_index\":0,\"delta\":\"first\"}\n\n"), true)

	body, _, _, captureError := collector.Finalize()
	require.Empty(t, captureError)
	assert.Equal(t, "first\nsecond", decodedPayloadField(t, body, "assistant_text"))
}

func TestClaudeAndGeminiStreamingStoreVisibleTextOnly(t *testing.T) {
	testCases := []struct {
		name     string
		protocol conversationProtocol
		stream   string
		want     string
	}{
		{
			name:     "claude",
			protocol: protocolClaude,
			stream:   "data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"private\"}}\n\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"visible text\"}}\n\n",
			want:     "visible text",
		},
		{
			name:     "gemini",
			protocol: protocolGemini,
			stream:   "data: {\"candidates\":[{\"index\":0,\"content\":{\"parts\":[{\"thought\":true,\"text\":\"private\"},{\"text\":\"visible text\"}]}},{\"index\":1,\"content\":{\"parts\":[{\"text\":\"ignored\"}]}}]}\n\n",
			want:     "visible text",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			collector := newResponseCollector(testCase.protocol, defaultMaxContentBytes, defaultMaxParseBytes, false)
			collector.Write([]byte(testCase.stream), true)
			body, _, _, captureError := collector.Finalize()
			require.Empty(t, captureError)
			assert.Equal(t, testCase.want, decodedPayloadField(t, body, "assistant_text"))
		})
	}
}

func TestNonStreamingResponsesStoreOnlyAssistantText(t *testing.T) {
	collector := newResponseCollector(protocolOpenAIResponses, defaultMaxContentBytes, defaultMaxParseBytes, false)
	collector.Write([]byte(`{"output":[{"type":"reasoning","content":[{"type":"summary_text","text":"private reasoning"}]},{"type":"message","role":"assistant","content":[{"type":"output_text","text":"first paragraph"},{"type":"output_text","text":"second paragraph"}]},{"type":"function_call","arguments":"secret"}]}`), false)

	body, truncated, _, captureError := collector.Finalize()
	require.Empty(t, captureError)
	assert.False(t, truncated)
	assert.Equal(t, "first paragraph\nsecond paragraph", decodedPayloadField(t, body, "assistant_text"))
	assert.NotContains(t, body, "private reasoning")
	assert.NotContains(t, body, "secret")
}

func TestSSEDecoderBoundsOversizedFrameWithoutBlockingLaterFrames(t *testing.T) {
	var payloads []string
	decoder := newSSEDecoder(32, func(payload []byte) {
		payloads = append(payloads, string(payload))
	})
	decoder.Write([]byte("data: " + strings.Repeat("x", 64) + "\n\ndata: {\"ok\":true}\n\n"))
	decoder.Flush()

	assert.True(t, decoder.Oversized())
	assert.Equal(t, []string{`{"ok":true}`}, payloads)
}

func TestMarshalCapturePayloadTruncatesOnUTF8Boundary(t *testing.T) {
	body, truncated, err := marshalCapturePayload(capturePayload{
		SchemaVersion: 2,
		CaptureMode:   "latest_user_text",
		UserInput:     strings.Repeat("会", 200),
	}, 180)
	require.NoError(t, err)
	assert.True(t, truncated)
	assert.LessOrEqual(t, len(body), 180)
	assert.True(t, utf8.ValidString(body))
	assert.True(t, utf8.ValidString(decodedPayloadField(t, body, "user_input")))
}

func TestExtractSafeErrorCodeDoesNotKeepErrorMessage(t *testing.T) {
	code := extractSafeErrorCode([]byte(`{"error":{"message":"do not store this","type":"upstream_error","code":"channel_unavailable"}}`))
	assert.Equal(t, "channel_unavailable", code)
}

func TestProtocolForRequestSkipsCompactAndBinaryEndpoints(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	request.Header.Set("Content-Type", "application/json")
	assert.Equal(t, protocolOpenAIChat, protocolForRequest(request))

	for _, path := range []string{"/v1/responses/compact", "/v1/images/generations", "/v1/audio/speech"} {
		request = httptest.NewRequest(http.MethodPost, path, nil)
		request.Header.Set("Content-Type", "application/json")
		assert.Equal(t, protocolUnknown, protocolForRequest(request), path)
	}
}

func decodedPayloadField(t *testing.T, body, field string) string {
	t.Helper()
	var payload map[string]any
	require.NoError(t, common.Unmarshal([]byte(body), &payload))
	value, ok := payload[field].(string)
	require.True(t, ok, "missing %s in %s", field, body)
	return value
}
