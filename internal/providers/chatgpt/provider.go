package chatgpt

import (
	"github.com/AI-Template-SDK/senso-workflows/internal/config"
	"github.com/AI-Template-SDK/senso-workflows/internal/providers/common"
	"github.com/AI-Template-SDK/senso-workflows/services"
)

// Provider implements the AIProvider interface for ChatGPT via BrightData
type Provider struct {
	client      *common.BrightDataClient
	datasetID   string
	costService services.CostService
}

// NewProvider creates a new ChatGPT provider
func NewProvider(cfg *config.Config, model string, costService services.CostService) *Provider {
	if cfg == nil {
		cfg = &config.Config{}
	}

	return &Provider{
		client:      common.NewBrightDataClient(cfg.BrightDataAPIKey),
		datasetID:   cfg.BrightDataDatasetID,
		costService: costService,
	}
}

// GetProviderName returns the name of this provider
func (p *Provider) Name() string {
	return "chatgpt"
}

// IsAsync returns true as ChatGPT provider uses async polling
func (p *Provider) IsAsync() bool {
	return true
}

// SupportsBatching returns true as ChatGPT supports batch processing
func (p *Provider) SupportsBatching() bool {
	return true
}

// GetMaxBatchSize returns the maximum batch size supported (20 for BrightData)
func (p *Provider) GetMaxBatchSize() int {
	return 20
}
