package service

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	cyberSessionShadow      = "shadow"
	cyberSessionEnforce     = "enforce"
	CyberSessionBlockedCode = "cyber_session_blocked"
	cyberSessionLogKey      = "cyber_session_guard_log"
	cyberSessionArmKey      = "cyber_session_guard_arm"
	cyberSessionCacheLimit  = 8 << 20
	cyberSessionRedisBudget = 250 * time.Millisecond
	// go-redis returns the raw sentinel for Redis PTTL when the key has no expiry.
	cyberSessionNoExpiry = -1 * time.Nanosecond
)

type cyberSessionConfig struct {
	mode     string
	tokenIDs map[int]struct{}
	ttl      time.Duration
}

var cyberSessionSettings cyberSessionConfig

var cyberSessionCounters struct {
	checked, ineligible, armed, wouldBlock, blocked, readError, writeError atomic.Uint64
}

func InitCyberSessionGuard() {
	settings, err := loadCyberSessionConfig(os.Getenv)
	cyberSessionSettings = settings
	if err != nil {
		common.SysError("cyber_session_guard disabled: " + err.Error())
		return
	}
	if settings.mode != "off" {
		common.SysLog(fmt.Sprintf("cyber_session_guard mode=%s token_count=%d ttl_seconds=%d redis_ready=%t", settings.mode, len(settings.tokenIDs), int(settings.ttl/time.Second), common.RedisEnabled && common.RDB != nil))
	}
}

func loadCyberSessionConfig(getEnv func(string) string) (cyberSessionConfig, error) {
	mode := strings.TrimSpace(getEnv("CYBER_SESSION_GUARD_MODE"))
	switch mode {
	case "", "off":
		return cyberSessionConfig{mode: "off"}, nil
	case cyberSessionShadow, cyberSessionEnforce:
	default:
		return cyberSessionConfig{}, fmt.Errorf("CYBER_SESSION_GUARD_MODE must be off, shadow or enforce")
	}
	settings := cyberSessionConfig{mode: mode, tokenIDs: make(map[int]struct{}), ttl: 10 * time.Minute}
	for item := range strings.SplitSeq(getEnv("CYBER_SESSION_GUARD_TOKEN_IDS"), ",") {
		id, err := strconv.Atoi(strings.TrimSpace(item))
		if err != nil || id <= 0 {
			return cyberSessionConfig{}, fmt.Errorf("CYBER_SESSION_GUARD_TOKEN_IDS must list positive local token IDs")
		}
		settings.tokenIDs[id] = struct{}{}
	}
	if value := strings.TrimSpace(getEnv("CYBER_SESSION_GUARD_TTL_SECONDS")); value != "" {
		seconds, err := strconv.Atoi(value)
		if err != nil || (seconds != 0 && (seconds < 30 || seconds > 3600)) || (seconds == 0 && mode != cyberSessionEnforce) {
			return cyberSessionConfig{}, fmt.Errorf("CYBER_SESSION_GUARD_TTL_SECONDS must be 30-3600, or 0 in enforce mode")
		}
		settings.ttl = time.Duration(seconds) * time.Second
	}
	return settings, nil
}

func CyberSessionGuardMetrics() map[string]any {
	mode := cyberSessionSettings.mode
	if mode == "" {
		mode = "off"
	}
	return map[string]any{
		"mode": mode, "ttl_seconds": int(cyberSessionSettings.ttl / time.Second),
		"permanent":         mode == cyberSessionEnforce && cyberSessionSettings.ttl == 0,
		"configured_tokens": len(cyberSessionSettings.tokenIDs), "redis_ready": common.RedisEnabled && common.RDB != nil,
		"checked": cyberSessionCounters.checked.Load(), "ineligible": cyberSessionCounters.ineligible.Load(),
		"armed": cyberSessionCounters.armed.Load(), "would_block": cyberSessionCounters.wouldBlock.Load(), "blocked": cyberSessionCounters.blocked.Load(),
		"read_error": cyberSessionCounters.readError.Load(), "write_error": cyberSessionCounters.writeError.Load(),
	}
}

