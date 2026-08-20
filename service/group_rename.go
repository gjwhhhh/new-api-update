package service

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"gorm.io/gorm"
)

const groupSpecialUsableGroupOptionKey = "group_ratio_setting.group_special_usable_group"

var (
	groupMutationGate sync.RWMutex
	groupRenameMutex  sync.Mutex
)

// LockGroupMutationRead protects the small routing window from observing a
// half-applied group migration. Callers must release it before starting relay
// work so existing streams are never held by an administrative rename.
func LockGroupMutationRead() {
	groupMutationGate.RLock()
}

func UnlockGroupMutationRead() {
	groupMutationGate.RUnlock()
}

func LockGroupMutation() {
	groupMutationGate.Lock()
}

func UnlockGroupMutation() {
	groupMutationGate.Unlock()
}

func ResolveRenamedGroup(group string) string {
	resolved, found, err := model.ResolveGroupRenameAlias(group)
	if err != nil {
		common.SysError("resolve group rename alias failed: " + err.Error())
		return group
	}
	if found && ratio_setting.ContainsGroupRatio(resolved) {
		return resolved
	}
	return group
}

func CurrentGroupConfig() (dto.GroupConfig, error) {
	config := dto.GroupConfig{
		GroupRatio:              ratio_setting.GetGroupRatioCopy(),
		UserUsableGroups:        setting.GetUserUsableGroupsCopy(),
		AutoGroups:              append([]string(nil), setting.GetAutoGroups()...),
		DefaultUseAutoGroup:     setting.DefaultUseAutoGroup,
		GroupSpecialUsableGroup: ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup.ReadAll(),
	}
	if err := common.UnmarshalJsonStr(common.TopupGroupRatio2JSONString(), &config.TopupGroupRatio); err != nil {
		return dto.GroupConfig{}, err
	}
	if err := common.UnmarshalJsonStr(ratio_setting.GroupGroupRatio2JSONString(), &config.GroupGroupRatio); err != nil {
		return dto.GroupConfig{}, err
	}
	if err := common.UnmarshalJsonStr(setting.ModelRequestRateLimitGroup2JSONString(), &config.ModelRequestRateLimit); err != nil {
		return dto.GroupConfig{}, err
	}
	return config, nil
}

func GroupConfigRevision(config dto.GroupConfig) (string, error) {
	data, err := common.Marshal(config)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return fmt.Sprintf("sha256:%x", digest[:]), nil
}

func validateGroupName(name string, allowDefault bool) error {
	if name == "" || strings.TrimSpace(name) != name {
		return errors.New("分组名称不能为空")
	}
	if utf8.RuneCountInString(name) > 64 {
		return errors.New("分组名称不能超过 64 个字符")
	}
	if strings.Contains(name, ",") {
		return errors.New("分组名称不能包含逗号")
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return errors.New("分组名称不能包含控制字符")
		}
	}
	if name == "auto" || (!allowDefault && name == "default") {
		return fmt.Errorf("%s 是保留分组名称", name)
	}
	return nil
}

func normalizeGroupConfig(config *dto.GroupConfig) {
	delete(config.GroupRatio, "auto")
	delete(config.TopupGroupRatio, "auto")
	delete(config.ModelRequestRateLimit, "auto")
	delete(config.GroupGroupRatio, "auto")
	for userGroup, ratios := range config.GroupGroupRatio {
		delete(ratios, "auto")
		config.GroupGroupRatio[userGroup] = ratios
	}
	filteredAutoGroups := make([]string, 0, len(config.AutoGroups))
	for _, group := range config.AutoGroups {
		if group != "auto" {
			filteredAutoGroups = append(filteredAutoGroups, group)
		}
	}
	config.AutoGroups = filteredAutoGroups
	delete(config.GroupSpecialUsableGroup, "auto")
	for userGroup, rules := range config.GroupSpecialUsableGroup {
		for ruleGroup := range rules {
			group := strings.TrimPrefix(strings.TrimPrefix(ruleGroup, "+:"), "-:")
			if group == "auto" {
				delete(rules, ruleGroup)
			}
		}
		if len(rules) == 0 {
			delete(config.GroupSpecialUsableGroup, userGroup)
			continue
		}
		config.GroupSpecialUsableGroup[userGroup] = rules
	}
}

