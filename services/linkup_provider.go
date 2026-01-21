// services/linkup_provider.go
package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/AI-Template-SDK/senso-workflows/internal/config"
	workflowModels "github.com/AI-Template-SDK/senso-workflows/internal/models"
)

type linkupProvider struct {
	apiKey      string
	baseURL     string
	costService CostService
	httpClient  *http.Client
}

func NewLinkupProvider(cfg *config.Config, model string, costService CostService) AIProvider {
	fmt.Printf("[NewLinkupProvider] Creating Linkup provider\n")
	fmt.Printf("[NewLinkupProvider]   - API Key: %s\n", maskAPIKey(cfg.LinkupAPIKey))

	if cfg.LinkupAPIKey == "" {
		fmt.Printf("[NewLinkupProvider] ⚠️ WARNING: LINKUP_API_KEY is empty!\n")
	}

	return &linkupProvider{
		apiKey:      cfg.LinkupAPIKey,
		baseURL:     "https://api.linkup.so/v1",
		costService: costService,
		httpClient: &http.Client{
			Timeout: 60 * time.Second, // 60 second timeout for synchronous calls
		},
	}
}

func (p *linkupProvider) GetProviderName() string {
	return "linkup"
}

// LinkupRequest represents the request structure for Linkup API
type LinkupRequest struct {
	Query                  string `json:"q"`
	Depth                  string `json:"depth"`
	OutputType             string `json:"outputType"`
	IncludeImages          bool   `json:"includeImages"`
	IncludeInlineCitations bool   `json:"includeInlineCitations"`
}

// LinkupResponse represents the response from Linkup API
type LinkupResponse struct {
	Answer  string         `json:"answer"`
	Sources []LinkupSource `json:"sources"`
}

// LinkupSource represents a single source in the Linkup response
type LinkupSource struct {
	Name    string `json:"name"`
	Snippet string `json:"snippet"`
	URL     string `json:"url"`
}

