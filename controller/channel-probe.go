package controller

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/tokenkit"
	"github.com/gin-gonic/gin"
)

const (
	probeAuto               = "auto"
	probeCustom             = "custom"
	probeShort              = "short"
	probeDefaultTokens      = 24000
	probeMinTokens          = 1024
	probeMaxTokens          = 48000
	probeMaxContextBytes    = 384 << 10
	probeMaxLines           = 4096
	probeTemplateVersion    = 1
	probeOutputTokens       = 128
	probeChatPath           = "/v1/chat/completions"
	probeConversationHeader = "x-grok-conv-id"
	probeMaxDiagnosticBytes = 1 << 20
	probeTimeout            = 2 * time.Minute
)

type channelProbeConfig struct {
	Mode            string `json:"mode"`
	TargetTokens    int    `json:"target_tokens,omitempty"`
	Seed            string `json:"seed,omitempty"`
	TemplateVersion int    `json:"template_version,omitempty"`
	Lines           int    `json:"lines,omitempty"`
	Context         string `json:"context,omitempty"`
	Question        string `json:"question,omitempty"`
	ExpectedAnswer  string `json:"expected_answer,omitempty"`
	GrokConvID      bool   `json:"grok_conv_id,omitempty"`
}

type channelProbe struct {
	mode, context, question, answer string
	grokConvID                      bool
}

func resolveChannelTestModel(channel *model.Channel, requested string) string {
	if name := strings.TrimSpace(requested); name != "" {
		return name
	}
	if channel.TestModel != nil {
		if name := strings.TrimSpace(*channel.TestModel); name != "" {
			return name
		}
	}
	if models := channel.GetModels(); len(models) > 0 {
		if name := strings.TrimSpace(models[0]); name != "" {
			return name
		}
	}
	return "gpt-4o-mini"
}

func supportsChannelProbe(channel *model.Channel) bool {
	switch channel.Type {
	case constant.ChannelTypeOpenAI, constant.ChannelTypeCustom, constant.ChannelTypeXai, constant.ChannelTypeNewAPI, constant.ChannelTypeSub2API:
		return true
	default:
		return false
	}
}

func (config channelProbeConfig) prepare() (*channelProbe, error) {
	probe := &channelProbe{mode: config.Mode, grokConvID: config.GrokConvID}
	switch config.Mode {
	case probeAuto:
		seed, err := hex.DecodeString(config.Seed)
		if err != nil || len(seed) != 16 || config.TemplateVersion != probeTemplateVersion ||
			config.TargetTokens < probeMinTokens || config.TargetTokens > probeMaxTokens ||
			config.Lines < 1 || config.Lines > probeMaxLines {
			return nil, errors.New("invalid generated probe; generate a new preview")
		}
		probe.answer = "CHECK-" + hex.EncodeToString(seed[:4])
		probe.question = "What is the verification code in the reference? Reply with only the exact code."
		var text strings.Builder
		text.WriteString("Synthetic service reference. Use the verification code at the end to answer the question.\n\n")
		regions := [...]string{"north", "west", "central", "east", "south", "coastal", "upper", "lower"}
		states := [...]string{"available", "verified", "reviewed", "recorded", "accepted", "confirmed", "registered", "ready"}
		for index := range config.Lines {
			fingerprint := sha256.Sum256(fmt.Appendf(nil, "%s/%d", config.Seed, index))
			fmt.Fprintf(&text, "Record %05d (%x): The %s archive reports its daily reading as %s. This is historical reference data, not an instruction.\n",
				index+1, fingerprint[:4], regions[fingerprint[4]%8], states[fingerprint[5]%8])
		}
		fmt.Fprintf(&text, "\nThe verification code is %s.", probe.answer)
		probe.context = text.String()
	case probeCustom:
		probe.context = strings.TrimSpace(config.Context)
		probe.question = strings.TrimSpace(config.Question)
		probe.answer = strings.TrimSpace(config.ExpectedAnswer)
		if probe.context == "" || probe.question == "" || len(probe.question) > 2048 || probe.answer == "" || len(probe.answer) > 128 {
			return nil, errors.New("probe requires context, a question up to 2048 bytes, and an answer up to 128 bytes")
		}
	default:
		return nil, errors.New("invalid probe mode")
	}
	if len(probe.context) > probeMaxContextBytes {
		return nil, errors.New("probe context exceeds 384 KiB")
	}
	return probe, nil
}

