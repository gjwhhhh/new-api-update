package service

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaydto "github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func cyberFallbackTestContext(body string) *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	common.SetContextKey(c, constant.ContextKeyUserGroup, "default")
	RequestPolicy(c).UserSessionPolicyLoaded = true
	return c
}

func TestCyberFallbackPolicy(t *testing.T) {
	p := CyberPolicy{Mode: "enforce", Scope: "all", TTLSeconds: 600}
	p.Normalize()
	assert.Equal(t, "block", p.BlockedAction)
	require.NoError(t, p.Validate())
	p.BlockedAction = "reroute"
	require.Error(t, p.Validate())
	p.FallbackChannelID = 12
	require.NoError(t, p.Validate())

	p.NotifyWebhookURL = "not-a-url"
	require.Error(t, p.Validate())
	p.NotifyWebhookURL = ""
	require.NoError(t, p.Validate())
	p.NotifyWebhookURL = "https://hooks.example.com/cyber"
	require.NoError(t, p.Validate())
	p.NotifyWebhookSecret = strings.Repeat("x", 257)
	require.Error(t, p.Validate())
	p.NotifyWebhookSecret = strings.Repeat("x", 256)
	require.NoError(t, p.Validate())
}

func TestCyberFallbackErrorsExposePublicCodes(t *testing.T) {
	for _, tc := range []struct {
		reason      string
		status      int
		code, etype string
	}{
		{reason: "binding_conflict", status: http.StatusConflict, code: "invalid_request", etype: "invalid_request_error"},
		{reason: "reconnect_required", status: http.StatusConflict, code: "invalid_request", etype: "invalid_request_error"},
		{reason: "target_rejected", status: http.StatusForbidden, code: "cyber_policy", etype: "invalid_request"},
		{reason: "same_source", status: http.StatusForbidden, code: "cyber_policy", etype: "invalid_request"},
		{reason: "model_forbidden", status: http.StatusForbidden, code: "permission_denied", etype: "invalid_request_error"},
		{reason: "state_unavailable", status: http.StatusServiceUnavailable, code: "server_error", etype: "server_error"},
		{reason: "target_unavailable", status: http.StatusServiceUnavailable, code: "server_error", etype: "server_error"},
		{reason: "stateful_context", status: http.StatusConflict, code: "invalid_request", etype: "invalid_request_error"},
	} {
		t.Run(fmt.Sprintf("%s/%d", tc.reason, tc.status), func(t *testing.T) {
			c := cyberFallbackTestContext(`{"input":"hello"}`)
			armed := false
			c.Set(cyberSessionArmKey, func() { armed = true })
			apiErr := cyberFallbackError(c, tc.reason, tc.status)
			assert.Equal(t, tc.status, apiErr.StatusCode)
			public := apiErr.ToOpenAIError()
			assert.Equal(t, tc.code, public.Code)
			assert.Equal(t, tc.etype, public.Type)
			assert.NotContains(t, public.Message, tc.reason)
			assert.Equal(t, tc.code, apiErr.ToClaudeError().Type)
			assert.True(t, types.IsSkipRetryError(apiErr))
			SetRelayFinalError(c, apiErr)
			assert.False(t, armed, "a local routing failure must not create another upstream rejection ban")
			failure, found := FinalRelayFailure(c)
			require.True(t, found)
			assert.Equal(t, "cyber_fallback_"+tc.reason, failure.Code)
		})
	}
}

func TestCyberFallbackForwardsStatefulContext(t *testing.T) {
	for _, body := range []string{
		`{"previous_response_id":"resp_parent","input":"next"}`,
		`{"conversation":"conv_private","input":[{"type":"reasoning","id":"rs_private"}]}`,
		`{"input":[{"type":"function_call_output","call_id":"call_private","output":"result"}]}`,
		`{"input":[{"type":"item_reference","id":"item_private"}]}`,
	} {
		t.Run(body, func(t *testing.T) {
			c := cyberFallbackTestContext(body)
			session := cyberRequestSession{policy: CyberPolicy{BlockedAction: "reroute", FallbackChannelID: 94}}
			require.Nil(t, activateCyberFallback(c, session, cyberFallbackBan{meta: CyberBanMetadata{ID: "ban", SourceChannelID: 79}}))
			require.True(t, IsCyberFallback(c))
			assert.Equal(t, 94, cyberFallback(c).TargetChannelID)
			raw, err := io.ReadAll(c.Request.Body)
			require.NoError(t, err)
			assert.Equal(t, body, string(raw))
			require.Len(t, GetChannelConstraints(c).Filters, 1)
			assert.Equal(t, []int{94}, GetChannelConstraints(c).Filters[0].AllowedChannelIDs)
		})
	}
}

