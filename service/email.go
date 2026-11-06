package service

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/mail"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	nethtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Safe email formatting deliberately supports paragraphs, lists, emphasis and
// HTTP(S) links only. No raw HTML or external resource loading is enabled.
var mailInlinePattern = regexp.MustCompile(`\*\*([^*\n]+)\*\*|\[([^\]\n]+)\]\((https?://[^)\s]+)\)`)

func RenderMailBody(body string) (string, error) {
	if strings.TrimSpace(body) == "" || len(body) > 256*1024 || !utf8.ValidString(body) {
		return "", errors.New("Email body is required and must not exceed 256 KiB")
	}
	var rendered strings.Builder
	inList := false
	for line := range strings.SplitSeq(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		list := strings.HasPrefix(line, "- ")
		if inList && !list {
			rendered.WriteString("</ul>")
			inList = false
		}
		if list && !inList {
			rendered.WriteString("<ul>")
			inList = true
		}
		if list {
			line = strings.TrimPrefix(line, "- ")
		}
		if list {
			rendered.WriteString("<li>")
		} else {
			rendered.WriteString("<p>")
		}
		last := 0
		for _, match := range mailInlinePattern.FindAllStringSubmatchIndex(line, -1) {
			rendered.WriteString(html.EscapeString(line[last:match[0]]))
			if match[2] >= 0 {
				rendered.WriteString("<strong>" + html.EscapeString(line[match[2]:match[3]]) + "</strong>")
			} else {
				label, address := line[match[4]:match[5]], line[match[6]:match[7]]
				parsed, err := url.Parse(address)
				if err == nil && parsed.Host != "" && parsed.User == nil {
					rendered.WriteString(`<a href="` + html.EscapeString(address) + `" rel="noopener noreferrer">` + html.EscapeString(label) + `</a>`)
				} else {
					rendered.WriteString(html.EscapeString(line[match[0]:match[1]]))
				}
			}
			last = match[1]
		}
		rendered.WriteString(html.EscapeString(line[last:]))
		if list {
			rendered.WriteString("</li>")
		} else {
			rendered.WriteString("</p>")
		}
	}
	if inList {
		rendered.WriteString("</ul>")
	}
	if rendered.Len() > 256*1024 {
		return "", errors.New("Rendered email body must not exceed 256 KiB")
	}
	return rendered.String(), nil
}

// Sanitized history has no scripts, tracking images, forms or remote styles.
// Authentication callers must redact credentials BEFORE invoking this function.
func SanitizeMailHTML(content string) string {
	tokenizer := nethtml.NewTokenizer(strings.NewReader(content))
	var safe strings.Builder
	for {
		tokenType := tokenizer.Next()
		if tokenType == nethtml.ErrorToken {
			break
		}
		token := tokenizer.Token()
		if tokenType == nethtml.TextToken {
			safe.WriteString(html.EscapeString(token.Data))
			continue
		}
		switch token.DataAtom {
		case atom.P, atom.Br, atom.Strong, atom.B, atom.Em, atom.I, atom.Ul, atom.Ol, atom.Li, atom.A:
		default:
			continue
		}
		if tokenType == nethtml.EndTagToken {
			if token.DataAtom != atom.Br {
				safe.WriteString("</" + token.Data + ">")
			}
			continue
		}
		if tokenType != nethtml.StartTagToken && tokenType != nethtml.SelfClosingTagToken {
			continue
		}
		safe.WriteString("<" + token.Data)
		if token.DataAtom == atom.A {
			for _, attribute := range token.Attr {
				if attribute.Key != "href" {
					continue
				}
				target, err := url.Parse(attribute.Val)
				if err == nil && (target.Scheme == "https" || target.Scheme == "http") && target.Host != "" && target.User == nil {
					safe.WriteString(` href="` + html.EscapeString(attribute.Val) + `" rel="noopener noreferrer"`)
				}
			}
		}
		safe.WriteString(">")
	}
	return safe.String()
}

func ValidateMailSubject(subject string) error {
	if strings.TrimSpace(subject) == "" || utf8.RuneCountInString(subject) > 200 || strings.ContainsFunc(subject, unicode.IsControl) {
		return errors.New("Email subject is required and must not exceed 200 characters or contain control characters")
	}
	return nil
}

func validMailAddress(address string) bool {
	if len(address) > 320 || strings.ContainsAny(address, "\r\n\x00;") {
		return false
	}
	parsed, err := mail.ParseAddress(address)
	return err == nil && parsed.Address == address
}

type EmailPreviewRequest struct {
	Subject   string                     `json:"subject"`
	Body      string                     `json:"body"`
	Selection model.EmailRecipientFilter `json:"selection"`
}
type EmailPreview struct {
	Batch      *model.EmailBatch     `json:"batch"`
	Message    *model.EmailMessage   `json:"message"`
	Recipients []model.EmailDelivery `json:"recipients"`
}