// CyberSessionGuard runs after token authentication and before routing/billing.
// For Responses WS the same middleware receives one normalized response.create.
func CyberSessionGuard(c *gin.Context) {
	settings := cyberSessionSettings
	tokenID := c.GetInt("token_id")
	_, enabled := settings.tokenIDs[tokenID]
	if !enabled || c.Request.Method != http.MethodPost || !strings.HasPrefix(c.ContentType(), "application/json") {
		return
	}
	switch c.Request.URL.Path {
	case "/v1/responses", "/v1/responses/compact", "/v1/chat/completions":
	default:
		return
	}
	if !common.RedisEnabled || common.RDB == nil {
		cyberSessionCounters.readError.Add(1)
		return
	}
	if c.Request.ContentLength > cyberSessionCacheLimit {
		cyberSessionCounters.ineligible.Add(1)
		return
	}
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		status := http.StatusBadRequest
		if common.IsRequestBodyTooLargeError(err) {
			status = http.StatusRequestEntityTooLarge
		}
		c.AbortWithStatusJSON(status, gin.H{"error": gin.H{"type": "invalid_request_error", "message": http.StatusText(status)}})
		return
	}
	c.Request.Body = io.NopCloser(storage)
	digest := cyberSessionFingerprint(storage)
	if digest == "" {
		cyberSessionCounters.ineligible.Add(1)
		return
	}
	key := fmt.Sprintf("new-api:cyber-session:v1:%s:%d:%s", settings.mode, tokenID, digest)
	ctx, cancel := context.WithTimeout(c.Request.Context(), cyberSessionRedisBudget)
	remaining, err := common.RDB.PTTL(ctx, key).Result()
	cancel()
	if err != nil {
		cyberSessionCounters.readError.Add(1)
	} else {
		cyberSessionCounters.checked.Add(1)
		if remaining >= 0 || remaining == cyberSessionNoExpiry {
			if settings.mode == cyberSessionEnforce {
				cyberSessionCounters.blocked.Add(1)
				permanent := remaining == cyberSessionNoExpiry
				c.Set(cyberSessionLogKey, gin.H{"mode": settings.mode, "action": "blocked", "session_fingerprint": digest, "permanent": permanent})
				message := "This conversation is temporarily blocked after an upstream content policy rejection"
				if permanent {
					message = "This conversation is blocked after an upstream content policy rejection"
				} else {
					c.Header("Retry-After", strconv.FormatInt(max(1, int64((remaining+time.Second-1)/time.Second)), 10))
				}
				apiErr := types.WithOpenAIError(types.OpenAIError{
					Type: "content_policy_violation", Code: CyberSessionBlockedCode,
					Message: message,
				}, http.StatusForbidden, types.ErrOptionWithSkipRetry())
				SetRelayFinalError(c, apiErr)
				c.AbortWithStatusJSON(apiErr.StatusCode, gin.H{"error": apiErr.ToOpenAIError()})
				return
			}
			cyberSessionCounters.wouldBlock.Add(1)
			logger.LogInfo(c, "cyber_session_guard mode=shadow decision=would_block")
		}
	}
	arm := sync.OnceFunc(func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), cyberSessionRedisBudget)
		defer cancel()
		// Repeated matches, including shadow-mode requests, do not extend the cooldown.
		created, err := common.RDB.SetNX(ctx, key, "1", settings.ttl).Result()
		action := "already_active"
		if err != nil {
			action = "store_failed"
			if cyberSessionCounters.writeError.Add(1) == 1 {
				logger.LogWarn(c, "cyber_session_guard could not store a cooldown; check Redis and guard metrics")
			}
		} else if created {
			action = "armed"
			cyberSessionCounters.armed.Add(1)
		}
		c.Set(cyberSessionLogKey, gin.H{"mode": settings.mode, "action": action, "session_fingerprint": digest, "configured_ttl_seconds": int(settings.ttl / time.Second)})
	})
	c.Set(cyberSessionArmKey, arm)
	c.Next()
	if failure, failed := FinalRelayFailure(c); failed && failure.Code == relaycommon.CyberPolicyCode {
		arm()
	}
}

func cyberSessionFingerprint(storage common.BodyStorage) string {
	if storage.Size() > cyberSessionCacheLimit {
		return ""
	}
	reader, err := storage.NewReader()
	if err != nil {
		return ""
	}
	defer reader.Close()
	var request struct {
		Key string `json:"prompt_cache_key"`
	}
	if common.DecodeJson(reader, &request) != nil || len(request.Key) != 36 {
		return ""
	}
	id, err := uuid.Parse(request.Key)
	if err != nil || (id.Version() != 4 && id.Version() != 7) || id.Variant() != uuid.RFC4122 || id.String() != request.Key {
		return ""
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(request.Key)))
}
