// services/grok_provider.go
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

// grokProvider talks to BrightData's Grok scraper dataset.
// The Grok scraper accepts a prompt plus optional country / web_search inputs and
// returns an answer_text_markdown payload along with citations and links_attached.
type grokProvider struct {
	apiKey      string
	datasetID   string
	baseURL     string
	costService CostService
	httpClient  *http.Client
}

func NewGrokProvider(cfg *config.Config, model string, costService CostService) AIProvider {
	fmt.Printf("[NewGrokProvider] Creating Grok provider\n")
	fmt.Printf("[NewGrokProvider]   - API Key: %s\n", maskAPIKey(cfg.BrightDataAPIKey))
	fmt.Printf("[NewGrokProvider]   - Dataset ID: %s\n", cfg.GrokDatasetID)

	if cfg.GrokDatasetID == "" {
		fmt.Printf("[NewGrokProvider] ⚠️ WARNING: GROK_DATASET_ID is empty!\n")
	}

	return &grokProvider{
		apiKey:      cfg.BrightDataAPIKey,
		datasetID:   cfg.GrokDatasetID,
		baseURL:     "https://api.brightdata.com/datasets/v3",
		costService: costService,
		httpClient: &http.Client{
			Timeout: 20 * time.Minute, // Long timeout for async operations
		},
	}
}

func (p *grokProvider) GetProviderName() string {
	return "grok"
}

// Grok request structures.
type GrokRequest []GrokInput

type GrokInput struct {
	URL     string `json:"url"`
	Prompt  string `json:"prompt"`
	Country string `json:"country"`
	Index   int    `json:"index"`
}

type GrokTriggerResponse struct {
	SnapshotID string `json:"snapshot_id"`
}

type GrokProgressResponse struct {
	Status             string `json:"status"`
	SnapshotID         string `json:"snapshot_id"`
	DatasetID          string `json:"dataset_id"`
	Records            *int   `json:"records,omitempty"`
	Errors             *int   `json:"errors,omitempty"`
	CollectionDuration *int   `json:"collection_duration,omitempty"`
}

type GrokLink struct {
	URL      string `json:"url"`
	Text     string `json:"text"`
	Position int    `json:"position"`
}

type GrokResult struct {
	URL                string         `json:"url"`
	Prompt             string         `json:"prompt"`
	AnswerTextMarkdown string         `json:"answer_text_markdown"`
	AnswerHTML         string         `json:"answer_html"`
	Citations          interface{}    `json:"citations"`
	Sources            interface{}    `json:"sources"`
	LinksAttached      []GrokLink     `json:"links_attached"`
	WebSearchTriggered bool           `json:"web_search_triggered"`
	Country            string         `json:"country"`
	Index              int            `json:"index"`
	Error              string         `json:"error,omitempty"`
	Input              *GrokInputEcho `json:"input,omitempty"`
}

type GrokInputEcho struct {
	URL       string `json:"url"`
	Prompt    string `json:"prompt"`
	Country   string `json:"country"`
	Index     int    `json:"index"`
	WebSearch bool   `json:"web_search"`
}

func (p *grokProvider) RunQuestion(ctx context.Context, query string, websearch bool, location *workflowModels.Location) (*AIResponse, error) {
	fmt.Printf("[GrokProvider] 🚀 Making Grok call for query: %s\n", query)

	snapshotID, err := p.submitJob(ctx, query, location, websearch)
	if err != nil {
		return nil, fmt.Errorf("failed to submit Grok job: %w", err)
	}

	fmt.Printf("[GrokProvider] 📋 Job submitted with snapshot ID: %s\n", snapshotID)

	result, err := p.pollUntilComplete(ctx, snapshotID)
	if err != nil {
		return nil, fmt.Errorf("failed to poll Grok job: %w", err)
	}

	return p.convertResultToResponse(result, 1), nil
}

