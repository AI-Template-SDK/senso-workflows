# AI Providers Package

This package contains all AI provider implementations for the senso-workflows project.

## Structure

```
providers/
├── provider.go              # AIProvider interface definition
├── factory.go              # Provider factory for model selection
├── common/                 # Shared utilities for BrightData-based providers
│   ├── types.go           # Common types (AIResponse, TriggerResponse, etc.)
│   ├── brightdata_client.go  # BrightData HTTP client
│   ├── location_mapper.go    # Country mapping utilities
│   └── response_parser.go    # Response parsing utilities
├── chatgpt/               # ChatGPT provider (via BrightData)
│   ├── provider.go        # Provider implementation
│   ├── batch.go          # Batch processing logic
│   ├── single.go         # Single question processing
│   └── types.go          # ChatGPT-specific types
├── perplexity/           # Perplexity provider (via BrightData)
│   ├── provider.go
│   ├── batch.go
│   ├── single.go
│   └── types.go
├── gemini/               # Gemini provider (via BrightData)
│   ├── provider.go
│   ├── batch.go
│   ├── single.go
│   └── types.go
├── openai/               # OpenAI provider (direct API)
│   ├── provider.go
│   ├── single.go         # Primary method for OpenAI
│   ├── batch.go          # Sequential fallback (no native batching)
│   └── types.go
└── anthropic/            # Anthropic provider (direct API)
    ├── provider.go
    ├── single.go         # Primary method for Anthropic
    ├── batch.go          # Sequential fallback (no native batching)
    └── types.go
```

## Usage

### Creating a Provider

Use the factory to create the appropriate provider:

```go
import "github.com/AI-Template-SDK/senso-workflows/internal/providers"

// Create provider
provider, err := providers.NewProvider("chatgpt", cfg, costService)
if err != nil {
    return err
}

// Use provider
responses, err := provider.RunQuestionBatch(ctx, queries, true, location)
```

### Provider Interface

All providers implement the `AIProvider` interface:

```go
type AIProvider interface {
    // Single question operations
    RunQuestion(ctx context.Context, query string, websearch bool, location *models.Location) (*common.AIResponse, error)
    RunQuestionWebSearch(ctx context.Context, query string) (*common.AIResponse, error)

    // Batch operations
    RunQuestionBatch(ctx context.Context, queries []string, websearch bool, location *models.Location) ([]*common.AIResponse, error)
    
    // Provider metadata
    GetProviderName() string
    SupportsBatching() bool
    GetMaxBatchSize() int
}
```

### Single vs Batch Operations

**BrightData-based providers (ChatGPT, Perplexity, Gemini):**
- **Batching**: Native support, processes up to 20 queries in a single API call
- **Single**: Available for compatibility, but batching is preferred

**Direct API providers (OpenAI, Anthropic):**
- **Single**: Primary method, direct API calls
- **Batching**: Sequential fallback (calls `RunQuestion()` in a loop)

## Common Package

The `common` package contains shared utilities for BrightData-based providers (ChatGPT, Perplexity, Gemini).

### BrightDataClient

Handles all HTTP interactions with the BrightData API:

- `SubmitBatchJob()` - Submit a batch job
- `CheckProgress()` - Check job status
- `PollUntilComplete()` - Poll until job completes
- `GetBatchResults()` - Retrieve results
- `SaveErrorResponse()` - Save error responses for debugging

### Utilities

- `MapLocationToCountry()` - Maps location objects to country codes
- `IsStatusResponse()` - Checks if response is a status object
- `Min()` - Helper for integer min

## Adding a New Provider

1. Create a new directory: `providers/{provider_name}/`
2. Create three files:
   - `provider.go` - Main provider struct and interface implementation
   - `batch.go` - Batch processing logic
   - `types.go` - Provider-specific types

3. Implement the `AIProvider` interface
4. Add provider to `factory.go`

### Example Provider Structure

```go
// provider.go
package myprovider

type Provider struct {
    client      *common.BrightDataClient  // If using BrightData
    datasetID   string
    costService services.CostService
}

func NewProvider(cfg *config.Config, model string, costService services.CostService) *Provider {
    return &Provider{
        client:      common.NewBrightDataClient(cfg.BrightDataAPIKey),
        datasetID:   cfg.MyProviderDatasetID,
        costService: costService,
    }
}

func (p *Provider) GetProviderName() string { return "myprovider" }
func (p *Provider) SupportsBatching() bool { return true }
func (p *Provider) GetMaxBatchSize() int { return 20 }
func (p *Provider) RunQuestion(ctx, query, websearch, location) (*common.AIResponse, error) { /* impl */ }
func (p *Provider) RunQuestionWebSearch(ctx, query) (*common.AIResponse, error) { /* impl */ }
func (p *Provider) RunQuestionBatch(ctx, queries, websearch, location) ([]*common.AIResponse, error) { /* impl */ }
```

**File Organization:**
- `provider.go` - Struct, constructor, metadata methods
- `single.go` - `RunQuestion()` and `RunQuestionWebSearch()` implementations
- `batch.go` - `RunQuestionBatch()` implementation
- `types.go` - Provider-specific request/response types

## Design Principles

1. **Separation of Concerns**: Each provider is self-contained
2. **DRY**: Common BrightData logic extracted to `common/`
3. **Interface-Based**: All providers implement `AIProvider`
4. **Factory Pattern**: Centralized provider selection
5. **No Import Cycles**: Types in `common/` to avoid cycles

## Migration Notes

This package replaces the old provider files in `services/`:
- `services/brightdata_provider.go` → `providers/chatgpt/`
- `services/perplexity_provider.go` → `providers/perplexity/`
- `services/gemini_provider.go` → `providers/gemini/`
- `services/openai_provider.go` → `providers/openai/`
- `services/anthropic_provider.go` → `providers/anthropic/`

Old provider files should be removed after migration is complete.

