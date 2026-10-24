package service

import (
	"context"
	"fmt"
	"hash/fnv"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/cachex"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/go-redis/redis/v8"
	"github.com/samber/hot"
)

const (
	UpstreamCacheWindow1h  = "1h"
	UpstreamCacheWindow24h = "24h"

	upstreamCacheUsageStatsNamespace1h  = "new-api:upstream_cache_usage_stats:v2:1h"
	upstreamCacheUsageStatsNamespace24h = "new-api:upstream_cache_usage_stats:v2:24h"

	upstreamCacheWindowSeconds1h  = int64(3600)
	upstreamCacheWindowSeconds24h = int64(86400)

	upstreamCacheUsageStatsCapacity = 100_000
	upstreamCacheBucketSeconds      = int64(60)
	// UpstreamCacheUsageStatsBatchMax is the max number of groups or channel IDs per batch query.
	UpstreamCacheUsageStatsBatchMax = 200
)

var (
	upstreamCacheUsageStats1hOnce   sync.Once
	upstreamCacheUsageStats24hOnce  sync.Once
	upstreamCacheUsageStats1hCache  *cachex.HybridCache[map[int64]UpstreamCacheUsageCounters]
	upstreamCacheUsageStats24hCache *cachex.HybridCache[map[int64]UpstreamCacheUsageCounters]

	upstreamCacheUsageStatsLocks [64]sync.Mutex
	upstreamCacheUsageStatsNow   = time.Now
)