func TestCyberFallbackAutoGroupSelectionDoesNotConsumeUnsentCandidate(t *testing.T) {
	for _, entry := range []string{"request", "pool"} {
		for _, tc := range []struct {
			name                     string
			sourceGroup, targetGroup string
			retryLimit, priorRetry   int
			groupIndex               int
			unavailable              bool
		}{
			{name: "same group without retries", sourceGroup: "default", targetGroup: "default"},
			{name: "next group without retries", sourceGroup: "default", targetGroup: "vip"},
			{name: "preserves prior attempts", sourceGroup: "default", targetGroup: "default", retryLimit: 1, priorRetry: 1},
			{name: "does not rewind an exhausted group", sourceGroup: "vip", targetGroup: "default", groupIndex: 1, unavailable: true},
			{name: "does not expand permitted groups", sourceGroup: "default", targetGroup: "private", unavailable: true},
		} {
			t.Run(entry+"/"+tc.name, func(t *testing.T) {
				db := setupChannelSelectAutoGroupsTest(t)
				common.RetryTimes = tc.retryLimit
				createChannelSelectAutoGroupsChannel(t, db, 4101, tc.sourceGroup, "test-model")
				createChannelSelectAutoGroupsChannel(t, db, 4102, tc.targetGroup, "test-model")
				if tc.priorRetry > 0 {
					createChannelSelectAutoGroupsChannel(t, db, 4103, "default", "test-model")
					require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 4103).Update("priority", 20).Error)
					require.NoError(t, db.Model(&model.Ability{}).Where("channel_id = ?", 4103).Update("priority", 20).Error)
				}
				require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 4101).Update("priority", 10).Error)
				require.NoError(t, db.Model(&model.Ability{}).Where("channel_id = ?", 4101).Update("priority", 10).Error)
				model.InitChannelCache()
				c := cyberFallbackTestContext(`{"input":"hello"}`)
				common.SetContextKey(c, constant.ContextKeyUsingGroup, "auto")
				common.SetContextKey(c, constant.ContextKeyTokenAutoGroups, []string{"default", "vip"})
				common.SetContextKey(c, constant.ContextKeyTokenCrossGroupRetry, true)
				// This cursor is the state left by earlier real attempts, not a fresh request.
				common.SetContextKey(c, constant.ContextKeyAutoGroupIndex, tc.groupIndex)
				session := cyberRequestSession{policy: CyberPolicy{Mode: "enforce", Scope: "channel", ChannelIDs: []int{4101}, BlockedAction: "reroute", FallbackChannelID: 4102}, tokenID: 84, digest: strings.Repeat("b", 64)}
				c.Set(cyberSessionRequestKey, session)
				c.Set(cyberFallbackBansKey, map[string]cyberFallbackBan{session.redisKey(4101): {key: session.redisKey(4101), meta: CyberBanMetadata{ID: "test-batch", SourceChannelID: 4101}}})
				param := &RetryParam{Ctx: c, TokenGroup: "auto", ModelName: "test-model", Retry: common.GetPointer(tc.priorRetry)}
				var channel *model.Channel
				var group string
				if entry == "request" {
					var err *ChannelSelectError
					channel, group, err = SelectChannelForRequest(c, "test-model", param)
					if tc.unavailable {
						require.NotNil(t, err)
						assert.Nil(t, channel)
						assert.Equal(t, "cyber_fallback_target_unavailable", string(err.Code))
						return
					}
					require.Nil(t, err)
				} else {
					var err error
					channel, group, err = CacheGetRandomSatisfiedChannel(param)
					if tc.unavailable {
						require.Error(t, err)
						assert.Nil(t, channel)
						return
					}
					require.NoError(t, err)
				}
				require.NotNil(t, channel)
				require.True(t, IsCyberFallback(c))
				assert.Equal(t, 4102, channel.Id)
				assert.Equal(t, tc.targetGroup, group)
				param.IncreaseRetry()
				channel, _, err := CacheGetRandomSatisfiedChannel(param)
				require.Error(t, err, "the selected fallback consumes the original attempt budget")
				assert.Nil(t, channel)
			})
		}
	}
}

