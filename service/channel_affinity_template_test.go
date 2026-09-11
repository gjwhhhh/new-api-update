package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

func buildChannelAffinityTemplateContextForTest(meta channelAffinityMeta) *gin.Context {
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	setChannelAffinityContext(ctx, meta)
	return ctx
}

func useChannelAffinityRedisForTest(t *testing.T) *redis.Client {
	t.Helper()
	server, err := miniredis.Run()
	require.NoError(t, err)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})

	oldRedisEnabled := common.RedisEnabled
	oldRedisClient := common.RDB
	channelAffinityCacheVersionLock.Lock()
	oldGlobalVersion := channelAffinityCacheVersionState.globalVersion
	oldRuleVersions := make(map[string]string, len(channelAffinityCacheVersionState.ruleVersions))
	for ruleName, cacheVersion := range channelAffinityCacheVersionState.ruleVersions {
		oldRuleVersions[ruleName] = cacheVersion
	}
	channelAffinityCacheVersionState.globalVersion = ""
	channelAffinityCacheVersionState.ruleVersions = make(map[string]string)
	channelAffinityCacheVersionLock.Unlock()

	common.RedisEnabled = true
	common.RDB = client
	t.Cleanup(func() {
		common.RedisEnabled = oldRedisEnabled
		common.RDB = oldRedisClient
		channelAffinityCacheVersionLock.Lock()
		channelAffinityCacheVersionState.globalVersion = oldGlobalVersion
		channelAffinityCacheVersionState.ruleVersions = oldRuleVersions
		channelAffinityCacheVersionLock.Unlock()
		_ = client.Close()
		server.Close()
	})
	return client
}

func TestApplyChannelAffinityOverrideTemplate_NoTemplate(t *testing.T) {
	ctx := buildChannelAffinityTemplateContextForTest(channelAffinityMeta{
		RuleName: "rule-no-template",
	})
	base := map[string]interface{}{
		"temperature": 0.7,
	}

	merged, applied := ApplyChannelAffinityOverrideTemplate(ctx, base)
	require.False(t, applied)
	require.Equal(t, base, merged)
}

func TestApplyChannelAffinityOverrideTemplate_MergeTemplate(t *testing.T) {
	ctx := buildChannelAffinityTemplateContextForTest(channelAffinityMeta{
		RuleName: "rule-with-template",
		ParamTemplate: map[string]interface{}{
			"temperature": 0.2,
			"top_p":       0.95,
		},
		UsingGroup:     "default",
		ModelName:      "gpt-4.1",
		RequestPath:    "/v1/responses",
		KeySourceType:  "gjson",
		KeySourcePath:  "prompt_cache_key",
		KeyHint:        "abcd...wxyz",
		KeyFingerprint: "abcd1234",
	})
	base := map[string]interface{}{
		"temperature": 0.7,
		"max_tokens":  2000,
	}

	merged, applied := ApplyChannelAffinityOverrideTemplate(ctx, base)
	require.True(t, applied)
	require.Equal(t, 0.7, merged["temperature"])
	require.Equal(t, 0.95, merged["top_p"])
	require.Equal(t, 2000, merged["max_tokens"])
	require.Equal(t, 0.7, base["temperature"])

	anyInfo, ok := ctx.Get(ginKeyChannelAffinityLogInfo)
	require.True(t, ok)
	info, ok := anyInfo.(map[string]interface{})
	require.True(t, ok)
	overrideInfoAny, ok := info["override_template"]
	require.True(t, ok)
	overrideInfo, ok := overrideInfoAny.(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, true, overrideInfo["applied"])
	require.Equal(t, "rule-with-template", overrideInfo["rule_name"])
	require.EqualValues(t, 2, overrideInfo["param_override_keys"])
}

