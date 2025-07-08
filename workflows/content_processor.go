// workflows/content_processor.go
package workflows

import (
	"context"
	"fmt"
	
	"github.com/AI-Template-SDK/senso-workflows/services"
	"github.com/inngest/inngestgo"
	"github.com/inngest/inngestgo/step"
)

// ContentWebCreatedEvent defines the data we expect from the API event.
type ContentWebCreatedEvent struct {
	ContentID string `json:"content_id"`
	VersionID string `json:"version_id"`
}

type ContentProcessor struct {
	ingestionService services.IngestionService
	client           inngestgo.Client
}

func NewContentProcessor(ingestionService services.IngestionService) *ContentProcessor {
	return &ContentProcessor{
		ingestionService: ingestionService,
	}
}

func (p *ContentProcessor) SetClient(client inngestgo.Client) {
	p.client = client
}

func (p *ContentProcessor) ProcessWebsiteContent() inngestgo.ServableFunction {
	fn, _ := inngestgo.CreateFunction(
		p.client,
		inngestgo.FunctionOpts{
			ID:      "process-web-content",
			Name:    "Process and Index Scraped Website Content",
			Retries: inngestgo.IntPtr(3),
		},
		// This function triggers whenever senso-api sends this event.
		inngestgo.EventTrigger("api/content.web.created", nil),
		func(ctx context.Context, input inngestgo.Input[ContentWebCreatedEvent]) (any, error) {
			contentID := input.Event.Data.ContentID
			fmt.Printf("[ProcessWebsiteContent] Starting ingestion pipeline for content: %s\n", contentID)
			
			// Use a single step to call your service. Inngest handles the retries.
			output, err := step.Run(ctx, "chunk-and-index-content", func(ctx context.Context) (interface{}, error) {
				err := p.ingestionService.ChunkAndIndexWebContent(ctx, contentID)
				if err != nil {
					return nil, err
				}
				return map[string]string{"status": "success"}, nil
			})
			if err != nil {
				return nil, fmt.Errorf("step 'chunk-and-index-content' failed: %w", err)
			}

			fmt.Printf("[ProcessWebsiteContent] ✅ COMPLETED: Ingestion pipeline for content %s\n", contentID)
			return output, nil
		},
	)
	return fn
}