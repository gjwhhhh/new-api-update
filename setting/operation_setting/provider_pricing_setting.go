package operation_setting

import (
	"fmt"
	"math"
	"strconv"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
)

type ProviderPricingSetting struct {
	CNYPerUSD float64  `json:"cny_per_usd"`
	Groups    []string `json:"groups"`
}

type providerPricingConfig struct {
	mutex sync.RWMutex
	value ProviderPricingSetting
}

var providerPricingSetting = providerPricingConfig{
	value: ProviderPricingSetting{
		CNYPerUSD: 0,
		Groups:    []string{},
	},
}

func init() {
	config.GlobalConfig.Register("provider_pricing_setting", &providerPricingSetting)
}

func GetProviderPricingSetting() ProviderPricingSetting {
	providerPricingSetting.mutex.RLock()
	defer providerPricingSetting.mutex.RUnlock()
	return ProviderPricingSetting{
		CNYPerUSD: providerPricingSetting.value.CNYPerUSD,
		Groups:    append([]string(nil), providerPricingSetting.value.Groups...),
	}
}

func (s *providerPricingConfig) ConfigToMap() (map[string]string, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	groups, err := common.Marshal(s.value.Groups)
	if err != nil {
		return nil, err
	}
	return map[string]string{
		"cny_per_usd": strconv.FormatFloat(s.value.CNYPerUSD, 'f', -1, 64),
		"groups":      string(groups),
	}, nil
}

func (s *providerPricingConfig) UpdateConfigFromMap(values map[string]string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	cnyPerUSD := s.value.CNYPerUSD
	groups := append([]string(nil), s.value.Groups...)
	if raw, ok := values["cny_per_usd"]; ok {
		parsed, err := strconv.ParseFloat(raw, 64)
		if err != nil || parsed < 0 || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
			return fmt.Errorf("provider pricing cny_per_usd must be zero or a finite positive number")
		}
		cnyPerUSD = parsed
	}
	if raw, ok := values["groups"]; ok {
		if err := common.Unmarshal([]byte(raw), &groups); err != nil {
			return fmt.Errorf("invalid provider pricing groups: %w", err)
		}
	}

	s.value.CNYPerUSD = cnyPerUSD
	s.value.Groups = groups
	return nil
}
