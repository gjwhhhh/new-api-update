package model

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	EmailPrepared  = "prepared"
	EmailPending   = "pending"
	EmailSending   = "sending"
	EmailRetry     = "retry"
	EmailAccepted  = "accepted"
	EmailFailed    = "failed"
	EmailUnknown   = "unknown"
	EmailCancelled = "cancelled"
	EmailSkipped   = "skipped"
)

var ErrEmailConflict = errors.New("Email batch expired or unavailable. Refresh the preview and try again.")

type EmailMessage struct {
	ID        int64  `json:"id" gorm:"primaryKey"`
	Subject   string `json:"subject" gorm:"type:varchar(800)"`
	Body      string `json:"body" gorm:"size:1048576"`
	Kind      string `json:"kind" gorm:"type:varchar(64);index"`
	Source    string `json:"source" gorm:"type:varchar(24);index"`
	Redacted  bool   `json:"redacted"`
	CreatedAt int64  `json:"created_at" gorm:"autoCreateTime;index"`
}

type EmailExclusion struct {
	UserID int    `json:"user_id"`
	Email  string `json:"email"`
	Reason string `json:"reason"`
}

type EmailBatch struct {
	LastTestAt int64            `json:"-"`
	ID         int64            `json:"id" gorm:"primaryKey"`
	MessageID  int64            `json:"message_id" gorm:"index"`
	ActorID    int              `json:"actor_id" gorm:"index"`
	ActorName  string           `json:"actor_name" gorm:"type:varchar(64)"`
	Status     string           `json:"status" gorm:"type:varchar(24);index"`
	Total      int              `json:"total"`
	Matched    int              `json:"matched"`
	Duplicates int              `json:"duplicates"`
	Exclusions []EmailExclusion `json:"exclusions" gorm:"serializer:json;size:8388608"`
	Selection  string           `json:"-" gorm:"size:1048576"`
	ExpiresAt  int64            `json:"expires_at"`
	CreatedAt  int64            `json:"created_at" gorm:"autoCreateTime;index"`
	UpdatedAt  int64            `json:"updated_at" gorm:"autoUpdateTime"`
}

type EmailDelivery struct {
	ID            int64  `json:"id" gorm:"primaryKey"`
	MessageID     int64  `json:"message_id" gorm:"index"`
	BatchID       int64  `json:"batch_id" gorm:"index:idx_email_batch_status,priority:1"`
	UserID        int    `json:"user_id" gorm:"index:idx_email_user_time,priority:1"`
	UserIDs       []int  `json:"user_ids" gorm:"serializer:json;size:1048576"`
	UserRefs      string `json:"-" gorm:"size:1048576"`
	Username      string `json:"username" gorm:"type:varchar(64)"`
	Email         string `json:"email" gorm:"type:varchar(320);index:idx_email_address_time,priority:1"`
	Status        string `json:"status" gorm:"type:varchar(24);index:idx_email_batch_status,priority:2;index:idx_email_due,priority:1"`
	Attempts      int    `json:"attempts"`
	RetryRound    int    `json:"-"`
	NextAttemptAt int64  `json:"next_attempt_at" gorm:"index:idx_email_due,priority:2"`
	LockedBy      string `json:"-" gorm:"type:varchar(64)"`
	LockedUntil   int64  `json:"-" gorm:"index"`
	LastError     string `json:"last_error" gorm:"type:varchar(255)"`
	CreatedAt     int64  `json:"created_at" gorm:"autoCreateTime;index:idx_email_user_time,priority:2;index:idx_email_address_time,priority:2"`
	UpdatedAt     int64  `json:"updated_at" gorm:"autoUpdateTime"`
}

type EmailAttempt struct {
	ID         int64  `json:"id" gorm:"primaryKey"`
	DeliveryID int64  `json:"delivery_id" gorm:"index"`
	MessageID  string `json:"message_id" gorm:"type:varchar(255)"`
	Stage      string `json:"stage" gorm:"type:varchar(24)"`
	Status     string `json:"status" gorm:"type:varchar(24)"`
	Error      string `json:"error" gorm:"type:varchar(255)"`
	SMTPCode   int    `json:"smtp_code"`
	StartedAt  int64  `json:"started_at"`
	FinishedAt int64  `json:"finished_at"`
}

