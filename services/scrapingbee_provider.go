// services/scrapingbee_provider.go
package services

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/AI-Template-SDK/senso-workflows/internal/config"
	workflowModels "github.com/AI-Template-SDK/senso-workflows/internal/models"
)

// ScrapingBee docs (GPT API):
// - https://www.scrapingbee.com/documentation/chatgpt/
// - https://scrapingbee.com/documentation/country_codes
//
// Endpoint:
// - GET https://app.scrapingbee.com/api/v1/chatgpt
//
// Notes:
// - Each successful request costs 15 API credits (per docs).
// - Citations are not returned 100% of the time; when present they are visible in results_markdown,
//   and (if add_html=true) can also be found under elements with data-testid="webpage-citation-pill".

type scrapingbeeProvider struct {
	apiKey      string
	baseURL     string
	costService CostService
	httpClient  *http.Client
}

func NewScrapingBeeProvider(cfg *config.Config, model string, costService CostService) AIProvider {
	fmt.Printf("[NewScrapingBeeProvider] Creating ScrapingBee provider\n")
	fmt.Printf("[NewScrapingBeeProvider]   - API Key: %s\n", maskAPIKey(cfg.ScrapingBeeAPIKey))
	fmt.Printf("[NewScrapingBeeProvider]   - Endpoint: https://app.scrapingbee.com/api/v1/chatgpt\n")
	fmt.Printf("[NewScrapingBeeProvider]   - Cost: 15 ScrapingBee credits per successful request\n")

	if strings.TrimSpace(cfg.ScrapingBeeAPIKey) == "" {
		fmt.Printf("[NewScrapingBeeProvider] ⚠️ WARNING: SCRAPINGBEE_API_KEY is empty!\n")
	}

	return &scrapingbeeProvider{
		apiKey:      strings.TrimSpace(cfg.ScrapingBeeAPIKey),
		baseURL:     "https://app.scrapingbee.com/api/v1/chatgpt",
		costService: costService,
		httpClient: &http.Client{
			Timeout: 90 * time.Second,
		},
	}
}

func (p *scrapingbeeProvider) GetProviderName() string { return "scrapingbee" }

type scrapingbeeGPTResponse struct {
	FullHTML        string          `json:"full_html"`
	LLMModel        string          `json:"llm_model"`
	Prompt          string          `json:"prompt"`
	ResultsMarkdown string          `json:"results_markdown"`
	ResultsText     string          `json:"results_text"`
	ResultsJSON     json.RawMessage `json:"results_json"`
}

func (p *scrapingbeeProvider) RunQuestion(ctx context.Context, query string, websearch bool, location *workflowModels.Location) (*AIResponse, error) {
	fmt.Printf("[ScrapingBeeProvider] 🚀 Making ScrapingBee GPT call for query: %s\n", query)
	fmt.Printf("[ScrapingBeeProvider]   - Web search: %t\n", websearch)

	if strings.TrimSpace(p.apiKey) == "" {
		return nil, fmt.Errorf("scrapingbee API key is not configured (SCRAPINGBEE_API_KEY)")
	}

	// Build request URL with query params (ScrapingBee GPT API is a GET endpoint).
	u, err := url.Parse(p.baseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse base url: %w", err)
	}
	q := u.Query()
	q.Set("api_key", p.apiKey)
	q.Set("prompt", query)
	q.Set("search", fmt.Sprintf("%t", websearch))

	// Best-effort geo: docs use ISO 3166-1 country codes (lowercase examples in country_codes list).
	if cc := p.mapLocationToCountryCode(location); cc != "" {
		fmt.Printf("[ScrapingBeeProvider]   - country_code: %s\n", cc)
		q.Set("country_code", cc)
	}

	// Only request HTML when we need it for citation pill parsing; it's large.
	// Per docs, citations may exist in results_markdown even without HTML.
	addHTML := websearch
	q.Set("add_html", fmt.Sprintf("%t", addHTML))
	u.RawQuery = q.Encode()

	bodyBytes, status, err := p.doWithRetries(ctx, u.String())
	if err != nil {
		return nil, err
	}

	if status < 200 || status >= 300 {
		preview := string(bodyBytes[:min(2000, len(bodyBytes))])
		return nil, fmt.Errorf("scrapingbee returned status %d: %s", status, preview)
	}

	var decoded scrapingbeeGPTResponse
	if err := json.Unmarshal(bodyBytes, &decoded); err != nil {
		fmt.Printf("[ScrapingBeeProvider] ❌ Failed to decode response JSON (http=%d). Body preview: %s\n",
			status, string(bodyBytes[:min(2000, len(bodyBytes))]))
		return nil, fmt.Errorf("failed to decode scrapingbee response: %w", err)
	}

	// Prefer markdown for downstream parsing (citations appear there when present).
	responseText := strings.TrimSpace(decoded.ResultsMarkdown)
	if responseText == "" {
		responseText = strings.TrimSpace(decoded.ResultsText)
	}

	shouldProcessEvaluation := responseText != ""
	if !shouldProcessEvaluation {
		fmt.Printf("[ScrapingBeeProvider] ⚠️ ScrapingBee returned empty results_markdown/results_text\n")
		responseText = "Question run failed for this model and location"
	}

	citations := p.extractCitations(decoded)
	if !shouldProcessEvaluation {
		citations = []string{}
	}

	fmt.Printf("[ScrapingBeeProvider] ✅ ScrapingBee call completed\n")
	fmt.Printf("[ScrapingBeeProvider]   - Model: %s\n", decoded.LLMModel)
	fmt.Printf("[ScrapingBeeProvider]   - Response length: %d characters\n", len(responseText))
	fmt.Printf("[ScrapingBeeProvider]   - Citations: %d\n", len(citations))

	// ScrapingBee charges in credits; we do not guess $ cost here.
	return &AIResponse{
		Response:                responseText,
		InputTokens:             0,
		OutputTokens:            0,
		Cost:                    0,
		Citations:               citations,
		ShouldProcessEvaluation: shouldProcessEvaluation,
	}, nil
}

