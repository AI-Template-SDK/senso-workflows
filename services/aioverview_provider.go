// services/aioverview_provider.go
package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/AI-Template-SDK/senso-workflows/internal/config"
	workflowModels "github.com/AI-Template-SDK/senso-workflows/internal/models"
)

type aiOverviewProvider struct {
	apiKey      string
	zone        string
	baseURL     string
	costService CostService
	httpClient  *http.Client
}

func NewAIOverviewProvider(cfg *config.Config, model string, costService CostService) AIProvider {
	fmt.Printf("[NewAIOverviewProvider] Creating AI Overview provider\n")
	fmt.Printf("[NewAIOverviewProvider]   - API Key: %s\n", maskAPIKey(cfg.BrightDataSERPAPIKey))

	return &aiOverviewProvider{
		apiKey:      cfg.BrightDataSERPAPIKey,
		zone:        "serp_api1",
		baseURL:     "https://api.brightdata.com/request",
		costService: costService,
		httpClient: &http.Client{
			// BrightData's synchronous SERP endpoint with AI Overview is slow and
			// occasionally takes well over a minute. A tight timeout caused frequent
			// "context deadline exceeded" failures, so give it generous headroom.
			Timeout: 240 * time.Second,
		},
	}
}

func (p *aiOverviewProvider) GetProviderName() string {
	return "aioverview"
}

// Brightdata SERP API request structure
type AIOverviewRequest struct {
	Zone   string `json:"zone"`
	URL    string `json:"url"`
	Format string `json:"format"`
}

// Brightdata SERP API response structures
type AIOverviewSERPResponse struct {
	General    AIOverviewGeneral     `json:"general"`
	Input      AIOverviewInput       `json:"input"`
	AIOverview *AIOverviewData       `json:"ai_overview"`
	Organic    []AIOverviewOrganic   `json:"organic"`
}

type AIOverviewGeneral struct {
	SearchEngine string `json:"search_engine"`
	Query        string `json:"query"`
	ResultsCnt   int    `json:"results_cnt"`
	CountryCode  string `json:"country_code"`
}

type AIOverviewInput struct {
	OriginalURL string `json:"original_url"`
	RequestID   string `json:"request_id"`
}

type AIOverviewData struct {
	Texts      []AIOverviewText      `json:"texts"`
	References []AIOverviewReference `json:"references"`
}

type AIOverviewText struct {
	Type            string           `json:"type"`
	Snippet         string           `json:"snippet"`
	Title           string           `json:"title,omitempty"`
	List            []AIOverviewText `json:"list,omitempty"`
	ReferenceIndexes []int           `json:"reference_indexes,omitempty"`
}

type AIOverviewReference struct {
	Href   string `json:"href"`
	Title  string `json:"title"`
	Source string `json:"source"`
	Index  int    `json:"index"`
}

