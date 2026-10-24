package service

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var upstreamCacheTestSeq atomic.Uint64

func uniqueUpstreamCacheGroup(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("g_uc_%d_%d", time.Now().UnixNano(), upstreamCacheTestSeq.Add(1))
}

func TestObserveUpstreamCacheUsage_DualWindowAndScopes(t *testing.T) {
	group := uniqueUpstreamCacheGroup(t)
	channelID := int(time.Now().UnixNano()%900000) + 100000

	info := &relaycommon.RelayInfo{
		UsingGroup:  group,
		ChannelMeta: &relaycommon.ChannelMeta{ChannelId: channelID},
	}
	usage := &dto.Usage{
		PromptTokens:     100,
		CompletionTokens: 20,
		TotalTokens:      120,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 40,
		},
	}

	ObserveUpstreamCacheUsage(info, usage, types.RelayFormatOpenAI)

	groupStats := GetUpstreamCacheUsageStats("group", group, 0, nil)
	require.Contains(t, groupStats.Windows, UpstreamCacheWindow1h)
	require.Contains(t, groupStats.Windows, UpstreamCacheWindow24h)
	assert.EqualValues(t, 1, groupStats.Windows[UpstreamCacheWindow1h].Total)
	assert.EqualValues(t, 1, groupStats.Windows[UpstreamCacheWindow1h].Hit)
	assert.EqualValues(t, 1, groupStats.Windows[UpstreamCacheWindow24h].Total)
	assert.EqualValues(t, 1, groupStats.Windows[UpstreamCacheWindow24h].Hit)
	require.NotNil(t, groupStats.Windows[UpstreamCacheWindow1h].RequestHitRate)
	assert.InDelta(t, 1.0, *groupStats.Windows[UpstreamCacheWindow1h].RequestHitRate, 1e-9)
	require.NotNil(t, groupStats.Windows[UpstreamCacheWindow1h].TokenHitRate)
	assert.InDelta(t, 0.4, *groupStats.Windows[UpstreamCacheWindow1h].TokenHitRate, 1e-9)
	assert.Equal(t, cacheTokenRateModeCachedOverPrompt, groupStats.Windows[UpstreamCacheWindow1h].CachedTokenRateMode)

	channelStats := GetUpstreamCacheUsageStats("channel", "", channelID, nil)
	assert.EqualValues(t, 1, channelStats.Windows[UpstreamCacheWindow1h].Total)
	assert.EqualValues(t, 40, channelStats.Windows[UpstreamCacheWindow1h].CachedTokens)

	// Cross keys are not written on observe (cardinality control).
	gcStats := GetUpstreamCacheUsageStats("group_channel", group, channelID, nil)
	assert.EqualValues(t, 0, gcStats.Windows[UpstreamCacheWindow24h].Total)
}

func TestObserveUpstreamCacheUsage_ClaudeTokenRate(t *testing.T) {
	group := uniqueUpstreamCacheGroup(t)
	channelID := int(time.Now().UnixNano()%900000) + 200000
	info := &relaycommon.RelayInfo{
		UsingGroup:  group,
		ChannelMeta: &relaycommon.ChannelMeta{ChannelId: channelID},
	}
	usage := &dto.Usage{
		PromptTokens: 80,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 20,
		},
	}

	ObserveUpstreamCacheUsage(info, usage, types.RelayFormatClaude)
	stats := GetUpstreamCacheUsageStats("channel", "", channelID, []string{UpstreamCacheWindow1h})
	w := stats.Windows[UpstreamCacheWindow1h]
	assert.Equal(t, cacheTokenRateModeCachedOverPromptPlusCached, w.CachedTokenRateMode)
	require.NotNil(t, w.TokenHitRate)
	assert.InDelta(t, 0.2, *w.TokenHitRate, 1e-9) // 20 / (80+20)
}

