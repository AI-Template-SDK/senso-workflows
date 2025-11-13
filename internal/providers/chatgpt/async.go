package chatgpt

import (
	"context"
	"fmt"

	"github.com/AI-Template-SDK/senso-workflows/internal/models"
	"github.com/AI-Template-SDK/senso-workflows/internal/providers/common"
)

// SubmitBatchJob submits a batch job and returns the job ID (snapshot ID)
func (p *Provider) SubmitBatchJob(ctx context.Context, queries []string, websearch bool, location *models.Location) (string, error) {
	fmt.Printf("[ChatGPTProvider] 📤 Submitting async batch job with %d queries\n", len(queries))

	if len(queries) > 20 {
		return "", fmt.Errorf("batch size %d exceeds maximum of 20", len(queries))
	}

	// Submit batch job to BrightData
	snapshotID, err := p.submitBatchJob(ctx, queries, location, websearch)
	if err != nil {
		return "", fmt.Errorf("failed to submit BrightData batch job: %w", err)
	}

	fmt.Printf("[ChatGPTProvider] ✅ Batch job submitted with snapshot ID: %s\n", snapshotID)
	return snapshotID, nil
}

// PollJobStatus checks the status of a submitted job
// Returns: (status, ready, error)
// - status: current job status (e.g., "running", "ready", "failed")
// - ready: true if job is complete and results can be retrieved
func (p *Provider) PollJobStatus(ctx context.Context, jobID string) (string, bool, error) {
	fmt.Printf("[ChatGPTProvider] 🔍 Checking status for job: %s\n", jobID)

	status, err := p.client.CheckProgress(ctx, jobID)
	if err != nil {
		return "", false, fmt.Errorf("failed to check progress: %w", err)
	}

	isReady := status.Status == "ready"
	fmt.Printf("[ChatGPTProvider] 📊 Job %s status: %s (ready: %t)\n", jobID, status.Status, isReady)

	if status.Status == "failed" {
		return status.Status, false, fmt.Errorf("job failed")
	}

	return status.Status, isReady, nil
}

// RetrieveBatchResults retrieves and processes the results from a completed job
// queries parameter is needed to match results to original query order
func (p *Provider) RetrieveBatchResults(ctx context.Context, jobID string, queries []string) ([]*common.AIResponse, error) {
	fmt.Printf("[ChatGPTProvider] 📥 Retrieving results for job: %s\n", jobID)

	// Get results from BrightData
	bodyBytes, err := p.client.GetBatchResults(ctx, jobID, "ChatGPTProvider")
	if err != nil {
		return nil, fmt.Errorf("failed to get batch results: %w", err)
	}

	// Parse results
	results, err := p.parseBatchResults(bodyBytes, jobID)
	if err != nil {
		return nil, err
	}

	fmt.Printf("[ChatGPTProvider] 📊 Retrieved %d results for %d queries\n", len(results), len(queries))

	// Match results to queries and convert to AIResponse
	responses, err := p.matchAndConvertResults(results, queries)
	if err != nil {
		return nil, err
	}

	fmt.Printf("[ChatGPTProvider] ✅ Successfully processed %d responses\n", len(responses))
	return responses, nil
}
