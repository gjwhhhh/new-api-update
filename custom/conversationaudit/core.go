package conversationaudit

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	settingsID              = 1
	defaultRetentionDays    = 30
	defaultMaxContentBytes  = 256 * 1024
	defaultMaxParseBytes    = 4 * 1024 * 1024
	minimumMaxContentBytes  = 1024
	maximumMaxContentBytes  = 1024 * 1024
	minimumMaxParseBytes    = 1024 * 1024
	maximumMaxParseBytes    = 16 * 1024 * 1024
	settingsRefreshInterval = time.Minute
	requestIDUniqueIndex    = "uq_custom_conversation_audits_request_id"
	legacyRequestIDIndex    = "idx_custom_conversation_audits_request_id"
)

// ConversationAudit is intentionally separate from model.Log. Normal user and
// consumption-log endpoints cannot return encrypted conversation payloads.
type ConversationAudit struct {
	ID                 uint   `json:"id" gorm:"primaryKey"`
	RequestID          string `json:"request_id" gorm:"uniqueIndex:uq_custom_conversation_audits_request_id;default:''"`
	UserID             int    `json:"user_id" gorm:"index;not null"`
	Username           string `json:"username" gorm:"index;default:''"`
	TokenID            int    `json:"token_id" gorm:"index;default:0"`
	ModelName          string `json:"model_name" gorm:"index;default:''"`
	RequestPath        string `json:"request_path" gorm:"index;default:''"`
	Status             string `json:"status" gorm:"index;default:''"`
	HTTPStatus         int    `json:"http_status"`
	ErrorCode          string `json:"error_code,omitempty" gorm:"index;default:''"`
	KeyVersion         string `json:"key_version" gorm:"default:''"`
	RequestNonce       string `json:"-"`
	RequestCiphertext  string `json:"-"`
	ResponseNonce      string `json:"-"`
	ResponseCiphertext string `json:"-"`
	RequestLength      int    `json:"request_length"`
	ResponseLength     int    `json:"response_length"`
	RequestTruncated   bool   `json:"request_truncated"`
	ResponseTruncated  bool   `json:"response_truncated"`
	CaptureError       string `json:"capture_error,omitempty" gorm:"default:''"`
	CreatedAt          int64  `json:"created_at" gorm:"index"`
	ExpiresAt          int64  `json:"expires_at" gorm:"index"`

	User model.User `json:"-" gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE"`
}

func (ConversationAudit) TableName() string {
	return "custom_conversation_audits"
}

// ConversationAuditResponseLink maps an opaque upstream Responses ID to one
// audited user turn. The ID is HMACed before storage so the audit database never
// contains an upstream response identifier in plaintext.
type ConversationAuditResponseLink struct {
	ID             uint   `json:"id" gorm:"primaryKey"`
	AuditID        uint   `json:"audit_id" gorm:"index;not null"`
	Protocol       string `json:"protocol" gorm:"size:32;uniqueIndex:uq_custom_conversation_audit_response_links_provider"`
	KeyVersion     string `json:"key_version" gorm:"size:32;uniqueIndex:uq_custom_conversation_audit_response_links_provider"`
	ResponseIDHash string `json:"-" gorm:"size:64;uniqueIndex:uq_custom_conversation_audit_response_links_provider"`
	UserID         int    `json:"user_id" gorm:"index;not null"`
	ExpiresAt      int64  `json:"expires_at" gorm:"index"`

	Audit ConversationAudit `json:"-" gorm:"foreignKey:AuditID;constraint:OnDelete:CASCADE"`
}

func (ConversationAuditResponseLink) TableName() string {
	return "custom_conversation_audit_response_links"
}

