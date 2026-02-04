// services/oxylabs_provider.go
package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/AI-Template-SDK/senso-workflows/internal/config"
	workflowModels "github.com/AI-Template-SDK/senso-workflows/internal/models"
)

// Oxylabs docs (ChatGPT target + Realtime method):
// - https://developers.oxylabs.io/scraping-solutions/web-scraper-api/targets/chatgpt
// - https://developers.oxylabs.io/scraping-solutions/web-scraper-api/integration-methods/realtime
// - https://developers.oxylabs.io/scraping-solutions/web-scraper-api/features/localization/proxy-location
// - https://developers.oxylabs.io/scraping-solutions/web-scraper-api/response-codes

type oxylabsProvider struct {
	username    string
	password    string
	baseURL     string
	costService CostService
	httpClient  *http.Client
}

func NewOxylabsProvider(cfg *config.Config, model string, costService CostService) AIProvider {
	fmt.Printf("[NewOxylabsProvider] Creating Oxylabs provider\n")
	fmt.Printf("[NewOxylabsProvider]   - Username: %s\n", maskSecret(cfg.OxylabsUsername))
	fmt.Printf("[NewOxylabsProvider]   - Password: %s\n", maskSecret(cfg.OxylabsPassword))
	fmt.Printf("[NewOxylabsProvider]   - Endpoint: https://realtime.oxylabs.io/v1/queries\n")
	fmt.Printf("[NewOxylabsProvider]   - Notes: chatgpt source does not support batch requests\n")

	if strings.TrimSpace(cfg.OxylabsUsername) == "" || strings.TrimSpace(cfg.OxylabsPassword) == "" {
		fmt.Printf("[NewOxylabsProvider] ⚠️ WARNING: OXYLABS_USERNAME / OXYLABS_PASSWORD are empty!\n")
	}

	return &oxylabsProvider{
		username:    strings.TrimSpace(cfg.OxylabsUsername),
		password:    strings.TrimSpace(cfg.OxylabsPassword),
		baseURL:     "https://realtime.oxylabs.io/v1/queries",
		costService: costService,
		httpClient: &http.Client{
			Timeout: 4 * time.Minute, // docs show 180s in examples; allow a bit of headroom
		},
	}
}

func (p *oxylabsProvider) GetProviderName() string {
	return "oxylabs"
}

type oxylabsChatGPTRequest struct {
	Source      string `json:"source"`
	Prompt      string `json:"prompt"`
	Parse       bool   `json:"parse"`
	Search      bool   `json:"search"`
	GeoLocation string `json:"geo_location,omitempty"`
}

type oxylabsChatGPTResponse struct {
	Results []oxylabsChatGPTResult `json:"results"`
}

type oxylabsChatGPTResult struct {
	Content      oxylabsChatGPTContent `json:"content"`
	URL          string               `json:"url"`
	Page         int                  `json:"page"`
	JobID        string               `json:"job_id"`
	GeoLocation  string               `json:"geo_location"`
	StatusCode   int                  `json:"status_code"` // Oxylabs scraper status code (see response-codes)
	CreatedAt    string               `json:"created_at"`
	UpdatedAt    string               `json:"updated_at"`
	ParserType   string               `json:"parser_type"`
	ParserPreset *string              `json:"parser_preset"`
}

type oxylabsChatGPTContent struct {
	Prompt         string                 `json:"prompt"`
	LLMModel       string                 `json:"llm_model"`
	MarkdownJSON   []map[string]any       `json:"markdown_json"`
	MarkdownText   string                 `json:"markdown_text"`
	ResponseText   string                 `json:"response_text"`
	Citations      []oxylabsChatGPTSource `json:"citations"`
	Links          []string               `json:"links"`
	ParseStatusCode int                   `json:"parse_status_code"`
}

type oxylabsChatGPTSource struct {
	URL  string `json:"url"`
	Text string `json:"text"`
}

