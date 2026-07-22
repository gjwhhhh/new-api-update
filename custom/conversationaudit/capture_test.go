package conversationaudit

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestExtractRequestExcludesSensitiveAndBinaryFields(t *testing.T) {
	raw, err := common.Marshal(map[string]any{
		"messages": []any{map[string]any{
			"role":      "user",
			"content":   "hello",
			"api_key":   "must-not-be-stored",
			"image_url": "data:image/png;base64,not-stored",
		}},
	})
	require.NoError(t, err)

	content := extractRequest(raw)
	require.Contains(t, content, "hello")
	require.NotContains(t, content, "must-not-be-stored")
	require.NotContains(t, content, "not-stored")
}

func TestExtractResponseReadsSSEEvents(t *testing.T) {
	content := extractResponse([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\ndata: [DONE]\n"))
	require.Contains(t, content, "hello")
}

func TestExtractSafeErrorCodeDoesNotKeepErrorMessage(t *testing.T) {
	code := extractSafeErrorCode([]byte(`{"error":{"message":"do not store this","type":"upstream_error","code":"channel_unavailable"}}`))
	require.Equal(t, "channel_unavailable", code)
}

func TestSupportsConversationPathSkipsBinaryEndpoints(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	request.Header.Set("Content-Type", "application/json")
	require.True(t, supportsConversationPath(request))
	request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	request.Header.Set("Content-Type", "application/json")
	require.False(t, supportsConversationPath(request))
}