// ConversationAuditResponseSegment is a later visible response fragment from
// a tool continuation. It deliberately contains no request, tool, or raw JSON
// content and is decrypted only with its parent audit detail.
type ConversationAuditResponseSegment struct {
	ID                 uint   `json:"id" gorm:"primaryKey"`
	AuditID            uint   `json:"audit_id" gorm:"index;not null"`
	RequestID          string `json:"request_id" gorm:"uniqueIndex:uq_custom_conversation_audit_response_segments_request_id;default:''"`
	KeyVersion         string `json:"key_version" gorm:"default:''"`
	ResponseNonce      string `json:"-"`
	ResponseCiphertext string `json:"-"`
	ResponseLength     int    `json:"response_length"`
	ResponseTruncated  bool   `json:"response_truncated"`
	CreatedAt          int64  `json:"created_at" gorm:"index"`
	ExpiresAt          int64  `json:"expires_at" gorm:"index"`

	Audit ConversationAudit `json:"-" gorm:"foreignKey:AuditID;constraint:OnDelete:CASCADE"`
}

func (ConversationAuditResponseSegment) TableName() string {
	return "custom_conversation_audit_response_segments"
}

// AuditSettings has operational settings only. Encryption keys stay in the
// deployment secret store and are never persisted in the database.
type AuditSettings struct {
	ID               uint   `json:"-" gorm:"primaryKey"`
	Enabled          bool   `json:"enabled"`
	RetentionDays    int    `json:"retention_days"`
	MaxContentBytes  int    `json:"max_content_bytes"`
	ActiveKeyVersion string `json:"active_key_version"`
	UpdatedAt        int64  `json:"updated_at"`
}

func (AuditSettings) TableName() string {
	return "custom_conversation_audit_settings"
}

type runtimeConfig struct {
	Enabled          bool
	RetentionDays    int
	MaxContentBytes  int
	MaxParseBytes    int
	ActiveKeyVersion string
}

var runtimeState struct {
	sync.RWMutex
	config  runtimeConfig
	keys    map[string][]byte
	initErr error
}

var initOnce sync.Once

func initialize() {
	keys, err := loadKeyring()
	if err != nil {
		setInitError(err)
		return
	}
	runtimeState.Lock()
	runtimeState.keys = keys
	runtimeState.config = configFromEnv()
	runtimeState.Unlock()
	if err := validateConfig(currentConfig()); err != nil {
		setInitError(err)
		return
	}

	if common.IsMasterNode {
		if err := migrateConversationAuditTables(model.DB); err != nil {
			setInitError(fmt.Errorf("migrate conversation audit tables: %w", err))
			return
		}
		ensureDefaultSettings()
		cleanupExpired()
	}
	refreshSettings()
	go refreshLoop()
}

func migrateConversationAuditTables(db *gorm.DB) error {
	if err := db.AutoMigrate(
		&ConversationAudit{},
		&AuditSettings{},
		&ConversationAuditResponseLink{},
		&ConversationAuditResponseSegment{},
	); err != nil {
		return err
	}

	migrator := db.Migrator()
	if !migrator.HasIndex(&ConversationAudit{}, requestIDUniqueIndex) {
		if err := migrator.CreateIndex(&ConversationAudit{}, requestIDUniqueIndex); err != nil {
			return fmt.Errorf("create request id unique index: %w", err)
		}
	}
	if migrator.HasIndex(&ConversationAudit{}, legacyRequestIDIndex) {
		if err := migrator.DropIndex(&ConversationAudit{}, legacyRequestIDIndex); err != nil {
			return fmt.Errorf("drop legacy request id index: %w", err)
		}
	}
	return nil
}

func refreshLoop() {
	ticker := time.NewTicker(settingsRefreshInterval)
	defer ticker.Stop()
	for range ticker.C {
		refreshSettings()
		if common.IsMasterNode {
			cleanupExpired()
		}
	}
}

func configFromEnv() runtimeConfig {
	return runtimeConfig{
		Enabled:          parseBoolEnv("CONVERSATION_AUDIT_ENABLED", false),
		RetentionDays:    boundedIntEnv("CONVERSATION_AUDIT_RETENTION_DAYS", defaultRetentionDays, 1, 3650),
		MaxContentBytes:  boundedIntEnv("CONVERSATION_AUDIT_MAX_BYTES", defaultMaxContentBytes, minimumMaxContentBytes, maximumMaxContentBytes),
		MaxParseBytes:    boundedIntEnv("CONVERSATION_AUDIT_MAX_PARSE_BYTES", defaultMaxParseBytes, minimumMaxParseBytes, maximumMaxParseBytes),
		ActiveKeyVersion: defaultKeyVersion(os.Getenv("CONVERSATION_AUDIT_ACTIVE_KEY_VERSION")),
	}
}