// UpstreamCacheUsageCounters is the persisted accumulator for one (window, key) bucket.
type UpstreamCacheUsageCounters struct {
	BucketStart         int64  `json:"bucket_start"`
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

// UpstreamCacheUsageWindowStats is the API-facing view for one sliding window.
type UpstreamCacheUsageWindowStats struct {
	CachedTokenRateMode string `json:"cached_token_rate_mode"`

	Hit                  int64    `json:"hit"`
	Total                int64    `json:"total"`
	RequestHitRate       *float64 `json:"request_hit_rate"`
	TokenHitRate         *float64 `json:"token_hit_rate"`
	WindowSeconds        int64    `json:"window_seconds"`
	PromptTokens         int64    `json:"prompt_tokens"`
	CompletionTokens     int64    `json:"completion_tokens"`
	TotalTokens          int64    `json:"total_tokens"`
	CachedTokens         int64    `json:"cached_tokens"`
	PromptCacheHitTokens int64    `json:"prompt_cache_hit_tokens"`
	LastSeenAt           int64    `json:"last_seen_at"`
}

// UpstreamCacheUsageStats is one group/channel/group_channel entry with both windows.
type UpstreamCacheUsageStats struct {
	Scope     string                                   `json:"scope"`
	Group     string                                   `json:"group,omitempty"`
	ChannelID int                                      `json:"channel_id,omitempty"`
	Windows   map[string]UpstreamCacheUsageWindowStats `json:"windows"`
}

// ObserveUpstreamCacheUsage records Prompt/KV cache signals for the final UsingGroup
// and ChannelId. Independent of channel affinity. Skips channel tests and relay
// formats without a known cached-token rate mode (Embedding/Image/Rerank/Gemini/…).
func ObserveUpstreamCacheUsage(relayInfo *relaycommon.RelayInfo, usage *dto.Usage, relayFormat types.RelayFormat) {
	if relayInfo == nil || usage == nil || relayInfo.IsChannelTest {
		return
	}
	rateMode := cachedTokenRateModeByRelayFormat(relayFormat)
	if rateMode == "" {
		return
	}

	group := strings.TrimSpace(relayInfo.UsingGroup)
	channelID := 0
	if relayInfo.ChannelMeta != nil {
		channelID = relayInfo.ChannelId
	}
	if group == "" && channelID <= 0 {
		return
	}

	// List metrics only use group + channel keys; skip gc cross keys to limit cardinality.
	keys := make([]string, 0, 2)
	if group != "" {
		keys = append(keys, upstreamCacheGroupKey(group))
	}
	if channelID > 0 {
		keys = append(keys, upstreamCacheChannelKey(channelID))
	}
	for _, key := range keys {
		observeUpstreamCacheUsageKey(UpstreamCacheWindow1h, key, usage, rateMode)
		observeUpstreamCacheUsageKey(UpstreamCacheWindow24h, key, usage, rateMode)
	}
}

func GetUpstreamCacheUsageStats(scope, group string, channelID int, windows []string) UpstreamCacheUsageStats {
	scope = strings.TrimSpace(scope)
	group = strings.TrimSpace(group)
	windows = normalizeUpstreamCacheWindows(windows)

	stats := UpstreamCacheUsageStats{
		Scope:     scope,
		Group:     group,
		ChannelID: channelID,
		Windows:   make(map[string]UpstreamCacheUsageWindowStats, len(windows)),
	}

	entryKey := ""
	switch scope {
	case "group":
		entryKey = upstreamCacheGroupKey(group)
	case "channel":
		entryKey = upstreamCacheChannelKey(channelID)
	case "group_channel":
		entryKey = upstreamCacheGroupChannelKey(group, channelID)
	default:
		return stats
	}
	if entryKey == "" {
		return stats
	}

	for _, window := range windows {
		stats.Windows[window] = buildUpstreamCacheWindowStats(window, entryKey)
	}
	return stats
}

func BatchGetUpstreamCacheUsageStatsByChannels(channelIDs []int, windows []string) ([]UpstreamCacheUsageStats, error) {
	windows = normalizeUpstreamCacheWindows(windows)
	out := make([]UpstreamCacheUsageStats, 0, len(channelIDs))
	seen := make(map[int]struct{}, len(channelIDs))
	for _, id := range channelIDs {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		if len(seen) > UpstreamCacheUsageStatsBatchMax {
			return nil, fmt.Errorf("too many channel ids: max %d", UpstreamCacheUsageStatsBatchMax)
		}
		out = append(out, GetUpstreamCacheUsageStats("channel", "", id, windows))
	}
	return out, nil
}

func BatchGetUpstreamCacheUsageStatsByGroups(groups []string, windows []string) ([]UpstreamCacheUsageStats, error) {
	windows = normalizeUpstreamCacheWindows(windows)
	out := make([]UpstreamCacheUsageStats, 0, len(groups))
	seen := make(map[string]struct{}, len(groups))
	for _, group := range groups {
		group = strings.TrimSpace(group)
		if group == "" {
			continue
		}
		if _, ok := seen[group]; ok {
			continue
		}
		seen[group] = struct{}{}
		if len(seen) > UpstreamCacheUsageStatsBatchMax {
			return nil, fmt.Errorf("too many groups: max %d", UpstreamCacheUsageStatsBatchMax)
		}
		out = append(out, GetUpstreamCacheUsageStats("group", group, 0, windows))
	}
	return out, nil
}

// ClearUpstreamCacheUsageStats clears counters.
// window: "1h", "24h", or "all".
// all=true purges the selected window cache(s). Otherwise scope+key are required;
// empty scope without all is rejected so accidental clears cannot wipe everything.
func ClearUpstreamCacheUsageStats(window string, all bool, scope, group string, channelID int) (cleared int, err error) {
	windows := normalizeUpstreamCacheWindows([]string{window})
	if window == "" || window == "all" {
		windows = []string{UpstreamCacheWindow1h, UpstreamCacheWindow24h}
	}

	scope = strings.TrimSpace(scope)
	group = strings.TrimSpace(group)

	if !all {
		if scope == "" {
			return 0, fmt.Errorf("missing scope: pass all=true to purge, or scope with key")
		}
	}

	for _, w := range windows {
		cache := getUpstreamCacheUsageStatsCache(w)
		if cache == nil {
			continue
		}
		if all {
			if e := cache.Purge(); e != nil {
				return cleared, e
			}
			cleared++
			continue
		}
		entryKey := ""
		switch scope {
		case "group":
			entryKey = upstreamCacheGroupKey(group)
		case "channel":
			entryKey = upstreamCacheChannelKey(channelID)
		case "group_channel":
			entryKey = upstreamCacheGroupChannelKey(group, channelID)
		default:
			return cleared, fmt.Errorf("invalid scope: %s", scope)
		}
		if entryKey == "" {
			return 0, fmt.Errorf("missing key for scope %s", scope)
		}
		lock := upstreamCacheUsageStatsLock(w + "\n" + entryKey)
		lock.Lock()
		res, e := cache.DeleteMany([]string{entryKey})
		lock.Unlock()
		if e != nil {
			return cleared, e
		}
		for _, ok := range res {
			if ok {
				cleared++
			}
		}
	}
	return cleared, nil
}

// One bounded hash ring per window/key. Updating a slot and its TTL in one
// script preserves increments from every gateway instance sharing Redis.
var upstreamCacheUsageIncrement = redis.NewScript(`
local incoming = cjson.decode(ARGV[3])
local raw = redis.call('HGET', KEYS[1], ARGV[1])
if raw then
    local previous = cjson.decode(raw)
    if previous.bucket_start == incoming.bucket_start then
        for _, field in ipairs({'hit', 'total', 'prompt_tokens', 'completion_tokens',
            'total_tokens', 'cached_tokens', 'prompt_cache_hit_tokens'}) do
            incoming[field] = incoming[field] + previous[field]
        end
        if previous.cached_token_rate_mode ~= incoming.cached_token_rate_mode then
            incoming.cached_token_rate_mode = 'mixed'
        end
        incoming.last_seen_at = math.max(previous.last_seen_at, incoming.last_seen_at)
    elseif previous.bucket_start > incoming.bucket_start then
        return 0
    end
end
redis.call('HSET', KEYS[1], ARGV[1], cjson.encode(incoming))
redis.call('EXPIRE', KEYS[1], ARGV[2])
return 1
`)

func observeUpstreamCacheUsageKey(window, entryKey string, usage *dto.Usage, cachedTokenRateMode string) {
	if entryKey == "" || usage == nil {
		return
	}
	cache := getUpstreamCacheUsageStatsCache(window)
	ttlSeconds := upstreamCacheWindowSeconds(window)
	now := upstreamCacheUsageStatsNow().Unix()
	bucketStart := now - now%upstreamCacheBucketSeconds
	slot := bucketStart / upstreamCacheBucketSeconds % (ttlSeconds / upstreamCacheBucketSeconds)
	hit, cachedTokens, promptCacheHitTokens := usageCacheSignals(usage)
	delta := UpstreamCacheUsageCounters{
		BucketStart:          bucketStart,
		CachedTokenRateMode:  normalizeCachedTokenRateMode(cachedTokenRateMode),
		Total:                1,
		WindowSeconds:        ttlSeconds,
		LastSeenAt:           now,
		CachedTokens:         cachedTokens,
		PromptCacheHitTokens: promptCacheHitTokens,
		PromptTokens:         int64(usagePromptTokens(usage)),
		CompletionTokens:     int64(usageCompletionTokens(usage)),
		TotalTokens:          int64(usageTotalTokens(usage)),
	}
	if hit {
		delta.Hit = 1
	}
	if common.RedisEnabled && common.RDB != nil {
		data, err := common.Marshal(delta)
		if err != nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = upstreamCacheUsageIncrement.Run(ctx, common.RDB, []string{cache.FullKey(entryKey)}, slot, ttlSeconds, string(data)).Err()
		return
	}

	lock := upstreamCacheUsageStatsLock(window + "\n" + entryKey)
	lock.Lock()
	defer lock.Unlock()
	buckets, found, err := cache.Get(entryKey)
	if err != nil {
		return
	}
	if !found {
		buckets = make(map[int64]UpstreamCacheUsageCounters)
	}
	if previous := buckets[slot]; previous.BucketStart == bucketStart {
		delta = addUpstreamCacheUsageCounters(delta, previous)
	} else if previous.BucketStart > bucketStart {
		return
	}
	buckets[slot] = delta
	_ = cache.SetWithTTL(entryKey, buckets, time.Duration(ttlSeconds)*time.Second)
}

func addUpstreamCacheUsageCounters(a, b UpstreamCacheUsageCounters) UpstreamCacheUsageCounters {
	if a.CachedTokenRateMode == "" {
		a.CachedTokenRateMode = b.CachedTokenRateMode
	} else if b.CachedTokenRateMode != "" && a.CachedTokenRateMode != b.CachedTokenRateMode {
		a.CachedTokenRateMode = cacheTokenRateModeMixed
	}
	a.Hit += b.Hit
	a.Total += b.Total
	a.PromptTokens += b.PromptTokens
	a.CompletionTokens += b.CompletionTokens
	a.TotalTokens += b.TotalTokens
	a.CachedTokens += b.CachedTokens
	a.PromptCacheHitTokens += b.PromptCacheHitTokens
	a.LastSeenAt = max(a.LastSeenAt, b.LastSeenAt)
	return a
}

func buildUpstreamCacheWindowStats(window, entryKey string) UpstreamCacheUsageWindowStats {
	windowSeconds := upstreamCacheWindowSeconds(window)
	counters := UpstreamCacheUsageCounters{WindowSeconds: windowSeconds}
	if entryKey == "" {
		return countersToUpstreamCacheWindowStats(counters)
	}
	cache := getUpstreamCacheUsageStatsCache(window)
	// Minute buckets bound storage and give the advertised approximate sliding
	// windows a maximum boundary error of less than one minute.
	cutoff := upstreamCacheUsageStatsNow().Unix() - windowSeconds
	if common.RedisEnabled && common.RDB != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		values, err := common.RDB.HVals(ctx, cache.FullKey(entryKey)).Result()
		if err != nil {
			return countersToUpstreamCacheWindowStats(counters)
		}
		for _, raw := range values {
			var bucket UpstreamCacheUsageCounters
			if common.UnmarshalJsonStr(raw, &bucket) == nil && bucket.BucketStart > cutoff {
				counters = addUpstreamCacheUsageCounters(counters, bucket)
			}
		}
		return countersToUpstreamCacheWindowStats(counters)
	}

	lock := upstreamCacheUsageStatsLock(window + "\n" + entryKey)
	lock.Lock()
	defer lock.Unlock()
	buckets, found, err := cache.Get(entryKey)
	if err == nil && found {
		for _, bucket := range buckets {
			if bucket.BucketStart > cutoff {
				counters = addUpstreamCacheUsageCounters(counters, bucket)
			}
		}
	}
	return countersToUpstreamCacheWindowStats(counters)
}

