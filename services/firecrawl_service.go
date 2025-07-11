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

// FirecrawlService defines the interface for interacting with the Firecrawl API.
type FirecrawlService interface {
	ScrapeURL(ctx context.Context, urlToScrape string) (*FirecrawlScrapeResult, error)
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