func TestObserveUpstreamCacheUsage_SkipsUnsupportedAndChannelTest(t *testing.T) {
	group := uniqueUpstreamCacheGroup(t)
	channelID := int(time.Now().UnixNano()%900000) + 300000
	usage := &dto.Usage{
		PromptTokens: 50,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 10,
		},
	}

	ObserveUpstreamCacheUsage(&relaycommon.RelayInfo{
		UsingGroup:  group,
		ChannelMeta: &relaycommon.ChannelMeta{ChannelId: channelID},
	}, usage, types.RelayFormatGemini)
	assert.EqualValues(t, 0, GetUpstreamCacheUsageStats("group", group, 0, []string{UpstreamCacheWindow1h}).Windows[UpstreamCacheWindow1h].Total)

	ObserveUpstreamCacheUsage(&relaycommon.RelayInfo{
		UsingGroup:    group,
		IsChannelTest: true,
		ChannelMeta:   &relaycommon.ChannelMeta{ChannelId: channelID},
	}, usage, types.RelayFormatOpenAI)
	assert.EqualValues(t, 0, GetUpstreamCacheUsageStats("channel", "", channelID, []string{UpstreamCacheWindow1h}).Windows[UpstreamCacheWindow1h].Total)
}

func TestObserveUpstreamCacheUsage_WorksWithoutAffinity(t *testing.T) {
	group := uniqueUpstreamCacheGroup(t)
	channelID := int(time.Now().UnixNano()%900000) + 400000
	info := &relaycommon.RelayInfo{
		UsingGroup:  group,
		ChannelMeta: &relaycommon.ChannelMeta{ChannelId: channelID},
	}
	ObserveUpstreamCacheUsage(info, &dto.Usage{PromptTokens: 10}, types.RelayFormatOpenAI)
	stats := GetUpstreamCacheUsageStats("group", group, 0, []string{UpstreamCacheWindow1h})
	assert.EqualValues(t, 1, stats.Windows[UpstreamCacheWindow1h].Total)
	assert.EqualValues(t, 0, stats.Windows[UpstreamCacheWindow1h].Hit)
}

func TestClearUpstreamCacheUsageStats_SingleWindow(t *testing.T) {
	group := uniqueUpstreamCacheGroup(t)
	channelID := int(time.Now().UnixNano()%900000) + 500000
	info := &relaycommon.RelayInfo{
		UsingGroup:  group,
		ChannelMeta: &relaycommon.ChannelMeta{ChannelId: channelID},
	}
	ObserveUpstreamCacheUsage(info, &dto.Usage{
		PromptTokens: 10,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 1,
		},
	}, types.RelayFormatOpenAI)

	_, err := ClearUpstreamCacheUsageStats(UpstreamCacheWindow1h, false, "", "", 0)
	require.Error(t, err)

	cleared, err := ClearUpstreamCacheUsageStats(UpstreamCacheWindow1h, false, "channel", "", channelID)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, cleared, 1)

	after1h := GetUpstreamCacheUsageStats("channel", "", channelID, []string{UpstreamCacheWindow1h, UpstreamCacheWindow24h})
	assert.EqualValues(t, 0, after1h.Windows[UpstreamCacheWindow1h].Total)
	assert.EqualValues(t, 1, after1h.Windows[UpstreamCacheWindow24h].Total)
}

func TestBatchGetUpstreamCacheUsageStats(t *testing.T) {
	groupA := uniqueUpstreamCacheGroup(t)
	groupB := uniqueUpstreamCacheGroup(t)
	channelA := int(time.Now().UnixNano()%900000) + 600000
	channelB := channelA + 1

	ObserveUpstreamCacheUsage(&relaycommon.RelayInfo{
		UsingGroup:  groupA,
		ChannelMeta: &relaycommon.ChannelMeta{ChannelId: channelA},
	}, &dto.Usage{PromptTokens: 5, PromptTokensDetails: dto.InputTokenDetails{CachedTokens: 1}}, types.RelayFormatOpenAI)
	ObserveUpstreamCacheUsage(&relaycommon.RelayInfo{
		UsingGroup:  groupB,
		ChannelMeta: &relaycommon.ChannelMeta{ChannelId: channelB},
	}, &dto.Usage{PromptTokens: 5}, types.RelayFormatOpenAI)

	byChannel, err := BatchGetUpstreamCacheUsageStatsByChannels([]int{channelA, channelB, channelA}, nil)
	require.NoError(t, err)
	require.Len(t, byChannel, 2)
	assert.EqualValues(t, 1, byChannel[0].Windows[UpstreamCacheWindow1h].Hit)
	assert.EqualValues(t, 0, byChannel[1].Windows[UpstreamCacheWindow1h].Hit)

	byGroup, err := BatchGetUpstreamCacheUsageStatsByGroups([]string{groupA, groupB}, []string{UpstreamCacheWindow24h})
	require.NoError(t, err)
	require.Len(t, byGroup, 2)
	assert.EqualValues(t, 1, byGroup[0].Windows[UpstreamCacheWindow24h].Total)
	_, has1h := byGroup[0].Windows[UpstreamCacheWindow1h]
	assert.False(t, has1h)
}

