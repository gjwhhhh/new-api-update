package operation_setting

import (
	"fmt"
	"github.com/QuantumNous/new-api/setting/config"
	"sync"
)

type MailSetting struct {
	RecordsEnabled       bool `json:"records_enabled"`
	SendingEnabled       bool `json:"sending_enabled"`
	BatchLimit           int  `json:"batch_limit"`
	IntervalMilliseconds int  `json:"interval_milliseconds"`
	RetentionDays        int  `json:"retention_days"`
}

var mailSetting = MailSetting{RecordsEnabled: true, SendingEnabled: true, BatchLimit: 1000, IntervalMilliseconds: 1000, RetentionDays: 90}
var mailSettingMu sync.RWMutex

type mailSettingConfig MailSetting

func init() { config.GlobalConfig.Register("mail", &mailSetting) }
func GetMailSetting() MailSetting {
	mailSettingMu.RLock()
	defer mailSettingMu.RUnlock()
	return mailSetting
}
func (setting *MailSetting) ConfigToMap() (map[string]string, error) {
	mailSettingMu.RLock()
	snapshot := mailSettingConfig(*setting)
	mailSettingMu.RUnlock()
	return config.ConfigToMap(&snapshot)
}
func (setting *MailSetting) UpdateConfigFromMap(values map[string]string) error {
	mailSettingMu.Lock()
	defer mailSettingMu.Unlock()
	next := mailSettingConfig(*setting)
	if err := config.UpdateConfigFromMap(&next, values); err != nil {
		return err
	}
	if next.BatchLimit < 1 || next.BatchLimit > 10000 || next.IntervalMilliseconds < 100 || next.IntervalMilliseconds > 60000 || next.RetentionDays < 1 || next.RetentionDays > 3650 {
		return fmt.Errorf("Invalid email settings")
	}
	*setting = MailSetting(next)
	return nil
}
