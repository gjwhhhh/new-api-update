package conversationaudit

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"log"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type legacyConversationAudit struct {
	ID        uint   `gorm:"primaryKey"`
	RequestID string `gorm:"index;default:''"`
}

func (legacyConversationAudit) TableName() string {
	return ConversationAudit{}.TableName()
}

func openConversationAuditMigrationTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	var dialector gorm.Dialector = sqlite.Open(dsn)
	dialect := os.Getenv("TEST_AUDIT_DB_DIALECT")
	if dialect == "mysql" || dialect == "postgres" {
		sum := sha256.Sum256([]byte(t.Name()))
		database := fmt.Sprintf("audit_test_%x", sum[:8])
		var adminDialector gorm.Dialector
		if dialect == "mysql" {
			dsn = os.Getenv("TEST_MYSQL_DSN")
			require.NotEmpty(t, dsn)
			adminDialector = mysql.Open(dsn)
		} else {
			dsn = os.Getenv("TEST_POSTGRES_DSN")
			require.NotEmpty(t, dsn)
			adminDialector = postgres.Open(dsn)
		}
		admin, err := gorm.Open(adminDialector, &gorm.Config{})
		require.NoError(t, err)
		quoted := "\"" + database + "\""
		if dialect == "mysql" {
			quoted = "`" + database + "`"
		}
		require.NoError(t, admin.Exec("CREATE DATABASE "+quoted).Error)
		t.Cleanup(func() {
			require.NoError(t, admin.Exec("DROP DATABASE "+quoted).Error)
			sqlDB, err := admin.DB()
			require.NoError(t, err)
			require.NoError(t, sqlDB.Close())
		})
		if dialect == "mysql" {
			base, query, hasQuery := strings.Cut(dsn, "?")
			dsn = base[:strings.LastIndex(base, "/")+1] + database
			if hasQuery {
				dsn += "?" + query
			}
			dialector = mysql.Open(dsn)
		} else {
			if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
				u, err := url.Parse(dsn)
				require.NoError(t, err)
				u.Path = "/" + database
				dialector = postgres.Open(u.String())
			} else {
				dialector = postgres.Open(dsn + " dbname=" + database)
			}
		}
	}
	db, err := gorm.Open(dialector, &gorm.Config{DisableForeignKeyConstraintWhenMigrating: true})
	require.NoError(t, err)
	t.Cleanup(func() {
		sqlDB, sqlErr := db.DB()
		if sqlErr == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func TestMigrateConversationAuditTablesCreatesPortableRequestIDIndex(t *testing.T) {
	db := openConversationAuditMigrationTestDB(t)

	require.NoError(t, migrateConversationAuditTables(db))
	require.True(t, db.Migrator().HasIndex(&ConversationAudit{}, requestIDUniqueIndex))
	require.False(t, db.Migrator().HasIndex(&ConversationAudit{}, legacyRequestIDIndex))
	require.True(t, db.Migrator().HasTable(&ConversationAuditResponseLink{}))
	require.True(t, db.Migrator().HasTable(&ConversationAuditResponseSegment{}))
}

func TestMigrateConversationAuditTablesReplacesLegacyRequestIDIndex(t *testing.T) {
	db := openConversationAuditMigrationTestDB(t)
	require.NoError(t, db.AutoMigrate(&legacyConversationAudit{}))
	require.True(t, db.Migrator().HasIndex(&ConversationAudit{}, legacyRequestIDIndex))

	require.NoError(t, migrateConversationAuditTables(db))
	require.True(t, db.Migrator().HasIndex(&ConversationAudit{}, requestIDUniqueIndex))
	require.False(t, db.Migrator().HasIndex(&ConversationAudit{}, legacyRequestIDIndex))
}

func TestConversationAuditSQLSchemasHaveSingleRequestIDIndexColumn(t *testing.T) {
	testCases := []struct {
		name      string
		dialector gorm.Dialector
	}{
		{
			name: "mysql",
			dialector: mysql.New(mysql.Config{
				DSN:                       "gorm:gorm@tcp(localhost:9910)/gorm?charset=utf8mb4&parseTime=True",
				SkipInitializeWithVersion: true,
			}),
		},
		{
			name:      "postgres",
			dialector: postgres.New(postgres.Config{DSN: "host=localhost user=gorm dbname=gorm sslmode=disable"}),
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			var sqlOutput bytes.Buffer
			db, err := gorm.Open(testCase.dialector, &gorm.Config{
				DryRun:               true,
				DisableAutomaticPing: true,
				Logger: logger.New(log.New(&sqlOutput, "", 0), logger.Config{
					LogLevel: logger.Info,
				}),
			})
			require.NoError(t, err)
			require.NoError(t, db.Migrator().CreateTable(&ConversationAudit{}))

			ddl := sqlOutput.String()
			require.Contains(t, ddl, requestIDUniqueIndex)
			require.NotContains(t, ddl, "`request_id`,`request_id`")
			require.NotContains(t, ddl, "\"request_id\",\"request_id\"")
		})
	}
}

func TestEncryptDecryptRejectsModifiedAAD(t *testing.T) {
	runtimeState.Lock()
	previousKeys := runtimeState.keys
	runtimeState.keys = map[string][]byte{"v1": []byte("12345678901234567890123456789012")}
	runtimeState.Unlock()
	t.Cleanup(func() {
		runtimeState.Lock()
		runtimeState.keys = previousKeys
		runtimeState.Unlock()
	})

	nonce, ciphertext, err := encrypt("v1", "confidential conversation", "request-1")
	require.NoError(t, err)
	plaintext, err := decrypt("v1", nonce, ciphertext, "request-1")
	require.NoError(t, err)
	require.Equal(t, "confidential conversation", plaintext)
	_, err = decrypt("v1", nonce, ciphertext, "different-request")
	require.Error(t, err)
}

func TestConfigFromEnvBoundsTemporaryParseLimit(t *testing.T) {
	t.Setenv("CONVERSATION_AUDIT_MAX_PARSE_BYTES", "512")
	assert.Equal(t, minimumMaxParseBytes, configFromEnv().MaxParseBytes)

	t.Setenv("CONVERSATION_AUDIT_MAX_PARSE_BYTES", "33554432")
	assert.Equal(t, maximumMaxParseBytes, configFromEnv().MaxParseBytes)

	t.Setenv("CONVERSATION_AUDIT_MAX_PARSE_BYTES", "4194304")
	assert.Equal(t, defaultMaxParseBytes, configFromEnv().MaxParseBytes)
}

func TestConfigFromEnvDisablesDiagnosticsByDefault(t *testing.T) {
	t.Setenv("CONVERSATION_AUDIT_DIAGNOSTICS", "")
	assert.False(t, configFromEnv().DiagnosticsEnabled)
	t.Setenv("CONVERSATION_AUDIT_DIAGNOSTICS", "true")
	assert.True(t, configFromEnv().DiagnosticsEnabled)
}

func TestRefreshSettingsLoadsFullPayloadToggle(t *testing.T) {
	db := openConversationAuditMigrationTestDB(t)
	require.NoError(t, migrateConversationAuditTables(db))

	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB })

	runtimeState.Lock()
	previousConfig := runtimeState.config
	previousKeys := runtimeState.keys
	runtimeState.keys = map[string][]byte{"v1": []byte("12345678901234567890123456789012")}
	runtimeState.Unlock()
	t.Cleanup(func() {
		runtimeState.Lock()
		runtimeState.config = previousConfig
		runtimeState.keys = previousKeys
		runtimeState.Unlock()
	})

	settings := AuditSettings{
		ID:                 settingsID,
		Enabled:            true,
		CaptureFullPayload: true,
		RetentionDays:      30,
		MaxContentBytes:    defaultMaxContentBytes,
		ActiveKeyVersion:   "v1",
	}
	require.NoError(t, db.Create(&settings).Error)
	refreshSettings()
	assert.True(t, currentConfig().CaptureFullPayload)

	settings.CaptureFullPayload = false
	require.NoError(t, db.Save(&settings).Error)
	refreshSettings()
	assert.False(t, currentConfig().CaptureFullPayload)
}

