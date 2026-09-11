package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/fnv"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/pkg/cachex"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/samber/hot"
	"github.com/tidwall/gjson"
)

const (
	ginKeyChannelAffinityCacheKey   = "channel_affinity_cache_key"
	ginKeyChannelAffinityTTLSeconds = "channel_affinity_ttl_seconds"
	ginKeyChannelAffinityMeta       = "channel_affinity_meta"
	ginKeyChannelAffinityLogInfo    = "channel_affinity_log_info"
	ginKeyChannelAffinitySkipRetry  = "channel_affinity_skip_retry_on_failure"

	channelAffinityCacheNamespace = "new-api:channel_affinity:v2"
	// Keep the existing Redis prefix so rolling deployments share one cache version.
	channelAffinityCacheVersionStorageNamespace = "new-api:channel_affinity:generation:v1"
	channelAffinityCacheVersionEventChannel     = "new-api:channel_affinity:cache_version_events:v1"
	channelAffinityUsageCacheStatsNamespace     = "new-api:channel_affinity_usage_cache_stats:v1"
	channelAffinityCacheCleanupBatchSize        = 1000
	channelAffinityCacheVersionSyncInterval     = time.Minute
	channelAffinityCacheVersionSyncChannelSize  = 128

	channelAffinityCacheVersionScopeGlobal = "global"
	channelAffinityCacheVersionScopeRule   = "rule"

	channelAffinityCacheVersionRotateScript = `
local previous = redis.call("GET", KEYS[1])
if not previous then
  previous = ""
end
redis.call("SET", KEYS[1], ARGV[1])
redis.call("PUBLISH", ARGV[2], ARGV[3])
return previous
`
)

var (
	channelAffinityCacheOnce sync.Once
	channelAffinityCache     *cachex.HybridCache[int]

	channelAffinityUsageCacheStatsOnce  sync.Once
	channelAffinityUsageCacheStatsCache *cachex.HybridCache[ChannelAffinityUsageCacheCounters]

	channelAffinityRegexCache sync.Map // map[string]*regexp.Regexp

	channelAffinityCacheVersionLock     sync.RWMutex
	channelAffinityCacheVersionSyncLock sync.Mutex
	channelAffinityCacheVersionSyncOnce sync.Once
	channelAffinityCacheVersionState    = struct {
		globalVersion string
		ruleVersions  map[string]string
	}{
		ruleVersions: make(map[string]string),
	}
)

type channelAffinityMeta struct {
	CacheKey       string
	TTLSeconds     int
	RuleName       string
	SkipRetry      bool
	ParamTemplate  map[string]interface{}
	KeySourceType  string
	KeySourceKey   string
	KeySourcePath  string
	KeyHint        string
	KeyFingerprint string
	UsingGroup     string
	ModelName      string
	RequestPath    string
}

// channelAffinityCacheVersion identifies the currently readable cache entries.
// Clearing affinity switches this version, so writes from earlier requests stay unreadable.
type channelAffinityCacheVersion struct {
	GlobalVersion string
	RuleVersion   string
}

type channelAffinityCacheVersionEvent struct {
	Scope        string `json:"scope"`
	RuleName     string `json:"rule_name,omitempty"`
	CacheVersion string `json:"cache_version"`
}

type ChannelAffinityCacheClearResult struct {
	Invalidated      bool   `json:"invalidated"`
	Scope            string `json:"scope"`
	CleanupScheduled bool   `json:"cleanup_scheduled"`
}

type ChannelAffinityStatsContext struct {
	RuleName       string
	UsingGroup     string
	KeyFingerprint string
	TTLSeconds     int64
}

const (
	cacheTokenRateModeCachedOverPrompt           = "cached_over_prompt"
	cacheTokenRateModeCachedOverPromptPlusCached = "cached_over_prompt_plus_cached"
	cacheTokenRateModeMixed                      = "mixed"
)

type ChannelAffinityCacheStats struct {
	Enabled       bool           `json:"enabled"`
	Total         int            `json:"total"`
	Unknown       int            `json:"unknown"`
	Stale         int            `json:"stale"`
	ByRuleName    map[string]int `json:"by_rule_name"`
	CacheCapacity int            `json:"cache_capacity"`
	CacheAlgo     string         `json:"cache_algo"`
	Scope         string         `json:"scope"`
}

func getChannelAffinityCache() *cachex.HybridCache[int] {
	channelAffinityCacheOnce.Do(func() {
		setting := operation_setting.GetChannelAffinitySetting()
		capacity := setting.MaxEntries
		if capacity <= 0 {
			capacity = 100_000
		}
		defaultTTLSeconds := setting.DefaultTTLSeconds
		if defaultTTLSeconds <= 0 {
			defaultTTLSeconds = 3600
		}

		channelAffinityCache = cachex.NewHybridCache[int](cachex.HybridCacheConfig[int]{
			Namespace: cachex.Namespace(channelAffinityCacheNamespace),
			Redis:     common.RDB,
			RedisEnabled: func() bool {
				return common.RedisEnabled && common.RDB != nil
			},
			RedisCodec: cachex.IntCodec{},
			Memory: func() *hot.HotCache[string, int] {
				return hot.NewHotCache[string, int](hot.LRU, capacity).
					WithTTL(time.Duration(defaultTTLSeconds) * time.Second).
					WithJanitor().
					Build()
			},
		})
	})
	return channelAffinityCache
}

func channelAffinityCacheScope() string {
	if common.RedisEnabled && common.RDB != nil {
		return "shared_redis"
	}
	return "local_memory"
}

func newChannelAffinityCacheVersion() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate channel affinity cache version: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}

func channelAffinityCacheVersionStorageKey(ruleName string) string {
	ruleName = strings.TrimSpace(ruleName)
	if ruleName == "" {
		return channelAffinityCacheVersionStorageNamespace + ":global"
	}
	digest := sha256.Sum256([]byte(ruleName))
	return channelAffinityCacheVersionStorageNamespace + ":rule:" + hex.EncodeToString(digest[:])
}

func getRedisChannelAffinityCacheVersion(key string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	cacheVersion, err := common.RDB.Get(ctx, key).Result()
	if err == nil {
		return cacheVersion, nil
	}
	if !errors.Is(err, redis.Nil) {
		return "", err
	}

	cacheVersion, err = newChannelAffinityCacheVersion()
	if err != nil {
		return "", err
	}
	created, err := common.RDB.SetNX(ctx, key, cacheVersion, 0).Result()
	if err != nil {
		return "", err
	}
	if created {
		return cacheVersion, nil
	}
	return common.RDB.Get(ctx, key).Result()
}

