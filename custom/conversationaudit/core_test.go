package conversationaudit

import (
	"bytes"
	"fmt"
	"log"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
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
