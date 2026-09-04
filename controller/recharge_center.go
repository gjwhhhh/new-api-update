package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

// GetRechargeCenterSetting returns the effective recharge-center setting for
// the authenticated user. It intentionally does not include user identity or
// credentials for the configured external site.
func GetRechargeCenterSetting(c *gin.Context) {
	setting := operation_setting.GetRechargeCenterSetting()
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    setting,
	})
}
