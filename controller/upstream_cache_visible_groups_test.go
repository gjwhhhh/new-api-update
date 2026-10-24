package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/setting/perf_metrics_setting"
	"github.com/stretchr/testify/assert"
)

func TestFilterVisibleUpstreamCacheGroups_UsableAndHidden(t *testing.T) {
	prevHidden := append([]string(nil), perf_metrics_setting.GetSetting().HiddenGroups...)
	t.Cleanup(func() {
		perf_metrics_setting.SetHiddenGroupsInMemory(prevHidden)
	})
	perf_metrics_setting.SetHiddenGroupsInMemory([]string{"secret"})

	usable := map[string]string{
		"default": "d",
		"vip":     "v",
		"secret":  "s",
	}

	admin := filterVisibleUpstreamCacheGroups(
		[]string{"default", "secret", "other", "vip", "default"},
		usable,
		true,
	)
	assert.Equal(t, []string{"default", "secret", "vip"}, admin)

	user := filterVisibleUpstreamCacheGroups(
		[]string{"default", "secret", "other", "vip"},
		usable,
		false,
	)
	assert.Equal(t, []string{"default", "vip"}, user)

	assert.Nil(t, filterVisibleUpstreamCacheGroups([]string{"default"}, nil, false))
}