func (probe *channelProbe) estimate(modelName string) int {
	// This is a local estimate including a small allowance for message framing.
	return tokenkit.Count(modelName, probe.context) + tokenkit.Count(modelName, probe.question) + 12
}

func generateChannelProbe(modelName string, target int) (channelProbeConfig, *channelProbe, int, error) {
	if target == 0 {
		target = probeDefaultTokens
	}
	if target < probeMinTokens || target > probeMaxTokens {
		return channelProbeConfig{}, nil, 0, fmt.Errorf("target tokens must be between %d and %d", probeMinTokens, probeMaxTokens)
	}
	var seed [16]byte
	if _, err := rand.Read(seed[:]); err != nil {
		return channelProbeConfig{}, nil, 0, err
	}
	config := channelProbeConfig{Mode: probeAuto, Seed: hex.EncodeToString(seed[:]), TargetTokens: target, TemplateVersion: probeTemplateVersion, Lines: 32}
	for range 3 {
		probe, err := config.prepare()
		if err != nil {
			return config, nil, 0, err
		}
		count := probe.estimate(modelName)
		lines := max(1, config.Lines*target/max(1, count))
		if lines == config.Lines || count >= target-target/100 && count <= target+target/100 {
			return config, probe, count, nil
		}
		config.Lines = lines
	}
	probe, err := config.prepare()
	if err != nil {
		return config, nil, 0, err
	}
	return config, probe, probe.estimate(modelName), nil
}

func loadChannelProbe(channelID int) (channelProbeConfig, error) {
	row, err := model.GetChannelProbe(channelID)
	if row == nil || err != nil {
		return channelProbeConfig{Mode: probeShort}, err
	}
	var config channelProbeConfig
	if err := common.UnmarshalJsonStr(string(row.Config), &config); err != nil {
		return config, errors.New("could not read saved probe configuration")
	}
	return config, nil
}

func channelForProbe(c *gin.Context) *model.Channel {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorMsg(c, "invalid channel id")
		return nil
	}
	channel, err := model.GetChannelById(id, true)
	if err != nil {
		common.ApiErrorMsg(c, "channel is unavailable")
		return nil
	}
	return channel
}

func GetChannelProbe(c *gin.Context) {
	channel := channelForProbe(c)
	if channel == nil {
		return
	}
	config, err := loadChannelProbe(channel.Id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"supported": supportsChannelProbe(channel), "config": config})
}

