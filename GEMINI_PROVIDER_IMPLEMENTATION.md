# Gemini Provider Implementation

## Summary

Added full support for Google Gemini via BrightData to the org evaluation pipeline. Gemini now has complete feature parity with ChatGPT and Perplexity providers.

## Changes Made

### 1. New Files Created

#### `services/gemini_provider.go`
Complete Gemini provider implementation with:
- ✅ Single question execution (`RunQuestion`)
- ✅ Batch execution up to 20 queries (`RunQuestionBatch`)
- ✅ Web search support (`RunQuestionWebSearch`)
- ✅ Async polling with 10-second intervals
- ✅ Retry logic for snapshot building (20 retries, 30s intervals)
- ✅ Index-based result mapping with prompt-text fallback
- ✅ Error handling with placeholder text for failed runs
- ✅ Location/country mapping (US, CA, GB, AU, DE, FR, IT, ES, NL, JP, KR, IN, BR, MX)
- ✅ Comprehensive logging and debugging
- ✅ Error file saving for troubleshooting
- ✅ Cost tracking at $0.0015 per API call

### 2. Modified Files

#### `internal/config/config.go`
- Added `GeminiDatasetID string` field to Config struct
- Added `GeminiDatasetID: os.Getenv("GEMINI_DATASET_ID")` to Load() function

#### `services/org_evaluation_service.go`
- Updated `getProvider()` to detect "gemini" in model names
- Returns `NewGeminiProvider()` for Gemini models

#### `services/question_runner_service.go`
- Updated `getProvider()` to detect "gemini" in model names
- Returns `NewGeminiProvider()` for Gemini models

## Configuration

### Environment Variables

Add to your `.env` or deployment configuration:

```bash
GEMINI_DATASET_ID=gd_mbz66arm2mf9cu856y
```

The existing `BRIGHTDATA_API_KEY` is shared across all BrightData providers (ChatGPT, Perplexity, Gemini).

### API Details

- **Base URL**: `https://api.brightdata.com/datasets/v3`
- **Dataset URL**: `https://gemini.google.com/`
- **Authentication**: Bearer token via `BRIGHTDATA_API_KEY`
- **Dataset ID**: Configured via `GEMINI_DATASET_ID`

## Usage

### 1. Database Model Registration

Add the Gemini model to your database:

```sql
INSERT INTO geo_models (geo_model_id, name, provider, active, created_at, updated_at)
VALUES (
    gen_random_uuid(),
    'gemini',
    'brightdata',
    true,
    NOW(),
    NOW()
);
```

### 2. Add to Organization Configuration

In your application, add the Gemini model to organizations that should use it.

### 3. Run Org Evaluation Pipeline

```bash
python trigger_org_evaluation_workflow.py
```

The system will automatically:
1. Detect "gemini" in the model name
2. Create a GeminiProvider instance
3. Batch up to 20 questions per API call
4. Poll until results are ready
5. Process evaluations, citations, and competitors
6. Store everything in the database

## Request Format

### Single Question
```json
[
  {
    "url": "https://gemini.google.com/",
    "prompt": "Your question here",
    "country": "US",
    "index": 1
  }
]
```

### Batch (up to 20 questions)
```json
[
  {
    "url": "https://gemini.google.com/",
    "prompt": "First question",
    "country": "US",
    "index": 1
  },
  {
    "url": "https://gemini.google.com/",
    "prompt": "Second question",
    "country": "US",
    "index": 2
  }
  // ... up to 20 total
]
```

## Response Format

BrightData returns results in this format:

```json
[
  {
    "url": "https://gemini.google.com/",
    "prompt": "Your question",
    "answer_text_markdown": "The response text...",
    "index": 1,
    "error": ""  // Empty if successful
  }
]
```

## Error Handling

### Failed Requests
If a question fails (network error, API error, empty response), the provider returns:
- Response text: `"Question run failed for this model and location"`
- `ShouldProcessEvaluation`: `false`
- Cost: $0.0015 (still charged by BrightData)

### Snapshot Building Delays
The provider automatically retries up to 20 times (10 minutes) if the snapshot is still building.

### Debug Files
Failed batch requests save the full API response to:
```
gemini_error_<snapshot_id>.txt
```

## Logging Output

Expected console output during execution:

```
[getProvider] 🎯 Selected Gemini provider for model: gemini
[GeminiProvider] 🚀 Making batched Gemini call for 15 queries
[GeminiProvider] 📤 Request payload for 15 queries
[GeminiProvider] 📋 Batch job submitted with snapshot ID: s_abc123...
[GeminiProvider] 📊 Batch job status: running (poll #1)
[GeminiProvider] 📊 Batch job status: running (poll #2)
[GeminiProvider] ✅ Batch job completed after 3 polls, retrieving results
[GeminiProvider] 📡 API Response Status Code: 200 (attempt 1/20)
[GeminiProvider] 🔍 Response body length: 45632 bytes
[GeminiProvider] ✅ Successfully retrieved 15 results
[GeminiProvider] ✅ Using index-based result mapping
[GeminiProvider] ✅ Batch completed: 15 questions processed, total cost: $0.0225
```

## Feature Parity Matrix

| Feature | ChatGPT | Perplexity | Gemini |
|---------|---------|------------|--------|
| Single question | ✅ | ✅ | ✅ |
| Batch processing (up to 20) | ✅ | ✅ | ✅ |
| Web search support | ✅ | ✅ | ✅ |
| Location/country targeting | ✅ | ✅ | ✅ |
| Async polling | ✅ | ✅ | ✅ |
| Retry logic | ✅ | ✅ | ✅ |
| Index-based mapping | ✅ | ✅ | ✅ |
| Prompt fallback matching | ✅ | ✅ | ✅ |
| Error handling | ✅ | ✅ | ✅ |
| Cost tracking | ✅ | ✅ | ✅ |
| Debug logging | ✅ | ✅ | ✅ |

## Testing Checklist

- [ ] Set `GEMINI_DATASET_ID` environment variable
- [ ] Add Gemini model to database
- [ ] Add Gemini to test organization
- [ ] Run single question test
- [ ] Run batch test (2-5 questions)
- [ ] Verify results are mapped correctly
- [ ] Check failed question handling
- [ ] Verify location mapping (US, UK, other)
- [ ] Confirm cost tracking ($0.0015/call)
- [ ] Review logs for proper provider selection
- [ ] Run full org evaluation pipeline

## Integration with Org Evaluation Pipeline

The Gemini provider integrates seamlessly with the existing pipeline:

1. **Question Execution**: Batches up to 20 questions per model-location pair
2. **Name Variations**: Uses pre-generated variations (once per org)
3. **Org Evaluation**: Extracts mentions, sentiment, and text
4. **Competitor Extraction**: Identifies competitors mentioned
5. **Citation Extraction**: Finds and classifies URLs
6. **Database Storage**: Stores in `org_evals`, `org_citations`, `org_competitors`

## Cost Structure

- **Per API Call**: $0.0015
- **Batch of 20 questions**: $0.0300 (20 × $0.0015)
- **Typical org with 50 questions × 3 locations**: $0.2250 (150 × $0.0015)

## Troubleshooting

### Provider Not Selected
**Issue**: System doesn't detect Gemini model
**Solution**: Ensure model name contains "gemini" (case-insensitive)

### Empty Dataset ID
**Issue**: Warning "GEMINI_DATASET_ID is empty"
**Solution**: Set `GEMINI_DATASET_ID` environment variable

### Snapshot Building Timeout
**Issue**: "snapshot still building after 20 attempts"
**Solution**: BrightData may be experiencing delays; check their status page

### Invalid Results
**Issue**: Results don't match questions
**Solution**: Check `gemini_error_*.txt` files for API response details

## Future Enhancements

Potential improvements for Gemini provider:

- [ ] Dynamic cost configuration (if BrightData changes pricing)
- [ ] Citation extraction from Gemini responses (if available)
- [ ] Streaming support (if BrightData adds it)
- [ ] Advanced location targeting (city/region level)
- [ ] Custom timeout configuration per organization

## Related Files

- `services/brightdata_provider.go` - ChatGPT implementation
- `services/perplexity_provider.go` - Perplexity implementation
- `services/interfaces.go` - AIProvider interface definition
- `services/org_evaluation_service.go` - Main evaluation service
- `workflows/org_evaluation_processor.go` - Inngest workflow
- `internal/config/config.go` - Configuration management

## Support

For issues or questions:
1. Check logs for detailed error messages
2. Review `gemini_error_*.txt` files if available
3. Verify environment variables are set correctly
4. Ensure BrightData API is accessible
5. Confirm dataset ID is valid for your account
