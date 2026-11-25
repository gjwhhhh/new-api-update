package operation_setting

import (
	"fmt"
	"net/url"
	"slices"
	"strings"
)

const CyberPolicyOptionKey = "CyberSessionPolicy"

const maxCyberNotifyWebhookSecretLen = 256

type CyberPolicy struct {
	CaptureFullPayload  *bool  `json:"capture_full_payload"`
	Mode                string `json:"mode"`
	Scope               string `json:"scope"`
	TokenIDs            []int  `json:"token_ids"`
	ChannelIDs          []int  `json:"channel_ids"`
	TTLSeconds          int    `json:"ttl_seconds"`
	MatchMessage        bool   `json:"match_message"`
	BlockedAction       string `json:"blocked_action"`
	FallbackChannelID   int    `json:"fallback_channel_id"`
	NotifyWebhookURL    string `json:"notify_webhook_url"`
	NotifyWebhookSecret string `json:"notify_webhook_secret"`
}

// CyberPolicyAPI is the admin API view of CyberPolicy. The webhook secret is never returned.
type CyberPolicyAPI struct {
	CyberPolicy
	NotifyWebhookSecretConfigured bool `json:"notify_webhook_secret_configured"`
}

// ForAPI redacts the webhook secret and reports whether one is stored.
func (p CyberPolicy) ForAPI() CyberPolicyAPI {
	configured := p.NotifyWebhookSecret != ""
	p.NotifyWebhookSecret = ""
	return CyberPolicyAPI{CyberPolicy: p, NotifyWebhookSecretConfigured: configured}
}

// Normalize fills defaults for policies saved before newer options existed.
func (p *CyberPolicy) Normalize() {
	if p.CaptureFullPayload == nil {
		enabled := true
		p.CaptureFullPayload = &enabled
	}
	if p.BlockedAction == "" {
		p.BlockedAction = "block"
	}
	if p.BlockedAction == "block" {
		p.FallbackChannelID = 0
	}
	p.NotifyWebhookURL = strings.TrimSpace(p.NotifyWebhookURL)
	p.NotifyWebhookSecret = strings.TrimSpace(p.NotifyWebhookSecret)
}

func (p CyberPolicy) Validate() error {
	p.Normalize()
	if p.BlockedAction != "block" && p.BlockedAction != "reroute" {
		return fmt.Errorf("invalid Cyber blocked action")
	}
	if p.BlockedAction == "reroute" && p.FallbackChannelID < 1 {
		return fmt.Errorf("a positive fallback channel ID is required")
	}
	if p.Mode != "off" && p.Mode != "shadow" && p.Mode != "enforce" {
		return fmt.Errorf("invalid Cyber policy mode")
	}
	switch p.Scope {
	case "all":
	case "token":
		if p.Mode != "off" && len(p.TokenIDs) == 0 {
			return fmt.Errorf("at least one token ID is required")
		}
	case "channel":
		if p.Mode != "off" && len(p.ChannelIDs) == 0 {
			return fmt.Errorf("at least one channel ID is required")
		}
	default:
		return fmt.Errorf("invalid Cyber policy scope")
	}
	for _, ids := range [][]int{p.TokenIDs, p.ChannelIDs} {
		if len(ids) > 1024 || slices.ContainsFunc(ids, func(id int) bool { return id < 1 }) {
			return fmt.Errorf("Cyber policy IDs must be positive and limited to 1024 per list")
		}
	}
	if (p.TTLSeconds == 0 && p.Mode == "shadow") || (p.TTLSeconds != 0 && (p.TTLSeconds < 30 || p.TTLSeconds > 3600)) {
		return fmt.Errorf("Cyber policy requires a TTL of 30–3600 seconds, or 0 for permanent enforcement")
	}
	if p.NotifyWebhookURL != "" {
		if _, err := url.ParseRequestURI(p.NotifyWebhookURL); err != nil {
			return fmt.Errorf("invalid Cyber notify webhook URL")
		}
	}
	if len(p.NotifyWebhookSecret) > maxCyberNotifyWebhookSecretLen {
		return fmt.Errorf("Cyber notify webhook secret must be at most %d bytes", maxCyberNotifyWebhookSecretLen)
	}
	return nil
}
