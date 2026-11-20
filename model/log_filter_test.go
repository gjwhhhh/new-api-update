package model

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestCyberPolicyAndEventDatabaseRoundTrip(t *testing.T) {
	const fingerprint = "8f684d23549eca894152d7e1104ad83ca0c696358034b2ed350ef67bc52b6c35"
	for _, dialect := range []common.DatabaseType{common.DatabaseTypeSQLite, common.DatabaseTypeMySQL, common.DatabaseTypePostgreSQL, common.DatabaseTypeClickHouse} {
		t.Run(string(dialect), func(t *testing.T) {
			var db *gorm.DB
			var err error
			if dialect == common.DatabaseTypeSQLite {
				db, err = gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "cyber.db")))
			} else {
				envKey := "TEST_" + strings.ToUpper(string(dialect)) + "_DSN"
				if os.Getenv(envKey) == "" {
					t.Skip(envKey + " not configured; use a disposable database")
				}
				var gotDialect common.DatabaseType
				db, gotDialect, err = chooseDB(envKey, true)
				require.Equal(t, dialect, gotDialect)
			}
			require.NoError(t, err)
			pool, err := db.DB()
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, pool.Close()) })
			oldMain, oldLog := DB, LOG_DB
			oldMainType, oldLogType := common.MainDatabaseType(), common.LogDatabaseType()
			LOG_DB = db
			common.SetLogDatabaseType(dialect)
			t.Cleanup(func() {
				DB, LOG_DB = oldMain, oldLog
				common.SetDatabaseTypes(oldMainType, oldLogType)
			})
			var version string
			if dialect == common.DatabaseTypeClickHouse {
				require.NoError(t, db.Raw("SELECT version()").Scan(&version).Error)
				require.NoError(t, migrateClickHouseLogDB())
			} else {
				DB = db
				common.SetMainDatabaseType(dialect)
				require.NoError(t, db.AutoMigrate(&Option{}, &Log{}))
				versionQuery := "SELECT version()"
				if dialect == common.DatabaseTypeSQLite {
					versionQuery = "SELECT sqlite_version()"
				}
				require.NoError(t, db.Raw(versionQuery).Scan(&version).Error)
				key := operation_setting.CyberPolicyOptionKey
				common.OptionMapRWMutex.Lock()
				oldOptions := common.OptionMap
				common.OptionMap = make(map[string]string)
				common.OptionMapRWMutex.Unlock()
				t.Cleanup(func() {
					common.OptionMapRWMutex.Lock()
					common.OptionMap = oldOptions
					common.OptionMapRWMutex.Unlock()
				})
				require.NoError(t, UpdateOption(key, `{"mode":"shadow","scope":"token","token_ids":[82],"ttl_seconds":600}`))
				want := `{"mode":"enforce","scope":"all","ttl_seconds":0,"match_message":true}`
				require.NoError(t, UpdateOption(key, want))
				assert.Error(t, UpdateOption(key, `{"mode":"enforce","scope":"channel","ttl_seconds":0}`))
				var saved Option
				require.NoError(t, db.First(&saved, &Option{Key: key}).Error)
				assert.Equal(t, want, saved.Value)
			}
			t.Logf("%s version: %s", dialect, version)
			const baseTime = 1795000000
			require.NoError(t, db.Create([]*Log{
				{CreatedAt: baseTime, RequestId: "cyber-policy", Type: LogTypeError, ChannelId: 5, TokenId: 8, ErrorCode: "cyber_policy", Other: `{"protocol":"sse","admin_info":{"cyber_match_source":"message","cyber_session_guard":{"mode":"enforce","action":"armed","configured_ttl_seconds":0,"session_fingerprint":"` + fingerprint + `"}}}`},
				{CreatedAt: baseTime + 1, RequestId: "cyber-blocked", Type: LogTypeError, ChannelId: 5, TokenId: 8, ErrorCode: "cyber_session_blocked"},
				{CreatedAt: baseTime + 2, RequestId: "cyber-other", Type: LogTypeError, ChannelId: 6, TokenId: 9, ErrorCode: "cyber_policy"},
				{CreatedAt: baseTime + 3, RequestId: "unrelated", Type: LogTypeError, ErrorCode: "unrelated"},
			}).Error)
			filter := CyberEventFilter{Start: baseTime, End: baseTime + 3, TokenID: 8, ChannelID: 5}
			items, err := CyberEvents(t.Context(), filter)
			require.NoError(t, err)
			require.Len(t, items, 2)
			assert.Equal(t, "blocked", items[0].Action)
			assert.Equal(t, "permanent", items[1].Action)
			filter.Source, filter.Action, filter.Fingerprint = "message", "permanent", fingerprint
			items, err = CyberEvents(t.Context(), filter)
			require.NoError(t, err)
			require.Len(t, items, 1)
			assert.Equal(t, "cyber-policy", items[0].RequestID)
		})
	}
}