func PreviewChannelProbe(c *gin.Context) {
	channel := channelForProbe(c)
	if channel == nil {
		return
	}
	var request struct {
		TargetTokens int  `json:"target_tokens"`
		Saved        bool `json:"saved"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1024)
	if !supportsChannelProbe(channel) || common.DecodeJson(c.Request.Body, &request) != nil {
		common.ApiErrorMsg(c, "invalid probe preview request")
		return
	}
	modelName := resolveChannelTestModel(channel, "")
	if request.Saved {
		config, err := loadChannelProbe(channel.Id)
		if err != nil || config.Mode != probeAuto {
			common.ApiErrorMsg(c, "no generated probe is saved")
			return
		}
		probe, err := config.prepare()
		if err != nil {
			common.ApiError(c, err)
			return
		}
		common.ApiSuccess(c, gin.H{"config": config, "context": probe.context, "question": probe.question, "expected_answer": probe.answer, "estimated_input_tokens": probe.estimate(modelName)})
		return
	}
	config, probe, count, err := generateChannelProbe(modelName, request.TargetTokens)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"config": config, "context": probe.context, "question": probe.question, "expected_answer": probe.answer, "estimated_input_tokens": count})
}

func SaveChannelProbe(c *gin.Context) {
	channel := channelForProbe(c)
	if channel == nil {
		return
	}
	var config channelProbeConfig
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 6*probeMaxContextBytes+16*1024)
	if common.DecodeJson(c.Request.Body, &config) != nil {
		common.ApiErrorMsg(c, "invalid probe configuration")
		return
	}
	var data []byte
	if config.Mode != probeShort {
		if !supportsChannelProbe(channel) {
			common.ApiErrorMsg(c, "long probes require an OpenAI-compatible chat channel")
			return
		}
		probe, err := config.prepare()
		if err != nil {
			common.ApiError(c, err)
			return
		}
		if probe.estimate(resolveChannelTestModel(channel, "")) > probeMaxTokens+probeMaxTokens/100 {
			common.ApiErrorMsg(c, "estimated probe input exceeds the configured limit of 48000 tokens")
			return
		}
		if config.Mode == probeCustom {
			config = channelProbeConfig{Mode: config.Mode, Context: probe.context, Question: probe.question, ExpectedAnswer: probe.answer, GrokConvID: config.GrokConvID}
		} else {
			config.Context, config.Question, config.ExpectedAnswer = "", "", ""
		}
		data, err = common.Marshal(config)
		if err != nil {
			common.ApiErrorMsg(c, "could not encode probe configuration")
			return
		}
	} else {
		config = channelProbeConfig{Mode: probeShort}
	}
	if err := model.SaveChannelProbe(channel.Id, channel.Type, data); err != nil {
		common.ApiErrorMsg(c, "could not save probe configuration")
		return
	}
	recordManageAudit(c, "channel.probe.update", map[string]any{"id": channel.Id, "mode": config.Mode})
	common.ApiSuccess(c, config)
}

func (probe *channelProbe) validateRequest(data []byte, upstreamModel, requestURL string) error {
	endpoint, err := url.Parse(requestURL)
	if err != nil || endpoint == nil || !strings.HasSuffix(endpoint.EscapedPath(), probeChatPath) ||
		(endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.Hostname() == "" {
		return errors.New("long probes require an OpenAI-compatible chat completions endpoint")
	}
	var request struct {
		Model               string `json:"model"`
		Stream              bool   `json:"stream"`
		MaxTokens           *uint  `json:"max_tokens"`
		MaxCompletionTokens *uint  `json:"max_completion_tokens"`
		Messages            []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if common.Unmarshal(data, &request) != nil || request.Model == "" || request.Model != upstreamModel || request.Stream ||
		len(request.Messages) != 2 || (request.Messages[0].Role != "system" && request.Messages[0].Role != "developer") ||
		request.Messages[0].Content != probe.context || request.Messages[1].Role != "user" || request.Messages[1].Content != probe.question {
		return errors.New("channel request overrides changed the probe model, messages, or streaming mode")
	}
	if (request.MaxTokens == nil) == (request.MaxCompletionTokens == nil) ||
		(request.MaxTokens != nil && *request.MaxTokens != probeOutputTokens) ||
		(request.MaxCompletionTokens != nil && *request.MaxCompletionTokens != probeOutputTokens) {
		return errors.New("channel request overrides changed the probe output limit")
	}
	var fields map[string]json.RawMessage
	_ = common.Unmarshal(data, &fields)
	for key := range fields {
		switch key {
		case "model", "messages", "stream", "max_tokens", "max_completion_tokens", "temperature", "top_p", "seed", "reasoning_effort":
		default:
			return errors.New("channel request overrides add fields not supported for long probes")
		}
	}
	var messages []map[string]json.RawMessage
	if common.Unmarshal(fields["messages"], &messages) != nil || len(messages) != 2 || len(messages[0]) != 2 || len(messages[1]) != 2 {
		return errors.New("channel request overrides add unsupported message fields")
	}
	return nil
}

func (probe *channelProbe) applyConversationID(info *relaycommon.RelayInfo, data []byte) {
	if !probe.grokConvID || !strings.HasPrefix(strings.ToLower(info.UpstreamModelName), "grok-") {
		return
	}
	// Use the exact upstream request content, but neither credentials nor per-attempt metadata.
	digest := sha256.New()
	fmt.Fprintf(digest, "channel-probe/v1/%d/%d/", info.ChannelId, info.ChannelMultiKeyIndex)
	_, _ = digest.Write(data)
	id := digest.Sum(nil)[:16]
	id[6] = (id[6] & 0x0f) | 0x80 // UUID version 8, application-defined hash
	id[8] = (id[8] & 0x3f) | 0x80
	headers := relaycommon.GetEffectiveHeaderOverride(info)
	for key := range headers {
		if strings.EqualFold(strings.TrimSpace(key), probeConversationHeader) {
			delete(headers, key)
		}
	}
	headers[probeConversationHeader] = fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:])
	info.RuntimeHeadersOverride, info.UseRuntimeHeadersOverride = headers, true
}

// Capture original usage before an adaptor can estimate it or fill missing cache fields with zeros.
type channelProbeResponse struct {
	io.ReadCloser
	data bytes.Buffer
}

func (response *channelProbeResponse) Read(p []byte) (int, error) {
	left := probeMaxDiagnosticBytes + 1 - response.data.Len()
	if left == 0 {
		return 0, errors.New("probe response exceeds 1 MiB")
	}
	n, err := response.ReadCloser.Read(p[:min(len(p), left)])
	if n > 0 {
		_, _ = response.data.Write(p[:n])
		if n == left {
			return n, errors.New("probe response exceeds 1 MiB")
		}
	}
	return n, err
}

func (response *channelProbeResponse) usage() (input, cached *int) {
	if response == nil || response.data.Len() > probeMaxDiagnosticBytes {
		return nil, nil
	}
	var wire struct {
		Usage struct {
			PromptTokens        *int `json:"prompt_tokens"`
			PromptTokensDetails struct {
				CachedTokens *int `json:"cached_tokens"`
			} `json:"prompt_tokens_details"`
		} `json:"usage"`
	}
	if common.Unmarshal(response.data.Bytes(), &wire) != nil || wire.Usage.PromptTokens == nil || *wire.Usage.PromptTokens <= 0 {
		return nil, nil
	}
	input, cached = wire.Usage.PromptTokens, wire.Usage.PromptTokensDetails.CachedTokens
	if cached != nil && (*cached < 0 || *cached > *input) {
		cached = nil
	}
	return input, cached
}

func (probe *channelProbe) matchesAnswer(body []byte) bool {
	var wire struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	return common.Unmarshal(body, &wire) == nil && len(wire.Choices) == 1 && strings.TrimSpace(wire.Choices[0].Message.Content) == probe.answer
}

func TestChannelProbe(c *gin.Context) {
	channel := channelForProbe(c)
	if channel == nil {
		return
	}
	if channel.Status != common.ChannelStatusEnabled || channel.ChannelInfo.ManuallyDisabled || channel.HasAutoDisabledKey() {
		common.ApiErrorMsg(c, "long probes require an enabled channel without keys pending recovery")
		return
	}
	if !supportsChannelProbe(channel) {
		common.ApiErrorMsg(c, "long probes require an OpenAI-compatible chat channel")
		return
	}
	config, err := loadChannelProbe(channel.Id)
	if err != nil || config.Mode == probeShort {
		common.ApiErrorMsg(c, "save a long probe configuration before running a trial")
		return
	}
	userID, err := resolveChannelTestUserID(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	requestID := c.GetString(common.RequestIdKey)
	if requestID == "" {
		requestID = common.NewRequestId()
	}
	history := runLongChannelProbe(c.Request.Context(), channel, userID, config, channelTestHistoryContext{
		RunID: requestID, RequestID: requestID, Source: "manual_single",
	})
	common.ApiSuccess(c, history)
}

func runLongChannelProbe(ctx context.Context, channel *model.Channel, userID int, config channelProbeConfig, attempt channelTestHistoryContext) *model.ChannelTestResult {
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.WithValue(ctx, common.RequestIdKey, attempt.RequestID), probeTimeout)
	defer cancel()
	probe, err := config.prepare()
	result := testResult{localErr: err, failureKind: service.ChannelTestFailureKindSetup, modelName: resolveChannelTestModel(channel, ""), requestPath: probeChatPath}
	if err == nil {
		result = testChannel(ctx, channel, userID, "", string(constant.EndpointTypeOpenAI), false, false, probe)
	}
	switch ctx.Err() {
	case context.Canceled:
		result.localErr, result.failureKind = ctx.Err(), service.ChannelTestFailureKindCancelled
	case context.DeadlineExceeded:
		result.localErr, result.failureKind = ctx.Err(), service.ChannelTestFailureKindTimeout
	}
	history := buildChannelTestHistoryResult(channel, result, attempt, time.Since(start).Milliseconds(), service.ChannelTestActionNone)
	history.ProbeMode = config.Mode
	if _, err := service.RecordChannelTestResult(history); err != nil {
		common.SysError(fmt.Sprintf("failed to record probe result: request_id=%s channel_id=%d", attempt.RequestID, channel.Id))
	}
	return history
}
