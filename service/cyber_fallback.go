package service

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
)

const cyberFallbackKey = "cyber_fallback"
const cyberFallbackBansKey = "cyber_fallback_bans"
const cyberFallbackLimit = 64

// A binding belongs to one physical WebSocket, never to a new HTTP request.
// Its fields are private so callers cannot construct a cross-channel allowance.
type CyberFallbackBinding struct {
	session   cyberRequestSession
	ban       cyberFallbackBan
	channelID int
}

type cyberFallbackContextKey struct{}

func WithCyberFallbackBinding(request *http.Request, binding *CyberFallbackBinding) *http.Request {
	if binding == nil {
		return request
	}
	return request.WithContext(context.WithValue(request.Context(), cyberFallbackContextKey{}, binding))
}

func CyberFallbackConnectionBinding(c *gin.Context) *CyberFallbackBinding {
	route := cyberFallback(c)
	if route == nil {
		return nil
	}
	session, _ := c.Get(cyberSessionRequestKey)
	return &CyberFallbackBinding{session: session.(cyberRequestSession), ban: route.ban, channelID: route.TargetChannelID}
}

type cyberFallbackBan struct {
	key, raw    string
	meta        CyberBanMetadata
	ttl         int64
	denied      bool
	unavailable bool
}

type cyberFallbackRoute struct {
	BatchID         string `json:"batch_id"`
	Fingerprint     string `json:"session_fingerprint"`
	SourceChannelID int    `json:"source_channel_id"`
	TargetChannelID int    `json:"target_channel_id"`
	ActualChannelID int    `json:"actual_channel_id"`
	Group           string `json:"group"`
	Action          string `json:"action"`
	Outcome         string `json:"outcome"`
	Reason          string `json:"reason"`
	RequestID       string `json:"request_id"`
	ban             cyberFallbackBan
}

var cyberFallbackCounters struct{ selected, rejected, stored, logError atomic.Uint64 }

func cyberFallback(c *gin.Context) *cyberFallbackRoute {
	value, _ := c.Get(cyberFallbackKey)
	route, _ := value.(*cyberFallbackRoute)
	return route
}

func IsCyberFallback(c *gin.Context) bool { return cyberFallback(c) != nil }

func cyberFallbackSetKey(parent, batch string) string {
	return fmt.Sprintf("new-api:cyber-fallback:v1:%x:%s", sha256.Sum256([]byte(parent)), batch)
}

// Read the bounded policy scope once, before the distributor takes its group
// mutation lock. Each parent value, TTL and rejection set is read atomically.
// Old values receive a new batch ID without extending the parent's lifetime.
var readCyberFallbackBans = redis.NewScript(`
local result = {}
for i, key in ipairs(KEYS) do
 local raw = redis.pcall('GET', key)
 if type(raw) == 'table' and raw.err then
  table.insert(result, key)
  table.insert(result, '')
  table.insert(result, redis.call('PTTL', key))
  table.insert(result, -1)
 elseif raw then
  local ttl = redis.call('PTTL', key)
  local valid, meta = pcall(cjson.decode, raw)
  if not valid or type(meta) ~= 'table' then meta = {} end
  if type(meta.id) ~= 'string' or meta.id == '' or string.len(meta.id) > 64 then
   meta.id = ARGV[1]
   raw = cjson.encode(meta)
   redis.call('SET', key, raw)
   if ttl >= 0 then redis.call('PEXPIRE', key, ttl) end
  end
  local child = ARGV[i + 2] .. tostring(meta.id)
  local denied = 0
  local count = redis.pcall('SCARD', child)
  local member = redis.pcall('SISMEMBER', child, ARGV[2])
  if type(count) ~= 'number' or type(member) ~= 'number' then denied = -1
  elseif count >= 64 or member == 1 then denied = 1 end
  table.insert(result, key)
  table.insert(result, raw)
  table.insert(result, ttl)
  table.insert(result, denied)
 end
end
return result
`)

