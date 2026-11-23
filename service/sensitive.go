package service

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
)

const relaySensitiveMatchKey = "relay_sensitive_match"

// RelaySensitiveMatch is private request evidence. Never attach it to usage logs or client errors.
type RelaySensitiveMatch struct {
	Rule             string `json:"rule"`
	Word             string `json:"word"`
	Source           string `json:"source"`
	Content          string `json:"content,omitempty"`
	Truncated        bool   `json:"truncated,omitempty"`
	ContentTruncated bool   `json:"content_truncated,omitempty"`
}

const relaySensitiveScopeKey = "relay_sensitive_scope"

// Max length for the matched field body kept in private audit evidence.
// Audit persist still applies MaxContentBytes; this bounds in-request memory.
const maxSensitiveMatchContentBytes = 256 * 1024

func RelayPromptSensitiveMatch(c *gin.Context) (RelaySensitiveMatch, bool) {
	value, _ := c.Get(relaySensitiveMatchKey)
	match, ok := value.(RelaySensitiveMatch)
	return match, ok
}

func CheckRelayPromptSensitive(c *gin.Context, request dto.Request, text string, keywords []string, scope string, channelID int) *types.NewAPIError {
	matched, words := AcSearch(strings.ToLower(text), keywords, true)
	if !matched {
		return nil
	}
	match := RelaySensitiveMatch{Rule: "local_prompt_sensitive_words", Word: words[0], Source: "unknown"}
	if responses, ok := request.(*dto.OpenAIResponsesRequest); ok {
		match.Source, match.Content = responsesSensitiveMatch(responses, text, match.Word)
	}
	for i := range match.Word {
		if i >= 120 {
			match.Word, match.Truncated = match.Word[:i], true
			break
		}
	}
	if match.Content != "" && len(match.Content) > maxSensitiveMatchContentBytes {
		match.Content = truncateUTF8Bytes(match.Content, maxSensitiveMatchContentBytes)
		match.ContentTruncated = true
	}
	c.Set(relaySensitiveMatchKey, match)
	c.Set(relaySensitiveScopeKey, scope)
	code := types.ErrorCodeSensitiveWordsDetected
	RequestPolicy(c).AddEvent(PolicyEvent{ChannelID: channelID, ErrorCode: string(code), ErrorSource: "local", Decision: PolicyDecision{Action: "stop", Reason: "local_rejection", Source: "global"}, Health: "unchanged"})
	// Keep the local rule for logs and audit. Clients receive the policy error
	// shape that Codex recognizes: HTTP 400 with code cyber_policy.
	apiErr := types.WithOpenAIError(types.OpenAIError{
		Type: "invalid_request", Code: code,
		Message: "Request rejected by the local sensitive word policy",
	}, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	clientErr := CyberPolicyError(http.StatusBadRequest).ToOpenAIError()
	clientErr.Message = "Request rejected by the local sensitive word policy"
	apiErr.RelayError = clientErr
	return apiErr
}

type sensitiveTextPart struct {
	source string
	text   string
}

// Attribute only when the reconstructed parts exactly match what the detector scanned.
// The budget bounds the extra parsing on rejected requests with large agent histories.
func responsesSensitiveMatch(request *dto.OpenAIResponsesRequest, text, word string) (string, string) {
	const unknown = "unknown"
	if len(request.Input) > 1<<20 || len(text) > 1<<20 {
		return unknown, ""
	}
	parts := responsesSensitiveParts(request)
	if len(parts) == 0 {
		return unknown, ""
	}
	word = strings.ToLower(word)
	if strings.Contains(word, "\n") {
		return unknown, firstMatchingPartContent(parts, word)
	}
	if source, content, ok := attributeResponsesSensitiveParts(parts, text, word); ok {
		return source, content
	}
	return unknown, firstMatchingPartContent(parts, word)
}

func responsesSensitiveParts(request *dto.OpenAIResponsesRequest) []sensitiveTextPart {
	var parts []sensitiveTextPart
	switch common.GetJsonType(request.Input) {
	case "string":
		var input string
		if common.Unmarshal(request.Input, &input) != nil {
			return nil
		}
		parts = append(parts, sensitiveTextPart{"current_user_text", input})
	case "array":
		var items []json.RawMessage
		if common.Unmarshal(request.Input, &items) != nil || len(items) > 128 {
			return nil
		}
		for i, raw := range items {
			var item dto.Input
			if common.Unmarshal(raw, &item) != nil {
				return nil
			}
			source := "input_context"
			switch item.Type {
			case "function_call", "custom_tool_call", "function_call_output", "custom_tool_call_output", "local_shell_call_output":
				source = "tool_context"
			case "message", "":
				if item.Role == "user" && item.Name == "" && i == len(items)-1 {
					source = "current_user_text"
				}
			}
			input, err := common.Marshal([]json.RawMessage{raw})
			if err != nil {
				return nil
			}
			for _, media := range (&dto.OpenAIResponsesRequest{Input: input}).ParseInput() {
				if media.Type != "input_image" && media.Type != "input_file" {
					parts = append(parts, sensitiveTextPart{source, media.Text})
				}
			}
		}
	}
	if len(request.Instructions) > 0 {
		instructions := string(request.Instructions)
		if common.GetJsonType(request.Instructions) == "string" {
			_ = common.Unmarshal(request.Instructions, &instructions)
		}
		parts = append(parts, sensitiveTextPart{"instructions", instructions})
	}
	for _, field := range []struct {
		source string
		value  json.RawMessage
	}{
		{"metadata", request.Metadata}, {"text_settings", request.Text}, {"tool_choice", request.ToolChoice},
		{"prompt_settings", request.Prompt}, {"tool_definitions", request.Tools},
	} {
		if len(field.value) > 0 {
			parts = append(parts, sensitiveTextPart{field.source, string(field.value)})
		}
	}
	return parts
}

func attributeResponsesSensitiveParts(parts []sensitiveTextPart, text, word string) (string, string, bool) {
	remaining := text
	for i, p := range parts {
		var ok bool
		if i > 0 {
			remaining, ok = strings.CutPrefix(remaining, "\n")
			if !ok {
				return "", "", false
			}
		}
		remaining, ok = strings.CutPrefix(remaining, p.text)
		if !ok {
			return "", "", false
		}
	}
	if remaining != "" {
		return "", "", false
	}
	for _, p := range parts {
		if strings.Contains(strings.ToLower(p.text), word) {
			return p.source, p.text, true
		}
	}
	return "", "", false
}

func firstMatchingPartContent(parts []sensitiveTextPart, word string) string {
	for _, p := range parts {
		if strings.Contains(strings.ToLower(p.text), word) {
			return p.text
		}
	}
	return ""
}

func truncateUTF8Bytes(text string, maxBytes int) string {
	if maxBytes <= 0 || len(text) <= maxBytes {
		return text
	}
	truncated := text[:maxBytes]
	for len(truncated) > 0 && !utf8.ValidString(truncated) {
		truncated = truncated[:len(truncated)-1]
	}
	return truncated
}

func CheckSensitiveMessages(messages []dto.Message) ([]string, error) {
	if len(messages) == 0 {
		return nil, nil
	}

	for _, message := range messages {
		arrayContent := message.ParseContent()
		for _, m := range arrayContent {
			if m.Type == "image_url" {
				// TODO: check image url
				continue
			}
			// 检查 text 是否为空
			if m.Text == "" {
				continue
			}
			if ok, words := SensitiveWordContains(m.Text); ok {
				return words, errors.New("sensitive words detected")
			}
		}
	}
	return nil, nil
}

func CheckSensitiveText(text string) (bool, []string) {
	return SensitiveWordContains(text)
}

// SensitiveWordContains 是否包含敏感词，返回是否包含敏感词和敏感词列表
func SensitiveWordContains(text string) (bool, []string) {
	if len(setting.SensitiveWords) == 0 {
		return false, nil
	}
	if len(text) == 0 {
		return false, nil
	}
	checkText := strings.ToLower(text)
	return AcSearch(checkText, setting.SensitiveWords, true)
}

// SensitiveWordReplace 敏感词替换，返回是否包含敏感词和替换后的文本
func SensitiveWordReplace(text string, returnImmediately bool) (bool, []string, string) {
	if len(setting.SensitiveWords) == 0 {
		return false, nil, text
	}
	checkText := strings.ToLower(text)
	m := getOrBuildAC(setting.SensitiveWords)
	hits := m.MultiPatternSearch([]rune(checkText), returnImmediately)
	if len(hits) > 0 {
		words := make([]string, 0, len(hits))
		var builder strings.Builder
		builder.Grow(len(text))
		lastPos := 0

		for _, hit := range hits {
			pos := hit.Pos
			word := string(hit.Word)
			builder.WriteString(text[lastPos:pos])
			builder.WriteString("**###**")
			lastPos = pos + len(word)
			words = append(words, word)
		}
		builder.WriteString(text[lastPos:])
		return true, words, builder.String()
	}
	return false, nil, text
}