func TestSplitLogFilterValues(t *testing.T) {
	assert.Empty(t, splitLogFilterValues(""))
	assert.Empty(t, splitLogFilterValues(" , ， "))
	assert.Equal(t, []string{"alice"}, splitLogFilterValues("alice"))
	assert.Equal(t, []string{"alice", "bob"}, splitLogFilterValues("alice, bob"))
	assert.Equal(t, []string{"alice", "bob"}, splitLogFilterValues("alice，bob"))
}

func TestGetAllLogsExcludesUsernames(t *testing.T) {
	truncateTables(t)
	require.NoError(t, LOG_DB.Create([]*Log{
		{Username: "alice", ModelName: "gpt-4", CreatedAt: 100},
		{Username: "bob", ModelName: "gpt-4", CreatedAt: 101},
		{Username: "root", ModelName: "gpt-4", CreatedAt: 102},
	}).Error)

	logs, total, err := GetAllLogs(LogTypeUnknown, 0, 0, "", "", "", 0, 20, 0, "", "", "", "alice, bob", "")
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, logs, 1)
	assert.Equal(t, "root", logs[0].Username)
}

func TestGetAllLogsExcludesUsernameLikePattern(t *testing.T) {
	truncateTables(t)
	require.NoError(t, LOG_DB.Create([]*Log{
		{Username: "alice", ModelName: "gpt-4", CreatedAt: 100},
		{Username: "albert", ModelName: "gpt-4", CreatedAt: 101},
		{Username: "bob", ModelName: "gpt-4", CreatedAt: 102},
	}).Error)

	logs, total, err := GetAllLogs(LogTypeUnknown, 0, 0, "", "", "", 0, 20, 0, "", "", "", "%al%", "")
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, logs, 1)
	assert.Equal(t, "bob", logs[0].Username)
}

func TestSumUsedQuotaExcludesUsernames(t *testing.T) {
	truncateTables(t)
	require.NoError(t, LOG_DB.Create([]*Log{
		{Username: "alice", Type: LogTypeConsume, Quota: 10, PromptTokens: 1, CompletionTokens: 1, CreatedAt: 100},
		{Username: "bob", Type: LogTypeConsume, Quota: 20, PromptTokens: 1, CompletionTokens: 1, CreatedAt: 101},
		{Username: "root", Type: LogTypeConsume, Quota: 30, PromptTokens: 1, CompletionTokens: 1, CreatedAt: 102},
	}).Error)

	stat, err := SumUsedQuota(LogTypeUnknown, 0, 0, "", "", "", 0, "", "alice")
	require.NoError(t, err)
	assert.Equal(t, 50, stat.Quota)
}

func TestApplyExcludedLogTextFilterRejectsTooManyValues(t *testing.T) {
	values := make([]string, maxLogTextFilterValues+1)
	for i := range values {
		values[i] = "user"
	}
	_, err := applyExcludedLogTextFilter(LOG_DB, "username", strings.Join(values, ","))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "排除用户过多")
}
