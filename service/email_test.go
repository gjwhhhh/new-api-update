package service

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/mail"
	"net/textproto"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// All database fixtures use isolated test databases, never application DSNs.
func mailDatabase(t *testing.T, dialect string) *gorm.DB {
	t.Helper()
	var driver gorm.Dialector
	switch dialect {
	case "mysql":
		dsn := os.Getenv("TEST_MYSQL_DSN")
		if dsn == "" {
			t.Skip("TEST_MYSQL_DSN is not configured")
		}
		driver = mysql.Open(dsn)
	case "postgres":
		dsn := os.Getenv("TEST_POSTGRES_DSN")
		if dsn == "" {
			t.Skip("TEST_POSTGRES_DSN is not configured")
		}
		driver = postgres.Open(dsn)
	default:
		driver = sqlite.Open(":memory:")
	}
	db, err := gorm.Open(driver, &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	previous, previousType := model.DB, common.MainDatabaseType()
	model.DB = db
	common.SetMainDatabaseType(common.DatabaseType(dialect))
	tables := []any{&model.EmailAttempt{}, &model.EmailDelivery{}, &model.EmailBatch{}, &model.EmailMessage{}, &model.User{}, &model.SystemTask{}, &model.SystemTaskLock{}}
	for _, table := range tables {
		require.NoError(t, db.Migrator().DropTable(table))
	}
	t.Cleanup(func() {
		for _, table := range tables {
			require.NoError(t, db.Migrator().DropTable(table))
		}
		model.DB = previous
		common.SetMainDatabaseType(previousType)
		require.NoError(t, sqlDB.Close())
	})
	// An existing released User schema/data must survive adding email tables.
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.SystemTask{}, &model.SystemTaskLock{}))
	users := []model.User{
		{Id: 1, Username: "operator", Password: "fixture", AffCode: "mail-admin", Role: 100, Status: 1, Email: "admin@example.com"},
		{Id: 2, Username: "alice", Password: "fixture", AffCode: "mail-alice", Status: 1, Email: "alice@example.com"},
		{Id: 3, Username: "alice-shared", Password: "fixture", AffCode: "mail-shared", Status: 1, Email: "alice@example.com"},
		{Id: 4, Username: "no-email", Password: "fixture", AffCode: "mail-none", Status: 1},
		{Id: 5, Username: "disabled", Password: "fixture", AffCode: "mail-disabled", Status: 2, Email: "disabled@example.com"},
	}
	require.NoError(t, db.Create(&users).Error)
	for range 2 {
		require.NoError(t, db.AutoMigrate(&model.EmailMessage{}, &model.EmailBatch{}, &model.EmailDelivery{}, &model.EmailAttempt{}))
	}
	var count int64
	require.NoError(t, db.Model(&model.User{}).Count(&count).Error)
	require.EqualValues(t, 5, count)
	var version string
	query := "SELECT version()"
	if dialect == "sqlite" {
		query = "SELECT sqlite_version()"
	}
	require.NoError(t, db.Raw(query).Scan(&version).Error)
	t.Logf("%s version: %s", dialect, version)
	return db
}

