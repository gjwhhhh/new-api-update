package router

import (
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
	"net/http"
)

func registerMailRoutes(api *gin.RouterGroup) {
	mail := api.Group("/mail", middleware.AdminAuth(), middleware.MailSessionRequired(), middleware.DisableCache())
	mail.GET("/settings", middleware.RequirePermission(authz.MailRead), controller.GetMailSettings)
	mail.GET("/records", middleware.RequirePermission(authz.MailRead), controller.ListEmailRecords)
	mail.GET("/records/:id", middleware.RequirePermission(authz.MailRead), controller.GetEmailRecord)
	mail.GET("/batches", middleware.RequirePermission(authz.MailRead), controller.ListEmailBatches)
	mail.GET("/batches/:id", middleware.RequirePermission(authz.MailRead), controller.GetEmailBatch)
	mail.GET("/recipients", middleware.RequirePermission(authz.MailWrite), controller.GetEmailRecipients)
	write := mail.Group("", middleware.MailWriteOriginGuard(), middleware.CriticalRateLimit(), middleware.UserCriticalRateLimit("mail"))
	write.POST("/batches/preview", middleware.RequirePermission(authz.MailWrite), controller.PreviewEmailBatch)
	write.POST("/batches/:id/submit", middleware.RequirePermission(authz.MailWrite), controller.SubmitEmailBatch)
	write.POST("/test", middleware.RequirePermission(authz.MailWrite), controller.TestEmail)
	write.POST("/batches/:id/:action", middleware.RequirePermission(authz.MailOperate), controller.OperateEmailBatch)
	write.Handle(http.MethodPut, "/settings", middleware.RootAuth(), controller.UpdateMailSettings)
}
