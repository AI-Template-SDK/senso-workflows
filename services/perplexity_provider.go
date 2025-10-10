// services/perplexity_provider.go
package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/AI-Template-SDK/senso-workflows/internal/config"
	workflowModels "github.com/AI-Template-SDK/senso-workflows/internal/models"
)

type perplexityProvider struct {
	apiKey      string
	datasetID   string
	baseURL     string
	costService CostService
	httpClient  *http.Client
}

func NewPerplexityProvider(cfg *config.Config, model string, costService CostService) AIProvider {
	fmt.Printf("[NewPerplexityProvider] Creating Perplexity provider\n")
	fmt.Printf("[NewPerplexityProvider]   - API Key: %s\n", maskAPIKey(cfg.BrightDataAPIKey))
	fmt.Printf("[NewPerplexityProvider]   - Dataset ID: %s\n", cfg.PerplexityDatasetID)

	if cfg.PerplexityDatasetID == "" {
		fmt.Printf("[NewPerplexityProvider] ⚠️ WARNING: PERPLEXITY_DATASET_ID is empty!\n")
	}

	return &perplexityProvider{
		apiKey:      cfg.BrightDataAPIKey,
		datasetID:   cfg.PerplexityDatasetID,
		baseURL:     "https://api.brightdata.com/datasets/v3",
		costService: costService,
		httpClient: &http.Client{
			Timeout: 20 * time.Minute, // Long timeout for async operations
		},
	}
}

// Helper function to mask API key for logging
func maskAPIKey(key string) string {
	if len(key) < 8 {
		return "***"
	}
	return key[:4] + "..." + key[len(key)-4:]
}

func (p *perplexityProvider) GetProviderName() string {
	return "perplexity"
}

// Perplexity API request structures (different from ChatGPT)
type PerplexityRequest []PerplexityInput

type PerplexityInput struct {
	URL                string `json:"url"`
	Prompt             string `json:"prompt"`
	Country            string `json:"country"`
	Index              int    `json:"index"`
	ExportMarkdownFile string `json:"export_markdown_file"`
}

// Perplexity API response structures
type PerplexityTriggerResponse struct {
	SnapshotID string `json:"snapshot_id"`
}

type PerplexityProgressResponse struct {
	Status             string `json:"status"`
	SnapshotID         string `json:"snapshot_id"`
	DatasetID          string `json:"dataset_id"`
	Records            *int   `json:"records,omitempty"`
	Errors             *int   `json:"errors,omitempty"`
	CollectionDuration *int   `json:"collection_duration,omitempty"`
}

type PerplexityResult struct {
	URL                string               `json:"url"`
	Prompt             string               `json:"prompt"`
	AnswerHTML         string               `json:"answer_html"`
	AnswerTextMarkdown string               `json:"answer_text_markdown"`
	Index              int                  `json:"index"`
	Error              string               `json:"error,omitempty"`
	Input              *PerplexityInputEcho `json:"input,omitempty"` // Echoed back on errors
}

type PerplexityInputEcho struct {
	URL     string `json:"url"`
	Prompt  string `json:"prompt"`
	Country string `json:"country"`
	Index   int    `json:"index"`
}