func (p *grokProvider) RunQuestionWebSearch(ctx context.Context, query string) (*AIResponse, error) {
	defaultLocation := &workflowModels.Location{Country: "US"}
	return p.RunQuestion(ctx, query, true, defaultLocation)
}

func (p *grokProvider) submitJob(ctx context.Context, query string, location *workflowModels.Location, websearch bool) (string, error) {
	country := p.mapLocationToCountry(location)

	_ = websearch
	payload := GrokRequest{
		{
			URL:     "https://grok.com/",
			Prompt:  query,
			Country: country,
			Index:   1,
		},
	}

	return p.triggerJob(ctx, payload)
}

func (p *grokProvider) triggerJob(ctx context.Context, payload GrokRequest) (string, error) {
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
			fmt.Printf("[GrokProvider] ⚠️ Trigger request failed (attempt %d/%d): %v\n", attempt, maxRetries, err)
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
			fmt.Printf("[GrokProvider] ⚠️ Trigger returned status %d (attempt %d/%d), retrying\n", resp.StatusCode, attempt, maxRetries)
			if attempt < maxRetries {
				time.Sleep(2 * time.Second)
				continue
			}
			break
		}

		var triggerResp GrokTriggerResponse
		if err := json.NewDecoder(resp.Body).Decode(&triggerResp); err != nil {
			resp.Body.Close()
			return "", fmt.Errorf("failed to decode trigger response: %w", err)
		}
		resp.Body.Close()
		return triggerResp.SnapshotID, nil
	}

	if lastErr != nil {
		fmt.Printf("[GrokProvider] ❌ Trigger failed after %d attempts: %v\n", maxRetries, lastErr)
		return "", fmt.Errorf("failed to make request: %w", lastErr)
	}

	fmt.Printf("[GrokProvider] ❌ Trigger failed after %d attempts: status=%d body=%s\n", maxRetries, lastStatus, lastBody)
	return "", fmt.Errorf("Grok API returned status %d: %s", lastStatus, lastBody)
}

func (p *grokProvider) pollUntilComplete(ctx context.Context, snapshotID string) (*GrokResult, error) {
	results, err := p.pollBatchUntilComplete(ctx, snapshotID)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("no results returned from Grok")
	}
	return &results[0], nil
}

func (p *grokProvider) checkProgress(ctx context.Context, snapshotID string) (*GrokProgressResponse, error) {
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

	var progressResp GrokProgressResponse
	if err := json.NewDecoder(resp.Body).Decode(&progressResp); err != nil {
		return nil, fmt.Errorf("failed to decode progress response: %w", err)
	}

	return &progressResp, nil
}

func (p *grokProvider) mapLocationToCountry(location *workflowModels.Location) string {
	normalized := normalizeLocation(location)
	return normalized.CountryCode
}

// SupportsBatching returns true for Grok (supports batch processing via BrightData).
func (p *grokProvider) SupportsBatching() bool {
	return true
}

// GetMaxBatchSize returns 100 for Grok.
func (p *grokProvider) GetMaxBatchSize() int {
	return 100
}

