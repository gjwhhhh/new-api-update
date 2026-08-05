package conversationaudit

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestCaptureRequestRecognizesOnlyToolContinuationWithPreviousResponseID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	request.Header.Set("Content-Type", "application/json")
	context.Request = request

	continuationRaw := []byte(`{"previous_response_id":"resp_parent","input":[{"type":"function_call_output","call_id":"call_1","output":"private tool output"}]}`)
	storage, err := common.CreateBodyStorage(continuationRaw)
	require.NoError(t, err)
	t.Cleanup(func() { _ = storage.Close() })
	context.Set(common.KeyBodyStorage, storage)

	captured := captureRequest(context, protocolOpenAIResponses, defaultMaxParseBytes, defaultMaxContentBytes)
	assert.Empty(t, captured.body)
	assert.Empty(t, captured.captureError)
	assert.Equal(t, "resp_parent", captured.continuationParentResponseID)

	newUserRaw := []byte(`{"previous_response_id":"resp_parent","input":[{"role":"user","content":[{"type":"input_text","text":"a new question"}]}]}`)
	storage, err = common.CreateBodyStorage(newUserRaw)
	require.NoError(t, err)
	t.Cleanup(func() { _ = storage.Close() })
	context.Set(common.KeyBodyStorage, storage)

	captured = captureRequest(context, protocolOpenAIResponses, defaultMaxParseBytes, defaultMaxContentBytes)
	assert.Equal(t, "", captured.continuationParentResponseID)
	assert.Equal(t, "a new question", decodedPayloadField(t, captured.body, "user_input"))
}

func TestResponsesCollectorCapturesProviderResponseID(t *testing.T) {
	collector := newResponseCollector(protocolOpenAIResponses, defaultMaxContentBytes, defaultMaxParseBytes)
	collector.Write([]byte("data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_stream\"}}\n\n"), true)
	collector.Write([]byte("data: {\"type\":\"response.output_text.delta\",\"output_index\":0,\"content_index\":0,\"delta\":\"visible\"}\n\n"), true)

	captured := collector.FinalizeCapture()
	assert.Equal(t, "resp_stream", captured.providerResponseID)
	assert.Equal(t, "visible", decodedPayloadField(t, captured.body, "assistant_text"))

	collector = newResponseCollector(protocolOpenAIResponses, defaultMaxContentBytes, defaultMaxParseBytes)
	collector.Write([]byte(`{"id":"resp_json","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"complete"}]}]}`), false)
	captured = collector.FinalizeCapture()
	assert.Equal(t, "resp_json", captured.providerResponseID)
	assert.Equal(t, "complete", decodedPayloadField(t, captured.body, "assistant_text"))
}

func TestResponsesContinuationAppendsToSameUserTurn(t *testing.T) {
	db := setupResponseTurnTest(t)

	rootContext := newResponseTurnContext("root-request", 1)
	rootRequest := capturePayloadJSON(t, capturePayload{
		SchemaVersion: 2,
		CaptureMode:   "latest_user_text",
		UserInput:     "xxxxx",
	})
	rootResponse := capturePayloadJSON(t, capturePayload{
		SchemaVersion: 2,
		CaptureMode:   "assistant_text",
		AssistantText: "first visible part",
	})
	rootAudit := persist(rootContext, rootRequest, rootResponse, "", "", false, false, "completed", http.StatusOK)
	require.NotNil(t, rootAudit)
	registerResponseLink(rootAudit, protocolOpenAIResponses, "resp_root")
	runtimeState.Lock()
	runtimeState.config.ActiveKeyVersion = "v2"
	runtimeState.keys["v2"] = []byte("abcdefghijklmnopqrstuvwxyz123456")
	runtimeState.Unlock()

	continuationContext := newResponseTurnContext("continuation-request", 1)
	continuationResponse := capturePayloadJSON(t, capturePayload{
		SchemaVersion: 2,
		CaptureMode:   "assistant_text",
		AssistantText: "final visible part",
	})
	appendResponseContinuation(
		continuationContext,
		protocolOpenAIResponses,
		"resp_root",
		continuationResponse,
		"",
		"",
		"resp_final",
		false,
		"completed",
		http.StatusOK,
	)

	var audit ConversationAudit
	require.NoError(t, db.Where("id = ?", rootAudit.ID).First(&audit).Error)
	assert.Equal(t, len(rootResponse)+len(continuationResponse), audit.ResponseLength)

	segments, err := decryptAuditResponseSegments(&audit)
	require.NoError(t, err)
	require.Equal(t, []string{continuationResponse}, segments)
	assert.Equal(t, "final visible part", decodedPayloadField(t, segments[0], "assistant_text"))
	var segment ConversationAuditResponseSegment
	require.NoError(t, db.Where("audit_id = ?", rootAudit.ID).First(&segment).Error)
	assert.Equal(t, "v2", segment.KeyVersion)

	finalLink, err := findResponseLink(protocolOpenAIResponses, 1, "resp_final")
	require.NoError(t, err)
	require.NotNil(t, finalLink)
	assert.Equal(t, rootAudit.ID, finalLink.AuditID)

	var link ConversationAuditResponseLink
	require.NoError(t, db.Where("audit_id = ?", rootAudit.ID).First(&link).Error)
	assert.NotContains(t, link.ResponseIDHash, "resp_root")

	otherUserContext := newResponseTurnContext("cross-user-request", 2)
	appendResponseContinuation(
		otherUserContext,
		protocolOpenAIResponses,
		"resp_final",
		capturePayloadJSON(t, capturePayload{SchemaVersion: 2, CaptureMode: "assistant_text", AssistantText: "must not append"}),
		"",
		"",
		"resp_cross_user",
		false,
		"completed",
		http.StatusOK,
	)
	segments, err = decryptAuditResponseSegments(&audit)
	require.NoError(t, err)
	assert.Len(t, segments, 1)
}

