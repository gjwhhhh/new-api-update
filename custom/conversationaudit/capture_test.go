package conversationaudit

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLocalSensitiveRejectionAuditKeepsPrivateEvidence(t *testing.T) {
	db := setupResponseTurnTest(t)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.UserSession{}, &model.Log{}, &model.AuditLog{}))
	oldLogDB, oldLogging := model.LOG_DB, constant.ErrorLogEnabled
	oldRedis := common.RedisEnabled
	oldLogType := common.LogDatabaseType()
	model.LOG_DB, constant.ErrorLogEnabled = db, false
	common.RedisEnabled = false
	const word = "fixture-trigger"
	t.Cleanup(func() {
		model.LOG_DB, constant.ErrorLogEnabled = oldLogDB, oldLogging
		common.RedisEnabled = oldRedis
		common.SetLogDatabaseType(oldLogType)
	})
	if dsn := os.Getenv("TEST_SENSITIVE_LOG_DSN"); dsn != "" {
		t.Setenv("LOG_SQL_DSN", dsn)
		oldMaster := common.IsMasterNode
		common.IsMasterNode = true
		t.Cleanup(func() { common.IsMasterNode = oldMaster })
		require.NoError(t, model.InitLogDB())
		logConn, err := model.LOG_DB.DB()
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, logConn.Close()) })
	}
	user := model.User{Username: "fixture-user", AffCode: "fix1", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, AuthVersion: 1}
	admin := model.User{Username: "fixture-admin", AffCode: "fix2", Role: common.RoleAdminUser, Status: common.UserStatusEnabled, AuthVersion: 1}
	require.NoError(t, db.Create(&user).Error)
	require.NoError(t, db.Create(&admin).Error)
	userSession, err := service.CreateLoginSession(user.Id, "password", "127.0.0.1", "fixture")
	require.NoError(t, err)
	adminSession, err := service.CreateLoginSession(admin.Id, "password", "127.0.0.1", "fixture")
	require.NoError(t, err)
	engine := gin.New()
	engine.POST("/v1/responses", func(c *gin.Context) {
		c.Set("id", user.Id)
		c.Set(common.RequestIdKey, c.GetHeader("X-Test-Request-ID"))
	}, service.RelayFailureLog(""), CaptureMiddleware(), func(c *gin.Context) {
		var request dto.OpenAIResponsesRequest
		require.NoError(t, c.ShouldBindJSON(&request))
		apiErr := service.CheckRelayPromptSensitive(c, &request, request.GetTokenCountMeta().CombineText, []string{word}, "all", 0)
		require.NotNil(t, apiErr)
		service.SetRelayFinalError(c, apiErr)
		c.JSON(apiErr.StatusCode, gin.H{"error": apiErr.ToOpenAIError()})
	})
	registerRoutes(engine.Group("/api"))
	for _, tc := range []struct {
		id, contentType, input, source, captureError, userText, matchContent string
	}{
		{"tool", "application/json", `{"previous_response_id":"resp_parent","input":[{"type":"function_call_output","output":"fixture-trigger private tool result"}]}`, "tool_context", "no_visible_text", "", "fixture-trigger private tool result"},
		{"tool-definition", "application/json", `{"input":[{"type":"function_call_output","output":"private tool result"}],"tools":[{"description":"fixture-trigger private definition"}]}`, "tool_definitions", "no_new_user_text", "", `[{"description":"fixture-trigger private definition"}]`},
		{"user-before-tool", "application/json", `{"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"please continue this turn"}]},{"type":"function_call_output","output":"fixture-trigger private tool result"}]}`, "tool_context", "", "please continue this turn", "fixture-trigger private tool result"},
		{"current-user", "application/json", `{"input":"fixture-trigger current request"}`, "current_user_text", "", word + " current request", word + " current request"},
		{"unsupported-content-type", "application/octet-stream", `{"input":"fixture-trigger current request"}`, "current_user_text", "unsupported_request_protocol", "", word + " current request"},
	} {
		t.Run(tc.id, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(tc.input))
			request.Header.Set("Content-Type", tc.contentType)
			request.Header.Set("X-Test-Request-ID", tc.id)
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, request)
			assert.Equal(t, http.StatusInternalServerError, response.Code)
			assert.Contains(t, response.Body.String(), "sensitive_words_detected")
			assert.NotContains(t, response.Body.String(), word)

			var log model.Log
			require.NoError(t, model.LOG_DB.Where("request_id = ?", tc.id).Take(&log).Error)
			assert.Equal(t, "sensitive_words_detected", log.ErrorCode)
			assert.Contains(t, log.Other, `"error_source":"local"`)
			assert.Contains(t, log.Other, `"prompt_text_filter_scope":"all"`)
			assert.NotContains(t, log.Content+log.Other, word)

			var audit ConversationAudit
			require.NoError(t, db.Where("request_id = ?", tc.id).First(&audit).Error)
			assert.Equal(t, "failed", audit.Status)
			assert.Equal(t, tc.captureError, audit.CaptureError)
			assert.NotEmpty(t, audit.RequestCiphertext)
			assert.NotContains(t, audit.RequestCiphertext, word)

			endpoint := "/api/custom/conversation-audits/" + tc.id
			for _, denied := range []struct {
				token  string
				status int
			}{{"", http.StatusUnauthorized}, {userSession.AccessToken, http.StatusForbidden}} {
				deniedRequest := httptest.NewRequest(http.MethodGet, endpoint, nil)
				if denied.token != "" {
					deniedRequest.Header.Set("Authorization", "Bearer "+denied.token)
				}
				result := httptest.NewRecorder()
				engine.ServeHTTP(result, deniedRequest)
				assert.Equal(t, denied.status, result.Code)
				assert.NotContains(t, result.Body.String(), word)
			}

			detail := httptest.NewRecorder()
			detailRequest := httptest.NewRequest(http.MethodGet, endpoint, nil)
			detailRequest.Header.Set("Authorization", "Bearer "+adminSession.AccessToken)
			engine.ServeHTTP(detail, detailRequest)
			require.Equal(t, http.StatusOK, detail.Code, detail.Body.String())
			assert.Contains(t, detail.Header().Get("Cache-Control"), "no-store")
			var payload struct {
				Data struct {
					Request string                      `json:"request"`
					Match   service.RelaySensitiveMatch `json:"local_sensitive_match"`
				} `json:"data"`
			}
			require.NoError(t, common.Unmarshal(detail.Body.Bytes(), &payload))
			assert.Equal(t, "local_prompt_sensitive_words", payload.Data.Match.Rule)
			assert.Equal(t, word, payload.Data.Match.Word)
			assert.Equal(t, tc.source, payload.Data.Match.Source)
			assert.Equal(t, tc.matchContent, payload.Data.Match.Content)
			assert.False(t, payload.Data.Match.ContentTruncated)
			if tc.userText != "" {
				assert.Equal(t, tc.userText, decodedPayloadField(t, payload.Data.Request, "user_input"))
			} else {
				assert.Empty(t, payload.Data.Request)
			}
		})
	}
}