// RunQuestionBatch processes multiple questions in a single Grok API call.
func (p *grokProvider) RunQuestionBatch(ctx context.Context, queries []string, websearch bool, location *workflowModels.Location) ([]*AIResponse, error) {
	fmt.Printf("[GrokProvider] 🚀 Making batched Grok call for %d queries\n", len(queries))

	if len(queries) > 100 {
		return nil, fmt.Errorf("batch size %d exceeds maximum of 100", len(queries))
	}

	localizedQueries := make([]string, len(queries))
	for i, query := range queries {
		localizedQueries[i] = p.buildLocalizedPrompt(query, location)
	}
	queries = localizedQueries

	snapshotID, err := p.submitBatchJob(ctx, queries, location, websearch)
	if err != nil {
		return nil, fmt.Errorf("failed to submit Grok batch job: %w", err)
	}

	fmt.Printf("[GrokProvider] 📋 Batch job submitted with snapshot ID: %s\n", snapshotID)

	results, err := p.pollBatchUntilComplete(ctx, snapshotID)
	if err != nil {
		return nil, fmt.Errorf("failed to poll Grok batch job: %w", err)
	}

	// Match results to queries: first by index, then by prompt text on collision.
	resultMap := make(map[int]*GrokResult)
	unmatchedResults := []*GrokResult{}
	hasValidIndices := true

	for i := range results {
		index := results[i].Index
		if index == 0 && results[i].Input != nil {
			index = results[i].Input.Index
		}

		if index < 1 || index > len(queries) {
			fmt.Printf("[GrokProvider] ⚠️ Result %d has invalid index %d, will match by prompt\n", i, index)
			unmatchedResults = append(unmatchedResults, &results[i])
			hasValidIndices = false
			continue
		}

		if _, exists := resultMap[index]; exists {
			fmt.Printf("[GrokProvider] ⚠️ Duplicate result index: %d, will match by prompt\n", index)
			unmatchedResults = append(unmatchedResults, &results[i])
			hasValidIndices = false
			continue
		}

		resultMap[index] = &results[i]
	}

	responses := make([]*AIResponse, len(queries))

	if hasValidIndices && len(resultMap) == len(queries) {
		fmt.Printf("[GrokProvider] ✅ Using index-based result mapping\n")
		for i := range queries {
			queryIndex := i + 1
			result, exists := resultMap[queryIndex]
			if !exists {
				return nil, fmt.Errorf("missing result for query index %d", queryIndex)
			}
			responses[i] = p.convertResultToResponse(result, queryIndex)
		}
	} else {
		fmt.Printf("[GrokProvider] 🔍 Using prompt-based result matching for safety\n")

		allResults := make(map[string]*GrokResult)
		for _, result := range resultMap {
			prompt := result.Prompt
			if prompt == "" && result.Input != nil {
				prompt = result.Input.Prompt
			}
			if prompt != "" {
				allResults[prompt] = result
			}
		}
		for _, result := range unmatchedResults {
			prompt := result.Prompt
			if prompt == "" && result.Input != nil {
				prompt = result.Input.Prompt
			}
			if prompt != "" {
				allResults[prompt] = result
			}
		}

		fmt.Printf("[GrokProvider] 📊 Built result map with %d prompts\n", len(allResults))

		for i, query := range queries {
			result, exists := allResults[query]
			if !exists {
				return nil, fmt.Errorf("no result found for query: %q (have %d results)", query, len(allResults))
			}
			responses[i] = p.convertResultToResponse(result, i+1)
			fmt.Printf("[GrokProvider] ✓ Matched query %d by prompt text\n", i+1)
		}
	}

	fmt.Printf("[GrokProvider] ✅ Batch completed: %d questions processed, total cost: $%.4f\n",
		len(responses), float64(len(responses))*0.0015)

	return responses, nil
}