func getCachedChannelAffinityCacheVersion(ruleName string) (channelAffinityCacheVersion, bool) {
	channelAffinityCacheVersionLock.RLock()
	defer channelAffinityCacheVersionLock.RUnlock()

	cacheVersion := channelAffinityCacheVersion{
		GlobalVersion: channelAffinityCacheVersionState.globalVersion,
	}
	if cacheVersion.GlobalVersion == "" {
		return cacheVersion, false
	}
	if ruleName == "" {
		return cacheVersion, true
	}
	cacheVersion.RuleVersion = channelAffinityCacheVersionState.ruleVersions[ruleName]
	return cacheVersion, cacheVersion.RuleVersion != ""
}

func setCachedChannelAffinityCacheVersion(ruleName string, cacheVersion string) {
	if cacheVersion == "" {
		return
	}
	channelAffinityCacheVersionLock.Lock()
	defer channelAffinityCacheVersionLock.Unlock()
	if ruleName == "" {
		channelAffinityCacheVersionState.globalVersion = cacheVersion
		return
	}
	channelAffinityCacheVersionState.ruleVersions[ruleName] = cacheVersion
}

func cachedChannelAffinityRuleNames() []string {
	channelAffinityCacheVersionLock.RLock()
	defer channelAffinityCacheVersionLock.RUnlock()

	ruleNames := make([]string, 0, len(channelAffinityCacheVersionState.ruleVersions))
	for ruleName := range channelAffinityCacheVersionState.ruleVersions {
		ruleNames = append(ruleNames, ruleName)
	}
	return ruleNames
}

func getLocalChannelAffinityCacheVersion(ruleName string) (channelAffinityCacheVersion, error) {
	channelAffinityCacheVersionLock.Lock()
	defer channelAffinityCacheVersionLock.Unlock()

	if channelAffinityCacheVersionState.globalVersion == "" {
		cacheVersion, err := newChannelAffinityCacheVersion()
		if err != nil {
			return channelAffinityCacheVersion{}, err
		}
		channelAffinityCacheVersionState.globalVersion = cacheVersion
	}
	cacheVersion := channelAffinityCacheVersion{GlobalVersion: channelAffinityCacheVersionState.globalVersion}
	if ruleName == "" {
		return cacheVersion, nil
	}
	if channelAffinityCacheVersionState.ruleVersions[ruleName] == "" {
		ruleVersion, err := newChannelAffinityCacheVersion()
		if err != nil {
			return channelAffinityCacheVersion{}, err
		}
		channelAffinityCacheVersionState.ruleVersions[ruleName] = ruleVersion
	}
	cacheVersion.RuleVersion = channelAffinityCacheVersionState.ruleVersions[ruleName]
	return cacheVersion, nil
}

func loadChannelAffinityCacheVersionFromRedis(ruleName string) (channelAffinityCacheVersion, error) {
	channelAffinityCacheVersionSyncLock.Lock()
	defer channelAffinityCacheVersionSyncLock.Unlock()

	cacheVersion, complete := getCachedChannelAffinityCacheVersion(ruleName)
	if complete {
		return cacheVersion, nil
	}
	if cacheVersion.GlobalVersion == "" {
		globalVersion, err := getRedisChannelAffinityCacheVersion(channelAffinityCacheVersionStorageKey(""))
		if err != nil {
			return channelAffinityCacheVersion{}, err
		}
		setCachedChannelAffinityCacheVersion("", globalVersion)
		cacheVersion.GlobalVersion = globalVersion
	}
	if ruleName == "" {
		return cacheVersion, nil
	}
	ruleVersion, err := getRedisChannelAffinityCacheVersion(channelAffinityCacheVersionStorageKey(ruleName))
	if err != nil {
		return channelAffinityCacheVersion{}, err
	}
	setCachedChannelAffinityCacheVersion(ruleName, ruleVersion)
	cacheVersion.RuleVersion = ruleVersion
	return cacheVersion, nil
}

func refreshChannelAffinityCacheVersionsFromRedis(ruleNames []string) error {
	channelAffinityCacheVersionSyncLock.Lock()
	defer channelAffinityCacheVersionSyncLock.Unlock()

	globalVersion, err := getRedisChannelAffinityCacheVersion(channelAffinityCacheVersionStorageKey(""))
	if err != nil {
		return err
	}
	setCachedChannelAffinityCacheVersion("", globalVersion)

	seenRuleNames := make(map[string]struct{}, len(ruleNames))
	for _, ruleName := range ruleNames {
		ruleName = strings.TrimSpace(ruleName)
		if ruleName == "" {
			continue
		}
		if _, ok := seenRuleNames[ruleName]; ok {
			continue
		}
		seenRuleNames[ruleName] = struct{}{}
		ruleVersion, err := getRedisChannelAffinityCacheVersion(channelAffinityCacheVersionStorageKey(ruleName))
		if err != nil {
			return err
		}
		setCachedChannelAffinityCacheVersion(ruleName, ruleVersion)
	}
	return nil
}

func reconcileChannelAffinityCacheVersions() error {
	return refreshChannelAffinityCacheVersionsFromRedis(cachedChannelAffinityRuleNames())
}

func handleChannelAffinityCacheVersionEvent(payload string) {
	event := channelAffinityCacheVersionEvent{}
	if err := common.UnmarshalJsonStr(payload, &event); err != nil {
		common.SysError(fmt.Sprintf("decode channel affinity cache version event failed: err=%v", err))
		return
	}
	if strings.TrimSpace(event.CacheVersion) == "" {
		common.SysError("ignore channel affinity cache version event without cache_version")
		return
	}

	switch event.Scope {
	case channelAffinityCacheVersionScopeGlobal:
		if err := refreshChannelAffinityCacheVersionsFromRedis(nil); err != nil {
			common.SysError(fmt.Sprintf("refresh global channel affinity cache version failed: err=%v", err))
		}
	case channelAffinityCacheVersionScopeRule:
		ruleName := strings.TrimSpace(event.RuleName)
		if ruleName == "" {
			common.SysError("ignore channel affinity rule cache version event without rule_name")
			return
		}
		if err := refreshChannelAffinityCacheVersionsFromRedis([]string{ruleName}); err != nil {
			common.SysError(fmt.Sprintf("refresh channel affinity rule cache version failed: rule=%q, err=%v", ruleName, err))
		}
	default:
		common.SysError(fmt.Sprintf("ignore channel affinity cache version event with unknown scope=%q", event.Scope))
	}
}

