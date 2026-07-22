package conversationaudit

import (
	_ "embed"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

//go:embed console/index.html
var consoleIndex []byte

// RegisterWebRoutes serves the self-contained audit console before the
// frontend SPA fallback can claim the path. Keeping the page in this package
// avoids a separate container and preserves the custom extension boundary.
func RegisterWebRoutes(router *gin.Engine) {
	router.GET("/conversation-audit", func(c *gin.Context) {
		c.Redirect(http.StatusPermanentRedirect, "/conversation-audit/")
	})
	router.GET("/conversation-audit/", requireAdminConsoleSession(), func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Data(http.StatusOK, "text/html; charset=utf-8", consoleIndex)
	})
}

// requireAdminConsoleSession intentionally checks only the signed browser
// session. API routes still use AdminAuth and require New-Api-User, whereas a
// document navigation cannot attach that browser-only header.
func requireAdminConsoleSession() gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)
		role, roleOK := session.Get("role").(int)
		status, statusOK := session.Get("status").(int)
		if !roleOK || !statusOK || role < common.RoleAdminUser || status != common.UserStatusEnabled {
			c.String(http.StatusForbidden, "管理员权限不足，无法访问会话审计页面。")
			c.Abort()
			return
		}
		c.Next()
	}
}
