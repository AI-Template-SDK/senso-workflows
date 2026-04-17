# PRD: Standalone Org Evaluation Pipeline (No Inngest)

## Problem Statement

The current org evaluation pipeline is orchestrated via Inngest, which introduces:
- Rate limits and concurrency constraints imposed by Inngest's execution model
- Step function overhead and serialization/deserialization of state between steps
- Inability to fine-tune parallelism at the org, question-run, and eval levels independently
- Debugging friction — errors surface in the Inngest dashboard rather than local logs
- Cold-start and queueing delays when processing many orgs

We need a standalone Go CLI that can be run directly (locally or on a server) to process orgs from `example_orgs.txt` with full control over parallelism.

---

## Current Pipeline (Inngest) — How It Works Today

The Inngest workflow (`org.evaluation.process`) is triggered per org via a Python script (`trigger_org_evaluation_workflow.py`) that sends events sequentially. Each org goes through:

### Step 1: Get or Create Batch
- Calls `OrgEvaluationService.GetOrCreateTodaysBatch(orgID, totalQuestions)`
- If a batch for today already exists, it resumes (idempotent)
- Calculates `totalQuestions = len(questions) × len(models) × len(locations)`

### Step 1.5: Check Partner Balance
- Calls `UsageService.CheckBalance(orgID, totalQuestions, "org")`
- Looks up the org's partner, finds wholesale pricing config, calculates estimated cost
- Checks credit balance (org-level or partner-level depending on `IsFreeTier`)
- If insufficient funds → mark batch as failed, report to Slack, abort

### Step 2: Start Batch
- Updates batch status to `"running"` (only if new, skips for resumed batches)

### Step 3: Run Question Matrix With Org Evaluation
This is the core processing step. It calls `OrgEvaluationService.RunQuestionMatrixWithOrgEvaluation()` which has 3 phases:

#### Phase 1: Generate Name Variations (once per org)
- Calls `GenerateNameVariations(orgName, websites)` — an OpenAI/Azure structured output call
- Generates 15-25 realistic brand name variations for fuzzy matching
- Uses `gpt-4.1-mini` (or Azure deployment)

#### Phase 2: Execute Questions (grouped by model×location)
- Creates all `ModelLocationPair` combinations
- For each pair, gets the appropriate `AIProvider`:
  - **BrightData ChatGPT** (`SupportsBatching=true`, max batch=1 currently, was 20) — async trigger → poll → results
  - **Perplexity** (via BrightData, `SupportsBatching=true`) — same async pattern
  - **Gemini** (via BrightData, `SupportsBatching=true`) — same async pattern
  - **AIOverview** (BrightData SERP API, `SupportsBatching=false`) — **synchronous, one at a time** ← THIS IS THE BOTTLENECK
  - **OpenAI** (direct API, `SupportsBatching=false`) — sequential
  - **Anthropic** (direct API, `SupportsBatching=false`) — sequential
  - **Linkup** (`SupportsBatching=false`) — sequential
- Resume support: `CheckQuestionRunExists()` skips already-completed question runs
- Each question run is stored in DB immediately after execution

#### Phase 3: Process Extractions (eval per question run, sequential)
- For each question run:
  1. **Check for existing extractions** (`CheckExtractionsExist`) — skip if already done
  2. **Quick mention check**: string-match response text against name variations (no LLM call)
  3. **If mentioned → Extract Org Evaluation** (OpenAI/Azure structured output call):
     - Verify mention, extract mention text, determine sentiment
     - Store `OrgEval` record
  4. **If not mentioned → Store minimal `OrgEval`** (mentioned=false, no LLM call)
  5. **Always Extract Competitors** (OpenAI `gpt-4.1-mini` structured output call):
     - Identify all competitor brands in the response
     - Store each `OrgCompetitor` record
  6. **Always Extract Citations** (no LLM call — regex URL extraction with `xurls`):
     - Find all URLs, classify as primary/secondary based on org domains
     - Store each `OrgCitation` record
  7. Update batch progress