func (p *perplexityProvider) RunQuestion(ctx context.Context, query string, websearch bool, location *workflowModels.Location) (*AIResponse, error) {
	fmt.Printf("[PerplexityProvider] 🚀 Making Perplexity call for query: %s\n", query)

	// 1. Submit job to Perplexity dataset
	snapshotID, err := p.submitJob(ctx, query, location)
	if err != nil {
		return nil, fmt.Errorf("failed to submit Perplexity job: %w", err)
	}

	fmt.Printf("[PerplexityProvider] 📋 Job submitted with snapshot ID: %s\n", snapshotID)

	// 2. Poll until completion
	result, err := p.pollUntilComplete(ctx, snapshotID)
	if err != nil {
		return nil, fmt.Errorf("failed to poll Perplexity job: %w", err)
	}

	// 3. Check for errors in response
	if result.Error != "" {
		return nil, fmt.Errorf("Perplexity returned error: %s", result.Error)
	}

	// 4. Handle response - use answer_text_markdown if available, otherwise create failed response
	var responseText string
	var shouldProcessEvaluation bool

	if result.Error != "" {
		responseText = "Question run failed for this model and location"
		shouldProcessEvaluation = false
		fmt.Printf("[PerplexityProvider] ⚠️ Perplexity returned error: %s\n", result.Error)
	} else if result.AnswerTextMarkdown == "" {
		responseText = "Question run failed for this model and location"
		shouldProcessEvaluation = false
		fmt.Printf("[PerplexityProvider] ⚠️ Perplexity returned empty answer_text_markdown\n")
	} else {
		responseText = result.AnswerTextMarkdown
		shouldProcessEvaluation = true
		fmt.Printf("[PerplexityProvider] ✅ Perplexity returned valid response\n")
	}

	// 5. Only extract citations if we have a valid response
	var citations []string
	if shouldProcessEvaluation && result.AnswerHTML != "" {
		citations = p.extractCitations(result.AnswerHTML)
	}

	fmt.Printf("[PerplexityProvider] ✅ Perplexity call completed\n")
	fmt.Printf("[PerplexityProvider]   - Response length: %d characters\n", len(responseText))
	fmt.Printf("[PerplexityProvider]   - Citations: %d\n", len(citations))
	fmt.Printf("[PerplexityProvider]   - Should process evaluation: %t\n", shouldProcessEvaluation)
	fmt.Printf("[PerplexityProvider]   - Cost: $0.0015\n")

	return &AIResponse{
		Response:                responseText,
		InputTokens:             0,      // Not available from BrightData
		OutputTokens:            0,      // Not available from BrightData
		Cost:                    0.0015, // Fixed cost per API call
		Citations:               citations,
		ShouldProcessEvaluation: shouldProcessEvaluation,
	}, nil
}

func (p *perplexityProvider) RunQuestionWebSearch(ctx context.Context, query string) (*AIResponse, error) {
	// For Perplexity, web search is always enabled, so we can use the same method
	// with a default US location
	defaultLocation := &workflowModels.Location{
		Country: "US",
	}
	return p.RunQuestion(ctx, query, true, defaultLocation)
}

func (p *perplexityProvider) submitJob(ctx context.Context, query string, location *workflowModels.Location) (string, error) {
	country := p.mapLocationToCountry(location)

	// Perplexity uses direct array format, not wrapped in {"input": [...]}
	payload := PerplexityRequest{
		{
			URL:                "https://www.perplexity.ai",
			Prompt:             query,
			Country:            country,
			Index:              1,
			ExportMarkdownFile: "",
		},
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	fmt.Printf("[PerplexityProvider] 📤 Request payload: %s\n", string(jsonData))

	url := fmt.Sprintf("%s/trigger?dataset_id=%s&include_errors=true", p.baseURL, p.datasetID)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Try to read error response for debugging
		var errorBody bytes.Buffer
		errorBody.ReadFrom(resp.Body)
		fmt.Printf("[PerplexityProvider] ❌ Error response: %s\n", errorBody.String())
		return "", fmt.Errorf("Perplexity API returned status %d: %s", resp.StatusCode, errorBody.String())
	}

	var triggerResp PerplexityTriggerResponse
	if err := json.NewDecoder(resp.Body).Decode(&triggerResp); err != nil {
		return "", fmt.Errorf("failed to decode trigger response: %w", err)
	}

	return triggerResp.SnapshotID, nil
}

func (p *perplexityProvider) pollUntilComplete(ctx context.Context, snapshotID string) (*PerplexityResult, error) {
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
				fmt.Printf("[PerplexityProvider] ⚠️ Progress check failed (attempt %d), retrying: %v\n", pollCount, err)
				continue // Retry on error
			}

			fmt.Printf("[PerplexityProvider] 📊 Job status: %s (poll #%d)\n", status.Status, pollCount)

			if status.Status == "ready" {
				fmt.Printf("[PerplexityProvider] ✅ Job completed after %d polls, retrieving results\n", pollCount)
				return p.getResults(ctx, snapshotID)
			}

			if status.Status == "failed" {
				return nil, fmt.Errorf("Perplexity job failed for snapshot %s", snapshotID)
			}

			// Continue polling if status is "running" or other non-terminal states
		}
	}
}