func (p *oxylabsProvider) RunQuestion(ctx context.Context, query string, websearch bool, location *workflowModels.Location) (*AIResponse, error) {
	fmt.Printf("[OxylabsProvider] 🚀 Making Oxylabs ChatGPT call for query: %s\n", query)
	fmt.Printf("[OxylabsProvider]   - Web search: %t\n", websearch)

	if strings.TrimSpace(p.username) == "" || strings.TrimSpace(p.password) == "" {
		return nil, fmt.Errorf("oxylabs credentials are not configured (OXYLABS_USERNAME/OXYLABS_PASSWORD)")
	}

	geo := p.mapLocationToGeoLocation(location)
	if geo != "" {
		fmt.Printf("[OxylabsProvider]   - geo_location: %s\n", geo)
	}

	payload := oxylabsChatGPTRequest{
		Source:      "chatgpt",
		Prompt:      query,
		Parse:       true,
		Search:      websearch,
		GeoLocation: geo,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal oxylabs request: %w", err)
	}

	respBytes, status, err := p.doWithRetries(ctx, jsonData)
	if err != nil {
		return nil, err
	}

	var decoded oxylabsChatGPTResponse
	if err := json.Unmarshal(respBytes, &decoded); err != nil {
		fmt.Printf("[OxylabsProvider] ❌ Failed to decode response JSON (http=%d). Body preview: %s\n",
			status, string(respBytes[:min(2000, len(respBytes))]))
		return nil, fmt.Errorf("failed to decode oxylabs response: %w", err)
	}

	if len(decoded.Results) == 0 {
		return nil, fmt.Errorf("oxylabs returned 0 results (http=%d)", status)
	}

	r := decoded.Results[0]

	// Oxylabs returns HTTP 200 for successful API call, and a scraper-level status_code for job status.
	if r.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("oxylabs job failed: status_code=%d parse_status_code=%d job_id=%s",
			r.StatusCode, r.Content.ParseStatusCode, r.JobID)
	}

	// Prefer markdown_text, but fall back to response_text.
	responseText := strings.TrimSpace(r.Content.MarkdownText)
	if responseText == "" {
		responseText = strings.TrimSpace(r.Content.ResponseText)
	}

	shouldProcessEvaluation := responseText != ""
	if !shouldProcessEvaluation {
		fmt.Printf("[OxylabsProvider] ⚠️ Oxylabs returned empty response text (parse_status_code=%d)\n", r.Content.ParseStatusCode)
		responseText = "Question run failed for this model and location"
	}

	// Extract citations (urls) from structured citations + links, de-duped.
	citations := p.extractCitations(r.Content.Citations, r.Content.Links)
	if !shouldProcessEvaluation {
		citations = []string{}
	}

	fmt.Printf("[OxylabsProvider] ✅ Oxylabs call completed\n")
	fmt.Printf("[OxylabsProvider]   - Model: %s\n", r.Content.LLMModel)
	fmt.Printf("[OxylabsProvider]   - Response length: %d characters\n", len(responseText))
	fmt.Printf("[OxylabsProvider]   - Citations: %d\n", len(citations))
	fmt.Printf("[OxylabsProvider]   - parse_status_code: %d\n", r.Content.ParseStatusCode)

	// Oxylabs billing is plan-specific; we do not guess cost here.
	return &AIResponse{
		Response:                responseText,
		InputTokens:             0,
		OutputTokens:            0,
		Cost:                    0,
		Citations:               citations,
		ShouldProcessEvaluation: shouldProcessEvaluation,
	}, nil
}

func (p *oxylabsProvider) RunQuestionWebSearch(ctx context.Context, query string) (*AIResponse, error) {
	defaultLocation := &workflowModels.Location{Country: "US"}
	return p.RunQuestion(ctx, query, true, defaultLocation)
}

func (p *oxylabsProvider) SupportsBatching() bool { return false }

func (p *oxylabsProvider) GetMaxBatchSize() int { return 1 }

func (p *oxylabsProvider) RunQuestionBatch(ctx context.Context, queries []string, websearch bool, location *workflowModels.Location) ([]*AIResponse, error) {
	// Oxylabs ChatGPT source explicitly does not support batch requests.
	// We still implement a safe sequential fallback for callers that ignore SupportsBatching.
	fmt.Printf("[OxylabsProvider] 🔄 Processing %d questions sequentially (no batching support)\n", len(queries))
	responses := make([]*AIResponse, len(queries))
	for i, q := range queries {
		r, err := p.RunQuestion(ctx, q, websearch, location)
		if err != nil {
			return nil, fmt.Errorf("failed to process question %d: %w", i+1, err)
		}
		responses[i] = r
	}
	return responses, nil
}

