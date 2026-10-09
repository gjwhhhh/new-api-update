package model

import (
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
)

type ProviderPricingCapability struct {
	Model             string
	Group             string
	Enabled           bool
	SupportsAnthropic bool
}

func GetProviderPricingCapabilities() ([]ProviderPricingCapability, error) {
	type abilityChannel struct {
		Ability
		ChannelType   int
		ChannelStatus int
	}

	var rows []abilityChannel
	err := DB.Table("abilities").
		Select("abilities.*, channels.type as channel_type, channels.status as channel_status").
		Joins("join channels on abilities.channel_id = channels.id").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	type capabilityKey struct {
		model string
		group string
	}
	capabilities := make(map[capabilityKey]ProviderPricingCapability)
	for _, row := range rows {
		key := capabilityKey{model: row.Model, group: row.Group}
		capability := capabilities[key]
		capability.Model = row.Model
		capability.Group = row.Group
		if row.Enabled && row.ChannelStatus == common.ChannelStatusEnabled {
			capability.Enabled = true
		}
		for _, endpointType := range common.GetEndpointTypesByChannelType(row.ChannelType, row.Model) {
			if endpointType == constant.EndpointTypeAnthropic {
				capability.SupportsAnthropic = true
				break
			}
		}
		capabilities[key] = capability
	}

	result := make([]ProviderPricingCapability, 0, len(capabilities))
	for _, capability := range capabilities {
		result = append(result, capability)
	}

	var modelMetadata []Model
	if err := DB.Find(&modelMetadata).Error; err != nil {
		return nil, err
	}
	for i := range result {
		var matched *Model
		for j := range modelMetadata {
			metadata := &modelMetadata[j]
			if metadata.NameRule == NameRuleExact && metadata.ModelName == result[i].Model {
				matched = metadata
				break
			}
		}
		if matched == nil {
			for _, nameRule := range []int{NameRulePrefix, NameRuleSuffix, NameRuleContains} {
				for j := range modelMetadata {
					metadata := &modelMetadata[j]
					if metadata.NameRule != nameRule {
						continue
					}
					matches := false
					switch nameRule {
					case NameRulePrefix:
						matches = strings.HasPrefix(result[i].Model, metadata.ModelName)
					case NameRuleSuffix:
						matches = strings.HasSuffix(result[i].Model, metadata.ModelName)
					case NameRuleContains:
						matches = strings.Contains(result[i].Model, metadata.ModelName)
					}
					if matches {
						matched = metadata
						break
					}
				}
				if matched != nil {
					break
				}
			}
		}
		if matched != nil && matched.Status != 1 {
			result[i].Enabled = false
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Model == result[j].Model {
			return result[i].Group < result[j].Group
		}
		return result[i].Model < result[j].Model
	})
	return result, nil
}
