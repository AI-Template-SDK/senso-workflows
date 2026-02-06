// services/gemini_provider.go
package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/AI-Template-SDK/senso-workflows/internal/config"
	workflowModels "github.com/AI-Template-SDK/senso-workflows/internal/models"
)

type geminiProvider struct {
	apiKey      string
	datasetID   string
	baseURL     string
	costService CostService
	httpClient  *http.Client
}

func NewGeminiProvider(cfg *config.Config, model string, costService CostService) AIProvider {
	fmt.Printf("[NewGeminiProvider] Creating Gemini provider\n")
	fmt.Printf("[NewGeminiProvider]   - API Key: %s\n", maskAPIKey(cfg.BrightDataAPIKey))
	fmt.Printf("[NewGeminiProvider]   - Dataset ID: %s\n", cfg.GeminiDatasetID)

	if cfg.GeminiDatasetID == "" {
		fmt.Printf("[NewGeminiProvider] ⚠️ WARNING: GEMINI_DATASET_ID is empty!\n")
	}

	return &geminiProvider{
		apiKey:      cfg.BrightDataAPIKey,
		datasetID:   cfg.GeminiDatasetID,
		baseURL:     "https://api.brightdata.com/datasets/v3",
		costService: costService,
		httpClient: &http.Client{
			Timeout: 20 * time.Minute, // Long timeout for async operations
		},
	}
}

func (p *geminiProvider) GetProviderName() string {
	return "gemini"
}

// Gemini API request structures
type GeminiRequest []GeminiInput

type GeminiInput struct {
	URL     string `json:"url"`
	Prompt  string `json:"prompt"`
	Country string `json:"country"`
	Index   int    `json:"index"`
}

// Gemini API response structures
type GeminiTriggerResponse struct {
	SnapshotID string `json:"snapshot_id"`
}

type GeminiProgressResponse struct {
	Status             string `json:"status"`
	SnapshotID         string `json:"snapshot_id"`
	DatasetID          string `json:"dataset_id"`
	Records            *int   `json:"records,omitempty"`
	Errors             *int   `json:"errors,omitempty"`
	CollectionDuration *int   `json:"collection_duration,omitempty"`
}

type GeminiResult struct {
	URL                string           `json:"url"`
	Prompt             string           `json:"prompt"`
	AnswerTextMarkdown string           `json:"answer_text_markdown"`
	Index              int              `json:"index"`
	Error              string           `json:"error,omitempty"`
	Input              *GeminiInputEcho `json:"input,omitempty"` // Echoed back on errors
}

type GeminiInputEcho struct {
	URL     string `json:"url"`
	Prompt  string `json:"prompt"`
	Country string `json:"country"`
	Index   int    `json:"index"`
}

func (p *geminiProvider) RunQuestion(ctx context.Context, query string, websearch bool, location *workflowModels.Location) (*AIResponse, error) {
	fmt.Printf("[GeminiProvider] 🚀 Making Gemini call for query: %s\n", query)

	// 1. Submit job to Gemini dataset
	snapshotID, err := p.submitJob(ctx, query, location)
	if err != nil {
		return nil, fmt.Errorf("failed to submit Gemini job: %w", err)
	}

	fmt.Printf("[GeminiProvider] 📋 Job submitted with snapshot ID: %s\n", snapshotID)

	// 2. Poll until completion
	result, err := p.pollUntilComplete(ctx, snapshotID)
	if err != nil {
		return nil, fmt.Errorf("failed to poll Gemini job: %w", err)
	}

	// 3. Handle response - use answer_text_markdown if available, otherwise create failed response
	var responseText string
	var shouldProcessEvaluation bool

	if result.Error != "" {
		responseText = "This prompt didn’t complete successfully due to a temporary AI model limitation. You were not charged for this prompt. We'll re-try in the next run."
		shouldProcessEvaluation = false
		fmt.Printf("[GeminiProvider] ⚠️ Gemini returned error: %s\n", result.Error)
	} else if result.AnswerTextMarkdown == "" {
		responseText = "This prompt didn’t complete successfully due to a temporary AI model limitation. You were not charged for this prompt. We'll re-try in the next run."
		shouldProcessEvaluation = false
		fmt.Printf("[GeminiProvider] ⚠️ Gemini returned empty answer_text_markdown\n")
	} else {
		responseText = result.AnswerTextMarkdown
		shouldProcessEvaluation = true
		fmt.Printf("[GeminiProvider] ✅ Gemini returned valid response\n")
	}

	fmt.Printf("[GeminiProvider] ✅ Gemini call completed\n")
	fmt.Printf("[GeminiProvider]   - Response length: %d characters\n", len(responseText))
	fmt.Printf("[GeminiProvider]   - Should process evaluation: %t\n", shouldProcessEvaluation)
	fmt.Printf("[GeminiProvider]   - Cost: $0.0015\n")

	return &AIResponse{
		Response:                responseText,
		InputTokens:             0,      // Not available from BrightData
		OutputTokens:            0,      // Not available from BrightData
		Cost:                    0.0015, // Fixed cost per API call
		Citations:               []string{},
		ShouldProcessEvaluation: shouldProcessEvaluation,
	}, nil
}

