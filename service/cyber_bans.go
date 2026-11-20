package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/go-redis/redis/v8"
)

var ErrCyberRedisUnavailable = errors.New("Cyber ban storage unavailable")
var ErrCyberBanChanged = errors.New("Cyber ban no longer exists or has changed")

type CyberBanMetadata struct {
	ID        string `json:"id"`
	RequestID string `json:"request_id"`
	CreatedAt int64  `json:"created_at"`
}

type CyberBanTarget struct {
	TokenID     int    `json:"token_id"`
	ChannelID   int    `json:"channel_id"`
	Fingerprint string `json:"fingerprint"`
}

type CyberBan struct {
	CyberBanTarget
	CyberBanMetadata
	Permanent  bool   `json:"permanent"`
	ExpiresAt  int64  `json:"expires_at,omitempty"`
	Generation string `json:"generation"`
}

func (target CyberBanTarget) key() (string, bool) {
	if !IsCyberFingerprint(target.Fingerprint) || target.TokenID < 1 || target.ChannelID < 0 {
		return "", false
	}
	session := cyberRequestSession{policy: CyberPolicy{Mode: cyberSessionEnforce}, tokenID: target.TokenID, digest: target.Fingerprint}
	return session.redisKey(target.ChannelID), true
}

func IsCyberFingerprint(s string) bool {
	if len(s) != sha256.Size*2 || s != strings.ToLower(s) {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

func parseCyberBanKey(key string) (CyberBanTarget, bool) {
	suffix, ok := strings.CutPrefix(key, cyberSessionPrefix+cyberSessionEnforce+":")
	if !ok {
		return CyberBanTarget{}, false
	}
	parts := strings.Split(suffix, ":")
	channelID := 0
	if len(parts) == 4 && parts[0] == "channel" {
		channelID, _ = strconv.Atoi(parts[1])
		if channelID <= 0 {
			return CyberBanTarget{}, false
		}
		parts = parts[2:]
	}
	if len(parts) != 2 {
		return CyberBanTarget{}, false
	}
	tokenID, _ := strconv.Atoi(parts[0])
	target := CyberBanTarget{TokenID: tokenID, ChannelID: channelID, Fingerprint: parts[1]}
	want, ok := target.key()
	return target, ok && key == want
}

// ListCyberBans returns one bounded SCAN window. A nonzero cursor means more
// windows may exist even if this window is empty. Redis never scans on relay.
func ListCyberBans(ctx context.Context, cursor uint64) ([]CyberBan, uint64, error) {
	if !common.RedisEnabled || common.RDB == nil {
		return nil, 0, ErrCyberRedisUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	keys, next, err := common.RDB.Scan(ctx, cursor, cyberSessionPrefix+cyberSessionEnforce+":*", 100).Result()
	if err != nil {
		return nil, 0, ErrCyberRedisUnavailable
	}
	items := make([]CyberBan, 0, len(keys))
	if len(keys) == 0 {
		return items, next, nil
	}
	type read struct {
		target CyberBanTarget
		value  *redis.StringCmd
		ttl    *redis.DurationCmd
	}
	reads := make([]read, 0, len(keys))
	_, err = common.RDB.Pipelined(ctx, func(pipe redis.Pipeliner) error {
		for _, key := range keys {
			if target, ok := parseCyberBanKey(key); ok {
				reads = append(reads, read{target, pipe.Get(ctx, key), pipe.PTTL(ctx, key)})
			}
		}
		return nil
	})
	if err != nil && !errors.Is(err, redis.Nil) {
		return nil, 0, ErrCyberRedisUnavailable
	}
	for _, r := range reads {
		value, err := r.value.Result()
		if errors.Is(err, redis.Nil) {
			continue
		}
		if err != nil || r.ttl.Err() != nil {
			return nil, 0, ErrCyberRedisUnavailable
		}
		ttl := r.ttl.Val()
		if ttl < 0 && ttl != cyberSessionNoExpiry {
			continue
		}
		var meta CyberBanMetadata
		if value != "1" && common.UnmarshalJsonStr(value, &meta) != nil {
			continue
		}
		item := CyberBan{CyberBanTarget: r.target, CyberBanMetadata: meta, Permanent: ttl == cyberSessionNoExpiry, Generation: fmt.Sprintf("%x", sha256.Sum256([]byte(value)))}
		if !item.Permanent {
			item.ExpiresAt = time.Now().Add(ttl).Unix()
		}
		items = append(items, item)
	}
	return items, next, nil
}

var revokeCyberBanScript = redis.NewScript(`
if redis.call('GET', KEYS[1]) == ARGV[1] then
    return redis.call('DEL', KEYS[1])
end
return 0
`)

func RevokeCyberBan(ctx context.Context, target CyberBanTarget, generation string) error {
	key, ok := target.key()
	if !ok || !IsCyberFingerprint(generation) {
		return ErrCyberBanChanged
	}
	if !common.RedisEnabled || common.RDB == nil {
		return ErrCyberRedisUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	value, err := common.RDB.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return ErrCyberBanChanged
	}
	if err != nil {
		return ErrCyberRedisUnavailable
	}
	if fmt.Sprintf("%x", sha256.Sum256([]byte(value))) != generation {
		return ErrCyberBanChanged
	}
	deleted, err := revokeCyberBanScript.Run(ctx, common.RDB, []string{key}, value).Int()
	if err != nil {
		return ErrCyberRedisUnavailable
	}
	if deleted != 1 {
		return ErrCyberBanChanged
	}
	return nil
}
