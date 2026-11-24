package controller

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/tidwall/gjson"
)

const probeConversationHeader = "x-grok-conv-id"

var (
	errProbeFields   = errors.New("channel request overrides add fields not supported for long probes")
	errProbeProtocol = errors.New("upstream request does not match the expected protocol, endpoint, or model for long probes")
	errProbeInput    = errors.New("channel request overrides changed the probe model, messages, or streaming mode")
	errProbeLimit    = errors.New("channel request overrides changed the probe output limit")
	errProbeCache    = errors.New("channel request overrides changed the probe cache control")
)

type probeWire struct {
	name          string
	classify      func([]byte) bool
	validate      func([]byte, string, *channelProbe) error
	usage         func([]byte) (*int, *int)
	cacheAffinity string
	affinity      func(*relaycommon.RelayInfo, map[string]string, []byte) ([]byte, error)
}

var probeWires = [...]probeWire{
	{
		name: "anthropic",
		classify: func(body []byte) bool {
			// The direct Messages API carries anthropic-version in a header.
			return probeHasFields(body, "messages", "system") &&
				(probeHasFields(body, "anthropic_version") || probeHasFields(body, "model"))
		},
		validate:      validateAnthropicProbe,
		usage:         anthropicProbeUsage,
		cacheAffinity: probeAffinityClaude,
		affinity:      applyClaudeProbeCache,
	},
	{
		name: "openai-chat",
		classify: func(body []byte) bool {
			return probeHasFields(body, "messages") &&
				(probeHasFields(body, "max_tokens") || probeHasFields(body, "max_completion_tokens"))
		},
		validate:      validateOpenAIChatProbe,
		usage:         openAIChatProbeUsage,
		cacheAffinity: probeAffinityGrok,
		affinity:      applyGrokProbeHeader,
	},
	{
		name: "openai-responses",
		classify: func(body []byte) bool {
			return probeHasFields(body, "input", "model")
		},
		validate: validateOpenAIResponsesProbe,
		usage:    openAIResponsesProbeUsage,
		affinity: noProbeAffinity,
	},
	{
		name: "gemini",
		classify: func(body []byte) bool {
			return probeHasFields(body, "contents", "generationConfig")
		},
		validate: validateGeminiProbe,
		usage:    geminiProbeUsage,
		affinity: noProbeAffinity,
	},
}

func probeHasFields(body []byte, keys ...string) bool {
	for _, key := range keys {
		if !gjson.GetBytes(body, key).Exists() {
			return false
		}
	}
	return true
}

func classifyProbeWire(body []byte) (*probeWire, error) {
	if gjson.ValidBytes(body) {
		for i := range probeWires {
			if probeWires[i].classify(body) {
				return &probeWires[i], nil
			}
		}
	}
	return nil, errors.New("unsupported upstream chat protocol")
}

type probeFields map[string]json.RawMessage

func (fields probeFields) only(keys ...string) bool {
	for key := range fields {
		if !slices.Contains(keys, key) {
			return false
		}
	}
	return true
}

func probeField[T any](fields probeFields, key string) T {
	var value T
	if raw, ok := fields[key]; ok && common.Unmarshal(raw, &value) == nil {
		return value
	}
	var zero T
	return zero
}

func (fields probeFields) nonStreaming() bool {
	if _, ok := fields["stream"]; !ok {
		return true
	}
	stream := probeField[*bool](fields, "stream")
	return stream != nil && !*stream
}

func (fields probeFields) matchesModel(upstreamModel string) bool {
	return upstreamModel != "" && probeField[string](fields, "model") == upstreamModel
}

func (fields probeFields) tokenLimit(key string) bool {
	return probeField[uint](fields, key) == probeOutputTokens
}

func validateOpenAIChatProbe(body []byte, upstreamModel string, p *channelProbe) error {
	var fields probeFields
	if common.Unmarshal(body, &fields) != nil ||
		!fields.only("model", "messages", "stream", "max_tokens", "max_completion_tokens", "temperature", "top_p", "seed", "reasoning_effort") {
		return errProbeFields
	}
	messages := probeField[[]probeFields](fields, "messages")
	if !fields.matchesModel(upstreamModel) || !fields.nonStreaming() || len(messages) != 2 ||
		!slices.Contains([]string{"system", "developer"}, probeField[string](messages[0], "role")) ||
		probeField[string](messages[0], "content") != p.context || !probeTextMessage(messages[1], "user", p.question) {
		return errProbeInput
	}
	if !messages[0].only("role", "content") {
		return errProbeFields
	}
	_, hasMax := fields["max_tokens"]
	_, hasCompletionMax := fields["max_completion_tokens"]
	if hasMax == hasCompletionMax || hasMax && !fields.tokenLimit("max_tokens") ||
		hasCompletionMax && !fields.tokenLimit("max_completion_tokens") {
		return errProbeLimit
	}
	return nil
}