func countersToUpstreamCacheWindowStats(v UpstreamCacheUsageCounters) UpstreamCacheUsageWindowStats {
	out := UpstreamCacheUsageWindowStats{
		CachedTokenRateMode:  v.CachedTokenRateMode,
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
	if v.Total > 0 {
		rate := float64(v.Hit) / float64(v.Total)
		out.RequestHitRate = &rate
	}
	if tokenRate, ok := computeUpstreamCacheTokenHitRate(v); ok {
		out.TokenHitRate = &tokenRate
	}
	return out
}

func computeUpstreamCacheTokenHitRate(v UpstreamCacheUsageCounters) (float64, bool) {
	switch v.CachedTokenRateMode {
	case cacheTokenRateModeCachedOverPrompt:
		if v.PromptTokens <= 0 {
			return 0, false
		}
		return float64(v.CachedTokens) / float64(v.PromptTokens), true
	case cacheTokenRateModeCachedOverPromptPlusCached:
		denom := v.PromptTokens + v.CachedTokens
		if denom <= 0 {
			return 0, false
		}
		return float64(v.CachedTokens) / float64(denom), true
	default:
		return 0, false
	}
}

func normalizeUpstreamCacheWindows(windows []string) []string {
	if len(windows) == 0 {
		return []string{UpstreamCacheWindow1h, UpstreamCacheWindow24h}
	}
	out := make([]string, 0, 2)
	seen := make(map[string]struct{}, 2)
	for _, w := range windows {
		w = strings.TrimSpace(strings.ToLower(w))
		if w == "all" {
			return []string{UpstreamCacheWindow1h, UpstreamCacheWindow24h}
		}
		if w != UpstreamCacheWindow1h && w != UpstreamCacheWindow24h {
			continue
		}
		if _, ok := seen[w]; ok {
			continue
		}
		seen[w] = struct{}{}
		out = append(out, w)
	}
	if len(out) == 0 {
		return []string{UpstreamCacheWindow1h, UpstreamCacheWindow24h}
	}
	return out
}

func upstreamCacheWindowSeconds(window string) int64 {
	switch window {
	case UpstreamCacheWindow24h:
		return upstreamCacheWindowSeconds24h
	default:
		return upstreamCacheWindowSeconds1h
	}
}

func upstreamCacheGroupKey(group string) string {
	group = strings.TrimSpace(group)
	if group == "" {
		return ""
	}
	return "g\n" + group
}

func upstreamCacheChannelKey(channelID int) string {
	if channelID <= 0 {
		return ""
	}
	return "c\n" + strconv.Itoa(channelID)
}

func upstreamCacheGroupChannelKey(group string, channelID int) string {
	group = strings.TrimSpace(group)
	if group == "" || channelID <= 0 {
		return ""
	}
	return "gc\n" + group + "\n" + strconv.Itoa(channelID)
}

func getUpstreamCacheUsageStatsCache(window string) *cachex.HybridCache[map[int64]UpstreamCacheUsageCounters] {
	switch window {
	case UpstreamCacheWindow24h:
		upstreamCacheUsageStats24hOnce.Do(func() {
			upstreamCacheUsageStats24hCache = newUpstreamCacheUsageStatsCache(
				upstreamCacheUsageStatsNamespace24h,
				upstreamCacheWindowSeconds24h,
			)
		})
		return upstreamCacheUsageStats24hCache
	default:
		upstreamCacheUsageStats1hOnce.Do(func() {
			upstreamCacheUsageStats1hCache = newUpstreamCacheUsageStatsCache(
				upstreamCacheUsageStatsNamespace1h,
				upstreamCacheWindowSeconds1h,
			)
		})
		return upstreamCacheUsageStats1hCache
	}
}

func newUpstreamCacheUsageStatsCache(namespace string, ttlSeconds int64) *cachex.HybridCache[map[int64]UpstreamCacheUsageCounters] {
	return cachex.NewHybridCache[map[int64]UpstreamCacheUsageCounters](cachex.HybridCacheConfig[map[int64]UpstreamCacheUsageCounters]{
		Namespace: cachex.Namespace(namespace),
		Redis:     common.RDB,
		RedisEnabled: func() bool {
			return common.RedisEnabled && common.RDB != nil
		},
		RedisCodec: cachex.JSONCodec[map[int64]UpstreamCacheUsageCounters]{},
		Memory: func() *hot.HotCache[string, map[int64]UpstreamCacheUsageCounters] {
			return hot.NewHotCache[string, map[int64]UpstreamCacheUsageCounters](hot.LRU, upstreamCacheUsageStatsCapacity).
				WithTTL(time.Duration(ttlSeconds) * time.Second).
				WithJanitor().
				Build()
		},
	})
}

func upstreamCacheUsageStatsLock(key string) *sync.Mutex {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	idx := h.Sum32() % uint32(len(upstreamCacheUsageStatsLocks))
	return &upstreamCacheUsageStatsLocks[idx]
}