func validateGroupConfig(config dto.GroupConfig) error {
	if config.GroupRatio == nil || config.TopupGroupRatio == nil || config.UserUsableGroups == nil ||
		config.GroupGroupRatio == nil || config.AutoGroups == nil || config.GroupSpecialUsableGroup == nil || config.ModelRequestRateLimit == nil {
		return errors.New("分组配置必须包含完整的配置快照")
	}
	for name, ratio := range config.GroupRatio {
		if err := validateGroupName(name, true); err != nil {
			return err
		}
		if ratio < 0 {
			return fmt.Errorf("分组 %s 的倍率不能为负数", name)
		}
	}
	assertKnownGroup := func(name string) error {
		if name == "auto" {
			return nil
		}
		if _, ok := config.GroupRatio[name]; !ok {
			return fmt.Errorf("分组 %s 未在 GroupRatio 中定义", name)
		}
		return nil
	}
	for name, ratio := range config.TopupGroupRatio {
		if err := assertKnownGroup(name); err != nil {
			return err
		}
		if ratio < 0 {
			return fmt.Errorf("分组 %s 的充值倍率不能为负数", name)
		}
	}
	for name := range config.UserUsableGroups {
		if err := assertKnownGroup(name); err != nil {
			return err
		}
	}
	for userGroup, ratios := range config.GroupGroupRatio {
		if err := assertKnownGroup(userGroup); err != nil {
			return err
		}
		for usingGroup, ratio := range ratios {
			if err := assertKnownGroup(usingGroup); err != nil {
				return err
			}
			if ratio < 0 {
				return fmt.Errorf("分组 %s 的专属倍率不能为负数", usingGroup)
			}
		}
	}
	autoSeen := make(map[string]struct{}, len(config.AutoGroups))
	for _, group := range config.AutoGroups {
		if err := assertKnownGroup(group); err != nil {
			return err
		}
		if _, ok := autoSeen[group]; ok {
			return fmt.Errorf("自动分组 %s 重复", group)
		}
		autoSeen[group] = struct{}{}
	}
	for userGroup, rules := range config.GroupSpecialUsableGroup {
		if err := assertKnownGroup(userGroup); err != nil {
			return err
		}
		for ruleGroup := range rules {
			group := strings.TrimPrefix(strings.TrimPrefix(ruleGroup, "+:"), "-:")
			if err := assertKnownGroup(group); err != nil {
				return err
			}
		}
	}
	for group, limits := range config.ModelRequestRateLimit {
		if err := assertKnownGroup(group); err != nil {
			return err
		}
		if limits[0] < 0 || limits[1] < 1 {
			return fmt.Errorf("分组 %s 的限流值无效", group)
		}
	}
	return nil
}

func groupConfigOptionValues(config dto.GroupConfig) (map[string]string, error) {
	values := make(map[string]string, 8)
	entries := map[string]any{
		"GroupRatio":                     config.GroupRatio,
		"TopupGroupRatio":                config.TopupGroupRatio,
		"UserUsableGroups":               config.UserUsableGroups,
		"GroupGroupRatio":                config.GroupGroupRatio,
		"AutoGroups":                     config.AutoGroups,
		groupSpecialUsableGroupOptionKey: config.GroupSpecialUsableGroup,
		"ModelRequestRateLimitGroup":     config.ModelRequestRateLimit,
	}
	for key, value := range entries {
		data, err := common.Marshal(value)
		if err != nil {
			return nil, err
		}
		values[key] = string(data)
	}
	if config.DefaultUseAutoGroup {
		values["DefaultUseAutoGroup"] = "true"
	} else {
		values["DefaultUseAutoGroup"] = "false"
	}
	return values, nil
}

