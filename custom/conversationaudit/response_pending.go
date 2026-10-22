package conversationaudit

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// An unresolved response remains a normal, viewable encrypted audit. This table
// adds retry metadata without putting provider identifiers into plaintext SQL.
type ConversationAuditPendingResponse struct {
	AuditID       uint   `gorm:"primaryKey;autoIncrement:false"`
	KeyVersion    string `gorm:"size:32"`
	Nonce         string
	Ciphertext    string
	CreatedAt     int64
	ExpiresAt     int64             `gorm:"index"`
	NextAttemptAt int64             `gorm:"index"`
	Audit         ConversationAudit `gorm:"foreignKey:AuditID;constraint:OnDelete:CASCADE"`
}

func (ConversationAuditPendingResponse) TableName() string {
	return "custom_conversation_audit_pending_responses"
}

type responseIdentity struct {
	protocol         conversationProtocol
	responseID       string
	parentResponseID string
}

type pendingResponseIdentity struct {
	ParentResponseID string `json:"parent_response_id"`
	ResponseID       string `json:"response_id"`
}

var responseReconcileWake = make(chan struct{}, 1)

func notifyResponseReconcile() {
	select {
	case responseReconcileWake <- struct{}{}:
	default:
	}
}

func pendingResponseAAD(audit *ConversationAudit) string {
	return fmt.Sprintf("conversation-audit-pending|%s|%d", audit.RequestID, audit.UserID)
}

// One bounded sweep works across instances and survives process restarts. A
// retry timestamp keeps missing/invalid parents from starving newer entries.
func reconcileResponseContinuationsDB(sourceDB *gorm.DB) {
	if !isEnabled() {
		return
	}
	db, cancel := boundedAuditDB(sourceDB)
	defer cancel()
	now := time.Now().Unix()
	scanStarted := time.Now()
	var pending []ConversationAuditPendingResponse
	if err := db.Where("next_attempt_at <= ? AND expires_at > ?", now, now).Order("next_attempt_at ASC, audit_id ASC").Limit(32).Find(&pending).Error; err != nil {
		observeAuditPhase("reconcile", scanStarted, true)
		common.SysError("conversation audit pending response scan failed: " + err.Error())
		return
	}
	for _, item := range pending {
		started := time.Now()
		err := db.Transaction(func(tx *gorm.DB) error {
			var current ConversationAuditPendingResponse
			query := tx
			if tx.Dialector.Name() != "sqlite" {
				query = tx.Clauses(clause.Locking{Strength: "UPDATE"})
			}
			if err := query.First(&current, item.AuditID).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return nil
				}
				return err
			}
			var orphan ConversationAudit
			if err := tx.First(&orphan, current.AuditID).Error; err != nil {
				return err
			}
			identityText, err := decrypt(current.KeyVersion, current.Nonce, current.Ciphertext, pendingResponseAAD(&orphan))
			if err != nil {
				return err
			}
			var identity pendingResponseIdentity
			if err := common.UnmarshalJsonStr(identityText, &identity); err != nil {
				return err
			}
			root, err := continuationRoot(tx, protocolOpenAIResponses, orphan.UserID, identity.ParentResponseID)
			if errors.Is(err, errContinuationParentMissing) {
				return tx.Model(&current).Update("next_attempt_at", now+5).Error
			}
			if err != nil {
				return err
			}
			body, err := decryptAuditContent(&orphan, "response")
			if err != nil {
				return err
			}
			// Re-encrypt for the segment's AAD rather than moving ciphertext that was
			// authenticated against the temporary audit's request ID.
			reasons := strings.Split(orphan.CaptureError, ",")
			cleanReasons := reasons[:0]
			for _, reason := range reasons {
				if reason != "pending_response_continuation" {
					cleanReasons = append(cleanReasons, reason)
				}
			}
			continuation := responseContinuation{RequestID: orphan.RequestID, UserID: orphan.UserID, ParentResponseID: identity.ParentResponseID, ProviderResponseID: identity.ResponseID, Body: body, ErrorCode: orphan.ErrorCode, CaptureError: strings.Join(cleanReasons, ","), Truncated: orphan.ResponseTruncated, Status: orphan.Status, HTTPStatus: orphan.HTTPStatus, CreatedAt: orphan.CreatedAt}
			if err := appendResponseSegment(tx, root, continuation); err != nil {
				return err
			}
			if err := tx.Delete(&current).Error; err != nil {
				return err
			}
			return tx.Delete(&orphan).Error
		})
		observeAuditPhase("reconcile", started, err != nil)
		if err != nil {
			common.SysError("conversation audit pending response reconciliation failed: " + err.Error())
			// Preserve the encrypted record on failure, and let other entries advance.
			if db.Statement.Context.Err() == nil {
				_ = db.Model(&ConversationAuditPendingResponse{}).Where("audit_id = ?", item.AuditID).Update("next_attempt_at", now+5).Error
			}
		}
		if db.Statement.Context.Err() != nil {
			break
		}
	}
}
