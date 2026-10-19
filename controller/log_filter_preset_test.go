package controller

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func logPresetTestDatabase(t *testing.T) (*gorm.DB, string) {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "presets.db") + "?_pragma=busy_timeout(30000)&_pragma=journal_mode(WAL)&_txlock=immediate"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	previousDB, previousLogDB, previousRedis := model.DB, model.LOG_DB, common.RedisEnabled
	previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
	model.DB, model.LOG_DB, common.RedisEnabled = db, db, false
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		model.DB, model.LOG_DB, common.RedisEnabled = previousDB, previousLogDB, previousRedis
		common.SetDatabaseTypes(previousMain, previousLog)
		connection, err := db.DB()
		require.NoError(t, err)
		require.NoError(t, connection.Close())
	})
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.UserLogFilterPreset{}, &model.UserLogFilterPreference{}))
	for _, user := range []model.User{{Id: 1, Username: "preset-owner", AffCode: "owner", Password: "unused", AuthVersion: 1, Setting: `{"language":"ja"}`}, {Id: 2, Username: "other-owner", AffCode: "other", Password: "unused", AuthVersion: 1}} {
		require.NoError(t, db.Create(&user).Error)
	}
	var version string
	require.NoError(t, db.Raw("SELECT sqlite_version()").Scan(&version).Error)
	t.Log("SQLite", version)
	return db, dsn
}

func logPresetRequest(t *testing.T, userId, role int, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	engine := gin.New()
	engine.Use(func(c *gin.Context) { c.Set("id", userId); c.Set("role", role) })
	engine.GET("/presets", ListLogFilterPresets)
	engine.GET("/presets/:id", GetLogFilterPreset)
	engine.POST("/presets", SaveLogFilterPreset)
	engine.PATCH("/presets/:id", SaveLogFilterPreset)
	engine.DELETE("/presets/:id", DeleteLogFilterPreset)
	engine.PUT("/default", SetDefaultLogFilterPreset)
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	return response
}

