package service

import (
	"context"
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
)

const (
	ChannelTestStatusSucceeded = "succeeded"
	ChannelTestStatusFailed    = "failed"
	ChannelTestStatusCancelled = "cancelled"

	ChannelTestFailureKindNone                 = "none"
	ChannelTestFailureKindUnsupported          = "unsupported"
	ChannelTestFailureKindSetup                = "setup"
	ChannelTestFailureKindTransport            = "transport"
	ChannelTestFailureKindUpstreamHTTP         = "upstream_http"
	ChannelTestFailureKindStreamTerminated     = "stream_terminated"
	ChannelTestFailureKindResponseDecode       = "response_decode"
	ChannelTestFailureKindResponseInvalid      = "response_invalid"
	ChannelTestFailureKindTimeout              = "timeout"
	ChannelTestFailureKindResponseTimeExceeded = "response_time_exceeded"
	ChannelTestFailureKindCancelled            = "cancelled"
	ChannelTestFailureKindUnknown              = "unknown"

	ChannelTestActionNone             = "none"
	ChannelTestActionChannelDisabled  = "channel_disabled"
	ChannelTestActionChannelEnabled   = "channel_enabled"
	ChannelTestActionKeyDisabled      = "key_disabled"
	ChannelTestActionKeyEnabled       = "key_enabled"
	ChannelTestActionStaleIgnored     = "stale_ignored"
	ChannelTestActionSkippedCancelled = "skipped_cancelled"
	ChannelTestActionFailed           = "action_failed"
)

type ChannelHealthTransitionInput struct {
	Channel       *model.Channel
	KeySelection  *model.ChannelKeySelection
	Success       bool
	ShouldDisable bool
	AllowDisable  bool
	Reason        string
	ContextErr    error
}

func ApplyChannelHealthTransition(input ChannelHealthTransitionInput) string {
	if input.ContextErr != nil {
		return ChannelTestActionSkippedCancelled
	}
	if input.Channel == nil {
		return ChannelTestActionNone
	}
	shouldDisable := input.AllowDisable && input.ShouldDisable && input.Channel.GetAutoBan()
	shouldEnable := common.AutomaticEnableChannelEnabled

	var result model.ChannelHealthCheckResult
	var err error
	if input.KeySelection != nil {
		result, err = model.ApplyMultiKeyHealthCheckResult(
			input.Channel.Id,
			*input.KeySelection,
			input.Success,
			shouldDisable,
			shouldEnable,
			input.Reason,
		)
	} else {
		result, err = model.ApplySingleKeyHealthCheckResult(
			input.Channel.Id,
			input.Channel.Status,
			input.Success,
			shouldDisable,
			shouldEnable,
			input.Reason,
		)
	}
	if err != nil {
		common.SysError("failed to apply channel health transition: " + err.Error())
		return ChannelTestActionFailed
	}
	if result.Stale {
		return ChannelTestActionStaleIgnored
	}
	if result.Disabled {
		if input.KeySelection != nil {
			return ChannelTestActionKeyDisabled
		}
		subject := "通道「" + input.Channel.Name + "」已被禁用"
		NotifyRootUser(formatNotifyType(input.Channel.Id, common.ChannelStatusAutoDisabled), subject, subject+"，原因："+input.Reason)
		return ChannelTestActionChannelDisabled
	}
	if result.Enabled {
		if input.KeySelection != nil {
			return ChannelTestActionKeyEnabled
		}
		subject := "通道「" + input.Channel.Name + "」已被启用"
		NotifyRootUser(formatNotifyType(input.Channel.Id, common.ChannelStatusEnabled), subject, subject)
		return ChannelTestActionChannelEnabled
	}
	return ChannelTestActionNone
}

func RecordChannelTestResult(result *model.ChannelTestResult) (bool, error) {
	if result == nil || !operation_setting.GetChannelTestHistorySetting().Enabled {
		return false, nil
	}
	if err := model.CreateChannelTestResult(result); err != nil {
		return false, err
	}
	return true, nil
}

func ChannelTestFailureKind(localErr error, apiErr *types.NewAPIError, upstreamStatus int) string {
	if localErr == nil && apiErr == nil {
		return ChannelTestFailureKindNone
	}
	if errors.Is(localErr, context.Canceled) || errors.Is(localErr, context.DeadlineExceeded) {
		if errors.Is(localErr, context.DeadlineExceeded) {
			return ChannelTestFailureKindTimeout
		}
		return ChannelTestFailureKindCancelled
	}
	if apiErr != nil {
		switch apiErr.GetErrorCode() {
		case types.ErrorCodeChannelUpstreamStreamTerminated:
			return ChannelTestFailureKindStreamTerminated
		case types.ErrorCodeDoRequestFailed:
			return ChannelTestFailureKindTransport
		case types.ErrorCodeReadResponseBodyFailed:
			return ChannelTestFailureKindResponseDecode
		case types.ErrorCodeBadResponseBody, types.ErrorCodeEmptyResponse:
			return ChannelTestFailureKindResponseInvalid
		case types.ErrorCodeChannelResponseTimeExceeded:
			return ChannelTestFailureKindResponseTimeExceeded
		case types.ErrorCodeGenRelayInfoFailed, types.ErrorCodeConvertRequestFailed,
			types.ErrorCodeInvalidApiType, types.ErrorCodeJsonMarshalFailed,
			types.ErrorCodeChannelModelMappedError, types.ErrorCodeChannelParamOverrideInvalid:
			return ChannelTestFailureKindSetup
		}
		if apiErr.StatusCode == http.StatusRequestTimeout || apiErr.StatusCode == http.StatusGatewayTimeout {
			return ChannelTestFailureKindTimeout
		}
	}
	if upstreamStatus > 0 {
		return ChannelTestFailureKindUpstreamHTTP
	}
	return ChannelTestFailureKindUnknown
}
