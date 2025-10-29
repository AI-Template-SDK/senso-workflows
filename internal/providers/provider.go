package providers

import (
	"context"

	"github.com/AI-Template-SDK/senso-workflows/internal/models"
	"github.com/AI-Template-SDK/senso-workflows/internal/providers/common"
)

// AIProvider interface for different AI models
type AIProvider interface {
	RunQuestionBatch(ctx context.Context, queries []string, websearch bool, location *models.Location) ([]*common.AIResponse, error)
	GetProviderName() string
	SupportsBatching() bool
	GetMaxBatchSize() int
}
