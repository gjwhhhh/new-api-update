package operation_setting

import (
	"fmt"
	"sync"

	"github.com/QuantumNous/new-api/setting/config"
)

type ChannelTestHistorySetting struct {
	Enabled       bool `json:"enabled"`
	RetentionDays int  `json:"retention_days"`
}

var channelTestHistorySetting = ChannelTestHistorySetting{
	Enabled:       false,
	RetentionDays: 7,
}

var channelTestHistorySettingMu sync.RWMutex

type channelTestHistorySettingConfig ChannelTestHistorySetting

func init() {
	config.GlobalConfig.Register("channel_test_history_setting", &channelTestHistorySetting)
}

func GetChannelTestHistorySetting() ChannelTestHistorySetting {
	channelTestHistorySettingMu.RLock()
	defer channelTestHistorySettingMu.RUnlock()
	return channelTestHistorySetting
}

func (setting *ChannelTestHistorySetting) ConfigToMap() (map[string]string, error) {
	channelTestHistorySettingMu.RLock()
	snapshot := channelTestHistorySettingConfig(*setting)
	channelTestHistorySettingMu.RUnlock()
	return config.ConfigToMap(&snapshot)
}

func (setting *ChannelTestHistorySetting) UpdateConfigFromMap(values map[string]string) error {
	channelTestHistorySettingMu.Lock()
	defer channelTestHistorySettingMu.Unlock()
	next := channelTestHistorySettingConfig(*setting)
	if err := config.UpdateConfigFromMap(&next, values); err != nil {
		return err
	}
	if next.RetentionDays < 1 || next.RetentionDays > 90 {
		return fmt.Errorf("channel test history retention_days must be between 1 and 90")
	}
	*setting = ChannelTestHistorySetting(next)
	return nil
}
