package model

import (
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrUserSessionAffinityConflict = errors.New("user session affinity configuration changed")

// Keep administrator routing policy separate from self-service user settings.
// Retain inherited rows so their versions also detect disable/restore/disable races.
type UserSessionAffinity struct {
	UserId   int   `json:"user_id" gorm:"primaryKey;autoIncrement:false"`
	Disabled bool  `json:"disabled" gorm:"not null"`
	Version  int64 `json:"version" gorm:"not null"`
}

type UserSessionAffinityDetail struct {
	UserId   int    `json:"user_id"`
	Username string `json:"username"`
	Disabled bool   `json:"disabled"`
	Version  int64  `json:"version"`
}

// Read policy from the authoritative database once per request, without another
// cache: a successful write is visible across instances and has no sync window.
func GetUserSessionAffinity(userId int) (UserSessionAffinityDetail, error) {
	var detail UserSessionAffinityDetail
	result := DB.Model(&User{}).
		Select("users.id AS user_id, users.username, COALESCE(user_session_affinities.disabled, "+commonFalseVal+") AS disabled, COALESCE(user_session_affinities.version, 0) AS version").
		Joins("LEFT JOIN user_session_affinities ON user_session_affinities.user_id = users.id").
		Where("users.id = ?", userId).Scan(&detail)
	if result.Error != nil {
		return detail, result.Error
	}
	if result.RowsAffected == 0 {
		return detail, gorm.ErrRecordNotFound
	}
	return detail, nil
}

func SearchUserSessionAffinities(keyword string, disabledOnly bool, offset, limit int) ([]UserSessionAffinityDetail, int64, error) {
	query := DB.Model(&User{}).
		Joins("LEFT JOIN user_session_affinities ON user_session_affinities.user_id = users.id")
	if disabledOnly {
		query = query.Where("user_session_affinities.disabled = ?", true)
	}
	if keyword = strings.TrimSpace(keyword); keyword != "" {
		query = query.Where("users.id = ? OR LOWER(users.username) LIKE ?", common.String2Int(keyword), "%"+strings.ToLower(keyword)+"%")
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	rows := []UserSessionAffinityDetail{}
	err := query.Select("users.id AS user_id, users.username, COALESCE(user_session_affinities.disabled, " + commonFalseVal + ") AS disabled, COALESCE(user_session_affinities.version, 0) AS version").
		Order("users.id DESC").Offset(offset).Limit(limit).Scan(&rows).Error
	return rows, total, err
}

// UpdateUserSessionAffinity applies one versioned change, preserving unrelated
// user settings and rejecting edits based on an obsolete policy snapshot.
func UpdateUserSessionAffinity(userId int, disabled bool, version int64) (UserSessionAffinityDetail, bool, error) {
	var detail UserSessionAffinityDetail
	var previous bool
	err := DB.Transaction(func(tx *gorm.DB) error {
		var user User
		if err := lockForUpdate(tx).Select("id", "username").First(&user, userId).Error; err != nil {
			return err
		}
		var policy UserSessionAffinity
		if err := tx.Where("user_id = ?", userId).Limit(1).Find(&policy).Error; err != nil {
			return err
		}
		if policy.Version != version {
			return ErrUserSessionAffinityConflict
		}
		previous = policy.Disabled
		detail = UserSessionAffinityDetail{UserId: userId, Username: user.Username, Disabled: disabled, Version: version}
		if previous == disabled {
			return nil
		}
		detail.Version++
		var result *gorm.DB
		if policy.UserId == 0 {
			result = tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&UserSessionAffinity{UserId: userId, Disabled: disabled, Version: detail.Version})
		} else {
			result = tx.Model(&UserSessionAffinity{}).Where("user_id = ? AND version = ?", userId, version).
				Updates(map[string]any{"disabled": disabled, "version": detail.Version})
		}
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrUserSessionAffinityConflict
		}
		return nil
	})
	return detail, previous, err
}
