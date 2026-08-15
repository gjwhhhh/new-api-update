package dto

// GroupConfig is the complete set of options whose entries use a group name as
// an identifier. Keeping them in one payload prevents ordinary saves from
// exposing a partially updated routing or billing configuration.
type GroupConfig struct {
	GroupRatio              map[string]float64            `json:"group_ratio"`
	TopupGroupRatio         map[string]float64            `json:"topup_group_ratio"`
	UserUsableGroups        map[string]string             `json:"user_usable_groups"`
	GroupGroupRatio         map[string]map[string]float64 `json:"group_group_ratio"`
	AutoGroups              []string                      `json:"auto_groups"`
	DefaultUseAutoGroup     bool                          `json:"default_use_auto_group"`
	GroupSpecialUsableGroup map[string]map[string]string  `json:"group_special_usable_group"`
	ModelRequestRateLimit   map[string][2]int             `json:"model_request_rate_limit"`
}

type GroupConfigUpdateRequest struct {
	Revision string      `json:"revision"`
	Config   GroupConfig `json:"config"`
}

type GroupRenamePreviewRequest struct {
	OldName string `json:"old_name"`
	NewName string `json:"new_name"`
}

type GroupRenameRequest struct {
	OldName      string `json:"old_name"`
	NewName      string `json:"new_name"`
	Revision     string `json:"revision"`
	Confirmation string `json:"confirmation"`
}

type GroupRenameAffected struct {
	Users               int64 `json:"users"`
	Tokens              int64 `json:"tokens"`
	Channels            int64 `json:"channels"`
	Abilities           int64 `json:"abilities"`
	SubscriptionPlans   int64 `json:"subscription_plans"`
	ActiveSubscriptions int64 `json:"active_subscriptions"`
	ActiveTasks         int64 `json:"active_tasks"`
	ConfigReferences    int   `json:"config_references"`
}

type GroupRenamePreview struct {
	OldName  string              `json:"old_name"`
	NewName  string              `json:"new_name"`
	Revision string              `json:"revision"`
	Affected GroupRenameAffected `json:"affected"`
	Warnings []string            `json:"warnings"`
}
