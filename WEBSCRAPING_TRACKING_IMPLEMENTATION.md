# Webscraping Tracking Implementation Guide

This document outlines the complete implementation of cost and token tracking for web scraping and crawling operations in the Senso workflows system.

## Overview

Following Tom's pattern for QuestionRunner tracking, we've implemented comprehensive tracking for website ingestion operations that captures:
- Input tokens
- Cost calculations
- Model used
- Operation metadata

## Phase 1: Database Schema (senso-api)

### 1. Migration Files Created

**`migrations/000011_create_webscraping_runs.up.sql`**
- Creates `webscraping_runs` table with comprehensive tracking fields
- Includes indexes for common queries
- Adds foreign key constraint to `orgs` table
- Sets up automatic `updated_at` trigger

**`migrations/000011_create_webscraping_runs.down.sql`**
- Properly drops all created objects in reverse order

### 2. Data Model

**`pkg/models/webscraping_run.go`**
```go
type WebscrapingRun struct {
    WebscrapingRunID uuid.UUID  `json:"webscraping_run_id" db:"webscraping_run_id"`
    OrgID            uuid.UUID  `json:"org_id" db:"org_id"`
    SourceURL        string     `json:"source_url" db:"source_url"`
    PageTitle        *string    `json:"page_title,omitempty" db:"page_title"`
    OperationType    string     `json:"operation_type" db:"operation_type"` // 'scrape' or 'crawl'
    EventName        string     `json:"event_name" db:"event_name"`         // e.g., 'website/scrape.requested'
    ChunksProcessed  int        `json:"chunks_processed" db:"chunks_processed"`
    InputTokens      *int       `json:"input_tokens,omitempty" db:"input_tokens"`
    TotalCost        *float64   `json:"total_cost,omitempty" db:"total_cost"`
    ModelUsed        *string    `json:"model_used,omitempty" db:"model_used"`
    Status           string     `json:"status" db:"status"` // 'completed', 'failed', 'partial'
    ErrorMessage     *string    `json:"error_message,omitempty" db:"error_message"`
    CreatedAt        time.Time  `json:"created_at" db:"created_at"`
    UpdatedAt        time.Time  `json:"updated_at" db:"updated_at"`
    DeletedAt        *time.Time `json:"deleted_at,omitempty" db:"deleted_at"`
}
```

### 3. Repository Layer

**`pkg/repositories/interfaces/webscraping_run.go`**
- Defines `WebscrapingRunRepository` interface
- Includes `WebscrapingRunStats` struct for aggregated statistics

**`pkg/repositories/postgresql/webscraping_run.go`**
- Full PostgreSQL implementation
- CRUD operations with proper error handling
- Statistics aggregation queries
- Follows established patterns from other repositories

## Phase 2: Workflow Integration (senso-workflows)

### 1. Updated AI Provider Interface

**`services/interfaces.go`**
```go
type AIProvider interface {
    RunQuestion(ctx context.Context, question string, webSearch bool, location *workflowModels.Location) (*AIResponse, error)
    CreateEmbedding(ctx context.Context, text []string, model string) (*EmbeddingResult, error)
}

type EmbeddingResult struct {
    Vectors      [][]float32
    InputTokens  int
    OutputTokens int
    Cost         float64
    Model        string
}
```

### 2. Updated OpenAI Provider

**`services/openai_provider.go`**
- Modified `CreateEmbedding` to return `*EmbeddingResult`
- Integrates with `CostService` for cost calculation
- Captures token usage from API response
- Returns comprehensive tracking data

### 3. Updated Anthropic Provider

**`services/anthropic_provider.go`**
- Updated method signature to match interface
- Returns error for unsupported embedding operations

### 4. Webscraping Tracking Service

**`services/webscraping_tracking_service.go`**
```go
type WebscrapingTrackingService interface {
    RecordRun(ctx context.Context, orgID uuid.UUID, sourceURL, pageTitle, operationType, eventName string, chunksProcessed int, inputTokens *int, totalCost *float64, modelUsed *string, status string, errorMessage *string) error
    GetStatsByOrgID(ctx context.Context, orgID uuid.UUID) (*interfaces.WebscrapingRunStats, error)
    GetRunsByOrgID(ctx context.Context, orgID uuid.UUID, limit, offset int) ([]*models.WebscrapingRun, error)
}
```

### 5. Updated Web Ingestion Processor

**`workflows/web_ingestion_processor.go`**
- Added `WebscrapingTrackingService` dependency
- Modified `_ingestContent` to accept `eventName` parameter
- Added tracking step after successful embedding generation
- Distinguishes between 'scrape' and 'crawl' operations based on event name

## Implementation Details

### Cost Calculation
- Uses existing `CostService` for consistent pricing
- Supports different models and providers
- Handles embedding-specific pricing (no output tokens)

### Operation Type Detection
- `website/scrape.requested` → `scrape` operation
- `website/content.found` → `crawl` operation

### Error Handling
- Graceful handling of tracking failures
- Non-blocking tracking (main operation continues)
- Comprehensive error logging

### Database Operations
- Soft deletes with `deleted_at` timestamps
- Proper indexing for performance
- Foreign key constraints for data integrity

## Usage Example

```go
// In your workflow
embeddingResult, err := p.openAIService.CreateEmbedding(ctx, chunks, "text-embedding-ada-002")
if err != nil {
    return nil, fmt.Errorf("embedding generation failed: %w", err)
}

// Record tracking data
err = p.webscrapingTrackingService.RecordRun(
    ctx,
    orgUUID,
    sourceURL,
    pageTitle,
    "scrape",
    "website/scrape.requested",
    len(chunks),
    &embeddingResult.InputTokens,
    &embeddingResult.Cost,
    &embeddingResult.Model,
    "completed",
    nil,
)
```

## Migration Steps

1. **Deploy senso-api changes first:**
   ```bash
   # Run the migration
   migrate -path migrations -database "postgres://..." up
   ```

2. **Update senso-workflows dependencies:**
   ```bash
   go get -u github.com/AI-Template-SDK/senso-api@latest
   ```

3. **Deploy senso-workflows changes**

## Benefits

- **Complete Visibility:** Track all AI operations and costs
- **Consistent Pattern:** Follows established QuestionRunner tracking
- **Scalable Design:** Supports multiple operation types and providers
- **Performance Optimized:** Proper indexing and efficient queries
- **Error Resilient:** Non-blocking tracking with comprehensive logging

## Future Enhancements

- Add cost alerts and thresholds
- Implement cost analytics dashboard
- Add support for more embedding models
- Create cost optimization recommendations