func (p *geminiProvider) RunQuestionWebSearch(ctx context.Context, query string) (*AIResponse, error) {
	// For Gemini, web search is always enabled, so we can use the same method
	// with a default US location
	defaultLocation := &workflowModels.Location{
		Country: "US",
	}
	return p.RunQuestion(ctx, query, true, defaultLocation)
}

func (p *geminiProvider) submitJob(ctx context.Context, query string, location *workflowModels.Location) (string, error) {
	country := p.mapLocationToCountry(location)

	// Gemini uses direct array format (like Perplexity)
	payload := GeminiRequest{
		{
			URL:     "https://gemini.google.com/",
			Prompt:  query,
			Country: country,
			Index:   1,
		},
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	fmt.Printf("[GeminiProvider] 📤 Request payload: %s\n", string(jsonData))

	url := fmt.Sprintf("%s/trigger?dataset_id=%s&include_errors=true", p.baseURL, p.datasetID)
	maxRetries := 5
	var lastStatus int
	var lastBody string
	var lastErr error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
		if err != nil {
			return "", fmt.Errorf("failed to create request: %w", err)
		}

		req.Header.Set("Authorization", "Bearer "+p.apiKey)
		req.Header.Set("Content-Type", "application/json")

		resp, err := p.httpClient.Do(req)
		if err != nil {
			lastErr = err
			fmt.Printf("[GeminiProvider] ⚠️ Trigger request failed (attempt %d/%d): %v\n", attempt, maxRetries, err)
			if attempt < maxRetries {
				time.Sleep(2 * time.Second)
				continue
			}
			break
		}

		if resp.StatusCode != http.StatusOK {
			bodyBytes, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			lastStatus = resp.StatusCode
			lastBody = string(bodyBytes)
			fmt.Printf("[GeminiProvider] ⚠️ Trigger returned status %d (attempt %d/%d), retrying\n", resp.StatusCode, attempt, maxRetries)
			if attempt < maxRetries {
				time.Sleep(2 * time.Second)
				continue
			}
			break
		}

		var triggerResp GeminiTriggerResponse
		if err := json.NewDecoder(resp.Body).Decode(&triggerResp); err != nil {
			resp.Body.Close()
			return "", fmt.Errorf("failed to decode trigger response: %w", err)
		}
		resp.Body.Close()
		return triggerResp.SnapshotID, nil
	}

	if lastErr != nil {
		fmt.Printf("[GeminiProvider] ❌ Trigger failed after %d attempts: %v\n", maxRetries, lastErr)
		return "", fmt.Errorf("failed to make request: %w", lastErr)
	}

	fmt.Printf("[GeminiProvider] ❌ Trigger failed after %d attempts: status=%d body=%s\n", maxRetries, lastStatus, lastBody)
	return "", fmt.Errorf("Gemini API returned status %d: %s", lastStatus, lastBody)
}

