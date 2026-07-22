package conversationaudit

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
)

func TestAuditConsoleRequiresAdminSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(sessions.Sessions("session", cookie.NewStore([]byte("conversation-audit-web-test"))))
	router.GET("/test-login", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("role", common.RoleAdminUser)
		session.Set("status", common.UserStatusEnabled)
		if err := session.Save(); err != nil {
			t.Fatalf("save session: %v", err)
		}
		c.Status(http.StatusNoContent)
	})
	RegisterWebRoutes(router)

	unauthorized := httptest.NewRecorder()
	router.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/conversation-audit/", nil))
	if unauthorized.Code != http.StatusForbidden {
		t.Fatalf("unauthorized status = %d, want %d", unauthorized.Code, http.StatusForbidden)
	}

	login := httptest.NewRecorder()
	router.ServeHTTP(login, httptest.NewRequest(http.MethodGet, "/test-login", nil))

	authorizedRequest := httptest.NewRequest(http.MethodGet, "/conversation-audit/", nil)
	for _, sessionCookie := range login.Result().Cookies() {
		authorizedRequest.AddCookie(sessionCookie)
	}
	authorized := httptest.NewRecorder()
	router.ServeHTTP(authorized, authorizedRequest)
	if authorized.Code != http.StatusOK {
		t.Fatalf("authorized status = %d, want %d", authorized.Code, http.StatusOK)
	}
	if !strings.Contains(authorized.Body.String(), "加密会话审计") {
		t.Fatal("audit console page content is missing")
	}

	redirect := httptest.NewRecorder()
	router.ServeHTTP(redirect, httptest.NewRequest(http.MethodGet, "/conversation-audit", nil))
	if redirect.Code != http.StatusPermanentRedirect || redirect.Header().Get("Location") != "/conversation-audit/" {
		t.Fatalf("redirect = %d %q, want %d %q", redirect.Code, redirect.Header().Get("Location"), http.StatusPermanentRedirect, "/conversation-audit/")
	}
}