func StartChannelAffinityCacheVersionSync() {
	if channelAffinityCacheScope() != "shared_redis" {
		return
	}
	channelAffinityCacheVersionSyncOnce.Do(func() {
		go runChannelAffinityCacheVersionSync()
	})
}

func runChannelAffinityCacheVersionSync() {
	client := common.RDB
	if client == nil {
		return
	}

	pubsub := client.Subscribe(context.Background(), channelAffinityCacheVersionEventChannel)
	defer func() {
		if err := pubsub.Close(); err != nil {
			common.SysError(fmt.Sprintf("close channel affinity cache version subscriber failed: err=%v", err))
		}
	}()

	events := pubsub.ChannelWithSubscriptions(context.Background(), channelAffinityCacheVersionSyncChannelSize)
	ticker := time.NewTicker(channelAffinityCacheVersionSyncInterval)
	defer ticker.Stop()

	for {
		select {
		case item, ok := <-events:
			if !ok {
				common.SysError("channel affinity cache version subscriber stopped")
				return
			}
			switch event := item.(type) {
			case *redis.Subscription:
				if event.Kind != "subscribe" || event.Channel != channelAffinityCacheVersionEventChannel {
					continue
				}
				if err := reconcileChannelAffinityCacheVersions(); err != nil {
					common.SysError(fmt.Sprintf("reconcile channel affinity cache versions after subscribe failed: err=%v", err))
				}
			case *redis.Message:
				handleChannelAffinityCacheVersionEvent(event.Payload)
			}
		case <-ticker.C:
			if err := reconcileChannelAffinityCacheVersions(); err != nil {
				common.SysError(fmt.Sprintf("periodic channel affinity cache version reconcile failed: err=%v", err))
			}
		}
	}
}

func getChannelAffinityCacheVersion(ruleName string) (channelAffinityCacheVersion, error) {
	ruleName = strings.TrimSpace(ruleName)
	if channelAffinityCacheScope() != "shared_redis" {
		return getLocalChannelAffinityCacheVersion(ruleName)
	}

	// The request path reads only this local snapshot after its initial Redis load.
	if cacheVersion, complete := getCachedChannelAffinityCacheVersion(ruleName); complete {
		return cacheVersion, nil
	}
	return loadChannelAffinityCacheVersionFromRedis(ruleName)
}

func rotateChannelAffinityCacheVersion(ruleName string) (string, error) {
	ruleName = strings.TrimSpace(ruleName)
	cacheVersion, err := newChannelAffinityCacheVersion()
	if err != nil {
		return "", err
	}

	if channelAffinityCacheScope() != "shared_redis" {
		channelAffinityCacheVersionLock.Lock()
		defer channelAffinityCacheVersionLock.Unlock()
		if ruleName == "" {
			previousCacheVersion := channelAffinityCacheVersionState.globalVersion
			if previousCacheVersion == "" {
				previousCacheVersion, err = newChannelAffinityCacheVersion()
				if err != nil {
					return "", err
				}
			}
			channelAffinityCacheVersionState.globalVersion = cacheVersion
			return previousCacheVersion, nil
		}
		previousCacheVersion := channelAffinityCacheVersionState.ruleVersions[ruleName]
		if previousCacheVersion == "" {
			previousCacheVersion, err = newChannelAffinityCacheVersion()
			if err != nil {
				return "", err
			}
		}
		channelAffinityCacheVersionState.ruleVersions[ruleName] = cacheVersion
		return previousCacheVersion, nil
	}

	eventScope := channelAffinityCacheVersionScopeGlobal
	if ruleName != "" {
		eventScope = channelAffinityCacheVersionScopeRule
	}
	eventPayload, err := common.Marshal(channelAffinityCacheVersionEvent{
		Scope:        eventScope,
		RuleName:     ruleName,
		CacheVersion: cacheVersion,
	})
	if err != nil {
		return "", fmt.Errorf("marshal channel affinity cache version event: %w", err)
	}

	channelAffinityCacheVersionSyncLock.Lock()
	defer channelAffinityCacheVersionSyncLock.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	result, err := common.RDB.Eval(
		ctx,
		channelAffinityCacheVersionRotateScript,
		[]string{channelAffinityCacheVersionStorageKey(ruleName)},
		cacheVersion,
		channelAffinityCacheVersionEventChannel,
		string(eventPayload),
	).Result()
	if err != nil {
		return "", err
	}
	previousCacheVersion, ok := result.(string)
	if !ok {
		return "", fmt.Errorf("unexpected channel affinity cache version result type %T", result)
	}
	setCachedChannelAffinityCacheVersion(ruleName, cacheVersion)
	return previousCacheVersion, nil
}

func buildChannelAffinityCacheKeySuffixWithVersion(rule operation_setting.ChannelAffinityRule, modelName string, usingGroup string, affinityValue string, cacheVersion channelAffinityCacheVersion) string {
	legacySuffix := buildChannelAffinityCacheKeySuffix(rule, modelName, usingGroup, affinityValue)
	ruleVersion := cacheVersion.RuleVersion
	if ruleVersion == "" {
		ruleVersion = "-"
	}
	return "g/" + cacheVersion.GlobalVersion + "/r/" + ruleVersion + "/" + legacySuffix
}

func parseChannelAffinityCacheKeySuffix(key string) (channelAffinityCacheVersion, string, bool) {
	prefix := channelAffinityCacheNamespace + ":g/"
	if !strings.HasPrefix(key, prefix) {
		return channelAffinityCacheVersion{}, "", false
	}
	rest := strings.TrimPrefix(key, prefix)
	parts := strings.SplitN(rest, "/", 4)
	if len(parts) != 4 || parts[1] != "r" || parts[0] == "" || parts[2] == "" || parts[3] == "" {
		return channelAffinityCacheVersion{}, "", false
	}
	ruleVersion := parts[2]
	if ruleVersion == "-" {
		ruleVersion = ""
	}
	return channelAffinityCacheVersion{GlobalVersion: parts[0], RuleVersion: ruleVersion}, parts[3], true
}