func (p *linkupProvider) RunQuestion(ctx context.Context, query string, websearch bool, location *workflowModels.Location) (*AIResponse, error) {
	fmt.Printf("[LinkupProvider] 🚀 Making Linkup call for query: %s\n", query)

	// Build location-aware prompt if location is provided
	queryText := query
	if location != nil {
		queryText = p.buildLocationPrompt(query, location)
	}

	// Prepare the request
	requestBody := LinkupRequest{
		Query:                  queryText,
		Depth:                  "standard",
		OutputType:             "sourcedAnswer",
		IncludeImages:          false,
		IncludeInlineCitations: true,
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	fmt.Printf("[LinkupProvider] 📤 Request payload: %s\n", string(jsonData))

	// Make the HTTP request
	url := fmt.Sprintf("%s/search", p.baseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Linkup request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Try to read error response for debugging
		var errorBody bytes.Buffer
		errorBody.ReadFrom(resp.Body)
		fmt.Printf("[LinkupProvider] ❌ Error response: %s\n", errorBody.String())
		return nil, fmt.Errorf("Linkup API returned status %d: %s", resp.StatusCode, errorBody.String())
	}

	// Parse the response
	var linkupResp LinkupResponse
	if err := json.NewDecoder(resp.Body).Decode(&linkupResp); err != nil {
		return nil, fmt.Errorf("failed to decode Linkup response: %w", err)
	}

	// Check if we have a valid response
	shouldProcessEvaluation := linkupResp.Answer != ""
	responseText := linkupResp.Answer
	if !shouldProcessEvaluation {
		responseText = "Question run failed for this model and location"
		fmt.Printf("[LinkupProvider] ⚠️ Linkup returned empty answer\n")
	} else {
		// Fix citations in the response text by converting [1] to [1](url)
		responseText = p.fixCitationsInResponse(linkupResp.Answer, linkupResp.Sources)
	}

	// Extract citation URLs (only for valid responses)
	var citations []string
	if shouldProcessEvaluation {
		for _, source := range linkupResp.Sources {
			if source.URL != "" {
				citations = append(citations, source.URL)
			}
		}
	}

	// Calculate cost - €0.005 per search = $0.0055 USD (at ~1.10 EUR/USD rate)
	fixedCost := 0.0055

	fmt.Printf("[LinkupProvider] ✅ Linkup call completed\n")
	fmt.Printf("[LinkupProvider]   - Response length: %d characters\n", len(responseText))
	fmt.Printf("[LinkupProvider]   - Citations: %d\n", len(citations))
	fmt.Printf("[LinkupProvider]   - Should process evaluation: %t\n", shouldProcessEvaluation)
	fmt.Printf("[LinkupProvider]   - Cost: $%.4f\n", fixedCost)

	return &AIResponse{
		Response:                responseText,
		InputTokens:             0, // Not available from Linkup
		OutputTokens:            0, // Not available from Linkup
		Cost:                    fixedCost,
		Citations:               citations,
		ShouldProcessEvaluation: shouldProcessEvaluation,
	}, nil
}

// RunQuestionWebSearch implements AIProvider for web search without location
func (p *linkupProvider) RunQuestionWebSearch(ctx context.Context, query string) (*AIResponse, error) {
	fmt.Printf("[RunQuestionWebSearch] 🚀 Making web search AI call for query: %s\n", query)

	// For Linkup, web search is always enabled, so we use the same method without location
	return p.RunQuestion(ctx, query, true, nil)
}

func (p *linkupProvider) buildLocationPrompt(query string, location *workflowModels.Location) string {
	locationStr := p.formatLocation(location)

	// Add location context to the question
	return fmt.Sprintf("Answer the following question with specific information relevant to %s:\n\n%s",
		locationStr, query)
}

func (p *linkupProvider) formatLocation(location *workflowModels.Location) string {
	if location == nil {
		return "the location"
	}

	parts := []string{}
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
		return "the location"
	}

	result := ""
	for i, part := range parts {
		if i > 0 {
			result += ", "
		}
		result += part
	}

	return result
}

// SupportsBatching returns false for Linkup (no native batching support)
func (p *linkupProvider) SupportsBatching() bool {
	return false
}

// GetMaxBatchSize returns 1 for Linkup (no batching)
func (p *linkupProvider) GetMaxBatchSize() int {
	return 1
}

// RunQuestionBatch processes questions sequentially for Linkup (no batching support)
func (p *linkupProvider) RunQuestionBatch(ctx context.Context, queries []string, websearch bool, location *workflowModels.Location) ([]*AIResponse, error) {
	fmt.Printf("[LinkupProvider] 🔄 Processing %d questions sequentially (no batching support)\n", len(queries))

	responses := make([]*AIResponse, len(queries))
	for i, query := range queries {
		response, err := p.RunQuestion(ctx, query, websearch, location)
		if err != nil {
			return nil, fmt.Errorf("failed to process question %d: %w", i+1, err)
		}
		responses[i] = response
	}

	return responses, nil
}

// fixCitationsInResponse converts inline citation markers [1][2][3] to markdown links [1](url)[2](url)[3](url)
// using the sources array to map citation numbers to URLs.
func (p *linkupProvider) fixCitationsInResponse(text string, sources []LinkupSource) string {
	if len(sources) == 0 {
		return text
	}

	// Create a copy of the text to modify
	result := text

	// Build a map of citation position to URL
	// Linkup sources are in the order they appear in the answer
	citationMap := make(map[int]string)
	for i, source := range sources {
		position := i + 1 // Citations are 1-indexed
		citationMap[position] = source.URL
	}

	// Replace each citation marker [position] with [position](url)
	for position, url := range citationMap {
		oldMarker := fmt.Sprintf("[%d]", position)
		newMarker := fmt.Sprintf("[%d](%s)", position, url)
		result = strings.Replace(result, oldMarker, newMarker, -1)
	}

	return result
}