func (p *geminiProvider) pollUntilComplete(ctx context.Context, snapshotID string) (*GeminiResult, error) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	// No timeout - let it run as long as needed
	pollCount := 0
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			pollCount++
			status, err := p.checkProgress(ctx, snapshotID)
			if err != nil {
				fmt.Printf("[GeminiProvider] ⚠️ Progress check failed (attempt %d), retrying: %v\n", pollCount, err)
				continue // Retry on error
			}

			fmt.Printf("[GeminiProvider] 📊 Job status: %s (poll #%d)\n", status.Status, pollCount)

			if status.Status == "ready" {
				fmt.Printf("[GeminiProvider] ✅ Job completed after %d polls, retrieving results\n", pollCount)
				return p.getResults(ctx, snapshotID)
			}

			if status.Status == "failed" {
				return nil, fmt.Errorf("Gemini job failed for snapshot %s", snapshotID)
			}

			// Continue polling if status is "running" or other non-terminal states
		}
	}
}

func (p *geminiProvider) checkProgress(ctx context.Context, snapshotID string) (*GeminiProgressResponse, error) {
	url := fmt.Sprintf("%s/progress/%s", p.baseURL, snapshotID)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create progress request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to check progress: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("progress check returned status %d", resp.StatusCode)
	}

	var progressResp GeminiProgressResponse
	if err := json.NewDecoder(resp.Body).Decode(&progressResp); err != nil {
		return nil, fmt.Errorf("failed to decode progress response: %w", err)
	}

	return &progressResp, nil
}

func (p *geminiProvider) getResults(ctx context.Context, snapshotID string) (*GeminiResult, error) {
	results, err := p.getBatchResults(ctx, snapshotID)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("no results returned from Gemini")
	}
	return &results[0], nil
}

func (p *geminiProvider) mapLocationToCountry(location *workflowModels.Location) string {
	normalized := normalizeLocation(location)
	return normalized.CountryCode
}

// SupportsBatching returns true for Gemini (supports batch processing via BrightData)
func (p *geminiProvider) SupportsBatching() bool {
	return true
}

// GetMaxBatchSize returns 20 for Gemini (can batch up to 20 questions)
func (p *geminiProvider) GetMaxBatchSize() int {
	return 20
}