func PrepareEmailBatch(actorID int, actorName string, request EmailPreviewRequest) (*EmailPreview, error) {
	settings := operation_setting.GetMailSetting()
	if !settings.SendingEnabled {
		return nil, errors.New("Email sending is disabled")
	}
	if common.SMTPServer == "" {
		return nil, errors.New("SMTP server is not configured")
	}
	if err := ValidateMailSubject(request.Subject); err != nil {
		return nil, err
	}
	body, err := RenderMailBody(request.Body)
	if err != nil {
		return nil, err
	}
	if len(request.Selection.IDs) > settings.BatchLimit {
		return nil, errors.New("Email recipient limit exceeded")
	}
	// Explicit selections survive paging/filter changes; filters only define the
	// recipient set when selecting all matching users.
	selection := request.Selection
	if !selection.All {
		selection.Keyword = ""
		selection.Group = ""
		selection.Status = 0
	}
	// Explicit IDs may include disabled users chosen by the administrator.
	var recipients []model.EmailRecipient
	var total int64
	if !selection.All {
		active, count, queryErr := model.FindEmailRecipients(selection, 0, settings.BatchLimit+1)
		if queryErr != nil {
			return nil, queryErr
		}
		selection.Status = common.UserStatusDisabled
		disabled, disabledCount, queryErr := model.FindEmailRecipients(selection, 0, settings.BatchLimit+1)
		if queryErr != nil {
			return nil, queryErr
		}
		recipients = append(active, disabled...)
		total = count + disabledCount
	} else {
		recipients, total, err = model.FindEmailRecipients(selection, 0, settings.BatchLimit+1)
		if err != nil {
			return nil, err
		}
	}
	if total > int64(settings.BatchLimit) {
		return nil, errors.New("Email recipient limit exceeded")
	}
	now := common.GetTimestamp()
	batch := &model.EmailBatch{ActorID: actorID, ActorName: actorName, Status: model.EmailPrepared, Matched: int(total), ExpiresAt: now + 900, Exclusions: []model.EmailExclusion{}}
	snapshot, err := common.Marshal(request.Selection)
	if err != nil {
		return nil, err
	}
	batch.Selection = string(snapshot)
	if !request.Selection.All {
		available := make(map[int]bool, len(recipients))
		for _, recipient := range recipients {
			available[recipient.ID] = true
		}
		for _, id := range request.Selection.IDs {
			if !available[id] {
				batch.Exclusions = append(batch.Exclusions, model.EmailExclusion{UserID: id, Reason: "User no longer available"})
				available[id] = true
			}
		}
	}
	deliveries := []model.EmailDelivery{}
	byAddress := map[string]int{}
	for _, recipient := range recipients {
		address := model.NormalizeEmail(recipient.Email)
		if !validMailAddress(address) {
			batch.Exclusions = append(batch.Exclusions, model.EmailExclusion{UserID: recipient.ID, Email: recipient.Email, Reason: "No valid email address"})
			continue
		}
		if index, exists := byAddress[address]; exists {
			deliveries[index].UserIDs = append(deliveries[index].UserIDs, recipient.ID)
			deliveries[index].UserRefs = model.EmailUserRefs(deliveries[index].UserIDs)
			batch.Duplicates++
			continue
		}
		byAddress[address] = len(deliveries)
		ids := []int{recipient.ID}
		deliveries = append(deliveries, model.EmailDelivery{UserID: recipient.ID, UserIDs: ids, UserRefs: model.EmailUserRefs(ids), Username: recipient.Username, Email: address, Status: model.EmailPrepared})
	}
	if len(deliveries) == 0 {
		return nil, errors.New("No users have a valid email address")
	}
	batch.Total = len(deliveries)
	message := &model.EmailMessage{Subject: strings.TrimSpace(request.Subject), Body: body, Kind: "manual", Source: "admin"}
	if err := model.CreateEmailBatch(message, batch, deliveries); err != nil {
		return nil, err
	}
	// Only a bounded sample is returned. The full snapshot is persisted and can
	// never be widened by changing the confirmation request.
	return &EmailPreview{Batch: batch, Message: message, Recipients: deliveries[:min(len(deliveries), 20)]}, nil
}

