# Async Provider Architecture

## Overview

The provider system now supports **two execution models**:
1. **Synchronous** (OpenAI, Anthropic) - Direct API calls with immediate responses
2. **Asynchronous** (ChatGPT, Perplexity, Gemini via BrightData) - Submit job, poll for completion, retrieve results

This architecture prevents Inngest workflow timeouts when using polling-based providers.

---

## Provider Interface

```go
type AIProvider interface {
    // Sync execution (all providers)
    RunQuestion(ctx, query, websearch, location) (*AIResponse, error)
    RunQuestionWebSearch(ctx, query) (*AIResponse, error)
    RunQuestionBatch(ctx, queries, websearch, location) ([]*AIResponse, error)

    // Async execution (only if IsAsync() == true)
    SubmitBatchJob(ctx, queries, websearch, location) (jobID string, error)
    PollJobStatus(ctx, jobID) (status string, ready bool, error)
    RetrieveBatchResults(ctx, jobID, queries) ([]*AIResponse, error)

    // Provider metadata
    GetProviderName() string
    IsAsync() bool          // NEW: Indicates execution model
    SupportsBatching() bool
    GetMaxBatchSize() int
}
```

---

## Execution Models

### **Synchronous Providers** (IsAsync() = false)

**Providers:** OpenAI, Anthropic

**Behavior:**
- `RunQuestion()` - Executes immediately, returns response
- `RunQuestionBatch()` - Loops through `RunQuestion()` sequentially
- Async methods return errors (not supported)

**Workflow Integration:**
```go
// Single Inngest step
questionRuns, err := step.Run(ctx, "run-questions", func(ctx) {
    return provider.RunQuestionBatch(ctx, queries, websearch, location)
})
```

---

### **Asynchronous Providers** (IsAsync() = true)

**Providers:** ChatGPT, Perplexity, Gemini (all via BrightData)

**Behavior:**
- `SubmitBatchJob()` - Submits to BrightData, returns snapshot ID (~1s)
- `PollJobStatus()` - Checks if job is ready (~1s per check)
- `RetrieveBatchResults()` - Fetches and processes results (~5s)
- `RunQuestionBatch()` - Combines all three steps (sync wrapper for compatibility)

**Workflow Integration (3 separate steps):**
```go
// Step 1: Submit all batch jobs (~1s per batch)
jobIDs, err := step.Run(ctx, "submit-batch-jobs", func(ctx) {
    // Submit all batches, collect job IDs
    return submitAllBatches(queries)
})

// Step 2: Poll until ready (~1s per poll, total ~3-8 minutes)
_, err = step.Run(ctx, "poll-batch-jobs", func(ctx) {
    // Poll all jobs until status == "ready"
    return pollAllJobs(jobIDs)
})

// Step 3: Retrieve results (~5s per batch)
responses, err := step.Run(ctx, "retrieve-batch-results", func(ctx) {
    // Get results from all completed jobs
    return retrieveAllResults(jobIDs, queries)
})
```

---

## File Structure

### ChatGPT Provider (Example)
```
internal/providers/chatgpt/
├── provider.go    # Constructor, metadata (GetProviderName, IsAsync, etc.)
├── single.go      # RunQuestion, RunQuestionWebSearch
├── batch.go       # RunQuestionBatch (sync wrapper)
├── async.go       # SubmitBatchJob, PollJobStatus, RetrieveBatchResults
└── types.go       # Request/Response types
```

### Services (Legacy)
```
services/
├── brightdata_provider.go   # IsAsync() = true + async methods
├── perplexity_provider.go   # IsAsync() = true + async methods
├── gemini_provider.go        # IsAsync() = true + async methods
├── openai_provider.go        # IsAsync() = false + stub async methods
└── anthropic_provider.go     # IsAsync() = false + stub async methods
```

---

## Benefits

### 1. **No Timeouts**
- Each Inngest step completes in seconds
- Long polling happens in dedicated step (resumable)

### 2. **Resumable**
- If polling step fails, Inngest retries just that step
- Already-submitted jobs continue running

### 3. **Parallel Processing**
- Submit multiple batches in parallel
- Poll all jobs together
- Retrieve all results in one step

### 4. **Backward Compatible**
- Sync providers work unchanged
- `RunQuestionBatch()` still available for both models
- Async providers can be called synchronously if needed

---

## Usage Examples

### Detecting Provider Type
```go
provider, err := providers.NewProvider(modelName, cfg, costService)

if provider.IsAsync() {
    // Use async workflow (3 steps)
    jobID, err := provider.SubmitBatchJob(ctx, queries, true, location)
    // ... poll and retrieve ...
} else {
    // Use sync workflow (1 step)
    responses, err := provider.RunQuestionBatch(ctx, queries, true, location)
}
```

### Async Workflow Pattern
```go
// Step 1: Submit
jobID, err := step.Run(ctx, "submit-batch", func(ctx) {
    return provider.SubmitBatchJob(ctx, queries, websearch, location)
})

// Step 2: Poll (with retry logic)
_, err = step.Run(ctx, "poll-batch", func(ctx) {
    for {
        status, ready, err := provider.PollJobStatus(ctx, jobID)
        if err != nil {
            return err
        }
        if ready {
            return nil // Move to next step
        }
        time.Sleep(10 * time.Second)
    }
})

// Step 3: Retrieve
responses, err := step.Run(ctx, "retrieve-batch", func(ctx) {
    return provider.RetrieveBatchResults(ctx, jobID, queries)
})
```

---

## Implementation Status

✅ **Interface Updated** - All 9 methods defined
✅ **ChatGPT Provider** - Full async implementation in `internal/providers/chatgpt/`
✅ **Perplexity Provider** - Async methods added to `services/perplexity_provider.go`
✅ **Gemini Provider** - Async methods added to `services/gemini_provider.go`
✅ **BrightData Provider** - Async methods added to `services/brightdata_provider.go`
✅ **OpenAI Provider** - Stub async methods (not supported)
✅ **Anthropic Provider** - Stub async methods (not supported)

🚧 **TODO: Update Workflows** - Modify `network_processor.go` to use async steps for async providers

---

## Migration Path

1. **Phase 1: Interface** ✅ - Add `IsAsync()` and async methods
2. **Phase 2: Providers** ✅ - Implement async methods
3. **Phase 3: Services** 🚧 - Update `question_runner_service` to route async calls
4. **Phase 4: Workflows** 🚧 - Split async execution into 3 steps
5. **Phase 5: Testing** - Verify no timeouts with ChatGPT/Perplexity/Gemini

---

## Key Design Decisions

1. **Keep RunQuestionBatch()**: Provides sync wrapper for async providers (compatibility)
2. **Queries Parameter in Retrieve**: Needed for result matching/ordering
3. **Stub Methods for Sync**: Errors instead of panic (clearer debugging)
4. **Status + Ready Tuple**: Poll returns both status string and boolean flag
5. **No Intermediate Storage**: Job IDs passed between Inngest steps (simple, stateless)

---

## Performance Impact

**Before (Sync):**
- Single 8-minute Inngest step → timeout risk

**After (Async):**
- Step 1: Submit (~1s) ✅
- Step 2: Poll (~1s × 48 checks = ~8min) ✅ Resumable
- Step 3: Retrieve (~5s) ✅

**Total time:** Same, but each step completes quickly and is independently retryable.

