// services/firecrawl_service.go
package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/AI-Template-SDK/senso-workflows/internal/config"
)

// FirecrawlScrapeResult defines the structure of a successful scrape response
type FirecrawlScrapeResult struct {
	Success bool `json:"success"`
	Data    struct {
		Content   string `json:"content"` // This is the older field for markdown
		Markdown  string `json:"markdown"` // This is the newer field for markdown
		HTML      string `json:"html"`
		SourceURL string `json:"sourceURL"`
		Title     string `json:"title"`
	} `json:"data"`
}

type FirecrawlCrawlRequest struct {
	URL           string                 `json:"url"`
	CrawlOptions  *CrawlOptions          `json:"crawlerOptions,omitempty"`
	ScrapeOptions *ScrapeOptions         `json:"scrapeOptions,omitempty"`
}

type CrawlOptions struct {
	ExcludePaths []string `json:"excludePaths,omitempty"`
	IncludePaths []string `json:"includePaths,omitempty"`
	MaxDepth     int      `json:"maxDepth,omitempty"`
	Limit        int      `json:"limit,omitempty"`
}

type ScrapeOptions struct {
	Formats []string `json:"formats,omitempty"` // e.g., ["markdown"]
}

// FirecrawlCrawlResponse defines the success response from the /crawl endpoint
type FirecrawlCrawlResponse struct {
	Success bool   `json:"success"`
	JobID   string `json:"id"`
}

// FirecrawlCrawlStatus defines the success response from the /crawl/{id} status endpoint
type FirecrawlCrawlStatus struct {
	Status    string                   `json:"status"` // "scraping", "completed", or "failed"
	Total     int                      `json:"total"`
	Completed int                      `json:"completed"`
	Data      []FirecrawlScrapeResult `json:"data"`
}

// FirecrawlService defines the interface for interacting with the Firecrawl API.
type FirecrawlService interface {
	ScrapeURL(ctx context.Context, urlToScrape string) (*FirecrawlScrapeResult, error)
	StartCrawl(ctx context.Context, urlToCrawl string) (string, error)
	CheckCrawlStatus(ctx context.Context, jobID string) (*FirecrawlCrawlStatus, error)
}

type firecrawlService struct {
	client  *http.Client
	cfg     *config.Config
}

// NewFirecrawlService creates a new FirecrawlService instance.
func NewFirecrawlService(cfg *config.Config) FirecrawlService {
	return &firecrawlService{
		client:  &http.Client{},
		cfg:     cfg,
	}
}

// ScrapeURL calls the Firecrawl /scrape endpoint for a single URL.
func (s *firecrawlService) ScrapeURL(ctx context.Context, urlToScrape string) (*FirecrawlScrapeResult, error) {
	// Prepare the request body
	requestBody, err := json.Marshal(map[string]string{
		"url": urlToScrape,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal firecrawl request: %w", err)
	}

	// Create the HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", s.cfg.Firecrawl.BaseURL+"/scrape", bytes.NewBuffer(requestBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create firecrawl request: %w", err)
	}

	// Set required headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.cfg.Firecrawl.APIKey)

	// Execute the request
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("firecrawl request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("firecrawl returned non-200 status: %s", resp.Status)
	}

	// Decode the successful response
	var result FirecrawlScrapeResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode firecrawl response: %w", err)
	}

	// The API sometimes returns markdown in 'content', sometimes in 'markdown'.
    // This makes sure we always get it.
    if result.Data.Markdown == "" && result.Data.Content != "" {
        result.Data.Markdown = result.Data.Content
    }

	return &result, nil
}

// StartCrawl calls the Firecrawl /crawl endpoint to start an asynchronous job.
func (s *firecrawlService) StartCrawl(ctx context.Context, urlToCrawl string) (string, error) {
	// Prepare the request body
	requestBody, err := json.Marshal(FirecrawlCrawlRequest{
		URL: urlToCrawl,
		ScrapeOptions: &ScrapeOptions{
			Formats: []string{"markdown"},
		},
		CrawlOptions: &CrawlOptions{
			Limit:    500, // As per Tom's suggestion
			MaxDepth: 5,
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to marshal firecrawl crawl request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", s.cfg.Firecrawl.BaseURL+"/crawl", bytes.NewBuffer(requestBody))
	if err != nil {
		return "", fmt.Errorf("failed to create firecrawl crawl request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.cfg.Firecrawl.APIKey)

	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("firecrawl crawl request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("firecrawl crawl returned non-200 status: %s", resp.Status)
	}

	var result FirecrawlCrawlResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode firecrawl crawl response: %w", err)
	}

	return result.JobID, nil
}

// CheckCrawlStatus polls the Firecrawl /crawl/{id} endpoint for job status.
func (s *firecrawlService) CheckCrawlStatus(ctx context.Context, jobID string) (*FirecrawlCrawlStatus, error) {
	apiURL := fmt.Sprintf("%s/crawl/%s", s.cfg.Firecrawl.BaseURL, jobID)
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create firecrawl status request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+s.cfg.Firecrawl.APIKey)

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("firecrawl status request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("firecrawl status returned non-200 status: %s", resp.Status)
	}

	var result FirecrawlCrawlStatus
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode firecrawl status response: %w", err)
	}

	// Clean up the data field like we did for the scrape endpoint
	for i := range result.Data {
		if result.Data[i].Data.Markdown == "" && result.Data[i].Data.Content != "" {
			result.Data[i].Data.Markdown = result.Data[i].Data.Content
		}
	}

	return &result, nil
}