package providers

import (
	"context"

	"github.com/AI-Template-SDK/senso-workflows/internal/models"
	"github.com/AI-Template-SDK/senso-workflows/internal/providers/common"
)

// AIProvider interface for different AI models
type AIProvider interface {
	// Sync execution methods (for direct API providers like OpenAI/Anthropic)
	RunQuestion(ctx context.Context, query string, websearch bool, location *models.Location) (*common.AIResponse, error)
	RunQuestionWebSearch(ctx context.Context, query string) (*common.AIResponse, error)
	RunQuestionBatch(ctx context.Context, queries []string, websearch bool, location *models.Location) ([]*common.AIResponse, error)

	// Async execution methods (for polling-based providers like BrightData)
	// Only implemented if IsAsync() returns true
	SubmitBatchJob(ctx context.Context, queries []string, websearch bool, location *models.Location) (string, error)
	PollJobStatus(ctx context.Context, jobID string) (string, bool, error)
	RetrieveBatchResults(ctx context.Context, jobID string, queries []string) ([]*common.AIResponse, error)

	// Provider metadata
	GetProviderName() string
	IsAsync() bool // NEW: Indicates if provider needs async execution
	SupportsBatching() bool
	GetMaxBatchSize() int
}
