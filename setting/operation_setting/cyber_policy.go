package operation_setting

import (
	"fmt"
	"slices"
)

const CyberPolicyOptionKey = "CyberSessionPolicy"

type CyberPolicy struct {
	Mode         string `json:"mode"`
	Scope        string `json:"scope"`
	TokenIDs     []int  `json:"token_ids"`
	ChannelIDs   []int  `json:"channel_ids"`
	TTLSeconds   int    `json:"ttl_seconds"`
	MatchMessage bool   `json:"match_message"`
}

func (p CyberPolicy) Validate() error {
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
	return nil
}
