package model

import (
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const GroupRenameAliasTTLSeconds int64 = 30 * 24 * 60 * 60

var groupRenameAliasRuntimeCache = struct {
	sync.RWMutex
	initialized bool
	aliases     map[string]GroupRenameAlias
}{aliases: make(map[string]GroupRenameAlias)}

// GroupRenameAlias preserves a short-lived compatibility mapping for browser
// state and Playground requests created before a group rename.
type GroupRenameAlias struct {
	OldName          string `json:"old_name" gorm:"primaryKey;type:varchar(64)"`
	NewName          string `json:"new_name" gorm:"type:varchar(64);index"`
	ExpiresAt        int64  `json:"expires_at" gorm:"bigint;index"`
	CreatedAt        int64  `json:"created_at" gorm:"bigint"`
	CreatedBy        int    `json:"created_by"`
	MigrationVersion string `json:"migration_version" gorm:"type:varchar(64);index"`
}

func (alias *GroupRenameAlias) BeforeCreate(_ *gorm.DB) error {
	if alias.CreatedAt == 0 {
		alias.CreatedAt = common.GetTimestamp()
	}
	return nil
}

// RefreshGroupRenameAliasCache reloads the short-lived alias cache after a
// committed migration and lazily on the first lookup after process startup.
func RefreshGroupRenameAliasCache() error {
	aliases, err := GetActiveGroupRenameAliases()
	if err != nil {
		return err
	}
	cache := make(map[string]GroupRenameAlias, len(aliases))
	for _, alias := range aliases {
		cache[alias.OldName] = alias
	}
	groupRenameAliasRuntimeCache.Lock()
	groupRenameAliasRuntimeCache.aliases = cache
	groupRenameAliasRuntimeCache.initialized = true
	groupRenameAliasRuntimeCache.Unlock()
	return nil
}

// ResolveGroupRenameAlias performs a single cached alias lookup. Rename writes
// always flatten old aliases, so following chains is neither required nor
// allowed.
func ResolveGroupRenameAlias(group string) (string, bool, error) {
	group = strings.TrimSpace(group)
	if group == "" {
		return "", false, nil
	}
	groupRenameAliasRuntimeCache.RLock()
	initialized := groupRenameAliasRuntimeCache.initialized
	alias, found := groupRenameAliasRuntimeCache.aliases[group]
	groupRenameAliasRuntimeCache.RUnlock()
	if !initialized {
		if err := RefreshGroupRenameAliasCache(); err != nil {
			return "", false, err
		}
		groupRenameAliasRuntimeCache.RLock()
		alias, found = groupRenameAliasRuntimeCache.aliases[group]
		groupRenameAliasRuntimeCache.RUnlock()
	}
	if !found {
		return group, false, nil
	}
	if alias.ExpiresAt <= common.GetTimestamp() {
		return group, false, nil
	}
	return alias.NewName, true, nil
}

func GetActiveGroupRenameAliases() ([]GroupRenameAlias, error) {
	var aliases []GroupRenameAlias
	err := DB.Where("expires_at > ?", common.GetTimestamp()).Order("old_name asc").Find(&aliases).Error
	return aliases, err
}
