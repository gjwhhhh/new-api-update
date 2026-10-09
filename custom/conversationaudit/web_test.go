package conversationaudit

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuditConsoleBootstrapsCurrentDashboardSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterWebRoutes(router)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/conversation-audit/", nil))
	require.Equal(t, http.StatusOK, response.Code)
	assert.Contains(t, response.Body.String(), "加密会话审计")
	assert.Contains(t, response.Body.String(), "/api/user/auth/refresh")
	assert.Contains(t, response.Body.String(), "Authorization")
	assert.True(t, strings.Contains(response.Header().Get("Cache-Control"), "no-store"))

	redirect := httptest.NewRecorder()
	router.ServeHTTP(redirect, httptest.NewRequest(http.MethodGet, "/conversation-audit", nil))
	require.Equal(t, http.StatusPermanentRedirect, redirect.Code)
	assert.Equal(t, "/conversation-audit/", redirect.Header().Get("Location"))
}