func ensureDefaultSettings() {
	var settings AuditSettings
	err := model.DB.First(&settings, settingsID).Error
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return
	}
	cfg := currentConfig()
	if err = model.DB.Create(&AuditSettings{
		ID:               settingsID,
		Enabled:          cfg.Enabled,
		RetentionDays:    cfg.RetentionDays,
		MaxContentBytes:  cfg.MaxContentBytes,
		ActiveKeyVersion: cfg.ActiveKeyVersion,
		UpdatedAt:        time.Now().Unix(),
	}).Error; err != nil {
		common.SysError("create conversation audit settings failed: " + err.Error())
	}
}

func refreshSettings() {
	var settings AuditSettings
	if err := model.DB.First(&settings, settingsID).Error; err != nil {
		return
	}
	envConfig := configFromEnv()
	cfg := runtimeConfig{
		Enabled:          settings.Enabled,
		RetentionDays:    clamp(settings.RetentionDays, 1, 3650),
		MaxContentBytes:  clamp(settings.MaxContentBytes, minimumMaxContentBytes, maximumMaxContentBytes),
		MaxParseBytes:    envConfig.MaxParseBytes,
		ActiveKeyVersion: defaultKeyVersion(settings.ActiveKeyVersion),
	}
	if err := validateConfig(cfg); err != nil {
		setInitError(err)
		return
	}
	runtimeState.Lock()
	runtimeState.config = cfg
	runtimeState.initErr = nil
	runtimeState.Unlock()
}

func setInitError(err error) {
	runtimeState.Lock()
	runtimeState.initErr = err
	runtimeState.config.Enabled = false
	runtimeState.Unlock()
	common.SysError("conversation audit disabled: " + err.Error())
}

func currentConfig() runtimeConfig {
	runtimeState.RLock()
	defer runtimeState.RUnlock()
	return runtimeState.config
}

func initializationError() error {
	runtimeState.RLock()
	defer runtimeState.RUnlock()
	return runtimeState.initErr
}

func isEnabled() bool {
	return initializationError() == nil && currentConfig().Enabled
}

func validateConfig(cfg runtimeConfig) error {
	if !cfg.Enabled {
		return nil
	}
	runtimeState.RLock()
	_, exists := runtimeState.keys[cfg.ActiveKeyVersion]
	runtimeState.RUnlock()
	if !exists {
		return fmt.Errorf("active conversation audit key %q is unavailable", cfg.ActiveKeyVersion)
	}
	return nil
}

func loadKeyring() (map[string][]byte, error) {
	keys := make(map[string][]byte)
	for _, pair := range os.Environ() {
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) != 2 || !strings.HasPrefix(parts[0], "CONVERSATION_AUDIT_KEY_V") {
			continue
		}
		version := "v" + strings.TrimPrefix(parts[0], "CONVERSATION_AUDIT_KEY_V")
		if version == "v" {
			continue
		}
		key, err := base64.StdEncoding.DecodeString(parts[1])
		if err != nil {
			key, err = base64.RawStdEncoding.DecodeString(parts[1])
		}
		if err != nil || len(key) != 32 {
			return nil, fmt.Errorf("%s must be a base64-encoded 32-byte AES key", parts[0])
		}
		keys[version] = key
	}
	return keys, nil
}

func encrypt(version, plaintext, aad string) (string, string, error) {
	runtimeState.RLock()
	key, exists := runtimeState.keys[version]
	runtimeState.RUnlock()
	if !exists {
		return "", "", fmt.Errorf("encryption key %q is unavailable", version)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", "", err
	}
	ciphertext := gcm.Seal(nil, nonce, []byte(plaintext), []byte(aad))
	return base64.RawStdEncoding.EncodeToString(nonce), base64.RawStdEncoding.EncodeToString(ciphertext), nil
}