// RunQuestionBatch processes multiple questions in a single Gemini API call
func (p *geminiProvider) RunQuestionBatch(ctx context.Context, queries []string, websearch bool, location *workflowModels.Location) ([]*AIResponse, error) {
	fmt.Printf("[GeminiProvider] 🚀 Making batched Gemini call for %d queries\n", len(queries))

	if len(queries) > 20 {
		return nil, fmt.Errorf("batch size %d exceeds maximum of 20", len(queries))
	}

	// Inject localized instructions into each prompt before submission
	localizedQueries := make([]string, len(queries))
	for i, query := range queries {
		localizedQueries[i] = p.buildLocalizedPrompt(query, location)
	}
	queries = localizedQueries

	// 1. Submit batch job to Gemini
	snapshotID, err := p.submitBatchJob(ctx, queries, location)
	if err != nil {
		return nil, fmt.Errorf("failed to submit Gemini batch job: %w", err)
	}

	fmt.Printf("[GeminiProvider] 📋 Batch job submitted with snapshot ID: %s\n", snapshotID)

	// 2. Poll until completion
	results, err := p.pollBatchUntilComplete(ctx, snapshotID)
	if err != nil {
		return nil, fmt.Errorf("failed to poll Gemini batch job: %w", err)
	}

	// 3. Match results to queries
	// Strategy 1: Use Index field if valid (1-based indices we sent)
	// Strategy 2: Match by Prompt text if Index is invalid
	// Strategy 3: FAIL - never use array order as it risks data corruption

	resultMap := make(map[int]*GeminiResult)
	unmatchedResults := []*GeminiResult{}
	hasValidIndices := true

	for i := range results {
		// Extract index - check both top-level and input.index (for error results)
		index := results[i].Index
		if index == 0 && results[i].Input != nil {
			index = results[i].Input.Index // Error results have index in input
		}

		// Check if index is valid (1-based)
		if index < 1 || index > len(queries) {
			fmt.Printf("[GeminiProvider] ⚠️ Result %d has invalid index %d (checked both top-level and input), will match by prompt\n", i, index)
			unmatchedResults = append(unmatchedResults, &results[i])
			hasValidIndices = false
			continue
		}

		// Check for duplicate indices
		if _, exists := resultMap[index]; exists {
			fmt.Printf("[GeminiProvider] ⚠️ Duplicate result index: %d, will match by prompt\n", index)
			unmatchedResults = append(unmatchedResults, &results[i])
			hasValidIndices = false
			continue
		}

		resultMap[index] = &results[i]
	}

	// 4. Build responses array in correct order
	responses := make([]*AIResponse, len(queries))

	if hasValidIndices && len(resultMap) == len(queries) {
		// Perfect: all results have valid unique indices
		fmt.Printf("[GeminiProvider] ✅ Using index-based result mapping\n")
		for i := range queries {
			queryIndex := i + 1 // Our indices are 1-based
			result, exists := resultMap[queryIndex]
			if !exists {
				return nil, fmt.Errorf("missing result for query index %d", queryIndex)
			}
			responses[i] = p.convertResultToResponse(result, queryIndex)
		}
	} else {
		// Fallback: match by prompt text (SAFE - matches actual question content)
		fmt.Printf("[GeminiProvider] 🔍 Using prompt-based result matching for safety\n")

		// Build map of all results (indexed + unmatched) by prompt
		// Handle both success results (prompt at top level) and error results (prompt in input.prompt)
		allResults := make(map[string]*GeminiResult)
		for _, result := range resultMap {
			prompt := result.Prompt
			if prompt == "" && result.Input != nil {
				prompt = result.Input.Prompt // Error results have prompt in input
			}
			if prompt != "" {
				allResults[prompt] = result
			}
		}
		for _, result := range unmatchedResults {
			prompt := result.Prompt
			if prompt == "" && result.Input != nil {
				prompt = result.Input.Prompt // Error results have prompt in input
			}
			if prompt != "" {
				allResults[prompt] = result
			}
		}

		fmt.Printf("[GeminiProvider] 📊 Built result map with %d prompts\n", len(allResults))

		// Match each query to its result by prompt text
		for i, query := range queries {
			result, exists := allResults[query]
			if !exists {
				return nil, fmt.Errorf("no result found for query: %q (have %d results)", query, len(allResults))
			}
			responses[i] = p.convertResultToResponse(result, i+1)
			fmt.Printf("[GeminiProvider] ✓ Matched query %d by prompt text\n", i+1)
		}
	}

	fmt.Printf("[GeminiProvider] ✅ Batch completed: %d questions processed, total cost: $%.4f\n",
		len(responses), float64(len(responses))*0.0015)

	return responses, nil
}

// convertResultToResponse converts a GeminiResult to an AIResponse
func (p *geminiProvider) convertResultToResponse(result *GeminiResult, displayIndex int) *AIResponse {
	// Handle response
	var responseText string
	var shouldProcessEvaluation bool

	if result.Error != "" {
		responseText = "This prompt didn’t complete successfully due to a temporary AI model limitation. You were not charged for this prompt. We'll re-try in the next run."
		shouldProcessEvaluation = false
		fmt.Printf("[GeminiProvider] ⚠️ Question %d returned error: %s\n", displayIndex, result.Error)
	} else if result.AnswerTextMarkdown == "" {
		responseText = "This prompt didn’t complete successfully due to a temporary AI model limitation. You were not charged for this prompt. We'll re-try in the next run."
		shouldProcessEvaluation = false
		fmt.Printf("[GeminiProvider] ⚠️ Question %d returned empty answer_text_markdown\n", displayIndex)
	} else {
		responseText = result.AnswerTextMarkdown
		shouldProcessEvaluation = true
	}

	return &AIResponse{
		Response:                responseText,
		InputTokens:             0,
		OutputTokens:            0,
		Cost:                    0.0015, // Fixed cost per API call
		Citations:               []string{},
		ShouldProcessEvaluation: shouldProcessEvaluation,
	}
}