func (p *perplexityProvider) checkProgress(ctx context.Context, snapshotID string) (*PerplexityProgressResponse, error) {
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

	var progressResp PerplexityProgressResponse
	if err := json.NewDecoder(resp.Body).Decode(&progressResp); err != nil {
		return nil, fmt.Errorf("failed to decode progress response: %w", err)
	}

	return &progressResp, nil
}

func (p *perplexityProvider) getResults(ctx context.Context, snapshotID string) (*PerplexityResult, error) {
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
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return nil, fmt.Errorf("results request returned status %d", resp.StatusCode)
	}

	var results []PerplexityResult
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return nil, fmt.Errorf("failed to decode results: %w", err)
	}

	if len(results) == 0 {
		return nil, fmt.Errorf("no results returned from Perplexity")
	}

	// Return the first (and should be only) result
	return &results[0], nil
}

func (p *perplexityProvider) htmlToText(html string) string {
	// Basic HTML to text conversion
	// Remove HTML tags
	re := regexp.MustCompile(`<[^>]*>`)
	text := re.ReplaceAllString(html, "")

	// Decode common HTML entities
	text = strings.ReplaceAll(text, "&amp;", "&")
	text = strings.ReplaceAll(text, "&lt;", "<")
	text = strings.ReplaceAll(text, "&gt;", ">")
	text = strings.ReplaceAll(text, "&quot;", "\"")
	text = strings.ReplaceAll(text, "&#39;", "'")
	text = strings.ReplaceAll(text, "&nbsp;", " ")

	// Clean up whitespace
	lines := strings.Split(text, "\n")
	var cleanLines []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			cleanLines = append(cleanLines, line)
		}
	}

	return strings.Join(cleanLines, "\n")
}

func (p *perplexityProvider) extractCitations(html string) []string {
	// Extract URLs from href attributes
	re := regexp.MustCompile(`href="([^"]+)"`)
	matches := re.FindAllStringSubmatch(html, -1)

	var citations []string
	seen := make(map[string]bool)

	for _, match := range matches {
		if len(match) > 1 {
			url := match[1]
			// Filter out internal/relative links and duplicates
			if strings.HasPrefix(url, "http") && !seen[url] {
				citations = append(citations, url)
				seen[url] = true
			}
		}
	}

	return citations
}

