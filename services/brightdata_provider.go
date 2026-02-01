// services/brightdata_provider.go
package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/AI-Template-SDK/senso-workflows/internal/config"
	workflowModels "github.com/AI-Template-SDK/senso-workflows/internal/models"
)

type brightDataProvider struct {
	apiKey      string
	datasetID   string
	baseURL     string
	costService CostService
	httpClient  *http.Client
}

func NewBrightDataProvider(cfg *config.Config, model string, costService CostService) AIProvider {
	return &brightDataProvider{
		apiKey:      cfg.BrightDataAPIKey,
		datasetID:   cfg.BrightDataDatasetID,
		baseURL:     "https://api.brightdata.com/datasets/v3",
		costService: costService,
		httpClient: &http.Client{
			Timeout: 20 * time.Minute, // Long timeout for async operations
		},
	}
}

func (p *brightDataProvider) GetProviderName() string {
	return "brightdata"
}

// BrightData API request structures
type BrightDataRequest struct {
	Input []BrightDataInput `json:"input"`
}

type BrightDataInput struct {
	URL              string `json:"url"`
	Prompt           string `json:"prompt"`
	Country          string `json:"country"`
	WebSearch        bool   `json:"web_search"`
	Index            int    `json:"index"`
	AdditionalPrompt string `json:"additional_prompt"`
}

// BrightData API response structures
type BrightDataTriggerResponse struct {
	SnapshotID string `json:"snapshot_id"`
}

type BrightDataProgressResponse struct {
	Status             string `json:"status"`
	SnapshotID         string `json:"snapshot_id"`
	DatasetID          string `json:"dataset_id"`
	Records            *int   `json:"records,omitempty"`
	Errors             *int   `json:"errors,omitempty"`
	CollectionDuration *int   `json:"collection_duration,omitempty"`
}

type BrightDataLinks struct {
	URL      string `json:"url"`
	Text     string `json:"text"`
	Position int    `json:"position"`
}

type BrightDataResult struct {
	URL                string               `json:"url"`
	Prompt             string               `json:"prompt"`
	Citations          interface{}          `json:"citations"`
	Country            string               `json:"country"`
	AnswerTextMarkdown string               `json:"answer_text_markdown"`
	WebSearchTriggered bool                 `json:"web_search_triggered"`
	Index              int                  `json:"index"`
	Error              string               `json:"error,omitempty"`
	Input              *BrightDataInputEcho `json:"input,omitempty"` // Echoed back on errors
	LinksAttached      []BrightDataLinks    `json:"links_attached"`
}

type BrightDataInputEcho struct {
	URL              string `json:"url"`
	Prompt           string `json:"prompt"`
	Country          string `json:"country"`
	Index            int    `json:"index"`
	WebSearch        bool   `json:"web_search"`
	AdditionalPrompt string `json:"additional_prompt"`
}

