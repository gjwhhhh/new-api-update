package openai

import (
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/types"
)

const (
	responsesStreamPreCommitMaxEvents = 8
	responsesStreamPreCommitMaxBytes  = 64 * 1024
)

type pendingResponsesStreamEvent struct {
	response dto.ResponsesStreamResponse
	data     string
}

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

	if event.Error != nil && (event.Error.Type != "" || event.Error.Message != "" || event.Error.Code != nil) {
		return event.Error
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

func responsesStreamTerminalEvent(eventType string, streamErr *types.NewAPIError) dto.ResponsesStreamResponse {
	openAIError := streamErr.ToOpenAIError()
	if eventType == "error" {
		return dto.ResponsesStreamResponse{
			Type:  "error",
			Error: &openAIError,
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
