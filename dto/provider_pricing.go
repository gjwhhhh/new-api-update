package dto

type ProviderPricingResponse struct {
	SchemaVersion string               `json:"schema_version"`
	Success       bool                 `json:"success"`
	Message       string               `json:"message"`
	Data          *ProviderPricingData `json:"data,omitempty"`
}

type ProviderPricingData struct {
	Currency   string                 `json:"currency"`
	PriceUnit  string                 `json:"price_unit"`
	SiteName   string                 `json:"site_name,omitempty"`
	SiteDomain string                 `json:"site_domain,omitempty"`
	UpdatedAt  string                 `json:"updated_at"`
	Models     []ProviderPricingModel `json:"models"`
}

type ProviderPricingModel struct {
	ModelName          string   `json:"model_name"`
	GroupName          string   `json:"group_name"`
	PriceUnit          string   `json:"price_unit,omitempty"`
	InputPrice         *float64 `json:"input_price,omitempty"`
	OutputPrice        *float64 `json:"output_price,omitempty"`
	CacheInputPrice    *float64 `json:"cache_input_price,omitempty"`
	CacheCreatePrice   *float64 `json:"cache_create_price,omitempty"`
	CacheCreatePrice1h *float64 `json:"cache_create_price_1h,omitempty"`
	UnitPrice          *float64 `json:"unit_price,omitempty"`
	Enabled            bool     `json:"enabled"`
	Note               string   `json:"note"`
}
