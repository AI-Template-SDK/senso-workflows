# Batch Processing Architecture

## Overview

This document describes the refactored org evaluation pipeline that separates question execution from data extraction, enabling efficient batch processing for BrightData-powered models (ChatGPT and Perplexity).

## Architecture Changes

### Previous Architecture (Single-Pass)
```
For each Question:
  For each Model:
    For each Location:
      1. Execute AI call (1 API call)
      2. Store QuestionRun
      3. Extract org evaluation
      4. Extract competitors
      5. Extract citations
```

**Problems:**
- Each question made a separate API call (inefficient for BrightData)
- Tight coupling between execution and extraction
- No way to batch questions for BrightData models

### New Architecture (Two-Phase)

```
PHASE 1: Name Variation Generation
  └─ Generate name variations once for the org

PHASE 2: Question Execution (Grouped by Model-Location)
  For each Model-Location pair:
    If provider supports batching (BrightData/Perplexity):
      └─ Process questions in batches of 20
         ├─ Submit batch (1 API call for 20 questions)
         ├─ Poll until complete
         └─ Store all 20 QuestionRuns
    Else (OpenAI/Anthropic):
      └─ Process questions sequentially
         └─ Store each QuestionRun

PHASE 3: Extraction Processing
  For each QuestionRun:
    ├─ Extract org evaluation → org_evals
    ├─ Extract competitors → org_competitors
    └─ Extract citations → org_citations
```

**Benefits:**
- BrightData: 20 questions in 1 API call (~90% reduction in calls)
- Clean separation: execution vs extraction
- Can resume at either phase independently
- No transient state (BrightData snapshot IDs) in database

## Model Support

### Supported Models

| Model Name | Provider | Batching | Max Batch Size | API Type |
|------------|----------|----------|----------------|----------|
| `gpt-4.1` | OpenAI | ❌ No | 1 | Web Search API |
| `chatgpt` | BrightData | ✅ Yes | 20 | Async Batch API |
| `perplexity` | Perplexity (via BrightData) | ✅ Yes | 20 | Async Batch API |
| `claude-*` | Anthropic | ❌ No | 1 | Direct API |

### Model Routing Logic

The `getProvider()` method routes models to the appropriate provider:

```go
if strings.Contains(modelLower, "chatgpt") {
    return NewBrightDataProvider(cfg, model, costService), nil
}
if strings.Contains(modelLower, "perplexity") {
    return NewPerplexityProvider(cfg, model, costService), nil
}
if strings.Contains(modelLower, "gpt") || strings.Contains(modelLower, "4.1") {
    return NewOpenAIProvider(cfg, model, costService), nil
}
if strings.Contains(modelLower, "claude") || ... {
    return NewAnthropicProvider(cfg, model, costService), nil
}
```

**Order matters:** `chatgpt` is checked before `gpt` to avoid misrouting.

## AIProvider Interface

### Enhanced Interface

```go
type AIProvider interface {
    // Existing methods
    RunQuestion(ctx, query, websearch, location) (*AIResponse, error)
    RunQuestionWebSearch(ctx, query) (*AIResponse, error)
    
    // New batching support
    SupportsBatching() bool
    GetMaxBatchSize() int
    RunQuestionBatch(ctx, queries, websearch, location) ([]*AIResponse, error)
}
```

### Provider Implementations

#### OpenAI Provider
```go
func (p *openAIProvider) SupportsBatching() bool { return false }
func (p *openAIProvider) GetMaxBatchSize() int { return 1 }
func (p *openAIProvider) RunQuestionBatch(...) {
    // Fallback: loop and call RunQuestion sequentially
}
```

#### BrightData Provider (ChatGPT)
```go
func (p *brightDataProvider) SupportsBatching() bool { return true }
func (p *brightDataProvider) GetMaxBatchSize() int { return 20 }
func (p *brightDataProvider) RunQuestionBatch(...) {
    // 1. Submit batch job with multiple inputs
    payload := BrightDataRequest{
        Input: []BrightDataInput{ /* up to 20 */ }
    }
    snapshotID := submitBatchJob(payload)
    
    // 2. Poll until complete
    results := pollBatchUntilComplete(snapshotID)
    
    // 3. Return array of AIResponse
    return results
}
```

#### Perplexity Provider
```go
func (p *perplexityProvider) SupportsBatching() bool { return true }
func (p *perplexityProvider) GetMaxBatchSize() int { return 20 }
func (p *perplexityProvider) RunQuestionBatch(...) {
    // Similar to BrightData but with Perplexity-specific format
    payload := PerplexityRequest[ /* up to 20 inputs */ ]
    // ... same async pattern
}
```

## Configuration

### Environment Variables

Add these to your `.env` file:

```bash
# BrightData Configuration
BRIGHTDATA_API_KEY=your_brightdata_api_key_here
BRIGHTDATA_DATASET_ID=your_chatgpt_dataset_id_here
PERPLEXITY_DATASET_ID=your_perplexity_dataset_id_here
```

### Config Structure

```go
type Config struct {
    // ... existing fields ...
    BrightDataAPIKey    string  // Shared by both BrightData providers
    BrightDataDatasetID string  // ChatGPT dataset
    PerplexityDatasetID string  // Perplexity dataset
}
```

## Key Implementation Details

### Phase 2: Question Execution

#### Model-Location Grouping

Questions are grouped by `(Model, Location)` pairs to enable batching:

```go
type ModelLocationPair struct {
    Model    *models.GeoModel
    Location *models.OrgLocation
}

pairs := createModelLocationPairs(models, locations)
// Example: [(gpt-4.1, US), (gpt-4.1, UK), (chatgpt, US), (chatgpt, UK), ...]
```

