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
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/parser"
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

		groupRatio := 1.0
		if configuredRatio, ok := groupRatios[capability.Group]; ok {
			groupRatio = configuredRatio
		}
		if !validProviderPriceFactor(groupRatio) {
			continue
		}

		if billing_setting.GetBillingMode(capability.Model) == billing_setting.BillingModeTieredExpr {
			expr, exists := billing_setting.GetBillingExpr(capability.Model)
			pricing, ok := providerPricingFromExpression(expr)
			if !exists || !ok {
				skippedDynamicModels[capability.Model] = struct{}{}
				continue
			}
			if pricing.FixedPrice != nil {
				unitPrice, valid := calculateProviderPrice(*pricing.FixedPrice, groupRatio, cnyPerUSD)
				if !valid || unitPrice == 0 {
					continue
				}
				models = append(models, dto.ProviderPricingModel{
					ModelName: capability.Model,
					GroupName: capability.Group,
					PriceUnit: "per_call",
					UnitPrice: &unitPrice,
					Enabled:   capability.Enabled,
				})
				continue
			}
			models = append(models, providerPricingTokenModel(capability, pricing, groupRatio, cnyPerUSD))
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

type providerPricingExpression struct {
	InputPrice         *float64
	OutputPrice        *float64
	CacheInputPrice    *float64
	CacheCreatePrice   *float64
	CacheCreatePrice1h *float64
	FixedPrice         *float64
}

func providerPricingTokenModel(capability model.ProviderPricingCapability, pricing providerPricingExpression, groupRatio, cnyPerUSD float64) dto.ProviderPricingModel {
	item := dto.ProviderPricingModel{
		ModelName:   capability.Model,
		GroupName:   capability.Group,
		Enabled:     capability.Enabled,
		InputPrice:  providerPricingPricePointer(pricing.InputPrice, groupRatio, cnyPerUSD),
		OutputPrice: providerPricingPricePointer(pricing.OutputPrice, groupRatio, cnyPerUSD),
	}
	if item.InputPrice == nil {
		zero := 0.0
		item.InputPrice = &zero
	}
	item.CacheInputPrice = providerPricingPricePointer(pricing.CacheInputPrice, groupRatio, cnyPerUSD)
	item.CacheCreatePrice = providerPricingPricePointer(pricing.CacheCreatePrice, groupRatio, cnyPerUSD)
	item.CacheCreatePrice1h = providerPricingPricePointer(pricing.CacheCreatePrice1h, groupRatio, cnyPerUSD)
	return item
}

func providerPricingPricePointer(price *float64, groupRatio, cnyPerUSD float64) *float64 {
	if price == nil {
		return nil
	}
	value, ok := calculateProviderPrice(*price, groupRatio, cnyPerUSD)
	if !ok {
		return nil
	}
	return &value
}

func providerPricingFromExpression(expression string) (providerPricingExpression, bool) {
	_, body := billingexpr.ParseExprVersion(expression)
	tree, err := parser.Parse(body)
	if err != nil || ast.Find(tree.Node, func(node ast.Node) bool {
		identifier, ok := node.(*ast.IdentifierNode)
		return ok && identifier.Value == "image_count"
	}) != nil {
		return providerPricingExpression{}, false
	}
	leaf, ok := firstProviderPricingLeaf(tree.Node)
	if !ok {
		return providerPricingExpression{}, false
	}
	if fixed, ok := providerPricingFixedPrice(leaf); ok {
		return providerPricingExpression{FixedPrice: &fixed}, true
	}
	coefficients, ok := providerPricingLinearCoefficients(leaf)
	if !ok {
		return providerPricingExpression{}, false
	}
	return providerPricingExpression{
		InputPrice:         providerPricingCoefficientPointer(coefficients, "p"),
		OutputPrice:        providerPricingCoefficientPointer(coefficients, "c"),
		CacheInputPrice:    providerPricingCoefficientPointer(coefficients, "cr"),
		CacheCreatePrice:   providerPricingCoefficientPointer(coefficients, "cc"),
		CacheCreatePrice1h: providerPricingCoefficientPointer(coefficients, "cc1h"),
	}, true
}

func providerPricingCoefficientPointer(coefficients map[string]float64, name string) *float64 {
	value, ok := coefficients[name]
	if !ok {
		return nil
	}
	return &value
}

func firstProviderPricingLeaf(node ast.Node) (ast.Node, bool) {
	switch current := node.(type) {
	case *ast.ConditionalNode:
		return firstProviderPricingLeaf(current.Exp1)
	case *ast.BinaryNode:
		if current.Operator == "*" {
			if leaf, ok := firstProviderPricingLeaf(current.Left); ok {
				return leaf, true
			}
			return firstProviderPricingLeaf(current.Right)
		}
	case *ast.CallNode:
		callee, ok := current.Callee.(*ast.IdentifierNode)
		if ok && callee.Value == "tier" && len(current.Arguments) == 2 {
			return current.Arguments[1], true
		}
	}
	return nil, false
}

func providerPricingFixedPrice(node ast.Node) (float64, bool) {
	call, ok := node.(*ast.CallNode)
	if !ok || len(call.Arguments) != 1 {
		return 0, false
	}
	callee, ok := call.Callee.(*ast.IdentifierNode)
	if !ok || callee.Value != "fixed" {
		return 0, false
	}
	return providerPricingNumber(call.Arguments[0])
}

func providerPricingLinearCoefficients(node ast.Node) (map[string]float64, bool) {
	coefficients := make(map[string]float64)
	constant, ok := providerPricingCollectLinear(node, coefficients)
	if !ok || constant != 0 {
		return nil, false
	}
	for _, value := range coefficients {
		if !validProviderPriceFactor(value) {
			return nil, false
		}
	}
	return coefficients, true
}

func providerPricingCollectLinear(node ast.Node, coefficients map[string]float64) (float64, bool) {
	switch current := node.(type) {
	case *ast.IdentifierNode:
		switch current.Value {
		case "p", "c", "cr", "cc", "cc1h":
			coefficients[current.Value]++
			return 0, true
		default:
			return 0, false
		}
	case *ast.IntegerNode, *ast.FloatNode:
		return providerPricingNumber(node)
	case *ast.UnaryNode:
		if current.Operator == "+" || current.Operator == "-" {
			temporary := map[string]float64{}
			constant, ok := providerPricingCollectLinear(current.Node, temporary)
			if !ok {
				return 0, false
			}
			factor := 1.0
			if current.Operator == "-" {
				factor = -1
			}
			for key, value := range temporary {
				coefficients[key] += factor * value
			}
			return factor * constant, true
		}
		return 0, false
	case *ast.BinaryNode:
		if current.Operator == "+" || current.Operator == "-" {
			left := map[string]float64{}
			right := map[string]float64{}
			leftConstant, leftOK := providerPricingCollectLinear(current.Left, left)
			rightConstant, rightOK := providerPricingCollectLinear(current.Right, right)
			if !leftOK || !rightOK {
				return 0, false
			}
			for key, value := range left {
				coefficients[key] += value
			}
			factor := 1.0
			if current.Operator == "-" {
				factor = -1
			}
			for key, value := range right {
				coefficients[key] += factor * value
			}
			return leftConstant + factor*rightConstant, true
		}
		if current.Operator == "*" || current.Operator == "/" {
			leftConstant, leftIsConstant := providerPricingNumber(current.Left)
			rightConstant, rightIsConstant := providerPricingNumber(current.Right)
			if current.Operator == "*" && leftIsConstant {
				coefficientsRight := map[string]float64{}
				constant, ok := providerPricingCollectLinear(current.Right, coefficientsRight)
				if !ok {
					return 0, false
				}
				for key, value := range coefficientsRight {
					coefficients[key] += leftConstant * value
				}
				return leftConstant * constant, true
			}
			if current.Operator == "*" && rightIsConstant {
				coefficientsLeft := map[string]float64{}
				constant, ok := providerPricingCollectLinear(current.Left, coefficientsLeft)
				if !ok {
					return 0, false
				}
				for key, value := range coefficientsLeft {
					coefficients[key] += rightConstant * value
				}
				return rightConstant * constant, true
			}
			if current.Operator == "/" && rightIsConstant && rightConstant != 0 {
				coefficientsLeft := map[string]float64{}
				constant, ok := providerPricingCollectLinear(current.Left, coefficientsLeft)
				if !ok {
					return 0, false
				}
				for key, value := range coefficientsLeft {
					coefficients[key] += value / rightConstant
				}
				return constant / rightConstant, true
			}
		}
	}
	return 0, false
}

func providerPricingNumber(node ast.Node) (float64, bool) {
	switch value := node.(type) {
	case *ast.IntegerNode:
		return float64(value.Value), true
	case *ast.FloatNode:
		return value.Value, true
	default:
		return 0, false
	}
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