func TestApplyChannelAffinityOverrideTemplate_MergeOperations(t *testing.T) {
	ctx := buildChannelAffinityTemplateContextForTest(channelAffinityMeta{
		RuleName: "rule-with-ops-template",
		ParamTemplate: map[string]interface{}{
			"operations": []map[string]interface{}{
				{
					"mode":  "pass_headers",
					"value": []string{"Originator"},
				},
			},
		},
	})
	base := map[string]interface{}{
		"temperature": 0.7,
		"operations": []map[string]interface{}{
			{
				"path":  "model",
				"mode":  "trim_prefix",
				"value": "openai/",
			},
		},
	}

	merged, applied := ApplyChannelAffinityOverrideTemplate(ctx, base)
	require.True(t, applied)
	require.Equal(t, 0.7, merged["temperature"])

	opsAny, ok := merged["operations"]
	require.True(t, ok)
	ops, ok := opsAny.([]interface{})
	require.True(t, ok)
	require.Len(t, ops, 2)

	firstOp, ok := ops[0].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, "pass_headers", firstOp["mode"])

	secondOp, ok := ops[1].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, "trim_prefix", secondOp["mode"])
}

func TestShouldSkipRetryAfterChannelAffinityFailure(t *testing.T) {
	tests := []struct {
		name string
		ctx  func() *gin.Context
		want bool
	}{
		{
			name: "nil context",
			ctx: func() *gin.Context {
				return nil
			},
			want: false,
		},
		{
			name: "explicit skip retry flag in context",
			ctx: func() *gin.Context {
				ctx := buildChannelAffinityTemplateContextForTest(channelAffinityMeta{
					RuleName:   "rule-explicit-flag",
					SkipRetry:  false,
					UsingGroup: "default",
					ModelName:  "gpt-5",
				})
				ctx.Set(ginKeyChannelAffinitySkipRetry, true)
				return ctx
			},
			want: true,
		},
		{
			name: "fallback to matched rule meta",
			ctx: func() *gin.Context {
				return buildChannelAffinityTemplateContextForTest(channelAffinityMeta{
					RuleName:   "rule-skip-retry",
					SkipRetry:  true,
					UsingGroup: "default",
					ModelName:  "gpt-5",
				})
			},
			want: true,
		},
		{
			name: "no flag and no skip retry meta",
			ctx: func() *gin.Context {
				return buildChannelAffinityTemplateContextForTest(channelAffinityMeta{
					RuleName:   "rule-no-skip-retry",
					SkipRetry:  false,
					UsingGroup: "default",
					ModelName:  "gpt-5",
				})
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, ShouldSkipRetryAfterChannelAffinityFailure(tt.ctx()))
		})
	}
}

func TestExtractChannelAffinityValue_RequestHeader(t *testing.T) {
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	ctx.Request.Header.Set("X-Affinity-Key", " tenant-123 ")

	value := extractChannelAffinityValue(ctx, operation_setting.ChannelAffinityKeySource{
		Type: "request_header",
		Key:  "X-Affinity-Key",
	})

	require.Equal(t, "tenant-123", value)
}

func TestGetPreferredChannelByAffinity_RequestHeaderKeySource(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rule := operation_setting.ChannelAffinityRule{
		Name:       "header-affinity",
		ModelRegex: []string{"^gpt-.*$"},
		PathRegex:  []string{"/v1/responses"},
		KeySources: []operation_setting.ChannelAffinityKeySource{
			{Type: "request_header", Key: "X-Affinity-Key"},
		},
		IncludeRuleName:  true,
		IncludeModelName: true,
	}

	affinityValue := fmt.Sprintf("header-hit-%d", time.Now().UnixNano())
	cacheVersion, err := getChannelAffinityCacheVersion(rule.Name)
	require.NoError(t, err)
	cacheKeySuffix := buildChannelAffinityCacheKeySuffixWithVersion(rule, "gpt-5", "default", affinityValue, cacheVersion)

	cache := getChannelAffinityCache()
	require.NoError(t, cache.SetWithTTL(cacheKeySuffix, 9528, time.Minute))
	t.Cleanup(func() {
		_, _ = cache.DeleteMany([]string{cacheKeySuffix})
	})

	setting := operation_setting.GetChannelAffinitySetting()
	originalRules := setting.Rules
	setting.Rules = append([]operation_setting.ChannelAffinityRule{rule}, originalRules...)
	t.Cleanup(func() {
		setting.Rules = originalRules
	})

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	ctx.Request.Header.Set("X-Affinity-Key", affinityValue)

	channelID, found := GetPreferredChannelByAffinity(ctx, "gpt-5", "default")
	require.True(t, found)
	require.Equal(t, 9528, channelID)

	meta, ok := getChannelAffinityMeta(ctx)
	require.True(t, ok)
	require.Equal(t, "request_header", meta.KeySourceType)
	require.Equal(t, "X-Affinity-Key", meta.KeySourceKey)
	require.Equal(t, buildChannelAffinityKeyHint(affinityValue), meta.KeyHint)
}

