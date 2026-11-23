package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
)

const (
	relayFailureLogActiveKey = "relay_failure_log_active"
	relayFinalErrorKey       = "relay_final_error"
	relayFailureProtocolKey  = "relay_failure_protocol"
	relayAuditFrameKey       = "relay_audit_frame"
)

var relayErrorIdentifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,95}$`)

type relayFailureLogCounters struct {
	cyber   atomic.Uint64
	stored  atomic.Uint64
	failed  atomic.Uint64
	ignored atomic.Uint64
}

var relayFailureLogMetrics = map[string]*relayFailureLogCounters{
	"http": {}, "sse": {}, "websocket": {},
}

type RelayFailureLogCounts struct {
	Cyber      uint64 `json:"cyber"`
	Stored     uint64 `json:"stored"`
	WriteError uint64 `json:"write_error"`
	Disabled   uint64 `json:"disabled"`
}

func RelayFailureLogMetrics() map[string]RelayFailureLogCounts {
	result := make(map[string]RelayFailureLogCounts, len(relayFailureLogMetrics))
	for protocol, counters := range relayFailureLogMetrics {
		result[protocol] = RelayFailureLogCounts{
			Cyber: counters.cyber.Load(), Stored: counters.stored.Load(),
			WriteError: counters.failed.Load(), Disabled: counters.ignored.Load(),
		}
	}
	return result
}

type RelayFailure struct {
	Code        string
	Type        string
	ErrorStatus int
	Protocol    string
}

func SetRelayFinalError(c *gin.Context, err *types.NewAPIError) {
	if err != nil {
		c.Set(relayFinalErrorKey, err)
		if err.GetErrorCode() == relaycommon.CyberPolicyCode {
			if arm, ok := c.Get(cyberSessionArmKey); ok {
				arm.(func())()
			}
		}
	}
}

func GetRelayFinalError(c *gin.Context) *types.NewAPIError {
	value, _ := c.Get(relayFinalErrorKey)
	apiErr, _ := value.(*types.NewAPIError)
	return apiErr
}

func SetRelayAuditFrameObserver(c *gin.Context, observer func([]byte)) {
	c.Set(relayAuditFrameKey, observer)
}

func ObserveRelayAuditFrame(c *gin.Context, frame []byte) {
	if value, ok := c.Get(relayAuditFrameKey); ok {
		if observer, ok := value.(func([]byte)); ok && observer != nil {
			observer(frame)
		}
	}
}

// FinalRelayFailure uses the upstream stream outcome before the HTTP status,
// since a terminal failure can arrive after the response has been committed.
func FinalRelayFailure(c *gin.Context) (RelayFailure, bool) {
	apiErr := GetRelayFinalError(c)
	value, _ := common.GetContextKey(c, constant.ContextKeyResponseStreamStatus)
	stream, _ := value.(*relaycommon.StreamStatus)
	outcome := stream.OutcomeSnapshot()
	failure := RelayFailure{Protocol: c.GetString(relayFailureProtocolKey)}
	if failure.Protocol == "" {
		failure.Protocol = "http"
		if stream != nil || common.GetContextKeyBool(c, constant.ContextKeyIsStream) {
			failure.Protocol = "sse"
		}
	}
	switch {
	case outcome.Response == relaycommon.ResponseOutcomeFailed && outcome.ErrorCode == relaycommon.CyberPolicyCode:
		failure.Code, failure.Type, failure.ErrorStatus = outcome.ErrorCode, "content_policy_violation", outcome.ErrorStatus
	case apiErr != nil && !errors.Is(apiErr, context.Canceled):
		failure.Code, failure.Type, failure.ErrorStatus = string(apiErr.GetErrorCode()), string(apiErr.GetErrorType()), apiErr.StatusCode
	case outcome.Response == relaycommon.ResponseOutcomeFailed:
		failure.Code, failure.Type, failure.ErrorStatus = outcome.ErrorCode, outcome.ErrorType, outcome.ErrorStatus
	case outcome.Response == relaycommon.ResponseOutcomeCancelled, outcome.EndReason == relaycommon.StreamEndReasonClientGone:
		return RelayFailure{}, false
	case c.Writer.Status() >= http.StatusBadRequest:
		failure.Code, failure.ErrorStatus = "http_error", c.Writer.Status()
	case outcome.HasErrors, outcome.EndReason == relaycommon.StreamEndReasonTimeout, outcome.EndReason == relaycommon.StreamEndReasonScannerErr, outcome.EndReason == relaycommon.StreamEndReasonPanic,
		outcome.ExpectsTerminal && outcome.Response == relaycommon.ResponseOutcomeUnknown && outcome.EndReason == relaycommon.StreamEndReasonEOF:
		failure.Code = string(types.ErrorCodeChannelUpstreamStreamTerminated)
	default:
		return RelayFailure{}, false
	}
	if !relayErrorIdentifier.MatchString(failure.Code) {
		failure.Code = "relay_failed"
	}
	if !relayErrorIdentifier.MatchString(failure.Type) {
		failure.Type = "relay_error"
	}
	return failure, true
}

func RelayFailureLog(protocol string) gin.HandlerFunc {
	if protocol != "websocket" {
		protocol = ""
	}
	return func(c *gin.Context) {
		if c.GetBool(relayFailureLogActiveKey) {
			c.Next()
			return
		}
		c.Set(relayFailureLogActiveKey, true)
		c.Set(relayFailureProtocolKey, protocol)
		c.Next()

		failure, failed := FinalRelayFailure(c)
		recordCyberFallbackOutcome(c, failed)
		if !failed {
			return
		}
		counters := relayFailureLogMetrics[failure.Protocol]
		isCyber := failure.Code == relaycommon.CyberPolicyCode
		_, localSensitive := RelayPromptSensitiveMatch(c)
		localSensitive = localSensitive && failure.Code == string(types.ErrorCodeSensitiveWordsDetected)
		if isCyber {
			counters.cyber.Add(1)
		} else if !localSensitive && failure.Code != CyberSessionBlockedCode && !IsCyberFallback(c) && !IsCyberFallbackError(failure.Code) && !constant.ErrorLogEnabled {
			counters.ignored.Add(1)
			return
		}
		other := model.NewLogOther()
		other.SetPublic("request_path", c.Request.URL.Path)
		other.SetPublic("error_code", failure.Code)
		other.SetPublic("error_type", failure.Type)
		other.SetPublic("protocol", failure.Protocol)
		if failure.Protocol != "websocket" {
			other.SetPublic("status_code", c.Writer.Status())
		}
		if failure.ErrorStatus != 0 {
			other.SetPublic("error_status", failure.ErrorStatus)
		}
		AppendRelayLogAdminInfo(c, nil, other)
		if guard, ok := c.Get(cyberSessionLogKey); ok {
			other.SetAdmin("cyber_session_guard", guard)
		} else if isCyber {
			reason := c.GetString(cyberSessionSkipKey)
			if reason == "" {
				reason = "unsupported_request"
			}
			other.SetAdmin("cyber_session_guard", gin.H{"action": "skipped", "reason": reason})
		}
		if isCyber {
			source := c.GetString(cyberMatchSourceKey)
			if source == "" {
				source = "code"
			}
			other.SetAdmin("cyber_match_source", source)
		}
		if localSensitive {
			other.SetPublic("error_source", "local")
			other.SetPublic("prompt_text_filter_scope", c.GetString(relaySensitiveScopeKey))
		}
		started := common.GetContextKeyTime(c, constant.ContextKeyRequestStartTime)
		elapsed := 0
		if !started.IsZero() {
			elapsed = max(0, int(time.Since(started).Seconds()))
		}
		message := fmt.Sprintf("Relay request failed (%s)", failure.Code)
		if isCyber {
			message = "Request rejected by an upstream content policy (cyber_policy)"
		}
		logCode := failure.Code
		if IsCyberFallback(c) && !isCyber {
			logCode = model.CyberFallbackFailedCode
		}
		if err := model.RecordErrorLog(c, c.GetInt("id"), c.GetInt("channel_id"), common.GetContextKeyString(c, constant.ContextKeyOriginalModel), c.GetString("token_name"), message,
			c.GetInt("token_id"), elapsed, failure.Protocol != "http", c.GetString("group"), logCode, other); err != nil {
			counters.failed.Add(1)
			logger.LogError(c, fmt.Sprintf("relay_failure_log protocol=%s code=%s result=write_failed", failure.Protocol, failure.Code))
			return
		}
		counters.stored.Add(1)
		if isCyber {
			logger.LogInfo(c, fmt.Sprintf("relay_failure_log protocol=%s code=cyber_policy result=stored", failure.Protocol))
		}
	}
}
