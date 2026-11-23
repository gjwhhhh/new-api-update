package controller

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/custom/conversationaudit"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

var responsesWSTestUserSequence atomic.Int64

func setupResponsesWSRequestTest(t *testing.T) (*model.User, *model.Token) {
	t.Helper()
	previousDB := model.DB
	previousLogDB := model.LOG_DB
	previousType := common.MainDatabaseType()
	previousLogType := common.LogDatabaseType()
	previousRedis := common.RedisEnabled
	previousRDB := common.RDB
	previousMaster, previousMemory, previousSQLite := common.IsMasterNode, common.MemoryCacheEnabled, common.SQLitePath
	previousEnabled := setting.ModelRequestRateLimitEnabled
	previousDuration := setting.ModelRequestRateLimitDurationMinutes
	previousTotal := setting.ModelRequestRateLimitCount
	previousSuccess := setting.ModelRequestRateLimitSuccessCount
	setting.ModelRequestRateLimitMutex.Lock()
	previousGroups := setting.ModelRequestRateLimitGroup
	setting.ModelRequestRateLimitGroup = nil
	setting.ModelRequestRateLimitMutex.Unlock()
	t.Setenv("SQL_DSN", os.Getenv("TEST_RESPONSES_SQL_DSN"))
	t.Setenv("LOG_SQL_DSN", os.Getenv("TEST_RESPONSES_LOG_SQL_DSN"))
	common.IsMasterNode, common.MemoryCacheEnabled, common.RedisEnabled = false, false, false
	common.SQLitePath = filepath.Join(t.TempDir(), "responses.db")
	require.NoError(t, model.InitDB())
	db := model.DB
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	if strings.HasPrefix(os.Getenv("TEST_RESPONSES_LOG_SQL_DSN"), "clickhouse://") {
		common.IsMasterNode = true
	}
	require.NoError(t, model.InitLogDB())
	common.IsMasterNode = false
	if model.LOG_DB != db {
		logSQL, err := model.LOG_DB.DB()
		require.NoError(t, err)
		logSQL.SetMaxOpenConns(1)
		t.Cleanup(func() { require.NoError(t, logSQL.Close()) })
	}
	setting.ModelRequestRateLimitEnabled = false
	t.Cleanup(func() {
		model.DB = previousDB
		model.LOG_DB = previousLogDB
		common.SetDatabaseTypes(previousType, previousLogType)
		common.RedisEnabled = previousRedis
		common.RDB = previousRDB
		common.IsMasterNode, common.MemoryCacheEnabled, common.SQLitePath = previousMaster, previousMemory, previousSQLite
		setting.ModelRequestRateLimitEnabled = previousEnabled
		setting.ModelRequestRateLimitDurationMinutes = previousDuration
		setting.ModelRequestRateLimitCount = previousTotal
		setting.ModelRequestRateLimitSuccessCount = previousSuccess
		setting.ModelRequestRateLimitMutex.Lock()
		setting.ModelRequestRateLimitGroup = previousGroups
		setting.ModelRequestRateLimitMutex.Unlock()
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.UserChannelPriority{}, &model.UserSessionAffinity{}))
	// The shared in-memory limiter outlives each database fixture. Give every
	// user a separate quota bucket, including when the tests run with -count.
	user := &model.User{Id: 5062000 + int(responsesWSTestUserSequence.Add(1)), Username: "responses-ws-user", Status: common.UserStatusEnabled, Group: "default", Quota: 1000, AuthVersion: 1}
	require.NoError(t, db.Create(user).Error)
	token := &model.Token{UserId: user.Id, Key: "responseswstoken", Status: common.TokenStatusEnabled, ExpiredTime: -1, RemainQuota: 100}
	require.NoError(t, db.Create(token).Error)
	t.Cleanup(func() {
		require.NoError(t, db.Unscoped().Delete(token).Error)
		require.NoError(t, db.Unscoped().Delete(user).Error)
	})
	return user, token
}

func newResponsesWSTestRunner(t *testing.T, token *model.Token) (relay.ResponsesWSRequestRunner, *http.Request) {
	t.Helper()
	var runner relay.ResponsesWSRequestRunner
	engine := gin.New()
	require.NoError(t, engine.SetTrustedProxies([]string{"127.0.0.1"}))
	engine.GET("/v1/responses", middleware.TokenAuth(), func(c *gin.Context) {
		runner = newResponsesWSRequestRunner(c)
	})
	request := httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	request.RemoteAddr = "127.0.0.1:12345"
	request.Header.Set("Authorization", "Bearer sk-"+token.Key)
	request.Header.Set("X-Forwarded-For", "203.0.113.8")
	request.Header.Set("Connection", "Upgrade")
	request.Header.Set("Upgrade", "websocket")
	request.Header.Set("Sec-WebSocket-Key", "handshake-key")
	request.Header.Set("Sec-WebSocket-Version", "13")
	request.Header.Set("Sec-WebSocket-Extensions", "permessage-deflate")
	request.Header.Set("Sec-WebSocket-Protocol", "responses")
	request.Header.Set("Content-Length", "99")
	request.Header.Set("Content-Encoding", "gzip")
	request.Header.Set("X-Application-Header", "preserved")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code)
	require.NotNil(t, runner)
	return runner, request
}

func TestResponsesWSRequestRunnerRefreshesBillingContextAndCleansBody(t *testing.T) {
	user, token := setupResponsesWSRequestTest(t)
	allowedIP := "203.0.113.8"
	require.NoError(t, model.DB.Model(token).Update("allow_ips", allowedIP).Error)
	runner, handshake := newResponsesWSTestRunner(t, token)
	// A later upstream header mutation must not replace the client's credential.
	handshake.Header.Set("Authorization", "Bearer upstream-secret")
	handshake.Header.Set("X-Forwarded-For", "198.51.100.99")
	var storage common.BodyStorage
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"input":"first"}`))
	require.Nil(t, runner(request, "ws-first", func(c *gin.Context) *types.NewAPIError {
		assert.Equal(t, user.Id, c.GetInt("id"))
		assert.Equal(t, 100, c.GetInt("token_quota"))
		assert.Equal(t, "203.0.113.8", c.ClientIP())
		assert.Equal(t, "ws-first", c.GetString(common.RequestIdKey))
		assert.Equal(t, "ws-first", c.Request.Context().Value(common.RequestIdKey))
		for _, header := range []string{"Connection", "Upgrade", "Sec-WebSocket-Key", "Sec-WebSocket-Version", "Sec-WebSocket-Extensions", "Sec-WebSocket-Protocol", "Content-Length", "Content-Encoding"} {
			assert.Empty(t, c.GetHeader(header))
		}
		assert.Equal(t, "preserved", c.GetHeader("X-Application-Header"))
		c.Set("previous_turn", true)
		var err error
		storage, err = common.GetBodyStorage(c)
		require.NoError(t, err)
		data, err := storage.Bytes()
		require.NoError(t, err)
		assert.JSONEq(t, `{"input":"first"}`, string(data))
		return nil
	}))
	_, err := storage.Bytes()
	assert.ErrorIs(t, err, common.ErrStorageClosed)
	require.NoError(t, model.DB.Model(token).Updates(map[string]any{
		"remain_quota": 2, "model_limits_enabled": true, "model_limits": "gpt-5.1",
	}).Error)
	require.NoError(t, model.DB.Model(user).Updates(map[string]any{
		"group": "updated", "setting": `{"billing_preference":"wallet_only"}`,
	}).Error)
	request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"input":"second"}`))
	require.Nil(t, runner(request, "ws-second", func(c *gin.Context) *types.NewAPIError {
		assert.Equal(t, 2, c.GetInt("token_quota"))
		assert.True(t, c.GetBool("token_model_limit_enabled"))
		modelLimits, _ := c.Get("token_model_limit")
		assert.Equal(t, map[string]bool{"gpt-5.1": true}, modelLimits)
		assert.Equal(t, "updated", common.GetContextKeyString(c, constant.ContextKeyUsingGroup))
		userSetting, ok := common.GetContextKeyType[dto.UserSetting](c, constant.ContextKeyUserSetting)
		require.True(t, ok)
		assert.Equal(t, "wallet_only", userSetting.BillingPreference)
		assert.Equal(t, "ws-second", c.GetString(common.RequestIdKey))
		_, leaked := c.Get("previous_turn")
		assert.False(t, leaked)
		_, leaked = c.Get(common.KeyBodyStorage)
		assert.False(t, leaked)
		return nil
	}))
}

func TestResponsesWSRequestRunnerRejectsRevokedCredentials(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		update func(*testing.T, *model.User, *model.Token)
	}{
		{name: "disabled token", status: http.StatusUnauthorized, update: func(t *testing.T, _ *model.User, token *model.Token) {
			require.NoError(t, model.DB.Model(token).Update("status", common.TokenStatusDisabled).Error)
		}},
		{name: "expired token", status: http.StatusUnauthorized, update: func(t *testing.T, _ *model.User, token *model.Token) {
			require.NoError(t, model.DB.Model(token).Update("expired_time", time.Now().Add(-time.Minute).Unix()).Error)
		}},
		{name: "exhausted token", status: http.StatusUnauthorized, update: func(t *testing.T, _ *model.User, token *model.Token) {
			require.NoError(t, model.DB.Model(token).Update("remain_quota", 0).Error)
		}},
		{name: "disabled user", status: http.StatusForbidden, update: func(t *testing.T, user *model.User, _ *model.Token) {
			require.NoError(t, model.DB.Model(user).Update("status", common.UserStatusDisabled).Error)
		}},
		{name: "deleted token", status: http.StatusUnauthorized, update: func(t *testing.T, _ *model.User, token *model.Token) {
			require.NoError(t, model.DB.Delete(token).Error)
		}},
		{name: "changed IP restriction", status: http.StatusForbidden, update: func(t *testing.T, _ *model.User, token *model.Token) {
			require.NoError(t, model.DB.Model(token).Update("allow_ips", "198.51.100.0/24").Error)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			user, token := setupResponsesWSRequestTest(t)
			runner, _ := newResponsesWSTestRunner(t, token)
			require.Nil(t, runner(httptest.NewRequest(http.MethodPost, "/v1/responses", nil), "before", func(*gin.Context) *types.NewAPIError { return nil }))
			tc.update(t, user, token)
			called := false
			apiError := runner(httptest.NewRequest(http.MethodPost, "/v1/responses", nil), "after", func(*gin.Context) *types.NewAPIError {
				called = true
				return nil
			})
			require.NotNil(t, apiError)
			assert.Equal(t, tc.status, apiError.StatusCode)
			assert.False(t, called)
			assert.NotContains(t, apiError.Error(), token.Key)
		})
	}
}

func TestResponsesWSRequestRunnerUsesExistingMemoryRateLimit(t *testing.T) {
	_, token := setupResponsesWSRequestTest(t)
	setting.ModelRequestRateLimitEnabled = true
	setting.ModelRequestRateLimitDurationMinutes = 1
	setting.ModelRequestRateLimitCount = 1
	setting.ModelRequestRateLimitSuccessCount = 100
	runner, _ := newResponsesWSTestRunner(t, token)
	called := 0
	handle := func(*gin.Context) *types.NewAPIError { called++; return nil }
	require.Nil(t, runner(httptest.NewRequest(http.MethodPost, "/v1/responses", nil), "first", handle))
	apiError := runner(httptest.NewRequest(http.MethodPost, "/v1/responses", nil), "limited", handle)
	require.NotNil(t, apiError)
	assert.Equal(t, http.StatusTooManyRequests, apiError.StatusCode)
	assert.Equal(t, http.StatusText(http.StatusTooManyRequests), apiError.Error())
	assert.Equal(t, 1, called)
}

func TestResponsesWSRequestRunnerSharesRedisSuccessLimitWithHTTP(t *testing.T) {
	_, token := setupResponsesWSRequestTest(t)
	redisServer := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	common.RedisEnabled = true
	common.RDB = client
	setting.ModelRequestRateLimitEnabled = true
	setting.ModelRequestRateLimitDurationMinutes = 1
	setting.ModelRequestRateLimitCount = 0
	setting.ModelRequestRateLimitSuccessCount = 1
	runner, _ := newResponsesWSTestRunner(t, token)
	failed := types.NewErrorWithStatusCode(errors.New("invalid input"), types.ErrorCodeInvalidRequest, http.StatusBadRequest)
	assert.Same(t, failed, runner(httptest.NewRequest(http.MethodPost, "/v1/responses", nil), "failed", func(*gin.Context) *types.NewAPIError { return failed }))
	require.Nil(t, runner(httptest.NewRequest(http.MethodPost, "/v1/responses", nil), "successful", func(*gin.Context) *types.NewAPIError { return nil }))
	engine := gin.New()
	called := false
	engine.POST("/v1/responses", middleware.TokenAuth(), middleware.ModelRequestRateLimit(), func(c *gin.Context) { called = true })
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	request.Header.Set("Authorization", "Bearer sk-"+token.Key)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	assert.Equal(t, http.StatusTooManyRequests, response.Code)
	assert.False(t, called)
}

type responsesWSBillingTest struct {
	redisServer  *miniredis.Miniredis
	httpUpstream http.HandlerFunc
	httpDone     chan struct{}
	user         *model.User
	token        *model.Token
	channel      *model.Channel
	client       *websocket.Conn
	done         chan struct{}
	upstreamDone chan struct{}
	connections  atomic.Int32
	gatewayURL   string
}