func prepareCyberFallbackBans(c *gin.Context, session cyberRequestSession, binding *CyberFallbackBinding) *types.NewAPIError {
	if !common.RedisEnabled || common.RDB == nil {
		return cyberFallbackError(c, "state_unavailable", http.StatusServiceUnavailable)
	}
	keys := []string{session.redisKey(0)}
	if session.policy.Scope == "channel" {
		for _, id := range session.policy.ChannelIDs {
			keys = append(keys, session.redisKey(id))
		}
	}
	if binding != nil && !slices.Contains(keys, binding.ban.key) {
		keys = append(keys, binding.ban.key)
	}
	args := []any{uuid.NewString(), session.policy.FallbackChannelID}
	for _, key := range keys {
		args = append(args, cyberFallbackSetKey(key, ""))
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), cyberSessionRedisBudget)
	defer cancel()
	values, err := readCyberFallbackBans.Run(ctx, common.RDB, keys, args...).Slice()
	if err != nil {
		cyberSessionCounters.readError.Add(1)
		// An established fallback connection already made a confirmed decision.
		if binding != nil {
			return cyberFallbackError(c, "state_unavailable", http.StatusServiceUnavailable)
		}
		c.Set(cyberSessionSkipKey, "redis_unavailable")
		return nil
	}
	bans := make(map[string]cyberFallbackBan, len(values)/4)
	for i := 0; i+3 < len(values); i += 4 {
		ban := cyberFallbackBan{key: values[i].(string), raw: values[i+1].(string), ttl: values[i+2].(int64), denied: values[i+3].(int64) == 1, unavailable: values[i+3].(int64) == -1}
		if common.UnmarshalJsonStr(ban.raw, &ban.meta) != nil || ban.meta.ID == "" {
			ban.unavailable = true
		}
		bans[ban.key] = ban
	}
	cyberSessionCounters.checked.Add(1)
	c.Set(cyberFallbackBansKey, bans)
	if binding != nil {
		ban, exists := bans[binding.ban.key]
		if !exists {
			ban = binding.ban
		}
		// A new batch may exist under the current scope after an old one expired.
		if current, ok := bans[session.redisKey(0)]; ok {
			ban = current
		}
		return activateCyberFallback(c, session, ban)
	}
	return nil
}

func activateCyberFallback(c *gin.Context, session cyberRequestSession, ban cyberFallbackBan) *types.NewAPIError {
	if cyberFallback(c) != nil {
		return nil
	}
	route := &cyberFallbackRoute{BatchID: ban.meta.ID, Fingerprint: session.digest, SourceChannelID: ban.meta.SourceChannelID,
		TargetChannelID: session.policy.FallbackChannelID, Action: "reroute", Outcome: "selected", Reason: "existing_ban", RequestID: c.GetString(common.RequestIdKey), ban: ban}
	c.Set(cyberFallbackKey, route)
	if ban.unavailable {
		return cyberFallbackError(c, "state_unavailable", http.StatusServiceUnavailable)
	}
	value, _ := c.Get(cyberFallbackBansKey)
	bans, _ := value.(map[string]cyberFallbackBan)
	if targetBan, exists := bans[session.redisKey(route.TargetChannelID)]; exists && targetBan.key != ban.key {
		return cyberFallbackError(c, "target_rejected", http.StatusForbidden)
	}
	if ban.denied {
		return cyberFallbackError(c, "target_rejected", http.StatusForbidden)
	}
	if route.SourceChannelID > 0 && route.SourceChannelID == route.TargetChannelID {
		return cyberFallbackError(c, "same_source", http.StatusForbidden)
	}

	GetChannelConstraints(c).AddFilter(dto.ChannelFilter{Kind: dto.FilterAllowedChannelIDs, AllowedChannelIDs: []int{route.TargetChannelID}})
	cyberFallbackCounters.selected.Add(1)
	return nil
}