#### Batch Processing Flow

```go
for each pair in pairs {
    provider := getProvider(pair.Model.Name)
    
    if provider.SupportsBatching() {
        // Process in chunks of 20
        for batch := range chunkQuestions(questions, 20) {
            responses := provider.RunQuestionBatch(batch)
            storeQuestionRuns(responses)
        }
    } else {
        // Process one at a time
        for question := range questions {
            response := provider.RunQuestion(question)
            storeQuestionRun(response)
        }
    }
}
```

### Phase 3: Extraction Processing

Extraction happens sequentially after all questions are executed:

```go
for each questionRun in allQuestionRuns {
    extractOrgEvaluation(questionRun)
    extractCompetitors(questionRun)
    extractCitations(questionRun)
}
```

## Performance Characteristics

### BrightData Batch Processing

**Before:**
- 100 questions × 1 model × 1 location = 100 API calls
- Each call: ~10-20 seconds
- Total time: ~20-30 minutes

**After:**
- 100 questions ÷ 20 per batch = 5 API calls
- Each batch: ~10-20 seconds
- Total time: ~2-3 minutes

**Improvement: ~90% reduction in execution time**

### Cost Impact

BrightData charges $0.0015 per question regardless of batching, so:
- 100 questions = $0.15 (same cost, much faster)

### OpenAI (No Change)

OpenAI doesn't support batching, so behavior is unchanged:
- Still processes 1 question at a time
- Same execution time
- Same cost structure

## Resume Functionality

The two-phase architecture maintains full resume capability:

### Resume at Phase 2 (Question Execution)
```go
// Check if QuestionRun already exists
existingRun := repos.QuestionRunRepo.GetByQuestionModelLocation(...)
if existingRun != nil {
    // Skip execution, use existing run
    continue
}
```

### Resume at Phase 3 (Extraction)
```go
// Check if org_eval already exists
existingEval := repos.OrgEvalRepo.GetByQuestionRunID(...)
if existingEval != nil {
    // Skip extraction
    continue
}
```

## Error Handling

### Batch Failures

BrightData's `include_errors=true` parameter ensures partial batch failures are handled:

```json
{
  "results": [
    {"answer_text_markdown": "...", "error": ""},
    {"answer_text_markdown": "", "error": "timeout"},
    {"answer_text_markdown": "...", "error": ""}
  ]
}
```

Each response is processed individually:
- Success → Normal QuestionRun
- Failure → QuestionRun with error message

### Provider Failures

If a provider fails for a model-location pair:
- Error is logged to `summary.ProcessingErrors`
- Processing continues with next pair
- Partial results are still stored

## Testing

### Test with Single Org

```bash
python trigger_org_evaluation_workflow.py <org_id>
```

### Verify Batch Processing

Look for these log messages:

```
[executeQuestionsForPair] 🔄 Provider supports batching (max size: 20)
[executeQuestionsForPair] 📦 Processing batch 1-20 of 50 questions
[BrightDataProvider] 🚀 Making batched BrightData call for 20 queries
[BrightDataProvider] ✅ Batch completed: 20 questions processed
```

### Monitor Costs

Check the summary output:

```
🎉 Question matrix completed: 100 processed, 100 evaluations, 523 citations, 45 competitors, $0.150000 total cost
```

## Future Optimizations

### Parallel Batch Submission

BrightData supports up to 100 concurrent batches:

```go
// Submit multiple batches in parallel
for each model-location pair {
    go func(pair) {
        executeBatch(pair)
    }(pair)
}
```

### Adaptive Batch Sizing

Adjust batch size based on question complexity:

```go
if avgTokensPerQuestion > 1000 {
    batchSize = 10  // Reduce for complex questions
} else {
    batchSize = 20  // Maximum for simple questions
}
```

### Smart Resume Logic

Track which batches are in-flight and poll existing snapshots:

```go
inFlightSnapshots := getInFlightSnapshots()
for snapshot := range inFlightSnapshots {
    results := pollAndStore(snapshot)
}
```

## Troubleshooting

### "Provider does not support batching" for chatgpt

**Cause:** Model routing is checking `gpt` before `chatgpt`

**Fix:** Ensure `chatgpt` check comes before `gpt` check in `getProvider()`

### "Batch returned X responses but expected Y"

**Cause:** BrightData returned fewer results than requested

**Check:** 
1. `include_errors=true` is set in API call
2. Review error responses in batch results

### "BrightData API returned status 401"

**Cause:** Invalid or missing API key

**Fix:** 
1. Check `BRIGHTDATA_API_KEY` environment variable
2. Verify API key is active in BrightData dashboard

### Extraction phase takes too long

**Current:** Sequential processing (1 at a time)

**Future Optimization:** Parallelize extraction phase
```go
parallel.ForEach(questionRuns, func(run) {
    extractOrgEvaluation(run)
})
```

## Migration Path

### Existing Data

No migration needed - new architecture is fully compatible:
- Existing QuestionRuns remain unchanged
- New runs use same database schema
- Extraction logic is identical

### Gradual Rollout

1. ✅ Add new model types to database
2. ✅ Configure BrightData credentials
3. ✅ Test with single org
4. Run full evaluation with new models

## Summary

The new batch processing architecture:
- ✅ Supports BrightData batching (20 questions per API call)
- ✅ Maintains backward compatibility with OpenAI/Anthropic
- ✅ Separates execution from extraction (cleaner code)
- ✅ Preserves resume functionality
- ✅ Reduces API calls by ~90% for BrightData models
- ✅ No transient state in database (snapshot IDs only in memory)
- ✅ Same cost, dramatically faster execution

The architecture is production-ready and provides a clear path for future optimizations. 