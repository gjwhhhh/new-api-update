package controller

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func logPresetScope(c *gin.Context) (int, string, string, bool) {
	userId := c.GetInt("id")
	if userId <= 0 {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"success": false})
		return 0, "", "", false
	}
	category, scope := c.DefaultQuery("category", "common"), c.DefaultQuery("scope", "self")
	if category != "common" || (scope != "self" && scope != "all") {
		logPresetError(c, model.ErrLogPresetInvalid)
		return 0, "", "", false
	}
	if scope == "all" && c.GetInt("role") < common.RoleAdminUser {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"success": false, "code": "LOG_PRESET_FORBIDDEN"})
		return 0, "", "", false
	}
	return userId, category, scope, true
}

func logPresetError(c *gin.Context, err error) {
	status, code := http.StatusInternalServerError, "LOG_PRESET_FAILED"
	switch {
	case errors.Is(err, model.ErrLogPresetInvalid):
		status, code = http.StatusBadRequest, "LOG_PRESET_INVALID"
	case errors.Is(err, model.ErrLogPresetConflict):
		status, code = http.StatusConflict, "LOG_PRESET_CONFLICT"
	case errors.Is(err, model.ErrLogPresetName):
		status, code = http.StatusConflict, "LOG_PRESET_NAME_EXISTS"
	case errors.Is(err, model.ErrLogPresetLimit):
		status, code = http.StatusBadRequest, "LOG_PRESET_LIMIT"
	case errors.Is(err, gorm.ErrRecordNotFound):
		status, code = http.StatusNotFound, "LOG_PRESET_NOT_FOUND"
	default:
		common.SysError("log filter preset operation failed: " + err.Error())
	}
	c.JSON(status, gin.H{"success": false, "code": code})
}

func ListLogFilterPresets(c *gin.Context) {
	userId, category, scope, ok := logPresetScope(c)
	if !ok {
		return
	}
	collection, err := model.ListLogFilterPresets(userId, category, scope)
	if err != nil {
		logPresetError(c, err)
		return
	}
	common.ApiSuccess(c, collection)
}

func GetLogFilterPreset(c *gin.Context) {
	userId, category, scope, ok := logPresetScope(c)
	if !ok {
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		logPresetError(c, model.ErrLogPresetInvalid)
		return
	}
	preset, err := model.GetLogFilterPreset(userId, id, category, scope)
	if err != nil {
		logPresetError(c, err)
		return
	}
	common.ApiSuccess(c, preset)
}

func SaveLogFilterPreset(c *gin.Context) {
	userId, category, scope, ok := logPresetScope(c)
	if !ok {
		return
	}
	id := 0
	if c.Request.Method == http.MethodPatch {
		var err error
		id, err = strconv.Atoi(c.Param("id"))
		if err != nil || id <= 0 {
			logPresetError(c, model.ErrLogPresetInvalid)
			return
		}
	}
	var request struct {
		Name    *string                 `json:"name"`
		Filters *model.LogPresetFilters `json:"filters"`
		Version *int64                  `json:"version"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16*1024)
	if err := c.ShouldBindJSON(&request); err != nil || (request.Name == nil && request.Filters == nil) || (id > 0 && (request.Version == nil || *request.Version < 1)) {
		logPresetError(c, model.ErrLogPresetInvalid)
		return
	}
	version := int64(0)
	if request.Version != nil {
		version = *request.Version
	}
	preset, err := model.SaveLogFilterPreset(userId, id, category, scope, version, request.Name, request.Filters)
	if err != nil {
		logPresetError(c, err)
		return
	}
	common.ApiSuccess(c, preset)
}

func DeleteLogFilterPreset(c *gin.Context) {
	userId, category, scope, ok := logPresetScope(c)
	if !ok {
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		logPresetError(c, model.ErrLogPresetInvalid)
		return
	}
	var request struct {
		Version *int64 `json:"version"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1024)
	if err := c.ShouldBindJSON(&request); err != nil || request.Version == nil || *request.Version < 1 {
		logPresetError(c, model.ErrLogPresetInvalid)
		return
	}
	preference, err := model.DeleteLogFilterPreset(userId, id, category, scope, *request.Version)
	if err != nil {
		logPresetError(c, err)
		return
	}
	common.ApiSuccess(c, preference)
}

func SetDefaultLogFilterPreset(c *gin.Context) {
	userId, category, scope, ok := logPresetScope(c)
	if !ok {
		return
	}
	var request struct {
		DefaultPresetId common.RawMessage `json:"default_preset_id"`
		Version         *int64            `json:"version"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1024)
	if err := c.ShouldBindJSON(&request); err != nil || request.Version == nil || *request.Version < 0 || len(request.DefaultPresetId) == 0 {
		logPresetError(c, model.ErrLogPresetInvalid)
		return
	}
	var id *int
	if err := common.Unmarshal(request.DefaultPresetId, &id); err != nil || (id != nil && *id <= 0) {
		logPresetError(c, model.ErrLogPresetInvalid)
		return
	}
	preference, err := model.SetDefaultLogFilterPreset(userId, category, scope, id, *request.Version)
	if err != nil {
		logPresetError(c, err)
		return
	}
	common.ApiSuccess(c, preference)
}
