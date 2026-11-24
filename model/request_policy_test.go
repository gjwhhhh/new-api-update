package model

import (
	"errors"
	"maps"
	"math"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func TestRequestPolicyDatabaseMatrix(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			var driver gorm.Dialector
			switch dialect {
			case "sqlite":
				driver = sqlite.Open(":memory:")
			case "mysql":
				dsn := os.Getenv("TEST_MYSQL_DSN")
				if dsn == "" {
					t.Skip("TEST_MYSQL_DSN not configured")
				}
				driver = mysql.Open(dsn)
			case "postgres":
				dsn := os.Getenv("TEST_POSTGRES_DSN")
				if dsn == "" {
					t.Skip("TEST_POSTGRES_DSN not configured")
				}
				driver = postgres.Open(dsn)
			}
			db, err := gorm.Open(driver, &gorm.Config{NamingStrategy: schema.NamingStrategy{TablePrefix: "policy_test_"}})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			previousDB, previousType := DB, common.MainDatabaseType()
			previousSnapshot, previousFilter := requestPolicySnapshot.Load(), promptTextFilterPolicy.Load()
			previousPolicyOptions := RequestPolicyOptions()
			common.OptionMapRWMutex.Lock()
			previousOptions := maps.Clone(common.OptionMap)
			common.OptionMap = maps.Clone(previousPolicyOptions)
			common.OptionMapRWMutex.Unlock()
			DB = db
			common.SetMainDatabaseType(common.DatabaseType(dialect))
			initCol()
			t.Cleanup(func() {
				require.NoError(t, db.Migrator().DropTable(&Option{}, &Channel{}, &Token{}))
				for k, v := range previousPolicyOptions {
					require.NoError(t, updateOptionMap(k, v))
				}
				requestPolicySnapshot.Store(previousSnapshot)
				promptTextFilterPolicy.Store(previousFilter)
				common.OptionMapRWMutex.Lock()
				common.OptionMap = previousOptions
				common.OptionMapRWMutex.Unlock()
				DB = previousDB
				common.SetMainDatabaseType(previousType)
				initCol()
				require.NoError(t, sqlDB.Close())
			})
			require.NoError(t, db.AutoMigrate(&Option{}, &Channel{}, &Token{}))
			var version string
			query := "SELECT version()"
			if dialect == "sqlite" {
				query = "SELECT sqlite_version()"
			}
			require.NoError(t, db.Raw(query).Scan(&version).Error)
			t.Logf("database version: %s", version)
			rules := `[{"name":"session","model_regex":[".*"],"key_sources":[{"type":"request_header","key":"X-Session"}],"ttl_seconds":0,"skip_retry_on_failure":false,"include_using_group":false,"param_override_template":{"temperature":0},"future_field":{"enabled":false}}]`
			require.NoError(t, UpdateRequestPolicyOptions(map[string]string{"RetryTimes": "2", "channel_affinity_setting.rules": rules}))
			assert.Equal(t, 2, CurrentRequestPolicy().RetryTimes)
			assert.Equal(t, 2, common.RetryTimes, "the runtime global follows the same write")
			require.NoError(t, UpdateOption("AutomaticRetryStatusCodes", "429,500-503"))
			assert.Equal(t, "429,500-503", RequestPolicyOptions()["AutomaticRetryStatusCodes"])
			assert.Equal(t, "429,500-503", operation_setting.AutomaticRetryStatusCodesToString())
			assert.Error(t, UpdateRequestPolicyOptions(map[string]string{"request_policy_setting.mode": "off"}), "the removed mode switch is not a policy option")
			assert.False(t, CurrentRequestPolicy().Affinity.Rules[0].SkipRetryOnFailure)
			assert.Equal(t, rules, RequestPolicyOptions()["channel_affinity_setting.rules"])
			loadOptionsFromDatabase()
			loadOptionsFromDatabase()
			assert.Equal(t, rules, RequestPolicyOptions()["channel_affinity_setting.rules"], "legacy rule JSON survives reloads without dropping extension fields")
			require.NoError(t, UpdateRequestPolicyOptions(map[string]string{"channel_affinity_setting.session_mode": "strict"}))
			loadOptionsFromDatabase()
			loadOptionsFromDatabase()
			assert.Equal(t, "strict", CurrentRequestPolicy().Affinity.SessionMode)
			assert.Equal(t, rules, RequestPolicyOptions()["channel_affinity_setting.rules"], "a global mode never rewrites rule fields")
			snapshot := CurrentRequestPolicy()
			assert.Error(t, UpdateRequestPolicyOptions(map[string]string{"channel_affinity_setting.session_mode": "unknown"}))
			assert.Same(t, snapshot, CurrentRequestPolicy())
			for _, value := range []string{"-1", "1.5", "bad", strconv.Itoa(math.MaxInt)} {
				assert.Error(t, UpdateRequestPolicyOptions(map[string]string{"RetryTimes": value}))
				assert.Same(t, snapshot, CurrentRequestPolicy())
			}
			assert.Error(t, UpdateRequestPolicyOptions(map[string]string{"channel_affinity_setting.rules": "[", "RetryTimes": "8"}))
			assert.Same(t, snapshot, CurrentRequestPolicy())
			var option Option
			require.NoError(t, db.Where(map[string]any{"key": "RetryTimes"}).First(&option).Error)
			assert.Equal(t, "2", option.Value)
			require.NoError(t, db.Callback().Update().Before("gorm:update").Register("policy_fail", func(tx *gorm.DB) {
				if tx.Statement.Table == "policy_test_options" {
					tx.AddError(errors.New("save rejected"))
				}
			}))
			assert.Error(t, UpdateRequestPolicyOptions(map[string]string{"RetryTimes": "8", "channel_affinity_setting.session_mode": "off"}))
			assert.Same(t, snapshot, CurrentRequestPolicy())
			require.NoError(t, db.Callback().Update().Remove("policy_fail"))
			option = Option{}
			require.NoError(t, db.Where(map[string]any{"key": "RetryTimes"}).First(&option).Error)
			assert.Equal(t, "2", option.Value)
			loadOptionsFromDatabase()
			assert.Equal(t, "strict", CurrentRequestPolicy().Affinity.SessionMode, "a failed save keeps the persisted global mode")

			legacy, err := buildPromptTextFilterPolicy(nil)
			require.NoError(t, err)
			assert.True(t, legacy.Target.Matches(0, 0), "missing scope preserves global filtering")
			assert.Equal(t, PromptRejectModeCyber, legacy.NormalizedRejectMode(), "new installations default to Cyber")
			for _, mode := range []string{PromptRejectModeSensitive, PromptRejectModeCyber} {
				require.NoError(t, UpdateRequestPolicyOptions(map[string]string{"PromptRejectMode": mode}))
				loadOptionsFromDatabase()
				assert.Equal(t, mode, CurrentPromptTextFilterPolicy().NormalizedRejectMode(), "saved rejection modes survive reload")
			}

			const optionKey = "PromptTextFilterTarget"
			for _, value := range []string{`null`, `{"scope":"channel"}`, `{"scope":"token","token_ids":[-1]}`, `{"scope":"channel_token","channel_ids":[79]}`, `{"scope":"all","token_ids":[82]}`} {
				assert.Error(t, UpdateRequestPolicyOptions(map[string]string{optionKey: value}))
			}
			target := `{"scope":"channel_token","channel_ids":[79,80],"token_ids":[82,83]}`
			require.NoError(t, UpdateRequestPolicyOptions(map[string]string{
				optionKey: target, "CheckSensitiveEnabled": "true", "CheckSensitiveOnPromptEnabled": "true", "SensitiveWords": "  fixture  \r\n\nsecond",
			}))
			policy := CurrentPromptTextFilterPolicy()
			assert.True(t, policy.Enabled)
			assert.Equal(t, []string{"fixture", "second"}, policy.Keywords)
			assert.True(t, policy.Target.Matches(80, 83))
			assert.False(t, policy.Target.Matches(79, 84))
			assert.False(t, policy.Target.Matches(81, 82))
			assert.False(t, policy.Target.Matches(79, 0))
			loadOptionsFromDatabase()
			assert.Equal(t, target, RequestPolicyOptions()[optionKey])
			assert.Equal(t, policy.Target, CurrentPromptTextFilterPolicy().Target)

			exemptOptions := map[string]string{"PromptTextFilterExempt": `{"token_ids":[82]}`}
			require.NoError(t, UpdateRequestPolicyOptions(exemptOptions))
			exemptPolicy := CurrentPromptTextFilterPolicy()
			assert.True(t, exemptPolicy.Exempted(79, 82))
			assert.False(t, exemptPolicy.Exempted(79, 83))
			overLimit := `{"token_ids":[` + strings.Repeat("1,", MaxPromptFilterTargets) + `1]}`
			for _, invalid := range []string{`{`, `{"token_ids":[0]}`, overLimit} {
				require.Error(t, UpdateRequestPolicyOptions(map[string]string{"PromptTextFilterExempt": invalid}))
				assert.Same(t, exemptPolicy, CurrentPromptTextFilterPolicy())
			}
			for _, invalid := range []string{"NaN", "+Inf", "-Inf", "0.59", "0.96"} {
				require.Error(t, UpdateRequestPolicyOptions(map[string]string{"PromptSimilarityThreshold": invalid}))
				assert.Same(t, exemptPolicy, CurrentPromptTextFilterPolicy())
			}
			require.False(t, exemptPolicy.BanSession, "old settings do not opt into session bans")
			require.Error(t, UpdateRequestPolicyOptions(map[string]string{"PromptRejectBanSession": "invalid"}))
			assert.Same(t, exemptPolicy, CurrentPromptTextFilterPolicy())
			require.Error(t, UpdateRequestPolicyOptions(map[string]string{"PromptSimilarityTemplates": strings.Repeat("x", 8193)}))
			assert.Same(t, exemptPolicy, CurrentPromptTextFilterPolicy())
			require.NoError(t, UpdateRequestPolicyOptions(map[string]string{"PromptRejectBanSession": "true"}))
			loadOptionsFromDatabase()
			loadOptionsFromDatabase()
			assert.True(t, CurrentPromptTextFilterPolicy().BanSession)
			assert.Equal(t, "true", RequestPolicyOptions()["PromptRejectBanSession"])
			require.NoError(t, UpdateRequestPolicyOptions(map[string]string{"PromptRejectBanSession": "false"}))
			loadOptionsFromDatabase()
			assert.False(t, CurrentPromptTextFilterPolicy().BanSession)
			exemptPolicy = CurrentPromptTextFilterPolicy()
			require.Error(t, UpdateRequestPolicyOptions(map[string]string{"PromptRejectMode": "invalid"}))
			assert.Same(t, exemptPolicy, CurrentPromptTextFilterPolicy())
			loadOptionsFromDatabase()
			assert.Equal(t, exemptOptions["PromptTextFilterExempt"], RequestPolicyOptions()["PromptTextFilterExempt"])
			require.NoError(t, UpdateRequestPolicyOptions(map[string]string{"PromptTextFilterExempt": `{}`}))

			const affinityKey = "channel_affinity_setting.rules"
			const invalidRules = `[{"name":"legacy rule","model_regex":["["]}]`
			require.NoError(t, db.Model(&Option{}).Where(map[string]any{"key": affinityKey}).Update("value", invalidRules).Error)
			requestPolicySnapshot.Store(nil)
			promptTextFilterPolicy.Store(nil)
			loadOptionsFromDatabase()
			require.Equal(t, policy, CurrentPromptTextFilterPolicy(), "cold startup loads a valid text filter despite invalid stored routing")
			require.Error(t, UpdateRequestPolicyOptions(map[string]string{"SensitiveWords": "must not write", "RetryTimes": "5"}))
			assert.Equal(t, []string{"fixture", "second"}, CurrentPromptTextFilterPolicy().Keywords)
			require.NoError(t, UpdateRequestPolicyOptions(map[string]string{"SensitiveWords": "updated"}))
			assert.Equal(t, []string{"updated"}, CurrentPromptTextFilterPolicy().Keywords)
			assert.Equal(t, policy.Target, CurrentPromptTextFilterPolicy().Target)
			assert.Equal(t, invalidRules, RequestPolicyOptions()[affinityKey])
			require.NoError(t, db.Model(&Option{}).Where(map[string]any{"key": "SensitiveWords"}).Update("value", "synced").Error)
			loadOptionsFromDatabase()
			assert.Equal(t, []string{"synced"}, CurrentPromptTextFilterPolicy().Keywords)
			assert.Equal(t, policy.Target, CurrentPromptTextFilterPolicy().Target)

			require.NoError(t, db.Create(&Channel{Id: 79, Name: "Team A", Key: "channel credential", Status: 1}).Error)
			require.NoError(t, db.Create(&Channel{Id: 80, Name: "Other channel", Key: "other credential", Status: 1}).Error)
			require.NoError(t, db.Create(&Token{Id: 82, Name: "Team_One", Key: "token credential", Status: 1}).Error)
			removed := Token{Id: 83, Name: "TeamXOne", Key: "removed credential", Status: 1}
			require.NoError(t, db.Create(&removed).Error)
			require.NoError(t, db.Delete(&removed).Error)
			rows, err := FindPromptFilterTargets(t.Context(), "channel", "#79", nil)
			require.NoError(t, err)
			assert.Equal(t, []PromptFilterSelectorRow{{ID: 79, Name: "Team A", Status: 1}}, rows)
			rows, err = FindPromptFilterTargets(t.Context(), "token", "team_", nil)
			require.NoError(t, err)
			assert.Equal(t, []PromptFilterSelectorRow{{ID: 82, Name: "Team_One", Status: 1}}, rows)
			rows, err = FindPromptFilterTargets(t.Context(), "token", "", []int{82, 83, 84})
			require.NoError(t, err)
			encoded, err := common.Marshal(rows)
			require.NoError(t, err)
			assert.JSONEq(t, `[{"id":82,"name":"Team_One","status":1}]`, string(encoded))
		})
	}
}
