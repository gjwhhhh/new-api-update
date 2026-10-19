package model

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const MaxLogFilterPresets = 20

var (
	ErrLogPresetInvalid  = errors.New("invalid log filter preset")
	ErrLogPresetConflict = errors.New("log filter preset version conflict")
	ErrLogPresetName     = errors.New("log filter preset name already exists")
	ErrLogPresetLimit    = errors.New("log filter preset limit reached")
)

type LogPresetFilters map[string]string

type UserLogFilterPreset struct {
	Id        int    `json:"id"`
	UserId    int    `json:"-" gorm:"uniqueIndex:idx_user_log_preset_name,priority:1;index"`
	Category  string `json:"category" gorm:"type:varchar(16);uniqueIndex:idx_user_log_preset_name,priority:2"`
	Scope     string `json:"scope" gorm:"type:varchar(8);uniqueIndex:idx_user_log_preset_name,priority:3"`
	Name      string `json:"name" gorm:"type:varchar(160)"`
	NameKey   string `json:"-" gorm:"type:varchar(64);uniqueIndex:idx_user_log_preset_name,priority:4"`
	Filters   string `json:"-" gorm:"type:text;not null"`
	Version   int64  `json:"version" gorm:"not null"`
	CreatedAt int64  `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt int64  `json:"updated_at" gorm:"autoUpdateTime"`
}

type UserLogFilterPreference struct {
	UserId          int    `json:"-" gorm:"primaryKey;autoIncrement:false"`
	Category        string `json:"category" gorm:"type:varchar(16);primaryKey"`
	Scope           string `json:"scope" gorm:"type:varchar(8);primaryKey"`
	DefaultPresetId *int   `json:"default_preset_id"`
	Version         int64  `json:"version" gorm:"not null"`
	UpdatedAt       int64  `json:"updated_at" gorm:"autoUpdateTime"`
}

type LogFilterPresetView struct {
	UserLogFilterPreset
	Content       LogPresetFilters `json:"filters"`
	IgnoredFields []string         `json:"ignored_fields,omitempty"`
}

type LogFilterPresetCollection struct {
	Items      []LogFilterPresetView   `json:"items"`
	Preference UserLogFilterPreference `json:"preference"`
}

func NormalizeLogPresetFilters(filters LogPresetFilters, scope string) (string, error) {
	normalized := LogPresetFilters{}
	for field, value := range filters {
		switch field {
		case "model", "group", "token", "requestId", "upstreamRequestId", "type":
		case "username", "excludeUsername", "channel":
			if scope != "all" {
				return "", ErrLogPresetInvalid
			}
		default:
			return "", ErrLogPresetInvalid
		}
		value = strings.TrimSpace(value)
		if !utf8.ValidString(value) || utf8.RuneCountInString(value) > 256 {
			return "", ErrLogPresetInvalid
		}
		if value == "" {
			continue
		}
		if field == "type" && (len(value) != 1 || value[0] < '0' || value[0] > '7') {
			return "", ErrLogPresetInvalid
		}
		if field == "channel" {
			id, err := strconv.Atoi(value)
			if err != nil || id <= 0 {
				return "", ErrLogPresetInvalid
			}
			value = strconv.Itoa(id)
		}
		normalized[field] = value
	}
	encoded, err := common.Marshal(normalized)
	if err != nil {
		return "", err
	}
	if len(encoded) > 8192 {
		return "", ErrLogPresetInvalid
	}
	return string(encoded), nil
}

func logPresetName(name string) (string, string, error) {
	name = strings.TrimSpace(name)
	if name == "" || !utf8.ValidString(name) || utf8.RuneCountInString(name) > 40 || strings.ContainsAny(name, "\r\n\t") {
		return "", "", ErrLogPresetInvalid
	}
	return name, fmt.Sprintf("%x", sha256.Sum256([]byte(strings.ToLower(name)))), nil
}

func (preset UserLogFilterPreset) View() (LogFilterPresetView, error) {
	view := LogFilterPresetView{UserLogFilterPreset: preset, Content: LogPresetFilters{}}
	if err := common.UnmarshalJsonStr(preset.Filters, &view.Content); err != nil {
		return view, err
	}
	if preset.Scope == "self" {
		for _, field := range []string{"username", "excludeUsername", "channel"} {
			if _, found := view.Content[field]; found {
				delete(view.Content, field)
				view.IgnoredFields = append(view.IgnoredFields, field)
			}
		}
	}
	return view, nil
}

func ListLogFilterPresets(userId int, category, scope string) (LogFilterPresetCollection, error) {
	result := LogFilterPresetCollection{Items: []LogFilterPresetView{}, Preference: UserLogFilterPreference{UserId: userId, Category: category, Scope: scope}}
	err := DB.Transaction(func(tx *gorm.DB) error {
		// Serialize the list/default snapshot with mutations, including deletion.
		if err := lockForUpdate(tx).Select("id").First(&User{}, userId).Error; err != nil {
			return err
		}
		var presets []UserLogFilterPreset
		if err := tx.Where("user_id = ? AND category = ? AND scope = ?", userId, category, scope).Order("id ASC").Find(&presets).Error; err != nil {
			return err
		}
		for _, preset := range presets {
			view, err := preset.View()
			if err != nil {
				return err
			}
			result.Items = append(result.Items, view)
		}
		return tx.Where("user_id = ? AND category = ? AND scope = ?", userId, category, scope).Limit(1).Find(&result.Preference).Error
	})
	return result, err
}

func GetLogFilterPreset(userId, id int, category, scope string) (LogFilterPresetView, error) {
	var preset UserLogFilterPreset
	if err := DB.Where("user_id = ? AND category = ? AND scope = ?", userId, category, scope).First(&preset, id).Error; err != nil {
		return LogFilterPresetView{}, err
	}
	return preset.View()
}

// SaveLogFilterPreset creates with id zero or updates an owned, versioned row.
// Locking the owner also makes the per-scope count and name checks atomic.
func SaveLogFilterPreset(userId, id int, category, scope string, version int64, name *string, filters *LogPresetFilters) (LogFilterPresetView, error) {
	var preset UserLogFilterPreset
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := lockForUpdate(tx).Select("id").First(&User{}, userId).Error; err != nil {
			return err
		}
		if id > 0 {
			if err := tx.Where("user_id = ? AND category = ? AND scope = ?", userId, category, scope).First(&preset, id).Error; err != nil {
				return err
			}
			if preset.Version != version {
				return ErrLogPresetConflict
			}
		} else {
			if name == nil || filters == nil {
				return ErrLogPresetInvalid
			}
			var count int64
			if err := tx.Model(&UserLogFilterPreset{}).Where("user_id = ? AND category = ? AND scope = ?", userId, category, scope).Count(&count).Error; err != nil {
				return err
			}
			if count >= MaxLogFilterPresets {
				return ErrLogPresetLimit
			}
			preset = UserLogFilterPreset{UserId: userId, Category: category, Scope: scope}
		}
		if name != nil {
			cleanName, key, err := logPresetName(*name)
			if err != nil {
				return err
			}
			var count int64
			if err := tx.Model(&UserLogFilterPreset{}).Where("user_id = ? AND category = ? AND scope = ? AND name_key = ? AND id <> ?", userId, category, scope, key, id).Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				return ErrLogPresetName
			}
			preset.Name, preset.NameKey = cleanName, key
		}
		if filters != nil {
			encoded, err := NormalizeLogPresetFilters(*filters, scope)
			if err != nil {
				return err
			}
			preset.Filters = encoded
		}
		preset.Version++
		if id == 0 {
			return tx.Create(&preset).Error
		}
		updated := tx.Model(&preset).Where("version = ?", version).Select("name", "name_key", "filters", "version", "updated_at").Updates(&preset)
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return ErrLogPresetConflict
		}
		return nil
	})
	if err != nil {
		return LogFilterPresetView{}, err
	}
	return preset.View()
}

func SetDefaultLogFilterPreset(userId int, category, scope string, id *int, version int64) (UserLogFilterPreference, error) {
	preference := UserLogFilterPreference{UserId: userId, Category: category, Scope: scope}
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := lockForUpdate(tx).Select("id").First(&User{}, userId).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ? AND category = ? AND scope = ?", userId, category, scope).Limit(1).Find(&preference).Error; err != nil {
			return err
		}
		if preference.Version != version {
			return ErrLogPresetConflict
		}
		if id != nil {
			var preset UserLogFilterPreset
			if err := tx.Where("user_id = ? AND category = ? AND scope = ?", userId, category, scope).First(&preset, *id).Error; err != nil {
				return err
			}
		}
		preference.DefaultPresetId = id
		preference.Version++
		if version == 0 {
			return tx.Create(&preference).Error
		}
		updated := tx.Model(&preference).Where("version = ?", version).Updates(map[string]any{"default_preset_id": id, "version": preference.Version, "updated_at": common.GetTimestamp()})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return ErrLogPresetConflict
		}
		return nil
	})
	return preference, err
}

func DeleteLogFilterPreset(userId, id int, category, scope string, version int64) (UserLogFilterPreference, error) {
	preference := UserLogFilterPreference{UserId: userId, Category: category, Scope: scope}
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := lockForUpdate(tx).Select("id").First(&User{}, userId).Error; err != nil {
			return err
		}
		var preset UserLogFilterPreset
		if err := tx.Where("user_id = ? AND category = ? AND scope = ?", userId, category, scope).First(&preset, id).Error; err != nil {
			return err
		}
		if preset.Version != version {
			return ErrLogPresetConflict
		}
		if err := tx.Delete(&preset).Error; err != nil {
			return err
		}
		if err := tx.Model(&UserLogFilterPreference{}).Where("user_id = ? AND category = ? AND scope = ? AND default_preset_id = ?", userId, category, scope, id).
			Updates(map[string]any{"default_preset_id": nil, "version": gorm.Expr("version + ?", 1), "updated_at": common.GetTimestamp()}).Error; err != nil {
			return err
		}
		return tx.Where("user_id = ? AND category = ? AND scope = ?", userId, category, scope).Limit(1).Find(&preference).Error
	})
	return preference, err
}

func deleteUserLogFilterPresetsWithTx(tx *gorm.DB, userId int) error {
	for _, table := range []any{&UserLogFilterPreset{}, &UserLogFilterPreference{}} {
		if tx.Migrator().HasTable(table) {
			if err := tx.Where("user_id = ?", userId).Delete(table).Error; err != nil {
				return err
			}
		}
	}
	return nil
}