func (fixture *responsesWSBillingTest) closeAndWait(t *testing.T) {
	t.Helper()
	_ = fixture.client.Close()
	select {
	case <-fixture.done:
	case <-time.After(3 * time.Second):
		t.Error("gateway handler did not stop after closing its client")
	}
	if fixture.connections.Load() > 0 {
		select {
		case <-fixture.upstreamDone:
		case <-time.After(3 * time.Second):
			t.Error("upstream connection was not closed")
		}
	}
	// Health classification is synchronous at the request boundary; only the
	// Redis write is asynchronous, see waitPerfCounters.
}

// waitPerfCounters waits for the asynchronous health samples of the fixture
// model to reach wantRequests and returns the request and success counters.
func waitPerfCounters(t *testing.T, wantRequests int64) (requests, successes int64) {
	t.Helper()
	require.Eventually(t, func() bool {
		requests, successes = 0, 0
		keys, err := common.RDB.Keys(context.Background(), "perf:ws-billing:*").Result()
		if err != nil {
			return false
		}
		for _, key := range keys {
			n, _ := common.RDB.HGet(context.Background(), key, "req").Int64()
			requests += n
			n, _ = common.RDB.HGet(context.Background(), key, "ok").Int64()
			successes += n
		}
		return requests >= wantRequests
	}, 3*time.Second, 10*time.Millisecond)
	return requests, successes
}

func newResponsesWSBillingTest(t *testing.T, expression string, handle func(*websocket.Conn, *http.Request), httpEvents ...string) *responsesWSBillingTest {
	t.Helper()
	user, token := setupResponsesWSRequestTest(t)
	previousTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = previousTimeout })
	previousBatch, previousLogs, previousCount, previousQuota := common.BatchUpdateEnabled, common.LogConsumeEnabled, constant.CountToken, common.QuotaPerUnit
	common.BatchUpdateEnabled, common.LogConsumeEnabled, constant.CountToken, common.QuotaPerUnit = false, true, false, 500000
	saved := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error {
		switch key {
		case "billing_setting.billing_mode", "billing_setting.billing_expr", "group_ratio_setting.group_ratio", "perf_metrics_setting.enabled":
			saved[key] = value
		}
		return nil
	}))
	t.Cleanup(func() {
		common.BatchUpdateEnabled, common.LogConsumeEnabled, constant.CountToken, common.QuotaPerUnit = previousBatch, previousLogs, previousCount, previousQuota
		require.NoError(t, config.GlobalConfig.LoadFromDB(saved))
	})
	expressions, err := common.Marshal(map[string]string{"ws-billing": expression})
	require.NoError(t, err)
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"billing_setting.billing_mode":    `{"ws-billing":"tiered_expr"}`,
		"billing_setting.billing_expr":    string(expressions),
		"group_ratio_setting.group_ratio": `{"default":1}`,
		"perf_metrics_setting.enabled":    "true",
	}))
	require.NoError(t, model.DB.AutoMigrate(&model.Channel{}, &model.Ability{}))
	if !common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		require.NoError(t, model.LOG_DB.AutoMigrate(&model.Log{}))
	}
	require.NoError(t, model.DB.Model(user).Updates(map[string]any{"quota": 100000, "setting": `{"billing_preference":"wallet_only"}`}).Error)
	require.NoError(t, model.DB.Model(token).Update("remain_quota", 3000).Error)

	fixture := &responsesWSBillingTest{user: user, token: token, done: make(chan struct{}), upstreamDone: make(chan struct{}), httpDone: make(chan struct{}, 1)}
	redisServer := miniredis.RunT(t)
	fixture.redisServer = redisServer
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	common.RDB, common.RedisEnabled = redisClient, true
	t.Cleanup(func() { require.NoError(t, redisClient.Close()) })
	var upstreamClosed sync.Once
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !websocket.IsWebSocketUpgrade(r) {
			if fixture.httpUpstream != nil {
				fixture.httpUpstream(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			for _, event := range httpEvents {
				_, _ = fmt.Fprintf(w, "data: %s\n\n", event)
			}
			_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}
		ws, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if !assert.NoError(t, err) {
			return
		}
		fixture.connections.Add(1)
		defer ws.Close()
		defer upstreamClosed.Do(func() { close(fixture.upstreamDone) })
		handle(ws, r)
	}))
	t.Cleanup(upstream.Close)
	channel := &model.Channel{Name: "responses-ws-upstream", Key: "upstream-first\nupstream-second", Status: common.ChannelStatusEnabled, Type: constant.ChannelTypeOpenAI, Group: "default", Models: "ws-billing", BaseURL: &upstream.URL,
		ChannelInfo: model.ChannelInfo{IsMultiKey: true, MultiKeySize: 2, MultiKeyMode: constant.MultiKeyModePolling}}
	modelMapping := `{"ws-billing":"gpt-4o"}`
	channel.ModelMapping = &modelMapping
	channel.SetSetting(dto.ChannelSettings{ResponsesWebSocketEnabled: true})
	channel.SetOtherSettings(dto.ChannelOtherSettings{AllowServiceTier: true})
	require.NoError(t, model.DB.Create(channel).Error)
	fixture.channel = channel
	require.NoError(t, model.DB.Create(&model.Ability{ChannelId: channel.Id, Model: "ws-billing", Group: "default", Enabled: true}).Error)
	t.Cleanup(func() {
		require.NoError(t, model.LOG_DB.Where("token_id = ?", token.Id).Delete(&model.Log{}).Error)
		require.NoError(t, model.DB.Where("channel_id = ?", channel.Id).Delete(&model.Ability{}).Error)
		require.NoError(t, model.DB.Delete(channel).Error)
	})
	engine := gin.New()
	var clientClosed sync.Once
	engine.GET("/v1/responses", middleware.TokenAuth(), func(c *gin.Context) {
		defer clientClosed.Do(func() { close(fixture.done) })
		c.Set(common.RequestIdKey, "responses-ws-billing")
		ResponsesWebSocket(c)
	})
	engine.POST("/v1/responses", middleware.BodyStorageCleanup(), middleware.TokenAuth(), func(c *gin.Context) {
		id := c.GetHeader("X-Test-Request-ID")
		if id == "" {
			id = "responses-http-billing"
		}
		c.Set(common.RequestIdKey, id)
	}, service.RelayFailureLog(""), conversationaudit.CaptureMiddleware(), service.CyberSessionGuard, middleware.ModelRequestRateLimit(), middleware.Distribute(), func(c *gin.Context) {
		defer func() { fixture.httpDone <- struct{}{} }()
		Relay(c, types.RelayFormatOpenAIResponses)
	})
	gateway := httptest.NewServer(engine)
	fixture.gatewayURL = gateway.URL
	t.Cleanup(gateway.Close)
	client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(gateway.URL, "http")+"/v1/responses", http.Header{"Authorization": []string{"Bearer sk-" + token.Key}})
	require.NoError(t, err)
	fixture.client = client
	t.Cleanup(func() { fixture.closeAndWait(t) })
	return fixture
}

func TestResponsesCyberSessionGuardAcrossHTTPAndWebSocket(t *testing.T) {
	const session = "019b0ac1-fbc3-7300-876a-d85d2b89c897"
	const otherSession = "0a1df426-f649-4f19-b33a-b790dd406fc8"
	const rejected = `{"type":"response.failed","response":{"id":"rejected","status":"failed","error":{"code":"cyber_policy"}}}`
	const completed = `{"type":"response.completed","response":{"id":"next","status":"completed","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`
	for _, tc := range []struct{ name, mode, ttl string }{
		{"off", "off", "30"}, {"shadow", "shadow", "30"}, {"temporary", "enforce", "30"}, {"permanent", "enforce", "0"}, {"redis unavailable", "enforce", "30"}, {"message rejection", "enforce", "30"}, {"channel scope", "enforce", "30"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Cleanup(service.InitCyberSessionGuard)
			fixture := newResponsesWSBillingTest(t, `tier("request", fixed(0.002))`, func(ws *websocket.Conn, _ *http.Request) {
				for turn := 0; ; turn++ {
					if _, _, err := ws.ReadMessage(); err != nil {
						return
					}
					event := strings.Replace(completed, `"next"`, fmt.Sprintf(`"next-%d"`, turn), 1)
					if turn == 0 {
						event = rejected
						if tc.name == "message rejection" {
							event = `{"type":"response.failed","response":{"id":"rejected","status":"failed","error":{"message":"denied (cyber_policy)"}}}`
						}
					}
					if err := ws.WriteMessage(websocket.TextMessage, []byte(event)); err != nil {
						return
					}
				}
			}, completed)
			t.Setenv("CYBER_SESSION_GUARD_MODE", tc.mode)
			t.Setenv("CYBER_SESSION_GUARD_TOKEN_IDS", fmt.Sprint(fixture.token.Id))
			t.Setenv("CYBER_SESSION_GUARD_TTL_SECONDS", tc.ttl)
			t.Setenv("CYBER_SESSION_GUARD_MATCH_MESSAGE", strconv.FormatBool(tc.name == "message rejection"))
			if tc.name == "channel scope" {
				t.Setenv("CYBER_SESSION_GUARD_SCOPE", "channel")
				t.Setenv("CYBER_SESSION_GUARD_CHANNEL_IDS", fmt.Sprint(fixture.channel.Id))
			}
			service.InitCyberSessionGuard()
			metricsBefore := service.CyberSessionGuardMetrics()
			assert.Equal(t, tc.mode, metricsBefore["mode"])
			if tc.name == "redis unavailable" {
				fixture.redisServer.SetError("ERR simulated Redis outage")
			}
			wsTurn := func(cacheKey string) map[string]any {
				body := fmt.Sprintf(`{"type":"response.create","model":"ws-billing","input":"test","prompt_cache_key":%q}`, cacheKey)
				require.NoError(t, fixture.client.WriteMessage(websocket.TextMessage, []byte(body)))
				return readResponsesWSTestEvent(t, fixture.client)
			}
			failed := wsTurn(session)
			assert.Equal(t, "response.failed", failed["type"])
			if tc.name == "message rejection" {
				assert.Equal(t, "cyber_policy", failed["response"].(map[string]any)["error"].(map[string]any)["code"])
			}
			if tc.name == "redis unavailable" {
				fixture.redisServer.SetError("")
				assert.Equal(t, "response.completed", wsTurn(session)["type"])
				return
			}
			active := tc.mode == "enforce"
			next := wsTurn(session) // Admission of this round waits for the first round's middleware.
			if !active {
				assert.Equal(t, "response.completed", next["type"])
				want := metricsBefore["would_block"].(uint64)
				if tc.mode == "shadow" {
					want++
				}
				assert.Equal(t, want, service.CyberSessionGuardMetrics()["would_block"])
				return
			}
			assert.Equal(t, float64(http.StatusForbidden), next["status"])
			require.IsType(t, map[string]any{}, next["error"])
			assert.Equal(t, "cyber_policy", next["error"].(map[string]any)["code"])
			req, err := http.NewRequest(http.MethodPost, fixture.gatewayURL+"/v1/responses", strings.NewReader(fmt.Sprintf(`{"model":"ws-billing","input":"test","prompt_cache_key":%q}`, session)))
			require.NoError(t, err)
			req.Header.Set("Authorization", "Bearer sk-"+fixture.token.Key)
			req.Header.Set("Content-Type", "application/json")
			resp, err := (&http.Client{Timeout: 3 * time.Second}).Do(req)
			require.NoError(t, err)
			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			require.NoError(t, resp.Body.Close())
			assert.Equal(t, http.StatusForbidden, resp.StatusCode)
			assert.Contains(t, string(body), `"code":"cyber_policy"`)
			if tc.ttl == "0" {
				assert.Empty(t, resp.Header.Get("Retry-After"))
			} else {
				assert.NotEmpty(t, resp.Header.Get("Retry-After"))
			}
			assert.Equal(t, "response.completed", wsTurn(otherSession)["type"])
			fixture.redisServer.FastForward(time.Hour)
			if tc.ttl == "0" {
				assert.Equal(t, float64(http.StatusForbidden), wsTurn(session)["status"])
				bans, _, err := service.ListCyberBans(t.Context(), 0)
				require.NoError(t, err)
				require.Len(t, bans, 1)
				assert.True(t, bans[0].Permanent)
				assert.NotEqual(t, session, bans[0].Fingerprint)
				require.NoError(t, service.RevokeCyberBan(t.Context(), bans[0].CyberBanTarget, bans[0].Generation))
				assert.ErrorIs(t, service.RevokeCyberBan(t.Context(), bans[0].CyberBanTarget, bans[0].Generation), service.ErrCyberBanChanged)
			}
			assert.Equal(t, "response.completed", wsTurn(session)["type"])
			fixture.closeAndWait(t)
			failures, _, err := model.GetUserLogs(fixture.user.Id, model.LogTypeUnknown, 0, 0, "", "", 0, 10, "", "", "", "cyber_session_blocked")
			require.NoError(t, err)
			require.NotEmpty(t, failures)
			assert.NotContains(t, failures[0].Other, session)
			assert.NotContains(t, failures[0].Other, "session_fingerprint", "non-admin usage logs must not expose the guard fingerprint")
		})
	}
}

