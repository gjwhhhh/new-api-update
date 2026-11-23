package service

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tidwall/gjson"
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

type CyberPolicy = operation_setting.CyberPolicy

type cyberPolicySnapshot struct {
	raw    string
	policy CyberPolicy
}

var cyberSessionDefaults atomic.Pointer[CyberPolicy]
var cyberSessionSnapshot atomic.Pointer[cyberPolicySnapshot]

func CurrentCyberPolicy() CyberPolicy {
	common.OptionMapRWMutex.RLock()
	raw := common.OptionMap[operation_setting.CyberPolicyOptionKey]
	common.OptionMapRWMutex.RUnlock()
	if snapshot := cyberSessionSnapshot.Load(); snapshot != nil && snapshot.raw == raw {
		return snapshot.policy
	}
	var policy CyberPolicy
	decoded := common.UnmarshalJsonStr(raw, &policy) == nil
	if decoded {
		policy.Normalize()
	}
	if raw != "" && decoded && policy.Validate() == nil {
		cyberSessionSnapshot.Store(&cyberPolicySnapshot{raw: raw, policy: policy})
		return policy
	}
	if snapshot := cyberSessionSnapshot.Load(); raw != "" && snapshot != nil {
		return snapshot.policy
	}
	if fallback := cyberSessionDefaults.Load(); fallback != nil {
		return *fallback
	}
	return CyberPolicy{Mode: "off", Scope: "token", TTLSeconds: 600, BlockedAction: "block", CaptureFullPayload: common.GetPointer(true)}
}

var cyberSessionCounters struct {
	checked, ineligible, armed, wouldBlock, blocked, readError, writeError atomic.Uint64
}

func InitCyberSessionGuard() {
	settings, err := loadCyberSessionConfig(os.Getenv)
	if err != nil {
		settings = CyberPolicy{Mode: "off", Scope: "token", TTLSeconds: 600}
		common.SysError("cyber_session_guard disabled: " + err.Error())
	}
	settings.Normalize()
	cyberSessionDefaults.Store(&settings)
	settings = CurrentCyberPolicy()
	if settings.Mode != "off" {
		common.SysLog(fmt.Sprintf("cyber_session_guard mode=%s scope=%s ttl_seconds=%d redis_ready=%t", settings.Mode, settings.Scope, settings.TTLSeconds, common.RedisEnabled && common.RDB != nil))
	}
}

func loadCyberSessionConfig(getEnv func(string) string) (CyberPolicy, error) {
	p := CyberPolicy{Mode: strings.TrimSpace(getEnv("CYBER_SESSION_GUARD_MODE")), Scope: strings.TrimSpace(getEnv("CYBER_SESSION_GUARD_SCOPE")), TTLSeconds: 600}
	if p.Mode == "" {
		p.Mode = "off"
	}
	if p.Scope == "" {
		p.Scope = "token"
	}
	for key, list := range map[string]*[]int{"CYBER_SESSION_GUARD_TOKEN_IDS": &p.TokenIDs, "CYBER_SESSION_GUARD_CHANNEL_IDS": &p.ChannelIDs} {
		for item := range strings.SplitSeq(strings.TrimSpace(getEnv(key)), ",") {
			if item == "" {
				continue
			}
			id, err := strconv.Atoi(strings.TrimSpace(item))
			if err != nil {
				return p, fmt.Errorf("invalid %s", key)
			}
			*list = append(*list, id)
		}
	}
	if value := strings.TrimSpace(getEnv("CYBER_SESSION_GUARD_TTL_SECONDS")); value != "" {
		seconds, err := strconv.Atoi(value)
		if err != nil {
			return p, fmt.Errorf("invalid CYBER_SESSION_GUARD_TTL_SECONDS")
		}
		p.TTLSeconds = seconds
	}
	if raw := strings.TrimSpace(getEnv("CYBER_SESSION_GUARD_MATCH_MESSAGE")); raw != "" {
		on, err := strconv.ParseBool(raw)
		if err != nil {
			return p, fmt.Errorf("invalid CYBER_SESSION_GUARD_MATCH_MESSAGE")
		}
		p.MatchMessage = on
	}
	p.Normalize()
	return p, p.Validate()
}

func CyberSessionGuardMetrics() map[string]any {
	p := CurrentCyberPolicy()
	return map[string]any{
		"mode": p.Mode, "scope": p.Scope, "ttl_seconds": p.TTLSeconds, "fallback": CyberFallbackMetrics(),
		"permanent":         p.Mode == cyberSessionEnforce && p.TTLSeconds == 0,
		"configured_tokens": len(p.TokenIDs), "redis_ready": common.RedisEnabled && common.RDB != nil,
		"checked": cyberSessionCounters.checked.Load(), "ineligible": cyberSessionCounters.ineligible.Load(),
		"armed": cyberSessionCounters.armed.Load(), "would_block": cyberSessionCounters.wouldBlock.Load(), "blocked": cyberSessionCounters.blocked.Load(),
		"read_error": cyberSessionCounters.readError.Load(), "write_error": cyberSessionCounters.writeError.Load(),
	}
}