func TestCyberFallbackSelectionPreservesCandidateIntersection(t *testing.T) {
	db := setupChannelSelectAutoGroupsTest(t)
	createChannelSelectAutoGroupsChannel(t, db, 3101, "default", "test-model")
	createChannelSelectAutoGroupsChannel(t, db, 3102, "vip", "test-model")
	createChannelSelectAutoGroupsChannel(t, db, 3103, "default", "other-model")
	model.InitChannelCache()
	for _, tc := range []struct {
		name    string
		target  int
		pins    []dto.ChannelPin
		filters []dto.ChannelFilter
		want    int
	}{
		{name: "eligible", target: 3101, want: 3101},
		{name: "wrong group", target: 3102},
		{name: "wrong model", target: 3103},
		{name: "hidden origin pin conflicts", target: 3101, pins: []dto.ChannelPin{{ChannelId: 3101, Rank: 0}, {ChannelId: 3102, Rank: 10}}},
		{name: "WS capability required", target: 3101, filters: []dto.ChannelFilter{{Kind: dto.FilterResponsesWebSocket}}},
		{name: "plugin identity required", target: 3101, filters: []dto.ChannelFilter{{Kind: dto.FilterTaskPluginIdentity, TaskPluginKey: "video"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := cyberFallbackTestContext(`{"input":"hello"}`)
			c.Set(cyberFallbackKey, &cyberFallbackRoute{TargetChannelID: tc.target})
			constraints := GetChannelConstraints(c)
			constraints.Pins = tc.pins
			constraints.Filters = append(tc.filters, dto.ChannelFilter{Kind: dto.FilterAllowedChannelIDs, AllowedChannelIDs: []int{tc.target}})
			param := &RetryParam{Ctx: c, TokenGroup: "default", ModelName: "test-model"}
			ch, _, err := SelectChannelForRequest(c, "test-model", param)
			if tc.want == 0 {
				require.NotNil(t, err)
				assert.Nil(t, ch)
				return
			}
			require.Nil(t, err)
			require.NotNil(t, ch)
			assert.Equal(t, tc.want, ch.Id)
			param.ExcludeChannel(ch.Id)
			ch, _, retryErr := CacheGetRandomSatisfiedChannel(param)
			require.Error(t, retryErr)
			assert.Nil(t, ch, "retry cannot escape to another channel")
		})
	}
}

func TestCyberFallbackRedisLifecycle(t *testing.T) {
	client := useChannelAffinityRedisForTest(t)
	if addr := os.Getenv("TEST_CYBER_REDIS_ADDR"); addr != "" {
		client = redis.NewClient(&redis.Options{Addr: addr})
		require.NoError(t, client.Ping(t.Context()).Err())
		common.RDB = client
		t.Cleanup(func() { require.NoError(t, client.Close()) })
	}
	for _, permanent := range []bool{false, true} {
		t.Run(fmt.Sprintf("permanent=%t", permanent), func(t *testing.T) {
			session := cyberRequestSession{policy: CyberPolicy{Mode: "enforce", Scope: "all", BlockedAction: "reroute", FallbackChannelID: 12}, tokenID: 84, digest: fmt.Sprintf("%x", sha256.Sum256([]byte(t.Name()+uuid.NewString())))}
			// Fingerprints use the same canonical SHA-256 representation as production.

			parent := session.redisKey(0)
			ttl := 2 * time.Minute
			if permanent {
				ttl = 0
			}
			require.NoError(t, client.Set(t.Context(), parent, "1", ttl).Err())
			t.Cleanup(func() { client.Del(context.Background(), parent) })
			read := func(target int) (*gin.Context, *cyberFallbackRoute) {
				s := session
				s.policy.FallbackChannelID = target
				c := cyberFallbackTestContext(`{"input":"hello"}`)
				c.Set(cyberSessionRequestKey, s)
				require.Nil(t, prepareCyberFallbackBans(c, s, nil))
				return c, &cyberFallbackRoute{TargetChannelID: target}
			}
			c, _ := read(12)
			require.Nil(t, checkCyberSession(c, session, 0))
			route := cyberFallback(c)
			require.NotNil(t, route)
			assert.NotEmpty(t, route.BatchID)
			child := cyberFallbackSetKey(parent, route.BatchID)
			t.Cleanup(func() { client.Del(context.Background(), child) })
			if permanent {
				assert.Equal(t, cyberSessionNoExpiry, client.PTTL(t.Context(), parent).Val())
			} else {
				assert.LessOrEqual(t, client.PTTL(t.Context(), parent).Val(), ttl)
			}
			// Concurrent reports share a bounded set and never extend the parent's TTL.
			var wg sync.WaitGroup
			for range 4 {
				wg.Go(func() {
					rejectCyberFallback(cyberFallbackTestContext(`{}`), &cyberFallbackRoute{TargetChannelID: 12, BatchID: route.BatchID, ban: route.ban})
				})
			}
			wg.Wait()
			assert.EqualValues(t, 1, client.SCard(t.Context(), child).Val())
			assert.LessOrEqual(t, client.PTTL(t.Context(), child).Val(), client.PTTL(t.Context(), parent).Val()+time.Millisecond)
			c, _ = read(12)
			err := checkCyberSession(c, session, 0)
			require.NotNil(t, err)
			assert.Equal(t, "cyber_fallback_target_rejected", string(err.GetErrorCode()))
			s := session
			s.policy.FallbackChannelID = 13
			c, _ = read(13)
			require.Nil(t, checkCyberSession(c, s, 0))
			// Revocation is generation checked and removes permanent child state too.
			generation := fmt.Sprintf("%x", sha256.Sum256([]byte(route.ban.raw)))
			require.NoError(t, RevokeCyberBan(t.Context(), CyberBanTarget{TokenID: session.tokenID, Fingerprint: session.digest}, generation))
			assert.Zero(t, client.Exists(t.Context(), parent, child).Val())
			rejectCyberFallback(cyberFallbackTestContext(`{}`), route)
			assert.Zero(t, client.Exists(t.Context(), parent, child).Val(), "late failure cannot resurrect a revoked batch")
			// Recreate the parent; a stale writer cannot poison the new batch.
			require.NoError(t, client.Set(t.Context(), parent, "1", ttl).Err())
			c, _ = read(12)
			require.Nil(t, checkCyberSession(c, session, 0))
			next := cyberFallback(c)
			require.NotNil(t, next)
			assert.NotEqual(t, route.BatchID, next.BatchID)
			t.Cleanup(func() { client.Del(context.Background(), cyberFallbackSetKey(parent, next.BatchID)) })
			rejectCyberFallback(cyberFallbackTestContext(`{}`), route)
			assert.Zero(t, client.SCard(t.Context(), cyberFallbackSetKey(parent, next.BatchID)).Val())
			// A full rejection set refuses new targets without deleting old members.
			members := make([]any, cyberFallbackLimit)
			for i := range members {
				members[i] = i + 100
			}
			require.NoError(t, client.SAdd(t.Context(), cyberFallbackSetKey(parent, next.BatchID), members...).Err())
			c, _ = read(13)
			err = checkCyberSession(c, s, 0)
			require.NotNil(t, err)
			assert.Equal(t, "cyber_fallback_target_rejected", string(err.GetErrorCode()))
		})
	}
}

func TestCyberFallbackConfirmedStorageFailureDoesNotReleaseBan(t *testing.T) {
	client := useChannelAffinityRedisForTest(t)
	session := cyberRequestSession{policy: CyberPolicy{Mode: "enforce", Scope: "all", BlockedAction: "reroute", FallbackChannelID: 12}, tokenID: 85, digest: strings.Repeat("a", 64)}
	meta := CyberBanMetadata{ID: uuid.NewString()}
	data, err := common.Marshal(meta)
	require.NoError(t, err)
	require.NoError(t, client.Set(t.Context(), session.redisKey(0), data, 0).Err())
	require.NoError(t, client.Set(t.Context(), cyberFallbackSetKey(session.redisKey(0), meta.ID), "wrong type", 0).Err())
	c := cyberFallbackTestContext(`{"input":"hello"}`)
	require.Nil(t, prepareCyberFallbackBans(c, session, nil))
	apiErr := checkCyberSession(c, session, 0)
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusServiceUnavailable, apiErr.StatusCode)

	// One corrupt parent must not make the entire bounded read fail open.
	session.policy.Scope = "channel"
	session.policy.ChannelIDs = []int{21}
	require.NoError(t, client.SAdd(t.Context(), session.redisKey(21), "wrong parent type").Err())
	c = cyberFallbackTestContext(`{"input":"hello"}`)
	require.Nil(t, prepareCyberFallbackBans(c, session, nil))
	apiErr = checkCyberSession(c, session, 0)
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusServiceUnavailable, apiErr.StatusCode, "the global ban remains enforced despite another corrupt parent")
	c = cyberFallbackTestContext(`{"input":"hello"}`)
	require.Nil(t, prepareCyberFallbackBans(c, session, nil))
	apiErr = checkCyberSession(c, session, 21)
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusServiceUnavailable, apiErr.StatusCode, "the corrupt parent itself is unavailable rather than unbanned")
}

