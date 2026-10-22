package conversationaudit

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

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

	captured := captureRequest(context, protocolOpenAIResponses, defaultMaxParseBytes, defaultMaxContentBytes, false)
	assert.Empty(t, captured.body)
	assert.Empty(t, captured.captureError)
	assert.Equal(t, "resp_parent", captured.continuationParentResponseID)

	newUserRaw := []byte(`{"previous_response_id":"resp_parent","input":[{"role":"user","content":[{"type":"input_text","text":"a new question"}]}]}`)
	storage, err = common.CreateBodyStorage(newUserRaw)
	require.NoError(t, err)
	t.Cleanup(func() { _ = storage.Close() })
	context.Set(common.KeyBodyStorage, storage)

	captured = captureRequest(context, protocolOpenAIResponses, defaultMaxParseBytes, defaultMaxContentBytes, false)
	assert.Equal(t, "", captured.continuationParentResponseID)
	assert.Equal(t, "a new question", decodedPayloadField(t, captured.body, "user_input"))
}

func TestResponsesCollectorCapturesProviderResponseID(t *testing.T) {
	collector := newResponseCollector(protocolOpenAIResponses, defaultMaxContentBytes, defaultMaxParseBytes, false)
	collector.Write([]byte("data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_stream\"}}\n\n"), true)
	collector.Write([]byte("data: {\"type\":\"response.output_text.delta\",\"output_index\":0,\"content_index\":0,\"delta\":\"visible\"}\n\n"), true)

	captured := collector.FinalizeCapture()
	assert.Equal(t, "resp_stream", captured.providerResponseID)
	assert.Equal(t, "visible", decodedPayloadField(t, captured.body, "assistant_text"))

	collector = newResponseCollector(protocolOpenAIResponses, defaultMaxContentBytes, defaultMaxParseBytes, false)
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
	rootAudit := persist(rootContext, rootRequest, rootResponse, "", "", false, false, false, "completed", http.StatusOK, responseIdentity{protocol: protocolOpenAIResponses, responseID: "resp_root"})
	require.NotNil(t, rootAudit)
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

func TestCaptureMiddlewareFullPayloadPersistsOriginalTransaction(t *testing.T) {
	db := setupResponseTurnTest(t)
	runtimeState.Lock()
	runtimeState.config.CaptureFullPayload = true
	runtimeState.Unlock()

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
		c.Set("original_model", "claude-test")
		c.Next()
		_ = storage.Close()
	})
	router.Use(CaptureMiddleware())
	router.POST("/v1/messages", func(c *gin.Context) {
		c.Data(http.StatusOK, "application/json", []byte(`{"id":"msg_test","content":[{"type":"thinking","thinking":"private"},{"type":"text","text":"visible"}]}`))
	})

	requestBody := `{"system":"keep this","messages":[{"role":"user","content":"hello"}],"metadata":{"trace":"full"}}`
	request := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(requestBody))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Test-Request-ID", "full-payload-request")
	router.ServeHTTP(httptest.NewRecorder(), request)

	var audit ConversationAudit
	require.NoError(t, db.Where("request_id = ?", "full-payload-request").First(&audit).Error)
	assert.True(t, audit.FullPayload)
	assert.Equal(t, requestBody, mustDecryptAuditContent(t, &audit, "request"))
	assert.Equal(t, `{"id":"msg_test","content":[{"type":"thinking","thinking":"private"},{"type":"text","text":"visible"}]}`, mustDecryptAuditContent(t, &audit, "response"))
}