func TestCaptureMiddlewareAppendsResponsesToolContinuationToRootTurn(t *testing.T) {
	db := setupResponseTurnTest(t)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		raw, err := io.ReadAll(c.Request.Body)
		require.NoError(t, err)
		storage, err := common.CreateBodyStorage(raw)
		require.NoError(t, err)
		c.Set(common.KeyBodyStorage, storage)
		c.Request.Body = io.NopCloser(strings.NewReader(string(raw)))
		c.Set(common.RequestIdKey, c.GetHeader("X-Test-Request-ID"))
		c.Set("id", 1)
		c.Set("username", "audit-user")
		c.Set("token_id", 2)
		c.Set("original_model", "gpt-test")
		c.Next()
		_ = storage.Close()
	})
	router.Use(CaptureMiddleware())
	router.POST("/v1/responses", func(c *gin.Context) {
		if c.GetHeader("X-Test-Phase") == "root" {
			c.Data(http.StatusOK, "application/json", []byte(`{"id":"resp_root","output":[{"type":"function_call","id":"call_1"}]}`))
			return
		}
		c.Data(http.StatusOK, "application/json", []byte(`{"id":"resp_final","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"merged final answer"}]}]}`))
	})

	root := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"input":[{"role":"user","content":[{"type":"input_text","text":"xxxxx"}]}]}`))
	root.Header.Set("Content-Type", "application/json")
	root.Header.Set("X-Test-Request-ID", "middleware-root")
	root.Header.Set("X-Test-Phase", "root")
	router.ServeHTTP(httptest.NewRecorder(), root)

	continuation := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"previous_response_id":"resp_root","input":[{"type":"function_call_output","call_id":"call_1","output":"private tool output"}]}`))
	continuation.Header.Set("Content-Type", "application/json")
	continuation.Header.Set("X-Test-Request-ID", "middleware-continuation")
	router.ServeHTTP(httptest.NewRecorder(), continuation)

	var audits []ConversationAudit
	require.NoError(t, db.Order("id ASC").Find(&audits).Error)
	require.Len(t, audits, 1)
	assert.Equal(t, "xxxxx", decodedPayloadField(t, mustDecryptAuditContent(t, &audits[0], "request"), "user_input"))
	segments, err := decryptAuditResponseSegments(&audits[0])
	require.NoError(t, err)
	require.Len(t, segments, 1)
	assert.Equal(t, "merged final answer", decodedPayloadField(t, segments[0], "assistant_text"))
}