func cyberFallbackError(c *gin.Context, reason string, status int) *types.NewAPIError {
	if cyberFallback(c) == nil {
		if binding, _ := c.Request.Context().Value(cyberFallbackContextKey{}).(*CyberFallbackBinding); binding != nil {
			c.Set(cyberFallbackKey, &cyberFallbackRoute{BatchID: binding.ban.meta.ID, Fingerprint: binding.session.digest, SourceChannelID: binding.ban.meta.SourceChannelID, TargetChannelID: binding.channelID, Action: "reroute", RequestID: c.GetString(common.RequestIdKey), ban: binding.ban})
		}
	}
	if route := cyberFallback(c); route != nil {
		route.Outcome, route.Reason = "rejected", reason
	}
	cyberFallbackCounters.rejected.Add(1)
	apiErr := types.WithOpenAIError(types.OpenAIError{Type: "invalid_request_error", Code: "cyber_fallback_" + reason,
		Message: "This conversation cannot use the configured fallback channel (" + reason + "); contact an administrator"}, status, types.ErrOptionWithSkipRetry())
	// Keep the routing reason for retry decisions and audit attribution; clients
	// receive the standard error that matches the actual failure.
	apiErr.RelayError = CyberClientError(string(apiErr.GetErrorCode()), status)
	return apiErr
}

// CyberClientError maps an internal gateway block or fallback routing failure to
// the client-facing error. Only content policy outcomes surface as cyber_policy;
// infrastructure and request-shape failures keep standard OpenAI semantics.
func CyberClientError(code string, status int) types.OpenAIError {
	switch strings.TrimPrefix(code, "cyber_fallback_") {
	case "target_rejected", "same_source":
		return CyberPolicyError(status).ToOpenAIError()
	case "model_forbidden":
		return types.OpenAIError{Type: "invalid_request_error", Code: "permission_denied",
			Message: "This model is not available on the configured fallback channel"}
	case "binding_conflict", "reconnect_required", "stateful_context":
		return types.OpenAIError{Type: "invalid_request_error", Code: "invalid_request",
			Message: "This conversation cannot use the configured fallback channel; reconnect or contact an administrator"}
	case "state_unavailable", "target_unavailable":
		return types.OpenAIError{Type: "server_error", Code: "server_error",
			Message: "The configured fallback channel is temporarily unavailable"}
	}
	return CyberPolicyError(status).ToOpenAIError()
}

// Validate every hard pin, including pins hidden by normal priority resolution.
func ValidateCyberFallbackChannel(c *gin.Context, channelID int) *types.NewAPIError {
	route := cyberFallback(c)
	if route == nil {
		return nil
	}
	for _, pin := range GetChannelConstraints(c).Pins {
		if pin.ChannelId != route.TargetChannelID {
			return cyberFallbackError(c, "binding_conflict", http.StatusConflict)
		}
	}
	if channelID != route.TargetChannelID {
		return cyberFallbackError(c, "reconnect_required", http.StatusConflict)
	}
	return nil
}

func selectCyberFallbackChannel(c *gin.Context, modelName string, retry *RetryParam) (*model.Channel, string, *ChannelSelectError) {
	route := cyberFallback(c)
	var apiErr *types.NewAPIError
	if common.GetContextKeyBool(c, constant.ContextKeyTokenModelLimitEnabled) {
		limit, _ := common.GetContextKeyType[map[string]bool](c, constant.ContextKeyTokenModelLimit)
		if !limit[modelName] && !limit[ratio_setting.FormatMatchingModelName(modelName)] && !limit[ratio_setting.RoutingMatchModelName(modelName)] {
			return nil, "", cyberChannelSelectError(cyberFallbackError(c, "model_forbidden", http.StatusForbidden))
		}
	}
	if apiErr = ValidateCyberFallbackChannel(c, route.TargetChannelID); apiErr == nil && retry.GetRetry() == 0 {
		preferred, found := GetPreferredChannelByAffinity(c, modelName, retry.TokenGroup)
		if found && RequestPolicy(c).SessionMode == "strict" && preferred != route.TargetChannelID {
			apiErr = cyberFallbackError(c, "binding_conflict", http.StatusConflict)
		}
	}
	if apiErr != nil {
		return nil, "", cyberChannelSelectError(apiErr)
	}
	// A token pin must not bypass the original group/model pool on rerouting.
	ch, group, err := cacheGetRandomSatisfiedChannel(retry)
	if err != nil || ch == nil {
		return nil, group, cyberChannelSelectError(cyberFallbackError(c, "target_unavailable", http.StatusServiceUnavailable))
	}
	route.Group = group
	return ch, group, nil
}

func cyberChannelSelectError(err *types.NewAPIError) *ChannelSelectError {
	return &ChannelSelectError{StatusCode: err.StatusCode, Code: err.GetErrorCode(), Message: err.Error(), APIError: err}
}