// convertResultToResponse converts a GrokResult to an AIResponse.
func (p *grokProvider) convertResultToResponse(result *GrokResult, displayIndex int) *AIResponse {
	var responseText string
	var shouldProcessEvaluation bool

	if result.Error != "" {
		responseText = "This prompt didn’t complete successfully due to a temporary AI model limitation. You were not charged for this prompt. We'll re-try in the next run."
		shouldProcessEvaluation = false
		fmt.Printf("[GrokProvider] ⚠️ Question %d returned error: %s\n", displayIndex, result.Error)
	} else if result.AnswerTextMarkdown == "" {
		responseText = "This prompt didn’t complete successfully due to a temporary AI model limitation. You were not charged for this prompt. We'll re-try in the next run."
		shouldProcessEvaluation = false
		fmt.Printf("[GrokProvider] ⚠️ Question %d returned empty answer_text_markdown\n", displayIndex)
	} else {
		responseText = p.fixCitationsInResponse(result.AnswerTextMarkdown, result.LinksAttached)
		shouldProcessEvaluation = true
	}

	var citations []string
	if shouldProcessEvaluation {
		citations = p.extractCitations(result)
	} else {
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

// extractCitations gathers and deduplicates URLs from citations, sources, and links_attached.
func (p *grokProvider) extractCitations(result *GrokResult) []string {
	if result == nil {
		return []string{}
	}

	seen := make(map[string]bool)
	citations := make([]string, 0)

	addURL := func(u string) {
		u = strings.TrimSpace(u)
		if u == "" || !strings.HasPrefix(u, "http") {
			return
		}
		if seen[u] {
			return
		}
		seen[u] = true
		citations = append(citations, u)
	}

	for _, link := range result.LinksAttached {
		addURL(link.URL)
	}

	collect := func(value interface{}) {
		if value == nil {
			return
		}
		switch v := value.(type) {
		case string:
			addURL(v)
		case []interface{}:
			for _, item := range v {
				switch entry := item.(type) {
				case string:
					addURL(entry)
				case map[string]interface{}:
					if u, ok := entry["url"].(string); ok {
						addURL(u)
					}
					if u, ok := entry["link"].(string); ok {
						addURL(u)
					}
					if u, ok := entry["source"].(string); ok {
						addURL(u)
					}
				}
			}
		case map[string]interface{}:
			if u, ok := v["url"].(string); ok {
				addURL(u)
			}
		}
	}

	collect(result.Citations)
	collect(result.Sources)

	return citations
}

// fixCitationsInResponse converts plain [position] markers in markdown to clickable links.
func (p *grokProvider) fixCitationsInResponse(text string, linksAttached []GrokLink) string {
	if len(linksAttached) == 0 {
		return text
	}

	result := text
	for _, link := range linksAttached {
		escapedOldMarker := fmt.Sprintf("\\[%d\\]", link.Position)
		escapedNewMarker := fmt.Sprintf("[%d](%s)", link.Position, link.URL)
		result = strings.ReplaceAll(result, escapedOldMarker, escapedNewMarker)

		oldMarker := fmt.Sprintf("[%d]", link.Position)
		newMarker := fmt.Sprintf("[%d](%s)", link.Position, link.URL)
		if !strings.Contains(result, fmt.Sprintf("[%d](", link.Position)) {
			result = strings.ReplaceAll(result, oldMarker, newMarker)
		}
	}

	return result
}

// submitBatchJob submits multiple queries to Grok in a single API call.
func (p *grokProvider) submitBatchJob(ctx context.Context, queries []string, location *workflowModels.Location, websearch bool) (string, error) {
	country := p.mapLocationToCountry(location)

	_ = websearch
	payload := make(GrokRequest, len(queries))
	for i, query := range queries {
		payload[i] = GrokInput{
			URL:     "https://grok.com/",
			Prompt:  query,
			Country: country,
			Index:   i + 1,
		}
	}

	return p.triggerJob(ctx, payload)
}

// pollBatchUntilComplete polls for batch completion and returns all results.
func (p *grokProvider) pollBatchUntilComplete(ctx context.Context, snapshotID string) ([]GrokResult, error) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	pollCount := 0
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			pollCount++
			status, err := p.checkProgress(ctx, snapshotID)
			if err != nil {
				fmt.Printf("[GrokProvider] ⚠️ Progress check failed (attempt %d), retrying: %v\n", pollCount, err)
				continue
			}

			fmt.Printf("[GrokProvider] 📊 Job status: %s (poll #%d)\n", status.Status, pollCount)

			if status.Status == "ready" {
				fmt.Printf("[GrokProvider] ✅ Job completed after %d polls, retrieving results\n", pollCount)
				return p.getBatchResults(ctx, snapshotID)
			}

			if status.Status == "failed" {
				return nil, fmt.Errorf("Grok job failed for snapshot %s", snapshotID)
			}
		}
	}
}

