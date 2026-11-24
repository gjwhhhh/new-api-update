package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPromptTextFilterExemptMatchesOR(t *testing.T) {
	exempt := PromptTextFilterExempt{ChannelIDs: []int{7}, TokenIDs: []int{9}}
	assert.True(t, exempt.Matches(7, 0))
	assert.True(t, exempt.Matches(0, 9))
	assert.True(t, exempt.Matches(7, 9))
	assert.False(t, exempt.Matches(8, 10))
	assert.False(t, PromptTextFilterExempt{}.Matches(7, 9))
}

func TestBuildPromptTextFilterPolicyExempt(t *testing.T) {
	policy, err := buildPromptTextFilterPolicy(map[string]string{
		"CheckSensitiveEnabled":         "true",
		"CheckSensitiveOnPromptEnabled": "true",
		"PromptSimilarityEnabled":       "false",
		"PromptSimilarityThreshold":     "0.80",
		"PromptSimilarityTemplates":     "",
		"PromptRejectMode":              PromptRejectModeCyber,
		"SensitiveWords":                "blocked",
		"PromptTextFilterTarget":        `{"scope":"all"}`,
		"PromptTextFilterExempt":        `{"channel_ids":[3],"token_ids":[5]}`,
	})
	require.NoError(t, err)
	assert.Equal(t, []int{3}, policy.Exempt.ChannelIDs)
	assert.Equal(t, []int{5}, policy.Exempt.TokenIDs)
	assert.True(t, policy.Exempted(3, 0))
	assert.True(t, policy.Exempted(0, 5))
	assert.False(t, policy.Exempted(4, 6))

	_, err = buildPromptTextFilterPolicy(map[string]string{
		"CheckSensitiveEnabled":         "true",
		"CheckSensitiveOnPromptEnabled": "true",
		"PromptSimilarityEnabled":       "false",
		"PromptSimilarityThreshold":     "0.80",
		"PromptSimilarityTemplates":     "",
		"PromptRejectMode":              PromptRejectModeCyber,
		"SensitiveWords":                "blocked",
		"PromptTextFilterTarget":        `{"scope":"all"}`,
		"PromptTextFilterExempt":        `{"token_ids":[-1]}`,
	})
	require.Error(t, err)
}

func TestPromptTextFilterCheckSkipsExempt(t *testing.T) {
	// Covered via Exempted helper used by relay.PromptTextFilter.Check.
	policy := &PromptTextFilterPolicy{
		Enabled:  true,
		Keywords: []string{"blocked"},
		Target:   PromptTextFilterTarget{Scope: "all"},
		Exempt:   PromptTextFilterExempt{TokenIDs: []int{42}},
	}
	assert.True(t, policy.ShouldScan())
	assert.True(t, policy.Target.Matches(1, 42))
	assert.True(t, policy.Exempted(1, 42))
}
