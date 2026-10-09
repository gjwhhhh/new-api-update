package operation_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProviderPricingSettingUpdatesAtomically(t *testing.T) {
	saved, err := providerPricingSetting.ConfigToMap()
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, providerPricingSetting.UpdateConfigFromMap(saved))
	})

	require.NoError(t, providerPricingSetting.UpdateConfigFromMap(map[string]string{
		"cny_per_usd": "6.8",
		"groups":      `["default","vip"]`,
	}))
	updated := GetProviderPricingSetting()
	assert.Equal(t, 6.8, updated.CNYPerUSD)
	assert.Equal(t, []string{"default", "vip"}, updated.Groups)

	err = providerPricingSetting.UpdateConfigFromMap(map[string]string{
		"cny_per_usd": "-1",
		"groups":      `["changed"]`,
	})
	require.Error(t, err)
	assert.Equal(t, updated, GetProviderPricingSetting())
}