func TestBatchGetUpstreamCacheUsageStats_RejectsOverLimit(t *testing.T) {
	ids := make([]int, 0, UpstreamCacheUsageStatsBatchMax+1)
	for i := range UpstreamCacheUsageStatsBatchMax + 1 {
		ids = append(ids, i+1)
	}
	_, err := BatchGetUpstreamCacheUsageStatsByChannels(ids, []string{UpstreamCacheWindow1h})
	require.Error(t, err)
}

func TestComputeUpstreamCacheTokenHitRate_MixedReturnsFalse(t *testing.T) {
	_, ok := computeUpstreamCacheTokenHitRate(UpstreamCacheUsageCounters{
		CachedTokenRateMode: cacheTokenRateModeMixed,
		CachedTokens:        10,
		PromptTokens:        100,
	})
	assert.False(t, ok)
}

// Each case owns the cache backend and clock; no real-time sleeps or external Redis.
func setupUpstreamCacheUsageTest(t *testing.T, useRedis bool) *time.Time {
	t.Helper()
	getUpstreamCacheUsageStatsCache(UpstreamCacheWindow1h)
	getUpstreamCacheUsageStatsCache(UpstreamCacheWindow24h)
	previous1h, previous24h := upstreamCacheUsageStats1hCache, upstreamCacheUsageStats24hCache
	previousNow := upstreamCacheUsageStatsNow
	previousEnabled, previousRedis := common.RedisEnabled, common.RDB
	t.Cleanup(func() {
		upstreamCacheUsageStats1hCache, upstreamCacheUsageStats24hCache = previous1h, previous24h
		upstreamCacheUsageStatsNow = previousNow
		common.RedisEnabled, common.RDB = previousEnabled, previousRedis
	})
	common.RedisEnabled = useRedis
	common.RDB = nil
	if useRedis {
		server := miniredis.RunT(t)
		client := redis.NewClient(&redis.Options{Addr: server.Addr()})
		t.Cleanup(func() { _ = client.Close() })
		common.RDB = client
	}
	upstreamCacheUsageStats1hCache = newUpstreamCacheUsageStatsCache(upstreamCacheUsageStatsNamespace1h, upstreamCacheWindowSeconds1h)
	upstreamCacheUsageStats24hCache = newUpstreamCacheUsageStatsCache(upstreamCacheUsageStatsNamespace24h, upstreamCacheWindowSeconds24h)
	now := time.Date(2026, time.October, 24, 12, 0, 0, 0, time.UTC)
	upstreamCacheUsageStatsNow = func() time.Time { return now }
	return &now
}