func (p *brightDataProvider) RunQuestion(ctx context.Context, query string, websearch bool, location *workflowModels.Location) (*AIResponse, error) {
	fmt.Printf("[BrightDataProvider] 🚀 Making BrightData call for query: %s\n", query)

	// 1. Submit job to BrightData
	snapshotID, err := p.submitJob(ctx, query, location, websearch)
	if err != nil {
		return nil, fmt.Errorf("failed to submit BrightData job: %w", err)
	}

	fmt.Printf("[BrightDataProvider] 📋 Job submitted with snapshot ID: %s\n", snapshotID)

	// 2. Poll until completion
	result, err := p.pollUntilComplete(ctx, snapshotID)
	if err != nil {
		return nil, fmt.Errorf("failed to poll BrightData job: %w", err)
	}

	// 3. Parse citations if available
	var citations []string
	if result.Citations != nil {
		// Handle citations - they might be null, string, or array
		switch v := result.Citations.(type) {
		case []interface{}:
			for _, citation := range v {
				if str, ok := citation.(string); ok {
					citations = append(citations, str)
				}
			}
		case string:
			if v != "" {
				citations = []string{v}
			}
		}
	}

	// 4. Handle response - use answer_text_markdown if available, otherwise create failed response
	var responseText string
	var shouldProcessEvaluation bool

	if result.Error != "" {
		responseText = "Question run failed for this model and location"
		shouldProcessEvaluation = false
		fmt.Printf("[BrightDataProvider] ⚠️ BrightData returned error: %s\n", result.Error)
	} else if result.AnswerTextMarkdown == "" {
		responseText = "Question run failed for this model and location"
		shouldProcessEvaluation = false
		fmt.Printf("[BrightDataProvider] ⚠️ BrightData returned empty answer_text_markdown\n")
	} else {
		// Fix citations in the response text by converting [position] to [position](url)
		responseText = p.fixCitationsInResponse(result.AnswerTextMarkdown, result.LinksAttached)
		shouldProcessEvaluation = true
		fmt.Printf("[BrightDataProvider] ✅ BrightData returned valid response\n")
	}

	// Only extract citations if we have a valid response
	if !shouldProcessEvaluation {
		citations = []string{} // Clear citations for failed responses
	}

	fmt.Printf("[BrightDataProvider] ✅ BrightData call completed\n")
	fmt.Printf("[BrightDataProvider]   - Response length: %d characters\n", len(responseText))
	fmt.Printf("[BrightDataProvider]   - Citations: %d\n", len(citations))
	fmt.Printf("[BrightDataProvider]   - Should process evaluation: %t\n", shouldProcessEvaluation)
	fmt.Printf("[BrightDataProvider]   - Cost: $0.0015\n")

	return &AIResponse{
		Response:                responseText,
		InputTokens:             0,      // Not available from BrightData
		OutputTokens:            0,      // Not available from BrightData
		Cost:                    0.0015, // Fixed cost per API call
		Citations:               citations,
		ShouldProcessEvaluation: shouldProcessEvaluation,
	}, nil
}

func (p *brightDataProvider) RunQuestionWebSearch(ctx context.Context, query string) (*AIResponse, error) {
	// For BrightData, web search is always enabled, so we can use the same method
	// with a default US location
	defaultLocation := &workflowModels.Location{
		Country: "US",
	}
	return p.RunQuestion(ctx, query, true, defaultLocation)
}

func (p *brightDataProvider) submitJob(ctx context.Context, query string, location *workflowModels.Location, websearch bool) (string, error) {
	country := p.mapLocationToCountry(location)

	payload := BrightDataRequest{
		Input: []BrightDataInput{
			{
				URL:              "https://chatgpt.com/",
				Prompt:           query,
				Country:          country,
				WebSearch:        websearch,
				Index:            1,
				AdditionalPrompt: "",
			},
		},
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

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
			fmt.Printf("[BrightDataProvider] ⚠️ Trigger request failed (attempt %d/%d): %v\n", attempt, maxRetries, err)
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
			fmt.Printf("[BrightDataProvider] ⚠️ Trigger returned status %d (attempt %d/%d), retrying\n", resp.StatusCode, attempt, maxRetries)
			if attempt < maxRetries {
				time.Sleep(2 * time.Second)
				continue
			}
			break
		}

		var triggerResp BrightDataTriggerResponse
		if err := json.NewDecoder(resp.Body).Decode(&triggerResp); err != nil {
			resp.Body.Close()
			return "", fmt.Errorf("failed to decode trigger response: %w", err)
		}
		resp.Body.Close()
		return triggerResp.SnapshotID, nil
	}

	if lastErr != nil {
		fmt.Printf("[BrightDataProvider] ❌ Trigger failed after %d attempts: %v\n", maxRetries, lastErr)
		return "", fmt.Errorf("failed to make request: %w", lastErr)
	}

	fmt.Printf("[BrightDataProvider] ❌ Trigger failed after %d attempts: status=%d body=%s\n", maxRetries, lastStatus, lastBody)
	return "", fmt.Errorf("BrightData API returned status %d: %s", lastStatus, lastBody)
}

func (p *brightDataProvider) pollUntilComplete(ctx context.Context, snapshotID string) (*BrightDataResult, error) {
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
				fmt.Printf("[BrightDataProvider] ⚠️ Progress check failed (attempt %d), retrying: %v\n", pollCount, err)
				continue // Retry on error
			}

			fmt.Printf("[BrightDataProvider] 📊 Job status: %s (poll #%d)\n", status.Status, pollCount)

			if status.Status == "ready" {
				fmt.Printf("[BrightDataProvider] ✅ Job completed after %d polls, retrieving results\n", pollCount)
				return p.getResults(ctx, snapshotID)
			}

			if status.Status == "failed" {
				return nil, fmt.Errorf("BrightData job failed for snapshot %s", snapshotID)
			}

			// Continue polling if status is "running" or other non-terminal states
		}
	}
}