func TestResponsesCyberSSEBlocksHTTPAndWrappedWebSocket(t *testing.T) {
	const session = "019b0ac1-fbc3-7300-876a-d85d2b89c897"
	const delta = `{"type":"response.output_text.delta","delta":"partial output"}`
	const rejected = `{"type":"response.failed","response":{"id":"rejected","status":"failed","error":{"type":"invalid_request","message":"denied (cyber_policy)"}}}`
	t.Cleanup(service.InitCyberSessionGuard)
	fixture := newResponsesWSBillingTest(t, `tier("request", fixed(0.002))`, func(*websocket.Conn, *http.Request) {
		t.Error("the blocked session must not reach the WebSocket upstream")
	}, delta, rejected)
	fixture.closeAndWait(t)
	key := operation_setting.CyberPolicyOptionKey
	common.OptionMapRWMutex.Lock()
	oldMap := common.OptionMap
	common.OptionMap = map[string]string{}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = oldMap
		common.OptionMapRWMutex.Unlock()
	})
	require.NoError(t, model.DB.AutoMigrate(&model.Option{}))
	require.NoError(t, model.UpdateOption(key, `{"mode":"enforce","scope":"token","token_ids":[999999],"ttl_seconds":0}`))
	require.NoError(t, model.UpdateOption(key, `{"mode":"enforce","scope":"all","ttl_seconds":0,"match_message":true}`))

	httpClient := &http.Client{Timeout: 3 * time.Second}
	post := func() (int, string) {
		t.Helper()
		payload := fmt.Sprintf(`{"model":"ws-billing","input":"test","stream":true,"prompt_cache_key":%q}`, session)
		req, err := http.NewRequest(http.MethodPost, fixture.gatewayURL+"/v1/responses", strings.NewReader(payload))
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer sk-"+fixture.token.Key)
		req.Header.Set("Content-Type", "application/json")
		response, err := httpClient.Do(req)
		require.NoError(t, err)
		body, err := io.ReadAll(response.Body)
		require.NoError(t, err)
		require.NoError(t, response.Body.Close())
		return response.StatusCode, string(body)
	}
	status, body := post()
	require.Equal(t, http.StatusOK, status)
	assert.Contains(t, body, "partial output")
	assert.Contains(t, body, `"code":"cyber_policy"`)
	assert.NotContains(t, body, "[DONE]")
	<-fixture.httpDone
	logs, total, err := model.GetUserLogs(fixture.user.Id, model.LogTypeError, 0, 0, "", "", 0, 10, "", "responses-http-billing", "", "cyber_policy")
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, logs, 1)
	assert.Contains(t, logs[0].Other, `"protocol":"sse"`)
	events, err := model.CyberEvents(t.Context(), model.CyberEventFilter{Start: 1, End: time.Now().Unix(), TokenID: fixture.token.Id, Source: "message", Action: "permanent"})
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Equal(t, "responses-http-billing", events[0].RequestID)
	assert.Len(t, events[0].Fingerprint, 64)

	status, body = post()
	require.Equal(t, http.StatusForbidden, status)
	assert.Contains(t, body, `"code":"cyber_policy"`)
	assert.Empty(t, fixture.httpDone, "the blocked request must not enter the HTTP relay")

	client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(fixture.gatewayURL, "http")+"/v1/responses", http.Header{"Authorization": []string{"Bearer sk-" + fixture.token.Key}})
	require.NoError(t, err)
	defer client.Close()
	payload := fmt.Sprintf(`{"type":"response.create","response":{"model":"ws-billing","input":"test","prompt_cache_key":%q}}`, session)
	require.NoError(t, client.WriteMessage(websocket.TextMessage, []byte(payload)))
	rejection := readResponsesWSTestEvent(t, client)
	require.Equal(t, float64(http.StatusForbidden), rejection["status"])
	require.IsType(t, map[string]any{}, rejection["error"])
	assert.Equal(t, "cyber_policy", rejection["error"].(map[string]any)["code"])
	assert.Zero(t, fixture.connections.Load())
}

func TestResponsesPromptTextFilterChecksActualChannelAndNewTurns(t *testing.T) {
	const word = "fixture-scoped-word"
	var wsCalls, firstHTTP, fallbackHTTP atomic.Int32
	fixture := newResponsesWSBillingTest(t, `tier("request", fixed(0.002))`, func(ws *websocket.Conn, _ *http.Request) {
		for {
			if _, _, err := ws.ReadMessage(); err != nil {
				return
			}
			result := fmt.Sprintf(`{"type":"response.completed","response":{"id":"scoped-%d","status":"completed","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`, wsCalls.Add(1))
			if !assert.NoError(t, ws.WriteMessage(websocket.TextMessage, []byte(result))) {
				return
			}
		}
	})
	fixture.httpUpstream = func(w http.ResponseWriter, _ *http.Request) {
		firstHTTP.Add(1)
		http.Error(w, "retryable test response", http.StatusServiceUnavailable)
	}
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fallbackHTTP.Add(1)
		http.Error(w, "unexpected fallback", http.StatusServiceUnavailable)
	}))
	t.Cleanup(fallback.Close)
	require.NoError(t, model.DB.Model(&model.Ability{}).Where("channel_id = ?", fixture.channel.Id).Update("priority", 10).Error)
	channel := &model.Channel{Name: "filtered fallback", Key: "fixture", Status: common.ChannelStatusEnabled, Type: constant.ChannelTypeOpenAI, Group: "default", Models: "ws-billing", BaseURL: &fallback.URL}
	require.NoError(t, model.DB.Create(channel).Error)
	require.NoError(t, model.DB.Create(&model.Ability{ChannelId: channel.Id, Model: "ws-billing", Group: "default", Enabled: true, Priority: common.GetPointer(int64(5))}).Error)
	require.NoError(t, model.DB.AutoMigrate(&model.Option{}))
	common.OptionMapRWMutex.Lock()
	oldOptions := common.OptionMap
	common.OptionMap = map[string]string{}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = oldOptions
		common.OptionMapRWMutex.Unlock()
	})
	previous := model.RequestPolicyOptions()
	t.Cleanup(func() { require.NoError(t, model.UpdateRequestPolicyOptions(previous)) })
	setFilter := func(channelID int) {
		t.Helper()
		require.NoError(t, model.UpdateRequestPolicyOptions(map[string]string{
			"CheckSensitiveEnabled": "true", "CheckSensitiveOnPromptEnabled": "true",
			"SensitiveWords": word, "RetryTimes": "1", "AutomaticRetryStatusCodes": "503",
			"PromptTextFilterTarget": fmt.Sprintf(`{"scope":"channel_token","channel_ids":[%d],"token_ids":[%d]}`, channelID, fixture.token.Id),
		}))
	}
	wsRequest := func(input string) map[string]any {
		t.Helper()
		payload := fmt.Sprintf(`{"type":"response.create","model":"ws-billing","input":%q}`, input)
		require.NoError(t, fixture.client.WriteMessage(websocket.TextMessage, []byte(payload)))
		return readResponsesWSTestEvent(t, fixture.client)
	}
	setFilter(channel.Id)
	require.Equal(t, "response.completed", wsRequest(word)["type"], "the keyword outside the channel scope is allowed")
	setFilter(fixture.channel.Id)
	rejection := wsRequest(strings.ToUpper(word))
	require.Equal(t, "error", rejection["type"])
	require.IsType(t, map[string]any{}, rejection["error"])
	assert.Equal(t, "cyber_policy", rejection["error"].(map[string]any)["code"])
	assert.NotContains(t, fmt.Sprint(rejection), word)
	require.Equal(t, "response.completed", wsRequest("clean")["type"], "a local rejection must not poison the connection")
	assert.EqualValues(t, 2, wsCalls.Load())

	setFilter(channel.Id)
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, fixture.gatewayURL+"/v1/responses",
		strings.NewReader(fmt.Sprintf(`{"model":"ws-billing","stream":true,"input":%q}`, word)))
	require.NoError(t, err)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer sk-"+fixture.token.Key)
	response, err := (&http.Client{Timeout: 3 * time.Second}).Do(request)
	require.NoError(t, err)
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Contains(t, string(body), `"code":"cyber_policy"`)
	assert.NotContains(t, string(body), word)
	assert.EqualValues(t, 1, firstHTTP.Load())
	assert.Zero(t, fallbackHTTP.Load(), "the retry must never reach the scoped channel")
	var logs []model.Log
	require.NoError(t, model.LOG_DB.Where("token_id = ? AND error_code = ?", fixture.token.Id, "sensitive_words_detected").Order("id").Find(&logs).Error)
	require.Len(t, logs, 2)
	assert.Equal(t, fixture.channel.Id, logs[0].ChannelId)
	assert.Contains(t, logs[0].Other, `"protocol":"websocket"`)
	assert.Equal(t, channel.Id, logs[1].ChannelId)
	assert.Contains(t, logs[1].Other, `"protocol":"sse"`)
	for _, row := range logs {
		assert.Contains(t, row.Other, `"prompt_text_filter_scope":"channel_token"`)
		assert.NotContains(t, row.Content+row.Other, word)
	}
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		var token model.Token
		assert.NoError(collect, model.DB.First(&token, fixture.token.Id).Error)
		assert.Equal(collect, 1000, token.RemainQuota, "only the two successful turns should be charged")
	}, 3*time.Second, 10*time.Millisecond)
}