type EmailRecipient struct {
	ID       int    `json:"id" gorm:"column:id"`
	Username string `json:"username"`
	Email    string `json:"email"`
	Group    string `json:"group"`
	Status   int    `json:"status"`
}

type EmailRecipientFilter struct {
	Keyword string `json:"keyword" form:"keyword"`
	Group   string `json:"group" form:"group"`
	Status  int    `json:"status" form:"status"`
	IDs     []int  `json:"ids" form:"-"`
	All     bool   `json:"all" form:"-"`
}

func FindEmailRecipients(filter EmailRecipientFilter, offset, limit int) ([]EmailRecipient, int64, error) {
	q := DB.Model(&User{})
	if filter.Keyword != "" {
		value := "%" + filter.Keyword + "%"
		id, _ := strconv.Atoi(filter.Keyword)
		q = q.Where("(username LIKE ? OR email LIKE ? OR display_name LIKE ? OR id = ?)", value, value, value, id)
	}
	if filter.Group != "" {
		q = q.Where(clause.Eq{Column: clause.Column{Name: "group"}, Value: filter.Group})
	}
	if filter.Status == 0 {
		filter.Status = common.UserStatusEnabled
	}
	if filter.Status != common.UserStatusEnabled && filter.Status != common.UserStatusDisabled {
		return nil, 0, errors.New("Invalid recipient filters")
	}
	q = q.Where("status = ?", filter.Status)
	if !filter.All {
		if len(filter.IDs) == 0 {
			return []EmailRecipient{}, 0, nil
		}
		q = q.Where("id IN ?", filter.IDs)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	rows := []EmailRecipient{}
	err := q.Select("id", "username", "email", "group", "status").Order("id asc").Offset(max(offset, 0)).Limit(limit).Find(&rows).Error
	return rows, total, err
}

func CreateEmailBatch(message *EmailMessage, batch *EmailBatch, deliveries []EmailDelivery) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(message).Error; err != nil {
			return err
		}
		batch.MessageID = message.ID
		if err := tx.Create(batch).Error; err != nil {
			return err
		}
		for i := range deliveries {
			deliveries[i].BatchID = batch.ID
			deliveries[i].MessageID = message.ID
		}
		return tx.CreateInBatches(&deliveries, 100).Error
	})
}

func CreateAutomaticEmail(message *EmailMessage, delivery *EmailDelivery) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(message).Error; err != nil {
			return err
		}
		delivery.MessageID = message.ID
		return tx.Create(delivery).Error
	})
}

func GetEmailBatch(id int64) (*EmailBatch, error) {
	var row EmailBatch
	err := DB.First(&row, id).Error
	return &row, err
}
func GetEmailMessage(id int64) (*EmailMessage, error) {
	var row EmailMessage
	err := DB.First(&row, id).Error
	return &row, err
}

// ConfirmEmailBatch is idempotent for its creator. The immutable prepared batch
// is the submission key; another account cannot submit its recipient snapshot.
func ConfirmEmailBatch(id int64, actorID int, now int64) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var batch EmailBatch
		if err := lockForUpdate(tx).First(&batch, id).Error; err != nil {
			return err
		}
		if batch.ActorID != actorID {
			return ErrEmailConflict
		}
		if batch.Status == EmailPending {
			return nil
		}
		if batch.Status != EmailPrepared || batch.ExpiresAt <= now {
			return ErrEmailConflict
		}
		result := tx.Model(&EmailBatch{}).Where("id = ? AND status = ?", id, EmailPrepared).Updates(map[string]any{"status": EmailPending, "updated_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrEmailConflict
		}
		return tx.Model(&EmailDelivery{}).Where("batch_id = ? AND status = ?", id, EmailPrepared).Updates(map[string]any{"status": EmailPending, "next_attempt_at": now, "updated_at": now}).Error
	})
}