func TestUpstreamCacheUsageSlidingWindows(t *testing.T) {
	for _, backend := range []string{"memory", "redis"} {
		t.Run(backend, func(t *testing.T) {
			now := setupUpstreamCacheUsageTest(t, backend == "redis")
			start := *now
			info := &relaycommon.RelayInfo{UsingGroup: "vip", ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 3}}
			ObserveUpstreamCacheUsage(info, &dto.Usage{
				PromptTokens:        80,
				PromptTokensDetails: dto.InputTokenDetails{CachedTokens: 20},
			}, types.RelayFormatClaude)
			*now = start.Add(59 * time.Minute)
			ObserveUpstreamCacheUsage(info, &dto.Usage{PromptTokens: 100}, types.RelayFormatOpenAI)
			mixed := GetUpstreamCacheUsageStats("channel", "", 3, nil).Windows[UpstreamCacheWindow1h]
			assert.EqualValues(t, 2, mixed.Total)
			assert.Equal(t, cacheTokenRateModeMixed, mixed.CachedTokenRateMode)

			*now = start.Add(time.Hour)
			stats := GetUpstreamCacheUsageStats("channel", "", 3, nil)
			w := stats.Windows[UpstreamCacheWindow1h]
			assert.EqualValues(t, 1, w.Total, "the first sample must age out despite continued traffic")
			assert.Zero(t, w.Hit)
			assert.EqualValues(t, 100, w.PromptTokens)
			assert.Equal(t, cacheTokenRateModeCachedOverPrompt, w.CachedTokenRateMode)
			require.NotNil(t, w.TokenHitRate)
			assert.Zero(t, *w.TokenHitRate)
			assert.EqualValues(t, 2, stats.Windows[UpstreamCacheWindow24h].Total)

			// Reads also discard old buckets when no new requests arrive.
			*now = start.Add(24 * time.Hour)
			stats = GetUpstreamCacheUsageStats("channel", "", 3, nil)
			assert.Zero(t, stats.Windows[UpstreamCacheWindow1h].Total)
			assert.EqualValues(t, 1, stats.Windows[UpstreamCacheWindow24h].Total)
			assert.Zero(t, stats.Windows[UpstreamCacheWindow24h].Hit)

			// Ring slots are reused without resurrecting the previous day's counters.
			ObserveUpstreamCacheUsage(info, &dto.Usage{PromptTokens: 50}, types.RelayFormatOpenAI)
			assert.EqualValues(t, 2, GetUpstreamCacheUsageStats("channel", "", 3, nil).Windows[UpstreamCacheWindow24h].Total)
			*now = start.Add(25 * time.Hour)
			stats = GetUpstreamCacheUsageStats("channel", "", 3, nil)
			assert.Zero(t, stats.Windows[UpstreamCacheWindow1h].Total)
			assert.EqualValues(t, 1, stats.Windows[UpstreamCacheWindow24h].Total)
			assert.EqualValues(t, 50, stats.Windows[UpstreamCacheWindow24h].PromptTokens)

			_, err := ClearUpstreamCacheUsageStats("24h", false, "channel", "", 3)
			require.NoError(t, err)
			assert.Zero(t, GetUpstreamCacheUsageStats("channel", "", 3, nil).Windows[UpstreamCacheWindow24h].Total)
			_, err = ClearUpstreamCacheUsageStats("all", true, "", "", 0)
			require.NoError(t, err)
			assert.Zero(t, GetUpstreamCacheUsageStats("group", "vip", 0, nil).Windows[UpstreamCacheWindow24h].Total)
		})
	}
}

func TestUpstreamCacheUsageRedisSharedWriters(t *testing.T) {
	now := setupUpstreamCacheUsageTest(t, true)
	second := redis.NewClient(common.RDB.Options())
	t.Cleanup(func() { _ = second.Close() })
	cache := getUpstreamCacheUsageStatsCache(UpstreamCacheWindow1h)
	key := cache.FullKey(upstreamCacheChannelKey(3))
	// Independent clients represent gateways sharing Redis, without a process-local lock.
	start := make(chan struct{})
	errors := make(chan error, 2)
	var writers sync.WaitGroup
	for i, client := range []*redis.Client{common.RDB, second} {
		delta := UpstreamCacheUsageCounters{
			BucketStart: now.Unix(), LastSeenAt: now.Unix(), Total: 1,
			PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120,
			CachedTokens: int64(i * 40), Hit: int64(i),
			CachedTokenRateMode: cacheTokenRateModeCachedOverPrompt,
			WindowSeconds:       upstreamCacheWindowSeconds1h,
		}
		payload, err := common.Marshal(delta)
		require.NoError(t, err)
		writers.Go(func() {
			<-start
			slot := now.Unix() / upstreamCacheBucketSeconds % (upstreamCacheWindowSeconds1h / upstreamCacheBucketSeconds)
			errors <- upstreamCacheUsageIncrement.Run(context.Background(), client, []string{key}, slot, upstreamCacheWindowSeconds1h, string(payload)).Err()
		})
	}
	close(start)
	writers.Wait()
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	w := GetUpstreamCacheUsageStats("channel", "", 3, nil).Windows[UpstreamCacheWindow1h]
	assert.EqualValues(t, 2, w.Total)
	assert.EqualValues(t, 1, w.Hit)
	assert.EqualValues(t, 200, w.PromptTokens)
	assert.EqualValues(t, 40, w.CachedTokens)
	assert.EqualValues(t, 40, w.CompletionTokens)
	assert.EqualValues(t, 240, w.TotalTokens)
	require.NotNil(t, w.RequestHitRate)
	assert.Equal(t, 0.5, *w.RequestHitRate)
	require.NotNil(t, w.TokenHitRate)
	assert.Equal(t, 0.2, *w.TokenHitRate)
}
