package service

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenamedGroupConfigRewritesEveryReference(t *testing.T) {
	config := dto.GroupConfig{
		GroupRatio:       map[string]float64{"default": 1, "vip": 0.5},
		TopupGroupRatio:  map[string]float64{"vip": 0.8},
		UserUsableGroups: map[string]string{"vip": "VIP"},
		GroupGroupRatio: map[string]map[string]float64{
			"vip":     {"default": 0.7},
			"default": {"vip": 0.6},
		},
		AutoGroups:          []string{"vip", "default"},
		DefaultUseAutoGroup: true,
		GroupSpecialUsableGroup: map[string]map[string]string{
			"vip": {"+:vip": "VIP", "-:default": "remove"},
		},
		ModelRequestRateLimit: map[string][2]int{"vip": {20, 10}},
	}

	renamed := renamedGroupConfig(config, "vip", "premium")
	require.Contains(t, renamed.GroupRatio, "premium")
	assert.NotContains(t, renamed.GroupRatio, "vip")
	assert.Equal(t, 0.8, renamed.TopupGroupRatio["premium"])
	assert.Equal(t, "VIP", renamed.UserUsableGroups["premium"])
	assert.Equal(t, 0.7, renamed.GroupGroupRatio["premium"]["default"])
	assert.Equal(t, 0.6, renamed.GroupGroupRatio["default"]["premium"])
	assert.Equal(t, []string{"premium", "default"}, renamed.AutoGroups)
	assert.Equal(t, "VIP", renamed.GroupSpecialUsableGroup["premium"]["+:premium"])
	assert.Equal(t, [2]int{20, 10}, renamed.ModelRequestRateLimit["premium"])
	require.NoError(t, validateGroupConfig(renamed))
}

func TestValidateGroupConfigRejectsDeletedExistingGroup(t *testing.T) {
	current := map[string]float64{"default": 1, "vip": 1}
	proposed := map[string]float64{"default": 1, "premium": 1}
	assert.False(t, currentGroupsPreserved(current, proposed))
}
