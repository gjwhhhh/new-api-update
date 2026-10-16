package controller

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	relaydto "github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func userSessionAffinityDB(t *testing.T) *gorm.DB {
	t.Helper()
	previousMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(previousMode) })
	require.NoError(t, i18n.Init())
	db := modelManagementDB(t, "sqlite", "")
	require.NoError(t, db.AutoMigrate(&model.UserSessionAffinity{}, &model.UserChannelPriority{}))
	connection, err := db.DB()
	require.NoError(t, err)
	connection.SetMaxOpenConns(1)
	return db
}

func userSessionAffinityRequest(t *testing.T, method, path, body string, role int) *httptest.ResponseRecorder {
	t.Helper()
	engine := gin.New()
	engine.Use(func(c *gin.Context) { c.Set("id", 1); c.Set("role", role) })
	engine.Use(middleware.RequirePermission(authz.ChannelWrite))
	engine.GET("/api/user/session-affinity", SearchUserSessionAffinities)
	engine.GET("/api/user/:id/session-affinity", GetUserSessionAffinity)
	engine.PATCH("/api/user/:id/session-affinity", UpdateUserSessionAffinity)
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)
	return recorder
}

func TestUserSessionAffinityManagementSQLite(t *testing.T) {
	db := userSessionAffinityDB(t)
	alice := model.User{Username: "alice-affinity", Password: "unused", Setting: `{"language":"ja"}`}
	bob := model.User{Username: "bob-affinity", Password: "unused", AffCode: "bobaff"}
	require.NoError(t, db.Create(&alice).Error)
	require.NoError(t, db.Create(&bob).Error)
	path := fmt.Sprintf("/api/user/%d/session-affinity", alice.Id)
	var response struct {
		Success bool                        `json:"success"`
		Data    userSessionAffinityResponse `json:"data"`
	}
	recorder := userSessionAffinityRequest(t, http.MethodGet, path, "", common.RoleRootUser)
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	assert.False(t, response.Data.Disabled)
	assert.Zero(t, response.Data.Version)

	for _, body := range []string{`{}`, `{"disabled":true}`, `{"disabled":true,"version":-1}`, `{"disabled":true,"version":"0"}`} {
		assert.Equal(t, http.StatusBadRequest, userSessionAffinityRequest(t, http.MethodPatch, path, body, common.RoleRootUser).Code)
	}
	for _, endpoint := range []struct{ method, path, body string }{
		{http.MethodGet, path, ""},
		{http.MethodPatch, path, `{"disabled":true,"version":0}`},
		{http.MethodGet, "/api/user/session-affinity?disabled_only=false", ""},
	} {
		assert.Equal(t, http.StatusForbidden, userSessionAffinityRequest(t, endpoint.method, endpoint.path, endpoint.body, common.RoleCommonUser).Code)
	}

	recorder = userSessionAffinityRequest(t, http.MethodPatch, path, `{"disabled":true,"version":0}`, common.RoleRootUser)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success, recorder.Body.String())
	assert.True(t, response.Data.Disabled)
	assert.EqualValues(t, 1, response.Data.Version)
	assert.Equal(t, http.StatusConflict, userSessionAffinityRequest(t, http.MethodPatch, path, `{"disabled":false,"version":0}`, common.RoleRootUser).Code)

	// A second database connection represents another instance, with no shared policy cache.
	otherDB, err := gorm.Open(sqlite.Open(common.SQLitePath), &gorm.Config{})
	require.NoError(t, err)
	model.DB = otherDB
	current, err := model.GetUserSessionAffinity(alice.Id)
	model.DB = db
	connection, closeErr := otherDB.DB()
	require.NoError(t, closeErr)
	require.NoError(t, connection.Close())
	require.NoError(t, err)
	assert.True(t, current.Disabled)

	for _, query := range []struct {
		params string
		total  int64
	}{
		{"", 1}, {"?keyword=alice", 1}, {"?keyword=" + strconv.Itoa(alice.Id), 1},
		{"?keyword=missing", 0}, {"?disabled_only=false&page_size=1", 2},
	} {
		recorder = userSessionAffinityRequest(t, http.MethodGet, "/api/user/session-affinity"+query.params, "", common.RoleRootUser)
		var list struct {
			Data struct {
				Items []model.UserSessionAffinityDetail `json:"items"`
				Total int64                             `json:"total"`
			} `json:"data"`
		}
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &list))
		assert.Equal(t, query.total, list.Data.Total, recorder.Body.String())
		if strings.Contains(query.params, "page_size=1") {
			assert.Len(t, list.Data.Items, 1)
		}
		assert.NotContains(t, recorder.Body.String(), "password")
		assert.NotContains(t, recorder.Body.String(), "setting")
	}

	require.NoError(t, model.UpdateUserSetting(alice.Id, relaydto.UserSetting{Language: "fr"}))
	current, err = model.GetUserSessionAffinity(alice.Id)
	require.NoError(t, err)
	assert.True(t, current.Disabled)
	recorder = userSessionAffinityRequest(t, http.MethodPatch, path, `{"disabled":false,"version":1}`, common.RoleRootUser)
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.False(t, response.Data.Disabled)
	assert.EqualValues(t, 2, response.Data.Version)
	var reloaded model.User
	require.NoError(t, db.First(&reloaded, alice.Id).Error)
	assert.Equal(t, "fr", reloaded.GetSetting().Language)
	var audits []model.AuditLog
	require.NoError(t, db.Where("action = ?", "user.session_affinity.update").Find(&audits).Error)
	require.Len(t, audits, 2)
	params, err := common.Marshal(audits[0].Other.Op.Params)
	require.NoError(t, err)
	assert.JSONEq(t, fmt.Sprintf(`{"target_user_id":%d,"previous_disabled":false,"disabled":true}`, alice.Id), string(params))

	require.NoError(t, db.Delete(&alice).Error)
	assert.Equal(t, http.StatusNotFound, userSessionAffinityRequest(t, http.MethodGet, path, "", common.RoleRootUser).Code)
	assert.Equal(t, http.StatusNotFound, userSessionAffinityRequest(t, http.MethodPatch, path, `{"disabled":true,"version":2}`, common.RoleRootUser).Code)
	rows, total, err := model.SearchUserSessionAffinities("", false, 0, 20)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	require.Len(t, rows, 1)
	assert.Equal(t, bob.Id, rows[0].UserId)
}