func TestCyberFallbackPolicyReloadDefaultsToBlock(t *testing.T) {
	common.OptionMapRWMutex.Lock()
	previous := common.OptionMap
	common.OptionMap = map[string]string{operation_setting.CyberPolicyOptionKey: `{"mode":"enforce","scope":"all","ttl_seconds":600}`}
	common.OptionMapRWMutex.Unlock()
	previousSnapshot := cyberSessionSnapshot.Load()
	cyberSessionSnapshot.Store(nil)
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previous
		common.OptionMapRWMutex.Unlock()
		cyberSessionSnapshot.Store(previousSnapshot)
	})
	assert.Equal(t, "block", CurrentCyberPolicy().BlockedAction)
	require.NotNil(t, CurrentCyberPolicy().CaptureFullPayload)
	assert.True(t, *CurrentCyberPolicy().CaptureFullPayload)
	common.OptionMapRWMutex.Lock()
	common.OptionMap[operation_setting.CyberPolicyOptionKey] = `{"mode":"enforce","scope":"all","ttl_seconds":600,"capture_full_payload":false}`
	common.OptionMapRWMutex.Unlock()
	assert.False(t, *CurrentCyberPolicy().CaptureFullPayload)
	cyberSessionSnapshot.Store(nil)
	assert.False(t, *CurrentCyberPolicy().CaptureFullPayload, "reload preserves an explicit disabled value")
	common.OptionMapRWMutex.Lock()
	common.OptionMap[operation_setting.CyberPolicyOptionKey] = `{"mode":"enforce","scope":"all","ttl_seconds":600,"fallback_channel_id":"invalid"}`
	common.OptionMapRWMutex.Unlock()
	assert.Equal(t, "block", CurrentCyberPolicy().BlockedAction, "invalid reload retains the last valid policy")
}