func TestCaptureCyberRejectionWithoutVisibleText(t *testing.T) {
	db := setupResponseTurnTest(t)
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set("id", 1)
		c.Set(common.RequestIdKey, "cyber-without-text")
	})
	engine.POST("/v1/responses", CaptureMiddleware(), func(c *gin.Context) {
		service.SetRelayFinalError(c, service.CyberPolicyError(http.StatusForbidden))
		c.Status(http.StatusForbidden)
	})
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"input":[{"type":"function_call_output","call_id":"test","output":"not user text"}]}`))
	request.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(httptest.NewRecorder(), request)
	var audit ConversationAudit
	require.NoError(t, db.Where("request_id = ?", "cyber-without-text").First(&audit).Error)
	assert.Equal(t, "failed", audit.Status)
	assert.Equal(t, "cyber_policy", audit.ErrorCode)
	assert.Equal(t, "no_new_user_text", audit.CaptureError)
	assert.Empty(t, audit.RequestCiphertext)
	assert.Empty(t, audit.ResponseCiphertext)
}

func TestCaptureCyberRejectionBackfillsUserTextBeforeToolOutput(t *testing.T) {
	db := setupResponseTurnTest(t)
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set("id", 1)
		c.Set(common.RequestIdKey, "cyber-with-user-text")
	})
	engine.POST("/v1/responses", CaptureMiddleware(), func(c *gin.Context) {
		service.SetRelayFinalError(c, service.CyberPolicyError(http.StatusForbidden))
		c.Status(http.StatusForbidden)
	})
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"keep this user input"}]},{"type":"function_call_output","call_id":"test","output":"tool result"}]}`))
	request.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(httptest.NewRecorder(), request)

	var audit ConversationAudit
	require.NoError(t, db.Where("request_id = ?", "cyber-with-user-text").First(&audit).Error)
	assert.Equal(t, "failed", audit.Status)
	assert.Equal(t, "cyber_policy", audit.ErrorCode)
	assert.Empty(t, audit.CaptureError)
	assert.Equal(t, "keep this user input", decodedPayloadField(t, mustDecryptAuditContent(t, &audit, "request"), "user_input"))
	assert.Empty(t, audit.ResponseCiphertext)
}