func currentGroupsPreserved(current map[string]float64, proposed map[string]float64) bool {
	for name := range current {
		if name == "auto" {
			continue
		}
		if _, ok := proposed[name]; !ok {
			return false
		}
	}
	return true
}

func UpdateGroupConfig(request dto.GroupConfigUpdateRequest) (string, error) {
	groupMutationGate.Lock()
	defer groupMutationGate.Unlock()

	current, err := CurrentGroupConfig()
	if err != nil {
		return "", err
	}
	revision, err := GroupConfigRevision(current)
	if err != nil {
		return "", err
	}
	if request.Revision != revision {
		return "", errors.New("分组配置已被其他操作修改，请刷新后重试")
	}
	normalizeGroupConfig(&request.Config)
	if err := validateGroupConfig(request.Config); err != nil {
		return "", err
	}
	if !currentGroupsPreserved(current.GroupRatio, request.Config.GroupRatio) {
		return "", errors.New("删除或改名分组必须使用分组重命名或停用流程")
	}
	values, err := groupConfigOptionValues(request.Config)
	if err != nil {
		return "", err
	}
	if err := model.UpdateOptionsBulk(values); err != nil {
		return "", err
	}
	newRevision, err := GroupConfigRevision(request.Config)
	if err != nil {
		return "", err
	}
	return newRevision, nil
}

func countGroupConfigReferences(config dto.GroupConfig, oldName string) int {
	count := 0
	if _, ok := config.GroupRatio[oldName]; ok {
		count++
	}
	if _, ok := config.TopupGroupRatio[oldName]; ok {
		count++
	}
	if _, ok := config.UserUsableGroups[oldName]; ok {
		count++
	}
	if ratios, ok := config.GroupGroupRatio[oldName]; ok {
		count++
		count += len(ratios)
	}
	for _, ratios := range config.GroupGroupRatio {
		if _, ok := ratios[oldName]; ok {
			count++
		}
	}
	for _, group := range config.AutoGroups {
		if group == oldName {
			count++
		}
	}
	if rules, ok := config.GroupSpecialUsableGroup[oldName]; ok {
		count++
		count += len(rules)
	}
	for _, rules := range config.GroupSpecialUsableGroup {
		for group := range rules {
			if strings.TrimPrefix(strings.TrimPrefix(group, "+:"), "-:") == oldName {
				count++
			}
		}
	}
	if _, ok := config.ModelRequestRateLimit[oldName]; ok {
		count++
	}
	return count
}

func renamedGroupConfig(config dto.GroupConfig, oldName string, newName string) dto.GroupConfig {
	result := config
	result.GroupRatio = renameFloatMap(config.GroupRatio, oldName, newName)
	result.TopupGroupRatio = renameFloatMap(config.TopupGroupRatio, oldName, newName)
	result.UserUsableGroups = renameStringMap(config.UserUsableGroups, oldName, newName)
	result.GroupGroupRatio = make(map[string]map[string]float64, len(config.GroupGroupRatio))
	for userGroup, ratios := range config.GroupGroupRatio {
		if userGroup == oldName {
			userGroup = newName
		}
		result.GroupGroupRatio[userGroup] = renameFloatMap(ratios, oldName, newName)
	}
	result.AutoGroups = make([]string, 0, len(config.AutoGroups))
	for _, group := range config.AutoGroups {
		if group == oldName {
			group = newName
		}
		result.AutoGroups = append(result.AutoGroups, group)
	}
	result.GroupSpecialUsableGroup = make(map[string]map[string]string, len(config.GroupSpecialUsableGroup))
	for userGroup, rules := range config.GroupSpecialUsableGroup {
		if userGroup == oldName {
			userGroup = newName
		}
		newRules := make(map[string]string, len(rules))
		for ruleGroup, description := range rules {
			prefix := ""
			baseGroup := ruleGroup
			if strings.HasPrefix(baseGroup, "+:") || strings.HasPrefix(baseGroup, "-:") {
				prefix = baseGroup[:2]
				baseGroup = baseGroup[2:]
			}
			if baseGroup == oldName {
				baseGroup = newName
			}
			newRules[prefix+baseGroup] = description
		}
		result.GroupSpecialUsableGroup[userGroup] = newRules
	}
	result.ModelRequestRateLimit = make(map[string][2]int, len(config.ModelRequestRateLimit))
	for group, limits := range config.ModelRequestRateLimit {
		if group == oldName {
			group = newName
		}
		result.ModelRequestRateLimit[group] = limits
	}
	return result
}