### Step 4: Track Usage
- `UsageService.TrackBatchUsage()` — creates idempotent credit ledger entries for successful runs

### Step 5: Complete Batch
- Updates batch status to `"completed"`
- Manages `is_latest` flags for question runs

### Step 6: Return Summary
- Aggregate stats: total processed, evaluations, citations, competitors, cost

---

## Existing Service Layer (Reusable — DO NOT REWRITE)

The following services/types can be imported and used directly in the new CLI:

| Service | Key Methods |
|---------|------------|
| `services.OrgService` | `GetOrgDetails(ctx, orgID)` → `*RealOrgDetails` |
| `services.OrgEvaluationService` | `GetOrCreateTodaysBatch`, `StartBatch`, `CompleteBatch`, `FailBatch`, `RunQuestionMatrixWithOrgEvaluation`, `GenerateNameVariations`, `ExtractOrgEvaluation`, `ExtractCompetitors`, `ExtractCitations`, `CheckQuestionRunExists`, `CheckExtractionsExist`, `UpdateBatchProgress` |
| `services.UsageService` | `CheckBalance`, `TrackBatchUsage` |
| `services.RepositoryManager` | All DB repos (org, question, model, location, question_run, org_eval, org_citation, org_competitor, batch, ledger, etc.) |
| `config.Config` | Loads all env vars (API keys, DB config, dataset IDs) |
| `AIProvider` interface | `RunQuestion`, `RunQuestionBatch`, `SupportsBatching`, `GetMaxBatchSize` |
| Provider implementations | `BrightDataProvider`, `PerplexityProvider`, `GeminiProvider`, `AIOverviewProvider`, `OpenAIProvider`, `AnthropicProvider`, `LinkupProvider` |

The new script should **import and compose these services** — not duplicate their logic.

---

## Proposed Architecture

### Entry Point
`cmd/org_eval_runner/main.go` — a standalone CLI binary

### Command-Line Interface
```
go run cmd/org_eval_runner/main.go [flags]

Flags:
  --orgs-file string       Path to org IDs file (default "example_orgs.txt")
  --org-workers int        Max concurrent org pipelines (default 20)
  --aio-workers int        Max concurrent AIOverview calls per org (default 20)
  --eval-workers int       Max concurrent eval extractions per org (default 5)
  --dry-run                Print org count and exit without processing
  --single-org string      Process a single org ID (ignores --orgs-file)
```

### Concurrency Model

```
                    ┌─────────────────────────────────────┐
                    │         Main Goroutine               │
                    │  Reads example_orgs.txt              │
                    │  Launches org worker pool (20)       │
                    └──────────┬──────────────────────────┘
                               │
              ┌────────────────┼────────────────┐
              ▼                ▼                 ▼
     ┌──────────────┐ ┌──────────────┐  ┌──────────────┐
     │  Org Worker 1 │ │  Org Worker 2 │  │ Org Worker N │  (up to 20 concurrent)
     │               │ │               │  │              │
     │  1. Get/Create│ │               │  │              │
     │     Batch     │ │               │  │              │
     │  2. Check     │ │               │  │              │
     │     Balance   │ │               │  │              │
     │  3. Start     │ │               │  │              │
     │     Batch     │ │               │  │              │
     │  4. Phase 1:  │ │               │  │              │
     │     Name Vars │ │               │  │              │
     │  5. Phase 2:  │ │               │  │              │
     │     Questions │ │               │  │              │
     │  6. Phase 3:  │ │               │  │              │
     │     Evals     │ │               │  │              │
     │  7. Track     │ │               │  │              │
     │     Usage     │ │               │  │              │
     │  8. Complete  │ │               │  │              │
     │     Batch     │ │               │  │              │
     └──────────────┘ └──────────────┘  └──────────────┘
```

#### Level 1: Org-level parallelism (pool of `--org-workers`)
- A semaphore-based worker pool processes orgs from the file concurrently
- Each org runs the full pipeline independently
- Use `sync.WaitGroup` + buffered channel semaphore

