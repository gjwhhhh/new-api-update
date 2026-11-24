package controller

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
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
	probeMaxDiagnosticBytes = 1 << 20
	probeTimeout            = 2 * time.Minute
	probeAffinityNone       = "none"
	probeAffinityGrok       = "grok_header"
	probeAffinityClaude     = "claude_cache_control"
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
	CacheAffinity   string `json:"cache_affinity"`
}

type channelProbe struct {
	mode, context, question, answer string
	cacheAffinity                   string
}

func (config *channelProbeConfig) UnmarshalJSON(data []byte) error {
	type probeConfig channelProbeConfig
	var saved struct {
		probeConfig
		GrokConvID bool `json:"grok_conv_id"`
	}
	if err := common.Unmarshal(data, &saved); err != nil {
		return err
	}
	*config = channelProbeConfig(saved.probeConfig)
	if config.CacheAffinity == "" {
		config.CacheAffinity = probeAffinityNone
		if saved.GrokConvID {
			config.CacheAffinity = probeAffinityGrok
		}
	}
	return nil
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
	if channel.Type == constant.ChannelTypeAws {
		// Bedrock either calls the AWS SDK directly or addresses the model in the
		// endpoint, so the probe body and endpoint checks cannot pass.
		return false
	}
	return !slices.Contains(unsupportedTestChannelTypes, channel.Type)
}

func channelProbeCacheAffinity(channel *model.Channel) string {
	switch channel.Type {
	case constant.ChannelTypeXai:
		return probeAffinityGrok
	case constant.ChannelTypeAnthropic:
		return probeAffinityClaude
	}
	testModel := resolveChannelTestModel(channel, "")
	if constant.IsAdvancedCustomChannel(channel.Type) {
		settings := channel.GetOtherSettings()
		route, _ := settings.AdvancedCustom.MatchPathForModel(probeChatPath, testModel)
		if strings.TrimSpace(route.Converter) == relayconvert.ConverterOpenAIChatToClaudeMessages {
			return probeAffinityClaude
		}
	}
	if channel.Type == constant.ChannelTypeVertexAi {
		c := &gin.Context{}
		c.Set("model_mapping", channel.GetModelMapping())
		info := &relaycommon.RelayInfo{OriginModelName: testModel, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: testModel}}
		if helper.ModelMappedHelper(c, info, nil) == nil && strings.HasPrefix(info.UpstreamModelName, "claude") {
			return probeAffinityClaude
		}
	}
	return probeAffinityNone
}

func (config channelProbeConfig) prepare() (*channelProbe, error) {
	switch config.CacheAffinity {
	case "", probeAffinityNone, probeAffinityGrok, probeAffinityClaude:
	default:
		return nil, errors.New("invalid probe cache affinity")
	}
	probe := &channelProbe{mode: config.Mode, cacheAffinity: config.CacheAffinity}
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
	config := channelProbeConfig{Mode: probeAuto, Seed: hex.EncodeToString(seed[:]), TargetTokens: target, TemplateVersion: probeTemplateVersion, Lines: 32, CacheAffinity: probeAffinityNone}
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
		return channelProbeConfig{Mode: probeShort, CacheAffinity: probeAffinityNone}, err
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
	affinity := channelProbeCacheAffinity(channel)
	if affinity == probeAffinityNone && supportsChannelProbe(channel) && config.CacheAffinity == probeAffinityGrok {
		// Keep the control available on compatible gateways with a saved legacy setting.
		affinity = probeAffinityGrok
	}
	common.ApiSuccess(c, gin.H{"supported": supportsChannelProbe(channel), "cache_affinity": affinity, "config": config})
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
			common.ApiErrorMsg(c, "this channel does not support long probes")
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
			config = channelProbeConfig{Mode: config.Mode, Context: probe.context, Question: probe.question, ExpectedAnswer: probe.answer, CacheAffinity: config.CacheAffinity}
		} else {
			config.Context, config.Question, config.ExpectedAnswer = "", "", ""
		}
		data, err = common.Marshal(config)
		if err != nil {
			common.ApiErrorMsg(c, "could not encode probe configuration")
			return
		}
	} else {
		config = channelProbeConfig{Mode: probeShort, CacheAffinity: probeAffinityNone}
	}
	if err := model.SaveChannelProbe(channel.Id, channel.Type, data); err != nil {
		common.ApiErrorMsg(c, "could not save probe configuration")
		return
	}
	recordManageAudit(c, "channel.probe.update", map[string]any{"id": channel.Id, "mode": config.Mode})
	common.ApiSuccess(c, config)
}

func (probe *channelProbe) validateRequest(wire *probeWire, info *relaycommon.RelayInfo, convertedBody, body []byte, requestURL string) ([]byte, error) {
	endpoint, err := url.Parse(requestURL)
	if err != nil || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.Hostname() == "" {
		return nil, errors.New("invalid upstream probe endpoint")
	}
	switch wire.name {
	case "gemini":
		if !strings.HasSuffix(endpoint.Path, "/models/"+info.UpstreamModelName+":generateContent") {
			return nil, errProbeProtocol
		}
	case "anthropic":
		if !probeHasFields(body, "model") &&
			(info.ChannelType != constant.ChannelTypeVertexAi || !strings.HasSuffix(endpoint.Path, ":rawPredict")) {
			return nil, errProbeProtocol
		}
	}

	headers := make(map[string]string)
	if probe.cacheAffinity == wire.cacheAffinity && wire.cacheAffinity != "" {
		body, err = wire.affinity(info, headers, body)
		if err != nil {
			return nil, err
		}
	}
	if err := wire.validate(body, info.UpstreamModelName, probe); err != nil {
		return nil, err
	}
	if wire.cacheAffinity == probeAffinityClaude && probe.cacheAffinity == probeAffinityClaude && len(info.ParamOverride) > 0 {
		// A delete against the original unmarked block is a no-op. Check the same
		// overrides with the marker present so injection cannot hide its removal.
		marked, err := wire.affinity(info, nil, convertedBody)
		if err != nil {
			return nil, err
		}
		overridden, err := relaycommon.ApplyParamOverride(marked, info.ParamOverride, relaycommon.BuildParamOverrideContext(info))
		if err == nil && !probeHasFields(overridden, "system.0.cache_control") {
			return nil, errProbeCache
		}
	}
	if len(headers) > 0 {
		overrides := relaycommon.GetEffectiveHeaderOverride(info)
		for key, value := range headers {
			// Overrides are later folded to one case, so drop any alias that would
			// otherwise compete with the injected value.
			for existing := range overrides {
				if strings.EqualFold(strings.TrimSpace(existing), key) {
					delete(overrides, existing)
				}
			}
			overrides[key] = value
		}
		info.RuntimeHeadersOverride, info.UseRuntimeHeadersOverride = overrides, true
	}
	return body, nil
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

func (response *channelProbeResponse) usage(wire *probeWire) (input, cached *int) {
	if response == nil || response.data.Len() > probeMaxDiagnosticBytes {
		return nil, nil
	}
	return wire.usage(response.data.Bytes())
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
		common.ApiErrorMsg(c, "this channel does not support long probes")
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