func scheduleChannelAffinityCacheCleanup(match func(channelAffinityCacheVersion) bool) {
	cache := getChannelAffinityCache()
	gopool.Go(func() {
		keys, err := cache.Keys()
		if err != nil {
			common.SysError(fmt.Sprintf("channel affinity cache cleanup list keys failed: err=%v", err))
			return
		}

		staleKeys := make([]string, 0)
		for _, key := range keys {
			cacheVersion, _, ok := parseChannelAffinityCacheKeySuffix(key)
			if ok && match(cacheVersion) {
				staleKeys = append(staleKeys, key)
			}
		}
		for start := 0; start < len(staleKeys); start += channelAffinityCacheCleanupBatchSize {
			end := start + channelAffinityCacheCleanupBatchSize
			if end > len(staleKeys) {
				end = len(staleKeys)
			}
			if _, err := cache.DeleteMany(staleKeys[start:end]); err != nil {
				common.SysError(fmt.Sprintf("channel affinity cache cleanup delete keys failed: err=%v", err))
				return
			}
		}
		if len(staleKeys) > 0 {
			common.SysLog(fmt.Sprintf("channel affinity cache cleanup removed %d stale entries", len(staleKeys)))
		}
	})
}

func GetChannelAffinityCacheStats() ChannelAffinityCacheStats {
	setting := operation_setting.GetChannelAffinitySetting()
	if setting == nil {
		return ChannelAffinityCacheStats{
			Enabled:    false,
			Total:      0,
			Unknown:    0,
			ByRuleName: map[string]int{},
			Scope:      channelAffinityCacheScope(),
		}
	}

	cache := getChannelAffinityCache()
	mainCap, _ := cache.Capacity()
	mainAlgo, _ := cache.Algorithm()

	rules := setting.Rules
	ruleByName := make(map[string]operation_setting.ChannelAffinityRule, len(rules))
	for _, r := range rules {
		name := strings.TrimSpace(r.Name)
		if name == "" {
			continue
		}
		if !r.IncludeRuleName {
			continue
		}
		ruleByName[name] = r
	}

	byRuleName := make(map[string]int, len(ruleByName))
	for name := range ruleByName {
		byRuleName[name] = 0
	}
	globalCacheVersion, err := getChannelAffinityCacheVersion("")
	if err != nil {
		common.SysError(fmt.Sprintf("channel affinity cache read version failed: err=%v", err))
		return ChannelAffinityCacheStats{
			Enabled:       setting.Enabled,
			ByRuleName:    byRuleName,
			CacheCapacity: mainCap,
			CacheAlgo:     mainAlgo,
			Scope:         channelAffinityCacheScope(),
		}
	}
	ruleCacheVersions := make(map[string]channelAffinityCacheVersion, len(ruleByName))
	for ruleName := range ruleByName {
		cacheVersion, err := getChannelAffinityCacheVersion(ruleName)
		if err != nil {
			common.SysError(fmt.Sprintf("channel affinity cache read rule version failed: rule=%q, err=%v", ruleName, err))
			continue
		}
		ruleCacheVersions[ruleName] = cacheVersion
	}

	keys, err := cache.Keys()
	if err != nil {
		common.SysError(fmt.Sprintf("channel affinity cache list keys failed: err=%v", err))
		keys = nil
	}
	total := 0
	unknown := 0
	stale := 0
	for _, k := range keys {
		cacheVersion, legacyKey, ok := parseChannelAffinityCacheKeySuffix(k)
		if !ok || cacheVersion.GlobalVersion != globalCacheVersion.GlobalVersion {
			stale++
			continue
		}
		total++
		parts := strings.Split(legacyKey, ":")
		if len(parts) == 0 {
			unknown++
			continue
		}
		ruleName := strings.TrimSpace(parts[0])
		rule, ok := ruleByName[ruleName]
		if !ok {
			unknown++
			continue
		}
		ruleCacheVersion, ok := ruleCacheVersions[ruleName]
		if !ok {
			unknown++
			continue
		}
		if cacheVersion.RuleVersion != ruleCacheVersion.RuleVersion {
			total--
			stale++
			continue
		}
		if rule.IncludeModelName {
			if len(parts) < 3 {
				unknown++
				continue
			}
		}
		if rule.IncludeUsingGroup {
			minParts := 3
			if rule.IncludeModelName {
				minParts = 4
			}
			if len(parts) < minParts {
				unknown++
				continue
			}
		}
		byRuleName[ruleName]++
	}

	return ChannelAffinityCacheStats{
		Enabled:       setting.Enabled,
		Total:         total,
		Unknown:       unknown,
		Stale:         stale,
		ByRuleName:    byRuleName,
		CacheCapacity: mainCap,
		CacheAlgo:     mainAlgo,
		Scope:         channelAffinityCacheScope(),
	}
}

func ClearChannelAffinityCacheAll() (ChannelAffinityCacheClearResult, error) {
	previousGlobalCacheVersion, err := rotateChannelAffinityCacheVersion("")
	if err != nil {
		return ChannelAffinityCacheClearResult{}, fmt.Errorf("rotate channel affinity cache version: %w", err)
	}
	scheduleChannelAffinityCacheCleanup(func(cacheVersion channelAffinityCacheVersion) bool {
		return cacheVersion.GlobalVersion == previousGlobalCacheVersion
	})
	return ChannelAffinityCacheClearResult{
		Invalidated:      true,
		Scope:            channelAffinityCacheScope(),
		CleanupScheduled: true,
	}, nil
}

func ClearChannelAffinityCacheByRuleName(ruleName string) (ChannelAffinityCacheClearResult, error) {
	ruleName = strings.TrimSpace(ruleName)
	if ruleName == "" {
		return ChannelAffinityCacheClearResult{}, fmt.Errorf("rule_name 不能为空")
	}

	setting := operation_setting.GetChannelAffinitySetting()
	if setting == nil {
		return ChannelAffinityCacheClearResult{}, fmt.Errorf("channel_affinity_setting 未初始化")
	}

	var matchedRule *operation_setting.ChannelAffinityRule
	for i := range setting.Rules {
		r := &setting.Rules[i]
		if strings.TrimSpace(r.Name) != ruleName {
			continue
		}
		matchedRule = r
		break
	}
	if matchedRule == nil {
		return ChannelAffinityCacheClearResult{}, fmt.Errorf("未知规则名称")
	}
	if !matchedRule.IncludeRuleName {
		return ChannelAffinityCacheClearResult{}, fmt.Errorf("该规则未启用 include_rule_name，无法按规则清空缓存")
	}

	previousRuleCacheVersion, err := rotateChannelAffinityCacheVersion(ruleName)
	if err != nil {
		return ChannelAffinityCacheClearResult{}, fmt.Errorf("rotate channel affinity rule cache version: %w", err)
	}
	scheduleChannelAffinityCacheCleanup(func(cacheVersion channelAffinityCacheVersion) bool {
		return cacheVersion.RuleVersion == previousRuleCacheVersion
	})
	return ChannelAffinityCacheClearResult{
		Invalidated:      true,
		Scope:            channelAffinityCacheScope(),
		CleanupScheduled: true,
	}, nil
}