#### Level 2: Question execution parallelism (within each org, Phase 2)
- **Batching providers** (BrightData, Perplexity, Gemini): Already handle batching internally via the provider's `RunQuestionBatch`. These are I/O bound (trigger → poll → results) and can run the existing batched flow as-is.
- **AIOverview (the key bottleneck)**: Currently runs sequentially (`SupportsBatching=false`, `RunQuestion` one at a time). The new script should **parallelize AIOverview calls** using a semaphore pool of `--aio-workers` (default 20). Each AIOverview call is independent (synchronous HTTP to BrightData SERP API, ~5-10s per call). With 20 workers, a set of 20 questions finishes in ~10s instead of ~200s.
- **OpenAI/Anthropic/Linkup**: These are direct API calls. They can also be parallelized with the same semaphore, though they're typically fewer in number.

**Implementation approach for Phase 2 parallelism:**
Rather than modifying the existing `executeAllQuestions` method (which processes model-location pairs sequentially), the new script should:
1. Compute all `(question, model, location)` jobs upfront
2. Group jobs by whether the provider supports batching
3. For batching providers: submit batches as today (they internally handle async trigger/poll)
4. For non-batching providers (especially AIOverview): fan out jobs into a bounded worker pool (`--aio-workers` goroutines), each calling `provider.RunQuestion()` directly
5. Collect all results, store question runs in DB

#### Level 3: Evaluation parallelism (within each org, Phase 3)
- Process extractions with a semaphore pool of `--eval-workers` (default 5) per org
- Each extraction involves 1-2 LLM calls (org eval + competitors) + URL parsing (citations)
- With 5 workers per org × 20 orgs = up to 100 concurrent eval LLM calls globally
- The extraction steps per question run are:
  1. Quick mention check (string match, no LLM) — instant
  2. `ExtractOrgEvaluation` (LLM call if mentioned) — ~2-5s
  3. `ExtractCompetitors` (LLM call, always) — ~2-5s
  4. `ExtractCitations` (URL regex, no LLM) — instant
  - Steps 2 and 3 within a single question run should be run **sequentially** (they share the same response text and both write to the summary)
  - But multiple question runs should be evaluated **in parallel** across the `--eval-workers` pool

---

## Detailed Implementation Plan

### 1. CLI Setup & Configuration (`cmd/org_eval_runner/main.go`)

```
- Parse CLI flags
- Load config (config.Load())
- Load .env / dev.env (godotenv)
- Connect to database (same as main.go's createDatabaseClient)
- Initialize RepositoryManager
- Initialize services: OrgService, OrgEvaluationService, UsageService, DataExtractionService
- Read org IDs from file
- Launch pipeline
```

### 2. Pipeline Orchestrator

```go
func runPipeline(ctx context.Context, orgIDs []string, cfg PipelineConfig) *PipelineReport {
    sem := make(chan struct{}, cfg.OrgWorkers)
    var wg sync.WaitGroup
    results := make(chan OrgResult, len(orgIDs))

    for _, orgID := range orgIDs {
        wg.Add(1)
        sem <- struct{}{} // acquire slot
        go func(id string) {
            defer wg.Done()
            defer func() { <-sem }() // release slot
            result := processOrg(ctx, id, cfg)
            results <- result
        }(orgID)
    }

    // Wait and collect
    go func() { wg.Wait(); close(results) }()

    report := &PipelineReport{}
    for r := range results {
        report.Add(r)
    }
    return report
}
```

### 3. Per-Org Processing (`processOrg`)

The per-org function mirrors the existing Inngest steps but runs inline:

