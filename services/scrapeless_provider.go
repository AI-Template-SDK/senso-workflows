// services/scrapeless_provider.go
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

// Scrapeless docs (LLM Chat Scraper):
// - https://docs.scrapeless.com/en/llm-chat-scraper/quickstart/getting-started/
// - https://docs.scrapeless.com/en/llm-chat-scraper/quickstart/task-lifecycle/
// - https://docs.scrapeless.com/en/llm-chat-scraper/scrapers/chatgpt/
// - https://docs.scrapeless.com/en/llm-chat-scraper/help/supported-countries/
//
// This provider uses:
// - POST https://api.scrapeless.com/api/v2/scraper/request
// - GET  https://api.scrapeless.com/api/v2/scraper/result/{task_id}

type scrapelessProvider struct {
	apiKey      string
	baseURL     string
	costService CostService
	httpClient  *http.Client
}

func NewScrapelessProvider(cfg *config.Config, model string, costService CostService) AIProvider {
	fmt.Printf("[NewScrapelessProvider] Creating Scrapeless provider\n")
	fmt.Printf("[NewScrapelessProvider]   - API Key: %s\n", maskAPIKey(cfg.ScrapelessAPIKey))
	fmt.Printf("[NewScrapelessProvider]   - Endpoint: https://api.scrapeless.com/api/v2/scraper/request\n")

	if strings.TrimSpace(cfg.ScrapelessAPIKey) == "" {
		fmt.Printf("[NewScrapelessProvider] ⚠️ WARNING: SCRAPELESS_API_KEY is empty!\n")
	}

	return &scrapelessProvider{
		apiKey:      strings.TrimSpace(cfg.ScrapelessAPIKey),
		baseURL:     "https://api.scrapeless.com/api/v2/scraper",
		costService: costService,
		httpClient: &http.Client{
			Timeout: 3 * time.Minute,
		},
	}
}

func (p *scrapelessProvider) GetProviderName() string { return "scrapeless" }

type scrapelessWebhook struct {
	URL string `json:"url"`
}

type scrapelessCreateRequest struct {
	Actor   string           `json:"actor"`
	Input   scrapelessInput  `json:"input"`
	Webhook *scrapelessWebhook `json:"webhook,omitempty"`
}

type scrapelessInput struct {
	Prompt    string `json:"prompt"`
	Country   string `json:"country"`
	WebSearch bool   `json:"web_search,omitempty"`
}

// scrapelessTaskEnvelope matches the Task Lifecycle "Result Data Structure".
type scrapelessTaskEnvelope struct {
	TaskID     string          `json:"task_id"`
	Status     string          `json:"status"`  // pending, running, success, failed
	Message    string          `json:"message"` // present on failures
	Input      json.RawMessage `json:"input,omitempty"`
	TaskResult json.RawMessage `json:"task_result,omitempty"`
}

// Actor payload for scraper.chatgpt (ChatGPT Scraper docs).
type scrapelessChatGPTResult struct {
	Prompt            string `json:"prompt"`
	ResultText        string `json:"result_text"` // markdown response
	Model             string `json:"model"`
	WebSearch         bool   `json:"web_search"`
	Links             []string `json:"links"`

	SearchResult []struct {
		Attribution string `json:"attribution"`
		Snippet     string `json:"snippet"`
		Title       string `json:"title"`
		URL         string `json:"url"`
	} `json:"search_result"`

	ContentReferences []struct {
		Attribution string `json:"attribution"`
		Title       string `json:"title"`
		URL         string `json:"url"`
	} `json:"content_references"`
}

