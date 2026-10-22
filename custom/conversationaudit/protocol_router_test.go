package conversationaudit_test

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/custom/conversationaudit"
	appI18n "github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/router"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResponsesProtocolRouteAuditsAuthenticatedRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	require.NoError(t, appI18n.Init())
	t.Setenv("CONVERSATION_AUDIT_ENABLED", "true")
	t.Setenv("CONVERSATION_AUDIT_ACTIVE_KEY_VERSION", "v1")
	t.Setenv("CONVERSATION_AUDIT_KEY_V1", base64.StdEncoding.EncodeToString([]byte("12345678901234567890123456789012")))
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousRedis, previousMaster := common.RedisEnabled, common.IsMasterNode
	previousPath := common.SQLitePath
	previousMainType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
	common.RedisEnabled, common.IsMasterNode = false, false
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.RedisEnabled, common.IsMasterNode = previousRedis, previousMaster
		common.SQLitePath = previousPath
		common.SetDatabaseTypes(previousMainType, previousLogType)
	})

	common.SQLitePath = ":memory:"
	t.Setenv("SQL_DSN", "local")
	t.Setenv("LOG_SQL_DSN", "")
	switch os.Getenv("TEST_AUDIT_DB_DIALECT") {
	case "mysql":
		require.NotEmpty(t, os.Getenv("TEST_MYSQL_DSN"))
		t.Setenv("SQL_DSN", os.Getenv("TEST_MYSQL_DSN"))
	case "postgres":
		require.NotEmpty(t, os.Getenv("TEST_POSTGRES_DSN"))
		t.Setenv("SQL_DSN", os.Getenv("TEST_POSTGRES_DSN"))
	}
	require.NoError(t, model.InitDB())
	db := model.DB
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	versionQuery := "SELECT version()"
	if db.Dialector.Name() == "sqlite" {
		versionQuery = "SELECT sqlite_version()"
	}
	var version string
	require.NoError(t, db.Raw(versionQuery).Scan(&version).Error)
	t.Logf("database: %s %s", db.Dialector.Name(), version)
	require.NoError(t, db.AutoMigrate(
		&model.User{}, &model.Token{}, &model.UserAccessToken{}, &model.AuditLog{}, &model.GroupRenameAlias{}, &conversationaudit.AuditSettings{},
		&conversationaudit.ConversationAudit{}, &conversationaudit.ConversationAuditResponseLink{},
		&conversationaudit.ConversationAuditResponseSegment{}, &conversationaudit.ConversationAuditPendingResponse{},
	))
	model.DB, model.LOG_DB = db, db
	require.NoError(t, db.Create(&conversationaudit.AuditSettings{
		ID: 1, Enabled: true, RetentionDays: 30, MaxContentBytes: 262144, ActiveKeyVersion: "v1",
	}).Error)
	user := model.User{Username: "protocol-audit-user", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default", Quota: 100}
	require.NoError(t, db.Create(&user).Error)
	token := model.Token{UserId: user.Id, Key: "protocolauditkey", Status: common.TokenStatusEnabled, ExpiredTime: -1, UnlimitedQuota: true}
	require.NoError(t, db.Create(&token).Error)
	t.Cleanup(func() {
		require.NoError(t, db.Where("token_id = ?", token.Id).Delete(&conversationaudit.ConversationAudit{}).Error)
		require.NoError(t, db.Unscoped().Delete(&token).Error)
		require.NoError(t, db.Unscoped().Delete(&user).Error)
		require.NoError(t, db.Delete(&conversationaudit.AuditSettings{}, 1).Error)
	})

	engine := gin.New()
	engine.Use(middleware.RequestId())
	conversationaudit.Register(engine.Group("/api"))
	router.SetRelayRouter(engine)
	router.SetTaskPluginProtocolRouter(engine)
	for _, authenticated := range []bool{false, true} {
		request := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"input":"current question"}`))
		request.Header.Set("Content-Type", "application/json")
		if authenticated {
			request.Header.Set("Authorization", "Bearer "+token.Key)
		}
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		var audits []conversationaudit.ConversationAudit
		require.NoError(t, db.Find(&audits).Error)
		if !authenticated {
			assert.Equal(t, http.StatusUnauthorized, response.Code)
			assert.Empty(t, audits, "unauthenticated bodies must not enter conversation audit")
			continue
		}
		// Missing model is rejected inside the real protocol pipeline, after
		// authentication. The audit must survive downstream early returns.
		require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
		require.Len(t, audits, 1)
		assert.Equal(t, user.Id, audits[0].UserID)
		assert.Equal(t, token.Id, audits[0].TokenID)
		assert.Equal(t, response.Header().Get(common.RequestIdKey), audits[0].RequestID)
		assert.Equal(t, "/v1/responses", audits[0].RequestPath)
		assert.Equal(t, "failed", audits[0].Status)
		assert.Equal(t, http.StatusBadRequest, audits[0].HTTPStatus)
		assert.Positive(t, audits[0].RequestLength)
		assert.NotEmpty(t, audits[0].RequestCiphertext)
		assert.NotContains(t, audits[0].RequestCiphertext, "current question")
	}
	// The metrics endpoint shares the existing Root settings boundary. A PAT's
	// scope cannot elevate a regular user, and an expired/deleted PAT cannot read it.
	root := model.User{Username: "protocol-audit-root", AffCode: "audit-metrics-root", Role: common.RoleRootUser, Status: common.UserStatusEnabled, Group: "default", AuthVersion: 1}
	require.NoError(t, db.Create(&root).Error)
	t.Cleanup(func() { require.NoError(t, db.Unscoped().Delete(&root).Error) })
	for i, tc := range []struct {
		name    string
		userID  int
		scope   string
		expires int64
		status  int
	}{
		{name: "anonymous", status: 401},
		{name: "regular user with scope", userID: user.Id, scope: "option:read", status: 403},
		{name: "root wrong scope", userID: root.Id, scope: "log:read", status: 403},
		{name: "root expired token", userID: root.Id, scope: "option:read", expires: time.Now().Unix() - 1, status: 401},
		{name: "root valid scope", userID: root.Id, scope: "option:read", status: 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := model.AccessTokenPrefix + strings.Repeat(string(rune('a'+i)), 43)
			pat := model.UserAccessToken{UserId: tc.userID, Name: "audit metrics", TokenHash: model.AccessTokenFingerprint(raw), TokenHint: model.AccessTokenHint(raw), ExpiresAt: tc.expires}
			if tc.userID != 0 {
				require.NoError(t, pat.SetScopes([]string{tc.scope}))
				require.NoError(t, db.Create(&pat).Error)
				t.Cleanup(func() { require.NoError(t, db.Delete(&pat).Error) })
			}
			request := httptest.NewRequest(http.MethodGet, "/api/custom/conversation-audit/metrics", nil)
			if tc.userID != 0 {
				request.Header.Set("Authorization", "Bearer "+raw)
			}
			recorder := httptest.NewRecorder()
			engine.ServeHTTP(recorder, request)
			require.Equal(t, tc.status, recorder.Code, recorder.Body.String())
			assert.NotContains(t, recorder.Body.String(), raw)
			if tc.status == 200 {
				require.NoError(t, db.Delete(&pat).Error)
				recorder = httptest.NewRecorder()
				engine.ServeHTTP(recorder, request)
				assert.Equal(t, 401, recorder.Code, "deleted tokens cannot replay metrics access")
			}
			require.NoError(t, db.Where("token_ref = ?", model.AccessTokenFingerprint(raw)).Delete(&model.AuditLog{}).Error)
		})
	}

}
