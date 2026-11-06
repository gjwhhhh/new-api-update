package common

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"slices"
	"strings"
	"time"
)

func generateMessageID() (string, error) {
	from := SMTPFrom
	if from == "" {
		from = SMTPAccount
	}
	_, domain, ok := strings.Cut(from, "@")
	if !ok || domain == "" {
		return "", fmt.Errorf("invalid SMTP account")
	}
	return fmt.Sprintf("<%d.%s@%s>", time.Now().UnixNano(), GetRandomString(12), domain), nil
}

func shouldUseSMTPLoginAuth() bool {
	if SMTPForceAuthLogin {
		return true
	}
	return isOutlookServer(SMTPAccount) || slices.Contains(EmailLoginAuthServerList, SMTPServer)
}

func getSMTPAuth() smtp.Auth {
	return AutoSMTPAuth(SMTPAccount, SMTPToken)
}

func shouldAuthenticateSMTP() bool {
	return SMTPAccount != "" && SMTPToken != ""
}

func smtpTLSConfig() *tls.Config {
	return &tls.Config{
		ServerName:         SMTPServer,
		InsecureSkipVerify: SMTPInsecureSkipVerify, // #nosec G402 -- admin-controlled SMTP compatibility option.
	}
}

// EmailSendResult distinguishes a confirmed SMTP rejection from an ambiguous
// connection loss after DATA. Error text deliberately excludes server responses.
type EmailSendResult struct {
	MessageID string
	Status    string
	Stage     string
	SMTPCode  int
	Retryable bool
}

func newSMTPClient(addr string) (*smtp.Client, error) {
	client, _, err := newSMTPClientContext(context.Background(), addr)
	return client, err
}

func newSMTPClientContext(ctx context.Context, addr string) (*smtp.Client, net.Conn, error) {
	conn, err := (&net.Dialer{Timeout: 30 * time.Second}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, nil, err
	}
	deadline := time.Now().Add(30 * time.Second)
	if limit, ok := ctx.Deadline(); ok && limit.Before(deadline) {
		deadline = limit
	}
	if err := conn.SetDeadline(deadline); err != nil {
		conn.Close()
		return nil, nil, err
	}
	rawConn := conn
	stop := context.AfterFunc(ctx, func() { rawConn.Close() })
	defer stop()
	if SMTPSSLEnabled || (SMTPPort == 465 && !SMTPStartTLSEnabled) {
		encrypted := tls.Client(conn, smtpTLSConfig())
		if err := encrypted.HandshakeContext(ctx); err != nil {
			conn.Close()
			return nil, nil, err
		}
		conn = encrypted
	}
	client, err := smtp.NewClient(conn, SMTPServer)
	if err != nil {
		conn.Close()
		return nil, nil, err
	}
	if SMTPStartTLSEnabled && !SMTPSSLEnabled {
		supported, _ := client.Extension("STARTTLS")
		if !supported {
			client.Close()
			return nil, nil, fmt.Errorf("SMTP server does not support STARTTLS")
		}
		if err := client.StartTLS(smtpTLSConfig()); err != nil {
			client.Close()
			return nil, nil, err
		}
	}
	return client, conn, nil
}

func SendEmail(subject string, receiver string, content string) error {
	_, err := SendEmailWithResult(context.Background(), subject, receiver, content)
	return err
}

// SendEmailWithResult performs one bounded SMTP transaction. A successful DATA
// response is definitive even when QUIT subsequently fails (RFC 5321 4.1.1.4).
func SendEmailWithResult(ctx context.Context, subject, receiver, content string) (result EmailSendResult, err error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	result.Status, result.Stage = "failed", "validation"
	defer func() {
		if err == nil {
			return
		}
		var smtpError *textproto.Error
		if errors.As(err, &smtpError) {
			result.SMTPCode = smtpError.Code
			result.Retryable = smtpError.Code >= 400 && smtpError.Code < 500
		} else if result.Status != "unknown" {
			var networkError net.Error
			result.Retryable = errors.As(err, &networkError)
		}
	}()
	from := SMTPFrom
	if from == "" {
		from = SMTPAccount
	}
	if strings.ContainsAny(subject+receiver+from+SystemName, "\r\n\x00") {
		return result, fmt.Errorf("invalid email header")
	}
	to := strings.Split(receiver, ";")
	for _, address := range append(append([]string{}, to...), from) {
		parsed, parseErr := mail.ParseAddress(address)
		if parseErr != nil || parsed.Address != address {
			return result, fmt.Errorf("invalid email address")
		}
	}
	if SMTPServer == "" {
		return result, fmt.Errorf("SMTP server is not configured")
	}
	result.MessageID, err = generateMessageID()
	if err != nil {
		return result, err
	}
	encodedSubject := strings.ReplaceAll(mime.BEncoding.Encode("UTF-8", subject), "?= =?", "?=\r\n =?")
	message := []byte(fmt.Sprintf("To: %s\r\nFrom: %s <%s>\r\nSubject: %s\r\nDate: %s\r\nMessage-ID: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=UTF-8\r\nContent-Transfer-Encoding: base64\r\n\r\n%s\r\n",
		strings.Join(to, ", "), SystemName, from, encodedSubject, time.Now().Format(time.RFC1123Z), result.MessageID, wrapEmailBase64(content)))
	result.Stage = "connect"
	client, conn, err := newSMTPClientContext(ctx, net.JoinHostPort(SMTPServer, fmt.Sprint(SMTPPort)))
	if err != nil {
		return result, err
	}
	defer client.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	result.Stage = "auth"
	if shouldAuthenticateSMTP() {
		if err = client.Auth(getSMTPAuth()); err != nil {
			return result, err
		}
	}
	result.Stage = "sender"
	if err = client.Mail(from); err != nil {
		return result, err
	}
	result.Stage = "recipient"
	for _, address := range to {
		if err = client.Rcpt(address); err != nil {
			return result, err
		}
	}
	result.Stage = "data"
	writer, err := client.Data()
	if err != nil {
		return result, err
	}
	if _, err = writer.Write(message); err != nil {
		result.Status = "unknown"
		return result, err
	}
	result.Stage = "accept"
	if err = writer.Close(); err != nil {
		var rejection *textproto.Error
		if !errors.As(err, &rejection) {
			result.Status = "unknown"
		}
		return result, err
	}
	result.Status = "accepted"
	// Acceptance must not be undone by a failed connection shutdown.
	_ = client.Quit()
	return result, nil
}

func wrapEmailBase64(content string) string {
	encoded := base64.StdEncoding.EncodeToString([]byte(content))
	var body strings.Builder
	for len(encoded) > 76 {
		body.WriteString(encoded[:76])
		body.WriteString("\r\n")
		encoded = encoded[76:]
	}
	body.WriteString(encoded)
	return body.String()
}