func TestCaptureMiddlewareSkipsOversizedRequestWithoutWriterOrPersist(t *testing.T) {
	db := setupResponseTurnTest(t)
	runtimeState.Lock()
	runtimeState.config.MaxParseBytes = 2048
	runtimeState.Unlock()

	gin.SetMode(gin.TestMode)
	router := gin.New()
	var sawCaptureWriter bool
	var sawBodyStorage bool
	router.Use(CaptureMiddleware())
	router.POST("/v1/responses", func(c *gin.Context) {
		_, sawCaptureWriter = c.Writer.(*captureWriter)
		_, sawBodyStorage = common.PeekBodyStorage(c)
		c.Data(http.StatusOK, "application/json", []byte(`{"id":"resp_skip","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"visible orphan that must not persist"}]}]}`))
	})

	body := `{"input":[{"role":"user","content":[{"type":"input_text","text":"` + strings.Repeat("x", 4096) + `"}]}]}`
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Test-Request-ID", "oversized-skip")
	router.ServeHTTP(httptest.NewRecorder(), request)

	assert.False(t, sawCaptureWriter)
	assert.False(t, sawBodyStorage)
	var count int64
	require.NoError(t, db.Model(&ConversationAudit{}).Count(&count).Error)
	assert.Zero(t, count)
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
	assert.EqualValues(t, 1, emptyCount)

	orphan := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"previous_response_id":"resp_before_audit_release","input":[{"type":"function_call_output","call_id":"call_1","output":"private tool output"}]}`))
	orphan.Header.Set("Content-Type", "application/json")
	orphan.Header.Set("X-Test-Request-ID", "unlinked-continuation")
	orphan.Header.Set("X-Test-Phase", "orphan")
	router.ServeHTTP(httptest.NewRecorder(), orphan)

	var audits []ConversationAudit
	require.NoError(t, db.Order("id ASC").Find(&audits).Error)
	require.Len(t, audits, 2)
	audits = audits[1:]
	assert.Zero(t, audits[0].RequestLength)
	assert.Positive(t, audits[0].ResponseLength)
	assert.Equal(t, "pending_response_continuation", audits[0].CaptureError)
	assert.Equal(t, "visible orphan response", decodedPayloadField(t, mustDecryptAuditContent(t, &audits[0], "response"), "assistant_text"))

	link, err := findResponseLink(protocolOpenAIResponses, 1, "resp_orphan")
	require.NoError(t, err)
	assert.Nil(t, link, "unresolved continuations must not publish a root mapping")

	continuation := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"previous_response_id":"resp_orphan","input":[{"type":"function_call_output","call_id":"call_2","output":"private next tool output"}]}`))
	continuation.Header.Set("Content-Type", "application/json")
	continuation.Header.Set("X-Test-Request-ID", "linked-continuation")
	router.ServeHTTP(httptest.NewRecorder(), continuation)

	require.NoError(t, db.Order("id ASC").Find(&audits).Error)
	require.Len(t, audits, 3)
	// Once the missing parent arrives, both pending steps can be attached.
	root := persist(newResponseTurnContext("late-root", 1), capturePayloadJSON(t, capturePayload{SchemaVersion: 2, CaptureMode: "latest_user_text", UserInput: "question"}), "", "", "", false, false, false, "completed", 200, responseIdentity{protocol: protocolOpenAIResponses, responseID: "resp_before_audit_release"})
	require.NotNil(t, root)
	reconcileResponseContinuations()
	segments, err := decryptAuditResponseSegments(root)
	require.NoError(t, err)
	require.Len(t, segments, 2)
	assert.Equal(t, "visible appended response", decodedPayloadField(t, segments[1], "assistant_text"))
}

func findResponseLink(protocol conversationProtocol, userID int, parentResponseID string) (*ConversationAuditResponseLink, error) {
	db, cancel := auditWriteDB()
	defer cancel()
	return findResponseLinkDB(db, protocol, userID, parentResponseID)
}

func reconcileResponseContinuations() { reconcileResponseContinuationsDB(model.DB) }

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