func (p *scrapelessProvider) RunQuestion(ctx context.Context, query string, websearch bool, location *workflowModels.Location) (*AIResponse, error) {
	fmt.Printf("[ScrapelessProvider] 🚀 Making Scrapeless ChatGPT call for query: %s\n", query)
	fmt.Printf("[ScrapelessProvider]   - Web search: %t\n", websearch)

	if strings.TrimSpace(p.apiKey) == "" {
		return nil, fmt.Errorf("scrapeless API key is not configured (SCRAPELESS_API_KEY)")
	}

	country := p.mapLocationToCountry(location)
	fmt.Printf("[ScrapelessProvider]   - country: %s\n", country)

	taskID, err := p.createTask(ctx, query, websearch, country)
	if err != nil {
		return nil, fmt.Errorf("failed to create scrapeless task: %w", err)
	}

	fmt.Printf("[ScrapelessProvider] 📋 Task created: %s\n", taskID)

	result, err := p.pollResult(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("failed to poll scrapeless result: %w", err)
	}

	responseText := strings.TrimSpace(result.ResultText)
	shouldProcessEvaluation := responseText != ""
	if !shouldProcessEvaluation {
		fmt.Printf("[ScrapelessProvider] ⚠️ Scrapeless returned empty result_text\n")
		responseText = "Question run failed for this model and location"
	}

	citations := p.extractCitations(result)
	if !shouldProcessEvaluation {
		citations = []string{}
	}

	fmt.Printf("[ScrapelessProvider] ✅ Scrapeless call completed\n")
	fmt.Printf("[ScrapelessProvider]   - Model: %s\n", result.Model)
	fmt.Printf("[ScrapelessProvider]   - Response length: %d characters\n", len(responseText))
	fmt.Printf("[ScrapelessProvider]   - Citations: %d\n", len(citations))

	// Scrapeless billing is plan-specific; we do not guess cost here.
	return &AIResponse{
		Response:                responseText,
		InputTokens:             0,
		OutputTokens:            0,
		Cost:                    0,
		Citations:               citations,
		ShouldProcessEvaluation: shouldProcessEvaluation,
	}, nil
}

func (p *scrapelessProvider) RunQuestionWebSearch(ctx context.Context, query string) (*AIResponse, error) {
	defaultLocation := &workflowModels.Location{Country: "US"}
	return p.RunQuestion(ctx, query, true, defaultLocation)
}

func (p *scrapelessProvider) SupportsBatching() bool { return false }

func (p *scrapelessProvider) GetMaxBatchSize() int { return 1 }

func (p *scrapelessProvider) RunQuestionBatch(ctx context.Context, queries []string, websearch bool, location *workflowModels.Location) ([]*AIResponse, error) {
	fmt.Printf("[ScrapelessProvider] 🔄 Processing %d questions sequentially (no batching support)\n", len(queries))
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

func (p *scrapelessProvider) createTask(ctx context.Context, prompt string, websearch bool, country string) (string, error) {
	reqBody := scrapelessCreateRequest{
		Actor: "scraper.chatgpt",
		Input: scrapelessInput{
			Prompt:    prompt,
			Country:   country,
			WebSearch: websearch,
		},
	}

	b, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	url := p.baseURL + "/request"
	bodyBytes, status, err := p.doJSONWithRetries(ctx, "POST", url, b)
	if err != nil {
		return "", err
	}

	var env scrapelessTaskEnvelope
	if err := json.Unmarshal(bodyBytes, &env); err != nil {
		return "", fmt.Errorf("failed to decode create response (http=%d): %w", status, err)
	}
	if strings.TrimSpace(env.TaskID) == "" {
		return "", fmt.Errorf("scrapeless create response missing task_id (http=%d)", status)
	}

	// HTTP 201 is "Task created successfully" per Task Lifecycle docs.
	if status != http.StatusCreated && status != http.StatusOK && status != http.StatusAccepted {
		fmt.Printf("[ScrapelessProvider] ⚠️ Unexpected create status %d (expected 201/200/202)\n", status)
	}
	return env.TaskID, nil
}

func (p *scrapelessProvider) pollResult(ctx context.Context, taskID string) (*scrapelessChatGPTResult, error) {
	// Results are retained briefly (docs: "retained for 5 minutes after completion").
	// We'll poll with a bounded timeout.
	deadline := time.Now().Add(2 * time.Minute)
	sleep := 2 * time.Second
	pollCount := 0

	url := p.baseURL + "/result/" + taskID
	for {
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timeout waiting for scrapeless task %s", taskID)
		}
		pollCount++

		bodyBytes, status, err := p.doJSONWithRetries(ctx, "GET", url, nil)
		if err != nil {
			fmt.Printf("[ScrapelessProvider] ⚠️ Poll failed (poll #%d): %v\n", pollCount, err)
			time.Sleep(sleep)
			sleep = minDuration(sleep*2, 10*time.Second)
			continue
		}

		// Per Task Lifecycle docs, API may return 202 while still running.
		if status == http.StatusAccepted {
			fmt.Printf("[ScrapelessProvider] ⏳ Task still running (http=202, poll #%d)\n", pollCount)
			time.Sleep(sleep)
			sleep = minDuration(sleep*2, 10*time.Second)
			continue
		}

		if status == http.StatusGone {
			return nil, fmt.Errorf("task result expired for task %s (http=410)", taskID)
		}

		if status != http.StatusOK && status != http.StatusCreated {
			preview := string(bodyBytes[:min(2000, len(bodyBytes))])
			return nil, fmt.Errorf("unexpected status %d polling result: %s", status, preview)
		}

		var env scrapelessTaskEnvelope
		if err := json.Unmarshal(bodyBytes, &env); err != nil {
			return nil, fmt.Errorf("failed to decode result envelope (http=%d): %w", status, err)
		}

		switch strings.ToLower(strings.TrimSpace(env.Status)) {
		case "pending", "running", "":
			fmt.Printf("[ScrapelessProvider] ⏳ Task status=%q (poll #%d)\n", env.Status, pollCount)
			time.Sleep(sleep)
			sleep = minDuration(sleep*2, 10*time.Second)
			continue
		case "failed":
			if env.Message == "" {
				env.Message = "unknown error"
			}
			return nil, fmt.Errorf("task failed: %s", env.Message)
		case "success":
			if len(env.TaskResult) == 0 {
				return nil, fmt.Errorf("task success but task_result is empty")
			}
			var r scrapelessChatGPTResult
			if err := json.Unmarshal(env.TaskResult, &r); err != nil {
				return nil, fmt.Errorf("failed to decode task_result: %w", err)
			}
			return &r, nil
		default:
			fmt.Printf("[ScrapelessProvider] ⚠️ Unknown task status=%q (poll #%d), retrying\n", env.Status, pollCount)
			time.Sleep(sleep)
			sleep = minDuration(sleep*2, 10*time.Second)
			continue
		}
	}
}

