package relay

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// PromptTextFilter holds one request's policy and shares extracted text with
// billing. Select a channel before checking; retries may change the scope match.
type PromptTextFilter struct {
	policy  *model.PromptTextFilterPolicy
	request dto.Request
	meta    *types.TokenCountMeta
	checked bool
}

func NewPromptTextFilter(request dto.Request) *PromptTextFilter {
	return &PromptTextFilter{policy: model.CurrentPromptTextFilterPolicy(), request: request}
}

func (f *PromptTextFilter) Check(c *gin.Context, channelID int) *types.NewAPIError {
	policy := f.policy
	if f.checked || f.request == nil || !policy.Enabled || len(policy.Keywords) == 0 ||
		!policy.Target.Matches(channelID, common.GetContextKeyInt(c, constant.ContextKeyTokenId)) {
		return nil
	}
	// Only an allowed request can retry; a clean scan need not be repeated.
	if meta := f.TokenCountMeta(); meta != nil {
		if err := service.CheckRelayPromptSensitive(c, f.request, meta.CombineText, policy.Keywords, policy.Target.Scope, channelID); err != nil {
			return err
		}
	}
	f.checked = true
	return nil
}

func (f *PromptTextFilter) TokenCountMeta() *types.TokenCountMeta {
	if f.meta == nil && f.request != nil {
		f.meta = f.request.GetTokenCountMeta()
	}
	return f.meta
}