```
func processOrg(ctx, orgID, cfg) OrgResult:
    1. orgDetails = orgService.GetOrgDetails(orgID)
    2. totalQuestions = len(questions) × len(models) × len(locations)
    3. batch, isExisting = orgEvalService.GetOrCreateTodaysBatch(orgID, totalQuestions)
    4. usageService.CheckBalance(orgID, totalQuestions, "org")  // fail fast
    5. orgEvalService.StartBatch(batchID)  // only if new
    6. nameVariations = orgEvalService.GenerateNameVariations(orgName, websites)
    7. questionRuns = executeQuestionsParallel(ctx, orgDetails, batch, cfg)  // NEW
    8. processExtractionsParallel(ctx, questionRuns, orgID, orgName, websites, nameVariations, batch, cfg)  // NEW
    9. usageService.TrackBatchUsage(orgID, batchID, "org")
   10. orgEvalService.CompleteBatch(batchID)
   11. Update is_latest flags
```

### 4. Parallel Question Execution (`executeQuestionsParallel`) — Phase 2

```
func executeQuestionsParallel(ctx, orgDetails, batchID, cfg) []*QuestionRun:
    // Build all jobs: (question, model, location) triples
    allJobs = buildAllJobs(orgDetails)

    // Separate into batching vs non-batching groups
    batchingJobs = groupByProvider(allJobs, supportsBatching=true)
    nonBatchingJobs = groupByProvider(allJobs, supportsBatching=false)

    var allRuns []*QuestionRun
    var mu sync.Mutex

    // A) Process batching providers (existing flow — sequential per model×location pair,
    //    but the batch API call itself handles multiple questions)
    for pair, jobs := range batchingJobs {
        provider = getProvider(pair.Model.Name)
        runs = executeBatch(ctx, jobs, pair, provider, batchID)  // existing method
        mu.Lock()
        allRuns = append(allRuns, runs...)
        mu.Unlock()
    }

    // B) Process non-batching providers in parallel (the key improvement)
    sem := make(chan struct{}, cfg.AIOWorkers)  // e.g., 20
    var wg sync.WaitGroup

    for _, job := range nonBatchingJobs {
        // Check if already exists (resume support)
        existing = checkQuestionRunExists(job.QuestionID, job.ModelID, job.LocationID, batchID)
        if existing != nil {
            mu.Lock()
            allRuns = append(allRuns, existing)
            mu.Unlock()
            continue
        }

        wg.Add(1)
        sem <- struct{}{}
        go func(j Job) {
            defer wg.Done()
            defer func() { <-sem }()

            provider = getProvider(j.Model.Name)
            response = provider.RunQuestion(ctx, j.QuestionText, true, location)
            run = createAndStoreQuestionRun(response, j, batchID)

            mu.Lock()
            allRuns = append(allRuns, run)
            mu.Unlock()
        }(job)
    }
    wg.Wait()

    return allRuns
```

### 5. Parallel Extraction Processing (`processExtractionsParallel`) — Phase 3

```
func processExtractionsParallel(ctx, questionRuns, orgID, orgName, websites, nameVariations, batchID, cfg):
    sem := make(chan struct{}, cfg.EvalWorkers)  // e.g., 5
    var wg sync.WaitGroup
    summary := &OrgEvaluationSummary{} // thread-safe with mutex

    for _, qr := range questionRuns {
        wg.Add(1)
        sem <- struct{}{}
        go func(questionRun *QuestionRun) {
            defer wg.Done()
            defer func() { <-sem }()

            // Skip if extractions already exist
            hasEval, _, _ = checkExtractionsExist(questionRun.ID, orgID)
            if hasEval { return }

            // Run extraction pipeline (sequential within one question run)
            processQuestionRunWithOrgEvaluation(ctx, questionRun, orgID, orgName, websites, nameVariations, summary)

            // Update batch progress
            orgEvalService.UpdateBatchProgress(batchID, 1, 0)
        }(qr)
    }
    wg.Wait()
```

### 6. Error Handling & Reporting