var rejectCyberFallbackScript = redis.NewScript(`
if redis.call('GET', KEYS[1]) ~= ARGV[1] then return 0 end
local ttl = redis.call('PTTL', KEYS[1])
if redis.call('SCARD', KEYS[2]) < 64 then redis.call('SADD', KEYS[2], ARGV[2]) end
if ttl >= 0 then redis.call('PEXPIRE', KEYS[2], ttl) end
return 1
`)

func rejectCyberFallback(c *gin.Context, route *cyberFallbackRoute) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), cyberSessionRedisBudget)
	defer cancel()
	_, err := rejectCyberFallbackScript.Run(ctx, common.RDB, []string{route.ban.key, cyberFallbackSetKey(route.ban.key, route.BatchID)}, route.ban.raw, strconv.Itoa(route.TargetChannelID)).Result()
	route.Outcome, route.Reason = "failed", "upstream_cyber"
	if err != nil {
		cyberSessionCounters.writeError.Add(1)
		logger.LogWarn(c, "cyber_fallback rejection state write failed")
	}
}

// Expose aggregate diagnostics without the private Redis key or raw ban value.
func CyberFallbackMetrics() map[string]uint64 {
	return map[string]uint64{"selected": cyberFallbackCounters.selected.Load(), "rejected": cyberFallbackCounters.rejected.Load(), "stored": cyberFallbackCounters.stored.Load(), "log_error": cyberFallbackCounters.logError.Load()}
}

func MarkCyberFallbackOutcome(c *gin.Context, outcome string) {
	if route := cyberFallback(c); route != nil {
		route.Outcome = outcome
		if outcome == "failed" {
			SetRelayFinalError(c, types.NewErrorWithStatusCode(fmt.Errorf("fallback upstream response failed"), types.ErrorCodeBadResponse, http.StatusBadGateway, types.ErrOptionWithSkipRetry()))
		}
	}
}

func IsCyberFallbackError(code string) bool { return strings.HasPrefix(code, "cyber_fallback_") }

// Task acceptance is distinct from successful completion of the upstream task.
func MarkCyberFallbackSubmitted(c *gin.Context, completed bool) {
	if route := cyberFallback(c); route != nil {
		route.Outcome = "submitted"
		if completed {
			route.Outcome = "completed"
		}
	}
}

func appendCyberFallbackInfo(c *gin.Context, other *model.LogOther) {
	if route := cyberFallback(c); route != nil {
		snapshot := *route
		snapshot.Outcome = cyberFallbackFinalOutcome(c, route)
		other.SetAdmin("cyber_fallback", snapshot)
	}
}

func cyberFallbackFinalOutcome(c *gin.Context, route *cyberFallbackRoute) string {
	outcome := route.Outcome
	if outcome == "selected" && RequestPolicy(c).Successful {
		outcome = "completed"
	}
	if outcome == "submitted" || outcome == "failed" || outcome == "rejected" {
		return outcome
	}
	value, _ := common.GetContextKey(c, constant.ContextKeyResponseStreamStatus)
	stream, _ := value.(*relaycommon.StreamStatus)
	response := stream.OutcomeSnapshot()
	if c.Request.Context().Err() != nil || response.Response == relaycommon.ResponseOutcomeIncomplete || response.Response == relaycommon.ResponseOutcomeCancelled || response.EndReason == relaycommon.StreamEndReasonClientGone {
		outcome = "interrupted"
	}
	return outcome
}

func recordCyberFallbackOutcome(c *gin.Context, failed bool) {
	route := cyberFallback(c)
	if route == nil {
		return
	}
	if failed {
		if route.Outcome != "rejected" {
			route.Outcome = "failed"
		}
		return
	}
	route.Outcome = cyberFallbackFinalOutcome(c, route)
	other := model.NewLogOther()
	other.SetPublic("protocol", c.GetString(relayFailureProtocolKey))
	appendCyberFallbackInfo(c, other)
	if err := model.RecordCyberFallbackLog(c, other); err != nil {
		cyberFallbackCounters.logError.Add(1)
		logger.LogError(c, "cyber_fallback outcome log write failed")
	} else {
		cyberFallbackCounters.stored.Add(1)
	}
}
