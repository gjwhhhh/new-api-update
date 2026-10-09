package conversationaudit

import (
	_ "embed"
	"net/http"

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
	router.GET("/conversation-audit/", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Data(http.StatusOK, "text/html; charset=utf-8", consoleIndex)
	})
}
