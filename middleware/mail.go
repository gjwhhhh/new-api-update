package middleware

import (
	"github.com/gin-gonic/gin"
	"net/http"
)

// MailSessionRequired prevents bulk email access through PATs or relay keys.
func MailSessionRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		if _, ok := GetSessionAuthIdentity(c); !ok {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"success": false, "message": "A browser login session is required"})
			return
		}
		c.Next()
	}
}

// MailWriteOriginGuard applies in development too: email changes never accept a
// missing/untrusted browser Origin or Referer, even with insecure local cookies.
func MailWriteOriginGuard() gin.HandlerFunc {
	return func(c *gin.Context) {
		origin, ok := requestBrowserOrigin(c.Request)
		if !ok || !isAllowedSessionOrigin(c.Request, origin) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"success": false, "message": "Request origin is not allowed"})
			return
		}
		if c.ContentType() != "application/json" {
			c.AbortWithStatusJSON(http.StatusUnsupportedMediaType, gin.H{"success": false, "message": "JSON content type is required"})
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 512*1024)
		c.Next()
	}
}