// submitBatchJob submits multiple queries to Gemini in a single API call
func (p *geminiProvider) submitBatchJob(ctx context.Context, queries []string, location *workflowModels.Location) (string, error) {
	country := p.mapLocationToCountry(location)

	// Build input array with all queries (Gemini uses direct array format like Perplexity)
	payload := make(GeminiRequest, len(queries))
	for i, query := range queries {
		payload[i] = GeminiInput{
			URL:     "https://gemini.google.com/",
			Prompt:  query,
			Country: country,
			Index:   i + 1,
		}
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	fmt.Printf("[GeminiProvider] 📤 Request payload for %d queries\n", len(queries))

	url := fmt.Sprintf("%s/trigger?dataset_id=%s&include_errors=true", p.baseURL, p.datasetID)
	maxRetries := 5
	var lastStatus int
	var lastBody string
	var lastErr error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
		if err != nil {
			return "", fmt.Errorf("failed to create request: %w", err)
		}

		req.Header.Set("Authorization", "Bearer "+p.apiKey)
		req.Header.Set("Content-Type", "application/json")

		resp, err := p.httpClient.Do(req)
		if err != nil {
			lastErr = err
			fmt.Printf("[GeminiProvider] ⚠️ Batch trigger request failed (attempt %d/%d): %v\n", attempt, maxRetries, err)
			if attempt < maxRetries {
				time.Sleep(2 * time.Second)
				continue
			}
			break
		}

		if resp.StatusCode != http.StatusOK {
			bodyBytes, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			lastStatus = resp.StatusCode
			lastBody = string(bodyBytes)
			fmt.Printf("[GeminiProvider] ⚠️ Batch trigger returned status %d (attempt %d/%d), retrying\n", resp.StatusCode, attempt, maxRetries)
			if attempt < maxRetries {
				time.Sleep(2 * time.Second)
				continue
			}
			break
		}

		var triggerResp GeminiTriggerResponse
		if err := json.NewDecoder(resp.Body).Decode(&triggerResp); err != nil {
			resp.Body.Close()
			return "", fmt.Errorf("failed to decode trigger response: %w", err)
		}
		resp.Body.Close()
		return triggerResp.SnapshotID, nil
	}

	if lastErr != nil {
		fmt.Printf("[GeminiProvider] ❌ Batch trigger failed after %d attempts: %v\n", maxRetries, lastErr)
		return "", fmt.Errorf("failed to make request: %w", lastErr)
	}

	fmt.Printf("[GeminiProvider] ❌ Batch trigger failed after %d attempts: status=%d body=%s\n", maxRetries, lastStatus, lastBody)
	return "", fmt.Errorf("Gemini API returned status %d: %s", lastStatus, lastBody)
}

// pollBatchUntilComplete polls for batch completion and returns all results
func (p *geminiProvider) pollBatchUntilComplete(ctx context.Context, snapshotID string) ([]GeminiResult, error) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	// No timeout - let it run as long as needed
	pollCount := 0
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			pollCount++
			status, err := p.checkProgress(ctx, snapshotID)
			if err != nil {
				fmt.Printf("[GeminiProvider] ⚠️ Progress check failed (attempt %d), retrying: %v\n", pollCount, err)
				continue
			}

			fmt.Printf("[GeminiProvider] 📊 Batch job status: %s (poll #%d)\n", status.Status, pollCount)

			if status.Status == "ready" {
				fmt.Printf("[GeminiProvider] ✅ Batch job completed after %d polls, retrieving results\n", pollCount)
				return p.getBatchResults(ctx, snapshotID)
			}

			if status.Status == "failed" {
				return nil, fmt.Errorf("Gemini batch job failed for snapshot %s", snapshotID)
			}
		}
	}
}

