package model

import (
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// Pre-probe test history shape used only to seed an upgrade path.
type channelTestResultWithoutProbe struct {
	ID                 int64  `gorm:"primaryKey"`
	RunID              string `gorm:"type:varchar(64)"`
	RequestID          string `gorm:"type:varchar(64)"`
	TaskID             string `gorm:"type:varchar(64)"`
	ChannelID          int
	ChannelName        string `gorm:"type:varchar(255)"`
	ChannelType        int
	ChannelGroups      string `gorm:"type:varchar(4096)"`
	Source             string `gorm:"type:varchar(32)"`
	HealthCheckMode    string `gorm:"type:varchar(32)"`
	ModelName          string `gorm:"type:varchar(255)"`
	EndpointType       string `gorm:"type:varchar(64)"`
	RequestPath        string `gorm:"type:varchar(255)"`
	IsStream           bool
	Status             string `gorm:"type:varchar(32)"`
	UpstreamHTTPStatus int
	ResultStatusCode   int
	FailureKind        string `gorm:"type:varchar(64)"`
	KeyIndex           *int
	StateAction        string `gorm:"type:varchar(32)"`
	DurationMs         int64
	CreatedAt          int64 `gorm:"bigint"`
}

func (channelTestResultWithoutProbe) TableName() string {
	return "probe_db_channel_test_results"
}

// Verifies channel_probes and the probe columns on channel_test_results across
// SQLite, MySQL, and PostgreSQL: fresh migrate, upgrade from a pre-probe
// schema, and a second migrate for idempotency.
func TestChannelProbeDatabaseMatrix(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			if dialect == "mysql" && os.Getenv("TEST_MYSQL_DSN") == "" {
				t.Skip("TEST_MYSQL_DSN not configured")
			}
			if dialect == "postgres" && os.Getenv("TEST_POSTGRES_DSN") == "" {
				t.Skip("TEST_POSTGRES_DSN not configured")
			}
			active := openChannelProbeDB(t, dialect)
			t.Cleanup(func() {
				_ = active.Migrator().DropTable(&ChannelProbe{}, &ChannelTestResult{}, &Channel{}, &channelTestResultWithoutProbe{})
				closeChannelProbeDB(t, active)
			})

			var version string
			query := "SELECT version()"
			if dialect == "sqlite" {
				query = "SELECT sqlite_version()"
			}
			require.NoError(t, active.Raw(query).Scan(&version).Error)
			t.Logf("database version: %s", version)

			previousDB, previousType := DB, common.MainDatabaseType()
			DB = active
			common.SetMainDatabaseType(common.DatabaseType(dialect))
			t.Cleanup(func() {
				DB = previousDB
				common.SetMainDatabaseType(previousType)
			})

			t.Run("fresh", func(t *testing.T) {
				require.NoError(t, active.Migrator().DropTable(&ChannelProbe{}, &ChannelTestResult{}, &Channel{}))
				require.NoError(t, active.AutoMigrate(&Channel{}, &ChannelProbe{}, &ChannelTestResult{}))
				assertChannelProbeSchema(t, active, dialect)
				assertChannelProbeCRUD(t, active)
				require.NoError(t, active.AutoMigrate(&Channel{}, &ChannelProbe{}, &ChannelTestResult{}))
				assertChannelProbeSchema(t, active, dialect)
				assertChannelProbeCRUD(t, active)
			})

			t.Run("upgrade", func(t *testing.T) {
				require.NoError(t, active.Migrator().DropTable(&ChannelProbe{}, &ChannelTestResult{}, &Channel{}, &channelTestResultWithoutProbe{}))
				require.NoError(t, active.AutoMigrate(&Channel{}, &channelTestResultWithoutProbe{}))
				require.False(t, active.Migrator().HasTable(&ChannelProbe{}))
				require.False(t, active.Migrator().HasColumn(&ChannelTestResult{}, "ProbeMode"))
				require.False(t, active.Migrator().HasColumn(&ChannelTestResult{}, "ProbeInputTokens"))
				require.False(t, active.Migrator().HasColumn(&ChannelTestResult{}, "ProbeCachedTokens"))

				seed := channelTestResultWithoutProbe{
					RunID: "legacy-run", RequestID: "legacy-req", ChannelID: 1,
					ChannelName: "legacy", Source: "manual_single", Status: "succeeded",
					CreatedAt: common.GetTimestamp(),
				}
				require.NoError(t, active.Create(&seed).Error)

				require.NoError(t, active.AutoMigrate(&Channel{}, &ChannelProbe{}, &ChannelTestResult{}))
				if dialect == "postgres" {
					// PostgreSQL may keep prepared plans from before ALTER TABLE
					// on the same pooled connection; reopen so SELECT * sees new columns.
					closeChannelProbeDB(t, active)
					active = openChannelProbeDB(t, dialect)
					DB = active
				}
				assertChannelProbeSchema(t, active, dialect)

				var kept channelTestResultWithoutProbe
				require.NoError(t, active.First(&kept, "run_id = ?", "legacy-run").Error)
				assert.Equal(t, "legacy-req", kept.RequestID)

				assertChannelProbeCRUD(t, active)
				require.NoError(t, active.AutoMigrate(&Channel{}, &ChannelProbe{}, &ChannelTestResult{}))
				assertChannelProbeSchema(t, active, dialect)
				assertChannelProbeCRUD(t, active)
			})
		})
	}
}

