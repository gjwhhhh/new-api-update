package model

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

// Probe content must not be loaded into the channel cache or returned by channel CRUD.
type ChannelProbe struct {
	ChannelID int      `gorm:"primaryKey;autoIncrement:false"`
	Config    LongText `gorm:"not null"`
}

func GetChannelProbe(channelID int) (*ChannelProbe, error) {
	var probe ChannelProbe
	result := DB.Limit(1).Find(&probe, "channel_id = ?", channelID)
	if result.Error != nil || result.RowsAffected == 0 {
		return nil, result.Error
	}
	return &probe, nil
}

func SaveChannelProbe(channelID, channelType int, config []byte) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		// Coordinate with channel deletion and reject a concurrently changed type.
		var channel Channel
		if err := lockForUpdate(tx).Select("id", "type").First(&channel, "id = ? AND type = ?", channelID, channelType).Error; err != nil {
			return err
		}
		if len(config) == 0 {
			return deleteChannelProbes(tx, []int{channelID})
		}
		// Even in debug mode, do not put custom probe content in SQL logs.
		return tx.Session(&gorm.Session{Logger: logger.Discard}).Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "channel_id"}}, DoUpdates: clause.AssignmentColumns([]string{"config"}),
		}).Create(&ChannelProbe{ChannelID: channelID, Config: LongText(config)}).Error
	})
}

func deleteChannelProbes(tx *gorm.DB, ids []int) error {
	return tx.Where("channel_id IN ?", ids).Delete(&ChannelProbe{}).Error
}

func InsertChannelCopy(clone *Channel, sourceID int) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(clone).Error; err != nil {
			return err
		}
		if err := clone.AddAbilities(tx); err != nil {
			return err
		}
		var probe ChannelProbe
		err := tx.First(&probe, "channel_id = ?", sourceID).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		probe.ChannelID = clone.Id
		return tx.Session(&gorm.Session{Logger: logger.Discard}).Create(&probe).Error
	})
}

func (channel *Channel) UpdateAutomaticProbeTime() error {
	return DB.Model(channel).Update("last_auto_test_time", common.GetTimestamp()).Error
}

// GetStableProbeKey keeps cache diagnostics on one enabled credential without
// changing normal traffic's rotation or selecting a key awaiting recovery.
func (channel *Channel) GetStableProbeKey() (string, int, *types.NewAPIError) {
	if !channel.ChannelInfo.IsMultiKey && channel.Key != "" {
		return channel.Key, 0, nil
	}
	lock := GetChannelPollingLock(channel.Id)
	lock.Lock()
	defer lock.Unlock()
	for index, key := range channel.GetKeys() {
		if key != "" && channel.multiKeyStatus(index) == common.ChannelStatusEnabled {
			return key, index, nil
		}
	}
	return "", -1, types.NewError(errors.New("no enabled probe key"), types.ErrorCodeChannelNoAvailableKey)
}
