package openai

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
)

// isResponsesStreamPreCommitEvent reports events that have no model output or
// tool side effect and may therefore remain private to an upstream attempt.
// Unknown events intentionally return false so protocol extensions fail closed.
func isResponsesStreamPreCommitEvent(eventType string) bool {
	switch eventType {
	case "codex.rate_limits", "codex.response.metadata", "response.created", "response.in_progress":
		return true
	default:
		return false
	}
}

func responsesStreamEventError(event *dto.ResponsesStreamResponse) *types.OpenAIError {
	if event == nil {
		return nil
	}

	if event.Type == "error" && (event.Code != "" || event.Message != "" || event.Param != "") {
		return &types.OpenAIError{Type: "upstream_error", Message: event.Message, Code: event.Code, Param: event.Param}
	}

	switch event.Type {
	case "error", "response.error", "response.failed":
	default:
		return nil
	}

	if event.Response != nil {
		if upstreamError := event.Response.GetOpenAIError(); upstreamError != nil && (upstreamError.Type != "" || upstreamError.Message != "" || upstreamError.Code != nil) {
			return upstreamError
		}
	}

	return &types.OpenAIError{
		Type:    "upstream_error",
		Message: fmt.Sprintf("responses stream terminal event: %s", event.Type),
	}
}

func newResponsesStreamChannelError(eventType string, skipRetry bool) *types.NewAPIError {
	options := make([]types.NewAPIErrorOptions, 0, 1)
	if skipRetry {
		options = append(options, types.ErrOptionWithSkipRetry())
	}

	return types.NewOpenAIError(
		fmt.Errorf("upstream responses stream terminated: event=%s", eventType),
		types.ErrorCodeChannelUpstreamStreamTerminated,
		http.StatusBadGateway,
		options...,
	)
}

func isResponsesStreamBusinessRejection(upstreamError *types.OpenAIError) bool {
	if upstreamError == nil {
		return false
	}
	code := strings.ToLower(fmt.Sprint(upstreamError.Code))
	switch code {
	case "context_length_exceeded", "invalid_request", "invalid_argument",
		"sensitive_words_detected", "prompt_blocked", "content_filter",
		"content_policy_violation", "safety":
		return true
	case "invalid_api_key", "api_key_invalid", "api_key_expired",
		"authentication_error", "unauthenticated", "permission_denied",
		"rate_limit_exceeded", "server_error", "internal_error":
		return false
	}
	if code != "" {
		return false
	}
	switch strings.ToLower(upstreamError.Type) {
	case "invalid_request", "invalid_request_error", "invalid_argument",
		"content_filter", "content_policy_violation", "safety":
		return true
	default:
		return false
	}
}

func responsesStreamPreCommitFallbackReason(err error) string {
	switch {
	case errors.Is(err, errResponsesStreamPreCommitEventLimit):
		return "event_limit"
	case errors.Is(err, errResponsesStreamPreCommitByteLimit):
		return "byte_limit"
	case errors.Is(err, errResponsesStreamPreCommitDiskBudget):
		return "disk_budget"
	default:
		return "storage_error"
	}
}

func isResponsesStreamPreCommitRetryableEnd(status *relaycommon.StreamStatus) bool {
	if status == nil || status.HasErrors() {
		return false
	}
	switch status.EndReason {
	case relaycommon.StreamEndReasonEOF, relaycommon.StreamEndReasonScannerErr:
		return true
	default:
		return false
	}
}

func responsesStreamTerminalEvent(eventType string, streamErr *types.NewAPIError) dto.ResponsesStreamResponse {
	openAIError := streamErr.ToOpenAIError()
	if eventType == "error" {
		return dto.ResponsesStreamResponse{
			Type:    "error",
			Code:    fmt.Sprint(openAIError.Code),
			Message: openAIError.Message,
			Param:   openAIError.Param,
		}
	}

	if eventType != "response.error" && eventType != "response.failed" {
		eventType = "response.failed"
	}
	return dto.ResponsesStreamResponse{
		Type: eventType,
		Response: &dto.OpenAIResponsesResponse{
			Error: openAIError,
		},
	}
}