func (p *brightDataProvider) checkProgress(ctx context.Context, snapshotID string) (*BrightDataProgressResponse, error) {
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

	var progressResp BrightDataProgressResponse
	if err := json.NewDecoder(resp.Body).Decode(&progressResp); err != nil {
		return nil, fmt.Errorf("failed to decode progress response: %w", err)
	}

	return &progressResp, nil
}

func (p *brightDataProvider) getResults(ctx context.Context, snapshotID string) (*BrightDataResult, error) {
	results, err := p.getBatchResults(ctx, snapshotID)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("no results returned from BrightData")
	}
	return &results[0], nil
}

func (p *brightDataProvider) mapLocationToCountry(location *workflowModels.Location) string {
	if location == nil {
		return "US" // Default to US
	}

	// Map location.Country to BrightData country codes
	countryMap := map[string]string{
		"US": "US",
		"CA": "CA",
		"GB": "GB",
		"UK": "GB", // Handle UK -> GB mapping
		"AU": "AU",
		"DE": "DE",
		"FR": "FR",
		"IT": "IT",
		"ES": "ES",
		"NL": "NL",
		"JP": "JP",
		"KR": "KR",
		"IN": "IN",
		"BR": "BR",
		"MX": "MX",
	}

	if country, exists := countryMap[strings.ToUpper(location.Country)]; exists {
		return country
	}

	// Fallback to US if country not found
	return "US"
}

// SupportsBatching returns true for BrightData (supports batch processing)
func (p *brightDataProvider) SupportsBatching() bool {
	return true
}

// GetMaxBatchSize returns 20 for BrightData (can batch up to 20 questions)
func (p *brightDataProvider) GetMaxBatchSize() int {
	return 20
}