// CyberSessionGuard runs after token authentication and before routing/billing.
// For Responses WS the same middleware receives one normalized response.create.
func CyberSessionGuard(c *gin.Context) {
	settings := CurrentCyberPolicy()
	binding, _ := c.Request.Context().Value(cyberFallbackContextKey{}).(*CyberFallbackBinding)
	bindingOutOfScope := false
	if binding != nil && settings.Scope == "channel" {
		target, _ := parseCyberBanKey(binding.ban.key)
		originalChannel := target.ChannelID
		if originalChannel == 0 {
			originalChannel = binding.ban.meta.SourceChannelID
		}
		bindingOutOfScope = !slices.Contains(settings.ChannelIDs, originalChannel)
	}
	if binding != nil && (settings.Mode != cyberSessionEnforce || settings.BlockedAction != "reroute" || settings.FallbackChannelID != binding.channelID || c.GetInt("token_id") != binding.session.tokenID || (settings.Scope == "token" && !slices.Contains(settings.TokenIDs, binding.session.tokenID)) || bindingOutOfScope) {
		apiErr := cyberFallbackError(c, "reconnect_required", http.StatusConflict)
		SetRelayFinalError(c, apiErr)
		c.AbortWithStatusJSON(apiErr.StatusCode, gin.H{"error": apiErr.ToOpenAIError()})
		return
	}
	c.Set(cyberMessagePolicyKey, settings.MatchMessage)
	c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), cyberUpstreamContextKey{}, c))
	tokenID := c.GetInt("token_id")
	if settings.Mode == "off" {
		c.Set(cyberSessionSkipKey, "guard_off")
		return
	}
	if tokenID <= 0 || (settings.Scope == "token" && !slices.Contains(settings.TokenIDs, tokenID)) {
		c.Set(cyberSessionSkipKey, "out_of_scope")
		return
	}
	if c.Request.Method != http.MethodPost || !strings.HasPrefix(c.ContentType(), "application/json") {
		c.Set(cyberSessionSkipKey, "unsupported_request")
		return
	}
	switch c.Request.URL.Path {
	case "/v1/responses", "/v1/responses/compact", "/v1/chat/completions":
	default:
		c.Set(cyberSessionSkipKey, "unsupported_request")
		return
	}
	if (!common.RedisEnabled || common.RDB == nil) && binding == nil {
		c.Set(cyberSessionSkipKey, "redis_unavailable")
		cyberSessionCounters.readError.Add(1)
		return
	}
	if binding != nil && (!common.RedisEnabled || common.RDB == nil || c.Request.ContentLength > cyberSessionCacheLimit) {
		apiErr := cyberFallbackError(c, "state_unavailable", http.StatusServiceUnavailable)
		SetRelayFinalError(c, apiErr)
		c.AbortWithStatusJSON(apiErr.StatusCode, gin.H{"error": apiErr.ToOpenAIError()})
		return
	}
	if c.Request.ContentLength > cyberSessionCacheLimit {
		c.Set(cyberSessionSkipKey, "request_too_large")
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
	if binding != nil {
		body, readErr := storage.Bytes()
		invalidSession := readErr != nil || digest == "" && gjson.GetBytes(body, "prompt_cache_key").Exists()
		if invalidSession || digest != "" && digest != binding.session.digest {
			apiErr := cyberFallbackError(c, "reconnect_required", http.StatusConflict)
			SetRelayFinalError(c, apiErr)
			c.AbortWithStatusJSON(apiErr.StatusCode, gin.H{"error": apiErr.ToOpenAIError()})
			return
		}
		digest = binding.session.digest
	}
	if digest == "" {
		c.Set(cyberSessionSkipKey, "missing_session")
		cyberSessionCounters.ineligible.Add(1)
		return
	}
	session := cyberRequestSession{policy: settings, tokenID: tokenID, digest: digest}
	c.Set(cyberSessionRequestKey, session)
	if settings.Mode == cyberSessionEnforce && settings.BlockedAction == "reroute" {
		if apiErr := prepareCyberFallbackBans(c, session, binding); apiErr != nil {
			SetRelayFinalError(c, apiErr)
			c.AbortWithStatusJSON(apiErr.StatusCode, gin.H{"error": apiErr.ToOpenAIError()})
			return
		}
	}
	if settings.Scope != "channel" {
		if apiErr := checkCyberSession(c, session, 0); apiErr != nil {
			SetRelayFinalError(c, apiErr)
			c.AbortWithStatusJSON(apiErr.StatusCode, gin.H{"error": apiErr.ToOpenAIError()})
			return
		}
	}
	arm := sync.OnceFunc(func() { writeCyberSession(c, session) })
	c.Set(cyberSessionArmKey, arm)
	c.Next()
	if failure, failed := FinalRelayFailure(c); failed && failure.Code == relaycommon.CyberPolicyCode {
		arm()
	}
}