func TestClearCurrentChannelAffinityCache(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cacheKeySuffix := fmt.Sprintf("codex cli trace:default:clear-current-%d", time.Now().UnixNano())
	cacheKeyFull := channelAffinityCacheNamespace + ":" + cacheKeySuffix
	cache := getChannelAffinityCache()
	require.NoError(t, cache.SetWithTTL(cacheKeySuffix, 9527, time.Minute))
	t.Cleanup(func() {
		_, _ = cache.DeleteMany([]string{cacheKeySuffix})
	})

	ctx := buildChannelAffinityTemplateContextForTest(channelAffinityMeta{
		CacheKey:   cacheKeyFull,
		TTLSeconds: 60,
		RuleName:   "codex cli trace",
		SkipRetry:  true,
	})
	require.True(t, ShouldSkipRetryAfterChannelAffinityFailure(ctx))

	deleted := ClearCurrentChannelAffinityCache(ctx)
	require.True(t, deleted)
	_, found, err := cache.Get(cacheKeySuffix)
	require.NoError(t, err)
	require.False(t, found)
	require.False(t, ShouldSkipRetryAfterChannelAffinityFailure(ctx))
}

func TestChannelAffinityCacheVersionUsesLocalValueUntilInvalidationEvent(t *testing.T) {
	client := useChannelAffinityRedisForTest(t)
	ctx := context.Background()
	ruleName := "local-version-event"

	require.NoError(t, client.Set(ctx, channelAffinityCacheVersionStorageKey(""), "global-v1", 0).Err())
	require.NoError(t, client.Set(ctx, channelAffinityCacheVersionStorageKey(ruleName), "rule-v1", 0).Err())

	initial, err := getChannelAffinityCacheVersion(ruleName)
	require.NoError(t, err)
	require.Equal(t, "global-v1", initial.GlobalVersion)
	require.Equal(t, "rule-v1", initial.RuleVersion)

	require.NoError(t, client.Set(ctx, channelAffinityCacheVersionStorageKey(""), "global-v2", 0).Err())
	require.NoError(t, client.Set(ctx, channelAffinityCacheVersionStorageKey(ruleName), "rule-v2", 0).Err())

	cached, err := getChannelAffinityCacheVersion(ruleName)
	require.NoError(t, err)
	require.Equal(t, initial, cached)

	eventPayload, err := common.Marshal(channelAffinityCacheVersionEvent{
		Scope:        channelAffinityCacheVersionScopeRule,
		RuleName:     ruleName,
		CacheVersion: "rule-v2",
	})
	require.NoError(t, err)
	handleChannelAffinityCacheVersionEvent(string(eventPayload))

	updated, err := getChannelAffinityCacheVersion(ruleName)
	require.NoError(t, err)
	require.Equal(t, "global-v2", updated.GlobalVersion)
	require.Equal(t, "rule-v2", updated.RuleVersion)
}

func TestRotateChannelAffinityCacheVersionPublishesUpdate(t *testing.T) {
	client := useChannelAffinityRedisForTest(t)
	ctx := context.Background()
	require.NoError(t, client.Set(ctx, channelAffinityCacheVersionStorageKey(""), "global-v1", 0).Err())

	pubsub := client.Subscribe(ctx, channelAffinityCacheVersionEventChannel)
	t.Cleanup(func() { _ = pubsub.Close() })
	_, err := pubsub.ReceiveTimeout(ctx, time.Second)
	require.NoError(t, err)

	previousVersion, err := rotateChannelAffinityCacheVersion("")
	require.NoError(t, err)
	require.Equal(t, "global-v1", previousVersion)

	currentVersion, err := client.Get(ctx, channelAffinityCacheVersionStorageKey("")).Result()
	require.NoError(t, err)
	require.NotEqual(t, previousVersion, currentVersion)

	message, err := pubsub.ReceiveMessage(ctx)
	require.NoError(t, err)
	event := channelAffinityCacheVersionEvent{}
	require.NoError(t, common.UnmarshalJsonStr(message.Payload, &event))
	require.Equal(t, channelAffinityCacheVersionScopeGlobal, event.Scope)
	require.Empty(t, event.RuleName)
	require.Equal(t, currentVersion, event.CacheVersion)

	cached, complete := getCachedChannelAffinityCacheVersion("")
	require.True(t, complete)
	require.Equal(t, currentVersion, cached.GlobalVersion)
}