// RunQuestionBatch processes multiple questions in a single BrightData API call
func (p *brightDataProvider) RunQuestionBatch(ctx context.Context, queries []string, websearch bool, location *workflowModels.Location) ([]*AIResponse, error) {
	fmt.Printf("[BrightDataProvider] 🚀 Making batched BrightData call for %d queries\n", len(queries))

	if len(queries) > 20 {
		return nil, fmt.Errorf("batch size %d exceeds maximum of 20", len(queries))
	}

	// Inject localized instructions into each prompt before submission
	localizedQueries := make([]string, len(queries))
	for i, query := range queries {
		localizedQueries[i] = p.buildLocalizedPrompt(query, location)
	}
	queries = localizedQueries

	// 1. Submit batch job to BrightData
	snapshotID, err := p.submitBatchJob(ctx, queries, location, websearch)
	if err != nil {
		return nil, fmt.Errorf("failed to submit BrightData batch job: %w", err)
	}

	fmt.Printf("[BrightDataProvider] 📋 Batch job submitted with snapshot ID: %s\n", snapshotID)

	// 2. Poll until completion
	results, err := p.pollBatchUntilComplete(ctx, snapshotID)
	if err != nil {
		return nil, fmt.Errorf("failed to poll BrightData batch job: %w", err)
	}

	fmt.Printf("[BrightDataProvider] 📊 Retrieved %d results for %d queries\n", len(results), len(queries))

	// 3. Sort results by Index to match query order
	// BrightData may return results in any order, so we use the Index field to map them correctly
	resultMap := make(map[int]*BrightDataResult)
	hasValidIndices := true

	for i := range results {
		// Extract index - check both top-level and input.index (for error results)
		index := results[i].Index
		if index == 0 && results[i].Input != nil {
			index = results[i].Input.Index // Error results have index in input
		}

		// Get prompt for logging - check both locations
		promptPreview := results[i].Prompt
		if promptPreview == "" && results[i].Input != nil {
			promptPreview = results[i].Input.Prompt // Error results have prompt in input
		}
		if len(promptPreview) > 50 {
			promptPreview = promptPreview[:50] + "..."
		}
		fmt.Printf("[BrightDataProvider] 🔍 Result %d: index=%d, prompt='%s', hasError=%t\n",
			i, index, promptPreview, results[i].Error != "")

		// Check if index is valid (1-based)
		if index < 1 || index > len(queries) {
			fmt.Printf("[BrightDataProvider] ⚠️ Result %d has invalid index %d (expected 1-%d, checked both top-level and input)\n", i, index, len(queries))
			hasValidIndices = false
			break
		}

		// Check for duplicate indices
		if _, exists := resultMap[index]; exists {
			fmt.Printf("[BrightDataProvider] ⚠️ Duplicate result index: %d, will match by prompt\n", index)
			hasValidIndices = false
			break
		}

		resultMap[index] = &results[i]
	}

	// Verify we got all expected results if using indices
	if hasValidIndices && len(resultMap) != len(queries) {
		fmt.Printf("[BrightDataProvider] ⚠️ Expected %d results but got %d with valid indices\n", len(queries), len(resultMap))
		fmt.Printf("[BrightDataProvider] 🔍 ResultMap has indices: ")
		for idx := range resultMap {
			fmt.Printf("%d ", idx)
		}
		fmt.Printf("\n")
		hasValidIndices = false
	}

	// 4. Build responses array in correct order
	responses := make([]*AIResponse, len(queries))

	if hasValidIndices {
		// Use index-based mapping
		fmt.Printf("[BrightDataProvider] ✅ Using index-based result mapping (all %d indices valid)\n", len(resultMap))
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
		// NEVER use array order as it risks data corruption if API reorders results
		fmt.Printf("[BrightDataProvider] 🔍 Using prompt-based result matching for safety\n")

		// Build map of all results by prompt
		// Handle both success results (prompt at top level) and error results (prompt in input.prompt)
		allResults := make(map[string]*BrightDataResult)
		for _, result := range resultMap {
			prompt := result.Prompt
			if prompt == "" && result.Input != nil {
				prompt = result.Input.Prompt // Error results have prompt in input
			}
			if prompt != "" {
				allResults[prompt] = result
			}
		}
		// Add any results that had invalid indices
		for i := range results {
			index := results[i].Index
			if index == 0 && results[i].Input != nil {
				index = results[i].Input.Index
			}
			if index < 1 || index > len(queries) {
				prompt := results[i].Prompt
				if prompt == "" && results[i].Input != nil {
					prompt = results[i].Input.Prompt
				}
				if prompt != "" {
					allResults[prompt] = &results[i]
				}
			}
		}

		fmt.Printf("[BrightDataProvider] 📊 Built result map with %d prompts\n", len(allResults))

		// Match each query to its result by prompt text
		for i, query := range queries {
			result, exists := allResults[query]
			if !exists {
				return nil, fmt.Errorf("no result found for query: %q (have %d results)", query, len(allResults))
			}
			responses[i] = p.convertResultToResponse(result, i+1)
			fmt.Printf("[BrightDataProvider] ✓ Matched query %d by prompt text\n", i+1)
		}
	}

	fmt.Printf("[BrightDataProvider] ✅ Batch completed: %d questions processed, total cost: $%.4f\n",
		len(responses), float64(len(responses))*0.0015)

	return responses, nil
}

// convertResultToResponse converts a BrightDataResult to an AIResponse
func (p *brightDataProvider) convertResultToResponse(result *BrightDataResult, displayIndex int) *AIResponse {
	// Parse citations if available
	var citations []string
	if result.Citations != nil {
		switch v := result.Citations.(type) {
		case []interface{}:
			for _, citation := range v {
				if str, ok := citation.(string); ok {
					citations = append(citations, str)
				}
			}
		case string:
			if v != "" {
				citations = []string{v}
			}
		}
	}

	// Handle response
	var responseText string
	var shouldProcessEvaluation bool

	if result.Error != "" {
		responseText = "Question run failed for this model and location"
		shouldProcessEvaluation = false
		fmt.Printf("[BrightDataProvider] ⚠️ Question %d returned error: %s\n", displayIndex, result.Error)
	} else if result.AnswerTextMarkdown == "" {
		responseText = "Question run failed for this model and location"
		shouldProcessEvaluation = false
		fmt.Printf("[BrightDataProvider] ⚠️ Question %d returned empty answer_text_markdown\n", displayIndex)
	} else {
		// Fix citations in the response text by converting [position] to [position](url)
		responseText = p.fixCitationsInResponse(result.AnswerTextMarkdown, result.LinksAttached)
		shouldProcessEvaluation = true
	}

	if !shouldProcessEvaluation {
		citations = []string{}
	}

	return &AIResponse{
		Response:                responseText,
		InputTokens:             0,
		OutputTokens:            0,
		Cost:                    0.0015, // Fixed cost per API call
		Citations:               citations,
		ShouldProcessEvaluation: shouldProcessEvaluation,
	}
}