func openChannelProbeDB(t *testing.T, dialect string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(channelProbeDialector(t, dialect), &gorm.Config{
		NamingStrategy: schema.NamingStrategy{TablePrefix: "probe_db_"},
	})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	return db
}

func closeChannelProbeDB(t *testing.T, db *gorm.DB) {
	t.Helper()
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())
}

func channelProbeDialector(t *testing.T, dialect string) gorm.Dialector {
	t.Helper()
	switch dialect {
	case "sqlite":
		return sqlite.Open("file:channel_probe_matrix?mode=memory&cache=shared")
	case "mysql":
		dsn := os.Getenv("TEST_MYSQL_DSN")
		require.NotEmpty(t, dsn)
		return mysql.Open(dsn)
	case "postgres":
		dsn := os.Getenv("TEST_POSTGRES_DSN")
		require.NotEmpty(t, dsn)
		return postgres.Open(dsn)
	default:
		t.Fatalf("unknown dialect %q", dialect)
		return nil
	}
}

func assertChannelProbeSchema(t *testing.T, db *gorm.DB, dialect string) {
	t.Helper()
	require.True(t, db.Migrator().HasTable(&ChannelProbe{}))
	require.True(t, db.Migrator().HasColumn(&ChannelProbe{}, "ChannelID"))
	require.True(t, db.Migrator().HasColumn(&ChannelProbe{}, "Config"))
	require.True(t, db.Migrator().HasColumn(&ChannelTestResult{}, "ProbeMode"))
	require.True(t, db.Migrator().HasColumn(&ChannelTestResult{}, "ProbeInputTokens"))
	require.True(t, db.Migrator().HasColumn(&ChannelTestResult{}, "ProbeCachedTokens"))

	if dialect != "mysql" {
		return
	}
	var columnType string
	require.NoError(t, db.Raw(`
		SELECT DATA_TYPE FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA = DATABASE()
		  AND TABLE_NAME = ?
		  AND COLUMN_NAME = 'config'
	`, "probe_db_channel_probes").Scan(&columnType).Error)
	assert.Equal(t, "longtext", strings.ToLower(columnType))
}

func assertChannelProbeCRUD(t *testing.T, db *gorm.DB) {
	t.Helper()
	require.NoError(t, db.Exec("DELETE FROM probe_db_channel_probes").Error)
	require.NoError(t, db.Exec("DELETE FROM probe_db_channel_test_results").Error)
	require.NoError(t, db.Exec("DELETE FROM probe_db_channels").Error)

	channel := &Channel{Type: 1, Name: "probe-db", Status: common.ChannelStatusEnabled, Key: "k"}
	require.NoError(t, db.Create(channel).Error)

	payload := []byte(`{"mode":"custom","context":"` + strings.Repeat("x", 70_000) + `","question":"q","expected_answer":"a"}`)
	require.NoError(t, SaveChannelProbe(channel.Id, channel.Type, payload))
	row, err := GetChannelProbe(channel.Id)
	require.NoError(t, err)
	require.NotNil(t, row)
	assert.Equal(t, payload, []byte(row.Config))

	inputTokens, cachedTokens := 24012, 0
	result := &ChannelTestResult{
		RunID:             "run-1",
		RequestID:         "req-1",
		ChannelID:         channel.Id,
		ChannelName:       channel.Name,
		ChannelType:       channel.Type,
		Source:            "manual_single",
		ProbeMode:         "custom",
		ProbeInputTokens:  &inputTokens,
		ProbeCachedTokens: &cachedTokens,
		ModelName:         "gpt-4o-mini",
		Status:            "succeeded",
	}
	require.NoError(t, CreateChannelTestResult(result))
	loaded, err := GetChannelTestResultByID(result.ID)
	require.NoError(t, err)
	assert.Equal(t, "custom", loaded.ProbeMode)
	require.NotNil(t, loaded.ProbeInputTokens)
	assert.Equal(t, 24012, *loaded.ProbeInputTokens)
	require.NotNil(t, loaded.ProbeCachedTokens)
	assert.Equal(t, 0, *loaded.ProbeCachedTokens)
}