func TestCaptureMiddlewareStoresUnlinkedContinuationWithVisibleResponse(t *testing.T) {
	db := setupResponseTurnTest(t)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		raw, err := io.ReadAll(c.Request.Body)
		require.NoError(t, err)
		storage, err := common.CreateBodyStorage(raw)
		require.NoError(t, err)
		c.Set(common.KeyBodyStorage, storage)
		c.Request.Body = io.NopCloser(strings.NewReader(string(raw)))
		c.Set(common.RequestIdKey, c.GetHeader("X-Test-Request-ID"))
		c.Set("id", 1)
		c.Set("username", "audit-user")
		c.Set("token_id", 2)
		c.Set("original_model", "gpt-test")
		c.Next()
		_ = storage.Close()
	})
	router.Use(CaptureMiddleware())
	router.POST("/v1/responses", func(c *gin.Context) {
		if c.GetHeader("X-Test-Phase") == "empty" {
			c.Data(http.StatusOK, "application/json", []byte(`{"id":"resp_empty","output":[{"type":"function_call","id":"call_empty"}]}`))
			return
		}
		if c.GetHeader("X-Test-Phase") == "orphan" {
			c.Data(http.StatusOK, "application/json", []byte(`{"id":"resp_orphan","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"visible orphan response"}]}]}`))
			return
		}
		c.Data(http.StatusOK, "application/json", []byte(`{"id":"resp_final","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"visible appended response"}]}]}`))
	})

	empty := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"previous_response_id":"resp_unknown_empty","input":[{"type":"function_call_output","call_id":"call_empty","output":"private empty tool output"}]}`))
	empty.Header.Set("Content-Type", "application/json")
	empty.Header.Set("X-Test-Request-ID", "empty-continuation")
	empty.Header.Set("X-Test-Phase", "empty")
	router.ServeHTTP(httptest.NewRecorder(), empty)

	var emptyCount int64
	require.NoError(t, db.Model(&ConversationAudit{}).Count(&emptyCount).Error)
	assert.Zero(t, emptyCount)

	orphan := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"previous_response_id":"resp_before_audit_release","input":[{"type":"function_call_output","call_id":"call_1","output":"private tool output"}]}`))
	orphan.Header.Set("Content-Type", "application/json")
	orphan.Header.Set("X-Test-Request-ID", "unlinked-continuation")
	orphan.Header.Set("X-Test-Phase", "orphan")
	router.ServeHTTP(httptest.NewRecorder(), orphan)

	var audits []ConversationAudit
	require.NoError(t, db.Order("id ASC").Find(&audits).Error)
	require.Len(t, audits, 1)
	assert.Zero(t, audits[0].RequestLength)
	assert.Positive(t, audits[0].ResponseLength)
	assert.Equal(t, "unlinked_response_continuation", audits[0].CaptureError)
	assert.Equal(t, "visible orphan response", decodedPayloadField(t, mustDecryptAuditContent(t, &audits[0], "response"), "assistant_text"))

	link, err := findResponseLink(protocolOpenAIResponses, 1, "resp_orphan")
	require.NoError(t, err)
	require.NotNil(t, link)
	assert.Equal(t, audits[0].ID, link.AuditID)

	continuation := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"previous_response_id":"resp_orphan","input":[{"type":"function_call_output","call_id":"call_2","output":"private next tool output"}]}`))
	continuation.Header.Set("Content-Type", "application/json")
	continuation.Header.Set("X-Test-Request-ID", "linked-continuation")
	router.ServeHTTP(httptest.NewRecorder(), continuation)

	require.NoError(t, db.Order("id ASC").Find(&audits).Error)
	require.Len(t, audits, 1)
	segments, err := decryptAuditResponseSegments(&audits[0])
	require.NoError(t, err)
	require.Len(t, segments, 1)
	assert.Equal(t, "visible appended response", decodedPayloadField(t, segments[0], "assistant_text"))
}

func setupResponseTurnTest(t *testing.T) *gorm.DB {
	t.Helper()
	db := openConversationAuditMigrationTestDB(t)
	require.NoError(t, migrateConversationAuditTables(db))

	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB })

	runtimeState.Lock()
	previousConfig := runtimeState.config
	previousKeys := runtimeState.keys
	runtimeState.config = runtimeConfig{
		Enabled:          true,
		RetentionDays:    30,
		MaxContentBytes:  defaultMaxContentBytes,
		MaxParseBytes:    defaultMaxParseBytes,
		ActiveKeyVersion: "v1",
	}
	runtimeState.keys = map[string][]byte{"v1": []byte("12345678901234567890123456789012")}
	runtimeState.Unlock()
	t.Cleanup(func() {
		runtimeState.Lock()
		runtimeState.config = previousConfig
		runtimeState.keys = previousKeys
		runtimeState.Unlock()
	})
	return db
}

func newResponseTurnContext(requestID string, userID int) *gin.Context {
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Set(common.RequestIdKey, requestID)
	context.Set("id", userID)
	context.Set("username", "audit-user")
	context.Set("token_id", 2)
	context.Set("original_model", "gpt-test")
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	return context
}

func capturePayloadJSON(t *testing.T, payload capturePayload) string {
	t.Helper()
	body, truncated, err := marshalCapturePayload(payload, defaultMaxContentBytes)
	require.NoError(t, err)
	require.False(t, truncated)
	return body
}

func mustDecryptAuditContent(t *testing.T, audit *ConversationAudit, kind string) string {
	t.Helper()
	content, err := decryptAuditContent(audit, kind)
	require.NoError(t, err)
	return content
}