func decrypt(version, nonceText, ciphertextText, aad string) (string, error) {
	runtimeState.RLock()
	key, exists := runtimeState.keys[version]
	runtimeState.RUnlock()
	if !exists {
		return "", fmt.Errorf("encryption key %q is unavailable", version)
	}
	nonce, err := base64.RawStdEncoding.DecodeString(nonceText)
	if err != nil {
		return "", err
	}
	ciphertext, err := base64.RawStdEncoding.DecodeString(ciphertextText)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	plaintext, err := gcm.Open(nil, nonce, ciphertext, []byte(aad))
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

func persist(c *gin.Context, requestBody, responseBody, errorCode, captureError string, requestTruncated, responseTruncated bool, status string, statusCode int) *ConversationAudit {
	cfg := currentConfig()
	if !cfg.Enabled || requestBody == "" {
		return nil
	}
	requestID := c.GetString(common.RequestIdKey)
	if requestID == "" {
		return nil
	}
	now := time.Now().Unix()
	audit := &ConversationAudit{
		RequestID:         requestID,
		UserID:            c.GetInt("id"),
		Username:          c.GetString("username"),
		TokenID:           c.GetInt("token_id"),
		ModelName:         c.GetString("original_model"),
		RequestPath:       c.Request.URL.Path,
		Status:            status,
		HTTPStatus:        statusCode,
		ErrorCode:         errorCode,
		KeyVersion:        cfg.ActiveKeyVersion,
		RequestLength:     len(requestBody),
		ResponseLength:    len(responseBody),
		RequestTruncated:  requestTruncated,
		ResponseTruncated: responseTruncated,
		CaptureError:      captureError,
		CreatedAt:         now,
		ExpiresAt:         now + int64(cfg.RetentionDays)*int64((24*time.Hour).Seconds()),
	}
	var err error
	if requestBody != "" {
		audit.RequestNonce, audit.RequestCiphertext, err = encrypt(cfg.ActiveKeyVersion, requestBody, auditAAD(audit, "request"))
	}
	if err == nil && responseBody != "" {
		audit.ResponseNonce, audit.ResponseCiphertext, err = encrypt(cfg.ActiveKeyVersion, responseBody, auditAAD(audit, "response"))
	}
	if err != nil {
		audit.Status = "capture_failed"
		audit.CaptureError = "encryption_failed"
		audit.RequestNonce = ""
		audit.RequestCiphertext = ""
		audit.ResponseNonce = ""
		audit.ResponseCiphertext = ""
	}
	if dbErr := model.DB.Create(audit).Error; dbErr != nil {
		common.SysError(fmt.Sprintf("conversation audit write failed request_id=%s: %v", requestID, dbErr))
		return nil
	}
	return audit
}

func auditAAD(audit *ConversationAudit, kind string) string {
	return fmt.Sprintf("conversation-audit|%s|%d|%s", audit.RequestID, audit.UserID, kind)
}

func auditResponseSegmentAAD(segment *ConversationAuditResponseSegment) string {
	return fmt.Sprintf("conversation-audit-segment|%d|%s", segment.AuditID, segment.RequestID)
}

func cleanupExpired() {
	if model.DB == nil {
		return
	}
	now := time.Now().Unix()
	if err := model.DB.Where("expires_at > 0 AND expires_at < ?", now).Delete(&ConversationAuditResponseSegment{}).Error; err != nil {
		common.SysError("conversation audit response segment cleanup failed: " + err.Error())
	}
	if err := model.DB.Where("expires_at > 0 AND expires_at < ?", now).Delete(&ConversationAuditResponseLink{}).Error; err != nil {
		common.SysError("conversation audit response link cleanup failed: " + err.Error())
	}
	if err := model.DB.Where("expires_at > 0 AND expires_at < ?", now).Delete(&ConversationAudit{}).Error; err != nil {
		common.SysError("conversation audit cleanup failed: " + err.Error())
	}
}

func parseBoolEnv(name string, fallback bool) bool {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func boundedIntEnv(name string, fallback, min, max int) int {
	value, err := strconv.Atoi(os.Getenv(name))
	if err != nil {
		return fallback
	}
	return clamp(value, min, max)
}

func defaultKeyVersion(value string) string {
	if value == "" {
		return "v1"
	}
	return value
}

func clamp(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}
