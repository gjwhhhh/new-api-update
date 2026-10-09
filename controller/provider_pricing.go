package controller

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
)

func GetProviderPricing(c *gin.Context) {
	models, err := service.GetProviderPricingModels()
	if err != nil {
		common.SysError("get provider pricing: " + err.Error())
		c.JSON(http.StatusInternalServerError, dto.ProviderPricingResponse{
			SchemaVersion: "1.1",
			Success:       false,
			Message:       "service temporarily unavailable",
		})
		return
	}

	siteDomain := ""
	if parsed, parseErr := url.Parse(strings.TrimSpace(system_setting.ServerAddress)); parseErr == nil {
		siteDomain = parsed.Hostname()
	}
	c.JSON(http.StatusOK, dto.ProviderPricingResponse{
		SchemaVersion: "1.1",
		Success:       true,
		Message:       "",
		Data: &dto.ProviderPricingData{
			Currency:   "CNY",
			PriceUnit:  "per_1m_tokens",
			SiteName:   common.SystemName,
			SiteDomain: siteDomain,
			UpdatedAt:  time.Now().UTC().Format(time.RFC3339),
			Models:     models,
		},
	})
}
