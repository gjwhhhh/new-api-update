package model

import (
	"context"
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
)

var ErrCyberEventWindow = errors.New("Too many Cyber events; choose a shorter time range")

type CyberEvent struct {
	CreatedAt   int64               `json:"created_at"`
	RequestID   string              `json:"request_id"`
	ChannelID   int                 `json:"channel_id"`
	TokenID     int                 `json:"token_id"`
	TokenName   string              `json:"token_name"`
	ErrorCode   string              `json:"error_code"`
	Protocol    string              `json:"protocol"`
	Source      string              `json:"source"`
	Action      string              `json:"action"`
	Reason      string              `json:"reason,omitempty"`
	Fingerprint string              `json:"fingerprint,omitempty"`
	Fallback    *CyberFallbackEvent `json:"fallback,omitempty"`
}

const CyberFallbackEventCode = "cyber_fallback_routed"
const CyberFallbackFailedCode = "cyber_fallback_failed"

type CyberFallbackEvent struct {
	BatchID         string `json:"batch_id"`
	Fingerprint     string `json:"session_fingerprint"`
	SourceChannelID int    `json:"source_channel_id"`
	TargetChannelID int    `json:"target_channel_id"`
	ActualChannelID int    `json:"actual_channel_id"`
	Group           string `json:"group"`
	Action          string `json:"action"`
	Outcome         string `json:"outcome"`
	Reason          string `json:"reason"`
	RequestID       string `json:"request_id"`
}

type CyberEventFilter struct {
	Start, End         int64
	TokenID, ChannelID int
	Source, Action     string
	Fingerprint        string
}

// Scope the indexed log read before projecting admin metadata. Bound rows so
// SQLite, MySQL, PostgreSQL and ClickHouse share exact filtering and totals.
func CyberEvents(ctx context.Context, filter CyberEventFilter) ([]CyberEvent, error) {
	const maxRows = 5000
	query := LOG_DB.WithContext(ctx).Model(&Log{}).
		Select("type", "created_at", "request_id", "channel_id", "token_id", "token_name", "error_code", "other").
		Where("created_at >= ? AND created_at <= ?", filter.Start, filter.End).
		Where("(type = ? AND error_code IN ?) OR (type = ? AND error_code = ?)", LogTypeError, cyberEventErrorCodes(), LogTypeSystem, CyberFallbackEventCode)
	if filter.TokenID > 0 {
		query = query.Where("token_id = ?", filter.TokenID)
	}
	if filter.ChannelID > 0 {
		query = query.Where("channel_id = ?", filter.ChannelID)
	}
	order := "created_at desc, id desc"
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		order = clickHouseLogOrder("")
	}
	var logs []Log
	if err := query.Order(order).Limit(maxRows + 1).Find(&logs).Error; err != nil {
		return nil, err
	}
	if len(logs) > maxRows {
		return nil, ErrCyberEventWindow
	}
	items := make([]CyberEvent, 0, len(logs))
	for _, row := range logs {
		var meta struct {
			Protocol string `json:"protocol"`
			Admin    struct {
				Source   string              `json:"cyber_match_source"`
				Fallback *CyberFallbackEvent `json:"cyber_fallback"`
				Guard    struct {
					Action      string `json:"action"`
					Reason      string `json:"reason"`
					Mode        string `json:"mode"`
					Fingerprint string `json:"session_fingerprint"`
					TTL         *int   `json:"configured_ttl_seconds"`
				} `json:"cyber_session_guard"`
			} `json:"admin_info"`
		}

		_ = common.UnmarshalJsonStr(row.Other, &meta)
		action := meta.Admin.Guard.Action
		switch {
		case row.ErrorCode == "cyber_session_blocked":
			action = "blocked"
		case action == "":
			action = "unknown"
		case meta.Admin.Guard.Mode == "shadow" && (action == "armed" || action == "already_active"):
			action = "shadow"
		case action == "armed" && meta.Admin.Guard.TTL != nil && *meta.Admin.Guard.TTL == 0:
			action = "permanent"
		}
		item := CyberEvent{CreatedAt: row.CreatedAt, RequestID: row.RequestId, ChannelID: row.ChannelId, TokenID: row.TokenId, TokenName: row.TokenName, ErrorCode: row.ErrorCode, Protocol: meta.Protocol, Source: meta.Admin.Source, Action: action, Reason: meta.Admin.Guard.Reason, Fingerprint: meta.Admin.Guard.Fingerprint}
		if meta.Admin.Fallback != nil {
			item.Fallback = meta.Admin.Fallback
			item.Action, item.Reason, item.Fingerprint = "reroute_"+item.Fallback.Outcome, item.Fallback.Reason, item.Fallback.Fingerprint
		}
		if (filter.Source == "" || item.Source == filter.Source) && (filter.Action == "" || item.Action == filter.Action) && (filter.Fingerprint == "" || item.Fingerprint == filter.Fingerprint) {
			items = append(items, item)
		}
	}
	return items, nil
}

// A zero-quota system event keeps routing observable when consume logs are off.
func RecordCyberFallbackLog(c *gin.Context, other *LogOther) error {
	group := common.GetContextKeyString(c, constant.ContextKeyAutoGroup)
	if group == "" {
		group = common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	}
	return createLog(&Log{UserId: c.GetInt("id"), Username: c.GetString("username"), CreatedAt: common.GetTimestamp(),
		Type: LogTypeSystem, Content: "Cyber session fallback routing", ChannelId: c.GetInt("channel_id"),
		TokenId: c.GetInt("token_id"), TokenName: c.GetString("token_name"), ModelName: c.GetString("original_model"),
		Group: group, RequestId: c.GetString(common.RequestIdKey), ErrorCode: CyberFallbackEventCode, Other: other.JSONString()})
}

func cyberEventErrorCodes() []string {
	codes := []string{"cyber_policy", "cyber_session_blocked", CyberFallbackFailedCode}
	for _, reason := range []string{"target_rejected", "same_source", "binding_conflict", "stateful_context", "unpaired_tool_result", "context_unavailable", "state_unavailable", "target_unavailable", "model_forbidden", "reconnect_required"} {
		codes = append(codes, "cyber_fallback_"+reason)
	}
	return codes
}
