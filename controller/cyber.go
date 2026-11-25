package controller

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

func GetCyberPolicy(c *gin.Context) {
	common.ApiSuccess(c, service.CurrentCyberPolicy().ForAPI())
}

func SaveCyberPolicy(c *gin.Context) {
	var policy service.CyberPolicy
	if err := decodeCyberBody(c, &policy); err != nil || policy.Validate() != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Invalid Cyber policy"})
		return
	}
	policy.Normalize()
	existing := service.CurrentCyberPolicy()
	if policy.NotifyWebhookURL == "" {
		policy.NotifyWebhookSecret = ""
	} else if policy.NotifyWebhookSecret == "" {
		policy.NotifyWebhookSecret = existing.NotifyWebhookSecret
	}
	if policy.BlockedAction == "reroute" {
		channel, err := model.GetChannelById(policy.FallbackChannelID, false)
		if err != nil || channel == nil || channel.Status != common.ChannelStatusEnabled {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "The fallback channel must exist and be enabled"})
			return
		}
	}
	data, err := common.Marshal(policy)
	if err == nil {
		err = model.UpdateOption(operation_setting.CyberPolicyOptionKey, string(data))
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.RecordAuditLog(c, model.AuditLog{UserId: c.GetInt("id"), ActorRole: c.GetInt("role"), Category: model.AuditCategoryOperation, Action: "cyber_policy_updated", Content: "Cyber policy updated", Success: true, Other: model.AuditOther{Op: &model.AuditOperation{Action: "cyber_policy_updated", Params: map[string]any{"mode": policy.Mode, "scope": policy.Scope, "token_ids": policy.TokenIDs, "channel_ids": policy.ChannelIDs, "ttl_seconds": policy.TTLSeconds, "match_message": policy.MatchMessage, "capture_full_payload": policy.CaptureFullPayload, "blocked_action": policy.BlockedAction, "fallback_channel_id": policy.FallbackChannelID, "notify_webhook_configured": policy.NotifyWebhookURL != ""}}}})
	common.ApiSuccess(c, service.CurrentCyberPolicy().ForAPI())
}

func decodeCyberBody(c *gin.Context, dst any) error {
	data, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 65536))
	if err != nil {
		return err
	}
	return common.Unmarshal(data, dst)
}

func GetCyberEvents(c *gin.Context) {
	end, e1 := strconv.ParseInt(c.DefaultQuery("end", strconv.FormatInt(time.Now().Unix(), 10)), 10, 64)
	start, e2 := strconv.ParseInt(c.DefaultQuery("start", strconv.FormatInt(end-86400, 10)), 10, 64)
	token, e3 := strconv.Atoi(c.DefaultQuery("token_id", "0"))
	channel, e4 := strconv.Atoi(c.DefaultQuery("channel_id", "0"))
	f := model.CyberEventFilter{Start: start, End: end, TokenID: token, ChannelID: channel, Source: c.Query("source"), Action: c.Query("action"), Fingerprint: c.Query("fingerprint")}
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil || token < 0 || channel < 0 || start <= 0 || end < start || end-start > 7*86400 || (f.Fingerprint != "" && !service.IsCyberFingerprint(f.Fingerprint)) {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Invalid Cyber event filters"})
		return
	}
	items, err := model.CyberEvents(c.Request.Context(), f)
	if errors.Is(err, model.ErrCyberEventWindow) {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"success": false, "message": err.Error()})
		return
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}
	page := common.GetPageQuery(c)
	if page.GetPageSize() < 1 || page.GetPage() < 1 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Invalid Cyber event pagination"})
		return
	}
	from := len(items)
	if page.GetPage()-1 <= len(items)/page.GetPageSize() {
		from = (page.GetPage() - 1) * page.GetPageSize()
	}
	to := min(from+page.GetPageSize(), len(items))
	page.SetItems(items[from:to])
	page.SetTotal(len(items))
	common.ApiSuccess(c, page)
}

func GetCyberBans(c *gin.Context) {
	cursor, err := strconv.ParseUint(c.DefaultQuery("cursor", "0"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false})
		return
	}
	items, next, err := service.ListCyberBans(c.Request.Context(), cursor)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": err.Error()})
		return
	}
	common.ApiSuccess(c, gin.H{"items": items, "cursor": strconv.FormatUint(next, 10)})
}

func DeleteCyberBan(c *gin.Context) {
	var form struct {
		service.CyberBanTarget
		Generation string `json:"generation"`
		Reason     string `json:"reason"`
	}
	if err := decodeCyberBody(c, &form); err != nil || strings.TrimSpace(form.Reason) == "" || len(form.Reason) > 256 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "A reason of 1–256 bytes is required"})
		return
	}
	form.Reason = strings.TrimSpace(form.Reason)
	err := service.RevokeCyberBan(c.Request.Context(), form.CyberBanTarget, form.Generation)
	status := http.StatusOK
	if errors.Is(err, service.ErrCyberBanChanged) {
		status = http.StatusConflict
	} else if err != nil {
		status = http.StatusServiceUnavailable
	}
	model.RecordAuditLog(c, model.AuditLog{UserId: c.GetInt("id"), ActorRole: c.GetInt("role"), Category: model.AuditCategoryOperation, Action: "cyber_ban_revoked", Content: "Cyber session ban revocation", Status: status, Success: err == nil, Other: model.AuditOther{Op: &model.AuditOperation{Action: "cyber_ban_revoked", Params: map[string]any{"token_id": form.TokenID, "channel_id": form.ChannelID, "fingerprint": form.Fingerprint, "reason": form.Reason}}}})
	if err != nil {
		c.JSON(status, gin.H{"success": false, "message": err.Error()})
		return
	}
	common.ApiSuccess(c, nil)
}
