/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
package service

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/setting"
)

func emptyGroupDisplayConfig() *dto.GroupDisplayConfig {
	return &dto.GroupDisplayConfig{
		Categories:  []dto.GroupDisplayCategory{},
		Assignments: map[string]dto.GroupDisplayAssignment{},
	}
}

func VisibleGroupDisplayCategories(config *dto.GroupDisplayConfig, visibleGroups map[string]struct{}) map[string]dto.GroupDisplayCategory {
	if config == nil {
		return map[string]dto.GroupDisplayCategory{}
	}
	visibleCategoryIDs := make(map[string]struct{})
	for group := range visibleGroups {
		if assignment, ok := config.Assignments[group]; ok {
			visibleCategoryIDs[assignment.CategoryID] = struct{}{}
			continue
		}
		visibleCategoryIDs[ResolveAutomaticGroupDisplayCategoryID(group)] = struct{}{}
	}
	result := make(map[string]dto.GroupDisplayCategory)
	for _, category := range config.Categories {
		if _, ok := visibleCategoryIDs[category.ID]; ok {
			result[category.ID] = category
		}
	}
	return result
}

func GetGroupDisplayConfig() *dto.GroupDisplayConfig {
	common.OptionMapRWMutex.RLock()
	raw := common.OptionMap[setting.GroupDisplayConfigOptionKey]
	common.OptionMapRWMutex.RUnlock()
	if raw == "" {
		return emptyGroupDisplayConfig()
	}

	config := emptyGroupDisplayConfig()
	if err := common.UnmarshalJsonStr(raw, config); err != nil {
		common.SysError("load group display config failed: " + err.Error())
		return emptyGroupDisplayConfig()
	}
	if config.Categories == nil {
		config.Categories = []dto.GroupDisplayCategory{}
	}
	if config.Assignments == nil {
		config.Assignments = map[string]dto.GroupDisplayAssignment{}
	}
	return config
}
