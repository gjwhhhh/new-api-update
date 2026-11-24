package relay

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/promptsim"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPromptTextFilterCheckExemptsToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	common.SetContextKey(c, constant.ContextKeyTokenId, 42)

	responses := &dto.OpenAIResponsesRequest{
		Model: "gpt-4o-mini",
		Input: []byte(`"blocked word here"`),
	}
	filter := &PromptTextFilter{
		policy: &model.PromptTextFilterPolicy{
			Enabled:    true,
			Keywords:   []string{"blocked"},
			RejectMode: model.PromptRejectModeSensitive,
			Target:     model.PromptTextFilterTarget{Scope: "all"},
			Exempt:     model.PromptTextFilterExempt{TokenIDs: []int{42}},
		},
		request: responses,
	}
	assert.Nil(t, filter.Check(c, 1), "exempt token must skip filtering")

	filter = &PromptTextFilter{policy: filter.policy, request: filter.request}
	common.SetContextKey(c, constant.ContextKeyTokenId, 7)
	err := filter.Check(c, 1)
	require.NotNil(t, err)
}

func TestPromptTextFilterCheckExemptsChannel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	common.SetContextKey(c, constant.ContextKeyTokenId, 7)

	responses := &dto.OpenAIResponsesRequest{
		Model: "gpt-4o-mini",
		Input: []byte(`"blocked word here"`),
	}
	filter := &PromptTextFilter{
		policy: &model.PromptTextFilterPolicy{
			Enabled:    true,
			Keywords:   []string{"blocked"},
			RejectMode: model.PromptRejectModeSensitive,
			Target:     model.PromptTextFilterTarget{Scope: "all"},
			Exempt:     model.PromptTextFilterExempt{ChannelIDs: []int{3}},
		},
		request: responses,
	}
	assert.Nil(t, filter.Check(c, 3))
	assert.NotNil(t, filter.Check(c, 4))
}

func TestPromptTextFilterTemplateOnlyRechecksAfterExemptChannel(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	text := strings.Repeat("sample request fragment for policy tests ", 4)
	input, err := common.Marshal(text)
	require.NoError(t, err)
	filter := &PromptTextFilter{
		policy: &model.PromptTextFilterPolicy{
			Enabled: true, Target: model.PromptTextFilterTarget{Scope: "all"},
			SimilarityEnabled: true, SimilarityThreshold: 0.80,
			SimilarityTemplates: promptsim.Compile([]string{text}, promptsim.DefaultNGram),
			Exempt:              model.PromptTextFilterExempt{ChannelIDs: []int{3}},
		},
		request: &dto.OpenAIResponsesRequest{Input: input},
	}
	assert.Nil(t, filter.Check(c, 3))
	require.NotNil(t, filter.Check(c, 4))
}
