package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResetStatusCode(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name             string
		statusCode       int
		statusCodeConfig string
		expectedCode     int
	}{
		{
			name:             "map string value",
			statusCode:       429,
			statusCodeConfig: `{"429":"503"}`,
			expectedCode:     503,
		},
		{
			name:             "map int value",
			statusCode:       429,
			statusCodeConfig: `{"429":503}`,
			expectedCode:     503,
		},
		{
			name:             "skip invalid string value",
			statusCode:       429,
			statusCodeConfig: `{"429":"bad-code"}`,
			expectedCode:     429,
		},
		{
			name:             "skip status code 200",
			statusCode:       200,
			statusCodeConfig: `{"200":503}`,
			expectedCode:     200,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			newAPIError := &types.NewAPIError{
				StatusCode: tc.statusCode,
			}
			ResetStatusCode(newAPIError, tc.statusCodeConfig)
			require.Equal(t, tc.expectedCode, newAPIError.StatusCode)
		})
	}
}

func TestRelayErrorHandlerTruncatesInvalidJSONBodyInLog(t *testing.T) {
	withDebugEnabled(t, false)

	body := strings.Repeat("b", common.LocalLogContentLimit+256)
	var logBuffer bytes.Buffer

	common.LogWriterMu.Lock()
	oldWriter := gin.DefaultErrorWriter
	gin.DefaultErrorWriter = &logBuffer
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		common.LogWriterMu.Lock()
		gin.DefaultErrorWriter = oldWriter
		common.LogWriterMu.Unlock()
	})

	resp := &http.Response{
		StatusCode: http.StatusInternalServerError,
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	newAPIError := RelayErrorHandler(context.Background(), resp, false)

	require.NotNil(t, newAPIError)
	require.Equal(t, "bad response status code 500", newAPIError.Error())
	require.Contains(t, logBuffer.String(), "[truncated")
	require.Contains(t, logBuffer.String(), fmt.Sprintf("original_length=%d", len(body)))
	require.NotContains(t, logBuffer.String(), strings.Repeat("b", common.LocalLogContentLimit+1))
}

func TestRelayErrorHandlerKeepsStructuredErrorMessage(t *testing.T) {
	message := strings.Repeat("c", common.LocalLogContentLimit+256)
	body := `{"message":"` + message + `"}`
	resp := &http.Response{
		StatusCode: http.StatusInternalServerError,
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	newAPIError := RelayErrorHandler(context.Background(), resp, false)

	require.NotNil(t, newAPIError)
	require.Equal(t, message, newAPIError.Error())
}

func TestRelayErrorHandlerKeepsOpenAIErrorMessage(t *testing.T) {
	message := strings.Repeat("d", common.LocalLogContentLimit+256)
	body := `{"error":{"message":"` + message + `","type":"server_error","code":"server_error"}}`
	resp := &http.Response{
		StatusCode: http.StatusInternalServerError,
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	newAPIError := RelayErrorHandler(context.Background(), resp, false)

	require.NotNil(t, newAPIError)
	require.Equal(t, message, newAPIError.Error())
}

func TestRelayErrorHandlerMatchesCyberPolicyByExactCode(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		code types.ErrorCode
	}{
		{"code only", `{"error":{"code":"cyber_policy"}}`, "cyber_policy"},
		{"code with message", `{"error":{"code":"cyber_policy","message":"private detail"}}`, "cyber_policy"},
		{"code with non-string message", `{"error":{"code":"cyber_policy","message":{}}}`, "cyber_policy"},
		{"message only", `{"error":{"message":"cyber_policy"}}`, "unknown_error"},
		{"status only", `{}`, types.ErrorCodeBadResponseStatusCode},
		{"different code", `{"error":{"code":"CYBER_POLICY","message":"cyber_policy"}}`, "CYBER_POLICY"},
		{"malformed body", `{"error":{"code":"cyber_policy"}`, types.ErrorCodeBadResponseStatusCode},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := &http.Response{
				StatusCode: http.StatusForbidden,
				Body:       io.NopCloser(strings.NewReader(tc.body)),
			}
			apiErr := RelayErrorHandler(t.Context(), resp, true)
			require.NotNil(t, apiErr)
			require.Equal(t, tc.code, apiErr.GetErrorCode())
			assert.Equal(t, http.StatusForbidden, apiErr.StatusCode)
			assert.Equal(t, tc.code == "cyber_policy", types.IsSkipRetryError(apiErr))
			if tc.code == "cyber_policy" {
				assert.Equal(t, "invalid_request", apiErr.ToOpenAIError().Type)
				assert.Equal(t, "Request rejected by an upstream content policy", apiErr.Error())
			}
		})
	}
}