func TestUpdateSettingsRequestPreservesFullPayloadFalse(t *testing.T) {
	var request updateSettingsRequest
	require.NoError(t, common.Unmarshal([]byte(`{"capture_full_payload":false}`), &request))
	require.NotNil(t, request.CaptureFullPayload)
	assert.False(t, *request.CaptureFullPayload)
}

func TestPersistStoresVisibleResponseWithoutNewUserText(t *testing.T) {
	db := openConversationAuditMigrationTestDB(t)
	require.NoError(t, migrateConversationAuditTables(db))

	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB })

	runtimeState.Lock()
	previousConfig := runtimeState.config
	previousKeys := runtimeState.keys
	runtimeState.config = runtimeConfig{
		Enabled:          true,
		RetentionDays:    30,
		MaxContentBytes:  defaultMaxContentBytes,
		MaxParseBytes:    defaultMaxParseBytes,
		ActiveKeyVersion: "v1",
	}
	runtimeState.keys = map[string][]byte{"v1": []byte("12345678901234567890123456789012")}
	runtimeState.Unlock()
	t.Cleanup(func() {
		runtimeState.Lock()
		runtimeState.config = previousConfig
		runtimeState.keys = previousKeys
		runtimeState.Unlock()
	})

	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Set(common.RequestIdKey, "request-with-tool-continuation")
	context.Set("id", 1)
	context.Set("username", "audit-user")
	context.Set("token_id", 2)
	context.Set("original_model", "gpt-test")
	context.Request = httptest.NewRequest("POST", "/v1/responses", nil)

	persist(context, "", `{"schema_version":2,"capture_mode":"assistant_text","assistant_text":"visible response"}`, "", "unlinked_response_continuation", false, false, false, "completed", 200)

	var audit ConversationAudit
	require.NoError(t, db.Where("request_id = ?", "request-with-tool-continuation").First(&audit).Error)
	assert.Zero(t, audit.RequestLength)
	assert.Positive(t, audit.ResponseLength)
	assert.Empty(t, audit.RequestCiphertext)
	assert.Equal(t, "unlinked_response_continuation", audit.CaptureError)

	context.Set(common.RequestIdKey, "empty-audit")
	assert.Nil(t, persist(context, "", "", "", "", false, false, false, "completed", 200))
	var emptyCount int64
	require.NoError(t, db.Model(&ConversationAudit{}).Where("request_id = ?", "empty-audit").Count(&emptyCount).Error)
	assert.Zero(t, emptyCount)
}
