# Network Org Missing Evaluations Workflow

## Overview

This workflow identifies and processes all question runs that are missing `network_org_eval` records for a given organization. Unlike the standard `network.org.process` workflow which only processes question runs where `is_latest=true`, this workflow finds **all** question runs (regardless of `is_latest` status) that don't have corresponding evaluation records.

## Use Case

This workflow is designed for:
- **Backfilling missing evaluations**: When an org was added to a network after question runs were created
- **Data integrity**: Ensuring all historical question runs have evaluations for all orgs
- **Recovery**: Re-processing failed or skipped evaluations

## Architecture

### Components

1. **Service Method**: `GetMissingNetworkOrgQuestionRuns()`
   - Location: `services/question_runner_service.go`
   - Queries all question runs for a network
   - Checks each run for existing `network_org_eval` record for the given org
   - Returns only runs missing evaluations

2. **Workflow Processor**: `NetworkOrgMissingProcessor`
   - Location: `workflows/network_org_missing_processor.go`
   - Event trigger: `network.org.missing.process`
   - Function ID: `process-network-org-missing`

3. **Trigger Script**: `trigger_network_org_missing_workflow.py`
   - Reads org IDs from `example_orgs.txt`
   - Sends events to Inngest

### Workflow Steps

```
Step 1: Fetch Org Details
  ↓
Step 2: Find Missing Question Runs
  - Get all network question runs
  - Check each for existing network_org_eval
  - Filter to only missing evaluations
  ↓
Step 3-N: Process Each Missing Run
  - Extract network org data (AI call)
  - Store in network_org_evals
  - Store competitors
  - Store citations
  ↓
Final: Return Summary
```

## Key Differences from Standard Workflow

| Feature | Standard (`network.org.process`) | Missing (`network.org.missing.process`) |
|---------|----------------------------------|----------------------------------------|
| **Event Name** | `network.org.process` | `network.org.missing.process` |
| **Filter** | Only `is_latest=true` runs | All runs without evaluations |
| **Method** | `GetLatestNetworkQuestionRuns()` | `GetMissingNetworkOrgQuestionRuns()` |
| **Purpose** | Process current network data | Backfill missing historical data |
| **Typical Volume** | ~10-50 runs per org | Varies (could be 0 to hundreds) |

## Database Schema

### Queries Executed

The workflow queries the following tables:

1. **geo_questions**: Get network questions
2. **question_runs**: Get all runs for each question
3. **network_org_evals**: Check if evaluation exists for (question_run_id, org_id)

### Stores Results In

- `network_org_evals`: Primary evaluation record
- `network_org_competitors`: Extracted competitors
- `network_org_citations`: Extracted citations

## Usage

### Triggering the Workflow

#### Bulk Processing (All Orgs)
```bash
python trigger_network_org_missing_workflow.py
```

This reads org IDs from `example_orgs.txt` and triggers the workflow for each.

#### Single Org Processing
```python
import requests

payload = {
    "name": "network.org.missing.process",
    "data": {
        "org_id": "your-org-id-here",
        "triggered_by": "manual"
    }
}

response = requests.post(
    "http://localhost:8288/e/dev",
    json=payload,
    headers={"Content-Type": "application/json"}
)
```

### Configuration

Edit `trigger_network_org_missing_workflow.py`:

```python
ORG_FILE = "example_orgs.txt"  # File with org IDs
TRIGGERED_BY = "manual"         # Trigger source
USER_ID = None                  # Optional user ID
INNGEST_URL = "http://localhost:8288/e/dev"  # Inngest endpoint
```

## Implementation Details

### Efficient Single-Query Approach

The workflow uses an optimized repository method that finds missing evaluations in a single SQL query:

```go
// Single efficient query using LEFT JOIN
missingRuns, err := s.repos.NetworkOrgEvalRepo.GetMissingQuestionRunsForOrg(
    ctx, networkUUID, orgUUID
)
```

**SQL Behind the Scenes**:
```sql
SELECT qr.question_run_id, gq.question_text, qr.response_text
FROM question_runs qr
JOIN geo_questions gq ON qr.geo_question_id = gq.geo_question_id
LEFT JOIN network_org_evals noe 
    ON noe.question_run_id = qr.question_run_id 
    AND noe.org_id = $1
WHERE gq.network_id = $2
    AND noe.network_org_eval_id IS NULL
```