func (p *perplexityProvider) mapLocationToCountry(location *workflowModels.Location) string {
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

// SupportsBatching returns true for Perplexity (supports batch processing via BrightData)
func (p *perplexityProvider) SupportsBatching() bool {
	return true
}

// GetMaxBatchSize returns 20 for Perplexity (can batch up to 20 questions)
func (p *perplexityProvider) GetMaxBatchSize() int {
	return 20
}

// RunQuestionBatch processes multiple questions in a single Perplexity API call
func (p *perplexityProvider) RunQuestionBatch(ctx context.Context, queries []string, websearch bool, location *workflowModels.Location) ([]*AIResponse, error) {
	fmt.Printf("[PerplexityProvider] 🚀 Making batched Perplexity call for %d queries\n", len(queries))

	if len(queries) > 20 {
		return nil, fmt.Errorf("batch size %d exceeds maximum of 20", len(queries))
	}

	// 1. Submit batch job to Perplexity
	snapshotID, err := p.submitBatchJob(ctx, queries, location)
	if err != nil {
		return nil, fmt.Errorf("failed to submit Perplexity batch job: %w", err)
	}

	fmt.Printf("[PerplexityProvider] 📋 Batch job submitted with snapshot ID: %s\n", snapshotID)

	// 2. Poll until completion
	results, err := p.pollBatchUntilComplete(ctx, snapshotID)
	if err != nil {
		return nil, fmt.Errorf("failed to poll Perplexity batch job: %w", err)
	}

	// 3. Match results to queries
	// Strategy 1: Use Index field if valid (1-based indices we sent)
	// Strategy 2: Match by Prompt text if Index is invalid
	// Strategy 3: FAIL - never use array order as it risks data corruption

	resultMap := make(map[int]*PerplexityResult)
	unmatchedResults := []*PerplexityResult{}
	hasValidIndices := true

	for i := range results {
		// Extract index - check both top-level and input.index (for error results)
		index := results[i].Index
		if index == 0 && results[i].Input != nil {
			index = results[i].Input.Index // Error results have index in input
		}

		// Check if index is valid (1-based)
		if index < 1 || index > len(queries) {
			fmt.Printf("[PerplexityProvider] ⚠️ Result %d has invalid index %d (checked both top-level and input), will match by prompt\n", i, index)
			unmatchedResults = append(unmatchedResults, &results[i])
			hasValidIndices = false
			continue
		}

		// Check for duplicate indices
		if _, exists := resultMap[index]; exists {
			fmt.Printf("[PerplexityProvider] ⚠️ Duplicate result index: %d, will match by prompt\n", index)
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
		fmt.Printf("[PerplexityProvider] ✅ Using index-based result mapping\n")
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
		fmt.Printf("[PerplexityProvider] 🔍 Using prompt-based result matching for safety\n")

		// Build map of all results (indexed + unmatched) by prompt
		// Handle both success results (prompt at top level) and error results (prompt in input.prompt)
		allResults := make(map[string]*PerplexityResult)
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

		fmt.Printf("[PerplexityProvider] 📊 Built result map with %d prompts\n", len(allResults))

		// Match each query to its result by prompt text
		for i, query := range queries {
			result, exists := allResults[query]
			if !exists {
				return nil, fmt.Errorf("no result found for query: %q (have %d results)", query, len(allResults))
			}
			responses[i] = p.convertResultToResponse(result, i+1)
			fmt.Printf("[PerplexityProvider] ✓ Matched query %d by prompt text\n", i+1)
		}
	}

	fmt.Printf("[PerplexityProvider] ✅ Batch completed: %d questions processed, total cost: $%.4f\n",
		len(responses), float64(len(responses))*0.0015)

	return responses, nil
}

// convertResultToResponse converts a PerplexityResult to an AIResponse
func (p *perplexityProvider) convertResultToResponse(result *PerplexityResult, displayIndex int) *AIResponse {
	// Handle response
	var responseText string
	var shouldProcessEvaluation bool

	if result.Error != "" {
		responseText = "Question run failed for this model and location"
		shouldProcessEvaluation = false
		fmt.Printf("[PerplexityProvider] ⚠️ Question %d returned error: %s\n", displayIndex, result.Error)
	} else if result.AnswerTextMarkdown == "" {
		responseText = "Question run failed for this model and location"
		shouldProcessEvaluation = false
		fmt.Printf("[PerplexityProvider] ⚠️ Question %d returned empty answer_text_markdown\n", displayIndex)
	} else {
		responseText = result.AnswerTextMarkdown
		shouldProcessEvaluation = true
	}

	// Only extract citations if we have a valid response
	var citations []string
	if shouldProcessEvaluation && result.AnswerHTML != "" {
		citations = p.extractCitations(result.AnswerHTML)
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

// submitBatchJob submits multiple queries to Perplexity in a single API call
func (p *perplexityProvider) submitBatchJob(ctx context.Context, queries []string, location *workflowModels.Location) (string, error) {
	country := p.mapLocationToCountry(location)

	// Build input array with all queries (Perplexity uses direct array format)
	payload := make(PerplexityRequest, len(queries))
	for i, query := range queries {
		payload[i] = PerplexityInput{
			URL:                "https://www.perplexity.ai",
			Prompt:             query,
			Country:            country,
			Index:              i + 1,
			ExportMarkdownFile: "",
		}
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	fmt.Printf("[PerplexityProvider] 📤 Request payload for %d queries\n", len(queries))

	url := fmt.Sprintf("%s/trigger?dataset_id=%s&include_errors=true", p.baseURL, p.datasetID)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errorBody bytes.Buffer
		errorBody.ReadFrom(resp.Body)
		fmt.Printf("[PerplexityProvider] ❌ Error response: %s\n", errorBody.String())
		return "", fmt.Errorf("Perplexity API returned status %d: %s", resp.StatusCode, errorBody.String())
	}

	var triggerResp PerplexityTriggerResponse
	if err := json.NewDecoder(resp.Body).Decode(&triggerResp); err != nil {
		return "", fmt.Errorf("failed to decode trigger response: %w", err)
	}

	return triggerResp.SnapshotID, nil
}

// pollBatchUntilComplete polls for batch completion and returns all results
func (p *perplexityProvider) pollBatchUntilComplete(ctx context.Context, snapshotID string) ([]PerplexityResult, error) {
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
				fmt.Printf("[PerplexityProvider] ⚠️ Progress check failed (attempt %d), retrying: %v\n", pollCount, err)
				continue
			}

			fmt.Printf("[PerplexityProvider] 📊 Batch job status: %s (poll #%d)\n", status.Status, pollCount)

			if status.Status == "ready" {
				fmt.Printf("[PerplexityProvider] ✅ Batch job completed after %d polls, retrieving results\n", pollCount)
				return p.getBatchResults(ctx, snapshotID)
			}

			if status.Status == "failed" {
				return nil, fmt.Errorf("Perplexity batch job failed for snapshot %s", snapshotID)
			}
		}
	}
}

// getBatchResults retrieves all results from a completed batch job
// It includes retry logic for when the snapshot is still building
func (p *perplexityProvider) getBatchResults(ctx context.Context, snapshotID string) ([]PerplexityResult, error) {
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
		fmt.Printf("[PerplexityProvider] 📡 API Response Status Code: %d (attempt %d/%d)\n", resp.StatusCode, attempt, maxRetries)

		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
			resp.Body.Close()
			return nil, fmt.Errorf("results request returned status %d", resp.StatusCode)
		}

		// Read the body first so we can log it if there's an error
		bodyBytes, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to read response body: %w", err)
		}

		fmt.Printf("[PerplexityProvider] 🔍 Response body length: %d bytes\n", len(bodyBytes))
		fmt.Printf("[PerplexityProvider] 🔍 Response body preview: %s\n", string(bodyBytes[:min(500, len(bodyBytes))]))

		// Check if this is a status response (still building)
		isStatus, status, message := p.isStatusResponse(bodyBytes)
		if isStatus {
			if status == "building" {
				fmt.Printf("[PerplexityProvider] ⏳ Snapshot still building (attempt %d/%d): %s\n", attempt, maxRetries, message)
				if attempt < maxRetries {
					fmt.Printf("[PerplexityProvider] 💤 Waiting %v before retry...\n", retryInterval)
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
				fmt.Printf("[PerplexityProvider] ⚠️ Unknown status '%s', attempting to decode as results\n", status)
			}
		}

		// Try to decode as results array
		var results []PerplexityResult
		if err := json.Unmarshal(bodyBytes, &results); err != nil {
			// Save the full response to a text file for inspection
			filename := fmt.Sprintf("perplexity_error_%s.txt", snapshotID)
			if writeErr := os.WriteFile(filename, bodyBytes, 0644); writeErr != nil {
				fmt.Printf("[PerplexityProvider] ⚠️ Failed to write error response to file: %v\n", writeErr)
			} else {
				fmt.Printf("[PerplexityProvider] 💾 Full response saved to: %s\n", filename)
			}

			fmt.Printf("[PerplexityProvider] ❌ Failed to decode as array: %v\n", err)
			fmt.Printf("[PerplexityProvider] 🔍 Response body preview (first 2000 chars):\n%s\n", string(bodyBytes[:min(2000, len(bodyBytes))]))
			return nil, fmt.Errorf("failed to decode results: %w", err)
		}

		if len(results) == 0 {
			fmt.Printf("[PerplexityProvider] ⚠️ Decoded successfully but got 0 results\n")
			return nil, fmt.Errorf("no results returned from Perplexity")
		}

		fmt.Printf("[PerplexityProvider] ✅ Successfully retrieved %d results\n", len(results))
		return results, nil
	}

	return nil, fmt.Errorf("failed to retrieve results after %d attempts", maxRetries)
}

// isStatusResponse checks if the response is a status object rather than results
func (p *perplexityProvider) isStatusResponse(bodyBytes []byte) (bool, string, string) {
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
