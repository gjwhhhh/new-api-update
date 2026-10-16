/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
package service

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type webhookRoundTripper func(*http.Request) (*http.Response, error)

func (roundTrip webhookRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

type capturedWebhookRequest struct {
	URL     *url.URL
	Headers http.Header
	Body    []byte
}

func captureWebhookRequest(t *testing.T, responseStatus int, responseBody string) *capturedWebhookRequest {
	t.Helper()
	previousClient := httpClient
	previousWorkerURL := system_setting.WorkerUrl
	previousWorkerKey := system_setting.WorkerValidKey
	previousSSRFProtection := system_setting.GetFetchSetting().EnableSSRFProtection
	captured := &capturedWebhookRequest{}
	httpClient = &http.Client{Transport: webhookRoundTripper(func(request *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		captured.URL = request.URL
		captured.Headers = request.Header.Clone()
		captured.Body = body
		return &http.Response{
			StatusCode: responseStatus,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(responseBody)),
			Request:    request,
		}, nil
	})}
	system_setting.WorkerUrl = ""
	system_setting.WorkerValidKey = ""
	system_setting.GetFetchSetting().EnableSSRFProtection = false
	t.Cleanup(func() {
		httpClient = previousClient
		system_setting.WorkerUrl = previousWorkerURL
		system_setting.WorkerValidKey = previousWorkerKey
		system_setting.GetFetchSetting().EnableSSRFProtection = previousSSRFProtection
	})
	return captured
}

func TestSendWebhookNotifyBuildsSignedDingTalkRequest(t *testing.T) {
	captured := captureWebhookRequest(t, http.StatusOK, `{"errcode":0,"errmsg":"ok"}`)
	notification := dto.NewNotify(
		dto.NotifyTypeChannelUpdate,
		"渠道状态变化",
		"原因：{{value}}<br/>请检查",
		[]any{"连接超时"},
	)

	require.NoError(t, SendWebhookNotify(
		"https://oapi.dingtalk.com/robot/send?access_token=fixture-token",
		"fixture-secret",
		notification,
	))

	require.NotNil(t, captured.URL)
	assert.Equal(t, "fixture-token", captured.URL.Query().Get("access_token"))
	timestamp := captured.URL.Query().Get("timestamp")
	require.NotEmpty(t, timestamp)
	signature := captured.URL.Query().Get("sign")
	require.NotEmpty(t, signature)
	hasher := hmac.New(sha256.New, []byte("fixture-secret"))
	_, err := hasher.Write([]byte(timestamp + "\nfixture-secret"))
	require.NoError(t, err)
	assert.Equal(t, base64.StdEncoding.EncodeToString(hasher.Sum(nil)), signature)
	assert.Empty(t, captured.Headers.Get("X-Webhook-Signature"))
	assert.Empty(t, captured.Headers.Get("Authorization"))

	var payload struct {
		MessageType string `json:"msgtype"`
		Text        struct {
			Content string `json:"content"`
		} `json:"text"`
	}
	require.NoError(t, common.Unmarshal(captured.Body, &payload))
	assert.Equal(t, "text", payload.MessageType)
	assert.Equal(t, "渠道状态变化\n原因：连接超时\n请检查", payload.Text.Content)
}

func TestSendWebhookNotifyRejectsDingTalkBusinessError(t *testing.T) {
	captureWebhookRequest(t, http.StatusOK, `{"errcode":310000,"errmsg":"keywords not in content"}`)

	err := SendWebhookNotify(
		"https://oapi.dingtalk.com/robot/send?access_token=fixture-token",
		"",
		dto.NewNotify(dto.NotifyTypeChannelUpdate, "渠道状态变化", "已禁用", nil),
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "errcode=310000")
	assert.NotContains(t, err.Error(), "fixture-token")
}