func renameFloatMap(source map[string]float64, oldName string, newName string) map[string]float64 {
	result := make(map[string]float64, len(source))
	for name, value := range source {
		if name == oldName {
			name = newName
		}
		result[name] = value
	}
	return result
}

func renameStringMap(source map[string]string, oldName string, newName string) map[string]string {
	result := make(map[string]string, len(source))
	for name, value := range source {
		if name == oldName {
			name = newName
		}
		result[name] = value
	}
	return result
}

func PreviewGroupRename(request dto.GroupRenamePreviewRequest) (dto.GroupRenamePreview, error) {
	oldName := strings.TrimSpace(request.OldName)
	newName := strings.TrimSpace(request.NewName)
	if err := validateGroupName(oldName, false); err != nil {
		return dto.GroupRenamePreview{}, err
	}
	if err := validateGroupName(newName, false); err != nil {
		return dto.GroupRenamePreview{}, err
	}
	if oldName == newName {
		return dto.GroupRenamePreview{}, errors.New("新旧分组名称不能相同")
	}
	config, err := CurrentGroupConfig()
	if err != nil {
		return dto.GroupRenamePreview{}, err
	}
	if _, ok := config.GroupRatio[oldName]; !ok {
		return dto.GroupRenamePreview{}, errors.New("原分组不存在")
	}
	if _, ok := config.GroupRatio[newName]; ok {
		return dto.GroupRenamePreview{}, errors.New("目标分组已存在；分组合并需要单独流程")
	}
	if err := model.ValidateGroupRenameChannelCapacity(oldName, newName); err != nil {
		return dto.GroupRenamePreview{}, err
	}
	newConfig := renamedGroupConfig(config, oldName, newName)
	if err := validateGroupConfig(newConfig); err != nil {
		return dto.GroupRenamePreview{}, err
	}
	values, err := groupConfigOptionValues(newConfig)
	if err != nil {
		return dto.GroupRenamePreview{}, err
	}
	revisionData := struct {
		OldName string            `json:"old_name"`
		NewName string            `json:"new_name"`
		Values  map[string]string `json:"values"`
	}{OldName: oldName, NewName: newName, Values: values}
	data, err := common.Marshal(revisionData)
	if err != nil {
		return dto.GroupRenamePreview{}, err
	}
	digest := sha256.Sum256(data)
	affected, err := model.CountGroupRenameAffected(oldName)
	if err != nil {
		return dto.GroupRenamePreview{}, err
	}
	affected.ConfigReferences = countGroupConfigReferences(config, oldName)
	return dto.GroupRenamePreview{
		OldName:  oldName,
		NewName:  newName,
		Revision: fmt.Sprintf("sha256:%x", digest[:]),
		Affected: affected,
		Warnings: []string{},
	}, nil
}

