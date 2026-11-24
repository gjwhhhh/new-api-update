package model

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/promptsim"
)

const MaxPromptFilterTargets = 100

const (
	PromptRejectModeSensitive = "sensitive"
	PromptRejectModeCyber     = "cyber"
)

// PromptTextFilterPolicy is independent of channel routing and health policy.
type PromptTextFilterPolicy struct {
	Enabled             bool
	Keywords            []string
	Target              PromptTextFilterTarget
	SimilarityEnabled   bool
	SimilarityThreshold float64
	SimilarityTemplates []promptsim.Template
	RejectMode          string
	BanSession          bool
	Exempt              PromptTextFilterExempt
}

func (p *PromptTextFilterPolicy) ShouldScan() bool {
	if p == nil || !p.Enabled {
		return false
	}
	if len(p.Keywords) > 0 {
		return true
	}
	return p.SimilarityEnabled && len(p.SimilarityTemplates) > 0
}

func (p *PromptTextFilterPolicy) NormalizedRejectMode() string {
	if p == nil {
		return PromptRejectModeCyber
	}
	switch p.RejectMode {
	case PromptRejectModeSensitive, PromptRejectModeCyber:
		return p.RejectMode
	default:
		return PromptRejectModeCyber
	}
}

// Exempted reports whether the request should skip local prompt filtering.
// Channel and token exemptions use OR matching.
func (p *PromptTextFilterPolicy) Exempted(channelID, tokenID int) bool {
	if p == nil {
		return false
	}
	return p.Exempt.Matches(channelID, tokenID)
}

var promptTextFilterPolicy atomic.Pointer[PromptTextFilterPolicy]
var defaultPromptTextFilterPolicy = func() *PromptTextFilterPolicy {
	policy, err := buildPromptTextFilterPolicy(nil)
	if err != nil {
		panic(err)
	}
	return policy
}()

func CurrentPromptTextFilterPolicy() *PromptTextFilterPolicy {
	if policy := promptTextFilterPolicy.Load(); policy != nil {
		return policy
	}
	return defaultPromptTextFilterPolicy
}

func isPromptTextFilterOption(key string) bool {
	switch key {
	case "CheckSensitiveEnabled", "CheckSensitiveOnPromptEnabled", "SensitiveWords", "PromptTextFilterTarget",
		"PromptTextFilterExempt",
		"PromptSimilarityEnabled", "PromptSimilarityThreshold", "PromptSimilarityTemplates",
		"PromptRejectMode", "PromptRejectBanSession":
		return true
	default:
		return false
	}
}

func buildPromptTextFilterPolicy(options map[string]string) (*PromptTextFilterPolicy, error) {
	policy := &PromptTextFilterPolicy{}
	value := func(key string) string {
		if value, ok := options[key]; ok {
			return value
		}
		return requestPolicyDefaults[key]
	}
	for _, key := range []string{"CheckSensitiveEnabled", "CheckSensitiveOnPromptEnabled", "PromptSimilarityEnabled"} {
		if _, err := strconv.ParseBool(value(key)); err != nil {
			return nil, fmt.Errorf("invalid boolean: %s", key)
		}
	}
	policy.Enabled = value("CheckSensitiveEnabled") == "true" && value("CheckSensitiveOnPromptEnabled") == "true"
	policy.SimilarityEnabled = value("PromptSimilarityEnabled") == "true"
	banSession, err := strconv.ParseBool(value("PromptRejectBanSession"))
	if err != nil {
		return nil, fmt.Errorf("invalid boolean: PromptRejectBanSession")
	}
	policy.BanSession = banSession
	if err := common.UnmarshalJsonStr(value("PromptTextFilterTarget"), &policy.Target); err != nil {
		return nil, fmt.Errorf("invalid prompt text filter target: %w", err)
	}
	if err := policy.Target.validate(); err != nil {
		return nil, err
	}
	for word := range strings.SplitSeq(value("SensitiveWords"), "\n") {
		if word = strings.TrimSpace(word); word != "" {
			policy.Keywords = append(policy.Keywords, word)
		}
	}
	threshold, err := strconv.ParseFloat(value("PromptSimilarityThreshold"), 64)
	if err != nil || math.IsNaN(threshold) || threshold < 0.60 || threshold > 0.95 {
		return nil, fmt.Errorf("invalid prompt similarity threshold")
	}
	policy.SimilarityThreshold = threshold
	templates, err := promptsim.ParseTemplates(value("PromptSimilarityTemplates"))
	if err != nil {
		return nil, err
	}
	policy.SimilarityTemplates = promptsim.Compile(templates, promptsim.DefaultNGram)
	mode := value("PromptRejectMode")
	switch mode {
	case PromptRejectModeSensitive, PromptRejectModeCyber:
		policy.RejectMode = mode
	default:
		return nil, fmt.Errorf("invalid prompt reject mode")
	}
	if err := common.UnmarshalJsonStr(value("PromptTextFilterExempt"), &policy.Exempt); err != nil {
		return nil, fmt.Errorf("invalid prompt text filter exempt list: %w", err)
	}
	if err := policy.Exempt.validate(); err != nil {
		return nil, err
	}
	return policy, nil
}

