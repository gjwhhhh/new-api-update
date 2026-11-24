package controller

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetChannelDefaultBaseURLsUsesBuiltInDefaults(t *testing.T) {
	originalBaseURLs := constant.ChannelBaseURLs
	constant.ChannelBaseURLs = append([]string(nil), originalBaseURLs...)
	constant.ChannelBaseURLs[constant.ChannelTypeDeepSeek] = "https://deepseek.server.example"
	t.Cleanup(func() {
		constant.ChannelBaseURLs = originalBaseURLs
	})

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/channel/default_base_urls", nil)
	GetChannelDefaultBaseURLs(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool           `json:"success"`
		Data    map[int]string `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	assert.Equal(t, "https://deepseek.server.example", response.Data[constant.ChannelTypeDeepSeek])
	assert.Equal(t, "https://api.openai.com", response.Data[constant.ChannelTypeOpenAI])
	assert.NotContains(t, response.Data, constant.ChannelTypeAzure)
	assert.NotContains(t, response.Data, constant.ChannelTypeNewAPI)
	assert.NotContains(t, response.Data, constant.ChannelTypeTaskPlugin)
}

func TestChannelProbeScheduledAndTrialSendSameRequest(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.ChannelTestResult{}, &model.Log{}))
	oldLogging, oldDisable := common.LogConsumeEnabled, common.AutomaticDisableChannelEnabled
	historySetting := config.GlobalConfig.Get("channel_test_history_setting").(*operation_setting.ChannelTestHistorySetting)
	oldHistory, err := historySetting.ConfigToMap()
	require.NoError(t, err)
	common.LogConsumeEnabled, common.AutomaticDisableChannelEnabled = true, true
	require.NoError(t, historySetting.UpdateConfigFromMap(map[string]string{"enabled": "true"}))
	t.Cleanup(func() {
		common.LogConsumeEnabled, common.AutomaticDisableChannelEnabled = oldLogging, oldDisable
		require.NoError(t, historySetting.UpdateConfigFromMap(oldHistory))
	})
	config, probe, estimate, err := generateChannelProbe("gpt-4o-mini", 0)
	require.NoError(t, err)
	assert.InDelta(t, 24000, estimate, 240)
	config.CacheAffinity = probeAffinityGrok
	rawConfig, err := common.Marshal(config)
	require.NoError(t, err)
	type requestSample struct{ body, conversationID string }
	requests := make(chan requestSample, 8)
	waiting, canceled := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, probeChatPath, r.URL.Path)
		assert.Equal(t, "Bearer local-test-key", r.Header.Get("Authorization"))
		body, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		requests <- requestSample{string(body), r.Header.Get(probeConversationHeader)}
		answer, details := probe.answer, `"prompt_tokens_details":{"cached_tokens":0},`
		switch calls.Add(1) {
		case 2:
			details = `"prompt_tokens_details":{"cached_tokens":24576},`
			answer = "wrong-answer"
		case 3:
			details = ""
		case 4:
			close(waiting)
			select {
			case <-r.Context().Done():
				close(canceled)
			case <-time.After(5 * time.Second):
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"id":"test","object":"chat.completion","model":"grok-test","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":%q}}],"usage":{"prompt_tokens":24606,%s"completion_tokens":3,"total_tokens":24609}}`, "  "+answer+"  ", details)
	}))
	t.Cleanup(upstream.Close)
	user := &model.User{Username: "probe-user", Role: common.RoleRootUser, Status: common.UserStatusEnabled, Group: "default"}
	user.SetSetting(dto.UserSetting{AcceptUnsetRatioModel: true})
	require.NoError(t, db.Create(user).Error)
	channel := &model.Channel{
		Type: constant.ChannelTypeXai, Status: common.ChannelStatusEnabled, Key: "disabled-key\nlocal-test-key\nrotation-key",
		Name: "Probe", Models: "gpt-4o-mini", Group: "default", BaseURL: &upstream.URL,
		ModelMapping: common.GetPointer(`{"gpt-4o-mini":"grok-test"}`), ResponseTime: 73, TestTime: 123,
		ChannelInfo: model.ChannelInfo{IsMultiKey: true, MultiKeySize: 3, MultiKeyMode: constant.MultiKeyModePolling, MultiKeyPollingIndex: 2,
			MultiKeyStatusList: map[int]int{0: common.ChannelStatusManuallyDisabled}},
	}
	require.NoError(t, db.Create(channel).Error)
	require.NoError(t, model.SaveChannelProbe(channel.Id, channel.Type, rawConfig))
	ctx := context.Background()
	first := runLongChannelProbe(ctx, channel, user.Id, config, channelTestHistoryContext{RequestID: "trial", Source: "manual_single"})
	require.Equal(t, service.ChannelTestStatusSucceeded, first.Status, "%+v", first)
	require.NotNil(t, first.ProbeCachedTokens)
	assert.Zero(t, *first.ProbeCachedTokens)
	target := automaticChannelTestTarget{Channel: channel, AllowDisable: true, RecordAutomaticTestTime: true, HealthCheckMode: "scheduled"}
	summary := testChannelForHealthCheck(ctx, target, user.Id, channelTestHistoryContext{Source: "scheduled"}, -1)
	assert.Equal(t, channelTestSummary{Tested: 1, Failed: 1}, summary)
	var saved model.ChannelTestResult
	require.NoError(t, db.Where("source = ?", "scheduled").First(&saved).Error)
	assert.Equal(t, service.ChannelTestFailureKindResponseInvalid, saved.FailureKind)
	assert.Equal(t, service.ChannelTestActionNone, saved.StateAction)
	require.NotNil(t, saved.ProbeInputTokens)
	require.NotNil(t, saved.ProbeCachedTokens)
	assert.Equal(t, 24606, *saved.ProbeInputTokens)
	assert.Equal(t, 24576, *saved.ProbeCachedTokens)
	stored, err := model.GetChannelById(channel.Id, true)
	require.NoError(t, err)
	assert.Equal(t, 73, stored.ResponseTime)
	assert.EqualValues(t, 123, stored.TestTime)
	assert.Positive(t, stored.LastAutoTestTime)
	assert.Equal(t, common.ChannelStatusEnabled, stored.Status)
	assert.Equal(t, 2, stored.ChannelInfo.MultiKeyPollingIndex)
	require.NotNil(t, saved.KeyIndex)
	assert.Equal(t, 1, *saved.KeyIndex)
	require.Equal(t, int32(2), calls.Load())
	trialRequest, scheduledRequest := <-requests, <-requests
	assert.NotEmpty(t, trialRequest.conversationID)
	assert.Equal(t, trialRequest, scheduledRequest)
	assert.Contains(t, trialRequest.body, `"model":"grok-test"`)
	assert.Contains(t, trialRequest.body, probe.answer)

	third := runLongChannelProbe(ctx, channel, user.Id, config, channelTestHistoryContext{RequestID: "missing-cache", Source: "manual_single"})
	assert.Equal(t, service.ChannelTestStatusSucceeded, third.Status)
	require.NotNil(t, third.ProbeInputTokens)
	assert.Nil(t, third.ProbeCachedTokens)
	var logs int64
	require.NoError(t, db.Model(&model.Log{}).Where("type = ?", model.LogTypeConsume).Count(&logs).Error)
	assert.EqualValues(t, 3, logs, "an invalid answer still produces a successful upstream consumption log")

	channel.ParamOverride = common.GetPointer(`{"stream":true}`)
	blocked := runLongChannelProbe(ctx, channel, user.Id, config, channelTestHistoryContext{RequestID: "override", Source: "manual_single"})
	assert.Equal(t, service.ChannelTestFailureKindSetup, blocked.FailureKind)
	assert.Equal(t, int32(3), calls.Load(), "an unsafe override does not contact upstream")

	channel.ParamOverride = nil
	cancelCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan *model.ChannelTestResult, 1)
	go func() {
		done <- runLongChannelProbe(cancelCtx, channel, user.Id, config, channelTestHistoryContext{RequestID: "canceled", Source: "manual_single"})
	}()
	select {
	case <-waiting:
	case <-time.After(5 * time.Second):
		t.Fatal("probe did not reach upstream")
	}
	cancel()
	select {
	case <-canceled:
	case <-time.After(5 * time.Second):
		t.Fatal("upstream did not receive cancellation")
	}
	result := <-done
	assert.Equal(t, service.ChannelTestStatusCancelled, result.Status)
}