// getBatchResults retrieves all results from a completed batch job.
func (p *grokProvider) getBatchResults(ctx context.Context, snapshotID string) ([]GrokResult, error) {
	maxRetries := 20
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

		fmt.Printf("[GrokProvider] 📡 API Response Status Code: %d (attempt %d/%d)\n", resp.StatusCode, attempt, maxRetries)

		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
			resp.Body.Close()
			if attempt < maxRetries {
				fmt.Printf("[GrokProvider] ⚠️ Results request returned %d (attempt %d/%d), retrying after %v\n",
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

		bodyBytes, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to read response body: %w", err)
		}

		fmt.Printf("[GrokProvider] 🔍 Response body length: %d bytes\n", len(bodyBytes))
		fmt.Printf("[GrokProvider] 🔍 Response body preview: %s\n", string(bodyBytes[:min(500, len(bodyBytes))]))

		isStatus, status, message := p.isStatusResponse(bodyBytes)
		if isStatus {
			if status == "building" {
				fmt.Printf("[GrokProvider] ⏳ Snapshot still building (attempt %d/%d): %s\n", attempt, maxRetries, message)
				if attempt < maxRetries {
					fmt.Printf("[GrokProvider] 💤 Waiting %v before retry...\n", retryInterval)
					select {
					case <-time.After(retryInterval):
						continue
					case <-ctx.Done():
						return nil, ctx.Err()
					}
				} else {
					return nil, fmt.Errorf("snapshot still building after %d attempts", maxRetries)
				}
			} else if status == "failed" {
				return nil, fmt.Errorf("snapshot failed: %s", message)
			} else {
				fmt.Printf("[GrokProvider] ⚠️ Unknown status '%s', attempting to decode as results\n", status)
			}
		}

		var results []GrokResult
		if err := json.Unmarshal(bodyBytes, &results); err != nil {
			filename := fmt.Sprintf("grok_error_%s.txt", snapshotID)
			if writeErr := os.WriteFile(filename, bodyBytes, 0644); writeErr != nil {
				fmt.Printf("[GrokProvider] ⚠️ Failed to write error response to file: %v\n", writeErr)
			} else {
				fmt.Printf("[GrokProvider] 💾 Full response saved to: %s\n", filename)
			}

			fmt.Printf("[GrokProvider] ❌ Failed to decode as array: %v\n", err)
			fmt.Printf("[GrokProvider] 🔍 Response body preview (first 2000 chars):\n%s\n", string(bodyBytes[:min(2000, len(bodyBytes))]))
			return nil, fmt.Errorf("failed to decode results: %w", err)
		}

		if len(results) == 0 {
			fmt.Printf("[GrokProvider] ⚠️ Decoded successfully but got 0 results\n")
			return nil, fmt.Errorf("no results returned from Grok")
		}

		fmt.Printf("[GrokProvider] ✅ Successfully retrieved %d results\n", len(results))
		return results, nil
	}

	return nil, fmt.Errorf("failed to retrieve results after %d attempts", maxRetries)
}

// isStatusResponse checks if the response is a status object rather than results.
func (p *grokProvider) isStatusResponse(bodyBytes []byte) (bool, string, string) {
	var statusResp struct {
		Status  string `json:"status"`
		Message string `json:"message"`
	}

	if err := json.Unmarshal(bodyBytes, &statusResp); err != nil {
		return false, "", ""
	}

	if statusResp.Status != "" {
		return true, statusResp.Status, statusResp.Message
	}

	return false, "", ""
}

func (p *grokProvider) buildLocalizedPrompt(query string, location *workflowModels.Location) string {
	locationDescription := formatLocationForPrompt(location)
	return fmt.Sprintf("Ensure your response is localized to %s. Answer the following question: %s",
		locationDescription, query)
}
