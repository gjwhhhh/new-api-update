package openai

import (
	"fmt"
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func OaiResponsesHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)

	// read response body
	var responsesResponse dto.OpenAIResponsesResponse
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}
	err = common.Unmarshal(responseBody, &responsesResponse)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if oaiError := responsesResponse.GetOpenAIError(); oaiError != nil && oaiError.Type != "" {
		return nil, types.WithOpenAIError(*oaiError, resp.StatusCode)
	}

	info.ObserveResponseModel(responsesResponse.Model)
	responseBody = rewriteSGLangResponsesCreatedAt(info, responseBody, "created_at", responsesResponse.CreatedAt)

	// 写入新的 response body
	service.IOCopyBytesGracefully(c, resp, responseBody)

	// compute usage
	usage := &dto.Usage{}
	service.ApplyResponsesUsage(usage, responsesResponse.Usage)
	// Count actual tool invocations from Output (not tool declarations).
	for _, output := range responsesResponse.Output {
		switch output.Type {
		case dto.BuildInCallWebSearchCall:
			info.CountBillableToolCall(dto.BuildInCallWebSearchCall, "")
		case dto.BuildInCallFileSearchCall:
			info.CountBillableToolCall(dto.BuildInCallFileSearchCall, "")
		case dto.BuildInCallFunctionCall:
			info.CountBillableToolCall(dto.BuildInCallFunctionCall, output.Name)
		}
	}
	info.ApplyVendorToolUsage(responseBody)

	imageCounter := &relaycommon.ImageGenerationCallCounter{}
	for i := range responsesResponse.Output {
		imageCounter.Observe(&responsesResponse.Output[i], &i)
	}
	imageCounter.Commit(info)

	return usage, nil
}

func OaiResponsesStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		logger.LogError(c, "invalid response or response body")
		return nil, types.NewError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse)
	}

	defer service.CloseResponseBodyGracefully(resp)

	accumulator := service.NewResponsesUsageAccumulator(info)
	var streamErr *types.NewAPIError
	pendingBuffer := newResponsesStreamPreCommitBuffer(responsesStreamPreCommitConfig())
	defer pendingBuffer.Close()
	var downstreamCommitter *helper.StreamCommitter
	hasCommittedEvent := false

	forwardEvent := func(streamResponse dto.ResponsesStreamResponse, data string) error {
		if !hasCommittedEvent {
			if downstreamCommitter == nil {
				return fmt.Errorf("responses stream downstream committer is unavailable")
			}
			helper.ExtendWriteDeadline(c)
			downstreamCommitter.Commit()
			hasCommittedEvent = true
			c.Set(string(constant.ContextKeyStreamResponseStarted), true)
		}
		if streamResponse.Response != nil {
			data = string(rewriteSGLangResponsesCreatedAt(info, []byte(data), "response.created_at", streamResponse.Response.CreatedAt))
		}
		if err := helper.ResponseChunkData(c, streamResponse, data); err != nil {
			return err
		}
		accumulator.Observe(&streamResponse, common.StringToByteSlice(data))
		return nil
	}
	flushPendingEvents := func() error {
		return pendingBuffer.Replay(func(data string) error {
			var pendingResponse dto.ResponsesStreamResponse
			if err := common.UnmarshalJsonStr(data, &pendingResponse); err != nil {
				return fmt.Errorf("unmarshal buffered responses stream event: %w", err)
			}
			return forwardEvent(pendingResponse, data)
		})
	}

	helper.StreamScannerHandlerWithOptions(c, resp, info, helper.StreamScannerOptions{DeferDownstreamStart: true}, func(data string, sr *helper.StreamResult, committer *helper.StreamCommitter) {
		downstreamCommitter = committer
		if streamErr != nil {
			sr.Stop(streamErr)
			return
		}

		var streamResponse dto.ResponsesStreamResponse
		if err := common.UnmarshalJsonStr(data, &streamResponse); err != nil {
			logger.LogError(c, "failed to unmarshal stream response: "+err.Error())
			if !hasCommittedEvent {
				streamErr = newResponsesStreamChannelError("invalid_pre_commit_event", false)
				sr.Stop(streamErr)
				return
			}
			sr.Error(err)
			return
		}
		if upstreamError := responsesStreamEventError(&streamResponse); upstreamError != nil {
			accumulator.Observe(&streamResponse, common.StringToByteSlice(data))
			businessRejection := isResponsesStreamBusinessRejection(upstreamError)
			if businessRejection {
				info.PerformanceBusinessRejection = true
			}
			logger.LogError(c, fmt.Sprintf("responses stream terminal error: event=%s type=%s code=%v committed=%t", streamResponse.Type, upstreamError.Type, upstreamError.Code, hasCommittedEvent))
			streamErr = newResponsesStreamChannelError(
				streamResponse.Type,
				hasCommittedEvent || businessRejection,
			)
			if hasCommittedEvent {
				c.Set(string(constant.ContextKeyStreamTerminalErrorSent), true)
				terminalEvent := responsesStreamTerminalEvent(streamResponse.Type, streamErr)
				terminalData, err := common.Marshal(terminalEvent)
				if err != nil {
					logger.LogError(c, "failed to marshal responses stream terminal error: "+err.Error())
				} else if err := helper.ResponseChunkData(c, terminalEvent, string(terminalData)); err != nil {
					logger.LogError(c, "failed to send responses stream terminal error: "+err.Error())
				}
			} else {
				channelID := 0
				if info != nil && info.ChannelMeta != nil {
					channelID = info.ChannelMeta.ChannelId
				}
				logger.LogInfo(c, fmt.Sprintf("responses stream pre-commit failure: event=%s buffered_events=%d buffered_bytes=%d spilled_bytes=%d from_channel=%d", streamResponse.Type, pendingBuffer.EventCount(), pendingBuffer.Bytes(), pendingBuffer.SpilledBytes(), channelID))
			}
			sr.Stop(streamErr)
			return
		}

		if !hasCommittedEvent && isResponsesStreamPreCommitEvent(streamResponse.Type) {
			if err := pendingBuffer.Append(data); err == nil {
				return
			} else {
				logger.LogInfo(c, fmt.Sprintf("responses stream pre-commit buffer fallback: reason=%s buffered_events=%d buffered_bytes=%d spilled_bytes=%d", responsesStreamPreCommitFallbackReason(err), pendingBuffer.EventCount(), pendingBuffer.Bytes(), pendingBuffer.SpilledBytes()))
				if flushErr := flushPendingEvents(); flushErr != nil {
					streamErr = newResponsesStreamChannelError("pre_commit_buffer_flush_failed", true)
					sr.Stop(streamErr)
					return
				}
			}
		}

		if !hasCommittedEvent && pendingBuffer.EventCount() > 0 {
			if err := flushPendingEvents(); err != nil {
				streamErr = newResponsesStreamChannelError("pre_commit_buffer_flush_failed", true)
				sr.Stop(streamErr)
				return
			}
		}
		if err := forwardEvent(streamResponse, data); err != nil {
			sr.Stop(err)
		}
	})

	common.SetContextKey(c, constant.ContextKeyResponseStreamStatus, info.StreamStatus)
	info.StreamStatus.RequireTerminal()
	if streamErr != nil {
		if hasCommittedEvent {
			return accumulator.Finish(), nil
		}
		return nil, streamErr
	}
	if !hasCommittedEvent && isResponsesStreamPreCommitRetryableEnd(info.StreamStatus) {
		return nil, newResponsesStreamChannelError("stream_"+string(info.StreamStatus.EndReason), false)
	}
	if !hasCommittedEvent && pendingBuffer.EventCount() > 0 && info.StreamStatus.EndReason == relaycommon.StreamEndReasonDone && !info.StreamStatus.HasErrors() {
		if err := flushPendingEvents(); err != nil {
			return nil, newResponsesStreamChannelError("pre_commit_buffer_flush_failed", true)
		}
	}
	return accumulator.Finish(), nil
}

func rewriteSGLangResponsesCreatedAt(info *relaycommon.RelayInfo, payload []byte, path string, createdAt dto.IntValue) []byte {
	if info.GetChannelType() != constant.ChannelTypeSGLang {
		return payload
	}
	if !gjson.GetBytes(payload, path).Exists() {
		return payload
	}
	patched, err := sjson.SetBytes(payload, path, int(createdAt))
	if err != nil {
		return payload
	}
	return patched
}
