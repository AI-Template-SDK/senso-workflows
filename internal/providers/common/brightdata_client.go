package common

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// BrightDataClient handles all HTTP interactions with the BrightData API
type BrightDataClient struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

// NewBrightDataClient creates a new BrightData API client
func NewBrightDataClient(apiKey string) *BrightDataClient {
	return &BrightDataClient{
		apiKey:  apiKey,
		baseURL: "https://api.brightdata.com/datasets/v3",
		httpClient: &http.Client{
			Timeout: 20 * time.Minute, // Long timeout for async operations
		},
	}
}

// SubmitBatchJob submits a batch job to BrightData
func (c *BrightDataClient) SubmitBatchJob(ctx context.Context, payload interface{}, datasetID string) (string, error) {
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/trigger?dataset_id=%s&include_errors=true", c.baseURL, datasetID)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("BrightData API returned status %d", resp.StatusCode)
	}

	var triggerResp TriggerResponse
	if err := json.NewDecoder(resp.Body).Decode(&triggerResp); err != nil {
		return "", fmt.Errorf("failed to decode trigger response: %w", err)
	}

	return triggerResp.SnapshotID, nil
}

// CheckProgress checks the progress of a BrightData job
func (c *BrightDataClient) CheckProgress(ctx context.Context, snapshotID string) (*ProgressResponse, error) {
	url := fmt.Sprintf("%s/progress/%s", c.baseURL, snapshotID)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create progress request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to check progress: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("progress check returned status %d", resp.StatusCode)
	}

	var progressResp ProgressResponse
	if err := json.NewDecoder(resp.Body).Decode(&progressResp); err != nil {
		return nil, fmt.Errorf("failed to decode progress response: %w", err)
	}

	return &progressResp, nil
}

// PollUntilComplete polls for batch completion and returns when ready
func (c *BrightDataClient) PollUntilComplete(ctx context.Context, snapshotID string, providerName string) error {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	pollCount := 0
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			pollCount++
			status, err := c.CheckProgress(ctx, snapshotID)
			if err != nil {
				fmt.Printf("[%s] ⚠️ Progress check failed (attempt %d), retrying: %v\n", providerName, pollCount, err)
				continue
			}

			fmt.Printf("[%s] 📊 Batch job status: %s (poll #%d)\n", providerName, status.Status, pollCount)

			if status.Status == "ready" {
				fmt.Printf("[%s] ✅ Batch job completed after %d polls\n", providerName, pollCount)
				return nil
			}

			if status.Status == "failed" {
				return fmt.Errorf("batch job failed for snapshot %s", snapshotID)
			}
		}
	}
}

// GetBatchResults retrieves all results from a completed batch job
// It includes retry logic for when the snapshot is still building
func (c *BrightDataClient) GetBatchResults(ctx context.Context, snapshotID string, providerName string) ([]byte, error) {
	maxRetries := 20
	retryInterval := 30 * time.Second

	for attempt := 1; attempt <= maxRetries; attempt++ {
		url := fmt.Sprintf("%s/snapshot/%s?format=json", c.baseURL, snapshotID)
		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create results request: %w", err)
		}

		req.Header.Set("Authorization", "Bearer "+c.apiKey)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to get results: %w", err)
		}

		fmt.Printf("[%s] 📡 API Response Status Code: %d (attempt %d/%d)\n", providerName, resp.StatusCode, attempt, maxRetries)

		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
			resp.Body.Close()
			return nil, fmt.Errorf("results request returned status %d", resp.StatusCode)
		}

		bodyBytes, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to read response body: %w", err)
		}

		fmt.Printf("[%s] 🔍 Response body length: %d bytes\n", providerName, len(bodyBytes))
		fmt.Printf("[%s] 🔍 Response body preview: %s\n", providerName, string(bodyBytes[:Min(500, len(bodyBytes))]))

		// Check if this is a status response (still building)
		isStatus, status, message := IsStatusResponse(bodyBytes)
		if isStatus {
			if status == "building" {
				fmt.Printf("[%s] ⏳ Snapshot still building (attempt %d/%d): %s\n", providerName, attempt, maxRetries, message)
				if attempt < maxRetries {
					fmt.Printf("[%s] 💤 Waiting %v before retry...\n", providerName, retryInterval)
					select {
					case <-time.After(retryInterval):
						continue // Retry
					case <-ctx.Done():
						return nil, ctx.Err()
					}
				} else {
					return nil, fmt.Errorf("snapshot still building after %d attempts", maxRetries)
				}
			} else if status == "failed" {
				return nil, fmt.Errorf("snapshot failed: %s", message)
			} else {
				fmt.Printf("[%s] ⚠️ Unknown status '%s', attempting to decode as results\n", providerName, status)
			}
		}

		// Return the raw body bytes for provider-specific parsing
		return bodyBytes, nil
	}

	return nil, fmt.Errorf("failed to retrieve results after %d attempts", maxRetries)
}

// SaveErrorResponse saves a failed response to a file for debugging
func (c *BrightDataClient) SaveErrorResponse(bodyBytes []byte, snapshotID string, providerName string) {
	filename := fmt.Sprintf("%s_error_%s.txt", providerName, snapshotID)
	if writeErr := os.WriteFile(filename, bodyBytes, 0644); writeErr != nil {
		fmt.Printf("[%s] ⚠️ Failed to write error response to file: %v\n", providerName, writeErr)
	} else {
		fmt.Printf("[%s] 💾 Full response saved to: %s\n", providerName, filename)
	}
}
