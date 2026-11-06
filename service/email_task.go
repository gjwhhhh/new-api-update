package service

import (
	"context"
	"errors"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

type emailDispatchHandler struct{}

func (emailDispatchHandler) Type() string { return "email_dispatch" }

// Idle installations need only an hourly maintenance pass. The scheduler
// still discovers pending batches and expired delivery leases every 15 seconds.
func (handler emailDispatchHandler) Enabled() bool {
	if model.DB == nil {
		return false
	}
	now := common.GetTimestamp()
	latest, err := model.GetLatestSystemTask(handler.Type())
	if err != nil {
		common.SysError("email scheduler state read failed")
		return false
	}
	if latest == nil || now-latest.UpdatedAt >= 3600 {
		return true
	}
	query := model.DB.Model(&model.EmailDelivery{}).Where("(status = ? AND locked_until < ?) OR (status = ? AND created_at <= ?)", model.EmailSending, now, model.EmailPrepared, now-900)
	if operation_setting.GetMailSetting().SendingEnabled {
		query = query.Or("status IN ? AND next_attempt_at <= ?", []string{model.EmailPending, model.EmailRetry}, now)
	}
	var work []int64
	if err := query.Select("id").Limit(1).Find(&work).Error; err != nil {
		common.SysError("email scheduler work read failed")
		return false
	}
	return len(work) > 0
}
func (emailDispatchHandler) Interval() time.Duration { return 15 * time.Second }
func (emailDispatchHandler) NewPayload() any         { return nil }
func init()                                          { RegisterSystemTaskHandler(emailDispatchHandler{}) }

func (emailDispatchHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	now := common.GetTimestamp()
	if err := model.RecoverEmailDeliveries(now); err != nil {
		failSystemTask(task, runnerID, err)
		return
	}
	settings := operation_setting.GetMailSetting()
	// Cleanup is independent of the sending switch. A bounded transaction
	// limits pressure on the primary DB.
	if err := model.CleanupEmailHistory(now - int64(settings.RetentionDays)*86400); err != nil {
		failSystemTask(task, runnerID, err)
		return
	}
	if settings.SendingEnabled {
		// Bounded passes yield to other batches and let the task lease govern all
		// SMTP work. A delivery lease is longer than the transport's hard timeout.
		for range 25 {
			if ctx.Err() != nil {
				return
			}
			settings = operation_setting.GetMailSetting()
			if !settings.SendingEnabled {
				break
			}
			due, err := model.FindDueEmailDeliveries(common.GetTimestamp(), 1)
			if err != nil {
				failSystemTask(task, runnerID, err)
				return
			}
			if len(due) == 0 {
				break
			}
			if err := dispatchEmailDelivery(ctx, due[0].ID, runnerID); err != nil && !errors.Is(err, model.ErrEmailConflict) {
				failSystemTask(task, runnerID, err)
				return
			}
			timer := time.NewTimer(time.Duration(settings.IntervalMilliseconds) * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}
	if err := model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusSucceeded, nil, ""); err != nil {
		common.SysError("email dispatcher completion failed")
	}
}

func dispatchEmailDelivery(ctx context.Context, id int64, owner string) error {
	delivery, err := model.ClaimEmailDelivery(id, owner, common.GetTimestamp())
	if err != nil {
		return err
	}
	attempt, err := model.StartEmailAttempt(delivery.ID, delivery.LockedBy, common.GetTimestamp())
	if err != nil {
		return err
	}
	// Every claimed item gets an attempt, including skips and preflight failures.
	unchanged, err := emailRecipientUnchanged(delivery)
	if err != nil {
		return err
	}
	if !unchanged {
		attempt.Status = model.EmailSkipped
		attempt.Stage = "recipient_check"
		attempt.Error = "User deleted or email address changed"
		attempt.FinishedAt = common.GetTimestamp()
		return model.FinishEmailDelivery(delivery, attempt, model.EmailSkipped, 0)
	}
	message, err := model.GetEmailMessage(delivery.MessageID)
	if err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	result, _ := common.SendEmailWithResult(ctx, message.Subject, delivery.Email, message.Body)
	completeEmailAttempt(attempt, result)
	status, next := result.Status, int64(0)
	if status == model.EmailFailed && result.Retryable && delivery.RetryRound < 3 {
		status = model.EmailRetry
		delay := int64(60)
		if delivery.RetryRound > 1 {
			delay = 300
		}
		next = common.GetTimestamp() + delay
	}
	return model.FinishEmailDelivery(delivery, attempt, status, next)
}