const cyberSessionSkipKey = "cyber_session_skip"
const cyberSessionRequestKey = "cyber_session_request"
const cyberSessionPrefix = "new-api:cyber-session:v1:"

type cyberRequestSession struct {
	policy  CyberPolicy
	tokenID int
	digest  string
}

func (s cyberRequestSession) redisKey(channelID int) string {
	if channelID > 0 {
		return fmt.Sprintf("%s%s:channel:%d:%d:%s", cyberSessionPrefix, s.policy.Mode, channelID, s.tokenID, s.digest)
	}
	return fmt.Sprintf("%s%s:%d:%s", cyberSessionPrefix, s.policy.Mode, s.tokenID, s.digest)
}

func CheckCyberSessionChannel(c *gin.Context, channelID int) *types.NewAPIError {
	if IsCyberFallback(c) {
		return ValidateCyberFallbackChannel(c, channelID)
	}
	value, _ := c.Get(cyberSessionRequestKey)
	session, ok := value.(cyberRequestSession)
	if !ok || session.policy.Scope != "channel" || !slices.Contains(session.policy.ChannelIDs, channelID) {
		return nil
	}
	return checkCyberSession(c, session, channelID)
}

func checkCyberSession(c *gin.Context, session cyberRequestSession, channelID int) *types.NewAPIError {
	if session.policy.Mode == cyberSessionEnforce && session.policy.BlockedAction == "reroute" {
		if IsCyberFallback(c) {
			return nil
		}
		value, _ := c.Get(cyberFallbackBansKey)
		bans, _ := value.(map[string]cyberFallbackBan)
		ban, found := bans[session.redisKey(channelID)]
		if !found && channelID > 0 {
			ban, found = bans[session.redisKey(0)]
		}
		if found {
			return activateCyberFallback(c, session, ban)
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), cyberSessionRedisBudget)
	remaining, err := common.RDB.PTTL(ctx, session.redisKey(channelID)).Result()
	if err == nil && remaining < 0 && remaining != cyberSessionNoExpiry && channelID > 0 {
		remaining, err = common.RDB.PTTL(ctx, session.redisKey(0)).Result()
	}
	cancel()
	if err != nil {
		cyberSessionCounters.readError.Add(1)
	} else {
		cyberSessionCounters.checked.Add(1)
		if remaining >= 0 || remaining == cyberSessionNoExpiry {
			if session.policy.Mode == cyberSessionEnforce {
				cyberSessionCounters.blocked.Add(1)
				if channelID > 0 {
					// Selection may reject this candidate before channel setup runs.
					common.SetContextKey(c, constant.ContextKeyChannelId, channelID)
				}
				permanent := remaining == cyberSessionNoExpiry
				c.Set(cyberSessionLogKey, gin.H{"mode": session.policy.Mode, "action": "blocked", "session_fingerprint": session.digest, "permanent": permanent, "channel_id": channelID})
				message := "This conversation is temporarily blocked after an upstream content policy rejection"
				if permanent {
					message = "This conversation is blocked after an upstream content policy rejection"
				} else {
					c.Header("Retry-After", strconv.FormatInt(max(1, int64((remaining+time.Second-1)/time.Second)), 10))
				}
				clientError := types.OpenAIError{
					Type: "invalid_request", Code: CyberSessionBlockedCode,
					Message: message,
				}
				apiErr := types.WithOpenAIError(clientError, http.StatusForbidden, types.ErrOptionWithSkipRetry())
				// Keep gateway blocks distinct from upstream rejections in relay handling and logs.
				clientError.Code = relaycommon.CyberPolicyCode
				apiErr.RelayError = clientError
				return apiErr
			}
			cyberSessionCounters.wouldBlock.Add(1)
			logger.LogInfo(c, "cyber_session_guard mode=shadow decision=would_block")
		}
	}
	return nil
}

func writeCyberSession(c *gin.Context, session cyberRequestSession) {
	if route := cyberFallback(c); route != nil {
		rejectCyberFallback(c, route)
		return
	}
	settings := session.policy
	channelID := 0
	if settings.Scope == "channel" {
		channelID = c.GetInt("channel_id")
		if !slices.Contains(settings.ChannelIDs, channelID) {
			c.Set(cyberSessionSkipKey, "out_of_scope")
			return
		}
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), cyberSessionRedisBudget)
	defer cancel()
	metadata, err := common.Marshal(CyberBanMetadata{ID: uuid.NewString(), RequestID: c.GetString(common.RequestIdKey), CreatedAt: time.Now().Unix(), SourceChannelID: c.GetInt("channel_id")})
	if err != nil {
		c.Set(cyberSessionSkipKey, "store_failed")
		return
	}
	created, err := common.RDB.SetNX(ctx, session.redisKey(channelID), metadata, time.Duration(settings.TTLSeconds)*time.Second).Result()
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
	c.Set(cyberSessionLogKey, gin.H{"mode": settings.Mode, "action": action, "session_fingerprint": session.digest, "configured_ttl_seconds": settings.TTLSeconds, "channel_id": channelID})
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
