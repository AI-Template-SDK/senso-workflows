// workflows/scrape_processor.go
package workflows

import (
	"context"
	"fmt"
	"encoding/json"

	"github.com/AI-Template-SDK/senso-workflows/services"
	"github.com/inngest/inngestgo"
	"github.com/inngest/inngestgo/step"
)

type ScrapeRequestEvent struct {
	URL   string `json:"url"`
	OrgID string `json:"org_id"`
}

type ScrapeProcessor struct {
	firecrawlService services.FirecrawlService
	// We will need a service to create content in the database later
	// contentCreatorService services.ContentCreationService 
	client           inngestgo.Client
}

func NewScrapeProcessor(firecrawlService services.FirecrawlService) *ScrapeProcessor {
	return &ScrapeProcessor{
		firecrawlService: firecrawlService,
	}
}

func (p *ScrapeProcessor) SetClient(client inngestgo.Client) {
	p.client = client
}

func (p *ScrapeProcessor) ScrapeURLWorkflow() inngestgo.ServableFunction {
	fn, _ := inngestgo.CreateFunction(
		p.client,
		inngestgo.FunctionOpts{ID: "scrape-single-url"},
		inngestgo.EventTrigger("website/scrape.requested", nil),
		func(ctx context.Context, input inngestgo.Input[ScrapeRequestEvent]) (any, error) {
			urlToScrape := input.Event.Data.URL
			fmt.Printf("[ScrapeURLWorkflow] Starting scrape for URL: %s\n", urlToScrape)

			// Step 1: Call Firecrawl to get the markdown
			// The result of this step is a map[string]interface{}
			scrapeResultMap, err := step.Run(ctx, "scrape-url", func(ctx context.Context) (interface{}, error) {
				return p.firecrawlService.ScrapeURL(ctx, urlToScrape)
			})
			if err != nil {
				return nil, fmt.Errorf("step 'scrape-url' failed: %w", err)
			}

			// --- START OF FIX ---
			// Convert the generic map into our specific struct
			var scrapeResult services.FirecrawlScrapeResult
			jsonBytes, err := json.Marshal(scrapeResultMap)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal scrape result map: %w", err)
			}
			if err := json.Unmarshal(jsonBytes, &scrapeResult); err != nil {
				return nil, fmt.Errorf("failed to unmarshal into FirecrawlScrapeResult: %w", err)
			}
			// --- END OF FIX ---

			// Now you can safely access the fields of your struct
			fmt.Printf("[ScrapeURLWorkflow] ✅ Scrape successful for %s. Markdown length: %d\n",
				scrapeResult.Data.SourceURL,
				len(scrapeResult.Data.Markdown))

			return map[string]interface{}{"status": "scrape successful"}, nil
		},
	)
	return fn
}