func TestResponsesInterruptedStreamHealth(t *testing.T) {
	for _, scenario := range []string{"websocket timeout", "sse timeout", "sse client cancel"} {
		t.Run(scenario, func(t *testing.T) {
			events := []string{`{"type":"response.created","response":{"id":"partial","status":"in_progress"}}`, `{"type":"response.output_text.delta","delta":"hello"}`}
			fixture := newResponsesWSBillingTest(t, `tier("request", fixed(0.002))`, func(ws *websocket.Conn, _ *http.Request) {
				if _, _, err := ws.ReadMessage(); !assert.NoError(t, err) {
					return
				}
				for _, event := range events {
					if !assert.NoError(t, ws.WriteMessage(websocket.TextMessage, []byte(event))) {
						return
					}
				}
				_, _, _ = ws.ReadMessage()
			})
			constant.StreamingTimeout = 1
			fixture.httpUpstream = func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				for _, event := range events {
					_, _ = fmt.Fprintf(w, "data: %s\n\n", event)
				}
				w.(http.Flusher).Flush()
				<-r.Context().Done()
			}
			if scenario == "websocket timeout" {
				require.NoError(t, fixture.client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"ws-billing","input":"hello"}`)))
				assert.Equal(t, "response.created", readResponsesWSTestEvent(t, fixture.client)["type"])
				assert.Equal(t, "response.output_text.delta", readResponsesWSTestEvent(t, fixture.client)["type"])
				_, _, err := fixture.client.ReadMessage()
				require.Error(t, err, "idle timeout closes the incomplete stream")
			} else {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				request, err := http.NewRequestWithContext(ctx, http.MethodPost, fixture.gatewayURL+"/v1/responses", strings.NewReader(`{"model":"ws-billing","input":"hello","stream":true}`))
				require.NoError(t, err)
				request.Header.Set("Authorization", "Bearer sk-"+fixture.token.Key)
				request.Header.Set("Content-Type", "application/json")
				response, err := (&http.Client{Timeout: 3 * time.Second}).Do(request)
				require.NoError(t, err)
				if scenario == "sse client cancel" {
					reader := bufio.NewReader(response.Body)
					for {
						line, err := reader.ReadString('\n')
						require.NoError(t, err)
						if strings.Contains(line, "response.output_text.delta") {
							break
						}
					}
					cancel()
				} else {
					_, err = io.Copy(io.Discard, response.Body)
					require.NoError(t, err)
				}
				require.NoError(t, response.Body.Close())
				select {
				case <-fixture.httpDone:
				case <-time.After(3 * time.Second):
					t.Fatal("interrupted SSE request did not finish")
				}
			}
			fixture.closeAndWait(t)
			assertResponsesWSAccounting(t, fixture, []int{1000})
			if scenario == "sse client cancel" {
				keys, err := common.RDB.Keys(context.Background(), "perf:ws-billing:*").Result()
				require.NoError(t, err)
				assert.Empty(t, keys, "client cancellation must not affect model health")
				return
			}
			requests, successes := waitPerfCounters(t, 1)
			assert.Equal(t, int64(1), requests)
			assert.Zero(t, successes)
		})
	}
}

func readResponsesWSTestEvent(t *testing.T, client *websocket.Conn) map[string]any {
	t.Helper()
	require.NoError(t, client.SetReadDeadline(time.Now().Add(3*time.Second)))
	_, data, err := client.ReadMessage()
	require.NoError(t, err)
	var event map[string]any
	require.NoError(t, common.Unmarshal(data, &event))
	return event
}

func assertResponsesWSAccounting(t *testing.T, fixture *responsesWSBillingTest, expectedQuotas []int) {
	t.Helper()
	var logs []model.Log
	require.NoError(t, model.LOG_DB.Where("type = ?", model.LogTypeConsume).Order("id").Find(&logs).Error)
	require.Len(t, logs, len(expectedQuotas))
	var charged int
	for index, quota := range expectedQuotas {
		assert.Equal(t, quota, logs[index].Quota)
		charged += quota
	}
	require.NoError(t, model.DB.First(fixture.token, fixture.token.Id).Error)
	require.NoError(t, model.DB.First(fixture.user, fixture.user.Id).Error)
	assert.Equal(t, 3000-charged, fixture.token.RemainQuota)
	assert.Equal(t, charged, fixture.token.UsedQuota)
	assert.Equal(t, 100000-charged, fixture.user.Quota)
	assert.Equal(t, charged, fixture.user.UsedQuota)
}

func TestResponsesWebSocketReusesConnectionAndSettlesEachRequest(t *testing.T) {
	type upstreamRequest struct {
		Authorization      string
		Type               string `json:"type"`
		Model              string `json:"model"`
		PreviousResponseID string `json:"previous_response_id"`
		ServiceTier        string `json:"service_tier"`
	}
	received := make(chan upstreamRequest, 3)
	fixture := newResponsesWSBillingTest(t, `param("service_tier") == "priority" ? tier("priority", p * 4) : tier("base", p * 2)`, func(ws *websocket.Conn, r *http.Request) {
		for index := 1; ; index++ {
			_, data, err := ws.ReadMessage()
			if err != nil {
				return
			}
			var event upstreamRequest
			if !assert.NoError(t, common.Unmarshal(data, &event)) {
				return
			}
			event.Authorization = r.Header.Get("Authorization")
			received <- event
			if index == 2 {
				duplicate := `{"type":"response.completed","response":{"id":"resp_1","status":"completed","usage":{"input_tokens":1000,"output_tokens":10,"total_tokens":1010}}}`
				if !assert.NoError(t, ws.WriteMessage(websocket.TextMessage, []byte(duplicate))) {
					return
				}
			}
			terminal := fmt.Sprintf(`{"type":"response.completed","response":{"id":"resp_%d","status":"completed","model":"ws-billing","output":[],"usage":{"input_tokens":1000,"output_tokens":10,"total_tokens":1010}}}`, index)
			if !assert.NoError(t, ws.WriteMessage(websocket.TextMessage, []byte(terminal))) {
				return
			}
		}
	})
	client, user, token := fixture.client, fixture.user, fixture.token

	for index, payload := range []string{
		`{"type":"response.create","model":"ws-billing","input":"first","service_tier":"default"}`,
		`{"type":"response.create","model":"ws-billing","input":"second","previous_response_id":"resp_1","service_tier":"priority"}`,
	} {
		require.NoError(t, client.WriteMessage(websocket.TextMessage, []byte(payload)))
		require.NoError(t, client.SetReadDeadline(time.Now().Add(3*time.Second)))
		_, data, err := client.ReadMessage()
		require.NoError(t, err)
		var terminal struct {
			Type     string `json:"type"`
			Response struct {
				ID string `json:"id"`
			} `json:"response"`
		}
		require.NoError(t, common.Unmarshal(data, &terminal))
		require.Equal(t, "response.completed", terminal.Type, "unexpected response: %s", data)
		assert.Equal(t, fmt.Sprintf("resp_%d", index+1), terminal.Response.ID)
		observed := <-received
		assert.Equal(t, "Bearer upstream-first", observed.Authorization)
		assert.Equal(t, "gpt-4o", observed.Model)
		assert.Equal(t, "response.create", observed.Type)
		if index == 0 {
			assert.Empty(t, observed.PreviousResponseID)
			assert.Equal(t, "default", observed.ServiceTier)
		} else {
			assert.Equal(t, "resp_1", observed.PreviousResponseID)
			assert.Equal(t, "priority", observed.ServiceTier)
		}
	}

	require.NoError(t, client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"ws-billing","input":"exhausted"}`)))
	require.NoError(t, client.SetReadDeadline(time.Now().Add(3*time.Second)))
	_, data, err := client.ReadMessage()
	require.NoError(t, err)
	var rejection struct {
		Type   string `json:"type"`
		Status int    `json:"status"`
	}
	require.NoError(t, common.Unmarshal(data, &rejection))
	assert.Equal(t, "error", rejection.Type)
	assert.Equal(t, http.StatusUnauthorized, rejection.Status)
	assert.Equal(t, int32(1), fixture.connections.Load())
	assert.Empty(t, received, "exhausted token must be rejected before contacting upstream")
	var logs []model.Log
	require.NoError(t, model.LOG_DB.Where("type = ?", model.LogTypeConsume).Order("id").Find(&logs).Error)
	require.Len(t, logs, 2)
	for index, expectedQuota := range []int{1000, 2000} {
		assert.Equal(t, expectedQuota, logs[index].Quota)
		assert.Equal(t, fmt.Sprintf("responses-ws-billing-ws-%d", index), logs[index].RequestId)
		assert.Equal(t, 1000, logs[index].PromptTokens)
		assert.Equal(t, 10, logs[index].CompletionTokens)
	}
	// Routing decisions are persisted per request: the first create selects a
	// channel, later creates on the same connection reuse it as a pin.
	for index, wantDecision := range []string{"attempt:channel_selected", "select:pinned_channel"} {
		var other struct {
			AdminInfo struct {
				RequestPolicy []struct {
					ChannelID int `json:"channel_id"`
					Decision  struct {
						Action string `json:"action"`
						Reason string `json:"reason"`
					} `json:"decision"`
				} `json:"request_policy"`
			} `json:"admin_info"`
		}
		require.NoError(t, common.UnmarshalJsonStr(logs[index].Other, &other))
		var decisions []string
		for _, event := range other.AdminInfo.RequestPolicy {
			if event.Decision.Action == "skip" || event.Decision.Action == "match" {
				assert.Zero(t, event.ChannelID, "session policy is evaluated before selecting a channel")
			} else {
				assert.Equal(t, fixture.channel.Id, event.ChannelID)
			}
			decisions = append(decisions, event.Decision.Action+":"+event.Decision.Reason)
		}
		assert.Contains(t, decisions, wantDecision, "request %d policy events: %v", index, decisions)
	}
	require.NoError(t, model.DB.First(token, token.Id).Error)
	require.NoError(t, model.DB.First(user, user.Id).Error)
	assert.Zero(t, token.RemainQuota)
	assert.Equal(t, 3000, token.UsedQuota)
	assert.Equal(t, 97000, user.Quota)
	assert.Equal(t, 3000, user.UsedQuota)
}

func TestResponsesWebSocketAuditFromUsageLog(t *testing.T) {
	if isolateResponsesAuditTest(t) {
		return
	}

	fixture := newResponsesWSBillingTest(t, `tier("request", fixed(0.002))`, func(ws *websocket.Conn, _ *http.Request) {
		for round := 1; round <= 2; round++ {
			if _, _, err := ws.ReadMessage(); !assert.NoError(t, err) {
				return
			}
			for _, frame := range []string{
				fmt.Sprintf(`{"type":"response.output_text.done","output_index":0,"content_index":0,"text":"visible reply %d"}`, round),
				fmt.Sprintf(`{"type":"response.completed","response":{"id":"resp_audit_%d","status":"completed","usage":{"input_tokens":4,"output_tokens":3,"total_tokens":7}}}`, round),
			} {
				if !assert.NoError(t, ws.WriteMessage(websocket.TextMessage, []byte(frame))) {
					return
				}
			}
		}
		_, _, _ = ws.ReadMessage() // Keep the upstream open until the client closes.
	})
	lookup, rawToken := setupResponsesAuditLookup(t, true, true, fixture.user.Id)

	for round := 1; round <= 2; round++ {
		frame := fmt.Sprintf(`{"type":"response.create","model":"ws-billing","input":"question %d","instructions":"private instructions"}`, round)
		require.NoError(t, fixture.client.WriteMessage(websocket.TextMessage, []byte(frame)))
		require.Equal(t, "response.output_text.done", readResponsesWSTestEvent(t, fixture.client)["type"])
		require.Equal(t, "response.completed", readResponsesWSTestEvent(t, fixture.client)["type"])
	}
	assert.EqualValues(t, 1, fixture.connections.Load())

	var logs []model.Log
	require.NoError(t, model.LOG_DB.Where("token_id = ? AND type = ?", fixture.token.Id, model.LogTypeConsume).Order("id").Find(&logs).Error)
	require.Len(t, logs, 2)
	for index, log := range logs {
		require.NotEmpty(t, log.RequestId)
		req := httptest.NewRequest(http.MethodGet, "/api/custom/conversation-audits/"+log.RequestId, nil)
		req.Header.Set("Authorization", "Bearer "+rawToken)
		result := httptest.NewRecorder()
		lookup.ServeHTTP(result, req)
		require.Equal(t, http.StatusOK, result.Code, result.Body.String())
		var detail struct {
			Data struct {
				Audit             conversationaudit.ConversationAudit
				Request, Response string
			}
		}
		require.NoError(t, common.Unmarshal(result.Body.Bytes(), &detail))
		assert.Equal(t, log.RequestId, detail.Data.Audit.RequestID)
		assert.Equal(t, "completed", detail.Data.Audit.Status)
		assert.False(t, detail.Data.Audit.FullPayload)
		assert.JSONEq(t, fmt.Sprintf(`{"schema_version":2,"capture_mode":"latest_user_text","user_input":"question %d"}`, index+1), detail.Data.Request)
		assert.JSONEq(t, fmt.Sprintf(`{"schema_version":2,"capture_mode":"assistant_text","assistant_text":"visible reply %d"}`, index+1), detail.Data.Response)
	}
}

// Both transports must reach the same upstream target with the same
// credential placement and settle identically for every supported channel type.
func TestResponsesWebSocketDialsNativeResponsesChannelTypes(t *testing.T) {
	type upstreamTarget struct {
		Path, Authorization, QueryKey string
	}
	queryAuthRoute := dto.ChannelOtherSettings{AdvancedCustom: &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{{
		IncomingPath: "/v1/responses", UpstreamPath: "/upstream/responses",
		Auth: &dto.AdvancedCustomRouteAuth{Type: dto.AdvancedCustomAuthTypeQuery, Name: "api_key", Value: "{api_key}"},
	}}}}
	bearer := func(key string) upstreamTarget {
		return upstreamTarget{Path: "/v1/responses", Authorization: "Bearer " + key}
	}
	for _, tc := range []struct {
		name          string
		channelType   int
		otherSettings dto.ChannelOtherSettings
		want          func(key string) upstreamTarget
	}{
		{name: "new api", channelType: constant.ChannelTypeNewAPI, want: bearer},
		{name: "sub2api", channelType: constant.ChannelTypeSub2API, want: bearer},
		{name: "advanced custom query auth", channelType: constant.ChannelTypeAdvancedCustom, otherSettings: queryAuthRoute, want: func(key string) upstreamTarget {
			return upstreamTarget{Path: "/upstream/responses", QueryKey: key}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const terminal = `{"type":"response.completed","response":{"id":"first","status":"completed","usage":{"input_tokens":1000,"output_tokens":10,"total_tokens":1010}}}`
			targets := make(chan upstreamTarget, 2)
			observe := func(r *http.Request) {
				targets <- upstreamTarget{Path: r.URL.Path, Authorization: r.Header.Get("Authorization"), QueryKey: r.URL.Query().Get("api_key")}
			}
			fixture := newResponsesWSBillingTest(t, `tier("request", fixed(0.002))`, func(ws *websocket.Conn, r *http.Request) {
				observe(r)
				if _, _, err := ws.ReadMessage(); !assert.NoError(t, err) {
					return
				}
				if !assert.NoError(t, ws.WriteMessage(websocket.TextMessage, []byte(terminal))) {
					return
				}
				_, _, _ = ws.ReadMessage()
			})
			fixture.httpUpstream = func(w http.ResponseWriter, r *http.Request) {
				observe(r)
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", terminal)
			}
			fixture.channel.SetOtherSettings(tc.otherSettings)
			require.NoError(t, model.DB.Model(fixture.channel).Updates(map[string]any{"type": tc.channelType, "settings": fixture.channel.OtherSettings}).Error)

			require.NoError(t, fixture.client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"ws-billing","input":"hi"}`)))
			assert.Equal(t, "response.completed", readResponsesWSTestEvent(t, fixture.client)["type"])
			assert.Equal(t, tc.want("upstream-first"), <-targets)

			request, err := http.NewRequest(http.MethodPost, fixture.gatewayURL+"/v1/responses", strings.NewReader(`{"model":"ws-billing","input":"hi","stream":true}`))
			require.NoError(t, err)
			request.Header.Set("Authorization", "Bearer sk-"+fixture.token.Key)
			request.Header.Set("Content-Type", "application/json")
			response, err := (&http.Client{Timeout: 3 * time.Second}).Do(request)
			require.NoError(t, err)
			body, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			require.NoError(t, response.Body.Close())
			assert.Equal(t, http.StatusOK, response.StatusCode)
			assert.Contains(t, string(body), terminal)
			select {
			case <-fixture.httpDone:
			case <-time.After(3 * time.Second):
				t.Fatal("HTTP request did not finish")
			}
			// The polling multi-key channel rotates to its second key for the HTTP request.
			assert.Equal(t, tc.want("upstream-second"), <-targets)
			fixture.closeAndWait(t)
			assertResponsesWSAccounting(t, fixture, []int{1000, 1000})
		})
	}
}

func TestResponsesWebSocketDisconnectSettlesDeliveredOutputOnce(t *testing.T) {
	fixture := newResponsesWSBillingTest(t, `tier("output", c * 2)`, func(ws *websocket.Conn, _ *http.Request) {
		if _, _, err := ws.ReadMessage(); !assert.NoError(t, err) {
			return
		}
		for _, event := range []string{
			`{"type":"response.created","response":{"id":"partial","status":"in_progress"}}`,
			`{"type":"response.output_text.delta","delta":"hello"}`,
		} {
			if !assert.NoError(t, ws.WriteMessage(websocket.TextMessage, []byte(event))) {
				return
			}
		}
		_, _, _ = ws.ReadMessage()
	})
	require.NoError(t, fixture.client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"ws-billing","input":"hi","max_output_tokens":1}`)))
	assert.Equal(t, "response.created", readResponsesWSTestEvent(t, fixture.client)["type"])
	delta := readResponsesWSTestEvent(t, fixture.client)
	require.Equal(t, "response.output_text.delta", delta["type"])
	assert.Equal(t, "hello", delta["delta"])
	fixture.closeAndWait(t)
	assertResponsesWSAccounting(t, fixture, []int{1})
	keys, err := common.RDB.Keys(context.Background(), "perf:ws-billing:*").Result()
	require.NoError(t, err)
	assert.Empty(t, keys, "client cancellation must not affect model health")
}