func RenameGroup(request dto.GroupRenameRequest, createdBy int) (dto.GroupRenamePreview, error) {
	groupRenameMutex.Lock()
	defer groupRenameMutex.Unlock()
	groupMutationGate.Lock()
	defer groupMutationGate.Unlock()

	if strings.TrimSpace(request.Confirmation) != strings.TrimSpace(request.OldName) {
		return dto.GroupRenamePreview{}, errors.New("确认文本必须与原分组名称完全一致")
	}
	instances, err := model.CountActiveSystemInstances()
	if err != nil {
		return dto.GroupRenamePreview{}, err
	}
	if instances > 1 {
		return dto.GroupRenamePreview{}, errors.New("当前有多个在线实例，分组重命名仅支持单节点部署")
	}
	preview, err := PreviewGroupRename(dto.GroupRenamePreviewRequest{OldName: request.OldName, NewName: request.NewName})
	if err != nil {
		return dto.GroupRenamePreview{}, err
	}
	if request.Revision != preview.Revision {
		return dto.GroupRenamePreview{}, errors.New("预检结果已过期，请重新预检后确认")
	}
	config, err := CurrentGroupConfig()
	if err != nil {
		return dto.GroupRenamePreview{}, err
	}
	renamedConfig := renamedGroupConfig(config, preview.OldName, preview.NewName)
	optionValues, err := groupConfigOptionValues(renamedConfig)
	if err != nil {
		return dto.GroupRenamePreview{}, err
	}
	migrationVersion := strings.TrimPrefix(preview.Revision, "sha256:")
	var invalidations model.GroupRenameCacheInvalidations
	err = model.DB.Transaction(func(tx *gorm.DB) error {
		affected, cacheInvalidations, migrationErr := model.RenameGroupInTransaction(tx, preview.OldName, preview.NewName, optionValues, createdBy, migrationVersion)
		if migrationErr != nil {
			return migrationErr
		}
		preview.Affected = affected
		preview.Affected.ConfigReferences = countGroupConfigReferences(config, preview.OldName)
		invalidations = cacheInvalidations
		return nil
	})
	if err != nil {
		return dto.GroupRenamePreview{}, err
	}
	if err := model.ApplyOptionValues(optionValues); err != nil {
		return dto.GroupRenamePreview{}, fmt.Errorf("迁移已提交，但加载新配置失败：%w", err)
	}
	if err := model.RefreshGroupRenameAliasCache(); err != nil {
		return dto.GroupRenamePreview{}, fmt.Errorf("迁移已提交，但刷新分组别名失败：%w", err)
	}
	if err := model.InvalidateUserCaches(invalidations.UserIDs); err != nil {
		common.SysError("invalidate renamed user caches failed: " + err.Error())
	}
	if err := model.InvalidateTokenCaches(invalidations.TokenKeys); err != nil {
		common.SysError("invalidate renamed token caches failed: " + err.Error())
	}
	for _, planID := range invalidations.SubscriptionPlanIDs {
		model.InvalidateSubscriptionPlanCache(planID)
	}
	ClearChannelAffinityCacheAll()
	model.InitChannelCache()
	return preview, nil
}

func GetUserGroupAliases(userGroup string) (map[string]string, error) {
	aliases, err := model.GetActiveGroupRenameAliases()
	if err != nil {
		return nil, err
	}
	usableGroups := GetUserUsableGroups(userGroup)
	result := make(map[string]string)
	for _, alias := range aliases {
		if _, ok := usableGroups[alias.NewName]; ok && ratio_setting.ContainsGroupRatio(alias.NewName) {
			result[alias.OldName] = alias.NewName
		}
	}
	return result, nil
}

// SortedGroupConfigOptionKeys exposes a stable key list for controllers and
// tests that need to distinguish this atomic API from generic Option updates.
func SortedGroupConfigOptionKeys() []string {
	keys := []string{
		"GroupRatio",
		"TopupGroupRatio",
		"UserUsableGroups",
		"GroupGroupRatio",
		"AutoGroups",
		"DefaultUseAutoGroup",
		groupSpecialUsableGroupOptionKey,
		"ModelRequestRateLimitGroup",
	}
	sort.Strings(keys)
	return keys
}

func IsGroupManagedOption(key string) bool {
	// These keys can remove or rename the primary group identity or alter its
	// authorization graph. The other two group-indexed options (top-up and
	// request-rate limits) remain independently editable because they neither
	// grant access nor select channels; a rename still rewrites them atomically.
	switch key {
	case "GroupRatio", "UserUsableGroups", "GroupGroupRatio", "AutoGroups", groupSpecialUsableGroupOptionKey:
		return true
	default:
		return false
	}
}

func IsGroupRelatedOption(key string) bool {
	for _, relatedKey := range SortedGroupConfigOptionKeys() {
		if key == relatedKey {
			return true
		}
	}
	return false
}