// codexRecognizesHTTPCyberPolicy mirrors openai/codex api_bridge.rs:
// HTTP CyberPolicy is accepted only when status is 400 and error.code is cyber_policy.
// The error.type field is not consulted.
func codexRecognizesHTTPCyberPolicy(status int, code, _type string) bool {
	_ = _type
	return status == http.StatusBadRequest && code == "cyber_policy"
}

func TestCodexHTTPCyberPolicyShapeMatrix(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name           string
		status         int
		code, typ      string
		wantRecognized bool
	}{
		{
			name:   "codex_fixture_400_invalid_request",
			status: http.StatusBadRequest, code: "cyber_policy", typ: "invalid_request",
			wantRecognized: true,
		},
		{
			name:   "planned_500_content_policy_violation",
			status: http.StatusInternalServerError, code: "cyber_policy", typ: "content_policy_violation",
			wantRecognized: false,
		},
		{
			name:   "planned_code_type_with_400",
			status: http.StatusBadRequest, code: "cyber_policy", typ: "content_policy_violation",
			wantRecognized: true,
		},
		{
			name:   "project_normalized_403_content_policy_violation",
			status: http.StatusForbidden, code: "cyber_policy", typ: "content_policy_violation",
			wantRecognized: false,
		},
		{
			name:   "local_sensitive_uses_recognized_shape",
			status: http.StatusBadRequest, code: "cyber_policy", typ: "invalid_request",
			wantRecognized: true,
		},
		{
			name:   "400_but_wrong_code",
			status: http.StatusBadRequest, code: "sensitive_words_detected", typ: "invalid_request",
			wantRecognized: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := codexRecognizesHTTPCyberPolicy(tc.status, tc.code, tc.typ)
			assert.Equal(t, tc.wantRecognized, got,
				"status=%d code=%q type=%q", tc.status, tc.code, tc.typ)
		})
	}
}

func TestRelayErrorHandlerOptionalCyberMessageMatchesOnlyEligibleFailures(t *testing.T) {
	for _, tc := range []struct {
		name, body, source string
		status             int
	}{
		{"nested", `{"error":{"code":"invalid_request_error","message":"denied [cyber_policy]"}}`, "message", 403},
		{"flat", `{"type":"invalid_request_error","message":"denied (cyber_policy)"}`, "message", 400},
		{"flat structured code", `{"code":"cyber_policy"}`, "code", 403},
		{"flat conflicting code", `{"code":"invalid_api_key","message":"cyber_policy"}`, "", 403},
		{"rate limit", `{"error":{"message":"cyber_policy"}}`, "", 429},
		{"identifier prefix", `{"error":{"message":"not_cyber_policy"}}`, "", 403},
		{"non-error field", `{"result":{"message":"cyber_policy"}}`, "", 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(nil)
			c.Set(cyberMessagePolicyKey, true)
			ctx := context.WithValue(t.Context(), cyberUpstreamContextKey{}, c)
			resp := &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body))}
			result := RelayErrorHandler(ctx, resp, false)
			require.NotNil(t, result)
			assert.Equal(t, tc.source, c.GetString(cyberMatchSourceKey))
			assert.Equal(t, tc.source != "", result.GetErrorCode() == "cyber_policy")
		})
	}
}

func TestRelayErrorHandlerKeepsInvalidJSONBodyInDebugLog(t *testing.T) {
	withDebugEnabled(t, true)

	body := strings.Repeat("e", common.LocalLogContentLimit+256)
	var logBuffer bytes.Buffer

	common.LogWriterMu.Lock()
	oldWriter := gin.DefaultErrorWriter
	gin.DefaultErrorWriter = &logBuffer
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		common.LogWriterMu.Lock()
		gin.DefaultErrorWriter = oldWriter
		common.LogWriterMu.Unlock()
	})

	resp := &http.Response{
		StatusCode: http.StatusInternalServerError,
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	newAPIError := RelayErrorHandler(context.Background(), resp, false)

	require.NotNil(t, newAPIError)
	require.NotContains(t, logBuffer.String(), "[truncated")
	require.Contains(t, logBuffer.String(), body)
}

func withDebugEnabled(t *testing.T, enabled bool) {
	t.Helper()

	oldDebug := common.DebugEnabled
	common.DebugEnabled = enabled
	t.Cleanup(func() {
		common.DebugEnabled = oldDebug
	})
}
