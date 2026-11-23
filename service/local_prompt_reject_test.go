package service

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/promptsim"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckRelayPromptSensitiveRejectModes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	request := &dto.GeneralOpenAIRequest{}
	text := "fixture-trigger in the user prompt"

	for _, mode := range []string{model.PromptRejectModeSensitive, model.PromptRejectModeCyber} {
		for _, ban := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/ban=%t", mode, ban), func(t *testing.T) {
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				policy := &model.PromptTextFilterPolicy{
					Enabled: true, Keywords: []string{"fixture-trigger"}, RejectMode: mode,
					BanSession: ban, Target: model.PromptTextFilterTarget{Scope: "all"},
				}
				apiErr := CheckRelayPromptSensitive(c, request, text, policy, 1)
				require.NotNil(t, apiErr)
				if mode == model.PromptRejectModeSensitive {
					assert.Equal(t, types.ErrorCodeSensitiveWordsDetected, apiErr.GetErrorCode())
					assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
					assert.Equal(t, relaycommon.CyberPolicyCode, apiErr.ToOpenAIError().Code)
				} else {
					assert.Equal(t, types.ErrorCode(relaycommon.CyberPolicyCode), apiErr.GetErrorCode())
					assert.Equal(t, http.StatusForbidden, apiErr.StatusCode)
				}
				match, ok := RelayPromptSensitiveMatch(c)
				require.True(t, ok)
				assert.Equal(t, "local_prompt_sensitive_words", match.Rule)
				policy.BanSession = !ban // A concurrent policy change must not change this request's decision.
				armed := false
				c.Set(cyberSessionArmKey, func() { armed = true })
				SetRelayFinalError(c, apiErr)
				assert.Equal(t, ban, armed)
			})
		}
	}
}

func TestCheckRelayPromptSensitiveTemplateSimilarity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	shell := "【运营方配置 · 平台执行策略】" + strings.Repeat("稳定特征段用于相似度匹配测试内容", 8)
	parts, err := promptsim.ParseTemplates(shell)
	require.NoError(t, err)
	tmpls := promptsim.Compile(parts, promptsim.DefaultNGram)
	require.NotEmpty(t, tmpls)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	policy := &model.PromptTextFilterPolicy{
		Enabled:             true,
		SimilarityEnabled:   true,
		SimilarityThreshold: 0.80,
		SimilarityTemplates: tmpls,
		RejectMode:          model.PromptRejectModeCyber,
		Target:              model.PromptTextFilterTarget{Scope: "all"},
	}
	apiErr := CheckRelayPromptSensitive(c, &dto.GeneralOpenAIRequest{}, shell+"\n"+strings.Repeat("变化正文", 50), policy, 1)
	require.NotNil(t, apiErr)
	match, ok := RelayPromptSensitiveMatch(c)
	require.True(t, ok)
	assert.Equal(t, "local_prompt_template_similarity", match.Rule)
	assert.GreaterOrEqual(t, match.Score, 0.80)
	assert.NotEmpty(t, match.TemplateID)
	assert.Empty(t, match.Word)
	assert.Empty(t, match.Content, "template text must never be substituted for request evidence")

	apiErr = CheckRelayPromptSensitive(c, &dto.GeneralOpenAIRequest{}, "请协助取证并建隧道", policy, 1)
	assert.Nil(t, apiErr)
}
