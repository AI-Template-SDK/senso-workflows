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
			Retries: inngestgo.IntPtr(2), // Crawls can be long, fewer retries
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
			for {
				// This unique step ID ensures Inngest retries from the last failed status check
				stepID := fmt.Sprintf("check-status-%s-%d", jobIDStr, time.Now().Unix())
				statusResultMap, err := step.Run(ctx, stepID, func(ctx context.Context) (interface{}, error) {
					return p.firecrawlService.CheckCrawlStatus(ctx, jobIDStr)
				})
				if err != nil {
					// We'll let Inngest retry this step after a delay
					return nil, fmt.Errorf("step '%s' failed: %w", stepID, err)
				}
				
				// Convert the generic map to our specific struct
				var statusResult services.FirecrawlCrawlStatus
				jsonBytes, _ := json.Marshal(statusResultMap)
				_ = json.Unmarshal(jsonBytes, &statusResult)

				if statusResult.Status == "completed" {
					fmt.Println("[CrawlWebsiteWorkflow] Crawl completed!")
					finalCrawlData = &statusResult
					break // Exit the loop
				}

				fmt.Printf("[CrawlWebsiteWorkflow] Crawl in progress (%d/%d pages). Waiting 1 minute...\n", statusResult.Completed, statusResult.Total)
				// Wait for 1 minute before checking the status again.
				if err := step.Sleep(ctx, 1*time.Minute); err != nil {
					return nil, err
				}
			}

			// Step 3: Send an event for EACH scraped page to be processed individually.
			_, err = step.Run(ctx, "send-page-processing-events", func(ctx context.Context) (interface{}, error) {
				if finalCrawlData == nil || len(finalCrawlData.Data) == 0 {
					return "Crawl finished with no pages found.", nil
				}

				fmt.Printf("[CrawlWebsiteWorkflow] Found %d pages. Sending events to trigger ingestion.\n", len(finalCrawlData.Data))
				var events []inngestgo.Event
				for _, page := range finalCrawlData.Data {
					// This event will trigger the 'ingest-scraped-url' workflow we already built
					events = append(events, inngestgo.Event{
						Name: "website/scrape.requested",
						Data: WebScrapeRequestEvent{
							OrgID: orgID,
							URL:   page.Data.SourceURL,
						},
					})
				}
				// Send all events in a single batch.
				return p.client.Send(ctx, events...)
			})
			if err != nil {
				return nil, fmt.Errorf("step 'send-page-processing-events' failed: %w", err)
			}

			return map[string]interface{}{"status": "success", "pages_found": len(finalCrawlData.Data)}, nil
		},
	)
	return fn
}