type AIOverviewOrganic struct {
	Link        string `json:"link"`
	Source      string `json:"source"`
	DisplayLink string `json:"display_link"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Rank        int    `json:"rank"`
	GlobalRank  int    `json:"global_rank"`
}

func (p *aiOverviewProvider) RunQuestion(ctx context.Context, query string, websearch bool, location *workflowModels.Location) (*AIResponse, error) {
	fmt.Printf("[AIOverviewProvider] Making AI Overview call for query: %s\n", query)

	// Build the Google search URL with required parameters
	searchURL := p.buildSearchURL(query, location)
	fmt.Printf("[AIOverviewProvider] Search URL: %s\n", searchURL)

	LogProvider(p.GetProviderName(), "RunQuestion query=%q location=%s searchURL=%s", query, formatLocation(location), searchURL)

	// Make the API request
	result, err := p.makeRequest(ctx, searchURL)
	if err != nil {
		LogProvider(p.GetProviderName(), "makeRequest failed query=%q err=%v", query, err)
		return nil, fmt.Errorf("failed to make AI Overview request: %w", err)
	}

	// Process the response
	return p.processResponse(result)
}

func (p *aiOverviewProvider) RunQuestionWebSearch(ctx context.Context, query string) (*AIResponse, error) {
	// AI Overview is always a web search
	defaultLocation := &workflowModels.Location{
		Country: "US",
	}
	return p.RunQuestion(ctx, query, true, defaultLocation)
}

func (p *aiOverviewProvider) buildSearchURL(query string, location *workflowModels.Location) string {
	// Get normalized country code
	normalized := normalizeLocation(location)
	countryCode := normalized.CountryCode

	// URL encode the query
	encodedQuery := url.QueryEscape(query)

	// Build Google search URL with AI Overview parameters
	// brd_json=1: Get parsed JSON response
	// brd_ai_overview=2: Increase likelihood of AI Overview
	searchURL := fmt.Sprintf(
		"https://www.google.com/search?q=%s&gl=%s&brd_json=1&brd_ai_overview=2",
		encodedQuery,
		countryCode,
	)

	return searchURL
}

func (p *aiOverviewProvider) makeRequest(ctx context.Context, searchURL string) (*AIOverviewSERPResponse, error) {
	// Build request payload
	payload := AIOverviewRequest{
		Zone:   p.zone,
		URL:    searchURL,
		Format: "raw", // Returns the parsed JSON directly when brd_json=1 is in URL
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	fmt.Printf("[AIOverviewProvider] Request payload: %s\n", string(jsonData))

	// Retry logic
	maxRetries := 3
	var lastErr error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, "POST", p.baseURL, bytes.NewBuffer(jsonData))
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		req.Header.Set("Authorization", "Bearer "+p.apiKey)
		req.Header.Set("Content-Type", "application/json")

		resp, err := p.httpClient.Do(req)
		if err != nil {
			lastErr = err
			fmt.Printf("[AIOverviewProvider] Request failed (attempt %d/%d): %v\n", attempt, maxRetries, err)
			LogProvider(p.GetProviderName(), "HTTP request failed (attempt %d/%d) url=%s err=%v", attempt, maxRetries, searchURL, err)
			if attempt < maxRetries {
				time.Sleep(2 * time.Second)
				continue
			}
			break
		}

		defer resp.Body.Close()

		// BrightData reports auth / config / proxy errors via x-brd-* response
		// headers — often alongside HTTP 200 and an EMPTY body, which otherwise
		// looks like a generic "empty response". Surface the real error here.
		// Example: x-brd-err-code=client_10030 ("IP not whitelisted in this zone").
		if brdErrCode := resp.Header.Get("x-brd-err-code"); brdErrCode != "" {
			brdErr := resp.Header.Get("x-brd-error")
			brdErrMsg := resp.Header.Get("x-brd-err-msg")
			LogProvider(p.GetProviderName(), "BrightData error (attempt %d/%d) status=%d code=%s error=%q msg=%q url=%s",
				attempt, maxRetries, resp.StatusCode, brdErrCode, brdErr, brdErrMsg, searchURL)
			fmt.Printf("[AIOverviewProvider] BrightData error (attempt %d/%d): code=%s error=%q msg=%q\n",
				attempt, maxRetries, brdErrCode, brdErr, brdErrMsg)

			lastErr = fmt.Errorf("BrightData error %s: %s", brdErrCode, brdErrMsg)

			// client_* codes are auth/config problems (e.g. IP not whitelisted,
			// bad zone). Retrying is pointless and only adds load/latency — fail fast.
			if strings.HasPrefix(brdErrCode, "client_") {
				LogProvider(p.GetProviderName(), "non-retryable BrightData auth/config error code=%s — failing fast", brdErrCode)
				return nil, lastErr
			}
			if attempt < maxRetries {
				time.Sleep(2 * time.Second)
				continue
			}
			break
		}

		if resp.StatusCode != http.StatusOK {
			bodyBytes, _ := io.ReadAll(resp.Body)
			lastErr = fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(bodyBytes))
			fmt.Printf("[AIOverviewProvider] API error (attempt %d/%d): %v\n", attempt, maxRetries, lastErr)
			LogProvider(p.GetProviderName(), "non-200 (attempt %d/%d) status=%d url=%s body=%s", attempt, maxRetries, resp.StatusCode, searchURL, string(bodyBytes))
			if attempt < maxRetries {
				time.Sleep(2 * time.Second)
				continue
			}
			break
		}

		// Read and parse response
		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			// A truncated read (e.g. connection reset mid-body) is transient — retry.
			lastErr = fmt.Errorf("failed to read response body: %w", err)
			LogProvider(p.GetProviderName(), "failed reading response body (attempt %d/%d) url=%s err=%v", attempt, maxRetries, searchURL, err)
			if attempt < maxRetries {
				time.Sleep(2 * time.Second)
				continue
			}
			break
		}

		fmt.Printf("[AIOverviewProvider] Response body length: %d bytes\n", len(bodyBytes))

		// BrightData sometimes returns 200 with an empty or truncated body when its
		// upstream times out. That surfaces as "unexpected end of JSON input" — a
		// transient condition, so treat empty/unparseable bodies as retryable.
		if len(bytes.TrimSpace(bodyBytes)) == 0 {
			lastErr = fmt.Errorf("empty response body (status 200)")
			LogProvider(p.GetProviderName(), "empty response body (attempt %d/%d) url=%s", attempt, maxRetries, searchURL)
			if attempt < maxRetries {
				time.Sleep(2 * time.Second)
				continue
			}
			break
		}

		var result AIOverviewSERPResponse
		if err := json.Unmarshal(bodyBytes, &result); err != nil {
			// Log the raw body so unparseable / unexpected payloads can be inspected.
			lastErr = fmt.Errorf("failed to parse response: %w", err)
			LogProvider(p.GetProviderName(), "JSON parse FAILED (attempt %d/%d) url=%s err=%v rawBody=%s", attempt, maxRetries, searchURL, err, string(bodyBytes))
			if attempt < maxRetries {
				time.Sleep(2 * time.Second)
				continue
			}
			break
		}

		LogProvider(p.GetProviderName(), "response parsed status=200 bodyLen=%d query=%q aiOverviewPresent=%v organicResults=%d",
			len(bodyBytes), result.General.Query, result.AIOverview != nil, len(result.Organic))

		return &result, nil
	}

	if lastErr != nil {
		LogProvider(p.GetProviderName(), "request FAILED after %d attempts url=%s err=%v", maxRetries, searchURL, lastErr)
		return nil, fmt.Errorf("request failed after %d attempts: %w", maxRetries, lastErr)
	}

	return nil, fmt.Errorf("request failed after %d attempts", maxRetries)
}

func (p *aiOverviewProvider) processResponse(result *AIOverviewSERPResponse) (*AIResponse, error) {
	var responseText string
	var shouldProcessEvaluation bool

	// Check if AI Overview is present
	if result.AIOverview == nil || len(result.AIOverview.Texts) == 0 {
		fmt.Printf("[AIOverviewProvider] No AI Overview returned for this query\n")
		LogProvider(p.GetProviderName(), "NO AI OVERVIEW query=%q aiOverviewNil=%v textBlocks=%d organicResults=%d",
			result.General.Query, result.AIOverview == nil, func() int {
				if result.AIOverview == nil {
					return 0
				}
				return len(result.AIOverview.Texts)
			}(), len(result.Organic))
		responseText = "No AI Overview was generated for this query. Google did not provide an AI-generated summary for this search."
		shouldProcessEvaluation = false
	} else {
		// Extract AI Overview text
		overviewText := p.extractAIOverviewText(result.AIOverview)
		responseText = overviewText
		shouldProcessEvaluation = true
		fmt.Printf("[AIOverviewProvider] AI Overview extracted: %d characters\n", len(overviewText))
	}

	// Append organic results as sources (for citation extraction)
	if len(result.Organic) > 0 {
		var sources []string
		for _, organic := range result.Organic {
			if organic.Link != "" {
				sources = append(sources, organic.Link)
			}
		}

		if len(sources) > 0 {
			responseText += "\n\nSources:\n"
			for _, source := range sources {
				responseText += fmt.Sprintf("- %s\n", source)
			}
			fmt.Printf("[AIOverviewProvider] Added %d organic results as sources\n", len(sources))
		}
	}

	// Extract citations from organic results
	var citations []string
	for _, organic := range result.Organic {
		if organic.Link != "" {
			citations = append(citations, organic.Link)
		}
	}

	fmt.Printf("[AIOverviewProvider] AI Overview call completed\n")
	fmt.Printf("[AIOverviewProvider]   - Response length: %d characters\n", len(responseText))
	fmt.Printf("[AIOverviewProvider]   - Citations: %d\n", len(citations))
	fmt.Printf("[AIOverviewProvider]   - Should process evaluation: %t\n", shouldProcessEvaluation)
	fmt.Printf("[AIOverviewProvider]   - Cost: $0.0015\n")

	return &AIResponse{
		Response:                responseText,
		InputTokens:             0,      // Not applicable for SERP API
		OutputTokens:            0,      // Not applicable for SERP API
		Cost:                    0.0015, // $1.50/1000 requests
		Citations:               citations,
		ShouldProcessEvaluation: shouldProcessEvaluation,
	}, nil
}

func (p *aiOverviewProvider) extractAIOverviewText(overview *AIOverviewData) string {
	var parts []string

	for _, text := range overview.Texts {
		extracted := p.extractTextBlock(text)
		if extracted != "" {
			parts = append(parts, extracted)
		}
	}

	return strings.Join(parts, "\n\n")
}

func (p *aiOverviewProvider) extractTextBlock(text AIOverviewText) string {
	switch text.Type {
	case "paragraph":
		return text.Snippet
	case "list":
		// If there's a title, include it
		var listParts []string
		if text.Title != "" {
			listParts = append(listParts, text.Title)
		}
		// Extract list items
		for _, item := range text.List {
			if item.Snippet != "" {
				listParts = append(listParts, "- "+item.Snippet)
			}
		}
		return strings.Join(listParts, "\n")
	default:
		// For unknown types, try to use snippet
		if text.Snippet != "" {
			return text.Snippet
		}
	}
	return ""
}

// SupportsBatching returns false for AI Overview (synchronous API)
func (p *aiOverviewProvider) SupportsBatching() bool {
	return false
}

// GetMaxBatchSize returns 1 for AI Overview (no batching)
func (p *aiOverviewProvider) GetMaxBatchSize() int {
	return 1
}

// RunQuestionBatch processes questions sequentially (no native batching)
func (p *aiOverviewProvider) RunQuestionBatch(ctx context.Context, queries []string, websearch bool, location *workflowModels.Location) ([]*AIResponse, error) {
	fmt.Printf("[AIOverviewProvider] Processing %d queries sequentially (no batching support)\n", len(queries))

	responses := make([]*AIResponse, len(queries))

	for i, query := range queries {
		response, err := p.RunQuestion(ctx, query, websearch, location)
		if err != nil {
			return nil, fmt.Errorf("failed to process query %d: %w", i+1, err)
		}
		responses[i] = response

		// Small delay between requests to avoid rate limiting
		if i < len(queries)-1 {
			time.Sleep(500 * time.Millisecond)
		}
	}

	fmt.Printf("[AIOverviewProvider] Batch completed: %d questions processed, total cost: $%.4f\n",
		len(responses), float64(len(responses))*0.0015)

	return responses, nil
}