func TestCyberFallbackChannelScopeAndAffinityIsolation(t *testing.T) {
	client := useChannelAffinityRedisForTest(t)
	session := cyberRequestSession{policy: CyberPolicy{Mode: "enforce", Scope: "channel", ChannelIDs: []int{21, 22}, BlockedAction: "reroute", FallbackChannelID: 22}, tokenID: 86, digest: strings.Repeat("b", 64)}
	require.NoError(t, client.Set(t.Context(), session.redisKey(21), "1", time.Minute).Err())
	c := cyberFallbackTestContext(`{"input":"hello"}`)
	c.Set(cyberSessionRequestKey, session)
	require.Nil(t, prepareCyberFallbackBans(c, session, nil))
	require.Nil(t, CheckCyberSessionChannel(c, 23))
	assert.False(t, IsCyberFallback(c), "outside channel scope stays on normal routing")
	for _, tc := range []struct {
		name    string
		tokenID int
		digest  string
	}{
		{name: "same token different session", tokenID: session.tokenID, digest: strings.Repeat("c", 64)},
		{name: "different token same fingerprint", tokenID: session.tokenID + 1, digest: session.digest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			independent := session
			independent.tokenID, independent.digest = tc.tokenID, tc.digest
			request := cyberFallbackTestContext(`{"input":"hello"}`)
			request.Set(cyberSessionRequestKey, independent)
			require.Nil(t, prepareCyberFallbackBans(request, independent, nil))
			require.Nil(t, CheckCyberSessionChannel(request, 21))
			assert.False(t, IsCyberFallback(request), "another session's ban cannot change this request's route")
		})
	}
	require.Nil(t, CheckCyberSessionChannel(c, 21))
	assert.True(t, IsCyberFallback(c))
	setChannelAffinityContext(c, channelAffinityMeta{RuleName: "original", ParamTemplate: map[string]any{"model": "original-only"}, SkipRetry: true})
	original := map[string]any{"model": "fallback-model"}
	merged, applied := ApplyChannelAffinityOverrideTemplate(c, original)
	assert.False(t, applied)
	assert.Equal(t, original, merged)
	assert.False(t, ShouldSkipRetryAfterChannelAffinityFailure(c), "original affinity must not suppress fallback retry policy")
	assert.False(t, ClearCurrentChannelAffinityCache(c))
	require.NoError(t, client.Set(t.Context(), session.redisKey(22), "1", time.Minute).Err())
	c = cyberFallbackTestContext(`{"input":"hello"}`)
	c.Set(cyberSessionRequestKey, session)
	require.Nil(t, prepareCyberFallbackBans(c, session, nil))
	apiErr := CheckCyberSessionChannel(c, 21)
	require.NotNil(t, apiErr)
	assert.Equal(t, "cyber_fallback_target_rejected", string(apiErr.GetErrorCode()), "a separately banned fallback cannot bypass its own active ban")
}