// SendSystemEmail retains only the caller-supplied safe body. A nil safeBody
// means the content is ordinary notification text; security callers supply a
// separate fully redacted representation, never an after-the-fact log scrubber.
func SendSystemEmail(kind string, userID int, receiver, subject, content string, safeBody *string) error {
	var sendError error
	for address := range strings.SplitSeq(receiver, ";") {
		retained := content
		if safeBody != nil {
			retained = *safeBody
		}
		message := &model.EmailMessage{Subject: subject, Body: SanitizeMailHTML(retained), Kind: kind, Source: "system", Redacted: safeBody != nil}
		now := common.GetTimestamp()
		delivery := &model.EmailDelivery{UserID: userID, Email: address, Status: model.EmailSending, Attempts: 1, RetryRound: 1, LockedBy: "immediate", LockedUntil: now + 120}
		if userID > 0 {
			delivery.UserIDs = []int{userID}
			delivery.UserRefs = model.EmailUserRefs(delivery.UserIDs)
		}
		recorded := false
		var attempt *model.EmailAttempt
		if operation_setting.GetMailSetting().RecordsEnabled && model.DB != nil {
			var users []model.User
			query := model.DB.Select("id", "username")
			if userID > 0 {
				query = query.Where("id = ?", userID)
			} else {
				query = query.Where("LOWER(email) = ?", model.NormalizeEmail(address))
			}
			if err := query.Order("id asc").Find(&users).Error; err == nil && len(users) > 0 {
				delivery.UserID, delivery.Username = users[0].Id, users[0].Username
				delivery.UserIDs = make([]int, len(users))
				for i, user := range users {
					delivery.UserIDs[i] = user.Id
				}
				delivery.UserRefs = model.EmailUserRefs(delivery.UserIDs)
			}
			if err := model.CreateAutomaticEmail(message, delivery); err == nil {
				attempt, err = model.StartEmailAttempt(delivery.ID, delivery.LockedBy, now)
				recorded = err == nil
				if err != nil {
					common.SysError("email history attempt write failed")
				}
			} else {
				common.SysError("email history write failed; continuing immediate delivery")
			}
		}
		result, err := common.SendEmailWithResult(context.Background(), subject, address, content)
		if recorded {
			completeEmailAttempt(attempt, result)
			if updateErr := model.FinishEmailDelivery(delivery, attempt, result.Status, 0); updateErr != nil {
				common.SysError(fmt.Sprintf("email history result write failed: delivery_id=%d", delivery.ID))
			}
		}
		if err != nil {
			sendError = errors.Join(sendError, fmt.Errorf("SMTP %s failed (code %d)", result.Stage, result.SMTPCode))
		}
	}
	return sendError
}

func completeEmailAttempt(attempt *model.EmailAttempt, result common.EmailSendResult) {
	attempt.Status = result.Status
	attempt.Stage = result.Stage
	attempt.SMTPCode = result.SMTPCode
	attempt.MessageID = result.MessageID
	attempt.FinishedAt = common.GetTimestamp()
	if result.Status != model.EmailAccepted {
		attempt.Error = fmt.Sprintf("SMTP %s failed (code %d)", result.Stage, result.SMTPCode)
	}
}

func SubmitEmailBatch(id int64, actorID int) error {
	if !operation_setting.GetMailSetting().SendingEnabled {
		return errors.New("Email sending is disabled")
	}
	if err := model.ConfirmEmailBatch(id, actorID, common.GetTimestamp()); err != nil {
		return err
	}
	// Persistence is the source of truth. If wakeup fails, the scheduled handler
	// discovers this batch on its next pass, so confirmation must remain successful.
	if _, _, err := EnqueueSystemTask("email_dispatch", nil); err != nil {
		common.SysError("email dispatch wakeup failed")
	}
	return nil
}

func SendTestEmail(ctx context.Context, actorID int, batchID int64, userID int) error {
	if !operation_setting.GetMailSetting().SendingEnabled {
		return errors.New("Email sending is disabled")
	}
	batch, err := model.GetEmailBatch(batchID)
	if err != nil {
		return err
	}
	if batch.ActorID != actorID || batch.Status != model.EmailPrepared || batch.ExpiresAt <= common.GetTimestamp() {
		return model.ErrEmailConflict
	}
	message, err := model.GetEmailMessage(batch.MessageID)
	if err != nil {
		return err
	}
	var user model.User
	if err := model.DB.Select("id", "username", "email").First(&user, userID).Error; err != nil {
		return err
	}
	address := model.NormalizeEmail(user.Email)
	if !validMailAddress(address) {
		return errors.New("No valid email address")
	}
	// Rate-limit tests using the persisted actor-owned test batch, across nodes.
	now := common.GetTimestamp()
	if err := model.ReserveEmailTest(batch.ID, actorID, now); err != nil {
		return err
	}
	testMessage := &model.EmailMessage{Subject: message.Subject, Body: message.Body, Kind: "test", Source: "admin"}
	delivery := &model.EmailDelivery{UserID: user.Id, UserIDs: []int{user.Id}, UserRefs: model.EmailUserRefs([]int{user.Id}), Username: user.Username, Email: address, Status: model.EmailSending, Attempts: 1, RetryRound: 1, LockedBy: "test", LockedUntil: now + 120}
	if err := model.CreateAutomaticEmail(testMessage, delivery); err != nil {
		return err
	}
	attempt, err := model.StartEmailAttempt(delivery.ID, "test", now)
	if err != nil {
		return err
	}
	result, sendErr := common.SendEmailWithResult(ctx, message.Subject, address, message.Body)
	completeEmailAttempt(attempt, result)
	if err := model.FinishEmailDelivery(delivery, attempt, result.Status, 0); err != nil {
		return err
	}
	if sendErr != nil {
		return errors.New("SMTP test email failed. Check the email record for details.")
	}
	return nil
}

func emailRecipientUnchanged(delivery *model.EmailDelivery) (bool, error) {
	var users []model.User
	if len(delivery.UserIDs) == 0 {
		return false, nil
	}
	if err := model.DB.Select("id", "email").Where("id IN ?", delivery.UserIDs).Find(&users).Error; err != nil {
		return false, err
	}
	return slices.ContainsFunc(users, func(user model.User) bool { return model.NormalizeEmail(user.Email) == delivery.Email }), nil
}