func TestChannelProbeRawUsage(t *testing.T) {
	wire, err := classifyProbeWire([]byte(`{"messages":[],"max_tokens":128}`))
	require.NoError(t, err)
	for _, tc := range []struct {
		name, usage string
		wantInput   *int
		wantCached  *int
	}{
		{"missing", `{}`, nil, nil},
		{"null cache", `{"prompt_tokens":200,"prompt_tokens_details":{"cached_tokens":null}}`, common.GetPointer(200), nil},
		{"invalid range", `{"prompt_tokens":200,"prompt_tokens_details":{"cached_tokens":201}}`, common.GetPointer(200), nil},
		{"zero", `{"prompt_tokens":200,"prompt_tokens_details":{"cached_tokens":0}}`, common.GetPointer(200), common.GetPointer(0)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := &channelProbeResponse{ReadCloser: io.NopCloser(strings.NewReader(`{"usage":` + tc.usage + `}`))}
			_, err := io.ReadAll(response)
			require.NoError(t, err)
			input, cached := response.usage(wire)
			assert.Equal(t, tc.wantInput, input)
			assert.Equal(t, tc.wantCached, cached)
		})
	}
}

func TestChannelProbeWireClassification(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"openai-chat", `{"messages":[],"max_completion_tokens":128}`},
		{"openai-responses", `{"input":[],"model":"mapped-model"}`},
		{"anthropic", `{"messages":[],"system":[],"anthropic_version":"vertex-2023-10-16","max_tokens":128}`},
		{"gemini", `{"contents":[],"generationConfig":{}}`},
		{"", `{"prompt":"example"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wire, err := classifyProbeWire([]byte(tc.body))
			if tc.name == "" {
				require.EqualError(t, err, "unsupported upstream chat protocol")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.name, wire.name)
		})
	}
}

func TestChannelProbeAnthropicValidation(t *testing.T) {
	const base = `{"model":"mapped-model","anthropic_version":"2023-06-01","max_tokens":128,"system":[{"type":"text","text":"reference"}],"messages":[{"role":"user","content":"question"}]`
	probe := &channelProbe{context: "reference", question: "question"}
	wire, err := classifyProbeWire([]byte(base + "}"))
	require.NoError(t, err)
	for _, tc := range []struct {
		name, body string
		wantErr    error
	}{
		{"valid", base + "}", nil},
		{"thinking", base + `,"thinking":{"type":"enabled","budget_tokens":1024}}`, errProbeFields},
		{"output config", base + `,"output_config":{"effort":"high"}}`, errProbeFields},
		{"stream", base + `,"stream":true}`, errProbeInput},
		{"limit", strings.Replace(base, "128", "1024", 1) + "}", errProbeLimit},
		{"model", strings.Replace(base, "mapped-model", "other-model", 1) + "}", errProbeInput},
		{"system text", strings.Replace(base, "reference", "changed", 1) + "}", errProbeInput},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := wire.validate([]byte(tc.body), "mapped-model", probe)
			if tc.wantErr == nil {
				require.NoError(t, err)
			} else {
				assert.ErrorIs(t, err, tc.wantErr)
			}
		})
	}
}

func TestChannelProbeResponsesAndGeminiValidation(t *testing.T) {
	const responses = `{"model":"mapped-model","max_output_tokens":128,"instructions":"reference","input":[{"role":"user","content":"question"}]}`
	const gemini = `{"systemInstruction":{"parts":[{"text":"reference"}]},"contents":[{"role":"user","parts":[{"text":"question"}]}],"generationConfig":{"maxOutputTokens":128}}`
	probe := &channelProbe{context: "reference", question: "question"}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "mapped-model"}}
	for _, tc := range []struct {
		name, original, body, endpoint string
		wantErr                        error
	}{
		{"Responses valid", responses, responses, "/v1/responses", nil},
		{"Responses model override", responses, strings.Replace(responses, "mapped-model", "other-model", 1), "/v1/responses", errProbeInput},
		{"Responses context override", responses, strings.Replace(responses, "reference", "changed", 1), "/v1/responses", errProbeInput},
		{"Responses output override", responses, strings.Replace(responses, "128", "1024", 1), "/v1/responses", errProbeLimit},
		{"Responses streaming override", responses, strings.Replace(responses, `"instructions":`, `"stream":true,"instructions":`, 1), "/v1/responses", errProbeInput},
		{"Responses tools override", responses, strings.Replace(responses, `"instructions":`, `"tools":[],"instructions":`, 1), "/v1/responses", errProbeFields},
		{"Gemini valid", gemini, gemini, "/v1beta/models/mapped-model:generateContent", nil},
		{"Gemini context override", gemini, strings.Replace(gemini, "reference", "changed", 1), "/v1beta/models/mapped-model:generateContent", errProbeInput},
		{"Gemini output override", gemini, strings.Replace(gemini, "128", "1024", 1), "/v1beta/models/mapped-model:generateContent", errProbeLimit},
		{"Gemini thinking override", gemini, strings.Replace(gemini, `"maxOutputTokens":128`, `"maxOutputTokens":128,"thinkingConfig":{"thinkingBudget":1024}`, 1), "/v1beta/models/mapped-model:generateContent", errProbeFields},
		{"Gemini model endpoint", gemini, gemini, "/v1beta/models/other-model:generateContent", errProbeProtocol},
		{"Gemini streaming endpoint", gemini, gemini, "/v1beta/models/mapped-model:streamGenerateContent", errProbeProtocol},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wire, err := classifyProbeWire([]byte(tc.original))
			require.NoError(t, err)
			_, err = probe.validateRequest(wire, info, []byte(tc.original), []byte(tc.body), "https://upstream.example"+tc.endpoint)
			if tc.wantErr == nil {
				require.NoError(t, err)
			} else {
				assert.ErrorIs(t, err, tc.wantErr)
			}
		})
	}
}

func TestChannelProbeClaudeCacheAffinity(t *testing.T) {
	body := []byte(`{"model":"mapped-model","max_tokens":128,"system":[{"type":"text","text":"reference"}],"messages":[{"role":"user","content":"question"}]}`)
	probe := &channelProbe{context: "reference", question: "question", cacheAffinity: probeAffinityClaude}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "mapped-model"}}
	wire, err := classifyProbeWire(body)
	require.NoError(t, err)
	const endpoint = "https://upstream.example/v1/messages"
	cached, err := probe.validateRequest(wire, info, body, body, endpoint)
	require.NoError(t, err)
	assert.Contains(t, string(cached), `"cache_control":{"type":"ephemeral"}`)
	assert.ErrorIs(t, wire.validate(body, info.UpstreamModelName, probe), errProbeCache)

	for _, tc := range []struct {
		name, override string
	}{
		{"replace marker", `{"operations":[{"mode":"set","path":"system.0.cache_control","value":{"type":"other"}}]}`},
		{"delete marker", `{"operations":[{"mode":"delete","path":"system.0.cache_control"}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, common.UnmarshalJsonStr(tc.override, &info.ParamOverride))
			overridden, err := relaycommon.ApplyParamOverrideWithRelayInfo(body, info)
			require.NoError(t, err)
			_, err = probe.validateRequest(wire, info, body, overridden, endpoint)
			assert.ErrorIs(t, err, errProbeCache)
		})
	}
}