func matchAnyRegexCached(patterns []string, s string) bool {
	if len(patterns) == 0 || s == "" {
		return false
	}
	for _, pattern := range patterns {
		if pattern == "" {
			continue
		}
		re, ok := channelAffinityRegexCache.Load(pattern)
		if !ok {
			compiled, err := regexp.Compile(pattern)
			if err != nil {
				continue
			}
			re = compiled
			channelAffinityRegexCache.Store(pattern, re)
		}
		if re.(*regexp.Regexp).MatchString(s) {
			return true
		}
	}
	return false
}

func matchAnyIncludeFold(patterns []string, s string) bool {
	if len(patterns) == 0 || s == "" {
		return false
	}
	sLower := strings.ToLower(s)
	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if strings.Contains(sLower, strings.ToLower(p)) {
			return true
		}
	}
	return false
}

func extractChannelAffinityValue(c *gin.Context, src operation_setting.ChannelAffinityKeySource) string {
	switch src.Type {
	case "context_int":
		if src.Key == "" {
			return ""
		}
		v := c.GetInt(src.Key)
		if v <= 0 {
			return ""
		}
		return strconv.Itoa(v)
	case "context_string":
		if src.Key == "" {
			return ""
		}
		return strings.TrimSpace(c.GetString(src.Key))
	case "request_header":
		if c == nil || c.Request == nil || src.Key == "" {
			return ""
		}
		return strings.TrimSpace(c.Request.Header.Get(src.Key))
	case "gjson":
		if src.Path == "" {
			return ""
		}
		storage, err := common.GetBodyStorage(c)
		if err != nil {
			return ""
		}
		body, err := storage.Bytes()
		if err != nil || len(body) == 0 {
			return ""
		}
		res := gjson.GetBytes(body, src.Path)
		if !res.Exists() {
			return ""
		}
		switch res.Type {
		case gjson.String, gjson.Number, gjson.True, gjson.False:
			return strings.TrimSpace(res.String())
		default:
			return strings.TrimSpace(res.Raw)
		}
	default:
		return ""
	}
}

func buildChannelAffinityCacheKeySuffix(rule operation_setting.ChannelAffinityRule, modelName string, usingGroup string, affinityValue string) string {
	parts := make([]string, 0, 4)
	if rule.IncludeRuleName && rule.Name != "" {
		parts = append(parts, rule.Name)
	}
	if rule.IncludeModelName && modelName != "" {
		parts = append(parts, modelName)
	}
	if rule.IncludeUsingGroup && usingGroup != "" {
		parts = append(parts, usingGroup)
	}
	parts = append(parts, affinityValue)
	return strings.Join(parts, ":")
}

func setChannelAffinityContext(c *gin.Context, meta channelAffinityMeta) {
	c.Set(ginKeyChannelAffinityCacheKey, meta.CacheKey)
	c.Set(ginKeyChannelAffinityTTLSeconds, meta.TTLSeconds)
	c.Set(ginKeyChannelAffinityMeta, meta)
}

func getChannelAffinityContext(c *gin.Context) (string, int, bool) {
	keyAny, ok := c.Get(ginKeyChannelAffinityCacheKey)
	if !ok {
		return "", 0, false
	}
	key, ok := keyAny.(string)
	if !ok || key == "" {
		return "", 0, false
	}
	ttlAny, ok := c.Get(ginKeyChannelAffinityTTLSeconds)
	if !ok {
		return key, 0, true
	}
	ttlSeconds, _ := ttlAny.(int)
	return key, ttlSeconds, true
}

func getChannelAffinityMeta(c *gin.Context) (channelAffinityMeta, bool) {
	anyMeta, ok := c.Get(ginKeyChannelAffinityMeta)
	if !ok {
		return channelAffinityMeta{}, false
	}
	meta, ok := anyMeta.(channelAffinityMeta)
	if !ok {
		return channelAffinityMeta{}, false
	}
	return meta, true
}

func GetChannelAffinityStatsContext(c *gin.Context) (ChannelAffinityStatsContext, bool) {
	if c == nil {
		return ChannelAffinityStatsContext{}, false
	}
	meta, ok := getChannelAffinityMeta(c)
	if !ok {
		return ChannelAffinityStatsContext{}, false
	}
	ruleName := strings.TrimSpace(meta.RuleName)
	keyFp := strings.TrimSpace(meta.KeyFingerprint)
	usingGroup := strings.TrimSpace(meta.UsingGroup)
	if ruleName == "" || keyFp == "" {
		return ChannelAffinityStatsContext{}, false
	}
	ttlSeconds := int64(meta.TTLSeconds)
	if ttlSeconds <= 0 {
		return ChannelAffinityStatsContext{}, false
	}
	return ChannelAffinityStatsContext{
		RuleName:       ruleName,
		UsingGroup:     usingGroup,
		KeyFingerprint: keyFp,
		TTLSeconds:     ttlSeconds,
	}, true
}

func affinityFingerprint(s string) string {
	if s == "" {
		return ""
	}
	hex := common.Sha1([]byte(s))
	if len(hex) >= 8 {
		return hex[:8]
	}
	return hex
}

func buildChannelAffinityKeyHint(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	if len(s) <= 12 {
		return s
	}
	return s[:4] + "..." + s[len(s)-4:]
}

