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
	var pendingEvents []pendingResponsesStreamEvent
	pendingBytes := 0
	hasCommittedEvent := false

	forwardEvent := func(streamResponse dto.ResponsesStreamResponse, data string) error {
		if err := helper.ResponseChunkData(c, streamResponse, data); err != nil {
			return err
		}
		hasCommittedEvent = true
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

	helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {
		if streamErr != nil {
			sr.Stop(streamErr)
			return
		}

		// 检查当前数据是否包含 completed 状态和 usage 信息
		var streamResponse dto.ResponsesStreamResponse
		if err := common.UnmarshalJsonStr(data, &streamResponse); err != nil {
			logger.LogError(c, "failed to unmarshal stream response: "+err.Error())
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
				logger.LogInfo(c, fmt.Sprintf("responses stream pre-commit failure: event=%s buffered_events=%d buffered_bytes=%d from_channel=%d", streamResponse.Type, len(pendingEvents), pendingBytes, channelID))
			}

			sr.Stop(streamErr)
			return
		}

		if !hasCommittedEvent && isResponsesStreamPreCommitEvent(streamResponse.Type) && len(pendingEvents) < responsesStreamPreCommitMaxEvents && pendingBytes+len(data) <= responsesStreamPreCommitMaxBytes {
			pendingEvents = append(pendingEvents, pendingResponsesStreamEvent{response: streamResponse, data: data})
			pendingBytes += len(data)
			return
		}

		if !hasCommittedEvent && len(pendingEvents) > 0 {
			if len(pendingEvents) >= responsesStreamPreCommitMaxEvents || pendingBytes+len(data) > responsesStreamPreCommitMaxBytes {
				logger.LogInfo(c, fmt.Sprintf("responses stream pre-commit buffer limit reached: events=%d bytes=%d", len(pendingEvents), pendingBytes))
			}
			for _, pendingEvent := range pendingEvents {
				if err := forwardEvent(pendingEvent.response, pendingEvent.data); err != nil {
					logger.LogError(c, "failed to send buffered responses stream data: "+err.Error())
					sr.Stop(err)
					return
				}
			}
			pendingEvents = nil
			pendingBytes = 0
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
	if !hasCommittedEvent && len(pendingEvents) > 0 && info.StreamStatus.IsNormalEnd() && !info.StreamStatus.HasErrors() {
		for _, pendingEvent := range pendingEvents {
			if err := forwardEvent(pendingEvent.response, pendingEvent.data); err != nil {
				logger.LogError(c, "failed to send completed responses stream pre-commit data: "+err.Error())
				break
			}
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