// The SMTP fixture exercises the actual transport, including DATA ambiguity.
func mailSMTPFixture(t *testing.T, mode string) <-chan string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	host, portText, err := net.SplitHostPort(listener.Addr().String())
	require.NoError(t, err)
	port, err := strconv.Atoi(portText)
	require.NoError(t, err)
	oldServer, oldPort, oldFrom, oldAccount, oldToken := common.SMTPServer, common.SMTPPort, common.SMTPFrom, common.SMTPAccount, common.SMTPToken
	oldSSL, oldTLS := common.SMTPSSLEnabled, common.SMTPStartTLSEnabled
	common.SMTPServer, common.SMTPPort, common.SMTPFrom, common.SMTPAccount, common.SMTPToken = host, port, "sender@example.com", "", ""
	common.SMTPSSLEnabled, common.SMTPStartTLSEnabled = false, false
	var workers sync.WaitGroup
	messages := make(chan string, 20)
	workers.Go(func() {
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			workers.Go(func() {
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
				reader := textproto.NewReader(bufio.NewReader(conn))
				writer := textproto.NewWriter(bufio.NewWriter(conn))
				if writer.PrintfLine("220 mail.test ESMTP") != nil {
					return
				}
				for {
					line, readErr := reader.ReadLine()
					if readErr != nil {
						return
					}
					switch {
					case strings.HasPrefix(line, "EHLO"), strings.HasPrefix(line, "HELO"), strings.HasPrefix(line, "MAIL"), strings.HasPrefix(line, "RCPT"):
						if writer.PrintfLine("250 OK") != nil {
							return
						}
					case line == "DATA":
						if writer.PrintfLine("354 Continue") != nil {
							return
						}
						raw, readErr := reader.ReadDotBytes()
						if readErr != nil {
							return
						}
						message, parseErr := mail.ReadMessage(strings.NewReader(string(raw)))
						if parseErr != nil {
							return
						}
						body, decodeErr := io.ReadAll(base64.NewDecoder(base64.StdEncoding, message.Body))
						if decodeErr != nil {
							return
						}
						messages <- string(body)
						if mode == "unknown" {
							return
						}
						if mode == "temporary" {
							if writer.PrintfLine("450 Temporary rejection") != nil {
								return
							}
						} else if mode == "permanent" {
							if writer.PrintfLine("550 Rejected") != nil {
								return
							}
						} else {
							if writer.PrintfLine("250 Queued") != nil {
								return
							}
						}
					case line == "QUIT":
						if mode != "quit_failure" {
							_ = writer.PrintfLine("221 Bye")
						}
						return
					default:
						if writer.PrintfLine("250 OK") != nil {
							return
						}
					}
				}
			})
		}
	})
	t.Cleanup(func() {
		listener.Close()
		workers.Wait()
		common.SMTPServer, common.SMTPPort, common.SMTPFrom, common.SMTPAccount, common.SMTPToken = oldServer, oldPort, oldFrom, oldAccount, oldToken
		common.SMTPSSLEnabled, common.SMTPStartTLSEnabled = oldSSL, oldTLS
	})
	return messages
}