func cloneStringAnyMap(src map[string]interface{}) map[string]interface{} {
	if len(src) == 0 {
		return map[string]interface{}{}
	}
	dst := make(map[string]interface{}, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func mergeChannelOverride(base map[string]interface{}, tpl map[string]interface{}) map[string]interface{} {
	if len(base) == 0 && len(tpl) == 0 {
		return map[string]interface{}{}
	}
	if len(tpl) == 0 {
		return base
	}
	out := cloneStringAnyMap(base)
	for k, v := range tpl {
		if strings.EqualFold(strings.TrimSpace(k), "operations") {
			baseOps, hasBaseOps := extractParamOperations(out[k])
			tplOps, hasTplOps := extractParamOperations(v)
			if hasTplOps {
				if hasBaseOps {
					out[k] = append(tplOps, baseOps...)
				} else {
					out[k] = tplOps
				}
				continue
			}
		}
		if _, exists := out[k]; exists {
			continue
		}
		out[k] = v
	}
	return out
}

func extractParamOperations(value interface{}) ([]interface{}, bool) {
	switch ops := value.(type) {
	case []interface{}:
		if len(ops) == 0 {
			return []interface{}{}, true
		}
		cloned := make([]interface{}, 0, len(ops))
		cloned = append(cloned, ops...)
		return cloned, true
	case []map[string]interface{}:
		cloned := make([]interface{}, 0, len(ops))
		for _, op := range ops {
			cloned = append(cloned, op)
		}
		return cloned, true
	default:
		return nil, false
	}
}

func appendChannelAffinityTemplateAdminInfo(c *gin.Context, meta channelAffinityMeta) {
	if c == nil {
		return
	}
	if len(meta.ParamTemplate) == 0 {
		return
	}

	templateInfo := map[string]interface{}{
		"applied":             true,
		"rule_name":           meta.RuleName,
		"param_override_keys": len(meta.ParamTemplate),
	}
	if anyInfo, ok := c.Get(ginKeyChannelAffinityLogInfo); ok {
		if info, ok := anyInfo.(map[string]interface{}); ok {
			info["override_template"] = templateInfo
			c.Set(ginKeyChannelAffinityLogInfo, info)
			return
		}
	}
	c.Set(ginKeyChannelAffinityLogInfo, map[string]interface{}{
		"reason":            meta.RuleName,
		"rule_name":         meta.RuleName,
		"using_group":       meta.UsingGroup,
		"model":             meta.ModelName,
		"request_path":      meta.RequestPath,
		"key_source":        meta.KeySourceType,
		"key_key":           meta.KeySourceKey,
		"key_path":          meta.KeySourcePath,
		"key_hint":          meta.KeyHint,
		"key_fp":            meta.KeyFingerprint,
		"override_template": templateInfo,
	})
}

// ApplyChannelAffinityOverrideTemplate merges per-rule channel override templates onto the selected channel override config.
func ApplyChannelAffinityOverrideTemplate(c *gin.Context, paramOverride map[string]interface{}) (map[string]interface{}, bool) {
	if c == nil {
		return paramOverride, false
	}
	meta, ok := getChannelAffinityMeta(c)
	if !ok {
		return paramOverride, false
	}
	if len(meta.ParamTemplate) == 0 {
		return paramOverride, false
	}

	mergedParam := mergeChannelOverride(paramOverride, meta.ParamTemplate)
	appendChannelAffinityTemplateAdminInfo(c, meta)
	return mergedParam, true
}

func GetPreferredChannelByAffinity(c *gin.Context, modelName string, usingGroup string) (int, bool) {
	setting := operation_setting.GetChannelAffinitySetting()
	if setting == nil || !setting.Enabled {
		return 0, false
	}
	path := ""
	if c != nil && c.Request != nil && c.Request.URL != nil {
		path = c.Request.URL.Path
	}
	userAgent := ""
	if c != nil && c.Request != nil {
		userAgent = c.Request.UserAgent()
	}

	for _, rule := range setting.Rules {
		if !matchAnyRegexCached(rule.ModelRegex, modelName) {
			continue
		}
		if len(rule.PathRegex) > 0 && !matchAnyRegexCached(rule.PathRegex, path) {
			continue
		}
		if len(rule.UserAgentInclude) > 0 && !matchAnyIncludeFold(rule.UserAgentInclude, userAgent) {
			continue
		}
		var affinityValue string
		var usedSource operation_setting.ChannelAffinityKeySource
		for _, src := range rule.KeySources {
			affinityValue = extractChannelAffinityValue(c, src)
			if affinityValue != "" {
				usedSource = src
				break
			}
		}
		if affinityValue == "" {
			continue
		}
		if rule.ValueRegex != "" && !matchAnyRegexCached([]string{rule.ValueRegex}, affinityValue) {
			continue
		}

		ttlSeconds := rule.TTLSeconds
		if ttlSeconds <= 0 {
			ttlSeconds = setting.DefaultTTLSeconds
		}
		versionRuleName := ""
		if rule.IncludeRuleName {
			versionRuleName = rule.Name
		}
		cacheVersion, err := getChannelAffinityCacheVersion(versionRuleName)
		if err != nil {
			common.SysError(fmt.Sprintf("channel affinity cache version read failed: rule=%q, err=%v", rule.Name, err))
			return 0, false
		}
		cacheKeySuffix := buildChannelAffinityCacheKeySuffixWithVersion(rule, modelName, usingGroup, affinityValue, cacheVersion)
		cacheKeyFull := channelAffinityCacheNamespace + ":" + cacheKeySuffix
		meta := channelAffinityMeta{
			CacheKey:       cacheKeyFull,
			TTLSeconds:     ttlSeconds,
			RuleName:       rule.Name,
			SkipRetry:      rule.SkipRetryOnFailure,
			ParamTemplate:  cloneStringAnyMap(rule.ParamOverrideTemplate),
			KeySourceType:  strings.TrimSpace(usedSource.Type),
			KeySourceKey:   strings.TrimSpace(usedSource.Key),
			KeySourcePath:  strings.TrimSpace(usedSource.Path),
			KeyHint:        buildChannelAffinityKeyHint(affinityValue),
			KeyFingerprint: affinityFingerprint(affinityValue),
			UsingGroup:     usingGroup,
			ModelName:      modelName,
			RequestPath:    path,
		}

		cache := getChannelAffinityCache()
		channelID, found, err := cache.Get(cacheKeySuffix)
		if err != nil {
			common.SysError(fmt.Sprintf("channel affinity cache get failed: rule=%q, err=%v", rule.Name, err))
			return 0, false
		}
		setChannelAffinityContext(c, meta)
		if found {
			return channelID, true
		}
		return 0, false
	}
	return 0, false
}

func ShouldSkipRetryAfterChannelAffinityFailure(c *gin.Context) bool {
	if c == nil {
		return false
	}
	v, ok := c.Get(ginKeyChannelAffinitySkipRetry)
	if ok {
		b, ok := v.(bool)
		if ok {
			return b
		}
	}
	meta, ok := getChannelAffinityMeta(c)
	if !ok {
		return false
	}
	return meta.SkipRetry
}

func ClearCurrentChannelAffinityCache(c *gin.Context) bool {
	if c == nil {
		return false
	}
	cacheKey, _, ok := getChannelAffinityContext(c)
	if !ok || cacheKey == "" {
		return false
	}

	cache := getChannelAffinityCache()
	deleted, err := cache.DeleteMany([]string{cacheKey})
	if err != nil {
		common.SysError(fmt.Sprintf("channel affinity cache delete current failed: err=%v", err))
		return false
	}
	c.Set(ginKeyChannelAffinitySkipRetry, false)
	for _, ok := range deleted {
		if ok {
			return true
		}
	}
	return false
}

func ShouldKeepChannelAffinityOnChannelDisabled() bool {
	setting := operation_setting.GetChannelAffinitySetting()
	if setting == nil {
		return false
	}
	return setting.KeepOnChannelDisabled
}

func MarkChannelAffinityUsed(c *gin.Context, selectedGroup string, channelID int) {
	if c == nil || channelID <= 0 {
		return
	}
	meta, ok := getChannelAffinityMeta(c)
	if !ok {
		return
	}
	c.Set(ginKeyChannelAffinitySkipRetry, meta.SkipRetry)
	info := map[string]interface{}{
		"reason":         meta.RuleName,
		"rule_name":      meta.RuleName,
		"using_group":    meta.UsingGroup,
		"selected_group": selectedGroup,
		"model":          meta.ModelName,
		"request_path":   meta.RequestPath,
		"channel_id":     channelID,
		"key_source":     meta.KeySourceType,
		"key_key":        meta.KeySourceKey,
		"key_path":       meta.KeySourcePath,
		"key_hint":       meta.KeyHint,
		"key_fp":         meta.KeyFingerprint,
	}
	c.Set(ginKeyChannelAffinityLogInfo, info)
}

func AppendChannelAffinityAdminInfo(c *gin.Context, adminInfo map[string]interface{}) {
	if c == nil || adminInfo == nil {
		return
	}
	anyInfo, ok := c.Get(ginKeyChannelAffinityLogInfo)
	if !ok || anyInfo == nil {
		return
	}
	adminInfo["channel_affinity"] = anyInfo
}

func RecordChannelAffinity(c *gin.Context, channelID int) {
	if channelID <= 0 {
		return
	}
	setting := operation_setting.GetChannelAffinitySetting()
	if setting == nil || !setting.Enabled {
		return
	}
	if setting.SwitchOnSuccess && c != nil {
		if successChannelID := c.GetInt("channel_id"); successChannelID > 0 {
			channelID = successChannelID
		}
	}
	cacheKey, ttlSeconds, ok := getChannelAffinityContext(c)
	if !ok {
		return
	}
	if ttlSeconds <= 0 {
		ttlSeconds = setting.DefaultTTLSeconds
	}
	if ttlSeconds <= 0 {
		ttlSeconds = 3600
	}
	cache := getChannelAffinityCache()
	if err := cache.SetWithTTL(cacheKey, channelID, time.Duration(ttlSeconds)*time.Second); err != nil {
		meta, _ := getChannelAffinityMeta(c)
		common.SysError(fmt.Sprintf("channel affinity cache set failed: rule=%q, err=%v", meta.RuleName, err))
	}
}

type ChannelAffinityUsageCacheStats struct {
	RuleName            string `json:"rule_name"`
	UsingGroup          string `json:"using_group"`
	KeyFingerprint      string `json:"key_fp"`
	CachedTokenRateMode string `json:"cached_token_rate_mode"`

	Hit           int64 `json:"hit"`
	Total         int64 `json:"total"`
	WindowSeconds int64 `json:"window_seconds"`

	PromptTokens         int64 `json:"prompt_tokens"`
	CompletionTokens     int64 `json:"completion_tokens"`
	TotalTokens          int64 `json:"total_tokens"`
	CachedTokens         int64 `json:"cached_tokens"`
	PromptCacheHitTokens int64 `json:"prompt_cache_hit_tokens"`
	LastSeenAt           int64 `json:"last_seen_at"`
}

type ChannelAffinityUsageCacheCounters struct {
	CachedTokenRateMode string `json:"cached_token_rate_mode"`

	Hit           int64 `json:"hit"`
	Total         int64 `json:"total"`
	WindowSeconds int64 `json:"window_seconds"`

	PromptTokens         int64 `json:"prompt_tokens"`
	CompletionTokens     int64 `json:"completion_tokens"`
	TotalTokens          int64 `json:"total_tokens"`
	CachedTokens         int64 `json:"cached_tokens"`
	PromptCacheHitTokens int64 `json:"prompt_cache_hit_tokens"`
	LastSeenAt           int64 `json:"last_seen_at"`
}

var channelAffinityUsageCacheStatsLocks [64]sync.Mutex

// ObserveChannelAffinityUsageCacheByRelayFormat records usage cache stats with a stable rate mode derived from relay format.
func ObserveChannelAffinityUsageCacheByRelayFormat(c *gin.Context, usage *dto.Usage, relayFormat types.RelayFormat) {
	ObserveChannelAffinityUsageCacheFromContext(c, usage, cachedTokenRateModeByRelayFormat(relayFormat))
}

func ObserveChannelAffinityUsageCacheFromContext(c *gin.Context, usage *dto.Usage, cachedTokenRateMode string) {
	statsCtx, ok := GetChannelAffinityStatsContext(c)
	if !ok {
		return
	}
	observeChannelAffinityUsageCache(statsCtx, usage, cachedTokenRateMode)
}

func GetChannelAffinityUsageCacheStats(ruleName, usingGroup, keyFp string) ChannelAffinityUsageCacheStats {
	ruleName = strings.TrimSpace(ruleName)
	usingGroup = strings.TrimSpace(usingGroup)
	keyFp = strings.TrimSpace(keyFp)

	entryKey := channelAffinityUsageCacheEntryKey(ruleName, usingGroup, keyFp)
	if entryKey == "" {
		return ChannelAffinityUsageCacheStats{
			RuleName:       ruleName,
			UsingGroup:     usingGroup,
			KeyFingerprint: keyFp,
		}
	}

	cache := getChannelAffinityUsageCacheStatsCache()
	v, found, err := cache.Get(entryKey)
	if err != nil || !found {
		return ChannelAffinityUsageCacheStats{
			RuleName:       ruleName,
			UsingGroup:     usingGroup,
			KeyFingerprint: keyFp,
		}
	}
	return ChannelAffinityUsageCacheStats{
		CachedTokenRateMode:  v.CachedTokenRateMode,
		RuleName:             ruleName,
		UsingGroup:           usingGroup,
		KeyFingerprint:       keyFp,
		Hit:                  v.Hit,
		Total:                v.Total,
		WindowSeconds:        v.WindowSeconds,
		PromptTokens:         v.PromptTokens,
		CompletionTokens:     v.CompletionTokens,
		TotalTokens:          v.TotalTokens,
		CachedTokens:         v.CachedTokens,
		PromptCacheHitTokens: v.PromptCacheHitTokens,
		LastSeenAt:           v.LastSeenAt,
	}
}

func observeChannelAffinityUsageCache(statsCtx ChannelAffinityStatsContext, usage *dto.Usage, cachedTokenRateMode string) {
	entryKey := channelAffinityUsageCacheEntryKey(statsCtx.RuleName, statsCtx.UsingGroup, statsCtx.KeyFingerprint)
	if entryKey == "" {
		return
	}

	windowSeconds := statsCtx.TTLSeconds
	if windowSeconds <= 0 {
		return
	}

	cache := getChannelAffinityUsageCacheStatsCache()
	ttl := time.Duration(windowSeconds) * time.Second

	lock := channelAffinityUsageCacheStatsLock(entryKey)
	lock.Lock()
	defer lock.Unlock()

	prev, found, err := cache.Get(entryKey)
	if err != nil {
		return
	}
	next := prev
	if !found {
		next = ChannelAffinityUsageCacheCounters{}
	}
	currentMode := normalizeCachedTokenRateMode(cachedTokenRateMode)
	if currentMode != "" {
		if next.CachedTokenRateMode == "" {
			next.CachedTokenRateMode = currentMode
		} else if next.CachedTokenRateMode != currentMode && next.CachedTokenRateMode != cacheTokenRateModeMixed {
			next.CachedTokenRateMode = cacheTokenRateModeMixed
		}
	}
	next.Total++
	hit, cachedTokens, promptCacheHitTokens := usageCacheSignals(usage)
	if hit {
		next.Hit++
	}
	next.WindowSeconds = windowSeconds
	next.LastSeenAt = time.Now().Unix()
	next.CachedTokens += cachedTokens
	next.PromptCacheHitTokens += promptCacheHitTokens
	next.PromptTokens += int64(usagePromptTokens(usage))
	next.CompletionTokens += int64(usageCompletionTokens(usage))
	next.TotalTokens += int64(usageTotalTokens(usage))
	_ = cache.SetWithTTL(entryKey, next, ttl)
}

func normalizeCachedTokenRateMode(mode string) string {
	switch mode {
	case cacheTokenRateModeCachedOverPrompt:
		return cacheTokenRateModeCachedOverPrompt
	case cacheTokenRateModeCachedOverPromptPlusCached:
		return cacheTokenRateModeCachedOverPromptPlusCached
	case cacheTokenRateModeMixed:
		return cacheTokenRateModeMixed
	default:
		return ""
	}
}

func cachedTokenRateModeByRelayFormat(relayFormat types.RelayFormat) string {
	switch relayFormat {
	case types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses, types.RelayFormatOpenAIResponsesCompaction:
		return cacheTokenRateModeCachedOverPrompt
	case types.RelayFormatClaude:
		return cacheTokenRateModeCachedOverPromptPlusCached
	default:
		return ""
	}
}

func channelAffinityUsageCacheEntryKey(ruleName, usingGroup, keyFp string) string {
	ruleName = strings.TrimSpace(ruleName)
	usingGroup = strings.TrimSpace(usingGroup)
	keyFp = strings.TrimSpace(keyFp)
	if ruleName == "" || keyFp == "" {
		return ""
	}
	return ruleName + "\n" + usingGroup + "\n" + keyFp
}

func usageCacheSignals(usage *dto.Usage) (hit bool, cachedTokens int64, promptCacheHitTokens int64) {
	if usage == nil {
		return false, 0, 0
	}

	cached := int64(0)
	if usage.PromptTokensDetails.CachedTokens > 0 {
		cached = int64(usage.PromptTokensDetails.CachedTokens)
	} else if usage.InputTokensDetails != nil && usage.InputTokensDetails.CachedTokens > 0 {
		cached = int64(usage.InputTokensDetails.CachedTokens)
	}
	pcht := int64(0)
	if usage.PromptCacheHitTokens > 0 {
		pcht = int64(usage.PromptCacheHitTokens)
	}
	return cached > 0 || pcht > 0, cached, pcht
}

func usagePromptTokens(usage *dto.Usage) int {
	if usage == nil {
		return 0
	}
	if usage.PromptTokens > 0 {
		return usage.PromptTokens
	}
	return usage.InputTokens
}

func usageCompletionTokens(usage *dto.Usage) int {
	if usage == nil {
		return 0
	}
	if usage.CompletionTokens > 0 {
		return usage.CompletionTokens
	}
	return usage.OutputTokens
}

func usageTotalTokens(usage *dto.Usage) int {
	if usage == nil {
		return 0
	}
	if usage.TotalTokens > 0 {
		return usage.TotalTokens
	}
	pt := usagePromptTokens(usage)
	ct := usageCompletionTokens(usage)
	if pt > 0 || ct > 0 {
		return pt + ct
	}
	return 0
}

func getChannelAffinityUsageCacheStatsCache() *cachex.HybridCache[ChannelAffinityUsageCacheCounters] {
	channelAffinityUsageCacheStatsOnce.Do(func() {
		setting := operation_setting.GetChannelAffinitySetting()
		capacity := 100_000
		defaultTTLSeconds := 3600
		if setting != nil {
			if setting.MaxEntries > 0 {
				capacity = setting.MaxEntries
			}
			if setting.DefaultTTLSeconds > 0 {
				defaultTTLSeconds = setting.DefaultTTLSeconds
			}
		}

		channelAffinityUsageCacheStatsCache = cachex.NewHybridCache[ChannelAffinityUsageCacheCounters](cachex.HybridCacheConfig[ChannelAffinityUsageCacheCounters]{
			Namespace: cachex.Namespace(channelAffinityUsageCacheStatsNamespace),
			Redis:     common.RDB,
			RedisEnabled: func() bool {
				return common.RedisEnabled && common.RDB != nil
			},
			RedisCodec: cachex.JSONCodec[ChannelAffinityUsageCacheCounters]{},
			Memory: func() *hot.HotCache[string, ChannelAffinityUsageCacheCounters] {
				return hot.NewHotCache[string, ChannelAffinityUsageCacheCounters](hot.LRU, capacity).
					WithTTL(time.Duration(defaultTTLSeconds) * time.Second).
					WithJanitor().
					Build()
			},
		})
	})
	return channelAffinityUsageCacheStatsCache
}

func channelAffinityUsageCacheStatsLock(key string) *sync.Mutex {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	idx := h.Sum32() % uint32(len(channelAffinityUsageCacheStatsLocks))
	return &channelAffinityUsageCacheStatsLocks[idx]
}
