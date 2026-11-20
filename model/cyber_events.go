package model

import (
	"context"
	"errors"

	"github.com/QuantumNous/new-api/common"
)

var ErrCyberEventWindow = errors.New("Too many Cyber events; choose a shorter time range")

type CyberEvent struct {
	CreatedAt   int64  `json:"created_at"`
	RequestID   string `json:"request_id"`
	ChannelID   int    `json:"channel_id"`
	TokenID     int    `json:"token_id"`
	TokenName   string `json:"token_name"`
	ErrorCode   string `json:"error_code"`
	Protocol    string `json:"protocol"`
	Source      string `json:"source"`
	Action      string `json:"action"`
	Reason      string `json:"reason,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
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
		Select("created_at", "request_id", "channel_id", "token_id", "token_name", "error_code", "other").
		Where("type = ? AND created_at >= ? AND created_at <= ? AND error_code IN ?", LogTypeError, filter.Start, filter.End, []string{"cyber_policy", "cyber_session_blocked"})
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
				Source string `json:"cyber_match_source"`
				Guard  struct {
					Action      string `json:"action"`
					Reason      string `json:"reason"`
					Mode        string `json:"mode"`
					Fingerprint string `json:"session_fingerprint"`
					TTL         *int   `json:"configured_ttl_seconds"`
				} `json:"cyber_session_guard"`
			} `json:"admin_info"`
		}
		if row.ErrorCode != "cyber_policy" && row.ErrorCode != "cyber_session_blocked" {
			continue
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
		if (filter.Source == "" || item.Source == filter.Source) && (filter.Action == "" || item.Action == filter.Action) && (filter.Fingerprint == "" || item.Fingerprint == filter.Fingerprint) {
			items = append(items, item)
		}
	}
	return items, nil
}
