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
	"github.com/QuantumNous/new-api/model"
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
	responseLinkWriteFailed          responseLinkStatus = "write_failed"
	continuationAppended             responseLinkStatus = "appended"
	continuationParentLinkNotFound   responseLinkStatus = "parent_link_not_found"
	continuationLinkLookupFailed     responseLinkStatus = "link_lookup_failed"
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

func registerResponseLink(audit *ConversationAudit, protocol conversationProtocol, responseID string) responseLinkStatus {
	linkProtocol := responseLinkProtocol(protocol)
	if linkProtocol == "" {
		return responseLinkNotApplicable
	}
	if audit == nil {
		return responseLinkRootPersistFailed
	}
	if responseID == "" {
		return responseLinkProviderIDAbsent
	}
	if err := createResponseLink(model.DB, audit.ID, audit.UserID, audit.ExpiresAt, linkProtocol, audit.KeyVersion, responseID); err != nil {
		common.SysError("conversation audit response link write failed: " + err.Error())
		return responseLinkWriteFailed
	}
	return responseLinkCreated
}

func findResponseLink(protocol conversationProtocol, userID int, parentResponseID string) (*ConversationAuditResponseLink, error) {
	linkProtocol := responseLinkProtocol(protocol)
	if linkProtocol == "" || parentResponseID == "" || model.DB == nil {
		return nil, nil
	}
	for _, keyVersion := range responseLinkKeyVersions() {
		hash, err := responseIDHash(keyVersion, linkProtocol, parentResponseID)
		if err != nil {
			continue
		}
		var link ConversationAuditResponseLink
		err = model.DB.Where(
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

func appendResponseContinuation(c *gin.Context, protocol conversationProtocol, parentResponseID, responseBody, errorCode, captureError, providerResponseID string, responseTruncated bool, status string, statusCode int) responseLinkStatus {
	if !isEnabled() || parentResponseID == "" {
		return continuationParentLinkNotFound
	}
	link, err := findResponseLink(protocol, c.GetInt("id"), parentResponseID)
	if err != nil {
		common.SysError("conversation audit response link lookup failed: " + err.Error())
		return continuationLinkLookupFailed
	}
	if link == nil {
		return continuationParentLinkNotFound
	}

	cfg := currentConfig()
	requestID := c.GetString(common.RequestIdKey)
	if requestID == "" {
		return continuationRequestIDUnavailable
	}
	linkProtocol := responseLinkProtocol(protocol)
	now := time.Now().Unix()
	err = model.DB.Transaction(func(tx *gorm.DB) error {
		var audit ConversationAudit
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&audit, link.AuditID).Error; err != nil {
			return err
		}
		if audit.UserID != c.GetInt("id") {
			return nil
		}

		segmentBody, limitTruncated, err := fitResponseSegment(responseBody, cfg.MaxContentBytes-audit.ResponseLength)
		if err != nil {
			return err
		}
		responseTruncated = responseTruncated || limitTruncated

		segmentLength := 0
		if segmentBody != "" {
			segment := &ConversationAuditResponseSegment{
				AuditID:           audit.ID,
				RequestID:         requestID,
				KeyVersion:        cfg.ActiveKeyVersion,
				ResponseLength:    len(segmentBody),
				ResponseTruncated: responseTruncated,
				CreatedAt:         now,
				ExpiresAt:         audit.ExpiresAt,
			}
			segment.ResponseNonce, segment.ResponseCiphertext, err = encrypt(segment.KeyVersion, segmentBody, auditResponseSegmentAAD(segment))
			if err != nil {
				return err
			}
			result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(segment)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected > 0 {
				segmentLength = segment.ResponseLength
			}
		}

		updates := map[string]interface{}{}
		if segmentLength > 0 {
			updates["response_length"] = audit.ResponseLength + segmentLength
		}
		if responseTruncated {
			updates["response_truncated"] = true
			updates["status"] = "truncated"
		} else if statusCode >= 400 {
			updates["status"] = "failed"
			updates["http_status"] = statusCode
			updates["error_code"] = errorCode
		}
		if captureError != "" {
			updates["capture_error"] = joinCaptureErrors(audit.CaptureError, captureError)
		}
		if len(updates) > 0 {
			if err := tx.Model(&ConversationAudit{}).Where("id = ?", audit.ID).Updates(updates).Error; err != nil {
				return err
			}
		}
		return createResponseLink(tx, audit.ID, audit.UserID, audit.ExpiresAt, linkProtocol, cfg.ActiveKeyVersion, providerResponseID)
	})
	if err != nil {
		common.SysError("conversation audit response continuation write failed request_id=" + requestID + ": " + err.Error())
		return continuationWriteFailed
	}
	return continuationAppended
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
