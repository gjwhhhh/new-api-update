package conversationaudit

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const openAIResponsesLinkProtocol = "openai_responses"

type responseLinkStatus string

const (
	responseLinkNotApplicable        responseLinkStatus = "not_applicable"
	responseLinkRootPersistFailed    responseLinkStatus = "root_persist_failed"
	responseLinkProviderIDAbsent     responseLinkStatus = "provider_response_id_absent"
	responseLinkCreated              responseLinkStatus = "created"
	responseLinkPending              responseLinkStatus = "pending"
	continuationAppended             responseLinkStatus = "appended"
	continuationParentLinkNotFound   responseLinkStatus = "parent_link_not_found"
	continuationRequestIDUnavailable responseLinkStatus = "request_id_unavailable"
	continuationWriteFailed          responseLinkStatus = "write_failed"
)

func responseLinkProtocol(protocol conversationProtocol) string {
	if protocol == protocolOpenAIResponses {
		return openAIResponsesLinkProtocol
	}
	return ""
}

func responseIDHash(version, protocol, responseID string) (string, error) {
	if version == "" || protocol == "" || responseID == "" {
		return "", errors.New("invalid conversation audit response link input")
	}
	runtimeState.RLock()
	key, exists := runtimeState.keys[version]
	runtimeState.RUnlock()
	if !exists {
		return "", fmt.Errorf("conversation audit response link key %q is unavailable", version)
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte("conversation-audit-response-link-v1\x00"))
	_, _ = mac.Write([]byte(protocol))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(responseID))
	return hex.EncodeToString(mac.Sum(nil)), nil
}

func responseLinkKeyVersions() []string {
	runtimeState.RLock()
	versions := make([]string, 0, len(runtimeState.keys))
	for version := range runtimeState.keys {
		versions = append(versions, version)
	}
	runtimeState.RUnlock()
	sort.Strings(versions)
	return versions
}

func createResponseLink(db *gorm.DB, auditID uint, userID int, expiresAt int64, protocol, keyVersion, responseID string) error {
	if responseID == "" || protocol == "" || auditID == 0 {
		return nil
	}
	hash, err := responseIDHash(keyVersion, protocol, responseID)
	if err != nil {
		return err
	}
	link := ConversationAuditResponseLink{
		AuditID:        auditID,
		Protocol:       protocol,
		KeyVersion:     keyVersion,
		ResponseIDHash: hash,
		UserID:         userID,
		ExpiresAt:      expiresAt,
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&link).Error
}

func persistedResponseLinkStatus(audit *ConversationAudit, protocol conversationProtocol, responseID, parentResponseID string) responseLinkStatus {
	if responseLinkProtocol(protocol) == "" {
		return responseLinkNotApplicable
	}
	if audit == nil {
		return responseLinkRootPersistFailed
	}
	if parentResponseID != "" {
		return responseLinkPending
	}
	if responseID == "" {
		return responseLinkProviderIDAbsent
	}
	return responseLinkCreated
}

