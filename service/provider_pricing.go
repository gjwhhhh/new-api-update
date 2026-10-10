package service

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"sync/atomic"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/shopspring/decimal"
)

const claudeCacheCreation1hMultiplier = 6.0 / 3.75

// Provider pricing follows the recharge display rate: one CNY represents one USD.
// This fallback is intentionally local to the provider pricing endpoint and must
// not reuse operation_setting.Price, which is still used by the model plaza.
const defaultProviderPricingCNYPerUSD = 1.0

var lastSkippedDynamicProviderModels atomic.Value

func init() {
	lastSkippedDynamicProviderModels.Store("")
}

func GetProviderPricingModels() ([]dto.ProviderPricingModel, error) {
	capabilities, err := model.GetProviderPricingCapabilities()
	if err != nil {
		return nil, fmt.Errorf("get provider pricing capabilities: %w", err)
	}

	publicGroups := GetUserUsableGroups("")
	providerSetting := operation_setting.GetProviderPricingSetting()
	allowedGroups := make(map[string]struct{}, len(providerSetting.Groups))
	for _, group := range providerSetting.Groups {
		group = strings.TrimSpace(group)
		if group != "" {
			allowedGroups[group] = struct{}{}
		}
	}

	cnyPerUSD := providerSetting.CNYPerUSD
	if cnyPerUSD == 0 {
		cnyPerUSD = defaultProviderPricingCNYPerUSD
	}
	if cnyPerUSD <= 0 || math.IsNaN(cnyPerUSD) || math.IsInf(cnyPerUSD, 0) {
		return nil, fmt.Errorf("provider pricing CNY per USD must be a finite positive number")
	}

	groupRatios := ratio_setting.GetGroupRatioCopy()
	models := make([]dto.ProviderPricingModel, 0, len(capabilities))
	skippedDynamicModels := make(map[string]struct{})
	for _, capability := range capabilities {
		if _, ok := publicGroups[capability.Group]; !ok {
			continue
		}
		if len(allowedGroups) > 0 {
			if _, ok := allowedGroups[capability.Group]; !ok {
				continue
			}
		}
		if billing_setting.GetBillingMode(capability.Model) == billing_setting.BillingModeTieredExpr {
			skippedDynamicModels[capability.Model] = struct{}{}
			continue
		}

		groupRatio := 1.0
		if configuredRatio, ok := groupRatios[capability.Group]; ok {
			groupRatio = configuredRatio
		}
		if !validProviderPriceFactor(groupRatio) {
			continue
		}

		if modelPrice, usePrice := ratio_setting.GetModelPrice(capability.Model, false); usePrice {
			unitPrice, ok := calculateProviderPrice(modelPrice, groupRatio, cnyPerUSD)
			if !ok || unitPrice == 0 {
				continue
			}
			models = append(models, dto.ProviderPricingModel{
				ModelName: capability.Model,
				GroupName: capability.Group,
				PriceUnit: "per_call",
				UnitPrice: &unitPrice,
				Enabled:   capability.Enabled,
				Note:      "",
			})
			continue
		}

		modelRatio, configured, _ := ratio_setting.GetModelRatio(capability.Model)
		if !configured {
			continue
		}
		inputPrice, ok := calculateProviderPrice(modelRatio, 2, groupRatio, cnyPerUSD)
		if !ok {
			continue
		}
		outputPrice, ok := calculateProviderPrice(inputPrice, ratio_setting.GetCompletionRatio(capability.Model))
		if !ok {
			continue
		}

		item := dto.ProviderPricingModel{
			ModelName:   capability.Model,
			GroupName:   capability.Group,
			InputPrice:  &inputPrice,
			OutputPrice: &outputPrice,
			Enabled:     capability.Enabled,
			Note:        "",
		}
		if cacheRatio, exists := ratio_setting.GetCacheRatio(capability.Model); exists {
			if price, valid := calculateProviderPrice(inputPrice, cacheRatio); valid {
				item.CacheInputPrice = &price
			}
		}
		if createCacheRatio, exists := ratio_setting.GetCreateCacheRatio(capability.Model); exists {
			if price, valid := calculateProviderPrice(inputPrice, createCacheRatio); valid {
				item.CacheCreatePrice = &price
				isClaudeModel := strings.HasPrefix(strings.ToLower(capability.Model), "claude-")
				if capability.SupportsAnthropic || isClaudeModel {
					if price1h, valid1h := calculateProviderPrice(price, claudeCacheCreation1hMultiplier); valid1h {
						item.CacheCreatePrice1h = &price1h
					}
				}
			}
		}
		models = append(models, item)
	}

	skipped := make([]string, 0, len(skippedDynamicModels))
	for modelName := range skippedDynamicModels {
		skipped = append(skipped, modelName)
	}
	sort.Strings(skipped)
	skippedSummary := strings.Join(skipped, ", ")
	if previous := lastSkippedDynamicProviderModels.Swap(skippedSummary); skippedSummary != "" && previous != skippedSummary {
		common.SysLog(fmt.Sprintf("provider pricing skipped dynamic billing models: %s", skippedSummary))
	}

	return models, nil
}

func validProviderPriceFactor(value float64) bool {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return false
	}
	return true
}

func calculateProviderPrice(factors ...float64) (float64, bool) {
	price := decimal.NewFromInt(1)
	for _, factor := range factors {
		if !validProviderPriceFactor(factor) {
			return 0, false
		}
		price = price.Mul(decimal.NewFromFloat(factor))
	}
	value, _ := price.Round(6).Float64()
	if !validProviderPriceFactor(value) {
		return 0, false
	}
	return value, true
}