func TestWriteCyberSessionBanNotify(t *testing.T) {
	client := useChannelAffinityRedisForTest(t)
	prevNotify := cyberBanNotifyFn
	t.Cleanup(func() { cyberBanNotifyFn = prevNotify })

	type call struct {
		url, secret string
		data        relaydto.Notify
	}
	var (
		mu    sync.Mutex
		calls []call
		wg    sync.WaitGroup
	)
	cyberBanNotifyFn = func(webhookURL, secret string, data relaydto.Notify) error {
		mu.Lock()
		calls = append(calls, call{url: webhookURL, secret: secret, data: data})
		mu.Unlock()
		wg.Done()
		return nil
	}

	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(t.Name()+uuid.NewString())))
	session := cyberRequestSession{
		policy: CyberPolicy{
			Mode:                "enforce",
			Scope:               "all",
			TTLSeconds:          600,
			BlockedAction:       "block",
			NotifyWebhookURL:    "https://hooks.example.com/cyber",
			NotifyWebhookSecret: "s3cret",
		},
		tokenID: 91,
		digest:  digest,
	}
	key := session.redisKey(0)
	t.Cleanup(func() { client.Del(context.Background(), key) })

	c := cyberFallbackTestContext(`{"input":"hello"}`)
	c.Set(common.RequestIdKey, "req-armed-1")
	c.Set("channel_id", 77)
	wg.Add(1)
	writeCyberSession(c, session)
	require.Equal(t, "armed", cyberSessionLogAction(t, c))
	exists, err := client.Exists(t.Context(), key).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(1), exists)
	require.True(t, waitGroupDone(t, &wg, time.Second), "armed notify should fire once")

	mu.Lock()
	require.Len(t, calls, 1)
	assert.Equal(t, "https://hooks.example.com/cyber", calls[0].url)
	assert.Equal(t, "s3cret", calls[0].secret)
	assert.Equal(t, relaydto.NotifyTypeCyberSessionBan, calls[0].data.Type)
	assert.Contains(t, calls[0].data.Content, "token_id=91")
	assert.Contains(t, calls[0].data.Content, "channel_id=77")
	assert.Contains(t, calls[0].data.Content, "request_id=req-armed-1")
	assert.Contains(t, calls[0].data.Content, "source=upstream_cyber_policy")
	mu.Unlock()

	c2 := cyberFallbackTestContext(`{"input":"hello"}`)
	c2.Set(common.RequestIdKey, "req-armed-2")
	c2.Set("channel_id", 77)
	writeCyberSession(c2, session)
	require.Equal(t, "already_active", cyberSessionLogAction(t, c2))
	mu.Lock()
	assert.Len(t, calls, 1, "already_active must not notify again")
	mu.Unlock()

	emptySession := session
	emptySession.digest = fmt.Sprintf("%x", sha256.Sum256([]byte(t.Name()+"empty"+uuid.NewString())))
	emptySession.policy.NotifyWebhookURL = ""
	emptyKey := emptySession.redisKey(0)
	t.Cleanup(func() { client.Del(context.Background(), emptyKey) })
	c3 := cyberFallbackTestContext(`{"input":"hello"}`)
	c3.Set("channel_id", 77)
	writeCyberSession(c3, emptySession)
	require.Equal(t, "armed", cyberSessionLogAction(t, c3))
	mu.Lock()
	assert.Len(t, calls, 1, "empty webhook URL must not notify")
	mu.Unlock()

	localSession := session
	localSession.digest = fmt.Sprintf("%x", sha256.Sum256([]byte(t.Name()+"local"+uuid.NewString())))
	localKey := localSession.redisKey(0)
	t.Cleanup(func() { client.Del(context.Background(), localKey) })
	wg.Add(1)
	cLocal := cyberFallbackTestContext(`{"input":"hello"}`)
	cLocal.Set(common.RequestIdKey, "req-local")
	cLocal.Set("channel_id", 88)
	cLocal.Set(relaySensitiveMatchKey, RelaySensitiveMatch{Rule: "local_prompt_sensitive_words"})
	writeCyberSession(cLocal, localSession)
	require.Equal(t, "armed", cyberSessionLogAction(t, cLocal))
	require.True(t, waitGroupDone(t, &wg, time.Second))
	mu.Lock()
	require.Len(t, calls, 2)
	assert.Contains(t, calls[1].data.Content, "channel_id=88")
	assert.Contains(t, calls[1].data.Content, "source=local:local_prompt_sensitive_words")
	mu.Unlock()

	failSession := session
	failSession.digest = fmt.Sprintf("%x", sha256.Sum256([]byte(t.Name()+"fail"+uuid.NewString())))
	failKey := failSession.redisKey(0)
	t.Cleanup(func() { client.Del(context.Background(), failKey) })
	cyberBanNotifyFn = func(webhookURL, secret string, data relaydto.Notify) error {
		wg.Done()
		return fmt.Errorf("webhook down")
	}
	wg.Add(1)
	c4 := cyberFallbackTestContext(`{"input":"hello"}`)
	c4.Set("channel_id", 77)
	writeCyberSession(c4, failSession)
	require.Equal(t, "armed", cyberSessionLogAction(t, c4))
	require.True(t, waitGroupDone(t, &wg, time.Second))
	exists, err = client.Exists(t.Context(), failKey).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(1), exists, "notify failure must not undo the ban")
}

