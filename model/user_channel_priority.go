package model

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	MinUserChannelPriority   = int64(-1_000_000_000)
	MaxUserChannelPriority   = int64(1_000_000_000)
	MaxUserChannelPriorities = 500
)

type UserChannelPriority struct {
	UserId    int   `json:"user_id" gorm:"primaryKey;autoIncrement:false"`
	ChannelId int   `json:"channel_id" gorm:"primaryKey;autoIncrement:false;index"`
	Priority  int64 `json:"priority" gorm:"not null"`
	CreatedAt int64 `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt int64 `json:"updated_at" gorm:"autoUpdateTime"`
}

type UserChannelPriorityDetail struct {
	UserId         int    `json:"user_id"`
	ChannelId      int    `json:"channel_id"`
	Priority       int64  `json:"priority"`
	ChannelName    string `json:"channel_name"`
	ChannelType    int    `json:"channel_type"`
	ChannelStatus  int    `json:"channel_status"`
	ChannelGroup   string `json:"channel_group"`
	GlobalPriority int64  `json:"global_priority"`
	Weight         uint   `json:"weight"`
}

type UserChannelPriorityOption struct {
	ChannelId      int    `json:"channel_id"`
	ChannelName    string `json:"channel_name"`
	ChannelType    int    `json:"channel_type"`
	ChannelStatus  int    `json:"channel_status"`
	ChannelGroup   string `json:"channel_group"`
	GlobalPriority int64  `json:"global_priority"`
	Weight         uint   `json:"weight"`
}

type cachedUserChannelPriorities struct {
	values    map[int]int64
	expiresAt time.Time
}

var userChannelPriorityMemoryCache sync.Map

func userChannelPriorityCacheKey(userId int) string {
	return fmt.Sprintf("user_channel_priorities:%d", userId)
}

func userChannelPriorityCacheTTL() time.Duration {
	seconds := common.RedisKeyCacheSeconds()
	if seconds <= 0 {
		seconds = 60
	}
	return time.Duration(seconds) * time.Second
}

func cloneUserChannelPriorities(values map[int]int64) map[int]int64 {
	result := make(map[int]int64, len(values))
	for channelId, priority := range values {
		result[channelId] = priority
	}
	return result
}

func GetUserChannelPriorities(userId int) (map[int]int64, error) {
	if userId <= 0 {
		return map[int]int64{}, nil
	}

	if common.RedisEnabled {
		encoded, err := common.RedisGet(userChannelPriorityCacheKey(userId))
		if err == nil {
			values := make(map[int]int64)
			if err := common.UnmarshalJsonStr(encoded, &values); err == nil {
				return values, nil
			}
		}
	} else if common.MemoryCacheEnabled {
		if value, ok := userChannelPriorityMemoryCache.Load(userId); ok {
			cached := value.(cachedUserChannelPriorities)
			if time.Now().Before(cached.expiresAt) {
				return cloneUserChannelPriorities(cached.values), nil
			}
			userChannelPriorityMemoryCache.Delete(userId)
		}
	}
	var rows []UserChannelPriority
	if err := DB.Where("user_id = ?", userId).Find(&rows).Error; err != nil {
		return nil, err
	}
	values := make(map[int]int64, len(rows))
	for _, row := range rows {
		values[row.ChannelId] = row.Priority
	}

	if common.RedisEnabled {
		encoded, err := common.Marshal(values)
		if err == nil {
			if err := common.RedisSet(userChannelPriorityCacheKey(userId), string(encoded), userChannelPriorityCacheTTL()); err != nil {
				common.SysError(fmt.Sprintf("failed to cache user channel priorities: user_id=%d error=%v", userId, err))
			}
		}
	} else if common.MemoryCacheEnabled {
		userChannelPriorityMemoryCache.Store(userId, cachedUserChannelPriorities{
			values:    cloneUserChannelPriorities(values),
			expiresAt: time.Now().Add(userChannelPriorityCacheTTL()),
		})
	}
	return values, nil
}

func InvalidateUserChannelPriorityCache(userId int) error {
	userChannelPriorityMemoryCache.Delete(userId)
	if !common.RedisEnabled {
		return nil
	}
	return common.RedisDelKey(userChannelPriorityCacheKey(userId))
}

func invalidateUserChannelPriorityCaches(userIds []int) {
	seen := make(map[int]struct{}, len(userIds))
	for _, userId := range userIds {
		if userId <= 0 {
			continue
		}
		if _, ok := seen[userId]; ok {
			continue
		}
		seen[userId] = struct{}{}
		if err := InvalidateUserChannelPriorityCache(userId); err != nil {
			common.SysError(fmt.Sprintf("failed to invalidate user channel priority cache: user_id=%d error=%v", userId, err))
		}
	}
}

func ListUserChannelPriorities(userId int) ([]UserChannelPriorityDetail, error) {
	var rows []UserChannelPriorityDetail
	err := DB.Table("user_channel_priorities").
		Select(`user_channel_priorities.user_id,
			user_channel_priorities.channel_id,
			user_channel_priorities.priority,
			channels.name AS channel_name,
			channels.type AS channel_type,
			channels.status AS channel_status,
			channels.`+commonGroupCol+` AS channel_group,
			COALESCE(channels.priority, 0) AS global_priority,
			COALESCE(channels.weight, 0) AS weight`).
		Joins("JOIN channels ON channels.id = user_channel_priorities.channel_id").
		Where("user_channel_priorities.user_id = ?", userId).
		Order("user_channel_priorities.priority DESC, user_channel_priorities.channel_id ASC").
		Scan(&rows).Error
	return rows, err
}

func SearchUserChannelPriorityOptions(keyword string, offset, limit int) ([]UserChannelPriorityOption, int64, error) {
	query := DB.Model(&Channel{})
	keyword = strings.TrimSpace(keyword)
	if keyword != "" {
		pattern := "%" + strings.ToLower(keyword) + "%"
		query = query.Where("id = ? OR LOWER(name) LIKE ?", common.String2Int(keyword), pattern)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []UserChannelPriorityOption
	err := query.Select(`id AS channel_id,
		name AS channel_name,
		type AS channel_type,
		status AS channel_status,
		` + commonGroupCol + ` AS channel_group,
		COALESCE(priority, 0) AS global_priority,
		COALESCE(weight, 0) AS weight`).
		Order("id DESC").
		Limit(limit).
		Offset(offset).
		Scan(&rows).Error
	return rows, total, err
}

func PatchUserChannelPriorities(userId int, upserts []UserChannelPriority, deletes []int) error {
	if userId <= 0 {
		return gorm.ErrRecordNotFound
	}
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := lockForUpdate(tx).Select("id").First(&User{}, userId).Error; err != nil {
			return err
		}

		channelIds := make([]int, 0, len(upserts))
		for i := range upserts {
			upserts[i].UserId = userId
			channelIds = append(channelIds, upserts[i].ChannelId)
		}
		if len(channelIds) > 0 {
			var channelCount int64
			if err := tx.Model(&Channel{}).Where("id IN ?", channelIds).Count(&channelCount).Error; err != nil {
				return err
			}
			if channelCount != int64(len(channelIds)) {
				return fmt.Errorf("one or more channels do not exist")
			}
			if err := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "user_id"}, {Name: "channel_id"}},
				DoUpdates: clause.AssignmentColumns([]string{"priority", "updated_at"}),
			}).Create(&upserts).Error; err != nil {
				return err
			}
		}
		if len(deletes) > 0 {
			if err := tx.Where("user_id = ? AND channel_id IN ?", userId, deletes).Delete(&UserChannelPriority{}).Error; err != nil {
				return err
			}
		}

		var count int64
		if err := tx.Model(&UserChannelPriority{}).Where("user_id = ?", userId).Count(&count).Error; err != nil {
			return err
		}
		if count > MaxUserChannelPriorities {
			return fmt.Errorf("a user can configure at most %d channel priorities", MaxUserChannelPriorities)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if err := InvalidateUserChannelPriorityCache(userId); err != nil {
		common.SysError(fmt.Sprintf("failed to invalidate user channel priority cache after update: user_id=%d error=%v", userId, err))
	}
	return nil
}

func deleteUserChannelPrioritiesWithTx(tx *gorm.DB, userId int) error {
	if !tx.Migrator().HasTable(&UserChannelPriority{}) {
		return nil
	}
	return tx.Where("user_id = ?", userId).Delete(&UserChannelPriority{}).Error
}

func deleteChannelPrioritiesWithTx(tx *gorm.DB, channelIds []int) ([]int, error) {
	if len(channelIds) == 0 || !tx.Migrator().HasTable(&UserChannelPriority{}) {
		return nil, nil
	}
	var userIds []int
	if err := tx.Model(&UserChannelPriority{}).
		Where("channel_id IN ?", channelIds).
		Distinct("user_id").
		Pluck("user_id", &userIds).Error; err != nil {
		return nil, err
	}
	if err := tx.Where("channel_id IN ?", channelIds).Delete(&UserChannelPriority{}).Error; err != nil {
		return nil, err
	}
	return userIds, nil
}