func TestResponsesWebSocketCancelErrorDoesNotFinishActiveRequest(t *testing.T) {
	complete := make(chan struct{})
	defer close(complete)
	allowCreated := make(chan struct{}, 1)
	defer close(allowCreated)
	receivedCreate := make(chan struct{}, 1)
	fixture := newResponsesWSBillingTest(t, `tier("base", p * 2)`, func(ws *websocket.Conn, _ *http.Request) {
		_, create, err := ws.ReadMessage()
		if !assert.NoError(t, err) || !assert.Contains(t, string(create), `"type":"response.create"`) {
			return
		}
		receivedCreate <- struct{}{}
		<-allowCreated
		if !assert.NoError(t, ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.created","response":{"id":"active","status":"in_progress"}}`))) {
			return
		}
		_, cancel, err := ws.ReadMessage()
		if !assert.NoError(t, err) || !assert.JSONEq(t, `{"type":"response.cancel","response_id":"wrong"}`, string(cancel)) {
			return
		}
		if !assert.NoError(t, ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"error","status":400,"error":{"type":"invalid_request_error","code":"response_not_found","message":"No such response"}}`))) {
			return
		}
		<-complete
		if !assert.NoError(t, ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.completed","response":{"id":"active","status":"completed","usage":{"input_tokens":1000,"output_tokens":10,"total_tokens":1010}}}`))) {
			return
		}
		_, _, _ = ws.ReadMessage()
	})
	require.NoError(t, fixture.client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"ws-billing","input":"hi"}`)))
	select {
	case <-receivedCreate:
	case <-time.After(3 * time.Second):
		t.Fatal("upstream did not receive the initial request")
	}
	// Queue cancellation before the upstream accepts the response. The following
	// conflict is an acknowledgement that the client loop processed both frames.
	require.NoError(t, fixture.client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.cancel","response_id":"wrong"}`)))
	require.NoError(t, fixture.client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"ws-billing","input":"before-acceptance"}`)))
	assert.Equal(t, float64(http.StatusConflict), readResponsesWSTestEvent(t, fixture.client)["status"])
	allowCreated <- struct{}{}
	assert.Equal(t, "response.created", readResponsesWSTestEvent(t, fixture.client)["type"])
	cancelError := readResponsesWSTestEvent(t, fixture.client)
	assert.Equal(t, "error", cancelError["type"])
	assert.Equal(t, float64(http.StatusBadRequest), cancelError["status"])
	require.NoError(t, fixture.client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"ws-billing","input":"overlapping","stream_id":"other","event_id":"busy"}`)))
	conflict := readResponsesWSTestEvent(t, fixture.client)
	assert.Equal(t, "error", conflict["type"])
	assert.Equal(t, float64(http.StatusConflict), conflict["status"])
	assert.Equal(t, "other", conflict["stream_id"])
	assert.Equal(t, "busy", conflict["event_id"])
	complete <- struct{}{}
	assert.Equal(t, "response.completed", readResponsesWSTestEvent(t, fixture.client)["type"])
	fixture.closeAndWait(t)
	assertResponsesWSAccounting(t, fixture, []int{1000})
}