func probeTextMessage(message probeFields, role, content string) bool {
	return message.only("role", "content") && probeField[string](message, "role") == role &&
		probeField[string](message, "content") == content
}

func validateOpenAIResponsesProbe(body []byte, upstreamModel string, p *channelProbe) error {
	var fields probeFields
	if common.Unmarshal(body, &fields) != nil ||
		!fields.only("model", "input", "instructions", "stream", "max_output_tokens", "temperature", "top_p", "reasoning") {
		return errProbeFields
	}
	input := probeField[[]probeFields](fields, "input")
	if !fields.matchesModel(upstreamModel) || !fields.nonStreaming() ||
		probeField[string](fields, "instructions") != p.context ||
		len(input) != 1 || !probeTextMessage(input[0], "user", p.question) {
		return errProbeInput
	}
	if !fields.tokenLimit("max_output_tokens") {
		return errProbeLimit
	}
	if _, ok := fields["reasoning"]; ok && !probeField[probeFields](fields, "reasoning").only("effort") {
		return errProbeFields
	}
	return nil
}

func validateAnthropicProbe(body []byte, upstreamModel string, p *channelProbe) error {
	var fields probeFields
	if common.Unmarshal(body, &fields) != nil ||
		!fields.only("model", "messages", "system", "anthropic_version", "stream", "max_tokens", "temperature", "top_p", "top_k") {
		return errProbeFields
	}
	// Vertex supplies the model in the endpoint and the protocol version in the body.
	if upstreamModel == "" {
		return errProbeProtocol
	}
	_, hasModel := fields["model"]
	_, hasVersion := fields["anthropic_version"]
	if hasModel && !fields.matchesModel(upstreamModel) ||
		!hasModel && !hasVersion || hasVersion && probeField[string](fields, "anthropic_version") == "" ||
		!fields.nonStreaming() {
		return errProbeInput
	}
	system := probeField[[]probeFields](fields, "system")
	messages := probeField[[]probeFields](fields, "messages")
	if len(system) != 1 || !system[0].only("type", "text", "cache_control") ||
		probeField[string](system[0], "type") != "text" || probeField[string](system[0], "text") != p.context ||
		len(messages) != 1 || !probeTextMessage(messages[0], "user", p.question) {
		return errProbeInput
	}
	if !fields.tokenLimit("max_tokens") {
		return errProbeLimit
	}
	_, hasCache := system[0]["cache_control"]
	if p.cacheAffinity == probeAffinityClaude {
		cache := probeField[probeFields](system[0], "cache_control")
		if !cache.only("type") || probeField[string](cache, "type") != "ephemeral" {
			return errProbeCache
		}
	} else if hasCache {
		return errProbeCache
	}
	return nil
}

func validateGeminiProbe(body []byte, upstreamModel string, p *channelProbe) error {
	var fields probeFields
	if common.Unmarshal(body, &fields) != nil ||
		!fields.only("contents", "systemInstruction", "generationConfig", "safetySettings") {
		return errProbeFields
	}
	// Gemini selects the mapped model and streaming mode in the endpoint.
	if upstreamModel == "" {
		return errProbeProtocol
	}
	system := probeField[probeFields](fields, "systemInstruction")
	contents := probeField[[]probeFields](fields, "contents")
	if !system.only("parts") || !probeGeminiText(system, p.context) ||
		len(contents) != 1 || !contents[0].only("role", "parts") ||
		probeField[string](contents[0], "role") != "user" || !probeGeminiText(contents[0], p.question) {
		return errProbeInput
	}
	config := probeField[probeFields](fields, "generationConfig")
	if !config.only("maxOutputTokens", "temperature", "topP", "topK", "seed") {
		return errProbeFields
	}
	if !config.tokenLimit("maxOutputTokens") {
		return errProbeLimit
	}
	return nil
}