func TestEmailCenterDatabaseMatrix(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			db := mailDatabase(t, dialect)
			mailSMTPFixture(t, "accepted")
			request := EmailPreviewRequest{Subject: "Service update", Body: "Hello **everyone**\n- [Status](https://example.com/status)", Selection: model.EmailRecipientFilter{All: true}}
			preview, err := PrepareEmailBatch(1, "operator", request)
			require.NoError(t, err)
			assert.Equal(t, 2, preview.Batch.Total)
			assert.Equal(t, 4, preview.Batch.Matched)
			assert.Equal(t, 1, preview.Batch.Duplicates)
			require.Len(t, preview.Batch.Exclusions, 1)
			persisted, err := model.GetEmailBatch(preview.Batch.ID)
			require.NoError(t, err)
			assert.Equal(t, preview.Batch.Exclusions, persisted.Exclusions)
			require.ErrorIs(t, model.ConfirmEmailBatch(preview.Batch.ID, 2, common.GetTimestamp()), model.ErrEmailConflict)
			require.NoError(t, model.ConfirmEmailBatch(preview.Batch.ID, 1, common.GetTimestamp()))
			require.NoError(t, model.ConfirmEmailBatch(preview.Batch.ID, 1, common.GetTimestamp()), "replayed submission must not duplicate recipients")
			records, total, err := model.ListEmailRecords(model.EmailRecordFilter{BatchID: preview.Batch.ID}, 0, 20)
			require.NoError(t, err)
			require.EqualValues(t, 2, total)
			var shared model.EmailRecord
			for _, record := range records {
				if record.Email == "alice@example.com" {
					shared = record
				}
			}
			require.NotZero(t, shared.ID)
			assert.Equal(t, []int{2, 3}, shared.UserIDs)
			_, total, err = model.ListEmailRecords(model.EmailRecordFilter{UserID: 3}, 0, 20)
			require.NoError(t, err)
			assert.EqualValues(t, 1, total)
			// Competing workers may only claim a recipient once.
			claims := make(chan error, 2)
			var concurrent sync.WaitGroup
			for i := range 2 {
				concurrent.Go(func() {
					_, claimErr := model.ClaimEmailDelivery(shared.ID, fmt.Sprint(i), common.GetTimestamp())
					claims <- claimErr
				})
			}
			concurrent.Wait()
			close(claims)
			succeeded := 0
			for claimErr := range claims {
				if claimErr == nil {
					succeeded++
				} else {
					require.ErrorIs(t, claimErr, model.ErrEmailConflict)
				}
			}
			assert.Equal(t, 1, succeeded)
			require.NoError(t, db.Model(&model.EmailDelivery{}).Where("id = ?", shared.ID).Update("locked_until", common.GetTimestamp()-1).Error)
			require.NoError(t, model.RecoverEmailDeliveries(common.GetTimestamp()))
			require.NoError(t, db.First(&shared.EmailDelivery, shared.ID).Error)
			assert.Equal(t, model.EmailUnknown, shared.Status)
			require.NoError(t, model.OperateEmailBatch(preview.Batch.ID, "retry", common.GetTimestamp()))
			due, err := model.FindDueEmailDeliveries(common.GetTimestamp(), 20)
			require.NoError(t, err)
			require.Len(t, due, 1, "unknown recipient must never be retried")
			require.NoError(t, dispatchEmailDelivery(context.Background(), due[0].ID, strings.Repeat("node", 20)+"-12345678"), "long node names must still complete delivery")
			_, message, attempts, err := model.GetEmailRecord(due[0].ID)
			require.NoError(t, err)
			assert.Contains(t, message.Body, "<strong>everyone</strong>")
			require.Len(t, attempts, 1)
			assert.Equal(t, model.EmailAccepted, attempts[0].Status)
			batches, total, err := model.ListEmailBatches(0, 20)
			require.NoError(t, err)
			require.EqualValues(t, 1, total)
			assert.EqualValues(t, 1, batches[0].Counts[model.EmailAccepted])
			assert.EqualValues(t, 1, batches[0].Counts[model.EmailUnknown])
			// Cleanup preserves the entire message when any outcome is unknown.
			require.NoError(t, db.Model(&model.EmailMessage{}).Where("id = ?", message.ID).Update("created_at", 1).Error)
			require.NoError(t, db.Model(&model.EmailDelivery{}).Where("message_id = ?", message.ID).Update("updated_at", 1).Error)
			require.NoError(t, model.CleanupEmailHistory(common.GetTimestamp()-86400))
			_, err = model.GetEmailMessage(message.ID)
			require.NoError(t, err)
			require.NoError(t, db.Model(&model.EmailDelivery{}).Where("id = ?", shared.ID).Update("status", model.EmailFailed).Error)
			require.NoError(t, model.CleanupEmailHistory(common.GetTimestamp()+1))
			_, err = model.GetEmailMessage(message.ID)
			require.ErrorIs(t, err, gorm.ErrRecordNotFound)
			for _, table := range []any{&model.EmailBatch{}, &model.EmailDelivery{}, &model.EmailAttempt{}} {
				var remaining int64
				require.NoError(t, db.Model(table).Count(&remaining).Error)
				assert.Zero(t, remaining)
			}
			// All indexes and existing email data survive repeated startup migrations.
			fresh, err := PrepareEmailBatch(1, "operator", request)
			require.NoError(t, err)
			for range 2 {
				require.NoError(t, db.AutoMigrate(&model.EmailMessage{}, &model.EmailBatch{}, &model.EmailDelivery{}, &model.EmailAttempt{}))
			}
			stored, err := model.GetEmailBatch(fresh.Batch.ID)
			require.NoError(t, err)
			assert.Equal(t, fresh.Batch.Total, stored.Total)
			// Both body content and exclusion snapshots may exceed MySQL TEXT's 64 KiB.
			largeBody := strings.Repeat("x", 200*1024)
			large, err := PrepareEmailBatch(1, "operator", EmailPreviewRequest{Subject: "Long notice", Body: largeBody, Selection: model.EmailRecipientFilter{IDs: []int{2}}})
			require.NoError(t, err)
			largeMessage, err := model.GetEmailMessage(large.Message.ID)
			require.NoError(t, err)
			assert.Contains(t, largeMessage.Body, largeBody)
			exclusions := make([]model.EmailExclusion, 1000)
			for i := range exclusions {
				exclusions[i] = model.EmailExclusion{UserID: i + 100, Email: strings.Repeat("x", 100), Reason: "No valid email address"}
			}
			require.NoError(t, db.Model(&model.EmailBatch{}).Where("id = ?", large.Batch.ID).Select("exclusions").Updates(&model.EmailBatch{Exclusions: exclusions}).Error)
			stored, err = model.GetEmailBatch(large.Batch.ID)
			require.NoError(t, err)
			assert.Equal(t, exclusions, stored.Exclusions)
		})
	}
}

