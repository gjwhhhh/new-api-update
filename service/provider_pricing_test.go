package service

import (
	"math"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func saveProviderPricingConfig(t *testing.T) map[string]string {
	t.Helper()
	saved := make(map[string]string)
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error {
		if strings.HasPrefix(key, "provider_pricing_setting.") || strings.HasPrefix(key, "billing_setting.") {
			saved[key] = value
		}
		return nil
	}))
	return saved
}

func TestGetProviderPricingModelsReturnsFinalCNYPrices(t *testing.T) {
	require.NoError(t, model.DB.AutoMigrate(&model.Ability{}, &model.Model{}, &model.Vendor{}))
	require.NoError(t, model.DB.Exec("DELETE FROM abilities").Error)
	require.NoError(t, model.DB.Exec("DELETE FROM channels").Error)
	require.NoError(t, model.DB.Exec("DELETE FROM models").Error)
	require.NoError(t, model.DB.Exec("DELETE FROM vendors").Error)

	savedConfig := saveProviderPricingConfig(t)
	savedModelPrice := ratio_setting.ModelPrice2JSONString()
	savedModelRatio := ratio_setting.ModelRatio2JSONString()
	savedCompletionRatio := ratio_setting.CompletionRatio2JSONString()
	savedCacheRatio := ratio_setting.CacheRatio2JSONString()
	savedCreateCacheRatio := ratio_setting.CreateCacheRatio2JSONString()
	savedGroupRatio := ratio_setting.GroupRatio2JSONString()
	savedUsableGroups := setting.UserUsableGroups2JSONString()
	savedPrice := operation_setting.Price
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(savedConfig))
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(savedModelPrice))
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(savedModelRatio))
		require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(savedCompletionRatio))
		require.NoError(t, ratio_setting.UpdateCacheRatioByJSONString(savedCacheRatio))
		require.NoError(t, ratio_setting.UpdateCreateCacheRatioByJSONString(savedCreateCacheRatio))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(savedGroupRatio))
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(savedUsableGroups))
		operation_setting.Price = savedPrice
		require.NoError(t, model.DB.Exec("DELETE FROM abilities").Error)
		require.NoError(t, model.DB.Exec("DELETE FROM channels").Error)
		require.NoError(t, model.DB.Exec("DELETE FROM models").Error)
		require.NoError(t, model.DB.Exec("DELETE FROM vendors").Error)
		model.InvalidatePricingCache()
	})

	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"provider_pricing_setting.cny_per_usd": "0",
		"provider_pricing_setting.groups":      `["default","vip"]`,
		"billing_setting.billing_mode":         `{"dynamic-model":"tiered_expr"}`,
		"billing_setting.billing_expr":         `{"dynamic-model":"tier(\"base\", p * 1 + c * 2)"}`,
	}))
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"fixed-model":0.2}`))
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"token-model":1,"disabled-model":2,"dynamic-model":1,"private-model":1}`))
	require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(`{"token-model":3,"disabled-model":2,"private-model":1}`))
	require.NoError(t, ratio_setting.UpdateCacheRatioByJSONString(`{"token-model":0.1}`))
	require.NoError(t, ratio_setting.UpdateCreateCacheRatioByJSONString(`{"token-model":1.25}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"vip":0.5,"private":1}`))
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default","vip":"VIP"}`))

	channels := []model.Channel{
		{Id: 9201, Type: constant.ChannelTypeAnthropic, Key: "key-1", Status: common.ChannelStatusEnabled, Name: "enabled"},
		{Id: 9202, Type: constant.ChannelTypeOpenAI, Key: "key-2", Status: common.ChannelStatusManuallyDisabled, Name: "disabled"},
	}
	require.NoError(t, model.DB.Create(&channels).Error)
	require.NoError(t, model.DB.Create(&[]model.Ability{
		{Group: "default", Model: "token-model", ChannelId: 9201, Enabled: true},
		{Group: "default", Model: "fixed-model", ChannelId: 9201, Enabled: true},
		{Group: "default", Model: "dynamic-model", ChannelId: 9201, Enabled: true},
		{Group: "private", Model: "private-model", ChannelId: 9201, Enabled: true},
		{Group: "vip", Model: "disabled-model", ChannelId: 9202, Enabled: false},
	}).Error)
	model.InvalidatePricingCache()

	prices, err := GetProviderPricingModels()
	require.NoError(t, err)
	require.Len(t, prices, 3)

	disabled := prices[0]
	assert.Equal(t, "disabled-model", disabled.ModelName)
	assert.Equal(t, "vip", disabled.GroupName)
	assert.False(t, disabled.Enabled)
	require.NotNil(t, disabled.InputPrice)
	require.NotNil(t, disabled.OutputPrice)
	assert.Equal(t, 2.0, *disabled.InputPrice)
	assert.Equal(t, 4.0, *disabled.OutputPrice)

	fixed := prices[1]
	assert.Equal(t, "fixed-model", fixed.ModelName)
	assert.Equal(t, "per_call", fixed.PriceUnit)
	assert.True(t, fixed.Enabled)
	require.NotNil(t, fixed.UnitPrice)
	assert.Equal(t, 0.2, *fixed.UnitPrice)
	assert.Nil(t, fixed.InputPrice)
	fixedJSON, err := common.Marshal(fixed)
	require.NoError(t, err)
	assert.Contains(t, string(fixedJSON), `"unit_price":0.2`)
	assert.NotContains(t, string(fixedJSON), `"input_price"`)

	token := prices[2]
	assert.Equal(t, "token-model", token.ModelName)
	assert.True(t, token.Enabled)
	require.NotNil(t, token.InputPrice)
	require.NotNil(t, token.OutputPrice)
	require.NotNil(t, token.CacheInputPrice)
	require.NotNil(t, token.CacheCreatePrice)
	require.NotNil(t, token.CacheCreatePrice1h)
	assert.Equal(t, 2.0, *token.InputPrice)
	assert.Equal(t, 6.0, *token.OutputPrice)
	assert.Equal(t, 0.2, *token.CacheInputPrice)
	assert.Equal(t, 2.5, *token.CacheCreatePrice)
	assert.Equal(t, 4.0, *token.CacheCreatePrice1h)
}

func TestCalculateProviderPriceRejectsInvalidAndRounds(t *testing.T) {
	price, ok := calculateProviderPrice(0.333333333, 3)
	require.True(t, ok)
	assert.Equal(t, 1.0, price)

	_, ok = calculateProviderPrice(math.Inf(1), 1)
	assert.False(t, ok)

	_, ok = calculateProviderPrice(-1, 1)
	assert.False(t, ok)
}