func findResponseLinkDB(db *gorm.DB, protocol conversationProtocol, userID int, parentResponseID string) (*ConversationAuditResponseLink, error) {
	linkProtocol := responseLinkProtocol(protocol)
	if linkProtocol == "" || parentResponseID == "" || db == nil {
		return nil, nil
	}
	for _, keyVersion := range responseLinkKeyVersions() {
		hash, err := responseIDHash(keyVersion, linkProtocol, parentResponseID)
		if err != nil {
			continue
		}
		var link ConversationAuditResponseLink
		err = db.Where(
			"protocol = ? AND key_version = ? AND response_id_hash = ? AND user_id = ?",
			linkProtocol,
			keyVersion,
			hash,
			userID,
		).First(&link).Error
		if err == nil {
			return &link, nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}
	return nil, nil
}

type responseContinuation struct {
	RequestID          string
	UserID             int
	ParentResponseID   string
	ProviderResponseID string
	Body               string
	ErrorCode          string
	CaptureError       string
	Truncated          bool
	Status             string
	HTTPStatus         int
	CreatedAt          int64
}

var errContinuationParentMissing = errors.New("conversation audit continuation parent not found")

func appendResponseContinuation(c *gin.Context, protocol conversationProtocol, parentResponseID, responseBody, errorCode, captureError, providerResponseID string, responseTruncated bool, status string, statusCode int) responseLinkStatus {
	if !isEnabled() || parentResponseID == "" {
		return continuationParentLinkNotFound
	}
	requestID := c.GetString(common.RequestIdKey)
	if requestID == "" {
		return continuationRequestIDUnavailable
	}
	continuation := responseContinuation{RequestID: requestID, UserID: c.GetInt("id"), ParentResponseID: parentResponseID, ProviderResponseID: providerResponseID, Body: responseBody, ErrorCode: errorCode, CaptureError: captureError, Truncated: responseTruncated, Status: status, HTTPStatus: statusCode, CreatedAt: time.Now().Unix()}
	db, cancel := auditWriteDB()
	defer cancel()
	started := time.Now()
	err := db.Transaction(func(tx *gorm.DB) error {
		audit, err := continuationRoot(tx, protocol, continuation.UserID, parentResponseID)
		if err != nil {
			return err
		}
		return appendResponseSegment(tx, audit, continuation)
	})
	observeAuditPhase("database", started, err != nil && !errors.Is(err, errContinuationParentMissing))
	if errors.Is(err, errContinuationParentMissing) {
		return continuationParentLinkNotFound
	}
	if err != nil {
		common.SysError("conversation audit response continuation write failed request_id=" + safeDiagnosticValue(requestID) + ": " + err.Error())
		return continuationWriteFailed
	}
	notifyResponseReconcile()
	return continuationAppended
}

// Pending continuations never publish response links. Only a durable root or
// an already attached segment can therefore be the target of a continuation.
func continuationRoot(tx *gorm.DB, protocol conversationProtocol, userID int, parentResponseID string) (*ConversationAudit, error) {
	link, err := findResponseLinkDB(tx, protocol, userID, parentResponseID)
	if err != nil {
		return nil, err
	}
	if link == nil {
		return nil, errContinuationParentMissing
	}
	var audit ConversationAudit
	query := tx
	if tx.Dialector.Name() != "sqlite" {
		query = tx.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := query.First(&audit, link.AuditID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errContinuationParentMissing
		}
		return nil, err
	}
	if audit.UserID != userID || audit.ExpiresAt <= time.Now().Unix() {
		return nil, errContinuationParentMissing
	}
	return &audit, nil
}

// Always store a segment receipt, including tool-only responses with no text,
// so replaying a request cannot reapply status or length changes.
func appendResponseSegment(tx *gorm.DB, audit *ConversationAudit, continuation responseContinuation) error {
	cfg := currentConfig()
	segmentBody, limitTruncated, err := fitResponseSegment(continuation.Body, cfg.MaxContentBytes-audit.ResponseLength)
	if err != nil {
		return err
	}
	truncated := continuation.Truncated || limitTruncated
	segment := &ConversationAuditResponseSegment{AuditID: audit.ID, RequestID: continuation.RequestID, KeyVersion: cfg.ActiveKeyVersion, ResponseLength: len(segmentBody), ResponseTruncated: truncated, CreatedAt: continuation.CreatedAt, ExpiresAt: audit.ExpiresAt}
	if segmentBody != "" {
		segment.ResponseNonce, segment.ResponseCiphertext, err = encrypt(segment.KeyVersion, segmentBody, auditResponseSegmentAAD(segment))
		if err != nil {
			return err
		}
	}
	result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(segment)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		var existing ConversationAuditResponseSegment
		if err := tx.Where("request_id = ?", continuation.RequestID).First(&existing).Error; err != nil {
			return err
		}
		if existing.AuditID != audit.ID {
			return errors.New("conversation audit segment belongs to another turn")
		}
	}
	if result.RowsAffected > 0 {
		updates := map[string]any{"response_length": audit.ResponseLength + segment.ResponseLength}
		if truncated {
			updates["response_truncated"] = true
		}
		// Preserve the worst outcome across a user turn. Content truncation must
		// never turn a failed or incomplete generation into an apparent success.
		status := audit.Status
		if continuation.Status == "failed" || continuation.HTTPStatus >= 400 {
			status = "failed"
			updates["http_status"] = continuation.HTTPStatus
			updates["error_code"] = continuation.ErrorCode
		} else if continuation.Status == "capture_failed" && status != "failed" {
			status = "capture_failed"
		} else if continuation.Status == "incomplete" && status != "failed" && status != "capture_failed" {
			status = "incomplete"
		} else if truncated && status == "completed" {
			status = "truncated"
		}
		updates["status"] = status
		if continuation.CaptureError != "" {
			updates["capture_error"] = joinCaptureErrors(audit.CaptureError, continuation.CaptureError)
		}
		if err := tx.Model(&ConversationAudit{}).Where("id = ?", audit.ID).Updates(updates).Error; err != nil {
			return err
		}
	}
	return createResponseLink(tx, audit.ID, audit.UserID, audit.ExpiresAt, openAIResponsesLinkProtocol, cfg.ActiveKeyVersion, continuation.ProviderResponseID)
}

func fitResponseSegment(body string, remaining int) (string, bool, error) {
	if body == "" {
		return "", false, nil
	}
	if remaining <= 0 {
		return "", true, nil
	}
	if len(body) <= remaining {
		return body, false, nil
	}
	var payload capturePayload
	if err := common.Unmarshal([]byte(body), &payload); err != nil || payload.AssistantText == "" {
		return "", true, nil
	}
	fitted, truncated, err := marshalCapturePayload(capturePayload{
		SchemaVersion: payload.SchemaVersion,
		CaptureMode:   payload.CaptureMode,
		AssistantText: payload.AssistantText,
	}, remaining)
	if err != nil {
		if errors.Is(err, io.ErrShortBuffer) {
			return "", true, nil
		}
		return "", false, err
	}
	return fitted, truncated, nil
}
