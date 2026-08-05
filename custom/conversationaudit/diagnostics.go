package conversationaudit

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

var auditDiagnosticLog = common.SysLog

// logCaptureDiagnostic records capture decisions without ever logging request
// or response bodies, provider response IDs, credentials, or tool payloads.
// It is intentionally controlled only by an environment variable because it
// can be high-volume on busy relay instances.
func logCaptureDiagnostic(c *gin.Context, protocol conversationProtocol, event, reason, linkStatus string, requestBytes, capturedRequestBytes, capturedResponseBytes int) {
	if !currentConfig().DiagnosticsEnabled {
		return
	}
	auditDiagnosticLog(fmt.Sprintf(
		"conversation audit diagnostic event=%s reason=%s link_status=%s request_id=%s path=%s protocol=%s http_status=%d request_bytes=%s captured_request_bytes=%d captured_response_bytes=%d",
		safeDiagnosticValue(event),
		safeDiagnosticValue(reason),
		safeDiagnosticValue(linkStatus),
		safeDiagnosticValue(c.GetString(common.RequestIdKey)),
		safeDiagnosticValue(c.Request.URL.Path),
		protocol.String(),
		c.Writer.Status(),
		formatDiagnosticSize(requestBytes),
		capturedRequestBytes,
		capturedResponseBytes,
	))
}

func formatDiagnosticSize(size int) string {
	if size < 0 {
		return "unknown"
	}
	return fmt.Sprintf("%d", size)
}

func safeDiagnosticValue(value string) string {
	if value == "" {
		return "none"
	}
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("._/-", r) {
			return r
		}
		return '_'
	}, value)
}

func (protocol conversationProtocol) String() string {
	switch protocol {
	case protocolOpenAIChat:
		return "openai_chat"
	case protocolOpenAIResponses:
		return "openai_responses"
	case protocolClaude:
		return "claude"
	case protocolGemini:
		return "gemini"
	case protocolLegacyCompletion:
		return "legacy_completion"
	default:
		return "unknown"
	}
}