func TestUserSessionAffinitySQLiteMigrationAndConflicts(t *testing.T) {
	db := userSessionAffinityDB(t)
	// Upgrade an existing users database without changing its schema or settings.
	require.NoError(t, db.Migrator().DropTable(&model.UserSessionAffinity{}))
	user := model.User{Username: "upgrade-affinity", Password: "unused", Setting: `{"language":"zh"}`}
	require.NoError(t, db.Create(&user).Error)
	for range 2 {
		require.NoError(t, db.AutoMigrate(&model.UserSessionAffinity{}))
	}
	current, err := model.GetUserSessionAffinity(user.Id)
	require.NoError(t, err)
	assert.False(t, current.Disabled)
	assert.Zero(t, current.Version)

	errorsByWriter := make([]error, 2)
	var writers sync.WaitGroup
	for i := range errorsByWriter {
		writers.Go(func() { _, _, errorsByWriter[i] = model.UpdateUserSessionAffinity(user.Id, true, 0) })
	}
	writers.Wait()
	successes, conflicts := 0, 0
	for _, writeErr := range errorsByWriter {
		if writeErr == nil {
			successes++
		} else {
			require.ErrorIs(t, writeErr, model.ErrUserSessionAffinityConflict)
			conflicts++
		}
	}
	assert.Equal(t, 1, successes)
	assert.Equal(t, 1, conflicts)
	for range 2 {
		require.NoError(t, db.AutoMigrate(&model.UserSessionAffinity{}))
	}
	current, err = model.GetUserSessionAffinity(user.Id)
	require.NoError(t, err)
	assert.True(t, current.Disabled)
	assert.EqualValues(t, 1, current.Version)
	_, _, err = model.UpdateUserSessionAffinity(user.Id, false, 1)
	require.NoError(t, err)
	_, _, err = model.UpdateUserSessionAffinity(user.Id, true, 0)
	assert.ErrorIs(t, err, model.ErrUserSessionAffinityConflict, "restoring inheritance must not reset the version")
	duplicate := model.UserSessionAffinity{UserId: user.Id, Disabled: true, Version: 10}
	assert.Error(t, db.Create(&duplicate).Error, "one policy per user")
	var restored model.User
	require.NoError(t, db.First(&restored, user.Id).Error)
	assert.Equal(t, user.Setting, restored.Setting)
}

func userAffinityRoutingContext(userId int, session string) *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Request.Header.Set("X-Session", session)
	common.SetContextKey(c, constant.ContextKeyUserId, userId)
	common.SetContextKey(c, constant.ContextKeyUsingGroup, "default")
	common.SetContextKey(c, constant.ContextKeyUserGroup, "default")
	return c
}