func TestResponsesWebSocketInitialUpstreamRejectionRefundsReservation(t *testing.T) {
	for _, tc := range []struct {
		name, upstream, wantType, wantMessage string
		status                                int
	}{
		{name: "structured error", upstream: `{"type":"error","response_id":"rejected","status":400,"error":{"type":"invalid_request_error","code":"invalid_input","message":"Invalid input"}}`, status: http.StatusBadRequest, wantType: "invalid_request_error", wantMessage: "Invalid input"},
		// A frame without an error object is still reported as a request error.
		{name: "bare error", upstream: `{"type":"error","status":500,"message":"upstream rejected"}`, status: http.StatusInternalServerError, wantType: "invalid_request_error", wantMessage: "upstream rejected"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			preConsumed := make(chan int, 1)
			tokenID := make(chan int, 1)
			// Pre-consume no longer estimates completion tokens, so an output-priced expression reserves nothing to refund.
			fixture := newResponsesWSBillingTest(t, `tier("request", fixed(0.002))`, func(ws *websocket.Conn, _ *http.Request) {
				if _, _, err := ws.ReadMessage(); !assert.NoError(t, err) {
					return
				}
				var token model.Token
				if !assert.NoError(t, model.DB.First(&token, <-tokenID).Error) {
					return
				}
				preConsumed <- token.RemainQuota
				if !assert.NoError(t, ws.WriteMessage(websocket.TextMessage, []byte(tc.upstream))) {
					return
				}
				_, _, _ = ws.ReadMessage()
			})
			tokenID <- fixture.token.Id
			// Refund is asynchronous. Observe committed token writes instead of waiting
			// a fixed delay or returning while its worker still uses the test database.
			updates := make(chan struct{}, 4)
			require.NoError(t, model.DB.Callback().Update().After("gorm:commit_or_rollback_transaction").Register("responses-ws-refund", func(tx *gorm.DB) {
				if tx.Error == nil && tx.Statement.Table == "tokens" {
					select {
					case updates <- struct{}{}:
					default:
					}
				}
			}))
			require.NoError(t, fixture.client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"ws-billing","input":"hi","max_output_tokens":10}`)))
			rejection := readResponsesWSTestEvent(t, fixture.client)
			assert.Equal(t, "error", rejection["type"])
			assert.Equal(t, float64(tc.status), rejection["status"])
			rejectionError, _ := rejection["error"].(map[string]any)
			assert.Equal(t, tc.wantType, rejectionError["type"])
			assert.Equal(t, tc.wantMessage, rejectionError["message"])
			assert.Equal(t, 2000, <-preConsumed, "the rejected request reserved quota before contacting upstream")
			deadline := time.NewTimer(3 * time.Second)
			defer deadline.Stop()
			for {
				require.NoError(t, model.DB.First(fixture.token, fixture.token.Id).Error)
				if fixture.token.RemainQuota == 3000 {
					break
				}
				select {
				case <-updates:
				case <-deadline.C:
					t.Fatal("initial rejection did not refund the token reservation")
				}
			}
			fixture.closeAndWait(t)
			assertResponsesWSAccounting(t, fixture, nil)
		})
	}
}

// TEST_RESPONSES_SQL_DSN / TEST_RESPONSES_LOG_SQL_DSN optionally run these
// entry-point regressions against isolated real MySQL/PostgreSQL databases.
func TestResponsesStreamOutcomesPreserveAccounting(t *testing.T) {
	for _, transport := range []string{"websocket", "http-sse"} {
		for _, tc := range []struct {
			name, expression, terminal string
			// sseTerminal is the flat error envelope used by the Responses SSE
			// protocol; the WebSocket protocol nests the error instead.
			sseTerminal string
			delta       bool
			failed      bool
			ignored     bool
		}{
			{name: "failed-null-fixed", expression: `tier("request", fixed(0.002))`, terminal: `{"type":"response.failed","response":{"id":"first","status":"failed","usage":null,"error":{"code":"server_error","message":"sensitive upstream detail"}}}`, failed: true},
			{name: "failed-missing-fixed", expression: `tier("request", fixed(0.002))`, terminal: `{"type":"response.failed","response":{"id":"first","status":"failed"}}`, failed: true},
			{name: "failed-actual-usage", expression: `tier("input", p * 2)`, terminal: `{"type":"response.failed","response":{"id":"first","status":"failed","usage":{"input_tokens":1000,"output_tokens":10,"total_tokens":1010}}}`, failed: true},
			{name: "failed-estimated-output", expression: `tier("output", c * 2000)`, terminal: `{"type":"response.failed","response":{"id":"first","status":"failed","usage":null}}`, delta: true, failed: true},
			{name: "error-after-created", expression: `tier("request", fixed(0.002))`, terminal: `{"type":"error","status":500,"error":{"type":"server_error","code":"server_error","message":"Internal server error"}}`, sseTerminal: `{"type":"error","code":"server_error","message":"Internal server error","param":null,"sequence_number":2}`, failed: true},
			{name: "business-error-after-created", expression: `tier("request", fixed(0.002))`, terminal: `{"type":"error","status":400,"error":{"type":"invalid_request_error","code":"context_length_exceeded","message":"Input too long"}}`, sseTerminal: `{"type":"error","code":"context_length_exceeded","message":"Input too long","param":null,"sequence_number":2}`, failed: true, ignored: true},
			{name: "cyber-before-output", expression: `tier("request", fixed(0.002))`, terminal: `{"type":"response.failed","response":{"id":"first","status":"failed","error":{"code":"cyber_policy","message":"sensitive upstream detail"}}}`, failed: true, ignored: true},
			{name: "cyber-after-output", expression: `tier("request", fixed(0.002))`, terminal: `{"type":"response.failed","response":{"id":"first","status":"failed","error":{"code":"cyber_policy"}}}`, sseTerminal: `{"type":"error","error":{"code":"cyber_policy"}}`, delta: true, failed: true, ignored: true},
			{name: "completed-at-output-limit", expression: `tier("request", fixed(0.002))`, terminal: `{"type":"response.incomplete","response":{"id":"first","status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"usage":{"input_tokens":1000,"output_tokens":1,"total_tokens":1001}}}`},
			{name: "completed-zero-fixed", expression: `tier("request", fixed(0.002))`, terminal: `{"type":"response.completed","response":{"id":"first","status":"completed","usage":{"input_tokens":0,"output_tokens":0,"total_tokens":0}}}`},
		} {
			t.Run(transport+"/"+tc.name, func(t *testing.T) {
				isCyber := strings.HasPrefix(tc.name, "cyber-")
				events := []string{`{"type":"response.created","response":{"id":"first","status":"in_progress"}}`}
				if tc.delta {
					events = append(events, `{"type":"response.output_text.delta","delta":"hello"}`)
				}
				terminal := tc.terminal
				if transport == "http-sse" && tc.sseTerminal != "" {
					terminal = tc.sseTerminal
				}
				events = append(events, terminal)
				fixture := newResponsesWSBillingTest(t, tc.expression, func(ws *websocket.Conn, _ *http.Request) {
					for turn := 0; ; turn++ {
						_, request, err := ws.ReadMessage()
						if err != nil {
							return
						}
						var body map[string]any
						if !assert.NoError(t, common.Unmarshal(request, &body)) {
							return
						}
						assert.Equal(t, "planner", body["stream_id"])
						assert.Equal(t, "gpt-4o", body["model"])
						output := events
						if transport == "http-sse" || turn > 0 {
							output = []string{`{"type":"response.completed","stream_id":"planner","response":{"id":"second","status":"completed","usage":{"input_tokens":1000,"output_tokens":1,"total_tokens":1001}}}`}
						}
						for _, event := range output {
							if !assert.NoError(t, ws.WriteMessage(websocket.TextMessage, []byte(event))) {
								return
							}
						}
					}
				}, events...)
				setting.ModelRequestRateLimitEnabled = true
				setting.ModelRequestRateLimitDurationMinutes = 1
				setting.ModelRequestRateLimitSuccessCount = 1
				setting.ModelRequestRateLimitCount = 0
				if transport == "http-sse" {
					request, err := http.NewRequest(http.MethodPost, fixture.gatewayURL+"/v1/responses", strings.NewReader(`{"model":"ws-billing","input":"hi","stream":true,"max_output_tokens":1}`))
					require.NoError(t, err)
					request.Header.Set("Authorization", "Bearer sk-"+fixture.token.Key)
					request.Header.Set("Content-Type", "application/json")
					response, err := (&http.Client{Timeout: 3 * time.Second}).Do(request)
					require.NoError(t, err)
					body, err := io.ReadAll(response.Body)
					require.NoError(t, err)
					require.NoError(t, response.Body.Close())
					if tc.failed && !tc.delta {
						if isCyber {
							assert.Equal(t, http.StatusForbidden, response.StatusCode)
							assert.Contains(t, string(body), "cyber_policy")
						} else {
							assert.Equal(t, http.StatusBadGateway, response.StatusCode)
							assert.Contains(t, string(body), string(types.ErrorCodeChannelUpstreamStreamTerminated))
						}
					} else {
						assert.Equal(t, http.StatusOK, response.StatusCode)
						if tc.failed {
							if isCyber {
								assert.Contains(t, string(body), "cyber_policy")
							} else {
								assert.Contains(t, string(body), "event: response.failed")
							}
						} else {
							assert.Contains(t, string(body), terminal)
						}
					}
					assert.NotContains(t, string(body), "sensitive upstream detail")
				} else {
					require.NoError(t, fixture.client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","stream_id":"planner","model":"ws-billing","input":"hi","max_output_tokens":1}`)))
					for _, expected := range events {
						actual, err := common.Marshal(readResponsesWSTestEvent(t, fixture.client))
						require.NoError(t, err)
						assert.JSONEq(t, expected, string(actual), "upstream frames must be delivered once and unchanged")
					}
				}
				quotas := []int{1000}
				if transport == "http-sse" && tc.failed && !tc.delta {
					quotas = nil
				}
				if tc.failed {
					// The first terminal must complete settlement and middleware before
					// admitting this immediately following request.
					require.NoError(t, fixture.client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","stream_id":"planner","model":"ws-billing","input":"next","max_output_tokens":1}`)))
					assert.Equal(t, "response.completed", readResponsesWSTestEvent(t, fixture.client)["type"])
					quotas = append(quotas, 1000)
				}
				require.NoError(t, fixture.client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","event_id":"limited","stream_id":"planner","model":"ws-billing","input":"limited"}`)))
				limited := readResponsesWSTestEvent(t, fixture.client)
				assert.Equal(t, float64(http.StatusTooManyRequests), limited["status"])
				assert.Equal(t, "planner", limited["stream_id"])
				assert.Equal(t, "limited", limited["event_id"])
				fixture.closeAndWait(t)
				if isCyber {
					failures, total, err := model.GetUserLogs(fixture.user.Id, model.LogTypeUnknown, 0, 0, "", "", 0, 10, "", "", "", "cyber_policy")
					require.NoError(t, err)
					require.Len(t, failures, 1)
					assert.EqualValues(t, 1, total)
					assert.Equal(t, fixture.channel.Id, failures[0].ChannelId)
					assert.NotEmpty(t, failures[0].RequestId)
					assert.NotContains(t, failures[0].Content+failures[0].Other, "sensitive upstream detail")
				}
				assertResponsesWSAccounting(t, fixture, quotas)
				var logs []model.Log
				require.NoError(t, model.LOG_DB.Where("token_id = ? AND type = ?", fixture.token.Id, model.LogTypeConsume).Order("id").Find(&logs).Error)
				require.Len(t, logs, len(quotas))
				var other map[string]any
				require.NoError(t, common.UnmarshalJsonStr(logs[0].Other, &other))
				stream := other["stream_status"].(map[string]any)
				chargedFailure := tc.failed && (transport != "http-sse" || tc.delta)
				if chargedFailure {
					assert.Equal(t, "error", stream["status"])
					assert.Equal(t, "failed", stream["response_status"])
				} else {
					assert.Equal(t, "ok", stream["status"])
					if tc.name == "completed-at-output-limit" {
						assert.Equal(t, "incomplete", stream["response_status"])
					} else {
						assert.Equal(t, "completed", stream["response_status"])
					}
				}
				assert.NotContains(t, logs[0].Other, "sensitive upstream detail")
				expectedRequests := int64(1)
				if tc.ignored {
					expectedRequests--
				}
				if tc.failed {
					expectedRequests++
				}
				requests, successes := waitPerfCounters(t, expectedRequests)
				assert.Equal(t, expectedRequests, requests)
				assert.Equal(t, int64(1), successes)
			})
		}
	}
}

func TestResponsesHTTPHealthCountsFinalResult(t *testing.T) {
	for _, tc := range []struct {
		name, code            string
		firstStatus, attempts int
		success               bool
		ignored               bool
	}{
		{"business rejection", "context_length_exceeded", 400, 1, false, true},
		{"cyber rejection", "cyber_policy", 403, 1, false, true},
		{"ordinary forbidden", "permission_denied", 403, 1, false, false},
		{"credentials rejected as 400", "invalid_api_key", 400, 1, false, false},
		{"retry succeeds", "server_error", 500, 2, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			previousErrorLog := constant.ErrorLogEnabled
			constant.ErrorLogEnabled = true
			t.Cleanup(func() { constant.ErrorLogEnabled = previousErrorLog })
			fixture := newResponsesWSBillingTest(t, `tier("request", fixed(0.002))`, func(*websocket.Conn, *http.Request) {})
			oldRetries := common.RetryTimes
			common.RetryTimes = 1
			if tc.code == "permission_denied" {
				common.RetryTimes = 0
			}
			t.Cleanup(func() { common.RetryTimes = oldRetries })
			var attempts atomic.Int64
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if attempts.Add(1) == 1 {
					w.WriteHeader(tc.firstStatus)
					_, _ = fmt.Fprintf(w, `{"error":{"type":"%s","code":"%s","message":"test rejection"}}`, tc.code, tc.code)
					return
				}
				_, _ = fmt.Fprint(w, `{"id":"completed","status":"completed","usage":{"input_tokens":1000,"output_tokens":1,"total_tokens":1001}}`)
			}))
			t.Cleanup(upstream.Close)
			require.NoError(t, model.DB.Model(&model.Channel{}).Where("name = ?", "responses-ws-upstream").Update("base_url", upstream.URL).Error)
			request, err := http.NewRequest(http.MethodPost, fixture.gatewayURL+"/v1/responses", strings.NewReader(`{"model":"ws-billing","input":"hello"}`))
			require.NoError(t, err)
			request.Header.Set("Authorization", "Bearer sk-"+fixture.token.Key)
			request.Header.Set("Content-Type", "application/json")
			response, err := http.DefaultClient.Do(request)
			require.NoError(t, err)
			_, err = io.Copy(io.Discard, response.Body)
			require.NoError(t, err)
			require.NoError(t, response.Body.Close())
			select {
			case <-fixture.httpDone:
			case <-time.After(3 * time.Second):
				t.Fatal("HTTP request did not finish")
			}
			fixture.closeAndWait(t)
			failures, total, err := model.GetUserLogs(fixture.user.Id, model.LogTypeError, 0, 0, "", "", 0, 10, "", "", "", "")
			require.NoError(t, err)
			if tc.success {
				assert.Zero(t, total, "a recovered attempt is not a final failure")
			} else {
				require.Len(t, failures, 1)
				assert.EqualValues(t, 1, total)
				assert.Equal(t, tc.code, failures[0].ErrorCode)
			}
			assert.Equal(t, int64(tc.attempts), attempts.Load())
			if !tc.success {
				assert.Equal(t, tc.firstStatus, response.StatusCode)
			} else {
				assert.Equal(t, http.StatusOK, response.StatusCode)
			}
			if tc.ignored {
				keys, err := common.RDB.Keys(context.Background(), "perf:ws-billing:*").Result()
				require.NoError(t, err)
				assert.Empty(t, keys)
				return
			}
			requests, successes := waitPerfCounters(t, 1)
			assert.Equal(t, int64(1), requests)
			if tc.success {
				assert.Equal(t, int64(1), successes)
			} else {
				assert.Zero(t, successes)
			}
		})
	}
}

func TestResponsesWebSocketLocalErrorsKeepStreamIdentity(t *testing.T) {
	fixture := newResponsesWSBillingTest(t, `tier("request", fixed(0.002))`, func(*websocket.Conn, *http.Request) {
		t.Error("local rejection reached upstream")
	})
	require.NoError(t, fixture.client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","event_id":"invalid","stream_id":"planner","model":"ws-billing","max_output_tokens":18446744073709551615}`)))
	invalid := readResponsesWSTestEvent(t, fixture.client)
	assert.Equal(t, float64(http.StatusBadRequest), invalid["status"])
	assert.Equal(t, "planner", invalid["stream_id"])
	assert.Equal(t, "invalid", invalid["event_id"])
	fixture.token.Status = common.TokenStatusDisabled
	require.NoError(t, fixture.token.SelectUpdate())
	require.NoError(t, fixture.client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","event_id":"revoked","stream_id":"planner","model":"ws-billing","input":"hi"}`)))
	revoked := readResponsesWSTestEvent(t, fixture.client)
	assert.Equal(t, float64(http.StatusUnauthorized), revoked["status"])
	assert.Equal(t, "planner", revoked["stream_id"])
	assert.Equal(t, "revoked", revoked["event_id"])
	fixture.closeAndWait(t)
	assertResponsesWSAccounting(t, fixture, nil)
}

func TestResponsesWebSocketAmbiguousControlErrorClosesAndSettlesOnce(t *testing.T) {
	const terminal = `{"type":"error","status":500,"error":{"type":"server_error","code":"server_error","message":"Internal server error"}}`
	fixture := newResponsesWSBillingTest(t, `tier("request", fixed(0.002))`, func(ws *websocket.Conn, _ *http.Request) {
		if _, _, err := ws.ReadMessage(); !assert.NoError(t, err) {
			return
		}
		if !assert.NoError(t, ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.created","response":{"id":"active","status":"in_progress"}}`))) {
			return
		}
		_, cancel, err := ws.ReadMessage()
		if !assert.NoError(t, err) {
			return
		}
		assert.Contains(t, string(cancel), `"type":"response.cancel"`)
		if !assert.NoError(t, ws.WriteMessage(websocket.TextMessage, []byte(terminal))) {
			return
		}
		_, _, _ = ws.ReadMessage()
	})
	require.NoError(t, fixture.client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"ws-billing","input":"hi"}`)))
	assert.Equal(t, "response.created", readResponsesWSTestEvent(t, fixture.client)["type"])
	require.NoError(t, fixture.client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.cancel","response_id":"active"}`)))
	body, err := common.Marshal(readResponsesWSTestEvent(t, fixture.client))
	require.NoError(t, err)
	assert.JSONEq(t, terminal, string(body))
	_, _, err = fixture.client.ReadMessage()
	require.Error(t, err, "an unattributed error must close the connection instead of remaining busy")
	var timeout net.Error
	assert.False(t, errors.As(err, &timeout) && timeout.Timeout(), "the server must close before the read deadline")
	fixture.closeAndWait(t)
	assertResponsesWSAccounting(t, fixture, []int{1000})
}

// A control event queued while another is still outstanding is rejected
// locally instead of reaching upstream twice.
func TestResponsesWebSocketRejectsSecondControlWhileOnePending(t *testing.T) {
	receivedCancel := make(chan struct{}, 1)
	release := make(chan struct{})
	defer close(release)
	fixture := newResponsesWSBillingTest(t, `tier("request", fixed(0.002))`, func(ws *websocket.Conn, _ *http.Request) {
		if _, _, err := ws.ReadMessage(); !assert.NoError(t, err) {
			return
		}
		if !assert.NoError(t, ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.created","response":{"id":"active","status":"in_progress"}}`))) {
			return
		}
		_, cancel, err := ws.ReadMessage()
		if !assert.NoError(t, err) || !assert.Contains(t, string(cancel), `"event_id":"first-cancel"`) {
			return
		}
		receivedCancel <- struct{}{}
		<-release
		if !assert.NoError(t, ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.completed","response":{"id":"active","status":"completed","usage":{"input_tokens":1000,"output_tokens":10,"total_tokens":1010}}}`))) {
			return
		}
		_, _, _ = ws.ReadMessage()
	})
	require.NoError(t, fixture.client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"ws-billing","input":"hi"}`)))
	assert.Equal(t, "response.created", readResponsesWSTestEvent(t, fixture.client)["type"])
	require.NoError(t, fixture.client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.cancel","event_id":"first-cancel","response_id":"active"}`)))
	select {
	case <-receivedCancel:
	case <-time.After(3 * time.Second):
		t.Fatal("upstream did not receive the first cancel")
	}
	require.NoError(t, fixture.client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.cancel","event_id":"second-cancel","response_id":"active"}`)))
	rejected := readResponsesWSTestEvent(t, fixture.client)
	assert.Equal(t, "error", rejected["type"])
	assert.Equal(t, float64(http.StatusBadRequest), rejected["status"])
	assert.Equal(t, "second-cancel", rejected["event_id"])
	rejectedError, _ := rejected["error"].(map[string]any)
	assert.Contains(t, rejectedError["message"], "already pending")
	release <- struct{}{}
	assert.Equal(t, "response.completed", readResponsesWSTestEvent(t, fixture.client)["type"])
	fixture.closeAndWait(t)
	assertResponsesWSAccounting(t, fixture, []int{1000})
}

