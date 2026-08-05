package conversationaudit

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestCaptureDiagnosticLogsOnlySafeMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest("POST", "/v1/responses", strings.NewReader("confidential user input"))
	context.Set(common.RequestIdKey, "request-id\nwith-newline")

	runtimeState.Lock()
	previousConfig := runtimeState.config
	runtimeState.config.DiagnosticsEnabled = true
	runtimeState.Unlock()
	t.Cleanup(func() {
		runtimeState.Lock()
		runtimeState.config = previousConfig
		runtimeState.Unlock()
	})

	previousLogger := auditDiagnosticLog
	var logged string
	auditDiagnosticLog = func(value string) { logged = value }
	t.Cleanup(func() { auditDiagnosticLog = previousLogger })

	logCaptureDiagnostic(context, protocolOpenAIResponses, "skipped", "request_parse_limit_exceeded", "not_applicable", 4*1024*1024+1, 0, 0)

	assert.Contains(t, logged, "event=skipped")
	assert.Contains(t, logged, "reason=request_parse_limit_exceeded")
	assert.Contains(t, logged, "request_id=request-id_with-newline")
	assert.Contains(t, logged, "request_bytes=4194305")
	assert.NotContains(t, logged, "confidential user input")
	assert.NotContains(t, logged, "\nwith-newline")
}
