package openai

import (
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/types"
)

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