// A control error resolves the outstanding control, so a corrected cancel is
// forwarded instead of being rejected as still pending.
func TestResponsesWebSocketAcceptsCancelAfterControlError(t *testing.T) {
	fixture := newResponsesWSBillingTest(t, `tier("request", fixed(0.002))`, func(ws *websocket.Conn, _ *http.Request) {
		if _, _, err := ws.ReadMessage(); !assert.NoError(t, err) {
			return
		}
		if !assert.NoError(t, ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.created","response":{"id":"active","status":"in_progress"}}`))) {
			return
		}
		_, cancel, err := ws.ReadMessage()
		if !assert.NoError(t, err) || !assert.JSONEq(t, `{"type":"response.cancel","response_id":"wrong"}`, string(cancel)) {
			return
		}
		if !assert.NoError(t, ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"error","status":400,"error":{"type":"invalid_request_error","code":"response_not_found","message":"No such response"}}`))) {
			return
		}
		_, cancel, err = ws.ReadMessage()
		if !assert.NoError(t, err) || !assert.JSONEq(t, `{"type":"response.cancel","response_id":"active"}`, string(cancel)) {
			return
		}
		if !assert.NoError(t, ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.cancelled","response":{"id":"active","status":"cancelled","usage":{"input_tokens":1000,"output_tokens":0,"total_tokens":1000}}}`))) {
			return
		}
		_, _, _ = ws.ReadMessage()
	})
	require.NoError(t, fixture.client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"ws-billing","input":"hi"}`)))
	assert.Equal(t, "response.created", readResponsesWSTestEvent(t, fixture.client)["type"])
	require.NoError(t, fixture.client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.cancel","response_id":"wrong"}`)))
	cancelError := readResponsesWSTestEvent(t, fixture.client)
	assert.Equal(t, "error", cancelError["type"])
	assert.Equal(t, float64(http.StatusBadRequest), cancelError["status"])
	require.NoError(t, fixture.client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.cancel","response_id":"active"}`)))
	assert.Equal(t, "response.cancelled", readResponsesWSTestEvent(t, fixture.client)["type"])
	fixture.closeAndWait(t)
	assertResponsesWSAccounting(t, fixture, []int{1000})
}

// A late error that names the previous response must not end the request
// that follows it on the same connection, whether that response completed or
// failed.
func TestResponsesWebSocketIgnoresLateErrorForPreviousResponse(t *testing.T) {
	for _, tc := range []struct {
		name, firstTerminal, firstTerminalType string
	}{
		{name: "after completed", firstTerminalType: "response.completed", firstTerminal: `{"type":"response.completed","response":{"id":"first","status":"completed","usage":{"input_tokens":1000,"output_tokens":10,"total_tokens":1010}}}`},
		{name: "after error", firstTerminalType: "error", firstTerminal: `{"type":"error","response_id":"first","status":500,"error":{"type":"server_error","code":"server_error","message":"Internal server error"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newResponsesWSBillingTest(t, `tier("request", fixed(0.002))`, func(ws *websocket.Conn, _ *http.Request) {
				for _, turn := range [][]string{
					{`{"type":"response.created","response":{"id":"first","status":"in_progress"}}`, tc.firstTerminal},
					{
						`{"type":"error","response_id":"first","status":500,"error":{"type":"server_error","code":"server_error","message":"late failure"}}`,
						`{"type":"response.created","response":{"id":"second","status":"in_progress"}}`,
						`{"type":"response.completed","response":{"id":"second","status":"completed","usage":{"input_tokens":1000,"output_tokens":10,"total_tokens":1010}}}`,
					},
				} {
					if _, _, err := ws.ReadMessage(); !assert.NoError(t, err) {
						return
					}
					for _, event := range turn {
						if !assert.NoError(t, ws.WriteMessage(websocket.TextMessage, []byte(event))) {
							return
						}
					}
				}
				_, _, _ = ws.ReadMessage()
			})
			require.NoError(t, fixture.client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"ws-billing","input":"first"}`)))
			assert.Equal(t, "response.created", readResponsesWSTestEvent(t, fixture.client)["type"])
			assert.Equal(t, tc.firstTerminalType, readResponsesWSTestEvent(t, fixture.client)["type"])
			require.NoError(t, fixture.client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"ws-billing","input":"second"}`)))
			created := readResponsesWSTestEvent(t, fixture.client)
			require.Equal(t, "response.created", created["type"], "the late error must not reach the client or end the request: %v", created)
			assert.Equal(t, "response.completed", readResponsesWSTestEvent(t, fixture.client)["type"])
			fixture.closeAndWait(t)
			assertResponsesWSAccounting(t, fixture, []int{1000, 1000})
		})
	}
}

// An error that names a different response than the active one is relayed to
// the client and leaves the active generation running.
func TestResponsesWebSocketForwardsErrorForOtherResponseWithoutEndingRequest(t *testing.T) {
	const strayError = `{"type":"error","response_id":"other","status":500,"error":{"type":"server_error","code":"server_error","message":"unrelated failure"}}`
	fixture := newResponsesWSBillingTest(t, `tier("request", fixed(0.002))`, func(ws *websocket.Conn, _ *http.Request) {
		if _, _, err := ws.ReadMessage(); !assert.NoError(t, err) {
			return
		}
		for _, event := range []string{
			`{"type":"response.created","response":{"id":"active","status":"in_progress"}}`,
			strayError,
			`{"type":"response.completed","response":{"id":"active","status":"completed","usage":{"input_tokens":1000,"output_tokens":10,"total_tokens":1010}}}`,
		} {
			if !assert.NoError(t, ws.WriteMessage(websocket.TextMessage, []byte(event))) {
				return
			}
		}
		_, _, _ = ws.ReadMessage()
	})
	require.NoError(t, fixture.client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"ws-billing","input":"hi"}`)))
	assert.Equal(t, "response.created", readResponsesWSTestEvent(t, fixture.client)["type"])
	stray, err := common.Marshal(readResponsesWSTestEvent(t, fixture.client))
	require.NoError(t, err)
	assert.JSONEq(t, strayError, string(stray))
	assert.Equal(t, "response.completed", readResponsesWSTestEvent(t, fixture.client)["type"])
	fixture.closeAndWait(t)
	assertResponsesWSAccounting(t, fixture, []int{1000})
	var logs []model.Log
	require.NoError(t, model.LOG_DB.Where("type = ?", model.LogTypeConsume).Find(&logs).Error)
	require.Len(t, logs, 1)
	var other map[string]any
	require.NoError(t, common.UnmarshalJsonStr(logs[0].Other, &other))
	stream, _ := other["stream_status"].(map[string]any)
	assert.Equal(t, "ok", stream["status"])
	assert.Equal(t, "completed", stream["response_status"])
}

