//go:build integration
// +build integration

package chatgpt_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/AI-Template-SDK/senso-workflows/internal/config"
	"github.com/AI-Template-SDK/senso-workflows/internal/providers/chatgpt"
	"github.com/AI-Template-SDK/senso-workflows/internal/providers/testutil"
)

// TestAsyncFlowIntegration tests the full async flow with real BrightData API
// Run with: go test -tags=integration ./internal/providers/chatgpt/
func TestAsyncFlowIntegration(t *testing.T) {
	// Skip if no API key
	apiKey := os.Getenv("BRIGHTDATA_API_KEY")
	if apiKey == "" {
		t.Skip("Skipping integration test: BRIGHTDATA_API_KEY not set")
	}

	datasetID := os.Getenv("BRIGHTDATA_DATASET_ID")
	if datasetID == "" {
		t.Skip("Skipping integration test: BRIGHTDATA_DATASET_ID not set")
	}

	cfg := &config.Config{
		BrightDataAPIKey:    apiKey,
		BrightDataDatasetID: datasetID,
	}

	costService := testutil.NewMockCostService()
	provider := chatgpt.NewProvider(cfg, "chatgpt", costService)

	queries := []string{
		"What is 2+2?",
	}
	location := testutil.SampleLocation()

	ctx := context.Background()

	// Step 1: Submit
	t.Log("Step 1: Submitting batch job...")
	jobID, err := provider.SubmitBatchJob(ctx, queries, true, location)
	if err != nil {
		t.Fatalf("Failed to submit batch job: %v", err)
	}
	t.Logf("✅ Job submitted: %s", jobID)

	// Step 2: Poll
	t.Log("Step 2: Polling for completion...")
	maxPolls := 60 // 10 minutes max
	pollCount := 0

	for pollCount < maxPolls {
		pollCount++
		status, ready, err := provider.PollJobStatus(ctx, jobID)
		if err != nil {
			t.Fatalf("Failed to poll job status: %v", err)
		}

		t.Logf("Poll #%d: status=%s, ready=%t", pollCount, status, ready)

		if ready {
			t.Logf("✅ Job ready after %d polls", pollCount)
			break
		}

		if pollCount >= maxPolls {
			t.Fatalf("Job did not complete after %d polls", maxPolls)
		}

		time.Sleep(10 * time.Second)
	}

	// Step 3: Retrieve
	t.Log("Step 3: Retrieving results...")
	responses, err := provider.RetrieveBatchResults(ctx, jobID, queries)
	if err != nil {
		t.Fatalf("Failed to retrieve results: %v", err)
	}
	t.Logf("✅ Retrieved %d responses", len(responses))

	// Verify responses
	if len(responses) != len(queries) {
		t.Errorf("Expected %d responses, got %d", len(queries), len(responses))
	}

	for i, resp := range responses {
		t.Logf("Response %d:", i+1)
		t.Logf("  - Answer: %s", resp.Response[:50])
		t.Logf("  - ShouldProcess: %t", resp.ShouldProcessEvaluation)
		t.Logf("  - Cost: $%.6f", resp.Cost)
		t.Logf("  - Citations: %d", len(resp.Citations))

		if !resp.ShouldProcessEvaluation {
			t.Errorf("Response %d failed processing", i+1)
		}
	}
}

// TestSyncBatchIntegration tests RunQuestionBatch with real API
func TestSyncBatchIntegration(t *testing.T) {
	apiKey := os.Getenv("BRIGHTDATA_API_KEY")
	if apiKey == "" {
		t.Skip("Skipping integration test: BRIGHTDATA_API_KEY not set")
	}

	datasetID := os.Getenv("BRIGHTDATA_DATASET_ID")
	if datasetID == "" {
		t.Skip("Skipping integration test: BRIGHTDATA_DATASET_ID not set")
	}

	cfg := &config.Config{
		BrightDataAPIKey:    apiKey,
		BrightDataDatasetID: datasetID,
	}

	costService := testutil.NewMockCostService()
	provider := chatgpt.NewProvider(cfg, "chatgpt", costService)

	queries := []string{
		"What is the capital of France?",
	}
	location := testutil.SampleLocation()

	ctx := context.Background()

	// Test sync batch (submit + poll + retrieve in one call)
	t.Log("Testing sync batch execution...")
	responses, err := provider.RunQuestionBatch(ctx, queries, true, location)
	if err != nil {
		t.Fatalf("RunQuestionBatch failed: %v", err)
	}

	t.Logf("✅ Sync batch completed with %d responses", len(responses))

	if len(responses) != len(queries) {
		t.Errorf("Expected %d responses, got %d", len(queries), len(responses))
	}
}