func TestChannelProbeProviderUsage(t *testing.T) {
	for _, tc := range []struct {
		name, body        string
		usage             func([]byte) (*int, *int)
		wantInput, cached *int
	}{
		{"Claude missing input", `{"usage":{}}`, anthropicProbeUsage, nil, nil},
		{"Claude missing read", `{"usage":{"input_tokens":200,"cache_creation_input_tokens":50}}`, anthropicProbeUsage, common.GetPointer(250), nil},
		{"Claude zero input", `{"usage":{"input_tokens":0,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}`, anthropicProbeUsage, nil, nil},
		{"Claude zero input with cache read", `{"usage":{"input_tokens":0,"cache_read_input_tokens":250}}`, anthropicProbeUsage, nil, nil},
		{"Claude sum", `{"usage":{"input_tokens":50,"cache_creation_input_tokens":200,"cache_read_input_tokens":250}}`, anthropicProbeUsage, common.GetPointer(500), common.GetPointer(250)},
		{"Claude negative read", `{"usage":{"input_tokens":50,"cache_read_input_tokens":-1}}`, anthropicProbeUsage, nil, nil},
		{"Claude negative creation", `{"usage":{"input_tokens":50,"cache_creation_input_tokens":-1,"cache_read_input_tokens":55}}`, anthropicProbeUsage, nil, nil},
		{"Claude sum overflow", fmt.Sprintf(`{"usage":{"input_tokens":%d,"cache_read_input_tokens":1}}`, math.MaxInt), anthropicProbeUsage, nil, nil},
		{"Claude read exceeds int", fmt.Sprintf(`{"usage":{"input_tokens":1,"cache_read_input_tokens":%d}`, uint64(math.MaxInt)+1) + "}", anthropicProbeUsage, nil, nil},
		{"Responses", `{"usage":{"input_tokens":300,"input_tokens_details":{"cached_tokens":250}}}`, openAIResponsesProbeUsage, common.GetPointer(300), common.GetPointer(250)},
		{"Gemini", `{"usageMetadata":{"promptTokenCount":300,"cachedContentTokenCount":250}}`, geminiProbeUsage, common.GetPointer(300), common.GetPointer(250)},
		{"Gemini cache exceeds input", `{"usageMetadata":{"promptTokenCount":300,"cachedContentTokenCount":301}}`, geminiProbeUsage, common.GetPointer(300), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input, cached := tc.usage([]byte(tc.body))
			assert.Equal(t, tc.wantInput, input)
			assert.Equal(t, tc.cached, cached)
		})
	}
}

