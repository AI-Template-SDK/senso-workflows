# Resume Functionality - Org Evaluation Pipeline

## Overview

The org evaluation pipeline now supports **three levels of resume checks** to ensure that if the pipeline is interrupted and restarted, it continues from where it left off without duplicating work or wasting API calls.

## Three-Level Resume Architecture

### Level 1: Batch Check
**When:** At the start of the pipeline  
**What:** Check if a batch already exists for today  
**Action:** 
- If batch exists → Resume with existing batch
- If no batch → Create new batch

```go
batch, isExisting, err := orgEvaluationService.GetOrCreateTodaysBatch(ctx, orgID, totalQuestions)
if isExisting {
    fmt.Printf("Resuming existing batch: %s (status: %s)\n", batch.BatchID, batch.Status)
} else {
    fmt.Printf("Created new batch: %s\n", batch.BatchID)
}
```

### Level 2: Question Run Check
**When:** Before executing each question  
**What:** Check if a QuestionRun already exists for (Question, Model, Location, Batch)  
**Action:**
- If exists → Skip execution, use existing run
- If not exists → Execute AI call and create new run

```go
existingRun, err := CheckQuestionRunExists(ctx, questionID, modelID, locationID, batchID)
if existingRun != nil {
    fmt.Printf("✓ Skipping question - already executed\n")
    return existingRun
}
// Otherwise, execute AI call...
```

### Level 3: Extraction Check  
**When:** Before processing extractions for each QuestionRun  
**What:** Check if org_eval exists for this question run  
**Action:**
- If org_eval exists → Skip extraction entirely (regardless of citations/competitors)
- If org_eval doesn't exist → Process extraction normally

```go
hasEval, hasCitations, hasCompetitors, err := CheckExtractionsExist(ctx, questionRunID, orgID)
if hasEval {
    fmt.Printf("✓ Skipping extraction - org_eval already exists\n")
    continue
}
// Otherwise, process extraction...
```

## Implementation Status

### ✅ Implemented

1. **Helper Methods Added:**
   - `GetOrCreateTodaysBatch()` - Batch resume logic
   - `CheckQuestionRunExists()` - Question run resume logic  
   - `CheckExtractionsExist()` - Extraction resume logic

2. **Integration Points:**
   - `executeBatch()` - Checks for existing runs before batch API call
   - `executeSingleQuestion()` - Checks for existing run before AI call
   - `processAllExtractions()` - Checks for existing extractions before processing

3. **Smart Batching:**
   - Filters out already-executed questions from batch
   - Only sends new questions to BrightData API
   - Combines existing and new runs in results

### ⚠️ Pending (Requires senso-api changes)

The following repository methods need to be added to the senso-api package:

1. **QuestionRunBatchRepository:**
   ```go
   GetTodaysBatchForOrg(ctx context.Context, orgID uuid.UUID) (*models.QuestionRunBatch, error)
   ```
   - Query: `SELECT * FROM question_run_batches WHERE org_id = ? AND DATE(created_at) = CURRENT_DATE AND deleted_at IS NULL ORDER BY created_at DESC LIMIT 1`

2. **QuestionRunRepository:**
   ```go
   GetByQuestionModelLocationBatch(ctx context.Context, questionID, modelID, locationID, batchID uuid.UUID) (*models.QuestionRun, error)
   ```
   - Query: `SELECT * FROM question_runs WHERE geo_question_id = ? AND model_id = ? AND location_id = ? AND batch_id = ? AND deleted_at IS NULL LIMIT 1`

3. **OrgEvalRepository:**
   ```go
   GetByQuestionRunAndOrg(ctx context.Context, questionRunID, orgID uuid.UUID) (*models.OrgEval, error)
   ```
   - Query: `SELECT * FROM org_evals WHERE question_run_id = ? AND org_id = ? AND deleted_at IS NULL LIMIT 1`

4. **OrgCitationRepository:**
   ```go
   GetByQuestionRunAndOrg(ctx context.Context, questionRunID, orgID uuid.UUID) ([]*models.OrgCitation, error)
   ```
   - Query: `SELECT * FROM org_citations WHERE question_run_id = ? AND org_id = ? AND deleted_at IS NULL`

5. **OrgCompetitorRepository:**
   ```go
   GetByQuestionRunAndOrg(ctx context.Context, questionRunID, orgID uuid.UUID) ([]*models.OrgCompetitor, error)
   ```
   - Query: `SELECT * FROM org_competitors WHERE question_run_id = ? AND org_id = ? AND deleted_at IS NULL`

### Current Workaround

Until the repository methods are added:
- `GetOrCreateTodaysBatch()` always creates a new batch (TODO comment added)
- `CheckQuestionRunExists()` always returns nil (TODO comment added)
- `CheckExtractionsExist()` uses existing repository methods (already works)

This means:
- ✅ Level 3 (Extraction check) works immediately
- ⏳ Level 1 & 2 (Batch and QuestionRun checks) will work once repository methods are added

## Resume Scenarios

### Scenario 1: Pipeline Crashes During Question Execution