func TestUserSessionAffinityRoutingPreservesTemplatesBindingsAndPins(t *testing.T) {
	db := userSessionAffinityDB(t)
	user := model.User{Username: "routing-affinity", Password: "unused"}
	control := model.User{Username: "control-affinity", Password: "unused", AffCode: "ctrlaff"}
	require.NoError(t, db.Create(&user).Error)
	require.NoError(t, db.Create(&control).Error)
	affinity := operation_setting.GetChannelAffinitySetting()
	previous := *affinity
	previousCodes := operation_setting.AutomaticRetryStatusCodesToString()
	t.Cleanup(func() {
		*affinity = previous
		require.NoError(t, operation_setting.AutomaticRetryStatusCodesFromString(previousCodes))
	})
	require.NoError(t, operation_setting.AutomaticRetryStatusCodesFromString("500"))
	channels := []model.Channel{
		{Name: "bound", Type: 1, Key: "unused", Status: common.ChannelStatusEnabled, Models: "affinity-model", Group: "default", Priority: common.GetPointer(int64(1))},
		{Name: "normal", Type: 1, Key: "unused", Status: common.ChannelStatusEnabled, Models: "affinity-model", Group: "default", Priority: common.GetPointer(int64(10))},
	}
	for i := range channels {
		channels[i].SetSetting(relaydto.ChannelSettings{ResponsesWebSocketEnabled: true})
		require.NoError(t, db.Create(&channels[i]).Error)
		require.NoError(t, channels[i].AddAbilities(db))
	}
	for _, ruleMode := range []string{"strict", "inherit", ""} {
		t.Run("mode="+ruleMode, func(t *testing.T) {
			*affinity = operation_setting.ChannelAffinitySetting{Enabled: true, SessionMode: "strict", DefaultTTLSeconds: 3600, Rules: []operation_setting.ChannelAffinityRule{{
				Name: t.Name(), ModelRegex: []string{".*"}, KeySources: []operation_setting.ChannelAffinityKeySource{{Type: "request_header", Key: "X-Session"}},
				SessionMode: ruleMode, SkipRetryOnFailure: true, IncludeRuleName: true, ParamOverrideTemplate: map[string]any{"temperature": 0},
			}}}
			seed := userAffinityRoutingContext(0, t.Name())
			_, found := service.GetPreferredChannelByAffinity(seed, "affinity-model", "default")
			require.False(t, found)
			service.RecordChannelAffinity(seed, channels[0].Id)
			t.Cleanup(func() { service.ClearCurrentChannelAffinityCache(seed) })
			current, err := model.GetUserSessionAffinity(user.Id)
			require.NoError(t, err)
			_, _, err = model.UpdateUserSessionAffinity(user.Id, true, current.Version)
			require.NoError(t, err)
			request := userAffinityRoutingContext(user.Id, t.Name())
			channel, _, selectErr := service.SelectChannelForRequest(request, "affinity-model", &service.RetryParam{Ctx: request, TokenGroup: "default", ModelName: "affinity-model"})
			require.Nil(t, selectErr)
			require.NotNil(t, channel)
			assert.Equal(t, channels[1].Id, channel.Id)
			assert.True(t, slices.ContainsFunc(service.RequestPolicy(request).Events(), func(event service.PolicyEvent) bool { return event.Decision.Reason == "session_disabled_for_user" }))
			template, applied := service.ApplyChannelAffinityOverrideTemplate(request, nil)
			assert.True(t, applied)
			assert.EqualValues(t, 0, template["temperature"])
			upstreamErr := types.NewOpenAIError(errors.New("upstream failed"), types.ErrorCodeBadResponseStatusCode, 500)
			assert.True(t, service.ShouldRetryRelayError(request, upstreamErr, 1))
			assert.False(t, service.ShouldRetryRelayError(request, upstreamErr, 0))
			service.RecordChannelAffinity(request, channels[1].Id)
			assert.False(t, service.ClearCurrentChannelAffinityCache(request))
			bound, found := service.GetPreferredChannelByAffinity(seed, "affinity-model", "default")
			require.True(t, found)
			assert.Equal(t, channels[0].Id, bound)
			// A disabled user must also ignore an unavailable strict binding,
			// without clearing the shared entry used by other users.
			require.NoError(t, db.Model(&channels[0]).Update("status", common.ChannelStatusAutoDisabled).Error)
			require.NoError(t, db.Model(&model.Ability{}).Where("channel_id = ?", channels[0].Id).Update("enabled", false).Error)
			unavailable := userAffinityRoutingContext(user.Id, t.Name())
			channel, _, selectErr = service.SelectChannelForRequest(unavailable, "affinity-model", &service.RetryParam{Ctx: unavailable, TokenGroup: "default", ModelName: "affinity-model"})
			require.Nil(t, selectErr)
			require.NotNil(t, channel)
			assert.Equal(t, channels[1].Id, channel.Id)
			bound, found = service.GetPreferredChannelByAffinity(seed, "affinity-model", "default")
			require.True(t, found)
			assert.Equal(t, channels[0].Id, bound)
			require.NoError(t, db.Model(&channels[0]).Update("status", common.ChannelStatusEnabled).Error)
			require.NoError(t, db.Model(&model.Ability{}).Where("channel_id = ?", channels[0].Id).Update("enabled", true).Error)

			other := userAffinityRoutingContext(control.Id, t.Name())
			channel, _, selectErr = service.SelectChannelForRequest(other, "affinity-model", &service.RetryParam{Ctx: other, TokenGroup: "default", ModelName: "affinity-model"})
			require.Nil(t, selectErr)
			assert.Equal(t, channels[0].Id, channel.Id)
			assert.False(t, service.ShouldRetryRelayError(other, upstreamErr, 1))

			current, err = model.GetUserSessionAffinity(user.Id)
			require.NoError(t, err)
			_, _, err = model.UpdateUserSessionAffinity(user.Id, false, current.Version)
			require.NoError(t, err)
			assert.True(t, service.RequestPolicy(request).UserSessionAffinityDisabled, "an in-flight request keeps its snapshot")
			restored := userAffinityRoutingContext(user.Id, t.Name())
			channel, _, selectErr = service.SelectChannelForRequest(restored, "affinity-model", &service.RetryParam{Ctx: restored, TokenGroup: "default", ModelName: "affinity-model"})
			require.Nil(t, selectErr)
			assert.Equal(t, channels[0].Id, channel.Id)
		})
	}
	current, err := model.GetUserSessionAffinity(user.Id)
	require.NoError(t, err)
	_, _, err = model.UpdateUserSessionAffinity(user.Id, true, current.Version)
	require.NoError(t, err)
	websocketRequest := userAffinityRoutingContext(user.Id, "new-websocket")
	websocketRequest.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	service.GetChannelConstraints(websocketRequest).AddFilter(dto.ChannelFilter{Kind: dto.FilterResponsesWebSocket})
	channel, _, selectErr := service.SelectChannelForRequest(websocketRequest, "affinity-model", &service.RetryParam{Ctx: websocketRequest, TokenGroup: "default", ModelName: "affinity-model"})
	require.Nil(t, selectErr)
	require.NotNil(t, channel)
	assert.Equal(t, channels[1].Id, channel.Id)
	assert.Equal(t, "off", service.RequestPolicy(websocketRequest).SessionMode)

	require.NoError(t, model.PatchUserChannelPriorities(user.Id, []model.UserChannelPriority{{ChannelId: channels[0].Id, Priority: 100}}, nil))
	prioritized := userAffinityRoutingContext(user.Id, "priority")
	channel, _, selectErr = service.SelectChannelForRequest(prioritized, "affinity-model", &service.RetryParam{Ctx: prioritized, TokenGroup: "default", ModelName: "affinity-model"})
	require.Nil(t, selectErr)
	require.NotNil(t, channel)
	assert.Equal(t, channels[0].Id, channel.Id, "user priorities still apply when affinity is disabled")
	pinned := userAffinityRoutingContext(user.Id, "pin")
	service.GetChannelConstraints(pinned).AddPin(dto.ChannelPin{ChannelId: channels[1].Id, Source: dto.PinSourceToken, Rank: dto.PinRankToken, RetryMode: dto.PinRetrySingleAttempt})
	channel, _, selectErr = service.SelectChannelForRequest(pinned, "affinity-model", &service.RetryParam{Ctx: pinned, TokenGroup: "default", ModelName: "affinity-model"})
	require.Nil(t, selectErr)
	assert.Equal(t, channels[1].Id, channel.Id)
	assert.False(t, service.ShouldRetryRelayError(pinned, types.NewOpenAIError(errors.New("failure"), types.ErrorCodeBadResponseStatusCode, 500), 1))
}
