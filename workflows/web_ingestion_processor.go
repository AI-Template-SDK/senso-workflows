// workflows/web_ingestion_processor.go
package workflows

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/AI-Template-SDK/senso-workflows/services"
	"github.com/google/uuid"
	"github.com/inngest/inngestgo"
	"github.com/inngest/inngestgo/step"
	"github.com/qdrant/go-client/qdrant"
	"github.com/typesense/typesense-go/v2/typesense"
	"github.com/typesense/typesense-go/v2/typesense/api"
)

// WebScrapeRequestEvent is the trigger for the entire web ingestion pipeline.
type WebScrapeRequestEvent struct {
	URL   string `json:"url"`
	OrgID string `json:"org_id"`
}

type WebIngestionProcessor struct {
	firecrawlService services.FirecrawlService
	openAIService    services.OpenAIProvider
	qdrantClient     *qdrant.Client
	typesenseClient  *typesense.Client
	client           inngestgo.Client
}

func NewWebIngestionProcessor(
	firecrawlService services.FirecrawlService,
	openAIService services.OpenAIProvider,
	qdrantClient *qdrant.Client,
	typesenseClient *typesense.Client,
) *WebIngestionProcessor {
	return &WebIngestionProcessor{
		firecrawlService: firecrawlService,
		openAIService:    openAIService,
		qdrantClient:     qdrantClient,
		typesenseClient:  typesenseClient,
	}
}

func (p *WebIngestionProcessor) SetClient(client inngestgo.Client) {
	p.client = client
}

// IngestURLWorkflow handles the entire process from scraping to indexing in a single function.
func (p *WebIngestionProcessor) IngestURLWorkflow() inngestgo.ServableFunction {
	fn, _ := inngestgo.CreateFunction(
		p.client,
		inngestgo.FunctionOpts{ID: "ingest-scraped-url"},
		inngestgo.EventTrigger("website/scrape.requested", nil),
		func(ctx context.Context, input inngestgo.Input[WebScrapeRequestEvent]) (any, error) {
			urlToScrape := input.Event.Data.URL
			orgID := input.Event.Data.OrgID
			fmt.Printf("[IngestURLWorkflow] Starting full pipeline for URL: %s\n", urlToScrape)

			// Step 1: Scrape the URL to get markdown content.
			scrapeResultMap, err := step.Run(ctx, "scrape-url", func(ctx context.Context) (interface{}, error) {
				return p.firecrawlService.ScrapeURL(ctx, urlToScrape)
			})
			if err != nil {
				return nil, fmt.Errorf("step 'scrape-url' failed: %w", err)
			}

			// Convert the generic map result into our specific struct.
			var scrapeResult services.FirecrawlScrapeResult
			jsonBytes, err := json.Marshal(scrapeResultMap)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal scrape result map: %w", err)
			}
			if err := json.Unmarshal(jsonBytes, &scrapeResult); err != nil {
				return nil, fmt.Errorf("failed to unmarshal into FirecrawlScrapeResult: %w", err)
			}

			// Step 2: Chunk the markdown content.
			chunks, err := step.Run(ctx, "chunk-markdown", func(ctx context.Context) ([]string, error) {
				if scrapeResult.Data.Markdown == "" {
					return []string{}, nil // Return empty slice if no markdown
				}
				return chunkMarkdownByHeadings(scrapeResult.Data.Markdown), nil
			})
			if err != nil {
				return nil, fmt.Errorf("step 'chunk-markdown' failed: %w", err)
			}
			if len(chunks) == 0 {
				return map[string]interface{}{"status": "completed", "message": "No content to chunk."}, nil
			}

			// Step 3: Generate embeddings for each chunk.
			vectors, err := step.Run(ctx, "generate-embeddings", func(ctx context.Context) ([][]float32, error) {
				return p.openAIService.CreateEmbedding(ctx, chunks, "text-embedding-ada-002")
			})
			if err != nil {
				return nil, fmt.Errorf("step 'generate-embeddings' failed: %w", err)
			}

			// Step 4: Upsert to Qdrant.
			_, err = step.Run(ctx, "index-in-qdrant", func(ctx context.Context) (interface{}, error) {
				qdrantPoints := make([]*qdrant.PointStruct, len(chunks))
				for i, chunk := range chunks {
					payload := qdrant.NewValueMap(map[string]any{
						"text":       chunk,
						"source_url": scrapeResult.Data.SourceURL,
						"page_title": scrapeResult.Data.Title,
						"org_id":     orgID,
					})
					qdrantPoints[i] = &qdrant.PointStruct{
						Id:      qdrant.NewID(uuid.New().String()),
						Vectors: qdrant.NewVectors(vectors[i]...),
						Payload: payload,
					}
				}
				waitUpsert := true
				return p.qdrantClient.Upsert(ctx, &qdrant.UpsertPoints{
					CollectionName: "website_content",
					Points:         qdrantPoints,
					Wait:           &waitUpsert,
				})
			})
			if err != nil {
				return nil, fmt.Errorf("step 'index-in-qdrant' failed: %w", err)
			}

			// Step 5: Upsert to Typesense.
			_, err = step.Run(ctx, "index-in-typesense", func(ctx context.Context) (interface{}, error) {
				typesenseDocs := make([]interface{}, len(chunks))
				for i, chunk := range chunks {
					typesenseDocs[i] = map[string]interface{}{
						"id":              uuid.New().String(),
						"content":         chunk,
						"source_page_url": scrapeResult.Data.SourceURL,
						"page_title":      scrapeResult.Data.Title,
						"org_id":          orgID,
					}
				}
				action := "upsert"
				return p.typesenseClient.Collection("markdown_chunks").Documents().Import(ctx, typesenseDocs, &api.ImportDocumentsParams{Action: &action})
			})
			if err != nil {
				return nil, fmt.Errorf("step 'index-in-typesense' failed: %w", err)
			}

			fmt.Printf("[IngestURLWorkflow] ✅ COMPLETED: Full pipeline for URL %s\n", urlToScrape)
			return map[string]interface{}{"status": "success", "chunks_indexed": len(chunks)}, nil
		},
	)
	return fn
}

// chunkMarkdownByHeadings is a helper function to split markdown text.
func chunkMarkdownByHeadings(markdown string) []string {
	re := regexp.MustCompile(`(?m)^(#{1,3}\s.*)$`)
	indexes := re.FindAllStringIndex(markdown, -1)
	var chunks []string
	start := 0

	if len(indexes) > 0 && indexes[0][0] > 0 {
		firstChunk := strings.TrimSpace(markdown[0:indexes[0][0]])
		if firstChunk != "" {
			chunks = append(chunks, firstChunk)
		}
	} else if len(indexes) == 0 && strings.TrimSpace(markdown) != "" {
		chunks = append(chunks, strings.TrimSpace(markdown))
		return chunks
	}

	for i, index := range indexes {
		start = index[0]
		var end int
		if i < len(indexes)-1 {
			end = indexes[i+1][0]
		} else {
			end = len(markdown)
		}
		chunk := strings.TrimSpace(markdown[start:end])
		if chunk != "" {
			chunks = append(chunks, chunk)
		}
	}
	return chunks
}