func TestCaptureSuccessfulResponsesBackfillsUserTextBeforeToolOutput(t *testing.T) {
	db := setupResponseTurnTest(t)
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set("id", 1)
		c.Set(common.RequestIdKey, "successful-user-before-tool")
	})
	engine.POST("/v1/responses", CaptureMiddleware(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"id":     "resp-successful-tool-turn",
			"status": "completed",
			"output": []gin.H{{"type": "function_call", "name": "lookup"}},
		})
	})
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"successful user input"}]},{"type":"function_call_output","call_id":"test","output":"tool result"}]}`))
	request.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(httptest.NewRecorder(), request)

	var audit ConversationAudit
	require.NoError(t, db.Where("request_id = ?", "successful-user-before-tool").First(&audit).Error)
	assert.Equal(t, "completed", audit.Status)
	assert.Equal(t, "successful user input", decodedPayloadField(t, mustDecryptAuditContent(t, &audit, "request"), "user_input"))
	assert.Empty(t, audit.ResponseCiphertext)
}

func TestCaptureCyberContinuationKeepsParentUserText(t *testing.T) {
	db := setupResponseTurnTest(t)
	root := persist(
		newResponseTurnContext("cyber-parent", 1),
		capturePayloadJSON(t, capturePayload{SchemaVersion: 2, CaptureMode: "latest_user_text", UserInput: "parent user input"}),
		"",
		"",
		"",
		false,
		false,
		false,
		"completed",
		http.StatusOK,
		responseIdentity{protocol: protocolOpenAIResponses, responseID: "resp-cyber-parent"},
	)
	require.NotNil(t, root)

	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set("id", 1)
		c.Set(common.RequestIdKey, "cyber-continuation")
	})
	engine.POST("/v1/responses", CaptureMiddleware(), func(c *gin.Context) {
		service.SetRelayFinalError(c, service.CyberPolicyError(http.StatusForbidden))
		c.Status(http.StatusForbidden)
	})
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"previous_response_id":"resp-cyber-parent","input":[{"type":"function_call_output","call_id":"test","output":"tool result"}]}`))
	request.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(httptest.NewRecorder(), request)

	var segment ConversationAuditResponseSegment
	require.NoError(t, db.Where("request_id = ?", "cyber-continuation").First(&segment).Error)
	assert.Equal(t, root.ID, segment.AuditID)
	assert.Zero(t, segment.ResponseLength)

	var audit ConversationAudit
	require.NoError(t, db.First(&audit, root.ID).Error)
	assert.Equal(t, "failed", audit.Status)
	assert.Equal(t, "cyber_policy", audit.ErrorCode)
	assert.Equal(t, "parent user input", decodedPayloadField(t, mustDecryptAuditContent(t, &audit, "request"), "user_input"))
}

