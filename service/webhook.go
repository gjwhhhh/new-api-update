package service

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"html"
	"io"
	"maps"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

// WebhookPayload webhook 通知的负载数据
type WebhookPayload struct {
	Type      string `json:"type"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	Values    []any  `json:"values,omitempty"`
	Timestamp int64  `json:"timestamp"`
}

type webhookProvider string

const (
	webhookProviderGeneric  webhookProvider = "generic"
	webhookProviderDingTalk webhookProvider = "dingtalk"
	maxWebhookResponseBytes                 = 64 << 10
)

type webhookRequestSpec struct {
	Provider webhookProvider
	URL      string
	Headers  map[string]string
	Body     []byte
}

type dingTalkWebhookPayload struct {
	MessageType string `json:"msgtype"`
	Text        struct {
		Content string `json:"content"`
	} `json:"text"`
}

type dingTalkWebhookResponse struct {
	ErrorCode    *int   `json:"errcode"`
	ErrorMessage string `json:"errmsg"`
}

var webhookHTMLTagPattern = regexp.MustCompile(`<[^>]+>`)

// generateSignature 生成 webhook 签名
func generateSignature(secret string, payload []byte) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write(payload)
	return hex.EncodeToString(h.Sum(nil))
}

func isDingTalkWebhook(parsedURL *url.URL) bool {
	return parsedURL.Scheme == "https" &&
		strings.EqualFold(parsedURL.Hostname(), "oapi.dingtalk.com") &&
		parsedURL.Path == "/robot/send"
}

func renderDingTalkText(data dto.Notify) string {
	content := data.Content
	for _, value := range data.Values {
		content = strings.Replace(content, dto.ContentValueParam, fmt.Sprint(value), 1)
	}
	content = strings.NewReplacer(
		"<br>", "\n",
		"<br/>", "\n",
		"<br />", "\n",
	).Replace(content)
	content = html.UnescapeString(webhookHTMLTagPattern.ReplaceAllString(content, ""))
	title := strings.TrimSpace(data.Title)
	content = strings.TrimSpace(content)
	if title == "" {
		return content
	}
	if content == "" {
		return title
	}
	return title + "\n" + content
}

func buildDingTalkWebhookRequest(parsedURL *url.URL, secret string, data dto.Notify, now time.Time) (*webhookRequestSpec, error) {
	payload := dingTalkWebhookPayload{MessageType: "text"}
	payload.Text.Content = renderDingTalkText(data)
	payloadBytes, err := common.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal DingTalk webhook payload: %v", err)
	}

	if secret != "" {
		timestamp := strconv.FormatInt(now.UnixMilli(), 10)
		hasher := hmac.New(sha256.New, []byte(secret))
		_, _ = hasher.Write([]byte(timestamp + "\n" + secret))
		query := parsedURL.Query()
		query.Set("timestamp", timestamp)
		query.Set("sign", base64.StdEncoding.EncodeToString(hasher.Sum(nil)))
		parsedURL.RawQuery = query.Encode()
	}

	return &webhookRequestSpec{
		Provider: webhookProviderDingTalk,
		URL:      parsedURL.String(),
		Headers:  map[string]string{"Content-Type": "application/json"},
		Body:     payloadBytes,
	}, nil
}

func buildGenericWebhookRequest(webhookURL, secret string, data dto.Notify, now time.Time) (*webhookRequestSpec, error) {
	content := data.Content
	for _, value := range data.Values {
		content = fmt.Sprintf(content, value)
	}
	payloadBytes, err := common.Marshal(WebhookPayload{
		Type:      data.Type,
		Title:     data.Title,
		Content:   content,
		Values:    data.Values,
		Timestamp: now.Unix(),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal webhook payload: %v", err)
	}

	headers := map[string]string{"Content-Type": "application/json"}
	if secret != "" {
		headers["X-Webhook-Signature"] = generateSignature(secret, payloadBytes)
	}
	return &webhookRequestSpec{
		Provider: webhookProviderGeneric,
		URL:      webhookURL,
		Headers:  headers,
		Body:     payloadBytes,
	}, nil
}

func buildWebhookRequest(webhookURL, secret string, data dto.Notify, now time.Time) (*webhookRequestSpec, error) {
	parsedURL, err := url.ParseRequestURI(webhookURL)
	if err != nil {
		return nil, fmt.Errorf("invalid webhook URL")
	}
	if isDingTalkWebhook(parsedURL) {
		return buildDingTalkWebhookRequest(parsedURL, secret, data, now)
	}
	return buildGenericWebhookRequest(webhookURL, secret, data, now)
}

func validateWebhookResponse(provider webhookProvider, response *http.Response) error {
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("webhook request failed with status code: %d", response.StatusCode)
	}
	if provider != webhookProviderDingTalk {
		return nil
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, maxWebhookResponseBytes+1))
	if err != nil {
		return fmt.Errorf("failed to read DingTalk webhook response: %v", err)
	}
	if len(body) > maxWebhookResponseBytes {
		return fmt.Errorf("DingTalk webhook response exceeds %d bytes", maxWebhookResponseBytes)
	}
	var result dingTalkWebhookResponse
	if err := common.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("failed to decode DingTalk webhook response")
	}
	if result.ErrorCode == nil {
		return fmt.Errorf("DingTalk webhook response is missing errcode")
	}
	if *result.ErrorCode == 0 {
		return nil
	}
	message := []rune(strings.TrimSpace(result.ErrorMessage))
	if len(message) > 256 {
		message = message[:256]
	}
	return fmt.Errorf("DingTalk webhook rejected request: errcode=%d errmsg=%s", *result.ErrorCode, string(message))
}

// SendWebhookNotify 发送 webhook 通知
func SendWebhookNotify(webhookURL string, secret string, data dto.Notify) error {
	requestSpec, err := buildWebhookRequest(webhookURL, secret, data, time.Now())
	if err != nil {
		return err
	}

	var resp *http.Response
	if system_setting.EnableWorker() {
		headers := make(map[string]string, len(requestSpec.Headers)+1)
		maps.Copy(headers, requestSpec.Headers)
		if requestSpec.Provider == webhookProviderGeneric && secret != "" {
			headers["Authorization"] = "Bearer " + secret
		}
		workerReq := &WorkerRequest{
			URL:     requestSpec.URL,
			Key:     system_setting.WorkerValidKey,
			Method:  http.MethodPost,
			Headers: headers,
			Body:    requestSpec.Body,
		}

		resp, err = DoWorkerRequest(workerReq)
		if err != nil {
			return fmt.Errorf("failed to send webhook request through worker: %s", common.MaskSensitiveInfo(err.Error()))
		}
	} else {
		if err := ValidateSSRFProtectedFetchURL(requestSpec.URL); err != nil {
			return fmt.Errorf("request reject: %v", err)
		}
		req, err := http.NewRequest(http.MethodPost, requestSpec.URL, bytes.NewBuffer(requestSpec.Body))
		if err != nil {
			return fmt.Errorf("failed to create webhook request: %v", err)
		}
		for key, value := range requestSpec.Headers {
			req.Header.Set(key, value)
		}
		client := GetSSRFProtectedHTTPClient()
		resp, err = client.Do(req)
		if err != nil {
			return fmt.Errorf("failed to send webhook request: %s", common.MaskSensitiveInfo(err.Error()))
		}
	}
	defer resp.Body.Close()
	return validateWebhookResponse(requestSpec.Provider, resp)
}
