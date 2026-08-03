package conversationaudit

import (
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func Register(apiRouter *gin.RouterGroup) {
	initOnce.Do(initialize)
	registerRoutes(apiRouter)
}

func registerRoutes(apiRouter *gin.RouterGroup) {
	auditRoute := apiRouter.Group("/custom/conversation-audits")
	auditRoute.Use(middleware.AdminAuth())
	{
		auditRoute.GET("", listAudits)
		auditRoute.GET("/:request_id", getAuditDetail)
	}

	rootAuditRoute := apiRouter.Group("/custom/conversation-audits")
	rootAuditRoute.Use(middleware.RootAuth())
	{
		rootAuditRoute.DELETE("/:request_id", deleteAudit)
	}

	settingsRoute := apiRouter.Group("/custom/conversation-audit")
	settingsRoute.Use(middleware.RootAuth())
	{
		settingsRoute.GET("/settings", getSettings)
		settingsRoute.PUT("/settings", updateSettings)
		settingsRoute.POST("/rotate-key", rotateKey)
	}
}

func listAudits(c *gin.Context) {
	query := model.DB.Model(&ConversationAudit{})
	if username := c.Query("username"); username != "" {
		query = query.Where("username LIKE ?", "%"+username+"%")
	}
	if userID, err := strconv.Atoi(c.Query("user_id")); err == nil && userID > 0 {
		query = query.Where("user_id = ?", userID)
	}
	if modelName := c.Query("model"); modelName != "" {
		query = query.Where("model_name = ?", modelName)
	}
	if status := c.Query("status"); status != "" {
		query = query.Where("status = ?", status)
	}
	if start, err := strconv.ParseInt(c.Query("start_timestamp"), 10, 64); err == nil && start > 0 {
		query = query.Where("created_at >= ?", start)
	}
	if end, err := strconv.ParseInt(c.Query("end_timestamp"), 10, 64); err == nil && end > 0 {
		query = query.Where("created_at <= ?", end)
	}

	page := positiveInt(c.Query("p"), 1)
	pageSize := clamp(positiveInt(c.Query("page_size"), 20), 1, 100)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "failed to query conversation audits")
		return
	}
	var audits []ConversationAudit
	if err := query.Order("created_at DESC, id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&audits).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "failed to query conversation audits")
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success":   true,
		"data":      audits,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

func getAuditDetail(c *gin.Context) {
	var audit ConversationAudit
	if err := model.DB.Where("request_id = ?", c.Param("request_id")).First(&audit).Error; err != nil {
		respondError(c, http.StatusNotFound, "conversation audit was not found")
		return
	}
	requestContent, err := decryptAuditContent(&audit, "request")
	if err != nil {
		respondError(c, http.StatusServiceUnavailable, "conversation audit content is unavailable")
		return
	}
	responseContent, err := decryptAuditContent(&audit, "response")
	if err != nil {
		respondError(c, http.StatusServiceUnavailable, "conversation audit content is unavailable")
		return
	}
	recordAdminAudit(c, "conversation_audit.view", audit.RequestID)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"audit":    audit,
			"request":  requestContent,
			"response": responseContent,
		},
	})
}