func TestClearChannelAffinityCacheAllPreventsLateRequestWrite(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rule := operation_setting.ChannelAffinityRule{
		Name:       "clear-all-late-write",
		ModelRegex: []string{"^gpt-.*$"},
		KeySources: []operation_setting.ChannelAffinityKeySource{
			{Type: "request_header", Key: "X-Affinity-Key"},
		},
		IncludeRuleName: true,
	}
	setting := operation_setting.GetChannelAffinitySetting()
	originalEnabled := setting.Enabled
	originalRules := setting.Rules
	setting.Enabled = true
	setting.Rules = []operation_setting.ChannelAffinityRule{rule}
	t.Cleanup(func() {
		setting.Enabled = originalEnabled
		setting.Rules = originalRules
	})

	affinityValue := fmt.Sprintf("clear-all-late-%d", time.Now().UnixNano())
	beforeClear := newChannelAffinityHeaderTestContext("X-Affinity-Key", affinityValue)
	_, found := GetPreferredChannelByAffinity(beforeClear, "gpt-5", "default")
	require.False(t, found)

	result, err := ClearChannelAffinityCacheAll()
	require.NoError(t, err)
	require.True(t, result.Invalidated)

	RecordChannelAffinity(beforeClear, 8811)
	afterClear := newChannelAffinityHeaderTestContext("X-Affinity-Key", affinityValue)
	_, found = GetPreferredChannelByAffinity(afterClear, "gpt-5", "default")
	require.False(t, found)

	RecordChannelAffinity(afterClear, 8812)
	current := newChannelAffinityHeaderTestContext("X-Affinity-Key", affinityValue)
	channelID, found := GetPreferredChannelByAffinity(current, "gpt-5", "default")
	require.True(t, found)
	require.Equal(t, 8812, channelID)

	ClearCurrentChannelAffinityCache(current)
}

func TestClearChannelAffinityCacheByRulePreventsLateRequestWrite(t *testing.T) {
	gin.SetMode(gin.TestMode)

	targetRule := operation_setting.ChannelAffinityRule{
		Name:       "clear-rule-late-write",
		ModelRegex: []string{"^gpt-.*$"},
		KeySources: []operation_setting.ChannelAffinityKeySource{
			{Type: "request_header", Key: "X-Target-Affinity-Key"},
		},
		IncludeRuleName: true,
	}
	otherRule := operation_setting.ChannelAffinityRule{
		Name:       "keep-other-rule",
		ModelRegex: []string{"^gpt-.*$"},
		KeySources: []operation_setting.ChannelAffinityKeySource{
			{Type: "request_header", Key: "X-Other-Affinity-Key"},
		},
		IncludeRuleName: true,
	}
	setting := operation_setting.GetChannelAffinitySetting()
	originalEnabled := setting.Enabled
	originalRules := setting.Rules
	setting.Enabled = true
	setting.Rules = []operation_setting.ChannelAffinityRule{targetRule, otherRule}
	t.Cleanup(func() {
		setting.Enabled = originalEnabled
		setting.Rules = originalRules
	})

	targetValue := fmt.Sprintf("clear-rule-target-%d", time.Now().UnixNano())
	otherValue := fmt.Sprintf("clear-rule-other-%d", time.Now().UnixNano())
	beforeClear := newChannelAffinityHeaderTestContext("X-Target-Affinity-Key", targetValue)
	_, found := GetPreferredChannelByAffinity(beforeClear, "gpt-5", "default")
	require.False(t, found)

	other := newChannelAffinityHeaderTestContext("X-Other-Affinity-Key", otherValue)
	_, found = GetPreferredChannelByAffinity(other, "gpt-5", "default")
	require.False(t, found)
	RecordChannelAffinity(other, 8821)

	result, err := ClearChannelAffinityCacheByRuleName(targetRule.Name)
	require.NoError(t, err)
	require.True(t, result.Invalidated)

	RecordChannelAffinity(beforeClear, 8822)
	afterClear := newChannelAffinityHeaderTestContext("X-Target-Affinity-Key", targetValue)
	_, found = GetPreferredChannelByAffinity(afterClear, "gpt-5", "default")
	require.False(t, found)

	otherAfterClear := newChannelAffinityHeaderTestContext("X-Other-Affinity-Key", otherValue)
	channelID, found := GetPreferredChannelByAffinity(otherAfterClear, "gpt-5", "default")
	require.True(t, found)
	require.Equal(t, 8821, channelID)

	ClearCurrentChannelAffinityCache(otherAfterClear)
}