// submitBatchJob submits multiple queries to BrightData in a single API call
func (p *brightDataProvider) submitBatchJob(ctx context.Context, queries []string, location *workflowModels.Location, websearch bool) (string, error) {
	country := p.mapLocationToCountry(location)

	// Build input array with all queries
	inputs := make([]BrightDataInput, len(queries))
	for i, query := range queries {
		inputs[i] = BrightDataInput{
			URL:              "https://chatgpt.com/",
			Prompt:           query,
			Country:          country,
			WebSearch:        websearch,
			Index:            i + 1,
			AdditionalPrompt: "",
		}
	}

	payload := BrightDataRequest{
		Input: inputs,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

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
			fmt.Printf("[BrightDataProvider] ⚠️ Batch trigger request failed (attempt %d/%d): %v\n", attempt, maxRetries, err)
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
			fmt.Printf("[BrightDataProvider] ⚠️ Batch trigger returned status %d (attempt %d/%d), retrying\n", resp.StatusCode, attempt, maxRetries)
			if attempt < maxRetries {
				time.Sleep(2 * time.Second)
				continue
			}
			break
		}

		var triggerResp BrightDataTriggerResponse
		if err := json.NewDecoder(resp.Body).Decode(&triggerResp); err != nil {
			resp.Body.Close()
			return "", fmt.Errorf("failed to decode trigger response: %w", err)
		}
		resp.Body.Close()
		return triggerResp.SnapshotID, nil
	}

	if lastErr != nil {
		fmt.Printf("[BrightDataProvider] ❌ Batch trigger failed after %d attempts: %v\n", maxRetries, lastErr)
		return "", fmt.Errorf("failed to make request: %w", lastErr)
	}

	fmt.Printf("[BrightDataProvider] ❌ Batch trigger failed after %d attempts: status=%d body=%s\n", maxRetries, lastStatus, lastBody)
	return "", fmt.Errorf("BrightData API returned status %d: %s", lastStatus, lastBody)
}

// pollBatchUntilComplete polls for batch completion and returns all results
func (p *brightDataProvider) pollBatchUntilComplete(ctx context.Context, snapshotID string) ([]BrightDataResult, error) {
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
				fmt.Printf("[BrightDataProvider] ⚠️ Progress check failed (attempt %d), retrying: %v\n", pollCount, err)
				continue
			}

			fmt.Printf("[BrightDataProvider] 📊 Batch job status: %s (poll #%d)\n", status.Status, pollCount)

			if status.Status == "ready" {
				fmt.Printf("[BrightDataProvider] ✅ Batch job completed after %d polls, retrieving results\n", pollCount)
				return p.getBatchResults(ctx, snapshotID)
			}

			if status.Status == "failed" {
				return nil, fmt.Errorf("BrightData batch job failed for snapshot %s", snapshotID)
			}
		}
	}
}