func TestCyberPolicyForAPIRedactsWebhookSecret(t *testing.T) {
	p := CyberPolicy{
		Mode:                "enforce",
		Scope:               "all",
		TTLSeconds:          600,
		NotifyWebhookURL:    "https://hooks.example.com/cyber",
		NotifyWebhookSecret: "s3cret",
	}
	view := p.ForAPI()
	assert.Equal(t, "s3cret", p.NotifyWebhookSecret)
	assert.Empty(t, view.NotifyWebhookSecret)
	assert.True(t, view.NotifyWebhookSecretConfigured)
	assert.Equal(t, "https://hooks.example.com/cyber", view.NotifyWebhookURL)

	empty := CyberPolicy{Mode: "off", Scope: "all", TTLSeconds: 600}.ForAPI()
	assert.False(t, empty.NotifyWebhookSecretConfigured)
	assert.Empty(t, empty.NotifyWebhookSecret)
}

func cyberSessionLogAction(t *testing.T, c *gin.Context) string {
	t.Helper()
	value, ok := c.Get(cyberSessionLogKey)
	require.True(t, ok)
	log, ok := value.(gin.H)
	require.True(t, ok)
	action, _ := log["action"].(string)
	return action
}

func waitGroupDone(t *testing.T, wg *sync.WaitGroup, timeout time.Duration) bool {
	t.Helper()
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}