func newChannelAffinityHeaderTestContext(headerName string, affinityValue string) *gin.Context {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	ctx.Request.Header.Set(headerName, affinityValue)
	return ctx
}

func TestChannelAffinityHitCodexTemplatePassHeadersEffective(t *testing.T) {
	gin.SetMode(gin.TestMode)

	setting := operation_setting.GetChannelAffinitySetting()
	require.NotNil(t, setting)

	var codexRule *operation_setting.ChannelAffinityRule
	for i := range setting.Rules {
		rule := &setting.Rules[i]
		if strings.EqualFold(strings.TrimSpace(rule.Name), "codex cli trace") {
			codexRule = rule
			break
		}
	}
	require.NotNil(t, codexRule)

	affinityValue := fmt.Sprintf("pc-hit-%d", time.Now().UnixNano())
	cacheVersion, err := getChannelAffinityCacheVersion(codexRule.Name)
	require.NoError(t, err)
	cacheKeySuffix := buildChannelAffinityCacheKeySuffixWithVersion(*codexRule, "gpt-5", "default", affinityValue, cacheVersion)

	cache := getChannelAffinityCache()
	require.NoError(t, cache.SetWithTTL(cacheKeySuffix, 9527, time.Minute))
	t.Cleanup(func() {
		_, _ = cache.DeleteMany([]string{cacheKeySuffix})
	})

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(fmt.Sprintf(`{"prompt_cache_key":"%s"}`, affinityValue)))
	ctx.Request.Header.Set("Content-Type", "application/json")

	channelID, found := GetPreferredChannelByAffinity(ctx, "gpt-5", "default")
	require.True(t, found)
	require.Equal(t, 9527, channelID)

	baseOverride := map[string]interface{}{
		"temperature": 0.2,
	}
	mergedOverride, applied := ApplyChannelAffinityOverrideTemplate(ctx, baseOverride)
	require.True(t, applied)
	require.Equal(t, 0.2, mergedOverride["temperature"])

	info := &relaycommon.RelayInfo{
		RequestHeaders: map[string]string{
			"Originator": "Codex CLI",
			"Session_id": "sess-123",
			"User-Agent": "codex-cli-test",
		},
		ChannelMeta: &relaycommon.ChannelMeta{
			ParamOverride: mergedOverride,
			HeadersOverride: map[string]interface{}{
				"X-Static": "legacy-static",
			},
		},
	}

	_, err = relaycommon.ApplyParamOverrideWithRelayInfo([]byte(`{"model":"gpt-5"}`), info)
	require.NoError(t, err)
	require.True(t, info.UseRuntimeHeadersOverride)

	require.Equal(t, "legacy-static", info.RuntimeHeadersOverride["x-static"])
	require.Equal(t, "Codex CLI", info.RuntimeHeadersOverride["originator"])
	require.Equal(t, "sess-123", info.RuntimeHeadersOverride["session_id"])
	require.Equal(t, "codex-cli-test", info.RuntimeHeadersOverride["user-agent"])

	_, exists := info.RuntimeHeadersOverride["x-codex-beta-features"]
	require.False(t, exists)
	_, exists = info.RuntimeHeadersOverride["x-codex-turn-metadata"]
	require.False(t, exists)
}