func TestSendWebhookNotifyRejectsInvalidDingTalkResponses(t *testing.T) {
	testCases := []struct {
		name            string
		status          int
		body            string
		expectedMessage string
	}{
		{name: "non-success status", status: http.StatusBadGateway, body: "", expectedMessage: "status code: 502"},
		{name: "invalid json", status: http.StatusOK, body: "not-json", expectedMessage: "failed to decode"},
		{name: "missing error code", status: http.StatusOK, body: `{}`, expectedMessage: "missing errcode"},
		{name: "oversized body", status: http.StatusOK, body: strings.Repeat("x", maxWebhookResponseBytes+1), expectedMessage: "exceeds"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			captureWebhookRequest(t, testCase.status, testCase.body)
			err := SendWebhookNotify(
				"https://oapi.dingtalk.com/robot/send?access_token=fixture-token",
				"",
				dto.NewNotify(dto.NotifyTypeChannelUpdate, "渠道状态变化", "已禁用", nil),
			)
			require.Error(t, err)
			assert.Contains(t, err.Error(), testCase.expectedMessage)
			assert.NotContains(t, err.Error(), "fixture-token")
		})
	}
}

func TestSendWebhookNotifyKeepsGenericWebhookContract(t *testing.T) {
	captured := captureWebhookRequest(t, http.StatusNoContent, "")
	notification := dto.NewNotify(
		dto.NotifyTypeQuotaExceed,
		"额度预警",
		"剩余额度不足",
		nil,
	)

	require.NoError(t, SendWebhookNotify(
		"https://hooks.example.com/notify?tenant=fixture",
		"generic-secret",
		notification,
	))

	require.NotNil(t, captured.URL)
	assert.Equal(t, "fixture", captured.URL.Query().Get("tenant"))
	assert.Empty(t, captured.URL.Query().Get("timestamp"))
	assert.Empty(t, captured.URL.Query().Get("sign"))
	var payload WebhookPayload
	require.NoError(t, common.Unmarshal(captured.Body, &payload))
	assert.Equal(t, notification.Type, payload.Type)
	assert.Equal(t, notification.Title, payload.Title)
	assert.Equal(t, notification.Content, payload.Content)
	assert.Equal(t, notification.Values, payload.Values)
	hasher := hmac.New(sha256.New, []byte("generic-secret"))
	_, err := hasher.Write(captured.Body)
	require.NoError(t, err)
	assert.Equal(t, hex.EncodeToString(hasher.Sum(nil)), captured.Headers.Get("X-Webhook-Signature"))
}

func TestSendWebhookNotifyDoesNotTreatDingTalkLookalikeAsDingTalk(t *testing.T) {
	captured := captureWebhookRequest(t, http.StatusNoContent, "")

	require.NoError(t, SendWebhookNotify(
		"https://oapi.dingtalk.com.example.com/robot/send",
		"",
		dto.NewNotify(dto.NotifyTypeChannelUpdate, "渠道状态变化", "已启用", nil),
	))

	var payload WebhookPayload
	require.NoError(t, common.Unmarshal(captured.Body, &payload))
	assert.Equal(t, "渠道状态变化", payload.Title)
}

func TestSendWebhookNotifyUsesDingTalkRequestThroughWorker(t *testing.T) {
	captured := captureWebhookRequest(t, http.StatusOK, `{"errcode":0,"errmsg":"ok"}`)
	system_setting.WorkerUrl = "https://worker.example.com"
	system_setting.WorkerValidKey = "worker-key"

	require.NoError(t, SendWebhookNotify(
		"https://oapi.dingtalk.com/robot/send?access_token=fixture-token",
		"fixture-secret",
		dto.NewNotify(dto.NotifyTypeChannelUpdate, "渠道状态变化", "已启用", nil),
	))

	require.NotNil(t, captured.URL)
	assert.Equal(t, "worker.example.com", captured.URL.Hostname())
	var workerRequest WorkerRequest
	require.NoError(t, common.Unmarshal(captured.Body, &workerRequest))
	workerTarget, err := url.Parse(workerRequest.URL)
	require.NoError(t, err)
	assert.Equal(t, "fixture-token", workerTarget.Query().Get("access_token"))
	assert.NotEmpty(t, workerTarget.Query().Get("timestamp"))
	assert.NotEmpty(t, workerTarget.Query().Get("sign"))
	assert.Empty(t, workerRequest.Headers["X-Webhook-Signature"])
	assert.Empty(t, workerRequest.Headers["Authorization"])
	assert.True(t, bytes.Contains(workerRequest.Body, []byte(`"msgtype":"text"`)))
}