// getBatchResults retrieves all results from a completed batch job
// It includes retry logic for when the snapshot is still building
func (p *geminiProvider) getBatchResults(ctx context.Context, snapshotID string) ([]GeminiResult, error) {
	maxRetries := 20 // Up to 20 retries (10 minutes with 30s intervals)
	retryInterval := 30 * time.Second

	for attempt := 1; attempt <= maxRetries; attempt++ {
		url := fmt.Sprintf("%s/snapshot/%s?format=json", p.baseURL, snapshotID)
		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create results request: %w", err)
		}

		req.Header.Set("Authorization", "Bearer "+p.apiKey)

		resp, err := p.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to get results: %w", err)
		}

		// Log the status code
		fmt.Printf("[GeminiProvider] 📡 API Response Status Code: %d (attempt %d/%d)\n", resp.StatusCode, attempt, maxRetries)

		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
			resp.Body.Close()
			if attempt < maxRetries {
				fmt.Printf("[GeminiProvider] ⚠️ Results request returned %d (attempt %d/%d), retrying after %v\n",
					resp.StatusCode, attempt, maxRetries, retryInterval)
				select {
				case <-time.After(retryInterval):
					continue
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
			return nil, fmt.Errorf("results request returned status %d", resp.StatusCode)
		}

		// Read the body first so we can log it if there's an error
		bodyBytes, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to read response body: %w", err)
		}

		fmt.Printf("[GeminiProvider] 🔍 Response body length: %d bytes\n", len(bodyBytes))
		fmt.Printf("[GeminiProvider] 🔍 Response body preview: %s\n", string(bodyBytes[:min(500, len(bodyBytes))]))

		// Check if this is a status response (still building)
		isStatus, status, message := p.isStatusResponse(bodyBytes)
		if isStatus {
			if status == "building" {
				fmt.Printf("[GeminiProvider] ⏳ Snapshot still building (attempt %d/%d): %s\n", attempt, maxRetries, message)
				if attempt < maxRetries {
					fmt.Printf("[GeminiProvider] 💤 Waiting %v before retry...\n", retryInterval)
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
				// Unknown status, try to decode as results anyway
				fmt.Printf("[GeminiProvider] ⚠️ Unknown status '%s', attempting to decode as results\n", status)
			}
		}

		// Try to decode as results array
		var results []GeminiResult
		if err := json.Unmarshal(bodyBytes, &results); err != nil {
			// Save the full response to a text file for inspection
			filename := fmt.Sprintf("gemini_error_%s.txt", snapshotID)
			if writeErr := os.WriteFile(filename, bodyBytes, 0644); writeErr != nil {
				fmt.Printf("[GeminiProvider] ⚠️ Failed to write error response to file: %v\n", writeErr)
			} else {
				fmt.Printf("[GeminiProvider] 💾 Full response saved to: %s\n", filename)
			}

			fmt.Printf("[GeminiProvider] ❌ Failed to decode as array: %v\n", err)
			fmt.Printf("[GeminiProvider] 🔍 Response body preview (first 2000 chars):\n%s\n", string(bodyBytes[:min(2000, len(bodyBytes))]))
			return nil, fmt.Errorf("failed to decode results: %w", err)
		}

		if len(results) == 0 {
			fmt.Printf("[GeminiProvider] ⚠️ Decoded successfully but got 0 results\n")
			return nil, fmt.Errorf("no results returned from Gemini")
		}

		fmt.Printf("[GeminiProvider] ✅ Successfully retrieved %d results\n", len(results))
		return results, nil
	}

	return nil, fmt.Errorf("failed to retrieve results after %d attempts", maxRetries)
}

// isStatusResponse checks if the response is a status object rather than results
func (p *geminiProvider) isStatusResponse(bodyBytes []byte) (bool, string, string) {
	var statusResp struct {
		Status  string `json:"status"`
		Message string `json:"message"`
	}

	if err := json.Unmarshal(bodyBytes, &statusResp); err != nil {
		return false, "", ""
	}

	// If it has a status field, it's a status response
	if statusResp.Status != "" {
		return true, statusResp.Status, statusResp.Message
	}

	return false, "", ""
}

func (p *geminiProvider) buildLocalizedPrompt(query string, location *workflowModels.Location) string {
	locationDescription := formatLocationForPrompt(location)
	return fmt.Sprintf("Ensure your response is localized to %s. Answer the following question: %s",
		locationDescription, query)
}
