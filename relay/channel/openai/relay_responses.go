package openai

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
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

	if responsesResponse.HasImageGenerationCall() {
		c.Set("image_generation_call", true)
		c.Set("image_generation_call_quality", responsesResponse.GetQuality())
		c.Set("image_generation_call_size", responsesResponse.GetSize())
	}

	// 写入新的 response body
	service.IOCopyBytesGracefully(c, resp, responseBody)

	// compute usage
	usage := dto.Usage{}
	if responsesResponse.Usage != nil {
		usage.PromptTokens = responsesResponse.Usage.InputTokens
		usage.CompletionTokens = responsesResponse.Usage.OutputTokens
		usage.TotalTokens = responsesResponse.Usage.TotalTokens
		if responsesResponse.Usage.InputTokensDetails != nil {
			usage.PromptTokensDetails.CachedTokens = responsesResponse.Usage.InputTokensDetails.CachedTokens
			usage.PromptTokensDetails.CacheWriteTokens = responsesResponse.Usage.InputTokensDetails.CacheWriteTokens
		}
	}
	if info == nil || info.ResponsesUsageInfo == nil || info.ResponsesUsageInfo.BuiltInTools == nil {
		return &usage, nil
	}
	// 解析 Tools 用量
	for _, tool := range responsesResponse.Tools {
		buildToolinfo, ok := info.ResponsesUsageInfo.BuiltInTools[common.Interface2String(tool["type"])]
		if !ok || buildToolinfo == nil {
			logger.LogError(c, fmt.Sprintf("BuiltInTools not found for tool type: %v", tool["type"]))
			continue
		}
		buildToolinfo.CallCount++
	}
	return &usage, nil
}

func OaiResponsesStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		logger.LogError(c, "invalid response or response body")
		return nil, types.NewError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse)
	}

	defer service.CloseResponseBodyGracefully(resp)

	var usage = &dto.Usage{}
	var responseTextBuilder strings.Builder
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
			// Once writing the first event begins, a downstream response may already
			// be visible even if FlushWriter later reports an error. Treat it as
			// committed so neither retry nor a trailing JSON body can corrupt SSE.
			hasCommittedEvent = true
			c.Set(string(constant.ContextKeyStreamResponseStarted), true)
		}
		if err := helper.ResponseChunkData(c, streamResponse, data); err != nil {
			return err
		}
		switch streamResponse.Type {
		case "response.completed":
			if streamResponse.Response != nil {
				if streamResponse.Response.Usage != nil {
					if streamResponse.Response.Usage.InputTokens != 0 {
						usage.PromptTokens = streamResponse.Response.Usage.InputTokens
					}
					if streamResponse.Response.Usage.OutputTokens != 0 {
						usage.CompletionTokens = streamResponse.Response.Usage.OutputTokens
					}
					if streamResponse.Response.Usage.TotalTokens != 0 {
						usage.TotalTokens = streamResponse.Response.Usage.TotalTokens
					}
					if streamResponse.Response.Usage.InputTokensDetails != nil {
						usage.PromptTokensDetails.CachedTokens = streamResponse.Response.Usage.InputTokensDetails.CachedTokens
						usage.PromptTokensDetails.CacheWriteTokens = streamResponse.Response.Usage.InputTokensDetails.CacheWriteTokens
					}
				}
				if streamResponse.Response.HasImageGenerationCall() {
					c.Set("image_generation_call", true)
					c.Set("image_generation_call_quality", streamResponse.Response.GetQuality())
					c.Set("image_generation_call_size", streamResponse.Response.GetSize())
				}
			}
		case "response.output_text.delta":
			responseTextBuilder.WriteString(streamResponse.Delta)
		case dto.ResponsesOutputTypeItemDone:
			if streamResponse.Item != nil {
				switch streamResponse.Item.Type {
				case dto.BuildInCallWebSearchCall:
					if info != nil && info.ResponsesUsageInfo != nil && info.ResponsesUsageInfo.BuiltInTools != nil {
						if webSearchTool, exists := info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview]; exists && webSearchTool != nil {
							webSearchTool.CallCount++
						}
					}
				}
			}
		}
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

		// 检查当前数据是否包含 completed 状态和 usage 信息
		var streamResponse dto.ResponsesStreamResponse
		if err := common.UnmarshalJsonStr(data, &streamResponse); err != nil {
			logger.LogError(c, "failed to unmarshal stream response: "+err.Error())
			if !hasCommittedEvent {
				// The malformed frame has not reached the client, so it is safe to
				// discard the private prelude and retry a different upstream channel.
				streamErr = newResponsesStreamChannelError("invalid_pre_commit_event", false)
				sr.Stop(streamErr)
				return
			}
			sr.Error(err)
			return
		}
		if upstreamError := responsesStreamEventError(&streamResponse); upstreamError != nil {
			logger.LogError(c, fmt.Sprintf("responses stream terminal error: event=%s type=%s code=%v committed=%t", streamResponse.Type, upstreamError.Type, upstreamError.Code, hasCommittedEvent))
			streamErr = newResponsesStreamChannelError(streamResponse.Type, hasCommittedEvent)

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
					logger.LogError(c, "failed to flush responses stream pre-commit buffer: "+flushErr.Error())
					streamErr = newResponsesStreamChannelError("pre_commit_buffer_flush_failed", true)
					sr.Stop(streamErr)
					return
				}
			}
		}

		if !hasCommittedEvent && pendingBuffer.EventCount() > 0 {
			if err := flushPendingEvents(); err != nil {
				logger.LogError(c, "failed to send buffered responses stream data: "+err.Error())
				sr.Stop(err)
				return
			}
		}

		if err := forwardEvent(streamResponse, data); err != nil {
			logger.LogError(c, "failed to send responses stream data: "+err.Error())
			sr.Stop(err)
			return
		}
	})
	if streamErr != nil {
		return nil, streamErr
	}
	if !hasCommittedEvent && isResponsesStreamPreCommitRetryableEnd(info.StreamStatus) {
		channelID := 0
		if info != nil && info.ChannelMeta != nil {
			channelID = info.ChannelMeta.ChannelId
		}
		logger.LogInfo(c, fmt.Sprintf("responses stream pre-commit transport failure: reason=%s buffered_events=%d buffered_bytes=%d spilled_bytes=%d from_channel=%d", info.StreamStatus.EndReason, pendingBuffer.EventCount(), pendingBuffer.Bytes(), pendingBuffer.SpilledBytes(), channelID))
		return nil, newResponsesStreamChannelError("stream_"+string(info.StreamStatus.EndReason), false)
	}
	if !hasCommittedEvent && pendingBuffer.EventCount() > 0 && info.StreamStatus.EndReason == relaycommon.StreamEndReasonDone && !info.StreamStatus.HasErrors() {
		if err := flushPendingEvents(); err != nil {
			logger.LogError(c, "failed to send completed responses stream pre-commit data: "+err.Error())
			return nil, newResponsesStreamChannelError("pre_commit_buffer_flush_failed", true)
		}
	}

	if usage.CompletionTokens == 0 {
		// 计算输出文本的 token 数量
		tempStr := responseTextBuilder.String()
		if len(tempStr) > 0 {
			// 非正常结束，使用输出文本的 token 数量
			completionTokens := service.CountTextToken(tempStr, info.UpstreamModelName)
			usage.CompletionTokens = completionTokens
		}
	}

	if usage.PromptTokens == 0 && usage.CompletionTokens != 0 {
		usage.PromptTokens = info.GetEstimatePromptTokens()
	}

	usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens

	return usage, streamErr
}