func TestChannelProbeSupportsChannelTypes(t *testing.T) {
	// Bedrock addresses the model in the endpoint or bypasses HTTP entirely.
	assert.False(t, supportsChannelProbe(&model.Channel{Type: constant.ChannelTypeAws}))
	assert.False(t, supportsChannelProbe(&model.Channel{Type: constant.ChannelTypeMidjourney}))
	assert.True(t, supportsChannelProbe(&model.Channel{Type: constant.ChannelTypeAnthropic}))
	assert.True(t, supportsChannelProbe(&model.Channel{Type: constant.ChannelTypeOpenAI}))
}

func TestChannelProbeGrokHeaderReplacesAlias(t *testing.T) {
	body := []byte(`{"model":"grok-4","max_tokens":128,"messages":[{"role":"system","content":"reference"},{"role":"user","content":"question"}]}`)
	probe := &channelProbe{context: "reference", question: "question", cacheAffinity: probeAffinityGrok}
	info := &relaycommon.RelayInfo{
		ChannelMeta:               &relaycommon.ChannelMeta{UpstreamModelName: "grok-4"},
		RuntimeHeadersOverride:    map[string]any{"X-Grok-Conv-Id": "operator-value"},
		UseRuntimeHeadersOverride: true,
	}
	wire, err := classifyProbeWire(body)
	require.NoError(t, err)
	_, err = probe.validateRequest(wire, info, body, body, "https://upstream.example/v1/chat/completions")
	require.NoError(t, err)

	var values []string
	for key, value := range info.RuntimeHeadersOverride {
		if strings.EqualFold(strings.TrimSpace(key), probeConversationHeader) {
			values = append(values, value.(string))
		}
	}
	require.Len(t, values, 1)
	assert.NotEqual(t, "operator-value", values[0])
}

func TestChannelProbeErrorsSeparateProtocolFromOverrides(t *testing.T) {
	probe := &channelProbe{context: "reference", question: "question"}
	chat := []byte(`{"model":"mapped-model","max_tokens":128,"messages":[{"role":"system","content":"reference"},{"role":"user","content":"question"}]}`)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "mapped-model"}}
	wire, err := classifyProbeWire(chat)
	require.NoError(t, err)

	// A Vertex-shaped body on a non-Vertex endpoint cannot be probed as Claude.
	vertex := []byte(`{"anthropic_version":"vertex-2023-10-16","max_tokens":128,"system":[{"type":"text","text":"reference"}],"messages":[{"role":"user","content":"question"}]}`)
	vertexWire, err := classifyProbeWire(vertex)
	require.NoError(t, err)
	_, err = probe.validateRequest(vertexWire, info, vertex, vertex, "https://upstream.example/v1/messages")
	assert.ErrorIs(t, err, errProbeProtocol)
	assert.NotErrorIs(t, err, errProbeInput)

	// An override that rewrites the probe model stays in the override class.
	changed := bytes.Replace(chat, []byte(`"mapped-model"`), []byte(`"other-model"`), 1)
	_, err = probe.validateRequest(wire, info, chat, changed, "https://upstream.example/v1/chat/completions")
	assert.ErrorIs(t, err, errProbeInput)
	assert.NotErrorIs(t, err, errProbeProtocol)
}

