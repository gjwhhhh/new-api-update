package controller

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type emailPageQuery struct {
	Offset int `form:"offset" binding:"gte=0"`
	Limit  int `form:"limit" binding:"gte=0,lte=100"`
}

func mailResponse(c *gin.Context, data any, err error) {
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, gorm.ErrRecordNotFound) {
			status = http.StatusNotFound
		}
		if errors.Is(err, model.ErrEmailConflict) {
			status = http.StatusConflict
		}
		c.JSON(status, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": data})
}
func emailID(c *gin.Context) (int64, error) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("Invalid email ID")
	}
	return id, nil
}

func ListEmailRecords(c *gin.Context) {
	var query struct {
		emailPageQuery
		model.EmailRecordFilter
	}
	if err := c.ShouldBindQuery(&query); err != nil {
		mailResponse(c, nil, errors.New("Invalid email filters"))
		return
	}
	if query.Limit == 0 {
		query.Limit = 20
	}
	rows, total, err := model.ListEmailRecords(query.EmailRecordFilter, query.Offset, query.Limit)
	mailResponse(c, gin.H{"items": rows, "total": total}, err)
}
func GetEmailRecord(c *gin.Context) {
	id, err := emailID(c)
	if err != nil {
		mailResponse(c, nil, err)
		return
	}
	record, message, attempts, err := model.GetEmailRecord(id)
	if err == nil {
		recordMailOperation(c, "mail.view", id, true)
	}
	mailResponse(c, gin.H{"record": record, "message": message, "attempts": attempts}, err)
}
func GetEmailRecipients(c *gin.Context) {
	var query struct {
		emailPageQuery
		model.EmailRecipientFilter
	}
	if err := c.ShouldBindQuery(&query); err != nil {
		mailResponse(c, nil, errors.New("Invalid recipient filters"))
		return
	}
	if query.Limit == 0 {
		query.Limit = 20
	}
	query.All = true
	rows, total, err := model.FindEmailRecipients(query.EmailRecipientFilter, query.Offset, query.Limit)
	mailResponse(c, gin.H{"items": rows, "total": total}, err)
}
func PreviewEmailBatch(c *gin.Context) {
	var request service.EmailPreviewRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		mailResponse(c, nil, errors.New("Invalid email request"))
		return
	}
	preview, err := service.PrepareEmailBatch(c.GetInt("id"), c.GetString("username"), request)
	mailResponse(c, preview, err)
}
func SubmitEmailBatch(c *gin.Context) {
	id, err := emailID(c)
	if err == nil {
		err = service.SubmitEmailBatch(id, c.GetInt("id"))
	}
	recordMailOperation(c, "mail.submit", id, err == nil)
	mailResponse(c, nil, err)
}
func TestEmail(c *gin.Context) {
	var request struct {
		BatchID int64 `json:"batch_id"`
		UserID  int   `json:"user_id"`
	}
	if err := c.ShouldBindJSON(&request); err != nil || request.BatchID <= 0 {
		mailResponse(c, nil, errors.New("Invalid email request"))
		return
	}
	if request.UserID == 0 {
		request.UserID = c.GetInt("id")
	}
	err := service.SendTestEmail(c.Request.Context(), c.GetInt("id"), request.BatchID, request.UserID)
	recordMailOperation(c, "mail.test", request.BatchID, err == nil)
	mailResponse(c, nil, err)
}
func ListEmailBatches(c *gin.Context) {
	var query emailPageQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		mailResponse(c, nil, errors.New("Invalid email filters"))
		return
	}
	if query.Limit == 0 {
		query.Limit = 20
	}
	rows, total, err := model.ListEmailBatches(query.Offset, query.Limit)
	mailResponse(c, gin.H{"items": rows, "total": total}, err)
}
func GetEmailBatch(c *gin.Context) {
	id, err := emailID(c)
	if err != nil {
		mailResponse(c, nil, err)
		return
	}
	batch, err := model.GetEmailBatch(id)
	if err != nil {
		mailResponse(c, nil, err)
		return
	}
	if batch.Status == model.EmailPrepared && batch.ActorID != c.GetInt("id") {
		mailResponse(c, nil, model.ErrEmailConflict)
		return
	}
	var query emailPageQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		mailResponse(c, nil, errors.New("Invalid email filters"))
		return
	}
	if query.Limit == 0 {
		query.Limit = 20
	}
	message, err := model.GetEmailMessage(batch.MessageID)
	if err != nil {
		mailResponse(c, nil, err)
		return
	}
	rows, total, err := model.ListEmailRecords(model.EmailRecordFilter{BatchID: id}, query.Offset, query.Limit)
	var counts []struct {
		Status string `json:"status"`
		Count  int64  `json:"count"`
	}
	if err == nil {
		err = model.DB.Model(&model.EmailDelivery{}).Select("status, COUNT(*) AS count").Where("batch_id = ?", id).Group("status").Find(&counts).Error
	}
	recordMailOperation(c, "mail.view_batch", id, err == nil)
	mailResponse(c, gin.H{"batch": batch, "message": message, "items": rows, "total": total, "counts": counts}, err)
}
func OperateEmailBatch(c *gin.Context) {
	id, err := emailID(c)
	action := c.Param("action")
	if action == "retry" && !operation_setting.GetMailSetting().SendingEnabled {
		err = errors.New("Email sending is disabled")
	}
	if err == nil {
		err = model.OperateEmailBatch(id, action, common.GetTimestamp())
	}
	recordMailOperation(c, "mail."+action, id, err == nil)
	mailResponse(c, nil, err)
}
func GetMailSettings(c *gin.Context) {
	mailResponse(c, gin.H{"settings": operation_setting.GetMailSetting(), "smtp_configured": common.SMTPServer != ""}, nil)
}
func UpdateMailSettings(c *gin.Context) {
	var settings operation_setting.MailSetting
	if err := c.ShouldBindJSON(&settings); err != nil {
		mailResponse(c, nil, errors.New("Invalid email settings"))
		return
	}
	values, err := settings.ConfigToMap()
	if err == nil {
		copy := operation_setting.GetMailSetting()
		err = copy.UpdateConfigFromMap(values)
	}
	if err == nil {
		options := map[string]string{}
		for key, value := range values {
			options["mail."+key] = value
		}
		err = model.UpdateOptionsBulk(options)
	}
	recordMailOperation(c, "mail.settings", 0, err == nil)
	mailResponse(c, nil, err)
}
func recordMailOperation(c *gin.Context, action string, id int64, success bool) {
	model.RecordAuditLog(c, model.AuditLog{UserId: c.GetInt("id"), Username: c.GetString("username"), ActorRole: c.GetInt("role"), Category: model.AuditCategoryOperation, Action: action, Success: success, Content: "Email center operation", Other: model.AuditOther{Op: &model.AuditOperation{Action: action, Params: model.AuditFields{"batch_or_record_id": id}}}})
}