func TestLogFilterPresetAPIIsolationValidationAndDefaults(t *testing.T) {
	db, _ := logPresetTestDatabase(t)
	assert.Equal(t, http.StatusUnauthorized, logPresetRequest(t, 0, 0, "GET", "/presets", "").Code)
	for _, method := range []string{"GET", "POST"} {
		assert.Equal(t, http.StatusForbidden, logPresetRequest(t, 1, common.RoleCommonUser, method, "/presets?scope=all", `{}`).Code)
	}
	for _, body := range []string{
		`{"name":"","filters":{}}`, `{"name":"bad","filters":{"startTime":"1"}}`, `{"name":"bad","filters":{"type":"8"}}`,
		`{"name":"bad","filters":{"channel":"1"}}`, `{"name":"bad","filters":{"model":2}}`, `{"name":"bad","filters":null}`,
		fmt.Sprintf(`{"name":"%s","filters":{}}`, strings.Repeat("界", 41)),
		fmt.Sprintf(`{"name":"bad","filters":{"model":"%s"}}`, strings.Repeat("a", 257)),
	} {
		assert.Equal(t, http.StatusBadRequest, logPresetRequest(t, 1, common.RoleCommonUser, "POST", "/presets", body).Code, body)
	}
	response := logPresetRequest(t, 1, common.RoleCommonUser, "POST", "/presets", `{"user_id":2,"name":"  Work  ","filters":{"model":"gpt-test","type":"2"}}`)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var created struct {
		Success bool                      `json:"success"`
		Data    model.LogFilterPresetView `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &created))
	require.True(t, created.Success)
	preset := created.Data
	assert.Equal(t, "Work", preset.Name)
	assert.EqualValues(t, 1, preset.Version)
	assert.Equal(t, "gpt-test", preset.Content["model"])
	assert.NotContains(t, response.Body.String(), "user_id")
	assert.Equal(t, http.StatusConflict, logPresetRequest(t, 1, common.RoleCommonUser, "POST", "/presets", `{"name":"work","filters":{}}`).Code)
	assert.Equal(t, http.StatusOK, logPresetRequest(t, 2, common.RoleCommonUser, "POST", "/presets", `{"name":"Work","filters":{}}`).Code)
	path := fmt.Sprintf("/presets/%d", preset.Id)
	for _, item := range []struct{ method, body string }{{"GET", ""}, {"PATCH", `{"name":"Stolen","version":1}`}, {"DELETE", `{"version":1}`}} {
		assert.Equal(t, http.StatusNotFound, logPresetRequest(t, 2, common.RoleCommonUser, item.method, path, item.body).Code)
	}
	assert.Equal(t, http.StatusNotFound, logPresetRequest(t, 2, common.RoleCommonUser, "PUT", "/default", fmt.Sprintf(`{"default_preset_id":%d,"version":0}`, preset.Id)).Code)
	assert.Equal(t, http.StatusNotFound, logPresetRequest(t, 1, common.RoleAdminUser, "GET", path+"?scope=all", "").Code)
	response = logPresetRequest(t, 1, common.RoleCommonUser, "PUT", "/default", fmt.Sprintf(`{"default_preset_id":%d,"version":0}`, preset.Id))
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	assert.Equal(t, http.StatusConflict, logPresetRequest(t, 1, common.RoleCommonUser, "PUT", "/default", `{"default_preset_id":null,"version":0}`).Code)
	assert.Equal(t, http.StatusBadRequest, logPresetRequest(t, 1, common.RoleCommonUser, "PUT", "/default", `{"version":1}`).Code)
	response = logPresetRequest(t, 1, common.RoleCommonUser, "PATCH", path, `{"filters":{},"version":1}`)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	created.Data = model.LogFilterPresetView{}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &created))
	assert.Empty(t, created.Data.Content)
	assert.EqualValues(t, 2, created.Data.Version)
	assert.Equal(t, http.StatusConflict, logPresetRequest(t, 1, common.RoleCommonUser, "DELETE", path, `{"version":1}`).Code)
	assert.Equal(t, http.StatusOK, logPresetRequest(t, 1, common.RoleCommonUser, "DELETE", path, `{"version":2}`).Code)
	collection, err := model.ListLogFilterPresets(1, "common", "self")
	require.NoError(t, err)
	assert.Empty(t, collection.Items)
	assert.Nil(t, collection.Preference.DefaultPresetId)
	assert.EqualValues(t, 2, collection.Preference.Version)
	assert.Equal(t, http.StatusNotFound, logPresetRequest(t, 1, common.RoleCommonUser, "PATCH", path, `{"name":"Recreate","version":2}`).Code)
	var user model.User
	require.NoError(t, db.First(&user, 1).Error)
	assert.Equal(t, `{"language":"ja"}`, user.Setting)
}

func TestLogFilterPresetSQLiteMigrationConcurrencyAndPersistence(t *testing.T) {
	db, dsn := logPresetTestDatabase(t)
	// The new tables upgrade an existing user database independently.
	require.NoError(t, db.Migrator().DropTable(&model.UserLogFilterPreset{}, &model.UserLogFilterPreference{}))
	for range 2 {
		require.NoError(t, db.AutoMigrate(&model.UserLogFilterPreset{}, &model.UserLogFilterPreference{}))
	}
	name, filters := "Preset", model.LogPresetFilters{"model": "one"}
	created, err := model.SaveLogFilterPreset(1, 0, "common", "self", 0, &name, &filters)
	require.NoError(t, err)
	_, err = model.SetDefaultLogFilterPreset(1, "common", "self", &created.Id, 0)
	require.NoError(t, err)
	var writers sync.WaitGroup
	results := make([]error, 2)
	for i := range results {
		writers.Go(func() {
			next := model.LogPresetFilters{"model": fmt.Sprintf("writer-%d", i)}
			_, results[i] = model.SaveLogFilterPreset(1, created.Id, "common", "self", 1, nil, &next)
		})
	}
	writers.Wait()
	winners, conflicts := 0, 0
	for _, result := range results {
		if result == nil {
			winners++
		} else {
			require.ErrorIs(t, result, model.ErrLogPresetConflict)
			conflicts++
		}
	}
	assert.Equal(t, 1, winners)
	assert.Equal(t, 1, conflicts)
	for range 2 {
		require.NoError(t, db.AutoMigrate(&model.UserLogFilterPreset{}, &model.UserLogFilterPreference{}))
	}
	other, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB = other
	read, err := model.GetLogFilterPreset(1, created.Id, "common", "self")
	model.DB = db
	require.NoError(t, err)
	assert.EqualValues(t, 2, read.Version)
	assert.Contains(t, []string{"writer-0", "writer-1"}, read.Content["model"])
	connection, err := other.DB()
	require.NoError(t, err)
	require.NoError(t, connection.Close())
	duplicate := model.UserLogFilterPreset{UserId: 1, Category: "common", Scope: "self", Name: "duplicate", NameKey: created.NameKey, Filters: "{}", Version: 1}
	assert.Error(t, db.Create(&duplicate).Error)
	for i := 1; i < model.MaxLogFilterPresets; i++ {
		nextName := fmt.Sprintf("preset-%d", i)
		_, err := model.SaveLogFilterPreset(1, 0, "common", "self", 0, &nextName, &filters)
		require.NoError(t, err)
	}
	lastName := "over-limit"
	_, err = model.SaveLogFilterPreset(1, 0, "common", "self", 0, &lastName, &filters)
	assert.ErrorIs(t, err, model.ErrLogPresetLimit)
	_, err = model.SaveLogFilterPreset(1, 0, "common", "all", 0, &lastName, &filters)
	require.NoError(t, err, "limits and names are isolated by effective scope")
}

func TestLogFilterPresetAccountDeletionCleanup(t *testing.T) {
	db := setupManageUserTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.UserLogFilterPreset{}, &model.UserLogFilterPreference{}))
	user := model.User{Username: "delete-presets", Password: "unused", AuthVersion: 1}
	require.NoError(t, db.Create(&user).Error)
	name, filters := "Personal", model.LogPresetFilters{}
	preset, err := model.SaveLogFilterPreset(user.Id, 0, "common", "self", 0, &name, &filters)
	require.NoError(t, err)
	_, err = model.SetDefaultLogFilterPreset(user.Id, "common", "self", &preset.Id, 0)
	require.NoError(t, err)
	_, err = model.DeleteUserForSession(model.AuthSessionIdentity{UserID: user.Id})
	require.Error(t, err)
	_, err = model.GetLogFilterPreset(user.Id, preset.Id, "common", "self")
	require.NoError(t, err, "failed session validation must not delete preferences")
	_, err = user.Delete()
	require.NoError(t, err)
	var count int64
	require.NoError(t, db.Model(&model.UserLogFilterPreset{}).Where("user_id = ?", user.Id).Count(&count).Error)
	assert.Zero(t, count)
	require.NoError(t, db.Model(&model.UserLogFilterPreference{}).Where("user_id = ?", user.Id).Count(&count).Error)
	assert.Zero(t, count)
}

func TestLogFilterPresetAcrossAuthenticatedClients(t *testing.T) {
	db, _ := logPresetTestDatabase(t)
	require.NoError(t, db.AutoMigrate(&model.UserSession{}))
	previousSecret := common.SessionSecret
	common.SessionSecret = "log-preset-client-test-secret"
	t.Cleanup(func() { common.SessionSecret = previousSecret })
	first, err := service.CreateLoginSession(1, "password", "127.0.0.1", "client-one")
	require.NoError(t, err)
	second, err := service.CreateLoginSession(1, "password", "127.0.0.1", "client-two")
	require.NoError(t, err)
	other, err := service.CreateLoginSession(2, "password", "127.0.0.1", "other-account")
	require.NoError(t, err)
	engine := gin.New()
	engine.Use(middleware.UserAuth())
	engine.GET("/api/user/self/log-filter-presets", ListLogFilterPresets)
	engine.GET("/api/user/self/log-filter-presets/:id", GetLogFilterPreset)
	engine.POST("/api/user/self/log-filter-presets", SaveLogFilterPreset)
	server := httptest.NewServer(engine)
	t.Cleanup(server.Close)
	firstClient, secondClient := &http.Client{}, &http.Client{}
	request, err := http.NewRequest(http.MethodPost, server.URL+"/api/user/self/log-filter-presets", strings.NewReader(`{"name":"Cross device","filters":{"model":"shared-model"}}`))
	require.NoError(t, err)
	request.Header.Set("Authorization", "Bearer "+first.AccessToken)
	request.Header.Set("Content-Type", "application/json")
	response, err := firstClient.Do(request)
	require.NoError(t, err)
	body, err := io.ReadAll(response.Body)
	require.NoError(t, response.Body.Close())
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.StatusCode, string(body))
	var created struct {
		Success bool                      `json:"success"`
		Data    model.LogFilterPresetView `json:"data"`
	}
	require.NoError(t, common.Unmarshal(body, &created))
	require.True(t, created.Success, string(body))
	for _, tc := range []struct {
		name, token string
		status      int
	}{
		{"same account, different session", second.AccessToken, http.StatusOK},
		{"other account", other.AccessToken, http.StatusNotFound},
		{"anonymous", "", http.StatusUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/user/self/log-filter-presets/%d", server.URL, created.Data.Id), nil)
			require.NoError(t, err)
			if tc.token != "" {
				request.Header.Set("Authorization", "Bearer "+tc.token)
			}
			response, err := secondClient.Do(request)
			require.NoError(t, err)
			body, err := io.ReadAll(response.Body)
			require.NoError(t, response.Body.Close())
			require.NoError(t, err)
			assert.Equal(t, tc.status, response.StatusCode, string(body))
			if tc.status == http.StatusOK {
				assert.Contains(t, string(body), "shared-model")
			} else {
				assert.NotContains(t, string(body), "shared-model")
			}
		})
	}
}
