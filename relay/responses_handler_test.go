package relay

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/stretchr/testify/assert"
)

func identityOpenAIResponsesInfo() *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeResponses,
		ChannelMeta: &relaycommon.ChannelMeta{
			ApiType: constant.APITypeOpenAI,
		},
	}
}

func TestCanPassThroughOpenAIResponsesBody(t *testing.T) {
	identityReq := &dto.OpenAIResponsesRequest{Model: "gpt-5"}

	tests := []struct {
		name    string
		info    *relaycommon.RelayInfo
		request *dto.OpenAIResponsesRequest
		want    bool
	}{
		{
			name:    "identity openai",
			info:    identityOpenAIResponsesInfo(),
			request: identityReq,
			want:    true,
		},
		{
			name: "identity codex",
			info: &relaycommon.RelayInfo{
				RelayMode: relayconstant.RelayModeResponses,
				ChannelMeta: &relaycommon.ChannelMeta{
					ApiType: constant.APITypeCodex,
				},
			},
			request: identityReq,
			want:    true,
		},
		{
			name: "compact mode",
			info: &relaycommon.RelayInfo{
				RelayMode: relayconstant.RelayModeResponsesCompact,
				ChannelMeta: &relaycommon.ChannelMeta{
					ApiType: constant.APITypeOpenAI,
				},
			},
			request: identityReq,
			want:    false,
		},
		{
			name: "non openai api type",
			info: &relaycommon.RelayInfo{
				RelayMode: relayconstant.RelayModeResponses,
				ChannelMeta: &relaycommon.ChannelMeta{
					ApiType: constant.APITypeAnthropic,
				},
			},
			request: identityReq,
			want:    false,
		},
		{
			name: "model mapped",
			info: &relaycommon.RelayInfo{
				RelayMode: relayconstant.RelayModeResponses,
				ChannelMeta: &relaycommon.ChannelMeta{
					ApiType:       constant.APITypeOpenAI,
					IsModelMapped: true,
				},
			},
			request: identityReq,
			want:    false,
		},
		{
			name: "param override",
			info: &relaycommon.RelayInfo{
				RelayMode: relayconstant.RelayModeResponses,
				ChannelMeta: &relaycommon.ChannelMeta{
					ApiType:       constant.APITypeOpenAI,
					ParamOverride: map[string]interface{}{"temperature": 0.2},
				},
			},
			request: identityReq,
			want:    false,
		},
		{
			name:    "reasoning effort model suffix",
			info:    identityOpenAIResponsesInfo(),
			request: &dto.OpenAIResponsesRequest{Model: "gpt-5-high"},
			want:    false,
		},
		{
			name:    "service tier would be stripped",
			info:    identityOpenAIResponsesInfo(),
			request: &dto.OpenAIResponsesRequest{Model: "gpt-5", ServiceTier: "priority"},
			want:    false,
		},
		{
			name: "service tier allowed",
			info: &relaycommon.RelayInfo{
				RelayMode: relayconstant.RelayModeResponses,
				ChannelMeta: &relaycommon.ChannelMeta{
					ApiType: constant.APITypeOpenAI,
					ChannelOtherSettings: dto.ChannelOtherSettings{
						AllowServiceTier: true,
					},
				},
			},
			request: &dto.OpenAIResponsesRequest{Model: "gpt-5", ServiceTier: "priority"},
			want:    true,
		},
		{
			name: "store disabled",
			info: &relaycommon.RelayInfo{
				RelayMode: relayconstant.RelayModeResponses,
				ChannelMeta: &relaycommon.ChannelMeta{
					ApiType: constant.APITypeOpenAI,
					ChannelOtherSettings: dto.ChannelOtherSettings{
						DisableStore: true,
					},
				},
			},
			request: &dto.OpenAIResponsesRequest{Model: "gpt-5", Store: json.RawMessage(`true`)},
			want:    false,
		},
		{
			name:    "safety identifier would be stripped",
			info:    identityOpenAIResponsesInfo(),
			request: &dto.OpenAIResponsesRequest{Model: "gpt-5", SafetyIdentifier: json.RawMessage(`"abc"`)},
			want:    false,
		},
		{
			name:    "include obfuscation would be stripped",
			info:    identityOpenAIResponsesInfo(),
			request: &dto.OpenAIResponsesRequest{Model: "gpt-5", StreamOptions: &dto.StreamOptions{IncludeObfuscation: true}},
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, canPassThroughOpenAIResponsesBody(tt.info, tt.request))
		})
	}
}