func (p *oxylabsProvider) doWithRetries(ctx context.Context, jsonData []byte) ([]byte, int, error) {
	const maxRetries = 5
	var lastStatus int
	var lastBody string
	var lastErr error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, "POST", p.baseURL, bytes.NewReader(jsonData))
		if err != nil {
			return nil, 0, fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.SetBasicAuth(p.username, p.password)

		resp, err := p.httpClient.Do(req)
		if err != nil {
			lastErr = err
			fmt.Printf("[OxylabsProvider] ⚠️ Request failed (attempt %d/%d): %v\n", attempt, maxRetries, err)
			if attempt < maxRetries {
				time.Sleep(time.Duration(attempt) * 2 * time.Second)
				continue
			}
			break
		}

		bodyBytes, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		lastStatus = resp.StatusCode
		lastBody = string(bodyBytes[:min(2000, len(bodyBytes))])

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return bodyBytes, resp.StatusCode, nil
		}

		// Retry transient statuses per Oxylabs response codes: 429, 500, 524, etc.
		if p.isRetryableHTTPStatus(resp.StatusCode) && attempt < maxRetries {
			fmt.Printf("[OxylabsProvider] ⚠️ Non-2xx status %d (attempt %d/%d). Body preview: %s\n",
				resp.StatusCode, attempt, maxRetries, lastBody)
			time.Sleep(time.Duration(attempt) * 2 * time.Second)
			continue
		}

		return nil, resp.StatusCode, fmt.Errorf("oxylabs API returned status %d: %s", resp.StatusCode, lastBody)
	}

	if lastErr != nil {
		return nil, lastStatus, fmt.Errorf("oxylabs request failed after %d attempts: %w", maxRetries, lastErr)
	}
	return nil, lastStatus, fmt.Errorf("oxylabs request failed after %d attempts: status=%d body=%s", maxRetries, lastStatus, lastBody)
}

func (p *oxylabsProvider) isRetryableHTTPStatus(code int) bool {
	switch code {
	case http.StatusRequestTimeout, http.StatusTooManyRequests:
		return true
	case http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	// Oxylabs documents 524 Timeout as well.
	if code == 524 {
		return true
	}
	return false
}

func (p *oxylabsProvider) extractCitations(citations []oxylabsChatGPTSource, links []string) []string {
	seen := make(map[string]struct{})
	var out []string

	for _, c := range citations {
		u := strings.TrimSpace(c.URL)
		if u == "" || !strings.HasPrefix(u, "http") {
			continue
		}
		if _, ok := seen[u]; ok {
			continue
		}
		seen[u] = struct{}{}
		out = append(out, u)
	}

	for _, u := range links {
		u = strings.TrimSpace(u)
		if u == "" || !strings.HasPrefix(u, "http") {
			continue
		}
		if _, ok := seen[u]; ok {
			continue
		}
		seen[u] = struct{}{}
		out = append(out, u)
	}

	return out
}

func (p *oxylabsProvider) mapLocationToGeoLocation(location *workflowModels.Location) string {
	if location == nil {
		return ""
	}
	c := strings.TrimSpace(location.Country)
	if c == "" {
		return ""
	}

	// Oxylabs expects country names (see Proxy Location doc).
	// We map common ISO-3166 codes used in this repo to Oxylabs supported geo_location values.
	switch strings.ToUpper(c) {
	case "US":
		return "United States"
	case "CA":
		return "Canada"
	case "GB", "UK":
		return "United Kingdom"
	case "AU":
		return "Australia"
	case "DE":
		return "Germany"
	case "FR":
		return "France"
	case "IT":
		return "Italy"
	case "ES":
		return "Spain"
	case "NL":
		return "Netherlands"
	case "JP":
		return "Japan"
	case "KR":
		return "Korea" // Oxylabs list uses "Korea" (not "South Korea") in the supported values list excerpt
	case "IN":
		return "India"
	case "BR":
		return "Brazil"
	case "MX":
		return "Mexico"
	default:
		// If caller passes a full country name already, forward it.
		return c
	}
}

func maskSecret(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "(empty)"
	}
	if len(s) <= 4 {
		return "***"
	}
	return s[:2] + "..." + s[len(s)-2:]
}