func TestEmailCleanupPreservesConcurrentRetry(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} {
		for _, action := range []string{"retry", "claim"} {
			t.Run(dialect+"/"+action, func(t *testing.T) {
				db := mailDatabase(t, dialect)
				sqlDB, err := db.DB()
				require.NoError(t, err)
				sqlDB.SetMaxOpenConns(4)
				now := common.GetTimestamp()
				message := &model.EmailMessage{Subject: "Old failure", CreatedAt: 1}
				batch := &model.EmailBatch{Status: model.EmailPending, CreatedAt: 1, UpdatedAt: 1}
				require.NoError(t, model.CreateEmailBatch(message, batch, []model.EmailDelivery{{Status: model.EmailFailed, CreatedAt: 1, UpdatedAt: 1}}))
				var delivery model.EmailDelivery
				require.NoError(t, db.Where("batch_id = ?", batch.ID).First(&delivery).Error)

				selected, resume := make(chan struct{}), make(chan struct{})
				var release sync.Once
				t.Cleanup(func() { release.Do(func() { close(resume) }) })
				// Pause after candidate selection, before any cleanup locks/deletes.
				require.NoError(t, db.Callback().Query().After("gorm:query").Register("mail_test:cleanup_selection", func(tx *gorm.DB) {
					if tx.Statement.Table != "email_messages" || !strings.Contains(tx.Statement.SQL.String(), "NOT IN") {
						return
					}
					close(selected)
					select {
					case <-resume:
					case <-time.After(10 * time.Second):
						tx.AddError(fmt.Errorf("cleanup test barrier timed out"))
					}
				}))
				t.Cleanup(func() { _ = db.Callback().Query().Remove("mail_test:cleanup_selection") })
				done := make(chan error, 1)
				go func() { done <- model.CleanupEmailHistory(now - 86400) }()
				select {
				case <-selected:
				case <-time.After(10 * time.Second):
					t.Fatal("cleanup did not select the old failure")
				}
				require.NoError(t, model.OperateEmailBatch(batch.ID, "retry", now))
				status := model.EmailPending
				if action == "claim" {
					_, err = model.ClaimEmailDelivery(delivery.ID, "worker", now)
					require.NoError(t, err)
					status = model.EmailSending
				}
				release.Do(func() { close(resume) })
				select {
				case err = <-done:
					require.NoError(t, err)
				case <-time.After(10 * time.Second):
					t.Fatal("cleanup did not finish")
				}
				require.NoError(t, db.First(&delivery, delivery.ID).Error)
				assert.Equal(t, status, delivery.Status)
				_, err = model.GetEmailBatch(batch.ID)
				require.NoError(t, err)
				_, err = model.GetEmailMessage(message.ID)
				require.NoError(t, err)
			})
		}
	}
}

func TestEmailDeliveryOutcomesAndRetries(t *testing.T) {
	for _, test := range []struct {
		mode, status string
		retry        bool
	}{{"accepted", model.EmailAccepted, false}, {"quit_failure", model.EmailAccepted, false}, {"unknown", model.EmailUnknown, false}, {"temporary", model.EmailRetry, true}, {"permanent", model.EmailFailed, false}} {
		t.Run(test.mode, func(t *testing.T) {
			db := mailDatabase(t, "sqlite")
			mailSMTPFixture(t, test.mode)
			preview, err := PrepareEmailBatch(1, "operator", EmailPreviewRequest{Subject: "Update", Body: "Hello", Selection: model.EmailRecipientFilter{IDs: []int{2}}})
			require.NoError(t, err)
			require.NoError(t, model.ConfirmEmailBatch(preview.Batch.ID, 1, common.GetTimestamp()))
			due, err := model.FindDueEmailDeliveries(common.GetTimestamp(), 20)
			require.NoError(t, err)
			require.Len(t, due, 1)
			require.NoError(t, dispatchEmailDelivery(context.Background(), due[0].ID, "worker"))
			var delivery model.EmailDelivery
			require.NoError(t, db.First(&delivery, due[0].ID).Error)
			assert.Equal(t, test.status, delivery.Status)
			if test.retry {
				require.Greater(t, delivery.NextAttemptAt, common.GetTimestamp())
				for range 2 {
					require.NoError(t, db.Model(&delivery).Update("next_attempt_at", common.GetTimestamp()).Error)
					require.NoError(t, dispatchEmailDelivery(context.Background(), delivery.ID, "worker"))
				}
				require.NoError(t, db.First(&delivery, delivery.ID).Error)
				assert.Equal(t, model.EmailFailed, delivery.Status)
				assert.Equal(t, 3, delivery.Attempts)
				_, _, attempts, err := model.GetEmailRecord(delivery.ID)
				require.NoError(t, err)
				assert.Len(t, attempts, 3)
			}
		})
	}
}