func (p *scrapingbeeProvider) RunQuestionWebSearch(ctx context.Context, query string) (*AIResponse, error) {
	defaultLocation := &workflowModels.Location{Country: "US"}
	return p.RunQuestion(ctx, query, true, defaultLocation)
}

func (p *scrapingbeeProvider) SupportsBatching() bool { return false }

func (p *scrapingbeeProvider) GetMaxBatchSize() int { return 1 }

func (p *scrapingbeeProvider) RunQuestionBatch(ctx context.Context, queries []string, websearch bool, location *workflowModels.Location) ([]*AIResponse, error) {
	fmt.Printf("[ScrapingBeeProvider] 🔄 Processing %d questions sequentially (no batching support)\n", len(queries))
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

func (p *scrapingbeeProvider) doWithRetries(ctx context.Context, urlStr string) ([]byte, int, error) {
	const maxRetries = 5
	var lastStatus int
	var lastBody string
	var lastErr error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to create request: %w", err)
		}

		resp, err := p.httpClient.Do(req)
		if err != nil {
			lastErr = err
			fmt.Printf("[ScrapingBeeProvider] ⚠️ Request failed (attempt %d/%d): %v\n", attempt, maxRetries, err)
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

		// Retry on transient errors.
		if (resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500) && attempt < maxRetries {
			fmt.Printf("[ScrapingBeeProvider] ⚠️ Non-2xx status %d (attempt %d/%d). Body preview: %s\n",
				resp.StatusCode, attempt, maxRetries, lastBody)
			time.Sleep(time.Duration(attempt) * 2 * time.Second)
			continue
		}

		return bodyBytes, resp.StatusCode, fmt.Errorf("scrapingbee returned status %d: %s", resp.StatusCode, lastBody)
	}

	if lastErr != nil {
		return nil, lastStatus, fmt.Errorf("scrapingbee request failed after %d attempts: %w", maxRetries, lastErr)
	}
	return nil, lastStatus, fmt.Errorf("scrapingbee request failed after %d attempts: status=%d body=%s", maxRetries, lastStatus, lastBody)
}

func (p *scrapingbeeProvider) extractCitations(r scrapingbeeGPTResponse) []string {
	seen := make(map[string]struct{})
	var out []string

	add := func(u string) {
		u = strings.TrimSpace(u)
		if u == "" || !strings.HasPrefix(u, "http") {
			return
		}
		if _, ok := seen[u]; ok {
			return
		}
		seen[u] = struct{}{}
		out = append(out, u)
	}

	// 1) Primary: parse markdown links in results_markdown.
	for _, u := range extractURLsFromMarkdown(r.ResultsMarkdown) {
		add(u)
	}

	// 2) Fallback: if add_html=true, try to extract citation pill hrefs.
	if len(out) == 0 && r.FullHTML != "" {
		for _, u := range extractCitationPillURLsFromHTML(r.FullHTML) {
			add(u)
		}
	}

	return out
}

func (p *scrapingbeeProvider) mapLocationToCountryCode(location *workflowModels.Location) string {
	if location == nil {
		return ""
	}
	cc := strings.TrimSpace(location.Country)
	if cc == "" {
		return ""
	}
	// ScrapingBee docs list codes in lowercase; accept uppercase inputs and normalize.
	return strings.ToLower(cc)
}

var markdownLinkURLRe = regexp.MustCompile(`\]\((https?://[^)]+)\)`)
var bareURLRe = regexp.MustCompile(`https?://[^\s)<>\"]+`)

func extractURLsFromMarkdown(md string) []string {
	md = strings.TrimSpace(md)
	if md == "" {
		return nil
	}

	seen := make(map[string]struct{})
	var out []string

	for _, m := range markdownLinkURLRe.FindAllStringSubmatch(md, -1) {
		if len(m) < 2 {
			continue
		}
		u := m[1]
		if _, ok := seen[u]; ok {
			continue
		}
		seen[u] = struct{}{}
		out = append(out, u)
	}

	// Capture any bare URLs too (sometimes citations show as plain URLs).
	for _, u := range bareURLRe.FindAllString(md, -1) {
		if _, ok := seen[u]; ok {
			continue
		}
		seen[u] = struct{}{}
		out = append(out, u)
	}

	return out
}

var citationPillHrefRe = regexp.MustCompile(`data-testid="webpage-citation-pill"[^>]*href="([^"]+)"`)

func extractCitationPillURLsFromHTML(html string) []string {
	if html == "" {
		return nil
	}
	seen := make(map[string]struct{})
	var out []string
	for _, m := range citationPillHrefRe.FindAllStringSubmatch(html, -1) {
		if len(m) < 2 {
			continue
		}
		u := m[1]
		if _, ok := seen[u]; ok {
			continue
		}
		seen[u] = struct{}{}
		out = append(out, u)
	}
	return out
}