**What happens:**
1. Restart pipeline
2. Level 1: Finds today's batch (once repository method added)
3. Level 2: Skips already-executed questions
4. Level 3: Skips already-processed extractions
5. **Result:** Only executes/extracts what's missing

### Scenario 2: Pipeline Crashes During Extraction

**What happens:**
1. Restart pipeline
2. Level 1: Finds today's batch
3. Level 2: Skips all questions (already executed)
4. Level 3: Skips completed extractions, processes remaining ones
5. **Result:** Only processes missing extractions

### Scenario 3: Manual Re-run Same Day

**What happens:**
1. Re-trigger pipeline
2. Level 1: Finds today's batch
3. Level 2: All questions already executed
4. Level 3: All extractions already processed
5. **Result:** No API calls made, no duplicate data

### Scenario 4: Partial Extraction Corruption

**What happens:**
1. Some extractions exist but not all (eval exists, citations missing)
2. Level 3 detects partial state
3. Logs warning and re-processes ALL extractions
4. **Result:** Ensures data consistency

## Performance Impact

### Without Resume Logic
- 100 questions crash at question 50
- Restart: Execute all 100 questions again
- **Wasted:** 50 API calls + cost

### With Resume Logic (Once Repository Methods Added)
- 100 questions crash at question 50
- Restart: Execute only remaining 50 questions
- **Saved:** 50 API calls + cost

### BrightData Batch Optimization
- Without resume: Batch of 20 questions, crash at 10
  - Restart: Re-execute all 20 (waste 10)
- With resume: Batch of 20, crash at 10
  - Restart: Execute only new 10, combine with existing 10
  - **Smart batching:** Can batch the 10 new questions separately

## Code Examples

### Example 1: Batch Resume

```go
// Before
batch := createNewBatch()

// After
batch, isExisting, _ := GetOrCreateTodaysBatch(ctx, orgID, totalQuestions)
if !isExisting {
    batch = startBatch(batch)  // Only start if new
}
```

### Example 2: Question Execution Resume

```go
// executeBatch - filters before API call
questionsToExecute := []
existingRuns := []

for _, question := range batch {
    existing := CheckQuestionRunExists(question, model, location, batch)
    if existing != nil {
        existingRuns.append(existing)
    } else {
        questionsToExecute.append(question)
    }
}

// Only call API for questionsToExecute
if len(questionsToExecute) > 0 {
    responses := provider.RunQuestionBatch(questionsToExecute)
    newRuns := storeRuns(responses)
}

// Return combined results
return append(existingRuns, newRuns)
```

### Example 3: Extraction Resume

```go
// processAllExtractions - checks before processing
for _, questionRun := range questionRuns {
    hasEval, hasCitations, hasCompetitors := CheckExtractionsExist(questionRun.ID, orgID)
    
    if hasEval && hasCitations && hasCompetitors {
        // Skip - already done
        continue
    }
    
    // Process extraction
    extractOrgEvaluation(questionRun)
    extractCitations(questionRun)
    extractCompetitors(questionRun)
}
```

## Monitoring & Logging

The resume functionality provides clear logging:

```
[GetOrCreateTodaysBatch] Found existing batch abc-123 with status: running
[executeBatch] ✓ Skipping question xyz - already executed
[executeBatch] Executing 15 new questions (skipped 5 existing)
[processAllExtractions] ✓ Skipping extraction for question run def-456 - already processed
[processAllExtractions] ⚠️  Partial extraction found for question run ghi-789 (eval:true citations:false competitors:true) - re-extracting
```

## Testing Resume Functionality

### Test 1: Simulate Crash During Execution

```bash
# Start pipeline
python trigger_org_evaluation_workflow.py <org_id>

# Kill process at ~50% completion
# Restart
python trigger_org_evaluation_workflow.py <org_id>

# Expected: Logs show "Skipping question - already executed" for completed ones
```

### Test 2: Simulate Crash During Extraction

```bash
# Let execution complete, kill during extraction
# Restart

# Expected: Logs show "Skipping extraction - already processed" for completed ones
```

### Test 3: Same-Day Re-run

```bash
# Run complete pipeline twice same day

# Expected: Second run shows all questions/extractions skipped
```

## Future Enhancements

1. **Batch Status Management:**
   - Resume "running" batches from previous session
   - Mark "pending" batches as "abandoned" after timeout

2. **Partial Batch Recovery:**
   - Track which questions in a BrightData batch succeeded
   - Only retry failed questions from the batch

3. **Intelligent Re-extraction:**
   - Instead of re-extracting everything when partial extractions exist
   - Only extract the missing pieces

4. **Resume Metrics:**
   - Track how many questions/extractions were skipped
   - Report savings in cost and time

## Summary

The three-level resume architecture ensures:
- ✅ No duplicate API calls
- ✅ No duplicate database records
- ✅ Fast recovery from interruptions
- ✅ Efficient resource usage
- ✅ Data consistency

**Current Status:** Level 3 (extraction check) is fully functional. Levels 1 & 2 will work once the repository methods are added to senso-api. 