func (p *scrapelessProvider) doJSONWithRetries(ctx context.Context, method, url string, body []byte) ([]byte, int, error) {
	const maxRetries = 5
	var lastStatus int
	var lastBody string
	var lastErr error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		var rdr io.Reader
		if body != nil {
			rdr = bytes.NewReader(body)
		}

		req, err := http.NewRequestWithContext(ctx, method, url, rdr)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-api-token", p.apiKey)

		resp, err := p.httpClient.Do(req)
		if err != nil {
			lastErr = err
			fmt.Printf("[ScrapelessProvider] ⚠️ Request failed (attempt %d/%d): %v\n", attempt, maxRetries, err)
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

		// Accept 2xx and 202 (still running) for poller to handle.
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return bodyBytes, resp.StatusCode, nil
		}

		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			if attempt < maxRetries {
				fmt.Printf("[ScrapelessProvider] ⚠️ Non-2xx status %d (attempt %d/%d). Body preview: %s\n",
					resp.StatusCode, attempt, maxRetries, lastBody)
				time.Sleep(time.Duration(attempt) * 2 * time.Second)
				continue
			}
		}

		return nil, resp.StatusCode, fmt.Errorf("scrapeless API returned status %d: %s", resp.StatusCode, lastBody)
	}

	if lastErr != nil {
		return nil, lastStatus, fmt.Errorf("scrapeless request failed after %d attempts: %w", maxRetries, lastErr)
	}
	return nil, lastStatus, fmt.Errorf("scrapeless request failed after %d attempts: status=%d body=%s", maxRetries, lastStatus, lastBody)
}

func (p *scrapelessProvider) extractCitations(r *scrapelessChatGPTResult) []string {
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

	for _, c := range r.ContentReferences {
		add(c.URL)
	}
	for _, s := range r.SearchResult {
		add(s.URL)
	}
	for _, u := range r.Links {
		add(u)
	}

	return out
}

func (p *scrapelessProvider) mapLocationToCountry(location *workflowModels.Location) string {
	// Scrapeless expects an ISO country code (docs: Supported Countries page).
	if location == nil {
		return "US"
	}
	c := strings.TrimSpace(location.Country)
	if c == "" {
		return "US"
	}
	return strings.ToUpper(c)
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

