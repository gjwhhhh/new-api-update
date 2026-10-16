package service

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/setting"
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
		MaxTokenAutoGroups:  5,
		GroupSpecialUsableGroup: map[string]map[string]string{
			"vip": {"+:vip": "VIP", "-:default": "remove"},
		},
		ModelRequestRateLimit: map[string][2]int{"vip": {20, 10}},
		GroupDisplay: &dto.GroupDisplayConfig{
			Categories: []dto.GroupDisplayCategory{{ID: "premium-models", Name: "Premium", Icon: "OpenAI.Color", Order: 1}},
			Assignments: map[string]dto.GroupDisplayAssignment{
				"vip": {CategoryID: "premium-models", Order: 2},
			},
		},
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
	require.NotNil(t, renamed.GroupDisplay)
	assert.Equal(t, dto.GroupDisplayAssignment{CategoryID: "premium-models", Order: 2}, renamed.GroupDisplay.Assignments["premium"])
	assert.NotContains(t, renamed.GroupDisplay.Assignments, "vip")
	require.NoError(t, validateGroupConfig(renamed))
}

func TestValidateGroupConfigRejectsInvalidDisplayAssignments(t *testing.T) {
	config := dto.GroupConfig{
		GroupRatio:              map[string]float64{"default": 1},
		TopupGroupRatio:         map[string]float64{},
		UserUsableGroups:        map[string]string{"default": "Default"},
		GroupGroupRatio:         map[string]map[string]float64{},
		AutoGroups:              []string{},
		MaxTokenAutoGroups:      5,
		GroupSpecialUsableGroup: map[string]map[string]string{},
		ModelRequestRateLimit:   map[string][2]int{},
		GroupDisplay: &dto.GroupDisplayConfig{
			Categories: []dto.GroupDisplayCategory{{ID: "openai", Name: "GPT"}},
			Assignments: map[string]dto.GroupDisplayAssignment{
				"default": {CategoryID: "missing"},
			},
		},
	}

	err := validateGroupConfig(config)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "不存在的展示分组")
}

func TestVisibleGroupDisplayCategoriesKeepsProviderOverridesWithoutLeakingHiddenCustomCategories(t *testing.T) {
	config := &dto.GroupDisplayConfig{
		Categories: []dto.GroupDisplayCategory{
			{ID: "provider:openai", Name: "GPT 专区", Icon: "OpenAI.Color", Order: 10},
			{ID: "vip-only", Name: "VIP 专区", Order: 20},
			{ID: "visible-custom", Name: "可见专区", Order: 30},
		},
		Assignments: map[string]dto.GroupDisplayAssignment{
			"hidden-vip": {CategoryID: "vip-only"},
			"visible":    {CategoryID: "visible-custom"},
		},
	}

	categories := VisibleGroupDisplayCategories(config, map[string]struct{}{
		"gpt-free": {},
		"visible":  {},
	})

	assert.Contains(t, categories, "provider:openai")
	assert.Contains(t, categories, "visible-custom")
	assert.NotContains(t, categories, "vip-only")

	categories = VisibleGroupDisplayCategories(config, map[string]struct{}{"visible": {}})
	assert.NotContains(t, categories, "provider:openai")
}

func TestResolveAutomaticGroupDisplayCategoryIDUsesStableGroupIdentifier(t *testing.T) {
	tests := []struct {
		group string
		want  string
	}{
		{group: "gpt", want: "provider:openai"},
		{group: "gpt-pro", want: "provider:openai"},
		{group: "claude-team", want: "provider:anthropic"},
		{group: "gemini-2.5", want: "provider:gemini"},
		{group: "qwen-max", want: "provider:domestic"},
		{group: "vip", want: "other"},
	}

	for _, test := range tests {
		t.Run(test.group, func(t *testing.T) {
			assert.Equal(t, test.want, ResolveAutomaticGroupDisplayCategoryID(test.group))
		})
	}
}

func TestValidateGroupConfigRejectsDeletedExistingGroup(t *testing.T) {
	current := map[string]float64{"default": 1, "vip": 1}
	proposed := map[string]float64{"default": 1, "premium": 1}
	assert.False(t, currentGroupsPreserved(current, proposed))
}

func TestValidateGroupConfigAllowsAutoInUserUsableGroupsWithoutDefault(t *testing.T) {
	config := dto.GroupConfig{
		GroupRatio:              map[string]float64{"vip": 1},
		TopupGroupRatio:         map[string]float64{},
		UserUsableGroups:        map[string]string{"auto": "Auto group description", "vip": "VIP"},
		GroupGroupRatio:         map[string]map[string]float64{},
		AutoGroups:              []string{"vip"},
		MaxTokenAutoGroups:      5,
		GroupSpecialUsableGroup: map[string]map[string]string{},
		ModelRequestRateLimit:   map[string][2]int{},
	}
	require.NoError(t, validateGroupConfig(config))
}

func TestNormalizeGroupConfigStripsAutoFromBillingMaps(t *testing.T) {
	config := dto.GroupConfig{
		GroupRatio:       map[string]float64{"vip": 1, "auto": 0},
		TopupGroupRatio:  map[string]float64{"auto": 1},
		UserUsableGroups: map[string]string{"auto": "Auto group description"},
		GroupGroupRatio: map[string]map[string]float64{
			"vip":  {"auto": 0.5},
			"auto": {"vip": 0.5},
		},
		AutoGroups:              []string{"vip", "auto"},
		MaxTokenAutoGroups:      5,
		GroupSpecialUsableGroup: map[string]map[string]string{"auto": {"+:vip": "VIP"}},
		ModelRequestRateLimit:   map[string][2]int{"auto": {10, 20}},
	}

	normalizeGroupConfig(&config)

	assert.NotContains(t, config.GroupRatio, "auto")
	assert.NotContains(t, config.TopupGroupRatio, "auto")
	assert.Equal(t, "Auto group description", config.UserUsableGroups["auto"])
	assert.Equal(t, []string{"vip"}, config.AutoGroups)
	assert.NotContains(t, config.GroupGroupRatio, "auto")
	assert.NotContains(t, config.GroupGroupRatio["vip"], "auto")
	assert.NotContains(t, config.GroupSpecialUsableGroup, "auto")
	assert.NotContains(t, config.ModelRequestRateLimit, "auto")
	require.NoError(t, validateGroupConfig(config))
}

func TestCurrentGroupsPreservedIgnoresAutoCleanup(t *testing.T) {
	current := map[string]float64{"vip": 1, "auto": 0}
	proposed := map[string]float64{"vip": 1}
	assert.True(t, currentGroupsPreserved(current, proposed))
}

func TestGroupConfigOptionValuesIncludeMaxTokenAutoGroups(t *testing.T) {
	values, err := groupConfigOptionValues(dto.GroupConfig{
		GroupRatio:              map[string]float64{},
		TopupGroupRatio:         map[string]float64{},
		UserUsableGroups:        map[string]string{},
		GroupGroupRatio:         map[string]map[string]float64{},
		AutoGroups:              []string{},
		MaxTokenAutoGroups:      7,
		GroupSpecialUsableGroup: map[string]map[string]string{},
		ModelRequestRateLimit:   map[string][2]int{},
		GroupDisplay: &dto.GroupDisplayConfig{
			Categories:  []dto.GroupDisplayCategory{},
			Assignments: map[string]dto.GroupDisplayAssignment{},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "7", values["MaxTokenAutoGroups"])
	assert.JSONEq(t, `{"categories":[],"assignments":{}}`, values[setting.GroupDisplayConfigOptionKey])
}
