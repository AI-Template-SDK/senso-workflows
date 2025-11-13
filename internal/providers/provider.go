package providers

import (
	"context"

	"github.com/AI-Template-SDK/senso-workflows/internal/models"
	"github.com/AI-Template-SDK/senso-workflows/internal/providers/common"
)

// AIProvider interface for different AI models
type AIProvider interface {
	// Sync execution methods (complete immediately or block until done)
	ExecutePrompt(ctx context.Context, query string, websearch bool, location *models.Location) (*common.AIResponse, error)
	ExecutePromptBatch(ctx context.Context, queries []string, websearch bool, location *models.Location) ([]*common.AIResponse, error)

	// Async execution methods (three-step job pattern for polling-based providers)
	// Only implemented if IsAsync() returns true
	SubmitPromptBatch(ctx context.Context, queries []string, websearch bool, location *models.Location) (string, error)
	PollPromptBatch(ctx context.Context, jobID string) (string, bool, error)
	RetrievePromptBatch(ctx context.Context, jobID string, queries []string) ([]*common.AIResponse, error)

	// Provider metadata
	Name() string           // Returns the provider name (e.g., "chatgpt", "openai")
	IsAsync() bool          // Returns true if provider requires async three-step job execution
	SupportsBatching() bool // Returns true if provider can process multiple queries at once
	MaxBatchSize() int      // Returns the maximum number of queries per batch (0 = unlimited)
}