func TestChannelProbeLegacyCacheAffinity(t *testing.T) {
	for _, tc := range []struct{ body, affinity string }{
		{`{"mode":"custom","grok_conv_id":true}`, probeAffinityGrok},
		{`{"mode":"custom","grok_conv_id":false}`, probeAffinityNone},
		{`{"mode":"custom"}`, probeAffinityNone},
		{`{"mode":"custom","grok_conv_id":true,"cache_affinity":"none"}`, probeAffinityNone},
		{`{"mode":"custom","cache_affinity":"claude_cache_control"}`, probeAffinityClaude},
	} {
		t.Run(tc.body, func(t *testing.T) {
			var config channelProbeConfig
			require.NoError(t, common.UnmarshalJsonStr(tc.body, &config))
			assert.Equal(t, probeCustom, config.Mode)
			assert.Equal(t, tc.affinity, config.CacheAffinity)
			encoded, err := common.Marshal(config)
			require.NoError(t, err)
			assert.NotContains(t, string(encoded), "grok_conv_id")
		})
	}
}

func TestValidateChannelProxy(t *testing.T) {
	tests := []struct {
		name    string
		proxy   string
		wantErr bool
	}{
		{name: "empty"},
		{name: "http", proxy: "http://proxy.example:8080"},
		{name: "https", proxy: "https://proxy.example:8443"},
		{name: "socks5", proxy: "socks5://proxy.example"},
		{name: "socks5h", proxy: "socks5h://proxy.example:1080/"},
		{name: "unsupported", proxy: "ftp://proxy.example", wantErr: true},
		{name: "path", proxy: "socks5://proxy.example:1080/path", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setting, err := common.Marshal(dto.ChannelSettings{Proxy: test.proxy})
			require.NoError(t, err)
			channel := &model.Channel{
				Type:    constant.ChannelTypeOpenAI,
				Setting: common.GetPointer(string(setting)),
			}

			err = validateChannel(channel, false)

			if test.wantErr {
				require.ErrorContains(t, err, "invalid channel proxy")
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestValidateChannelRequiresNewAPIBaseURL(t *testing.T) {
	tests := []struct {
		name    string
		baseURL *string
		wantErr bool
	}{
		{name: "missing", wantErr: true},
		{name: "blank", baseURL: common.GetPointer("  "), wantErr: true},
		{name: "configured", baseURL: common.GetPointer("https://new-api.example")},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			channel := &model.Channel{
				Type:    constant.ChannelTypeNewAPI,
				BaseURL: test.baseURL,
			}

			err := validateChannel(channel, false)

			if test.wantErr {
				require.ErrorContains(t, err, "New API channel base URL cannot be empty")
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestNewAPIChannelRegistration(t *testing.T) {
	apiType, ok := common.ChannelType2APIType(constant.ChannelTypeNewAPI)

	require.True(t, ok)
	assert.Equal(t, constant.APITypeNewAPI, apiType)
	assert.Equal(t, "New API", constant.GetChannelTypeName(constant.ChannelTypeNewAPI))
	require.Greater(t, len(constant.ChannelBaseURLs), constant.ChannelTypeNewAPI)
	assert.Empty(t, constant.ChannelBaseURLs[constant.ChannelTypeNewAPI])
}

func TestResponsesCompactChannelSupport(t *testing.T) {
	tests := []struct {
		name        string
		channelType int
		apiType     int
		want        bool
	}{
		{name: "OpenAI", channelType: constant.ChannelTypeOpenAI, apiType: constant.APITypeOpenAI, want: true},
		{name: "Azure", channelType: constant.ChannelTypeAzure, apiType: constant.APITypeOpenAI, want: true},
		{name: "Codex", channelType: constant.ChannelTypeCodex, apiType: constant.APITypeCodex, want: true},
		{name: "Advanced Custom", channelType: constant.ChannelTypeAdvancedCustom, apiType: constant.APITypeAdvancedCustom, want: true},
		{name: "Sub2API", channelType: constant.ChannelTypeSub2API, apiType: constant.APITypeSub2API, want: true},
		{name: "New API", channelType: constant.ChannelTypeNewAPI, apiType: constant.APITypeNewAPI, want: true},
		{name: "Anthropic", channelType: constant.ChannelTypeAnthropic, apiType: constant.APITypeAnthropic, want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, common.SupportsResponsesCompact(test.channelType, test.apiType))
		})
	}
}

func TestMultiprotocolGatewayEndpointTypes(t *testing.T) {
	want := []constant.EndpointType{
		constant.EndpointTypeOpenAI,
		constant.EndpointTypeOpenAIResponse,
		constant.EndpointTypeOpenAIResponseCompact,
		constant.EndpointTypeAnthropic,
		constant.EndpointTypeGemini,
		constant.EndpointTypeOpenAIAlphaSearch,
	}

	assert.Equal(t, want, common.GetEndpointTypesByChannelType(constant.ChannelTypeNewAPI, "gpt-5"))
	assert.Equal(t, want, common.GetEndpointTypesByChannelType(constant.ChannelTypeSub2API, "gpt-5"))
}

func TestCopyChannelRejectsInvalidLegacyProxySettings(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	settingBytes, err := common.Marshal(dto.ChannelSettings{
		Proxy: "socks5://proxy.example/legacy-path",
	})
	require.NoError(t, err)
	setting := string(settingBytes)
	origin := &model.Channel{
		Type:    constant.ChannelTypeOpenAI,
		Name:    "legacy proxy channel",
		Key:     "test-key",
		Models:  "gpt-test",
		Group:   "default",
		Setting: &setting,
	}
	require.NoError(t, db.Create(origin).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "id", Value: fmt.Sprintf("%d", origin.Id)}}
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/channel/copy", nil)

	CopyChannel(ctx)

	assert.Contains(t, recorder.Body.String(), "invalid channel settings")
	var channelCount int64
	require.NoError(t, db.Model(&model.Channel{}).Count(&channelCount).Error)
	assert.Equal(t, int64(1), channelCount)
}

func TestDeleteChannelResetsProxyCacheWhenPreReadFails(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}, &model.AuditLog{}))
	service.ResetProxyClientCache()
	t.Cleanup(service.ResetProxyClientCache)

	proxyURL := "http://proxy.example:8080"
	beforeDelete, err := service.GetHttpClientWithProxy(proxyURL)
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "id", Value: "999999"}}
	ctx.Request = httptest.NewRequest(http.MethodDelete, "/api/channel/999999", nil)

	DeleteChannel(ctx)

	assert.Contains(t, recorder.Body.String(), `"success":true`)
	afterDelete, err := service.GetHttpClientWithProxy(proxyURL)
	require.NoError(t, err)
	assert.NotSame(t, beforeDelete, afterDelete)
}

