// workflows/crawl_processor.go
package workflows

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/AI-Template-SDK/senso-workflows/services"
	"github.com/inngest/inngestgo"
	"github.com/inngest/inngestgo/step"
)

// This event will trigger the crawl workflow
type CrawlRequestEvent struct {
	URL   string `json:"url"`
	OrgID string `json:"org_id"`
}

type CrawlProcessor struct {
	firecrawlService services.FirecrawlService
	client           inngestgo.Client
}

func NewCrawlProcessor(firecrawlService services.FirecrawlService) *CrawlProcessor {
	return &CrawlProcessor{
		firecrawlService: firecrawlService,
	}
}

func (p *CrawlProcessor) SetClient(client inngestgo.Client) {
	p.client = client
}

func (p *CrawlProcessor) CrawlWebsiteWorkflow() inngestgo.ServableFunction {
	fn, _ := inngestgo.CreateFunction(
		p.client,
		inngestgo.FunctionOpts{
			ID:      "crawl-full-website",
			Name:    "Crawl Full Website and Process All Pages",
			Retries: inngestgo.IntPtr(2),
		},
		inngestgo.EventTrigger("website/crawl.requested", nil),
		func(ctx context.Context, input inngestgo.Input[CrawlRequestEvent]) (any, error) {
			urlToCrawl := input.Event.Data.URL
			orgID := input.Event.Data.OrgID
			fmt.Printf("[CrawlWebsiteWorkflow] Starting crawl for URL: %s\n", urlToCrawl)

			// Step 1: Start the asynchronous crawl job with Firecrawl.
			jobID, err := step.Run(ctx, "start-crawl-job", func(ctx context.Context) (interface{}, error) {
				return p.firecrawlService.StartCrawl(ctx, urlToCrawl)
			})
			if err != nil {
				return nil, fmt.Errorf("step 'start-crawl-job' failed: %w", err)
			}
			jobIDStr := jobID.(string)
			fmt.Printf("[CrawlWebsiteWorkflow] Crawl job started with ID: %s\n", jobIDStr)

			// Step 2: Poll for crawl completion.
			var finalCrawlData *services.FirecrawlCrawlStatus
			for i := 0; ; i++ {
				// Use the loop counter 'i' to create a stable ID for each iteration
				stepID := fmt.Sprintf("check-status-%s-%d", jobIDStr, i)
				statusResultMap, err := step.Run(ctx, stepID, func(ctx context.Context) (interface{}, error) {
					return p.firecrawlService.CheckCrawlStatus(ctx, jobIDStr)
				})
				if err != nil {
					return nil, fmt.Errorf("step '%s' failed: %w", stepID, err)
				}

				var statusResult services.FirecrawlCrawlStatus
				jsonBytes, _ := json.Marshal(statusResultMap)
				_ = json.Unmarshal(jsonBytes, &statusResult)

				if statusResult.Status == "completed" {
					fmt.Println("[CrawlWebsiteWorkflow] Crawl completed!")
					finalCrawlData = &statusResult
					break
				}

				fmt.Printf("[CrawlWebsiteWorkflow] Crawl in progress (%d/%d pages). Waiting 1 minute...\n", statusResult.Completed, statusResult.Total)
				
				// Use the loop counter for a stable sleep ID as well
				// Use the loop counter for a stable sleep ID.
				sleepID := fmt.Sprintf("wait-after-check-%d", i)

				step.Sleep(ctx, sleepID, 1*time.Minute)
			}

			// Step 3: Send an event with PRE-SCRAPED content for each page.
			_, err = step.Run(ctx, "send-content-found-events", func(ctx context.Context) (interface{}, error) {
				if finalCrawlData == nil || len(finalCrawlData.Data) == 0 {
					return "Crawl finished with no pages found.", nil
				}

				fmt.Printf("[CrawlWebsiteWorkflow] Found %d pages. Sending events to trigger ingestion.\n", len(finalCrawlData.Data))
				var events []inngestgo.Event
				for _, page := range finalCrawlData.Data {
					// Use the corrected struct paths and send the new event
					if page.Metadata.SourceURL == "" {
						fmt.Printf("[CrawlWebsiteWorkflow] Skipping page with empty sourceURL.\n")
						continue
					}
					events = append(events, inngestgo.Event{
						Name: "website/content.found", // ✅ New, descriptive event name
						Data: map[string]any{
							"org_id":   orgID,
							"url":      page.Metadata.SourceURL,
							"markdown": page.Markdown,         // ✅ Pass the markdown
							"title":    page.Metadata.Title,   // ✅ Pass the title
						},
					})
				}

				if len(events) == 0 {
					return "Crawl completed, but no valid pages with URLs were found to process.", nil
				}

				// Convert to []any for the v0.12.0 SendMany method
				eventsToSend := make([]any, len(events))
				for i, e := range events {
					eventsToSend[i] = e
				}

				// Use the correct SendMany method from the client.
				return p.client.SendMany(ctx, eventsToSend)
			})
			if err != nil {
				return nil, fmt.Errorf("step 'send-content-found-events' failed: %w", err)
			}

			return map[string]interface{}{"status": "success", "pages_processed": len(finalCrawlData.Data)}, nil
		},
	)
	return fn
}