func OperateEmailBatch(id int64, action string, now int64) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var batch EmailBatch
		if err := lockForUpdate(tx).First(&batch, id).Error; err != nil {
			return err
		}
		if batch.Status != EmailPending {
			return ErrEmailConflict
		}
		if action == "cancel" {
			if err := tx.Model(&EmailBatch{}).Where("id = ?", id).Updates(map[string]any{"status": EmailCancelled, "updated_at": now}).Error; err != nil {
				return err
			}
			return tx.Model(&EmailDelivery{}).Where("batch_id = ? AND status IN ?", id, []string{EmailPending, EmailRetry}).Updates(map[string]any{"status": EmailCancelled, "updated_at": now}).Error
		}
		if action != "retry" {
			return ErrEmailConflict
		}
		return tx.Model(&EmailDelivery{}).Where("batch_id = ? AND status = ?", id, EmailFailed).Updates(map[string]any{"status": EmailPending, "retry_round": 0, "next_attempt_at": now, "updated_at": now}).Error
	})
}

// ClaimEmailDelivery rechecks the batch's cancel flag while holding the same
// batch lock as cancellation. Network I/O happens only after this commits.
func ClaimEmailDelivery(id int64, owner string, now int64) (*EmailDelivery, error) {
	// Node names have no length limit; keep the lease identity within its column.
	owner = fmt.Sprintf("%x", sha256.Sum256([]byte(owner)))
	var delivery EmailDelivery
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&delivery, id).Error; err != nil {
			return err
		}
		var batch EmailBatch
		if err := lockForUpdate(tx).First(&batch, delivery.BatchID).Error; err != nil {
			return err
		}
		if batch.Status != EmailPending {
			return ErrEmailConflict
		}
		result := tx.Model(&EmailDelivery{}).Where("id = ? AND status IN ? AND next_attempt_at <= ?", id, []string{EmailPending, EmailRetry}, now).
			Updates(map[string]any{"status": EmailSending, "locked_by": owner, "locked_until": now + 120, "attempts": gorm.Expr("attempts + 1"), "retry_round": gorm.Expr("retry_round + 1"), "updated_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrEmailConflict
		}
		return tx.First(&delivery, id).Error
	})
	return &delivery, err
}

func FindDueEmailDeliveries(now int64, limit int) ([]EmailDelivery, error) {
	rows := []EmailDelivery{}
	err := DB.Where("batch_id IN (?)", DB.Model(&EmailBatch{}).Select("id").Where("status = ?", EmailPending)).
		Where("status IN ? AND next_attempt_at <= ?", []string{EmailPending, EmailRetry}, now).Order("id asc").Limit(limit).Find(&rows).Error
	return rows, err
}

func StartEmailAttempt(deliveryID int64, owner string, now int64) (*EmailAttempt, error) {
	attempt := &EmailAttempt{DeliveryID: deliveryID, StartedAt: now, Status: EmailSending, Stage: "claimed"}
	err := DB.Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&EmailDelivery{}).Where("id = ? AND status = ? AND locked_by = ? AND locked_until > ?", deliveryID, EmailSending, owner, now).Count(&count).Error; err != nil {
			return err
		}
		if count != 1 {
			return ErrEmailConflict
		}
		return tx.Create(attempt).Error
	})
	return attempt, err
}

func FinishEmailDelivery(delivery *EmailDelivery, attempt *EmailAttempt, status string, next int64) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&EmailDelivery{}).Where("id = ? AND status = ? AND locked_by = ?", delivery.ID, EmailSending, delivery.LockedBy).
			Updates(map[string]any{"status": status, "next_attempt_at": next, "locked_by": "", "locked_until": 0, "last_error": attempt.Error, "updated_at": attempt.FinishedAt})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrEmailConflict
		}
		return tx.Model(&EmailAttempt{}).Where("id = ? AND status = ?", attempt.ID, EmailSending).Updates(map[string]any{
			"status": attempt.Status, "stage": attempt.Stage, "smtp_code": attempt.SMTPCode, "message_id": attempt.MessageID, "error": attempt.Error, "finished_at": attempt.FinishedAt,
		}).Error
	})
}