**Performance**: Optimized for networks of any size. A network with 1000 question runs is processed with just 1 database query instead of 1001 queries (1000x improvement!).

### Deduplication Strategy

Uses `ProcessNetworkOrgQuestionRunWithCleanup()` which:
1. Deletes existing evaluation data for (question_run_id, org_id)
2. Extracts fresh data from AI
3. Stores new records

This ensures idempotency - running the workflow multiple times is safe.

## Monitoring

### Success Indicators

Look for these log messages:

```
[ProcessNetworkOrgMissing] Found X question runs missing evaluations
[ProcessNetworkOrgMissing] ✅ COMPLETED: Network org missing evaluation processing
[ProcessNetworkOrgMissing] 📊 Data stored: X missing evaluations processed
```

### Early Exit Condition

If no missing evaluations are found:

```
[ProcessNetworkOrgMissing] ✅ No missing evaluations found for org {org_id}
```

The workflow completes successfully without processing any runs.

## Error Handling

- **Invalid Org ID**: Returns error in Step 1
- **Network Not Found**: Returns error in Step 2
- **Individual Run Failure**: Logs warning, continues with next run
- **Retries**: 3 automatic retries (configured in Inngest)

## Cost Considerations

Each missing evaluation requires:
- 1 AI call to extract org data (~$0.01-0.05)
- Database writes for eval/competitors/citations

**Example**: If 100 question runs are missing evaluations, expect ~$1-5 in AI costs.

## Related Workflows

- **`network.org.process`**: Process latest evaluations only
- **`network.org.reeval.enhanced`**: Re-evaluate ALL runs (uses org evaluation methodology)
- **`network.process`**: Create new network question runs

## Testing

### Local Testing

1. Start the workflow service:
```bash
go run main.go
```

2. Trigger for a single org:
```bash
curl -X POST http://localhost:8288/e/dev \
  -H "Content-Type: application/json" \
  -d '{
    "name": "network.org.missing.process",
    "data": {
      "org_id": "5eecfd56-13ca-4bb0-b3d2-18fc0c8ac4c9",
      "triggered_by": "manual_test"
    }
  }'
```

3. Check Inngest UI at `http://localhost:8288`

### Verify Results

Query the database:

```sql
-- Check evaluations created
SELECT COUNT(*) 
FROM network_org_evals 
WHERE org_id = 'your-org-id-here';

-- Check latest processing
SELECT 
    noe.network_org_eval_id,
    noe.mentioned,
    noe.citation,
    noe.created_at
FROM network_org_evals noe
WHERE noe.org_id = 'your-org-id-here'
ORDER BY noe.created_at DESC
LIMIT 10;
```

## Troubleshooting

### "No missing evaluations found" but expected some

1. Check if org is part of the network:
```sql
SELECT network_id FROM orgs WHERE org_id = 'your-org-id-here';
```

2. Verify question runs exist:
```sql
SELECT COUNT(*) 
FROM question_runs qr
JOIN geo_questions gq ON qr.geo_question_id = gq.geo_question_id
WHERE gq.network_id = 'your-network-id-here';
```

3. Check existing evaluations:
```sql
SELECT COUNT(*) 
FROM network_org_evals 
WHERE org_id = 'your-org-id-here';
```

### Slow Processing (AI Calls, Not Query Speed)

- The database query is fast (single optimized query)
- Processing time is dominated by AI extraction calls (~1-5 seconds per run)
- For 100 missing evaluations, expect 2-10 minutes total processing time
- This is expected and unavoidable (AI processing time)

## Future Enhancements

Potential improvements:

1. **Batch processing**: Process multiple orgs in parallel
2. **Progress tracking**: Store processing state for very large backlogs
3. **Incremental processing**: Add time-based filtering to only check recent question runs

## Summary

This workflow fills a critical gap in the network org evaluation pipeline by:
- ✅ Finding all question runs without evaluations
- ✅ Processing historical data retroactively
- ✅ Ensuring data completeness across all orgs
- ✅ Operating safely with deduplication
- ✅ Working within existing repository constraints