func TestDeleteChannelBatchReportsAndAuditsActualDeletedCount(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}, &model.AuditLog{}))
	channel := &model.Channel{Name: "existing", Key: "test-key"}
	require.NoError(t, db.Create(channel).Error)

	requestBody, err := common.Marshal(ChannelBatch{Ids: []int{channel.Id, 999999}})
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodDelete, "/api/channel/batch", bytes.NewReader(requestBody))
	ctx.Request.Header.Set("Content-Type", "application/json")

	DeleteChannelBatch(ctx)

	var response struct {
		Success bool  `json:"success"`
		Data    int64 `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.True(t, response.Success)
	assert.Equal(t, int64(1), response.Data)

	var auditLog model.AuditLog
	require.NoError(t, db.Order("id desc").First(&auditLog).Error)
	var auditData struct {
		Operation struct {
			Params map[string]any `json:"params"`
		} `json:"op"`
	}
	encodedAudit, err := common.Marshal(auditLog.Other)
	require.NoError(t, err)
	require.NoError(t, common.Unmarshal(encodedAudit, &auditData))
	assert.Equal(t, float64(1), auditData.Operation.Params["count"])
}

func TestSettleTestQuotaUsesTieredBilling(t *testing.T) {
	info := &relaycommon.RelayInfo{
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode:   "tiered_expr",
			ExprString:    `param("stream") == true ? tier("stream", p * 3) : tier("base", p * 2)`,
			ExprHash:      billingexpr.ExprHashString(`param("stream") == true ? tier("stream", p * 3) : tier("base", p * 2)`),
			GroupRatio:    1,
			EstimatedTier: "stream",
			QuotaPerUnit:  common.QuotaPerUnit,
			ExprVersion:   1,
		},
		BillingRequestInput: &billingexpr.RequestInput{
			Body: []byte(`{"stream":true}`),
		},
	}

	quota, result := settleTestQuota(info, types.PriceData{
		ModelRatio:      1,
		CompletionRatio: 2,
	}, &dto.Usage{
		PromptTokens: 1000,
	})

	require.Equal(t, 1500, quota)
	require.NotNil(t, result)
	require.Equal(t, "stream", result.MatchedTier)
}

func TestBuildTestLogOtherInjectsTieredInfo(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	info := &relaycommon.RelayInfo{
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode: "tiered_expr",
			ExprString:  `tier("base", p * 2)`,
		},
		ChannelMeta: &relaycommon.ChannelMeta{},
	}
	priceData := types.PriceData{
		GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1},
	}
	usage := &dto.Usage{
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 12,
		},
	}

	requestRules := []billingexpr.RequestRuleTrace{{
		Cond:       `param("service_tier") == "fast"`,
		Multiplier: 2,
		Matched:    true,
	}}
	other := buildTestLogOther(ctx, info, priceData, usage, &billingexpr.TieredResult{
		MatchedTier:  "base",
		RequestRules: requestRules,
	})

	fields := other.Snapshot()
	require.Equal(t, "tiered_expr", fields["billing_mode"])
	require.Equal(t, "base", fields["matched_tier"])
	require.Equal(t, requestRules, fields["request_rules"])
	require.NotEmpty(t, fields["expr_b64"])
}

func TestBuildChannelTestHistoryResultKeepsUpstreamAndGatewayStatus(t *testing.T) {
	channel := &model.Channel{Id: 36, Name: "stream", Type: 1, Group: " default, gpt-pro "}
	streamErr := relaytypes.NewOpenAIError(
		errors.New("stream terminated"),
		relaytypes.ErrorCodeChannelUpstreamStreamTerminated,
		http.StatusBadGateway,
	)
	result := buildChannelTestHistoryResult(channel, testResult{
		localErr:           streamErr,
		newAPIError:        streamErr,
		modelName:          "gpt-test",
		requestPath:        "/v1/responses",
		isStream:           true,
		upstreamHTTPStatus: http.StatusOK,
		resultStatusCode:   http.StatusBadGateway,
	}, channelTestHistoryContext{
		RunID:     "run-1",
		RequestID: "request-1",
		Source:    "scheduled",
	}, 1200, service.ChannelTestActionNone)

	require.Equal(t, service.ChannelTestStatusFailed, result.Status)
	require.Equal(t, "stream_terminated", result.FailureKind)
	require.Equal(t, http.StatusOK, result.UpstreamHTTPStatus)
	require.Equal(t, http.StatusBadGateway, result.ResultStatusCode)
	require.Equal(t, "request-1", result.RequestID)
	require.Equal(t, "default,gpt-pro", result.ChannelGroups)
}

func TestBuildChannelTestHistoryResultPreservesUnsupportedFailure(t *testing.T) {
	channel := &model.Channel{Id: 36, Name: "unsupported", Type: 1}
	result := buildChannelTestHistoryResult(channel, testResult{
		localErr:    errors.New("channel test is not supported"),
		failureKind: service.ChannelTestFailureKindUnsupported,
	}, channelTestHistoryContext{Source: "manual_single"}, 0, service.ChannelTestActionNone)

	require.Equal(t, service.ChannelTestStatusFailed, result.Status)
	require.Equal(t, service.ChannelTestFailureKindUnsupported, result.FailureKind)
}

func TestResolveChannelTestUserIDUsesRequestUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("id", 2)

	userID, err := resolveChannelTestUserID(ctx)

	require.NoError(t, err)
	require.Equal(t, 2, userID)
}

func TestSelectChannelsForAutomaticTestPassiveRecoveryUsesPendingAutoDisabledKeys(t *testing.T) {
	channels := []*model.Channel{
		{Id: 1, Status: common.ChannelStatusEnabled},
		{Id: 2, Status: common.ChannelStatusAutoDisabled},
		{Id: 3, Status: common.ChannelStatusManuallyDisabled},
		{
			Id:     4,
			Status: common.ChannelStatusEnabled,
			Key:    "enabled\npending-recovery",
			ChannelInfo: model.ChannelInfo{
				IsMultiKey:         true,
				MultiKeyStatusList: map[int]int{1: common.ChannelStatusAutoDisabled},
			},
		},
	}

	selected := selectChannelsForAutomaticTest(channels, operation_setting.ChannelTestModePassiveRecovery, 10*time.Minute, 1000)

	require.Len(t, selected, 2)
	require.Equal(t, 2, selected[0].Channel.Id)
	require.Equal(t, 4, selected[1].Channel.Id)
	require.False(t, selected[0].AllowDisable)
	require.True(t, selected[0].RecordAutomaticTestTime)
}

func TestSelectChannelsForAutomaticTestScheduledSkipsManualDisabled(t *testing.T) {
	channels := []*model.Channel{
		{Id: 1, Status: common.ChannelStatusEnabled},
		{Id: 2, Status: common.ChannelStatusAutoDisabled},
		{Id: 3, Status: common.ChannelStatusManuallyDisabled},
	}

	selected := selectChannelsForAutomaticTest(channels, operation_setting.ChannelTestModeScheduledAll, 10*time.Minute, 1000)

	require.Len(t, selected, 2)
	require.Equal(t, 1, selected[0].Channel.Id)
	require.Equal(t, 2, selected[1].Channel.Id)
	require.True(t, selected[0].AllowDisable)
	require.True(t, selected[1].AllowDisable)
}

func TestSelectChannelsForAutomaticTestHonorsChannelHealthCheckOverrides(t *testing.T) {
	tenMinutes := 10
	thirtyMinutes := 30
	channels := []*model.Channel{
		{Id: 1, Status: common.ChannelStatusEnabled},
		{Id: 2, Status: common.ChannelStatusEnabled, LastAutoTestTime: 100},
		{Id: 3, Status: common.ChannelStatusEnabled},
		{Id: 4, Status: common.ChannelStatusAutoDisabled},
		{Id: 5, Status: common.ChannelStatusAutoDisabled, LastAutoTestTime: 900},
		{Id: 6, Status: common.ChannelStatusEnabled},
	}
	channels[1].SetOtherSettings(dto.ChannelOtherSettings{HealthCheck: &dto.ChannelHealthCheckSettings{Mode: dto.ChannelHealthCheckModeScheduled, IntervalMinutes: &thirtyMinutes}})
	channels[2].SetOtherSettings(dto.ChannelOtherSettings{HealthCheck: &dto.ChannelHealthCheckSettings{Mode: dto.ChannelHealthCheckModeExcluded}})
	channels[3].SetOtherSettings(dto.ChannelOtherSettings{HealthCheck: &dto.ChannelHealthCheckSettings{Mode: dto.ChannelHealthCheckModePassiveRecovery, IntervalMinutes: &tenMinutes}})
	channels[4].SetOtherSettings(dto.ChannelOtherSettings{HealthCheck: &dto.ChannelHealthCheckSettings{Mode: dto.ChannelHealthCheckModePassiveRecovery, IntervalMinutes: &thirtyMinutes}})
	channels[5].SetOtherSettings(dto.ChannelOtherSettings{HealthCheck: &dto.ChannelHealthCheckSettings{Mode: dto.ChannelHealthCheckModeScheduled}})

	selected := selectChannelsForAutomaticTest(channels, operation_setting.ChannelTestModeScheduledAll, 20*time.Minute, 1000)

	require.Len(t, selected, 3)
	require.Equal(t, 1, selected[0].Channel.Id)
	require.Equal(t, 4, selected[1].Channel.Id)
	require.Equal(t, 6, selected[2].Channel.Id)
	require.True(t, selected[0].AllowDisable)
	require.False(t, selected[1].AllowDisable)
	require.True(t, selected[2].AllowDisable)
}

func TestSelectChannelsForManualTestIgnoresChannelHealthCheckOverrides(t *testing.T) {
	channels := []*model.Channel{
		{Id: 1, Status: common.ChannelStatusEnabled},
		{Id: 2, Status: common.ChannelStatusAutoDisabled},
		{Id: 3, Status: common.ChannelStatusManuallyDisabled},
	}
	channels[0].SetOtherSettings(dto.ChannelOtherSettings{HealthCheck: &dto.ChannelHealthCheckSettings{Mode: dto.ChannelHealthCheckModeExcluded}})

	selected := selectChannelsForManualTest(channels)

	require.Len(t, selected, 2)
	require.Equal(t, 1, selected[0].Channel.Id)
	require.Equal(t, 2, selected[1].Channel.Id)
	require.False(t, selected[0].RecordAutomaticTestTime)
	require.False(t, selected[1].RecordAutomaticTestTime)
}

func TestSelectChannelsForAutomaticTestAutoBanOnlyUsesEligibleChannels(t *testing.T) {
	autoBanEnabled := 1
	autoBanDisabled := 0
	channels := []*model.Channel{
		{Id: 1, Status: common.ChannelStatusEnabled, AutoBan: &autoBanEnabled},
		{Id: 2, Status: common.ChannelStatusEnabled, AutoBan: &autoBanDisabled},
		{Id: 3, Status: common.ChannelStatusAutoDisabled, AutoBan: &autoBanEnabled},
		{Id: 4, Status: common.ChannelStatusManuallyDisabled, AutoBan: &autoBanEnabled},
		{Id: 5, Status: common.ChannelStatusEnabled},
	}

	selected := selectChannelsForAutomaticTest(channels, operation_setting.ChannelTestModeAutoBanOnly, 10*time.Minute, 1000)

	require.Len(t, selected, 2)
	require.Equal(t, 1, selected[0].Channel.Id)
	require.Equal(t, 3, selected[1].Channel.Id)
}

func TestRunChannelTestWorkersHonorsConfiguredConcurrency(t *testing.T) {
	originalInterval := common.RequestInterval
	common.RequestInterval = 0
	t.Cleanup(func() { common.RequestInterval = originalInterval })

	targets := []automaticChannelTestTarget{
		{Channel: &model.Channel{Id: 1, Status: common.ChannelStatusEnabled}},
		{Channel: &model.Channel{Id: 2, Status: common.ChannelStatusEnabled}},
		{Channel: &model.Channel{Id: 3, Status: common.ChannelStatusEnabled}},
		{Channel: &model.Channel{Id: 4, Status: common.ChannelStatusEnabled}},
	}
	started := make(chan struct{}, len(targets))
	release := make(chan struct{})
	var active atomic.Int32
	var maxActive atomic.Int32
	progress := make([]int, 0, len(targets)+1)
	summaryResult := make(chan channelTestSummary, 1)

	go func() {
		summaryResult <- runChannelTestWorkers(
			context.Background(),
			targets,
			2,
			func(_ context.Context, _ automaticChannelTestTarget) channelTestSummary {
				current := active.Add(1)
				defer active.Add(-1)
				for {
					observed := maxActive.Load()
					if current <= observed || maxActive.CompareAndSwap(observed, current) {
						break
					}
				}
				started <- struct{}{}
				<-release
				return channelTestSummary{Tested: 1, Succeeded: 1}
			},
			func(processed, _ int) {
				progress = append(progress, processed)
			},
		)
	}()

	<-started
	<-started
	select {
	case <-started:
		t.Fatal("started more channel tests than the configured concurrency")
	default:
	}
	close(release)

	summary := <-summaryResult

	assert.Equal(t, int32(2), maxActive.Load())
	assert.Equal(t, channelTestSummary{Tested: 4, Succeeded: 4}, summary)
	assert.Equal(t, []int{0, 1, 2, 3, 4}, progress)
}

func TestRunChannelTestWorkersStopsAfterCancellation(t *testing.T) {
	originalInterval := common.RequestInterval
	common.RequestInterval = 0
	t.Cleanup(func() { common.RequestInterval = originalInterval })

	ctx, cancel := context.WithCancel(context.Background())
	targets := []automaticChannelTestTarget{
		{Channel: &model.Channel{Id: 1, Status: common.ChannelStatusEnabled}},
		{Channel: &model.Channel{Id: 2, Status: common.ChannelStatusEnabled}},
		{Channel: &model.Channel{Id: 3, Status: common.ChannelStatusEnabled}},
		{Channel: &model.Channel{Id: 4, Status: common.ChannelStatusEnabled}},
	}
	started := make(chan struct{}, len(targets))
	progress := make([]int, 0, 1)
	summaryResult := make(chan channelTestSummary, 1)

	go func() {
		summaryResult <- runChannelTestWorkers(
			ctx,
			targets,
			2,
			func(ctx context.Context, _ automaticChannelTestTarget) channelTestSummary {
				started <- struct{}{}
				<-ctx.Done()
				return channelTestSummary{Tested: 1, Succeeded: 1}
			},
			func(processed, _ int) {
				progress = append(progress, processed)
			},
		)
	}()

	<-started
	<-started
	cancel()

	summary := <-summaryResult

	select {
	case <-started:
		t.Fatal("started another channel test after cancellation")
	default:
	}
	assert.Equal(t, channelTestSummary{Tested: 2, Succeeded: 2}, summary)
	assert.Equal(t, []int{0}, progress)
}

func TestTestAllChannelsRejectsExistingActiveTask(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.SystemTask{}, &model.SystemTaskLock{}))

	existing, err := model.CreateSystemTask(model.SystemTaskTypeChannelTest, nil, nil)
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/channel/test", nil)

	TestAllChannels(ctx)

	require.Equal(t, http.StatusConflict, recorder.Code)
	require.Contains(t, recorder.Body.String(), existing.TaskID)
	require.Contains(t, recorder.Body.String(), "已有通道测试任务正在运行或等待中")
}