// PromptTextFilterExempt is an OR whitelist: matching channel or token skips filtering.
type PromptTextFilterExempt struct {
	ChannelIDs []int `json:"channel_ids,omitempty"`
	TokenIDs   []int `json:"token_ids,omitempty"`
}

func (exempt PromptTextFilterExempt) validate() error {
	for _, ids := range [][]int{exempt.ChannelIDs, exempt.TokenIDs} {
		if len(ids) > MaxPromptFilterTargets || slices.ContainsFunc(ids, func(id int) bool { return id <= 0 }) {
			return fmt.Errorf("prompt text filter exempt IDs must be positive and number at most %d per dimension", MaxPromptFilterTargets)
		}
	}
	return nil
}

func (exempt PromptTextFilterExempt) Matches(channelID, tokenID int) bool {
	if channelID > 0 && slices.Contains(exempt.ChannelIDs, channelID) {
		return true
	}
	if tokenID > 0 && slices.Contains(exempt.TokenIDs, tokenID) {
		return true
	}
	return false
}

type PromptTextFilterTarget struct {
	Scope      string `json:"scope"`
	ChannelIDs []int  `json:"channel_ids,omitempty"`
	TokenIDs   []int  `json:"token_ids,omitempty"`
}

func (target PromptTextFilterTarget) validate() error {
	var channels, tokens bool
	switch target.Scope {
	case "all":
	case "channel":
		channels = true
	case "token":
		tokens = true
	case "channel_token":
		channels, tokens = true, true
	default:
		return fmt.Errorf("invalid prompt text filter scope")
	}
	if channels != (len(target.ChannelIDs) > 0) || tokens != (len(target.TokenIDs) > 0) {
		return fmt.Errorf("select at least one ID for each dimension in the prompt text filter scope")
	}
	for _, ids := range [][]int{target.ChannelIDs, target.TokenIDs} {
		if len(ids) > MaxPromptFilterTargets || slices.ContainsFunc(ids, func(id int) bool { return id <= 0 }) {
			return fmt.Errorf("prompt text filter IDs must be positive and number at most %d per dimension", MaxPromptFilterTargets)
		}
	}
	return nil
}

func (target PromptTextFilterTarget) Matches(channelID, tokenID int) bool {
	switch target.Scope {
	case "all":
		return true
	case "channel":
		return channelID > 0 && slices.Contains(target.ChannelIDs, channelID)
	case "token":
		return tokenID > 0 && slices.Contains(target.TokenIDs, tokenID)
	case "channel_token":
		return channelID > 0 && tokenID > 0 && slices.Contains(target.ChannelIDs, channelID) && slices.Contains(target.TokenIDs, tokenID)
	default:
		return false
	}
}

type PromptFilterSelectorRow struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Status int    `json:"status"`
}

// FindPromptFilterTargets projects display metadata only. The selector never
// loads local API keys or upstream credentials from the database.
func FindPromptFilterTargets(ctx context.Context, kind, search string, ids []int) ([]PromptFilterSelectorRow, error) {
	query := DB.WithContext(ctx)
	switch kind {
	case "channel":
		query = query.Model(&Channel{})
	case "token":
		query = query.Model(&Token{})
	default:
		return nil, fmt.Errorf("invalid prompt filter target kind")
	}
	if len(ids) > 0 {
		query = query.Where("id IN ?", ids)
	} else {
		query = query.Limit(30)
		if search != "" {
			name := strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(strings.ToLower(search))
			if id, err := strconv.Atoi(strings.TrimPrefix(search, "#")); err == nil && id > 0 {
				query = query.Where("id = ? OR LOWER(name) LIKE ? ESCAPE '!'", id, "%"+name+"%")
				query = query.Order("id = " + strconv.Itoa(id) + " DESC")
			} else {
				query = query.Where("LOWER(name) LIKE ? ESCAPE '!'", "%"+name+"%")
			}
		}
	}
	rows := make([]PromptFilterSelectorRow, 0)
	err := query.Select("id", "name", "status").Order("id DESC").Find(&rows).Error
	return rows, err
}
