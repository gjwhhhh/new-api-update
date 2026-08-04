package conversationaudit

import (
	"bytes"
	"fmt"
	"log"
	"net/http/httptest"
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
	RequestID string `gorm:"uniqueIndex;index;default:''"`
}

func (legacyConversationAudit) TableName() string {
	return ConversationAudit{}.TableName()
}

func openConversationAuditMigrationTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{DisableForeignKeyConstraintWhenMigrating: true})
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

func TestPersistSkipsRecordWhenRequestHasNoNewUserText(t *testing.T) {
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

	persist(context, "", `{"schema_version":2,"capture_mode":"assistant_text","assistant_text":"visible response"}`, "", "no_new_user_text", false, false, "completed", 200)

	var count int64
	require.NoError(t, db.Model(&ConversationAudit{}).Where("request_id = ?", "request-with-tool-continuation").Count(&count).Error)
	assert.Zero(t, count)
}
