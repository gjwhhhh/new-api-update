package conversationaudit

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func Register(apiRouter *gin.RouterGroup) {
	if model.DB != nil {
		initOnce.Do(initialize)
	}
	registerRoutes(apiRouter)
}

func registerRoutes(apiRouter *gin.RouterGroup) {
	auditRoute := apiRouter.Group("/custom/conversation-audits", middleware.DisableCache())
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
		settingsRoute.GET("/metrics", getAuditMetrics)
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
	requestID := c.Param("request_id")
	preview := c.Query("preview") == "true"
	now := time.Now().Unix()
	db := model.DB.WithContext(c.Request.Context())
	var audit ConversationAudit
	err := db.Where("request_id = ? AND expires_at > ?", requestID, now).Take(&audit).Error
	bySegment := errors.Is(err, gorm.ErrRecordNotFound)
	if bySegment {
		segmentAuditID := db.Model(&ConversationAuditResponseSegment{}).Select("audit_id").
			Where("request_id = ? AND expires_at > ?", requestID, now)
		err = db.Where("expires_at > ? AND id = (?)", now, segmentAuditID).Take(&audit).Error
	}
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			respondError(c, http.StatusNotFound, "conversation audit was not found")
		} else {
			respondError(c, http.StatusServiceUnavailable, "conversation audit content is unavailable")
		}
		return
	}
	if !bySegment && audit.RequestID != requestID {
		respondError(c, http.StatusNotFound, "conversation audit was not found")
		return
	}
	requestContent, err := decryptAuditContent(&audit, "request")
	if err != nil {
		respondError(c, http.StatusServiceUnavailable, "conversation audit content is unavailable")
		return
	}
	var sensitiveMatch *service.RelaySensitiveMatch
	if payload, ok := strings.CutPrefix(requestContent, sensitiveAuditPrefix); ok {
		var request sensitiveAuditRequest
		if common.UnmarshalJsonStr(payload, &request) != nil {
			respondError(c, http.StatusServiceUnavailable, "conversation audit content is unavailable")
			return
		}
		requestContent = request.Request
		sensitiveMatch = &request.Match
	}
	responseContent, err := decryptAuditContent(&audit, "response")
	if err != nil {
		respondError(c, http.StatusServiceUnavailable, "conversation audit content is unavailable")
		return
	}
	if preview {
		requestPreview, requestPreviewTruncated := auditPreviewText(requestContent, "user_input")
		responsePreviewContent := responseContent
		responsePreviewTruncated := false
		if bySegment {
			var segment ConversationAuditResponseSegment
			segmentErr := db.Where("audit_id = ? AND request_id = ? AND expires_at > ?", audit.ID, requestID, now).Take(&segment).Error
			if errors.Is(segmentErr, gorm.ErrRecordNotFound) {
				respondError(c, http.StatusNotFound, "conversation audit was not found")
				return
			}
			if segmentErr != nil {
				respondError(c, http.StatusServiceUnavailable, "conversation audit content is unavailable")
				return
			}
			responsePreviewContent, err = decrypt(segment.KeyVersion, segment.ResponseNonce, segment.ResponseCiphertext, auditResponseSegmentAAD(&segment))
			if err != nil {
				respondError(c, http.StatusServiceUnavailable, "conversation audit content is unavailable")
				return
			}
			responsePreviewContent, responsePreviewTruncated = auditPreviewText(responsePreviewContent, "assistant_text")
		} else {
			responsePreviewContent, responsePreviewTruncated = auditPreviewText(responseContent, "assistant_text")
		}
		recordAdminAudit(c, "conversation_audit.view", requestID)
		detail := gin.H{
			"audit":                      audit,
			"preview":                    true,
			"request_preview":            requestPreview,
			"response_preview":           responsePreviewContent,
			"request_preview_truncated":  requestPreviewTruncated,
			"response_preview_truncated": responsePreviewTruncated,
		}
		if sensitiveMatch != nil {
			detail["local_sensitive_match"] = sensitiveMatch
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "data": detail})
		return
	}
	responseSegments, matchedSegment, err := decryptAuditResponseSegments(db, &audit, requestID, now)
	if err != nil {
		respondError(c, http.StatusServiceUnavailable, "conversation audit content is unavailable")
		return
	}
	if bySegment && matchedSegment == nil {
		respondError(c, http.StatusNotFound, "conversation audit was not found")
		return
	}
	responsePartCount := len(responseSegments)
	if responseContent != "" {
		responsePartCount++
	}
	recordAdminAudit(c, "conversation_audit.view", requestID)
	detail := gin.H{
		"audit": audit, "request": requestContent, "response": responseContent,
		"response_segments": responseSegments, "response_part_count": responsePartCount,
	}
	if sensitiveMatch != nil {
		detail["local_sensitive_match"] = sensitiveMatch
	}
	if matchedSegment != nil {
		detail["matched_segment"] = matchedSegment
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    detail,
	})
}