// getBatchResults retrieves all results from a completed batch job
// It includes retry logic for when the snapshot is still building
func (p *brightDataProvider) getBatchResults(ctx context.Context, snapshotID string) ([]BrightDataResult, error) {
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
		fmt.Printf("[BrightDataProvider] 📡 API Response Status Code: %d (attempt %d/%d)\n", resp.StatusCode, attempt, maxRetries)

		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
			resp.Body.Close()
			if attempt < maxRetries {
				fmt.Printf("[BrightDataProvider] ⚠️ Results request returned %d (attempt %d/%d), retrying after %v\n",
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

		fmt.Printf("[BrightDataProvider] 🔍 Response body length: %d bytes\n", len(bodyBytes))
		fmt.Printf("[BrightDataProvider] 🔍 Response body preview: %s\n", string(bodyBytes[:min(500, len(bodyBytes))]))

		// Check if this is a status response (still building)
		isStatus, status, message := p.isStatusResponse(bodyBytes)
		if isStatus {
			if status == "building" {
				fmt.Printf("[BrightDataProvider] ⏳ Snapshot still building (attempt %d/%d): %s\n", attempt, maxRetries, message)
				if attempt < maxRetries {
					fmt.Printf("[BrightDataProvider] 💤 Waiting %v before retry...\n", retryInterval)
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
				fmt.Printf("[BrightDataProvider] ⚠️ Unknown status '%s', attempting to decode as results\n", status)
			}
		}

		// Try to decode as results array
		var results []BrightDataResult
		if err := json.Unmarshal(bodyBytes, &results); err != nil {
			// Save the full response to a text file for inspection
			filename := fmt.Sprintf("brightdata_error_%s.txt", snapshotID)
			if writeErr := os.WriteFile(filename, bodyBytes, 0644); writeErr != nil {
				fmt.Printf("[BrightDataProvider] ⚠️ Failed to write error response to file: %v\n", writeErr)
			} else {
				fmt.Printf("[BrightDataProvider] 💾 Full response saved to: %s\n", filename)
			}

			fmt.Printf("[BrightDataProvider] ❌ Failed to decode as array: %v\n", err)
			fmt.Printf("[BrightDataProvider] 🔍 Response body preview (first 2000 chars):\n%s\n", string(bodyBytes[:min(2000, len(bodyBytes))]))
			return nil, fmt.Errorf("failed to decode results: %w", err)
		}

		if len(results) == 0 {
			fmt.Printf("[BrightDataProvider] ⚠️ Decoded successfully but got 0 results\n")
			return nil, fmt.Errorf("no results returned from BrightData")
		}

		fmt.Printf("[BrightDataProvider] ✅ Successfully retrieved %d results\n", len(results))
		return results, nil
	}

	return nil, fmt.Errorf("failed to retrieve results after %d attempts", maxRetries)
}

// isStatusResponse checks if the response is a status object rather than results
func (p *brightDataProvider) isStatusResponse(bodyBytes []byte) (bool, string, string) {
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

func (p *brightDataProvider) buildLocalizedPrompt(query string, location *workflowModels.Location) string {
	locationDescription := formatLocationForPrompt(location)
	return fmt.Sprintf("Ensure your response is localized to %s. Answer the following question: %s",
		locationDescription, query)
}

func formatLocationForPrompt(location *workflowModels.Location) string {
	if location == nil {
		return "the relevant region and country"
	}

	var parts []string
	if location.City != nil && *location.City != "" {
		parts = append(parts, *location.City)
	}
	if location.Region != nil && *location.Region != "" {
		parts = append(parts, *location.Region)
	}
	if location.Country != "" {
		parts = append(parts, location.Country)
	}

	if len(parts) == 0 {
		return "the relevant region and country"
	}

	return strings.Join(parts, ", ")
}

// fixCitationsInResponse fixes citation markers in the response text by converting
// plain citation markers to markdown links [position](url) using the LinksAttached data.
// This matches the Python implementation that replaces citation placeholders with proper markdown links.
// Handles both [position] and \[position\] formats (escaped brackets).
func (p *brightDataProvider) fixCitationsInResponse(text string, linksAttached []BrightDataLinks) string {
	if len(linksAttached) == 0 {
		return text
	}

	// Create a copy of the text to modify
	result := text

	// Replace each citation marker with [position](url)
	// Python code uses \[position\], so we handle both escaped and non-escaped formats
	for _, link := range linksAttached {
		// Try escaped format first (\[position\]) to match Python implementation
		escapedOldMarker := fmt.Sprintf("\\[%d\\]", link.Position)
		escapedNewMarker := fmt.Sprintf("[%d](%s)", link.Position, link.URL)
		result = strings.ReplaceAll(result, escapedOldMarker, escapedNewMarker)

		// Also handle non-escaped format ([position]) as fallback
		oldMarker := fmt.Sprintf("[%d]", link.Position)
		newMarker := fmt.Sprintf("[%d](%s)", link.Position, link.URL)
		// Only replace if it's not already a markdown link (doesn't contain parentheses)
		if !strings.Contains(result, fmt.Sprintf("[%d](", link.Position)) {
			result = strings.ReplaceAll(result, oldMarker, newMarker)
		}
	}

	return result
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