func RecoverEmailDeliveries(now int64) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var ids []int64
		if err := tx.Model(&EmailDelivery{}).Where("status = ? AND locked_until < ?", EmailSending, now).Pluck("id", &ids).Error; err != nil {
			return err
		}
		if len(ids) > 0 {
			if err := tx.Model(&EmailAttempt{}).Where("delivery_id IN ? AND status = ?", ids, EmailSending).Updates(map[string]any{"status": EmailUnknown, "stage": "interrupted", "error": "SMTP outcome requires review", "finished_at": now}).Error; err != nil {
				return err
			}
			if err := tx.Model(&EmailDelivery{}).Where("id IN ? AND status = ? AND locked_until < ?", ids, EmailSending, now).Updates(map[string]any{"status": EmailUnknown, "locked_by": "", "locked_until": 0, "last_error": "SMTP outcome requires review", "updated_at": now}).Error; err != nil {
				return err
			}
		}
		if err := tx.Model(&EmailDelivery{}).Where("status = ? AND batch_id IN (?)", EmailPrepared, tx.Model(&EmailBatch{}).Select("id").Where("status = ? AND expires_at <= ?", EmailPrepared, now)).Updates(map[string]any{"status": EmailCancelled, "updated_at": now}).Error; err != nil {
			return err
		}
		return tx.Model(&EmailBatch{}).Where("status = ? AND expires_at <= ?", EmailPrepared, now).Updates(map[string]any{"status": EmailCancelled, "updated_at": now}).Error
	})
}

type EmailRecord struct {
	EmailDelivery
	Subject  string `json:"subject"`
	Kind     string `json:"kind"`
	Source   string `json:"source"`
	Redacted bool   `json:"redacted"`
}
type EmailRecordFilter struct {
	Keyword string `form:"keyword"`
	UserID  int    `form:"user_id"`
	Status  string `form:"status"`
	Kind    string `form:"kind"`
	Source  string `form:"source"`
	BatchID int64  `form:"batch_id"`
	Start   int64  `form:"start"`
	End     int64  `form:"end"`
}