const auditPreviewLimit = 8 * 1024

func auditPreviewText(payload, field string) (string, bool) {
	text := payload
	var captured capturePayload
	if common.UnmarshalJsonStr(payload, &captured) == nil && captured.SchemaVersion == 2 {
		if field == "user_input" {
			text = captured.UserInput
		} else {
			text = captured.AssistantText
		}
	}
	if len(text) <= auditPreviewLimit {
		return text, false
	}
	text = text[:auditPreviewLimit]
	for !utf8.ValidString(text) {
		text = text[:len(text)-1]
	}
	return text + "…", true
}

func deleteAudit(c *gin.Context) {
	requestID := c.Param("request_id")
	var audit ConversationAudit
	if err := model.DB.Where("request_id = ?", requestID).First(&audit).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			respondError(c, http.StatusNotFound, "conversation audit was not found")
			return
		}
		respondError(c, http.StatusInternalServerError, "failed to delete conversation audit")
		return
	}
	if err := model.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("audit_id = ?", audit.ID).Delete(&ConversationAuditPendingResponse{}).Error; err != nil {
			return err
		}
		if err := tx.Where("audit_id = ?", audit.ID).Delete(&ConversationAuditResponseSegment{}).Error; err != nil {
			return err
		}
		if err := tx.Where("audit_id = ?", audit.ID).Delete(&ConversationAuditResponseLink{}).Error; err != nil {
			return err
		}
		return tx.Delete(&audit).Error
	}); err != nil {
		respondError(c, http.StatusInternalServerError, "failed to delete conversation audit")
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
	Enabled            *bool   `json:"enabled"`
	CaptureFullPayload *bool   `json:"capture_full_payload"`
	RetentionDays      *int    `json:"retention_days"`
	MaxContentBytes    *int    `json:"max_content_bytes"`
	ActiveKeyVersion   *string `json:"active_key_version"`
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
	if request.CaptureFullPayload != nil {
		settings.CaptureFullPayload = *request.CaptureFullPayload
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
		Enabled:            settings.Enabled,
		CaptureFullPayload: settings.CaptureFullPayload,
		RetentionDays:      settings.RetentionDays,
		MaxContentBytes:    settings.MaxContentBytes,
		ActiveKeyVersion:   settings.ActiveKeyVersion,
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
		Enabled:            settings.Enabled,
		CaptureFullPayload: settings.CaptureFullPayload,
		RetentionDays:      settings.RetentionDays,
		MaxContentBytes:    settings.MaxContentBytes,
		ActiveKeyVersion:   settings.ActiveKeyVersion,
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

type auditSegmentMatch struct {
	// Index in response_segments; -1 denotes a stored continuation without text.
	ResponseIndex int  `json:"response_index"`
	Truncated     bool `json:"truncated"`
}

func decryptAuditResponseSegments(db *gorm.DB, audit *ConversationAudit, requestID string, now int64) ([]string, *auditSegmentMatch, error) {
	var segments []ConversationAuditResponseSegment
	if err := db.Where("audit_id = ? AND expires_at > ?", audit.ID, now).Order("created_at ASC, id ASC").Find(&segments).Error; err != nil {
		return nil, nil, err
	}
	plaintexts := make([]string, 0, len(segments))
	var match *auditSegmentMatch
	for i := range segments {
		segment := &segments[i]
		if segment.RequestID == requestID {
			match = &auditSegmentMatch{ResponseIndex: -1, Truncated: segment.ResponseTruncated}
		}
		if segment.ResponseCiphertext == "" {
			continue
		}
		plaintext, err := decrypt(segment.KeyVersion, segment.ResponseNonce, segment.ResponseCiphertext, auditResponseSegmentAAD(segment))
		if err != nil {
			return nil, nil, err
		}
		if segment.RequestID == requestID {
			match.ResponseIndex = len(plaintexts)
		}
		plaintexts = append(plaintexts, plaintext)
	}
	return plaintexts, match, nil
}

func recordAdminAudit(c *gin.Context, action, requestID string) {
	c.Set(string(constant.ContextKeyAuditLogged), true)
	params := map[string]any{}
	if requestID != "" {
		params["request_id"] = requestID
	}
	model.RecordOperationAuditLog(
		c.GetInt("id"),
		c.GetInt("role"),
		"Conversation audit operation",
		c.ClientIP(),
		action,
		params,
		&model.AuditAdminInfo{
			AdminID:       c.GetInt("id"),
			AdminUsername: c.GetString("username"),
			AdminRole:     c.GetInt("role"),
		},
		nil,
		c,
	)
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