func probeGeminiText(content probeFields, text string) bool {
	parts := probeField[[]probeFields](content, "parts")
	return len(parts) == 1 && parts[0].only("text") && probeField[string](parts[0], "text") == text
}

func openAIChatProbeUsage(raw []byte) (*int, *int) {
	var response struct {
		Usage struct {
			Input   *int `json:"prompt_tokens"`
			Details struct {
				Cached *int `json:"cached_tokens"`
			} `json:"prompt_tokens_details"`
		} `json:"usage"`
	}
	if common.Unmarshal(raw, &response) != nil {
		return nil, nil
	}
	return probeTokenUsage(response.Usage.Input, response.Usage.Details.Cached)
}

func openAIResponsesProbeUsage(raw []byte) (*int, *int) {
	var response struct {
		Usage struct {
			Input   *int `json:"input_tokens"`
			Details struct {
				Cached *int `json:"cached_tokens"`
			} `json:"input_tokens_details"`
		} `json:"usage"`
	}
	if common.Unmarshal(raw, &response) != nil {
		return nil, nil
	}
	return probeTokenUsage(response.Usage.Input, response.Usage.Details.Cached)
}

func anthropicProbeUsage(raw []byte) (*int, *int) {
	var response struct {
		Usage struct {
			Input    *int `json:"input_tokens"`
			Creation *int `json:"cache_creation_input_tokens"`
			Read     *int `json:"cache_read_input_tokens"`
		} `json:"usage"`
	}
	if common.Unmarshal(raw, &response) != nil || response.Usage.Input == nil || *response.Usage.Input <= 0 {
		return nil, nil
	}
	total := *response.Usage.Input
	for _, count := range []*int{response.Usage.Creation, response.Usage.Read} {
		if count != nil {
			if *count < 0 || *count > math.MaxInt-total {
				return nil, nil
			}
			total += *count
		}
	}
	return &total, response.Usage.Read
}

func geminiProbeUsage(raw []byte) (*int, *int) {
	var response struct {
		Usage struct {
			Input  *int `json:"promptTokenCount"`
			Cached *int `json:"cachedContentTokenCount"`
		} `json:"usageMetadata"`
	}
	if common.Unmarshal(raw, &response) != nil {
		return nil, nil
	}
	return probeTokenUsage(response.Usage.Input, response.Usage.Cached)
}

func probeTokenUsage(input, cached *int) (*int, *int) {
	if input == nil || *input <= 0 {
		return nil, nil
	}
	if cached != nil && (*cached < 0 || *cached > *input) {
		cached = nil
	}
	return input, cached
}

func noProbeAffinity(_ *relaycommon.RelayInfo, _ map[string]string, body []byte) ([]byte, error) {
	return body, nil
}

func applyGrokProbeHeader(info *relaycommon.RelayInfo, headers map[string]string, body []byte) ([]byte, error) {
	if !strings.HasPrefix(strings.ToLower(info.UpstreamModelName), "grok-") {
		return body, nil
	}
	// Hash the exact upstream body with the channel and selected key index.
	digest := sha256.New()
	fmt.Fprintf(digest, "channel-probe/v1/%d/%d/", info.ChannelId, info.ChannelMultiKeyIndex)
	_, _ = digest.Write(body)
	id := digest.Sum(nil)[:16]
	id[6] = (id[6] & 0x0f) | 0x80 // UUID version 8, application-defined hash
	id[8] = (id[8] & 0x3f) | 0x80
	headers[probeConversationHeader] = fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:])
	return body, nil
}

func applyClaudeProbeCache(_ *relaycommon.RelayInfo, _ map[string]string, body []byte) ([]byte, error) {
	var fields probeFields
	if err := common.Unmarshal(body, &fields); err != nil {
		return nil, err
	}
	system := probeField[[]probeFields](fields, "system")
	if len(system) != 1 || probeField[string](system[0], "type") != "text" {
		return nil, errProbeProtocol
	}
	if _, exists := system[0]["cache_control"]; exists {
		// The validator checks any explicit override instead of silently replacing it.
		return body, nil
	}
	system[0]["cache_control"] = json.RawMessage(`{"type":"ephemeral"}`)
	encoded, err := common.Marshal(system)
	if err != nil {
		return nil, err
	}
	fields["system"] = encoded
	return common.Marshal(fields)
}