// Requests rejected before a RelayInfo exists reach the model health sampler
// with the HTTP classification: routing failures count against the model,
// business rejections such as a token model limit do not.
func TestResponsesWebSocketPreRoutingRejectionsFollowHealthClassification(t *testing.T) {
	require.NoError(t, i18n.Init())
	for _, tc := range []struct {
		name    string
		reject  func(*testing.T, *responsesWSBillingTest)
		status  int
		sampled bool
	}{
		{name: "no eligible channel", status: http.StatusServiceUnavailable, sampled: true, reject: func(t *testing.T, fixture *responsesWSBillingTest) {
			require.NoError(t, model.DB.Model(&model.Ability{}).Where("channel_id = ?", fixture.channel.Id).Update("enabled", false).Error)
		}},
		{name: "token model limit", status: http.StatusForbidden, reject: func(t *testing.T, fixture *responsesWSBillingTest) {
			// Update through the model so the cached token is invalidated.
			fixture.token.ModelLimitsEnabled = true
			fixture.token.ModelLimits = "other-model"
			require.NoError(t, fixture.token.Update())
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newResponsesWSBillingTest(t, `tier("request", fixed(0.002))`, func(*websocket.Conn, *http.Request) {
				t.Error("rejected request reached upstream")
			})
			tc.reject(t, fixture)
			require.NoError(t, fixture.client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"ws-billing","input":"hi"}`)))
			rejected := readResponsesWSTestEvent(t, fixture.client)
			assert.Equal(t, "error", rejected["type"])
			assert.Equal(t, float64(tc.status), rejected["status"])
			fixture.closeAndWait(t)
			assertResponsesWSAccounting(t, fixture, nil)
			if !tc.sampled {
				keys, err := common.RDB.Keys(context.Background(), "perf:ws-billing:*").Result()
				require.NoError(t, err)
				assert.Empty(t, keys, "a business rejection must not affect model health")
				return
			}
			requests, successes := waitPerfCounters(t, 1)
			assert.Equal(t, int64(1), requests)
			assert.Zero(t, successes)
		})
	}
}

func TestCyberFallbackResponsesRoutingAndBilling(t *testing.T) {
	if isolateResponsesAuditTest(t) {
		return
	}
	const session = "019b0ac1-fbc3-7300-876a-d85d2b89c897"
	const completed = `{"type":"response.completed","response":{"id":"resp_fallback","object":"response","status":"completed","model":"gpt-4o","output":[],"usage":{"input_tokens":4,"output_tokens":3,"total_tokens":7}}}`
	fixture := newResponsesWSBillingTest(t, `tier("request", fixed(0.002))`, func(ws *websocket.Conn, _ *http.Request) { t.Error("original WebSocket must not be called") })
	lookup, rawToken := setupResponsesAuditLookup(t, false, false, fixture.user.Id)
	expectedPayloads := map[string][2]string{}
	fixture.httpDone = make(chan struct{}, 16)
	var originalCalls, fallbackCalls, failureStatus, continuations atomic.Int32
	var responseStatus atomic.Value
	responseStatus.Store("completed")
	fixture.httpUpstream = func(w http.ResponseWriter, r *http.Request) {
		originalCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"error":{"code":"cyber_policy","message":"rejected"}}`)
	}
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer fallback-key", r.Header.Get("Authorization"))
		if websocket.IsWebSocketUpgrade(r) {
			ws, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
			if !assert.NoError(t, err) {
				return
			}
			defer ws.Close()
			for {
				_, body, err := ws.ReadMessage()
				if err != nil {
					return
				}
				assert.Contains(t, string(body), `"type":"response.create"`)
				n := fallbackCalls.Add(1)
				frame := strings.ReplaceAll(completed, "resp_fallback", fmt.Sprintf("resp_fallback_%d", n))
				if ws.WriteMessage(websocket.TextMessage, []byte(frame)) != nil {
					return
				}
			}
		}
		fallbackCalls.Add(1)
		if status := failureStatus.Load(); status > 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(int(status))
			if status == http.StatusForbidden {
				_, _ = io.WriteString(w, `{"error":{"code":"cyber_policy"}}`)
			} else {
				_, _ = io.WriteString(w, `{"error":{"type":"server_error","code":"upstream_unavailable","message":"Temporarily unavailable"}}`)
			}
			return
		}
		var request struct {
			Stream             bool   `json:"stream"`
			PreviousResponseID string `json:"previous_response_id"`
		}
		require.NoError(t, common.DecodeJson(r.Body, &request))
		if request.PreviousResponseID != "" {
			assert.Equal(t, "resp_fallback", request.PreviousResponseID)
			continuations.Add(1)
		}
		if request.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			frame := strings.ReplaceAll(completed, "completed", responseStatus.Load().(string))
			_, _ = fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", frame)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		body := `{"id":"resp_fallback","object":"response","status":"completed","model":"gpt-4o","output":[],"usage":{"input_tokens":4,"output_tokens":3,"total_tokens":7}}`
		_, _ = io.WriteString(w, strings.ReplaceAll(body, "completed", responseStatus.Load().(string)))
	}))
	t.Cleanup(fallback.Close)
	channel := &model.Channel{Name: "cyber-fallback", Key: "fallback-key", Status: common.ChannelStatusEnabled, Type: constant.ChannelTypeOpenAI, Group: "default", Models: "ws-billing", BaseURL: &fallback.URL, ModelMapping: fixture.channel.ModelMapping}
	channel.SetSetting(dto.ChannelSettings{ResponsesWebSocketEnabled: true})
	require.NoError(t, model.DB.Create(channel).Error)
	priority := int64(100)
	require.NoError(t, model.DB.Model(fixture.channel).Update("priority", priority).Error)
	require.NoError(t, model.DB.Model(&model.Ability{}).Where("channel_id = ?", fixture.channel.Id).Update("priority", priority).Error)
	require.NoError(t, model.DB.Create(&model.Ability{ChannelId: channel.Id, Model: "ws-billing", Group: "default", Enabled: true}).Error)
	require.NoError(t, model.DB.Model(fixture.token).Update("remain_quota", 100000).Error)
	require.NoError(t, model.InvalidateTokenCaches([]string{fixture.token.Key}))
	t.Cleanup(func() {
		require.NoError(t, model.DB.Where("channel_id = ?", channel.Id).Delete(&model.Ability{}).Error)
		require.NoError(t, model.DB.Delete(channel).Error)
	})
	require.NoError(t, model.DB.AutoMigrate(&model.Option{}, &model.GroupRenameAlias{}))
	common.OptionMapRWMutex.Lock()
	wasNil := common.OptionMap == nil
	if wasNil {
		common.OptionMap = make(map[string]string)
	}
	old, had := common.OptionMap[operation_setting.CyberPolicyOptionKey]
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		if wasNil {
			common.OptionMap = nil
		} else if had {
			common.OptionMap[operation_setting.CyberPolicyOptionKey] = old
		} else {
			delete(common.OptionMap, operation_setting.CyberPolicyOptionKey)
		}
		common.OptionMapRWMutex.Unlock()
	})
	policy := service.CyberPolicy{Mode: "enforce", Scope: "all", TTLSeconds: 600, BlockedAction: "reroute", FallbackChannelID: channel.Id}
	save := func() {
		data, err := common.Marshal(policy)
		require.NoError(t, err)
		require.NoError(t, model.UpdateOption(operation_setting.CyberPolicyOptionKey, string(data)))
		assert.Equal(t, policy.FallbackChannelID, service.CurrentCyberPolicy().FallbackChannelID)
	}
	save()
	httpSequence := 0
	request := func(extra string) (int, string) {
		httpSequence++
		id := fmt.Sprintf("cyber-http-%d", httpSequence)
		body := fmt.Sprintf(`{"model":"ws-billing","input":"hello","prompt_cache_key":%q%s}`, session, extra)
		req, err := http.NewRequest("POST", fixture.gatewayURL+"/v1/responses", strings.NewReader(body))
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer sk-"+fixture.token.Key)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Test-Request-ID", id)
		response, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer response.Body.Close()
		data, err := io.ReadAll(response.Body)
		require.NoError(t, err)
		expectedPayloads[id] = [2]string{body, string(data)}
		return response.StatusCode, string(data)
	}
	status, body := request("")
	assert.Equal(t, 403, status, body)
	assert.Zero(t, fallbackCalls.Load(), "first upstream Cyber must not trigger a fallback retry")
	status, body = request("")
	assert.Equal(t, 200, status, body)
	assert.EqualValues(t, 1, originalCalls.Load())
	assert.EqualValues(t, 1, fallbackCalls.Load())
	status, body = request(`,"previous_response_id":"resp_fallback"`)
	assert.Equal(t, 200, status, body)
	assert.EqualValues(t, 2, fallbackCalls.Load())
	assert.EqualValues(t, 1, continuations.Load(), "the normal relay preserves the provider continuation identifier")
	common.LogConsumeEnabled = false
	status, body = request(`,"stream":true`)
	assert.Equal(t, 200, status, body)
	assert.Contains(t, body, "response.completed")
	for round := range 2 {
		extra := ""
		if round > 0 {
			extra = `,"previous_response_id":"resp_fallback_4"`
		}
		frame := fmt.Sprintf(`{"type":"response.create","model":"ws-billing","input":"hello","prompt_cache_key":%q%s}`, session, extra)
		require.NoError(t, fixture.client.WriteMessage(websocket.TextMessage, []byte(frame)))
		_, response, err := fixture.client.ReadMessage()
		require.NoError(t, err)
		var event map[string]any
		require.NoError(t, common.Unmarshal(response, &event))
		require.Equal(t, "response.completed", event["type"], "%v", event)
		expectedPayloads[fmt.Sprintf("responses-ws-billing-ws-%d", round)] = [2]string{frame, string(response) + "\n"}
		if round == 0 {
			bans, _, err := service.ListCyberBans(t.Context(), 0)
			require.NoError(t, err)
			require.Len(t, bans, 1)
			require.NoError(t, service.RevokeCyberBan(t.Context(), bans[0].CyberBanTarget, bans[0].Generation))
		}
	}
	assert.EqualValues(t, 5, fallbackCalls.Load())
	// After revocation, a new independent HTTP request uses normal routing again.
	status, body = request("")
	assert.Equal(t, 403, status, body)
	assert.EqualValues(t, 2, originalCalls.Load())
	responseStatus.Store("incomplete")
	for _, extra := range []string{"", `,"stream":true`} {
		status, body = request(extra)
		assert.Equal(t, http.StatusOK, status, body)
		assert.Contains(t, body, `"status":"incomplete"`)
	}
	responseStatus.Store("completed")
	failureStatus.Store(http.StatusServiceUnavailable)
	status, body = request("")
	assert.GreaterOrEqual(t, status, 400, body)
	assert.Contains(t, body, "upstream_unavailable")
	assert.EqualValues(t, 8, fallbackCalls.Load(), "ordinary failure must not escape to an unrelated channel")
	failureStatus.Store(http.StatusForbidden)
	status, body = request("")
	assert.Equal(t, 403, status, body)
	assert.EqualValues(t, 9, fallbackCalls.Load())
	status, body = request("")
	assert.Equal(t, http.StatusForbidden, status, body)
	assert.Contains(t, body, `"code":"cyber_policy"`)
	assert.NotContains(t, body, "cyber_fallback_")
	assert.EqualValues(t, 9, fallbackCalls.Load(), "the rejected fallback must not be called again in this batch")
	failedEvents, err := model.CyberEvents(t.Context(), model.CyberEventFilter{Start: 1, End: time.Now().Unix(), TokenID: fixture.token.Id, Action: "reroute_failed"})
	require.NoError(t, err)
	assert.Len(t, failedEvents, 2)
	policy.BlockedAction = "block"
	policy.FallbackChannelID = 0
	save()
	const blockedFrame = `{"type":"response.create","model":"ws-billing","input":"after policy change"}`
	require.NoError(t, fixture.client.WriteMessage(websocket.TextMessage, []byte(blockedFrame)))
	_, blockedResponse, err := fixture.client.ReadMessage()
	require.NoError(t, err)
	var event map[string]any
	require.NoError(t, common.Unmarshal(blockedResponse, &event))
	expectedPayloads["responses-ws-billing-ws-2"] = [2]string{blockedFrame, string(blockedResponse) + "\n"}
	assert.Equal(t, "error", event["type"])
	assert.Equal(t, float64(http.StatusConflict), event["status"])
	assert.Equal(t, "invalid_request_error", event["error"].(map[string]any)["type"])
	assert.Equal(t, "invalid_request", event["error"].(map[string]any)["code"])
	assert.NotContains(t, string(blockedResponse), "cyber_fallback_")
	_, _, err = fixture.client.ReadMessage()
	require.Error(t, err)
	var user model.User
	require.NoError(t, model.DB.First(&user, fixture.user.Id).Error)
	assert.Equal(t, 93000, user.Quota, "five completed and two incomplete fallback calls are billed once each for their actual usage")
	var token model.Token
	require.NoError(t, model.DB.First(&token, fixture.token.Id).Error)
	assert.Equal(t, 93000, token.RemainQuota)
	interrupted, err := model.CyberEvents(t.Context(), model.CyberEventFilter{Start: 1, End: time.Now().Unix(), TokenID: fixture.token.Id, Action: "reroute_interrupted"})
	require.NoError(t, err)
	assert.Len(t, interrupted, 2, "HTTP 200 does not make incomplete JSON or SSE responses completed")
	events, err := model.CyberEvents(t.Context(), model.CyberEventFilter{Start: 1, End: time.Now().Unix(), TokenID: fixture.token.Id, Action: "reroute_completed"})
	require.NoError(t, err)
	require.Len(t, events, 5, "successful system events remain queryable with consume logs disabled")
	for _, event := range events {
		require.NotNil(t, event.Fallback)
		assert.Equal(t, channel.Id, event.Fallback.ActualChannelID)
		assert.Equal(t, fixture.channel.Id, event.Fallback.SourceChannelID)
		assert.Equal(t, "default", event.Fallback.Group)
	}
	for page := 1; page <= 2; page++ {
		response := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(response)
		c.Request = httptest.NewRequest("GET", fmt.Sprintf("/api/cyber/events?start=%d&end=%d&token_id=%d&action=reroute_completed&p=%d&page_size=2", time.Now().Add(-time.Hour).Unix(), time.Now().Unix(), fixture.token.Id, page), nil)
		GetCyberEvents(c)
		assert.Equal(t, 200, response.Code)
		var result struct {
			Success bool `json:"success"`
			Data    struct {
				Total int                `json:"total"`
				Items []model.CyberEvent `json:"items"`
			} `json:"data"`
		}
		require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
		assert.True(t, result.Success)
		assert.Equal(t, 5, result.Data.Total)
		assert.Len(t, result.Data.Items, 2)
	}
	logs, _, err := model.GetUserLogs(fixture.user.Id, model.LogTypeUnknown, 0, 0, "", "", 0, 100, "", "", "", "")
	require.NoError(t, err)
	for _, log := range logs {
		assert.NotContains(t, log.Other, `"cyber_fallback":`, "route details must remain admin-only")
	}
	for requestID, payloads := range expectedPayloads {
		req := httptest.NewRequest(http.MethodGet, "/api/custom/conversation-audits/"+requestID, nil)
		req.Header.Set("Authorization", "Bearer "+rawToken)
		result := httptest.NewRecorder()
		lookup.ServeHTTP(result, req)
		require.Equal(t, http.StatusOK, result.Code, "%s: %s", requestID, result.Body.String())
		var detail struct {
			Data struct {
				Audit             conversationaudit.ConversationAudit
				Request, Response string
			}
		}
		require.NoError(t, common.Unmarshal(result.Body.Bytes(), &detail))
		assert.True(t, detail.Data.Audit.FullPayload, requestID)
		assert.Empty(t, detail.Data.Audit.CaptureError, requestID)
		assert.Equal(t, payloads[0], detail.Data.Request, requestID)
		assert.Equal(t, payloads[1], detail.Data.Response, requestID)
	}

	policy.CaptureFullPayload = common.GetPointer(false)
	save()
	var saved model.Option
	require.NoError(t, model.DB.Where(&model.Option{Key: operation_setting.CyberPolicyOptionKey}).Take(&saved).Error)
	var savedPolicy service.CyberPolicy
	require.NoError(t, common.UnmarshalJsonStr(saved.Value, &savedPolicy))
	require.NotNil(t, savedPolicy.CaptureFullPayload)
	assert.False(t, *savedPolicy.CaptureFullPayload)
	status, body = request("")
	assert.Equal(t, http.StatusForbidden, status, body)
	var summary conversationaudit.ConversationAudit
	require.Eventually(t, func() bool {
		return model.DB.Where("request_id = ?", fmt.Sprintf("cyber-http-%d", httpSequence)).Take(&summary).Error == nil
	}, time.Second, time.Millisecond)
	assert.False(t, summary.FullPayload, "changing the persisted switch applies to the next request")

}

func isolateResponsesAuditTest(t *testing.T) bool {
	t.Helper()
	// Audit registration starts a process-wide worker against the current DB.
	// Isolate it from the other tests, which replace and close their own DBs.
	const worker = "TEST_RESPONSES_WS_AUDIT_WORKER"
	if os.Getenv(worker) != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^"+t.Name()+"$", "-test.v")
		cmd.Env = append(os.Environ(), worker+"=1")
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", output)
		return true
	}

	return false
}

func setupResponsesAuditLookup(t *testing.T, fullPayload, captureWSSuccess bool, userID int) (*gin.Engine, string) {
	t.Helper()
	t.Setenv("CONVERSATION_AUDIT_ENABLED", "true")
	t.Setenv("CONVERSATION_AUDIT_CAPTURE_WS_SUCCESS", strconv.FormatBool(captureWSSuccess))
	t.Setenv("CONVERSATION_AUDIT_ACTIVE_KEY_VERSION", "v1")
	t.Setenv("CONVERSATION_AUDIT_KEY_V1", base64.StdEncoding.EncodeToString([]byte("12345678901234567890123456789012")))
	require.NoError(t, model.DB.AutoMigrate(&conversationaudit.AuditSettings{}, &conversationaudit.ConversationAudit{},
		&conversationaudit.ConversationAuditResponseLink{}, &conversationaudit.ConversationAuditResponseSegment{},
		&conversationaudit.ConversationAuditPendingResponse{}, &model.UserAccessToken{}, &model.AuditLog{}))
	require.NoError(t, model.DB.Create(&conversationaudit.AuditSettings{
		ID: 1, Enabled: true, CaptureFullPayload: fullPayload, RetentionDays: 30, MaxContentBytes: 262144, ActiveKeyVersion: "v1",
	}).Error)
	admin := model.User{Username: "ws-audit-admin", AffCode: "ws-aud", Role: common.RoleAdminUser, Status: common.UserStatusEnabled, AuthVersion: 1}
	require.NoError(t, model.DB.Create(&admin).Error)
	rawToken := model.AccessTokenPrefix + strings.Repeat("a", 43)
	pat := model.UserAccessToken{UserId: admin.Id, Name: "audit test", TokenHash: model.AccessTokenFingerprint(rawToken), TokenHint: model.AccessTokenHint(rawToken), ExpiresAt: time.Now().Add(time.Hour).Unix()}
	require.NoError(t, pat.SetScopes([]string{"log:read"}))
	require.NoError(t, model.DB.Create(&pat).Error)
	lookup := gin.New()
	conversationaudit.Register(lookup.Group("/api"))

	t.Cleanup(func() {
		var audits []conversationaudit.ConversationAudit
		require.NoError(t, model.DB.Where("user_id = ?", userID).Find(&audits).Error)
		for _, audit := range audits {
			require.NoError(t, model.DB.Where("audit_id = ?", audit.ID).Delete(&conversationaudit.ConversationAuditPendingResponse{}).Error)
			require.NoError(t, model.DB.Where("audit_id = ?", audit.ID).Delete(&conversationaudit.ConversationAuditResponseSegment{}).Error)
			require.NoError(t, model.DB.Where("audit_id = ?", audit.ID).Delete(&conversationaudit.ConversationAuditResponseLink{}).Error)
		}
		require.NoError(t, model.DB.Where("user_id = ?", userID).Delete(&conversationaudit.ConversationAudit{}).Error)
		require.NoError(t, model.DB.Delete(&conversationaudit.AuditSettings{ID: 1}).Error)
		require.NoError(t, model.DB.Delete(&pat).Error)
		require.NoError(t, model.DB.Unscoped().Delete(&admin).Error)
	})

	return lookup, rawToken
}
