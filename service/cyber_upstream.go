package service

import (
	"context"
	"net/http"
	"regexp"
	"strings"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
)

type cyberUpstreamContextKey struct{}

const cyberMatchSourceKey = "cyber_match_source"
const cyberMessagePolicyKey = "cyber_message_policy"

var cyberMessageWord = regexp.MustCompile(`(^|[^\p{L}\p{N}_])cyber_policy($|[^\p{L}\p{N}_])`)

// RecognizeCyberUpstreamError only accepts a parsed upstream failure before
// message or status rewriting. Never pass client text or a generated reply.
func RecognizeCyberUpstreamError(ctx context.Context, upstream *types.OpenAIError, status int) *types.NewAPIError {
	if upstream == nil {
		return nil
	}
	c, _ := ctx.Value(cyberUpstreamContextKey{}).(*gin.Context)
	if upstream.Code == relaycommon.CyberPolicyCode {
		if c != nil {
			c.Set(cyberMatchSourceKey, "code")
		}
		return CyberPolicyError(status)
	}
	if len(upstream.Message) > 4096 || !strings.Contains(upstream.Message, relaycommon.CyberPolicyCode) {
		return nil
	}
	switch upstream.Code {
	case nil, "", "upstream_error", "invalid_request", "invalid_request_error", "content_policy_violation":
	default:
		return nil
	}
	switch upstream.Type {
	case "", "error", "upstream_error", "invalid_request", "invalid_request_error", "content_policy_violation":
	default:
		return nil
	}
	if status >= 500 || status == http.StatusUnauthorized || status == http.StatusPaymentRequired ||
		status == http.StatusProxyAuthRequired || status == http.StatusTooManyRequests || !cyberMessageWord.MatchString(upstream.Message) {
		return nil
	}
	enabled := false
	if c != nil {
		enabled = c.GetBool(cyberMessagePolicyKey)
	} else {
		enabled = CurrentCyberPolicy().MatchMessage
	}
	if !enabled {
		return nil
	}
	if c != nil {
		c.Set(cyberMatchSourceKey, "message")
	}
	return CyberPolicyError(status)
}
