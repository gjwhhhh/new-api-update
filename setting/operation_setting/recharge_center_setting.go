package operation_setting

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
)

const (
	RechargeCenterDisplayModeRedirect = "redirect"
	RechargeCenterDisplayModeEmbed    = "embed"
	maxRechargeCenterURLLength        = 2048
)

// RechargeCenterSetting controls the optional external recharge center shown
// to signed-in users. The configured URL is only exposed through the
// authenticated recharge-center endpoint.
type RechargeCenterSetting struct {
	Enabled     bool   `json:"enabled"`
	URL         string `json:"url"`
	DisplayMode string `json:"display_mode"`
}

var rechargeCenterSetting = RechargeCenterSetting{
	Enabled:     false,
	URL:         "",
	DisplayMode: RechargeCenterDisplayModeRedirect,
}

func init() {
	config.GlobalConfig.Register("recharge_center_setting", &rechargeCenterSetting)
}

func ValidateRechargeCenterURL(rawURL string) error {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil
	}
	if len(rawURL) > maxRechargeCenterURLLength {
		return fmt.Errorf("recharge center URL must not exceed %d characters", maxRechargeCenterURLLength)
	}

	parsedURL, err := url.ParseRequestURI(rawURL)
	if err != nil || parsedURL.Scheme != "https" || parsedURL.Host == "" {
		return fmt.Errorf("recharge center URL must be an absolute HTTPS URL")
	}
	if parsedURL.User != nil {
		return fmt.Errorf("recharge center URL must not include credentials")
	}
	return nil
}

func ValidateRechargeCenterDisplayMode(displayMode string) error {
	switch displayMode {
	case RechargeCenterDisplayModeRedirect, RechargeCenterDisplayModeEmbed:
		return nil
	default:
		return fmt.Errorf("invalid recharge center display mode")
	}
}

// GetRechargeCenterSetting returns the effective, safe-to-render setting.
// Invalid persisted data and incomplete configurations remain disabled rather
// than being exposed to users.
func GetRechargeCenterSetting() RechargeCenterSetting {
	common.OptionMapRWMutex.RLock()
	setting := rechargeCenterSetting
	common.OptionMapRWMutex.RUnlock()
	setting.URL = strings.TrimSpace(setting.URL)
	if ValidateRechargeCenterDisplayMode(setting.DisplayMode) != nil {
		setting.DisplayMode = RechargeCenterDisplayModeRedirect
	}
	if !setting.Enabled || ValidateRechargeCenterURL(setting.URL) != nil {
		setting.Enabled = false
		setting.URL = ""
	}
	return setting
}