func ListEmailRecords(filter EmailRecordFilter, offset, limit int) ([]EmailRecord, int64, error) {
	q := DB.Table("email_deliveries AS d").Joins("JOIN email_messages AS m ON m.id = d.message_id").Where("d.status <> ?", EmailPrepared)
	if filter.Keyword != "" {
		like := "%" + filter.Keyword + "%"
		q = q.Where("(d.email LIKE ? OR d.username LIKE ? OR m.subject LIKE ?)", like, like, like)
	}
	if filter.UserID > 0 {
		q = q.Where("(d.user_id = ? OR d.user_refs LIKE ?)", filter.UserID, "%,"+strconv.Itoa(filter.UserID)+",%")
	}
	if filter.Status != "" {
		q = q.Where("d.status = ?", filter.Status)
	}
	if filter.Kind != "" {
		q = q.Where("m.kind = ?", filter.Kind)
	}
	if filter.Source != "" {
		q = q.Where("m.source = ?", filter.Source)
	}
	if filter.BatchID > 0 {
		q = q.Where("d.batch_id = ?", filter.BatchID)
	}
	if filter.Start > 0 {
		q = q.Where("d.created_at >= ?", filter.Start)
	}
	if filter.End > 0 {
		q = q.Where("d.created_at <= ?", filter.End)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	rows := []EmailRecord{}
	err := q.Select("d.*, m.subject, m.kind, m.source, m.redacted").Order("d.id desc").Offset(max(offset, 0)).Limit(min(max(limit, 1), 100)).Find(&rows).Error
	return rows, total, err
}

func GetEmailRecord(id int64) (*EmailRecord, *EmailMessage, []EmailAttempt, error) {
	var record EmailRecord
	err := DB.Table("email_deliveries AS d").Select("d.*, m.subject, m.kind, m.source, m.redacted").Joins("JOIN email_messages AS m ON m.id = d.message_id").Where("d.id = ? AND d.status <> ?", id, EmailPrepared).Take(&record).Error
	if err != nil {
		return nil, nil, nil, err
	}
	message, err := GetEmailMessage(record.EmailDelivery.MessageID)
	if err != nil {
		return nil, nil, nil, err
	}
	attempts := []EmailAttempt{}
	err = DB.Where("delivery_id = ?", id).Order("id asc").Find(&attempts).Error
	return &record, message, attempts, err
}

type EmailBatchSummary struct {
	EmailBatch
	Subject string           `json:"subject"`
	Counts  map[string]int64 `json:"counts" gorm:"-"`
}

func ListEmailBatches(offset, limit int) ([]EmailBatchSummary, int64, error) {
	var total int64
	if err := DB.Model(&EmailBatch{}).Where("status <> ?", EmailPrepared).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	rows := []EmailBatchSummary{}
	err := DB.Table("email_batches AS b").Select("b.*, m.subject").Joins("JOIN email_messages AS m ON m.id = b.message_id").Where("b.status <> ?", EmailPrepared).Order("b.id desc").Offset(max(offset, 0)).Limit(min(max(limit, 1), 100)).Find(&rows).Error
	if err != nil {
		return nil, 0, err
	}
	ids := make([]int64, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	var counts []struct {
		BatchID int64
		Status  string
		Count   int64
	}
	if len(ids) > 0 {
		err = DB.Model(&EmailDelivery{}).Select("batch_id, status, COUNT(*) AS count").Where("batch_id IN ?", ids).Group("batch_id, status").Find(&counts).Error
	}
	for i := range rows {
		rows[i].Counts = map[string]int64{}
		for _, count := range counts {
			if count.BatchID == rows[i].ID {
				rows[i].Counts[count.Status] = count.Count
			}
		}
	}
	return rows, total, err
}

// CleanupEmailHistory deletes complete message groups in bounded transactions.
// Unknown outcomes remain available for investigation and are never auto-pruned.
func CleanupEmailHistory(cutoff int64) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var ids []int64
		activeStatuses := []string{EmailPrepared, EmailPending, EmailSending, EmailRetry, EmailUnknown}
		active := tx.Model(&EmailDelivery{}).Select("message_id").Where("status IN ? OR updated_at >= ?", activeStatuses, cutoff)
		if err := tx.Model(&EmailMessage{}).Where("created_at < ? AND id NOT IN (?)", cutoff, active).Order("id asc").Limit(100).Pluck("id", &ids).Error; err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		// Serialize with retry/cancel/claim, then use locking reads so MySQL's
		// repeatable-read snapshot cannot hide a retry committed after selection.
		var batches []EmailBatch
		if err := lockForUpdate(tx).Select("id").Where("message_id IN ?", ids).Order("id asc").Find(&batches).Error; err != nil {
			return err
		}
		var current []EmailDelivery
		if err := lockForUpdate(tx).Select("id", "message_id", "status", "updated_at").Where("message_id IN ?", ids).Order("id asc").Find(&current).Error; err != nil {
			return err
		}
		protected := make(map[int64]bool)
		for _, delivery := range current {
			if delivery.UpdatedAt >= cutoff || slices.Contains(activeStatuses, delivery.Status) {
				protected[delivery.MessageID] = true
			}
		}
		ids = slices.DeleteFunc(ids, func(id int64) bool { return protected[id] })
		if len(ids) == 0 {
			return nil
		}
		deliveries := tx.Model(&EmailDelivery{}).Select("id").Where("message_id IN ?", ids)
		if err := tx.Where("delivery_id IN (?)", deliveries).Delete(&EmailAttempt{}).Error; err != nil {
			return err
		}
		if err := tx.Where("message_id IN ?", ids).Delete(&EmailDelivery{}).Error; err != nil {
			return err
		}
		if err := tx.Where("message_id IN ?", ids).Delete(&EmailBatch{}).Error; err != nil {
			return err
		}
		return tx.Where("id IN ?", ids).Delete(&EmailMessage{}).Error
	})
}

func EmailUserRefs(ids []int) string {
	var refs strings.Builder
	refs.WriteByte(',')
	for _, id := range ids {
		fmt.Fprintf(&refs, "%d,", id)
	}
	return refs.String()
}

func ReserveEmailTest(batchID int64, actorID int, now int64) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var user User
		if err := lockForUpdate(tx).Select("id").First(&user, actorID).Error; err != nil {
			return err
		}
		var count int64
		if err := tx.Model(&EmailBatch{}).Where("actor_id = ? AND last_test_at > ?", actorID, now-10).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return errors.New("Please wait before sending another test email")
		}
		return tx.Model(&EmailBatch{}).Where("id = ? AND actor_id = ?", batchID, actorID).Update("last_test_at", now).Error
	})
}