func TestEmailSnapshotsCancellationAndSecurityHistory(t *testing.T) {
	db := mailDatabase(t, "sqlite")
	messages := mailSMTPFixture(t, "accepted")
	request := EmailPreviewRequest{Subject: "Notice", Body: "News", Selection: model.EmailRecipientFilter{IDs: []int{2, 5}}}
	preview, err := PrepareEmailBatch(1, "operator", request)
	require.NoError(t, err)
	require.Equal(t, 2, preview.Batch.Total, "explicit disabled recipients must remain selectable")
	require.ErrorIs(t, model.ConfirmEmailBatch(preview.Batch.ID, 1, preview.Batch.ExpiresAt), model.ErrEmailConflict)
	require.NoError(t, model.ConfirmEmailBatch(preview.Batch.ID, 1, common.GetTimestamp()))
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", 2).Update("email", "changed@example.com").Error)
	due, err := model.FindDueEmailDeliveries(common.GetTimestamp(), 20)
	require.NoError(t, err)
	require.NoError(t, dispatchEmailDelivery(context.Background(), due[0].ID, "worker"))
	var changed model.EmailDelivery
	require.NoError(t, db.First(&changed, due[0].ID).Error)
	assert.Equal(t, model.EmailSkipped, changed.Status)
	require.NoError(t, model.OperateEmailBatch(preview.Batch.ID, "cancel", common.GetTimestamp()))
	due, err = model.FindDueEmailDeliveries(common.GetTimestamp(), 20)
	require.NoError(t, err)
	assert.Empty(t, due)
	safe := "<p>Code: [REDACTED]</p>"
	require.NoError(t, SendSystemEmail("verification", 2, "alice@example.com", "Verify", "<p>Code: secret-123456</p>", &safe))
	assert.Contains(t, <-messages, "secret-123456", "the real recipient must still receive the usable verification code")
	records, total, err := model.ListEmailRecords(model.EmailRecordFilter{Kind: "verification"}, 0, 20)
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	_, message, attempts, err := model.GetEmailRecord(records[0].ID)
	require.NoError(t, err)
	assert.Equal(t, safe, message.Body)
	assert.True(t, message.Redacted)
	encoded, err := common.Marshal(attempts)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "secret-123456")
	require.NoError(t, model.ReserveEmailTest(preview.Batch.ID, 1, common.GetTimestamp()))
	require.Error(t, model.ReserveEmailTest(preview.Batch.ID, 1, common.GetTimestamp()))
	next, err := PrepareEmailBatch(1, "operator", request)
	require.NoError(t, err)
	require.Error(t, model.ReserveEmailTest(next.Batch.ID, 1, common.GetTimestamp()), "a new preview cannot bypass actor rate limiting")
}

func TestEmailContentValidationAndSafePreview(t *testing.T) {
	for _, subject := range []string{"", "  ", "Header\r\nBcc: attacker@example.com", strings.Repeat("x", 201), "control\x00"} {
		assert.Error(t, ValidateMailSubject(subject))
	}
	assert.NoError(t, ValidateMailSubject("服务更新"))
	rendered, err := RenderMailBody("<script>alert(1)</script>\n**News**\n- [Safe](https://example.com/?a=1&b=2)\n- [Unsafe](javascript:alert)")
	require.NoError(t, err)
	assert.NotContains(t, rendered, "<script>")
	assert.Contains(t, rendered, "<strong>News</strong>")
	assert.NotContains(t, rendered, `href="javascript:`)
	sanitized := SanitizeMailHTML(`<p onclick="bad()">Hi</p><img src="https://tracking.test"><a href="javascript:alert(1)">bad</a><a href="https://example.com">safe</a>`)
	assert.NotContains(t, sanitized, "onclick")
	assert.NotContains(t, sanitized, "tracking.test")
	assert.NotContains(t, sanitized, "javascript:")
	assert.Contains(t, sanitized, `href="https://example.com"`)
	_, err = RenderMailBody(" ")
	assert.Error(t, err)
	_, err = RenderMailBody(strings.Repeat("x", 256*1024+1))
	assert.Error(t, err)
}