func deleteAudit(c *gin.Context) {
	requestID := c.Param("request_id")
	result := model.DB.Where("request_id = ?", requestID).Delete(&ConversationAudit{})
	if result.Error != nil {
		respondError(c, http.StatusInternalServerError, "failed to delete conversation audit")
		return
	}
	if result.RowsAffected == 0 {
		respondError(c, http.StatusNotFound, "conversation audit was not found")
		return
	}
	recordAdminAudit(c, "conversation_audit.delete", requestID)
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func getSettings(c *gin.Context) {
	settings, err := loadSettings()
	if err != nil {
		respondError(c, http.StatusServiceUnavailable, "conversation audit settings are unavailable")
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": settings})
}

type updateSettingsRequest struct {
	Enabled          *bool   `json:"enabled"`
	RetentionDays    *int    `json:"retention_days"`
	MaxContentBytes  *int    `json:"max_content_bytes"`
	ActiveKeyVersion *string `json:"active_key_version"`
}

func updateSettings(c *gin.Context) {
	settings, err := loadSettings()
	if err != nil {
		respondError(c, http.StatusServiceUnavailable, "conversation audit settings are unavailable")
		return
	}
	var request updateSettingsRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		respondError(c, http.StatusBadRequest, "invalid settings request")
		return
	}
	if request.Enabled != nil {
		settings.Enabled = *request.Enabled
	}
	if request.RetentionDays != nil {
		settings.RetentionDays = clamp(*request.RetentionDays, 1, 3650)
	}
	if request.MaxContentBytes != nil {
		settings.MaxContentBytes = clamp(*request.MaxContentBytes, minimumMaxContentBytes, maximumMaxContentBytes)
	}
	if request.ActiveKeyVersion != nil {
		settings.ActiveKeyVersion = defaultKeyVersion(*request.ActiveKeyVersion)
	}
	if err := validateConfig(runtimeConfig{
		Enabled:          settings.Enabled,
		RetentionDays:    settings.RetentionDays,
		MaxContentBytes:  settings.MaxContentBytes,
		ActiveKeyVersion: settings.ActiveKeyVersion,
	}); err != nil {
		respondError(c, http.StatusBadRequest, "the configured encryption key is unavailable")
		return
	}
	settings.UpdatedAt = time.Now().Unix()
	if err := model.DB.Save(&settings).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "failed to save conversation audit settings")
		return
	}
	refreshSettings()
	recordAdminAudit(c, "conversation_audit.settings.update", "")
	c.JSON(http.StatusOK, gin.H{"success": true, "data": settings})
}

type rotateKeyRequest struct {
	ActiveKeyVersion string `json:"active_key_version"`
}

func rotateKey(c *gin.Context) {
	var request rotateKeyRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil || request.ActiveKeyVersion == "" {
		respondError(c, http.StatusBadRequest, "active_key_version is required")
		return
	}
	settings, err := loadSettings()
	if err != nil {
		respondError(c, http.StatusServiceUnavailable, "conversation audit settings are unavailable")
		return
	}
	settings.ActiveKeyVersion = defaultKeyVersion(request.ActiveKeyVersion)
	if err := validateConfig(runtimeConfig{
		Enabled:          settings.Enabled,
		RetentionDays:    settings.RetentionDays,
		MaxContentBytes:  settings.MaxContentBytes,
		ActiveKeyVersion: settings.ActiveKeyVersion,
	}); err != nil {
		respondError(c, http.StatusBadRequest, "the configured encryption key is unavailable")
		return
	}
	settings.UpdatedAt = time.Now().Unix()
	if err := model.DB.Save(&settings).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "failed to rotate conversation audit key")
		return
	}
	refreshSettings()
	recordAdminAudit(c, "conversation_audit.key.rotate", "")
	c.JSON(http.StatusOK, gin.H{"success": true, "data": settings})
}

func loadSettings() (AuditSettings, error) {
	var settings AuditSettings
	err := model.DB.First(&settings, settingsID).Error
	return settings, err
}

func decryptAuditContent(audit *ConversationAudit, kind string) (string, error) {
	if kind == "request" {
		if audit.RequestCiphertext == "" {
			return "", nil
		}
		return decrypt(audit.KeyVersion, audit.RequestNonce, audit.RequestCiphertext, auditAAD(audit, kind))
	}
	if audit.ResponseCiphertext == "" {
		return "", nil
	}
	return decrypt(audit.KeyVersion, audit.ResponseNonce, audit.ResponseCiphertext, auditAAD(audit, kind))
}

func recordAdminAudit(c *gin.Context, action, requestID string) {
	c.Set(string(constant.ContextKeyAuditLogged), true)
	params := map[string]interface{}{}
	if requestID != "" {
		params["request_id"] = requestID
	}
	model.RecordOperationAuditLog(c.GetInt("id"), "Conversation audit operation", c.ClientIP(), action, params, map[string]interface{}{
		"operator_id":       c.GetInt("id"),
		"operator_username": c.GetString("username"),
	}, nil)
}

func respondError(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{"success": false, "message": message})
}

func positiveInt(value string, fallback int) int {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 {
		return fallback
	}
	return parsed
}