func TestCaptureResponsesWSFailuresKeepsOnlyFailedSummary(t *testing.T) {
	db := setupResponseTurnTest(t)
	engine := gin.New()
	engine.POST("/v1/responses", func(c *gin.Context) {
		c.Set("id", 1)
		c.Set(common.RequestIdKey, c.GetHeader("X-Test-Request-ID"))
	}, CaptureResponsesWSFailures(), func(c *gin.Context) {
		service.ObserveRelayAuditFrame(c, []byte(`{"type":"response.output_text.delta","delta":"visible reply"}`))
		if c.GetString(common.RequestIdKey) == "failed-ws-turn" {
			service.ObserveRelayAuditFrame(c, []byte(`{"type":"response.failed","response":{"id":"failed-turn","error":{"code":"cyber_policy","message":"not visible"}}}`))
			service.SetRelayFinalError(c, service.CyberPolicyError(http.StatusForbidden))
		}
	})
	for _, id := range []string{"failed-ws-turn", "successful-ws-turn"} {
		request := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"input":"turn input"}`))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-Test-Request-ID", id)
		engine.ServeHTTP(httptest.NewRecorder(), request)
	}
	var audits []ConversationAudit
	require.NoError(t, db.Find(&audits).Error)
	require.Len(t, audits, 1)
	assert.Equal(t, "failed-ws-turn", audits[0].RequestID)
	assert.Equal(t, "cyber_policy", audits[0].ErrorCode)
	assert.Equal(t, "visible reply", decodedPayloadField(t, mustDecryptAuditContent(t, &audits[0], "response"), "assistant_text"))
}

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
	assert.Equal(t, captureErrorRequestParseLimitExceeded, captured.captureError)
}

func TestCaptureRequestSkipsOversizedContentLengthWithoutReadingBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", http.NoBody)
	request.Body = io.NopCloser(errorReader{err: io.ErrUnexpectedEOF})
	request.ContentLength = 8 << 20
	request.Header.Set("Content-Type", "application/json")
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = request

	captured := captureRequest(context, protocolOpenAIChat, 1024, defaultMaxContentBytes, false)
	assert.Empty(t, captured.body)
	assert.Equal(t, captureErrorRequestParseLimitExceeded, captured.captureError)
	assert.Equal(t, 8<<20, captured.requestBytes)
	_, exists := context.Get(common.KeyBodyStorage)
	assert.False(t, exists)
}

type errorReader struct {
	err error
}

func (r errorReader) Read([]byte) (int, error) {
	return 0, r.err
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

	collector.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\"}}\n\n"), true)
	body, _, _, captureError := collector.Finalize()
	require.Empty(t, captureError)
	assert.Equal(t, "first\nsecond", decodedPayloadField(t, body, "assistant_text"))
}

func TestResponsesSSECompletesVisibleTextWithoutReplacingDeltas(t *testing.T) {
	const parentPrefix = `{"type":"response.output_text.delta","delta":"第一段"}`
	const textDone = `{"type":"response.output_text.done","text":"第一段结束"}`
	const completed = `{"type":"response.completed","response":{"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"第一段结束"}]}]}}`
	for _, tc := range []struct {
		name, text, status string
		events             []string
	}{
		{name: "text done without deltas", events: []string{textDone}, text: "第一段结束", status: "incomplete"},
		{name: "text done fills a partial delta", events: []string{parentPrefix, textDone}, text: "第一段结束", status: "incomplete"},
		{name: "terminal fills a partial delta", events: []string{parentPrefix, completed}, text: "第一段结束", status: "completed"},
		{name: "terminal does not duplicate text done", events: []string{parentPrefix, textDone, completed}, text: "第一段结束", status: "completed"},
		{name: "conflicting final text preserves delta", events: []string{`{"type":"response.output_text.delta","delta":"already sent"}`, textDone, completed}, text: "already sent", status: "completed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			collector := newResponseCollector(protocolOpenAIResponses, defaultMaxContentBytes, defaultMaxParseBytes, false)
			for _, event := range tc.events {
				collector.Write([]byte("data: "+event+"\n\n"), true)
			}
			captured := collector.FinalizeCapture()
			assert.Equal(t, tc.status, captured.status)
			assert.Equal(t, tc.text, decodedPayloadField(t, captured.body, "assistant_text"))
			if tc.status == "incomplete" {
				assert.Equal(t, "response_terminal_missing", captured.captureError)
			} else {
				assert.Empty(t, captured.captureError)
			}
		})
	}
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
