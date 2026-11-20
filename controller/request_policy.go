package controller

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func GetRequestPolicy(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"options": model.RequestPolicyOptions()}})
}

func UpdateRequestPolicy(c *gin.Context) {
	var request struct {
		Options map[string]string `json:"options"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	if err := model.UpdateRequestPolicyOptions(request.Options); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	GetRequestPolicy(c)
}

func GetPromptFilterTargets(c *gin.Context) {
	kind, search := c.Query("kind"), strings.TrimSpace(c.Query("q"))
	rawIDs := c.QueryArray("id")
	if (kind != "channel" && kind != "token") || len(search) > 192 || len(rawIDs) > model.MaxPromptFilterTargets || (search != "" && len(rawIDs) > 0) {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Invalid filter target search"})
		return
	}
	ids := make([]int, 0, len(rawIDs))
	for _, raw := range rawIDs {
		id, err := strconv.Atoi(raw)
		if err != nil || id <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Invalid filter target ID"})
			return
		}
		ids = append(ids, id)
	}
	rows, err := model.FindPromptFilterTargets(c.Request.Context(), kind, search, ids)
	if err != nil {
		common.SysError("prompt filter target lookup failed: " + err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "Failed to load filter targets"})
		return
	}
	common.ApiSuccess(c, rows)
}