- **Per-org errors**: Catch panics, log errors, continue processing other orgs. One org failure should not stop the pipeline.
- **Per-question errors**: Same as today — log and continue. Store error in summary.
- **Slack alerts**: Call `ReportPipelineFailureToSlack` on org-level failures (same as today)
- **Final report**: Print summary table at the end:
  ```
  ============ PIPELINE COMPLETE ============
  Total orgs: 88
  Successful: 85
  Failed: 3
  Total question runs: 4,250
  Total evaluations: 4,250
  Total citations: 12,340
  Total competitors: 8,120
  Total cost: $45.23
  Duration: 12m 34s
  ==========================================
  ```

### 7. Graceful Shutdown

- Trap `SIGINT`/`SIGTERM`
- Cancel context → all in-flight goroutines check `ctx.Done()`
- Wait for current goroutines to finish (with timeout)
- Log partial progress
- Batch status will be left as "running" — next run will resume via `GetOrCreateTodaysBatch`

---

## Database Connection Pool Considerations

With 20 org workers × (question execution + eval extraction), we could have many concurrent DB operations. The current config defaults:
- `MaxOpenConns: 25`
- `MaxIdleConns: 25`

This should be increased for the standalone runner:
- `MaxOpenConns: 100` (or configurable via `--db-max-conns`)
- `MaxIdleConns: 50`

This prevents connection starvation when 20 orgs are all writing question runs simultaneously.

---

## What Changes vs. Existing Code

| Aspect | Current (Inngest) | New (Standalone) |
|--------|-------------------|------------------|
| Orchestration | Inngest step functions | Go goroutines + semaphores |
| Org parallelism | 1 org per Inngest event (parallel depends on Inngest concurrency limits) | Configurable pool (default 20) |
| AIOverview execution | Sequential (1 at a time per model×location pair) | Parallel pool (default 20 concurrent) |
| Eval extraction | Sequential (1 question run at a time) | Parallel pool (default 5 per org) |
| Trigger mechanism | Python script → Inngest event | Direct CLI execution |
| Resume support | Inngest step memoization + batch checking | Same batch checking (reused) |
| Monitoring | Inngest dashboard | stdout/stderr logs + Slack alerts |
| Retry logic | Inngest auto-retry (3 retries) | Manual retry per-question (configurable) |

---

## What Does NOT Change

- All service layer code (OrgEvaluationService, UsageService, OrgService, providers)
- All database models and repositories
- All AI provider implementations (BrightData, OpenAI, Anthropic, etc.)
- The batch management logic (create/start/complete/fail/resume)
- The extraction pipeline (name variations → mention check → org eval → competitors → citations)
- The usage tracking and credit balance checking
- Slack alerting
- The `example_orgs.txt` file format

---

## File Structure

```
cmd/org_eval_runner/
    main.go           # CLI entry point, flag parsing, service init
    pipeline.go       # Pipeline orchestrator (runPipeline, processOrg)
    parallel.go       # Parallel question execution and eval extraction
    report.go         # Result collection and summary reporting
```

---

## Open Questions / Decisions

1. **Global rate limiting for AI providers**: With 20 orgs × 20 AIO workers = up to 400 concurrent BrightData SERP calls. Do we need a global semaphore across all orgs for the SERP API, or is per-org sufficient? → **Recommendation**: Start with per-org semaphores. If we hit BrightData rate limits, add a global semaphore.

2. **Progress persistence**: The existing batch system provides resume capability. Should we add a local progress file as well (e.g., `progress.json`)? → **Recommendation**: No — the DB batch system is sufficient and already battle-tested.

3. **Batching provider batch size**: BrightData `GetMaxBatchSize()` is currently hardcoded to 1 (was 20). Should we restore batch size 20 in the new runner? → **Recommendation**: Yes, restore to 20 for the standalone runner. The batch-of-1 was likely a workaround for Inngest step size limits.

4. **Should this replace the Inngest pipeline entirely?**: → **Recommendation**: Run both in parallel initially. The standalone runner is for bulk/batch operations. The Inngest pipeline can remain for on-demand single-org triggers from the app UI. Eventual migration to standalone-only once proven stable.