func TestEmailSendingSwitchAndLimits(t *testing.T) {
	common.OptionMapRWMutex.Lock()
	previousOptions := common.OptionMap
	common.OptionMap = map[string]string{}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previousOptions
		common.OptionMapRWMutex.Unlock()
	})
	mailDatabase(t, "sqlite")
	mailSMTPFixture(t, "accepted")
	original := operation_setting.GetMailSetting()
	values, err := original.ConfigToMap()
	require.NoError(t, err)
	t.Cleanup(func() {
		options := map[string]string{}
		for key, value := range values {
			options["mail."+key] = value
		}
		require.NoError(t, model.ApplyOptionValues(options))
	})
	// Update the registered setting through the public option application path.
	require.NoError(t, model.ApplyOptionValues(map[string]string{"mail.sending_enabled": "false"}))
	_, err = PrepareEmailBatch(1, "operator", EmailPreviewRequest{Subject: "News", Body: "Hello", Selection: model.EmailRecipientFilter{IDs: []int{2}}})
	require.Error(t, err)
	require.NoError(t, model.ApplyOptionValues(map[string]string{"mail.sending_enabled": "true", "mail.batch_limit": "1"}))
	_, err = PrepareEmailBatch(1, "operator", EmailPreviewRequest{Subject: "News", Body: "Hello", Selection: model.EmailRecipientFilter{All: true}})
	require.Error(t, err)
	require.NoError(t, model.ApplyOptionValues(map[string]string{"mail.sending_enabled": strconv.FormatBool(original.SendingEnabled), "mail.batch_limit": strconv.Itoa(original.BatchLimit)}))
}

func TestEmailDispatcherKeepsSeparateBatches(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			db := mailDatabase(t, dialect)
			messages := mailSMTPFixture(t, "accepted")
			assert.True(t, emailDispatchHandler{}.Enabled(), "first run must perform maintenance")
			for _, subject := range []string{"First batch", "Second batch"} {
				preview, err := PrepareEmailBatch(1, "operator", EmailPreviewRequest{Subject: subject, Body: subject, Selection: model.EmailRecipientFilter{IDs: []int{2}}})
				require.NoError(t, err)
				require.NoError(t, SubmitEmailBatch(preview.Batch.ID, 1))
			}
			task, err := model.GetActiveSystemTask("email_dispatch")
			require.NoError(t, err)
			require.NotNil(t, task)
			claimed, ok, err := model.ClaimSystemTask(task.ID, task.Type, "mail-worker", common.GetTimestamp()+60)
			require.NoError(t, err)
			require.True(t, ok)
			emailDispatchHandler{}.Run(context.Background(), claimed, "mail-worker")
			completed, err := model.GetSystemTaskByTaskID(task.TaskID)
			require.NoError(t, err)
			assert.Equal(t, model.SystemTaskStatusSucceeded, completed.Status)
			var accepted int64
			require.NoError(t, db.Model(&model.EmailDelivery{}).Where("status = ?", model.EmailAccepted).Count(&accepted).Error)
			assert.EqualValues(t, 2, accepted)
			assert.Contains(t, <-messages, "First batch")
			assert.Contains(t, <-messages, "Second batch")
			assert.False(t, emailDispatchHandler{}.Enabled(), "idle dispatchers must not create a task every poll")
		})
	}
}

func TestEmailTransportCancellationBeforeData(t *testing.T) {
	mailSMTPFixture(t, "accepted")
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	_, port, err := net.SplitHostPort(listener.Addr().String())
	require.NoError(t, err)
	common.SMTPPort, err = strconv.Atoi(port)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan common.EmailSendResult, 1)
	go func() {
		result, _ := common.SendEmailWithResult(ctx, "Cancelled", "receiver@example.com", "Body")
		finished <- result
	}()
	conn, err := listener.Accept()
	require.NoError(t, err)
	defer conn.Close()
	cancel()
	select {
	case result := <-finished:
		assert.Equal(t, model.EmailFailed, result.Status)
		assert.Equal(t, "connect", result.Stage)
	case <-time.After(5 * time.Second):
		t.Fatal("cancellation did not close the SMTP connection")
	}
}