func TestResponsesAuditTerminalResults(t *testing.T) {
	cases := []struct {
		name, terminal, status, code, reason string
		streaming, truncated                 bool
	}{
		{name: "stream completed", terminal: `{"type":"response.completed","response":{"id":"resp_test","status":"completed"}}`, status: "completed", streaming: true},
		{name: "stream failed", terminal: `{"type":"response.failed","response":{"id":"resp_test","status":"failed","error":{"code":"server_error","message":"private failure"}}}`, status: "failed", code: "server_error", streaming: true},
		{name: "stream incomplete", terminal: `{"type":"response.incomplete","response":{"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"}}}`, status: "incomplete", reason: "response_incomplete_max_output_tokens", streaming: true},
		{name: "stream interrupted", status: "incomplete", reason: "response_terminal_missing", streaming: true},
		{name: "stream top level error", terminal: `{"type":"error","code":"server_error","message":"private failure"}`, status: "failed", code: "server_error", streaming: true},
		{name: "truncation preserves failure", terminal: `{"type":"response.failed","response":{"error":{"type":"server_error"}}}`, status: "failed", code: "server_error", streaming: true, truncated: true},
		{name: "json failed", terminal: `{"id":"resp_test","status":"failed","error":{"code":"server_error","message":"private failure"}}`, status: "failed", code: "server_error"},
		{name: "json queued", terminal: `{"id":"resp_test","status":"queued"}`, status: "incomplete"},
		{name: "numeric error fallback", terminal: `{"id":"resp_test","status":"failed","error":{"code":500,"type":"server_error"}}`, status: "failed", code: "server_error"},
		{name: "json incomplete", terminal: `{"id":"resp_test","status":"incomplete","incomplete_details":{"reason":"max_output_tokens"}}`, status: "incomplete", reason: "response_incomplete_max_output_tokens"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := setupResponseTurnTest(t)
			if tc.truncated {
				runtimeState.Lock()
				runtimeState.config.MaxContentBytes = 128
				runtimeState.Unlock()
			}
			text := "visible partial"
			if tc.truncated {
				text = strings.Repeat("x", 256)
			}
			wire := tc.terminal
			contentType := "application/json"
			if tc.streaming {
				contentType = "text/event-stream"
				delta, err := common.Marshal(map[string]any{"type": "response.output_text.delta", "delta": text})
				require.NoError(t, err)
				wire = "data: " + string(delta) + "\n\n"
				if tc.terminal != "" {
					wire += "data: " + tc.terminal + "\n\n"
				}
			}
			engine := gin.New()
			engine.Use(func(c *gin.Context) { c.Set("id", 1); c.Set(common.RequestIdKey, "terminal-request"); c.Next() })
			engine.Use(CaptureMiddleware())
			engine.POST("/v1/responses", func(c *gin.Context) { c.Data(http.StatusOK, contentType, []byte(wire)) })
			req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"input":"question"}`))
			req.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			engine.ServeHTTP(recorder, req)
			assert.Equal(t, wire, recorder.Body.String(), "audit must not modify the client response")
			var audit ConversationAudit
			require.NoError(t, db.First(&audit).Error)
			assert.Equal(t, tc.status, audit.Status)
			assert.Equal(t, 200, audit.HTTPStatus)
			assert.Equal(t, tc.code, audit.ErrorCode)
			if tc.reason != "" {
				assert.Contains(t, audit.CaptureError, tc.reason)
			}
			assert.Equal(t, tc.truncated, audit.ResponseTruncated)
			if tc.streaming {
				assert.NotEmpty(t, mustDecryptAuditContent(t, &audit, "response"))
			}
			assert.NotContains(t, mustDecryptAuditContent(t, &audit, "response"), "private failure")
		})
	}
}

func TestResponsesImmediateContinuationReconcilesAfterRootReturns(t *testing.T) {
	db := setupResponseTurnTest(t)
	rootDelivered, releaseRoot, rootDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set("id", 1)
		c.Set(common.RequestIdKey, c.GetHeader("X-Test-Request-ID"))
		c.Next()
	})
	engine.Use(CaptureMiddleware())
	engine.POST("/v1/responses", func(c *gin.Context) {
		if c.GetHeader("X-Test-Request-ID") == "delayed-root" {
			c.Data(200, "application/json", []byte(`{"id":"resp_root","status":"completed","output":[]}`))
			c.Writer.Flush()
			close(rootDelivered)
			<-releaseRoot
			return
		}
		c.Data(200, "text/event-stream", []byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial answer\"}\n\ndata: {\"type\":\"response.failed\",\"response\":{\"id\":\"resp_final\",\"error\":{\"code\":\"server_error\"}}}\n\n"))
	})
	serve := func(id, body string) {
		req := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Test-Request-ID", id)
		engine.ServeHTTP(httptest.NewRecorder(), req)
	}
	go func() { defer close(rootDone); serve("delayed-root", `{"input":"question"}`) }()
	<-rootDelivered
	serve("fast-continuation", `{"previous_response_id":"resp_root","input":[{"type":"function_call_output","call_id":"call_1","output":"private"}]}`)
	var pending ConversationAuditPendingResponse
	require.NoError(t, db.First(&pending).Error)
	assert.NotContains(t, pending.Ciphertext, "resp_root")
	close(releaseRoot)
	<-rootDone
	// The sweep uses durable state only, as it would after a process restart.
	var workers sync.WaitGroup
	for range 2 {
		workers.Go(reconcileResponseContinuations)
	}
	workers.Wait()
	reconcileResponseContinuations()
	var audits []ConversationAudit
	require.NoError(t, db.Find(&audits).Error)
	require.Len(t, audits, 1)
	assert.Equal(t, "delayed-root", audits[0].RequestID)
	assert.Equal(t, "failed", audits[0].Status)
	assert.Equal(t, "server_error", audits[0].ErrorCode)
	segments, err := decryptAuditResponseSegments(&audits[0])
	require.NoError(t, err)
	require.Len(t, segments, 1)
	assert.Equal(t, "partial answer", decodedPayloadField(t, segments[0], "assistant_text"))
	assert.Equal(t, len(segments[0]), audits[0].ResponseLength)
	var count int64
	require.NoError(t, db.Model(&ConversationAuditPendingResponse{}).Count(&count).Error)
	assert.Zero(t, count)
}

func TestResponsesPendingChainRetryIsolationAndRotation(t *testing.T) {
	db := setupResponseTurnTest(t)
	root := persist(newResponseTurnContext("root", 1), capturePayloadJSON(t, capturePayload{SchemaVersion: 2, CaptureMode: "latest_user_text", UserInput: "question"}), "", "", "", false, false, false, "completed", 200, responseIdentity{protocol: protocolOpenAIResponses, responseID: "resp_root"})
	require.NotNil(t, root)
	// Reverse arrival order and a tool-only middle step must retain the chain.
	finalBody := capturePayloadJSON(t, capturePayload{SchemaVersion: 2, CaptureMode: "assistant_text", AssistantText: "answer"})
	final := persist(newResponseTurnContext("final", 1), "", finalBody, "", "pending_response_continuation", false, false, false, "completed", 200, responseIdentity{protocol: protocolOpenAIResponses, parentResponseID: "resp_middle", responseID: "resp_final"})
	require.NotNil(t, final)
	middle := persist(newResponseTurnContext("middle", 1), "", "", "", "pending_response_continuation", false, false, false, "completed", 200, responseIdentity{protocol: protocolOpenAIResponses, parentResponseID: "resp_root", responseID: "resp_middle"})
	require.NotNil(t, middle)
	other := persist(newResponseTurnContext("other-user", 2), "", finalBody, "", "pending_response_continuation", false, false, false, "completed", 200, responseIdentity{protocol: protocolOpenAIResponses, parentResponseID: "resp_root", responseID: "resp_other"})
	require.NotNil(t, other)
	runtimeState.Lock()
	runtimeState.config.ActiveKeyVersion = "v2"
	runtimeState.keys["v2"] = []byte("abcdefghijklmnopqrstuvwxyz123456")
	runtimeState.Unlock()
	reconcileResponseContinuations()
	// Missing predecessors remain retryable; advance the durable retry schedule
	// explicitly instead of using sleeps in a regression test.
	require.NoError(t, db.Model(&ConversationAuditPendingResponse{}).Where("audit_id = ?", final.ID).Update("next_attempt_at", time.Now().Unix()-1).Error)
	reconcileResponseContinuations()
	require.NoError(t, db.First(root, root.ID).Error)
	segments, err := decryptAuditResponseSegments(root)
	require.NoError(t, err)
	require.Equal(t, []string{finalBody}, segments)
	assert.Equal(t, len(finalBody), root.ResponseLength)
	link, err := findResponseLink(protocolOpenAIResponses, 1, "resp_final")
	require.NoError(t, err)
	require.NotNil(t, link)
	assert.Equal(t, root.ID, link.AuditID)
	var receiptCount int64
	require.NoError(t, db.Model(&ConversationAuditResponseSegment{}).Where("audit_id = ?", root.ID).Count(&receiptCount).Error)
	assert.EqualValues(t, 2, receiptCount, "empty tool step still has a deduplication receipt")
	// Duplicate delivery cannot append twice or overwrite the failure outcome.
	assert.Equal(t, continuationAppended, appendResponseContinuation(newResponseTurnContext("final", 1), protocolOpenAIResponses, "resp_root", finalBody, "", "", "resp_final", false, "completed", 200))
	require.NoError(t, db.First(root, root.ID).Error)
	assert.Equal(t, len(finalBody), root.ResponseLength)
	var pending []ConversationAuditPendingResponse
	require.NoError(t, db.Find(&pending).Error)
	require.Len(t, pending, 1)
	assert.Equal(t, other.ID, pending[0].AuditID)
}

func TestResponsesPendingMigrationPreservesReleasedData(t *testing.T) {
	db := openConversationAuditMigrationTestDB(t)
	db.Config.DisableForeignKeyConstraintWhenMigrating = false
	if db.Dialector.Name() == "sqlite" {
		require.NoError(t, db.Exec("PRAGMA foreign_keys = ON").Error)
	}
	// These are the exact audit models from the deployed revision, before the
	// new pending-response table exists.
	require.NoError(t, db.AutoMigrate(&ConversationAudit{}, &AuditSettings{}, &ConversationAuditResponseLink{}, &ConversationAuditResponseSegment{}))
	require.NoError(t, db.Create(&model.User{Id: 1, Username: "migration-user"}).Error)
	audit := ConversationAudit{RequestID: "released-request", UserID: 1, Status: "completed", KeyVersion: "v1", ResponseCiphertext: "existing-encrypted-content", CreatedAt: time.Now().Unix(), ExpiresAt: time.Now().Add(24 * time.Hour).Unix()}
	require.NoError(t, db.Create(&audit).Error)
	link := ConversationAuditResponseLink{AuditID: audit.ID, Protocol: openAIResponsesLinkProtocol, KeyVersion: "v1", ResponseIDHash: strings.Repeat("a", 64), UserID: 1, ExpiresAt: audit.ExpiresAt}
	require.NoError(t, db.Create(&link).Error)
	segment := ConversationAuditResponseSegment{AuditID: audit.ID, RequestID: "released-segment", ResponseCiphertext: "existing-segment-content", ExpiresAt: audit.ExpiresAt}
	require.NoError(t, db.Create(&segment).Error)
	require.NoError(t, migrateConversationAuditTables(db))
	require.NoError(t, migrateConversationAuditTables(db))
	var loaded ConversationAudit
	require.NoError(t, db.First(&loaded, audit.ID).Error)
	assert.Equal(t, audit.ResponseCiphertext, loaded.ResponseCiphertext)
	var loadedSegment ConversationAuditResponseSegment
	require.NoError(t, db.First(&loadedSegment, segment.ID).Error)
	assert.Equal(t, segment.ResponseCiphertext, loadedSegment.ResponseCiphertext)
	require.NoError(t, db.First(&ConversationAuditResponseLink{}, link.ID).Error)
	// Existing uniqueness and the new cascade remain effective after upgrade.
	assert.Error(t, db.Create(&ConversationAudit{RequestID: audit.RequestID, UserID: 1}).Error)
	pending := ConversationAuditPendingResponse{AuditID: audit.ID, KeyVersion: "v1", Ciphertext: "encrypted-metadata", ExpiresAt: audit.ExpiresAt}
	require.NoError(t, db.Create(&pending).Error)
	assert.Error(t, db.Create(&ConversationAuditPendingResponse{AuditID: audit.ID}).Error)
	require.NoError(t, db.Delete(&audit).Error)
	var count int64
	require.NoError(t, db.Model(&ConversationAuditPendingResponse{}).Count(&count).Error)
	assert.Zero(t, count)
	require.NoError(t, db.Model(&ConversationAuditResponseSegment{}).Count(&count).Error)
	assert.Zero(t, count)
	versionQuery := "SELECT version()"
	if db.Dialector.Name() == "sqlite" {
		versionQuery = "SELECT sqlite_version()"
	}
	var version string
	require.NoError(t, db.Raw(versionQuery).Scan(&version).Error)
	t.Logf("verified released-schema upgrade twice and cascades: %s %s", db.Dialector.Name(), version)
}

func TestResponsesPendingFailureRetainsEncryptedContent(t *testing.T) {
	db := setupResponseTurnTest(t)
	root := persist(newResponseTurnContext("root", 1), capturePayloadJSON(t, capturePayload{SchemaVersion: 2, CaptureMode: "latest_user_text", UserInput: "question"}), "", "", "", false, false, false, "completed", 200, responseIdentity{protocol: protocolOpenAIResponses, responseID: "resp_root"})
	require.NotNil(t, root)
	body := capturePayloadJSON(t, capturePayload{SchemaVersion: 2, CaptureMode: "assistant_text", AssistantText: "retain this"})
	orphan := persist(newResponseTurnContext("retry", 1), "", body, "", "pending_response_continuation", false, false, false, "completed", 200, responseIdentity{protocol: protocolOpenAIResponses, parentResponseID: "resp_root", responseID: "resp_retry"})
	require.NotNil(t, orphan)
	// Corrupt the staged ciphertext to exercise a real decryption failure. The
	// failed transaction must leave the source intact and append nothing.
	ciphertext := orphan.ResponseCiphertext
	require.NoError(t, db.Model(orphan).Update("response_ciphertext", "invalid").Error)
	reconcileResponseContinuations()
	var count int64
	require.NoError(t, db.Model(&ConversationAuditPendingResponse{}).Count(&count).Error)
	assert.EqualValues(t, 1, count)
	require.NoError(t, db.Model(&ConversationAuditResponseSegment{}).Count(&count).Error)
	assert.Zero(t, count)
	require.NoError(t, db.Model(orphan).Update("response_ciphertext", ciphertext).Error)
	require.NoError(t, db.Model(&ConversationAuditPendingResponse{}).Where("audit_id = ?", orphan.ID).Update("next_attempt_at", time.Now().Unix()-1).Error)
	reconcileResponseContinuations()
	segments, err := decryptAuditResponseSegments(root)
	require.NoError(t, err)
	assert.Equal(t, []string{body}, segments)
}

func TestResponsesAuditDatabaseFailureDoesNotChangeReply(t *testing.T) {
	db := setupResponseTurnTest(t)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())
	engine := gin.New()
	engine.Use(func(c *gin.Context) { c.Set("id", 1); c.Set(common.RequestIdKey, "failed-db"); c.Next() })
	engine.Use(CaptureMiddleware())
	const reply = `{"id":"resp_success","status":"completed","output":[]}`
	engine.POST("/v1/responses", func(c *gin.Context) { c.Data(200, "application/json", []byte(reply)) })
	request := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{"input":"question"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)
	assert.Equal(t, 200, recorder.Code)
	assert.Equal(t, reply, recorder.Body.String())
}

func TestResponsesAuditMetricsReportsPendingAndTimings(t *testing.T) {
	db := setupResponseTurnTest(t)
	orphan := persist(newResponseTurnContext("pending-metrics", 1), "", "", "", "pending_response_continuation", false, false, false, "completed", 200, responseIdentity{protocol: protocolOpenAIResponses, parentResponseID: "resp_missing", responseID: "resp_pending"})
	require.NotNil(t, orphan)
	var result struct {
		Success bool `json:"success"`
		Data    struct {
			Phases map[string]struct {
				Count int     `json:"count"`
				P95   float64 `json:"p95_ms"`
				P99   float64 `json:"p99_ms"`
			} `json:"phases"`
			PendingCount         int `json:"pending_count"`
			OldestPendingSeconds int `json:"oldest_pending_seconds"`
		} `json:"data"`
	}
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	getAuditMetrics(context)
	require.Equal(t, 200, response.Code)
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
	assert.True(t, result.Success)
	assert.Equal(t, 1, result.Data.PendingCount)
	assert.GreaterOrEqual(t, result.Data.OldestPendingSeconds, 0)
	assert.Positive(t, result.Data.Phases["database"].Count)
	assert.GreaterOrEqual(t, result.Data.Phases["database"].P99, result.Data.Phases["database"].P95)
	assert.NotContains(t, response.Body.String(), "resp_missing")
	assert.NotContains(t, response.Body.String(), "resp_pending")
	// Retention removes both the visible provisional record and retry metadata.
	expired := time.Now().Unix() - 1
	require.NoError(t, db.Model(orphan).Update("expires_at", expired).Error)
	require.NoError(t, db.Model(&ConversationAuditPendingResponse{}).Where("audit_id = ?", orphan.ID).Update("expires_at", expired).Error)
	cleanupExpired()
	var count int64
	require.NoError(t, db.Model(&ConversationAuditPendingResponse{}).Count(&count).Error)
	assert.Zero(t, count)
	require.NoError(t, db.Model(&ConversationAudit{}).Count(&count).Error)
	assert.Zero(t, count)
}
