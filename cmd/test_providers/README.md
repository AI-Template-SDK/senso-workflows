# Provider Test Script

A standalone test script to verify AI provider functionality (both sync and async).

## Quick Start

### 1. Set Up Configuration

**Option A: Use .env file (Recommended)**

```bash
cd cmd/test_providers
cp env.example .env
# Edit .env with your API keys
```

**Option B: Export environment variables**

```bash
# For BrightData providers (ChatGPT/Perplexity/Gemini)
export BRIGHTDATA_API_KEY="your-api-key"
export BRIGHTDATA_DATASET_ID="your-chatgpt-dataset-id"
export PERPLEXITY_DATASET_ID="your-perplexity-dataset-id"
export GEMINI_DATASET_ID="your-gemini-dataset-id"

# For OpenAI
export OPENAI_API_KEY="your-openai-key"

# For Azure OpenAI (optional)
export AZURE_OPENAI_ENDPOINT="https://your-resource.openai.azure.com"
export AZURE_OPENAI_KEY="your-azure-key"
export AZURE_OPENAI_DEPLOYMENT_NAME="gpt-4"

# For Anthropic
export ANTHROPIC_API_KEY="your-anthropic-key"
```

### 2. Edit the Script

Open `main.go` and uncomment the provider you want to test:

```go
// Test ChatGPT (Async)
testProvider("chatgpt", cfg, costService, queries, location)

// Test Perplexity (Async)
// testProvider("perplexity", cfg, costService, queries, location)

// Test Gemini (Async)
// testProvider("gemini", cfg, costService, queries, location)

// Test OpenAI (Sync)
// testProvider("gpt-4.1", cfg, costService, queries, location)

// Test Anthropic (Sync)
// testProvider("claude-3-5-sonnet-20241022", cfg, costService, queries, location)
```

### 3. Run the Script

```bash
cd cmd/test_providers
go run main.go
```

## What It Tests

### For Async Providers (ChatGPT, Perplexity, Gemini)

The script simulates the 3-step Inngest workflow:

1. **Submit** - Submits batch job to BrightData (~1s)
2. **Poll** - Polls every 10s until ready (3-8 minutes)
3. **Retrieve** - Fetches and processes results (~5s)

### For Sync Providers (OpenAI, Anthropic)

The script executes a single synchronous batch call.

## Example Output

### Async Provider (ChatGPT)
```
🧪 AI Provider Test Script
==================================================

📋 Test Configuration:
  - Queries: 3
  - Location: California, US

🎯 Testing Provider: chatgpt
============================================================
✅ Provider created: chatgpt
   - IsAsync: true
   - SupportsBatching: true
   - MaxBatchSize: 20

🔄 Testing ASYNC provider (3-step process)

📤 Step 1: Submitting batch job...
✅ Batch job submitted in 1.2s
   Job ID: abc123...

⏳ Step 2: Polling for job completion...
   Poll #1: status=running, ready=false
   Poll #2: status=running, ready=false
   ...
   Poll #28: status=ready, ready=true
✅ Job completed in 4m35s (28 polls)

📥 Step 3: Retrieving batch results...
✅ Results retrieved in 4.8s

📊 Results Summary:
   - Total responses: 3

Question 1: What are the top 3 credit unions in California?
  Status: ✅ Success
  Response: Based on recent data, the top 3 credit unions in California...
  Tokens: 0 input, 0 output
  Cost: $0.001500
  Citations: 5
    - https://www.creditunions.com/california
    - https://www.example.com/top-credit-unions
    ... and 3 more

💰 Total Cost: $0.004500
✅ Success Rate: 3/3 (100.0%)

⏱️  Total Time: 4m41s
   - Submit: 1.2s
   - Poll: 4m35s (28 checks)
   - Retrieve: 4.8s
```

### Sync Provider (OpenAI)
```
🎯 Testing Provider: gpt-4.1
============================================================
✅ Provider created: openai
   - IsAsync: false
   - SupportsBatching: false
   - MaxBatchSize: 1

⚡ Testing SYNC provider (1-step process)

🚀 Executing batch request...
✅ Batch completed in 15.3s

📊 Results Summary:
   - Total responses: 3

Question 1: What are the top 3 credit unions in California?
  Status: ✅ Success
  Response: Here are the top 3 credit unions in California...
  Tokens: 45 input, 250 output
  Cost: $0.002345
  Citations: 0

💰 Total Cost: $0.006835
✅ Success Rate: 3/3 (100.0%)

⏱️  Total Time: 15.3s
```

## Customizing Tests

### Change Test Queries

Edit the `queries` slice in `main.go`:

```go
queries := []string{
    "Your custom question 1",
    "Your custom question 2",
    "Your custom question 3",
}
```

### Change Location

```go
location := &models.Location{
    Country: "CA",  // Canada
    Region:  strPtr("Ontario"),
}
```

### Test Multiple Providers

Uncomment multiple `testProvider()` calls to test providers in sequence.

## Troubleshooting

### "Failed to create provider: unsupported model"
- Check that the model name matches the factory patterns in `internal/providers/factory.go`
- For ChatGPT, use: `"chatgpt"`
- For OpenAI, use: `"gpt-4.1"` or any model containing "gpt"
- For Anthropic, use model names containing "claude", "sonnet", "opus", or "haiku"

### "Failed to submit batch job: BrightData API returned status 401"
- Check that `BRIGHTDATA_API_KEY` is set correctly
- Verify the dataset ID matches the provider

### "Job timed out after 60 polls"
- Increase the poll limit in the code
- Check BrightData dashboard for job status
- Verify the dataset is configured correctly

### "No results returned from BrightData"
- The batch may have failed on BrightData's side
- Check the error files written to disk (`chatgpt_error_*.txt`)
- Verify your BrightData account has available credits

## Performance Benchmarks

Expected times for async providers (3 queries):

- **Submit**: ~1-2 seconds
- **Poll**: ~3-8 minutes (depends on BrightData queue)
- **Retrieve**: ~3-5 seconds
- **Total**: ~4-9 minutes

Expected times for sync providers (3 queries):

- **OpenAI**: ~15-30 seconds (sequential)
- **Anthropic**: ~15-30 seconds (sequential)

## Notes

- The script uses the new `internal/providers` package
- Async providers automatically handle result matching by index or prompt
- Failed questions are marked with `ShouldProcessEvaluation: false`
- Cost calculations use the `CostService` from `services/`
- Poll interval is fixed at 10 seconds (matches production workflow)

