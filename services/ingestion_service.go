package services

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"

	"github.com/AI-Template-SDK/senso-api/pkg/models"
	"github.com/AI-Template-SDK/senso-workflows/internal/config"
	"github.com/google/uuid"
	"github.com/qdrant/go-client/qdrant"
	"github.com/typesense/typesense-go/v2/typesense"
	"github.com/typesense/typesense-go/v2/typesense/api"
)

type IngestionService interface {
	ChunkAndIndexWebContent(ctx context.Context, contentID, versionID string) error
}

type ingestionService struct {
	qdrantClient    *qdrant.Client // Changed to the high-level client
	typesenseClient *typesense.Client
	openAIService   OpenAIProvider
	apiClient       *http.Client
	cfg             *config.Config
}

func NewIngestionService(
	qdrantClient *qdrant.Client, // Changed to the high-level client
	typesenseClient *typesense.Client,
	openAIService OpenAIProvider,
	cfg *config.Config,
) IngestionService {
	return &ingestionService{
		qdrantClient:    qdrantClient,
		typesenseClient: typesenseClient,
		openAIService:   openAIService,
		apiClient:       &http.Client{},
		cfg:             cfg,
	}
}

// ChunkAndIndexWebContent is the main method that orchestrates the entire process.
func (s *ingestionService) ChunkAndIndexWebContent(ctx context.Context, contentID, versionID string) error {
	log.Printf("[IngestionService] Starting chunking and indexing for content ID: %s", contentID)

	// 1. Fetch content from senso-api
	log.Println("[IngestionService] Step 1: Fetching content from senso-api...")
	apiURL := fmt.Sprintf("%s/api/v1/content/%s", s.cfg.SensoAPI.BaseURL, contentID)
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request to senso-api: %w", err)
	}
	req.Header.Set("X-API-Key", s.cfg.SensoAPI.APIKey)

	resp, err := s.apiClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to call senso-api: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("senso-api returned non-200 status: %s - %s", resp.Status, string(body))
	}

	var contentData models.ContentWithCurrentVersion
	if err := json.NewDecoder(resp.Body).Decode(&contentData); err != nil {
		return fmt.Errorf("failed to decode response from senso-api: %w", err)
	}

	if contentData.WebVersion == nil || contentData.WebVersion.Markdown == "" {
		return fmt.Errorf("no web content or markdown found for content ID %s", contentID)
	}
	markdownContent := contentData.WebVersion.Markdown

	// 2. Chunk the markdown content
	log.Println("[IngestionService] Step 2: Chunking content...")
	chunks := s.chunkMarkdownByHeadings(markdownContent)
	if len(chunks) == 0 {
		log.Println("[IngestionService] No chunks were generated from the markdown content.")
		return nil
	}

	// 3. Generate embeddings for each chunk
	log.Printf("[IngestionService] Step 3: Generating embeddings for %d chunks...", len(chunks))
	vectors, err := s.openAIService.CreateEmbedding(ctx, chunks, "text-embedding-ada-002")
	if err != nil {
		return fmt.Errorf("failed to generate embeddings: %w", err)
	}

	// 4. Upsert chunks and vectors into Qdrant
	log.Printf("[IngestionService] Step 4: Indexing %d vectors to Qdrant...", len(vectors))
	qdrantPoints := make([]*qdrant.PointStruct, len(chunks))
	for i, chunk := range chunks {
		// Create the payload using a simple map and the NewValueMap helper
		payload := qdrant.NewValueMap(map[string]any{
			"text":       chunk,
			"content_id": contentID,
			"version_id": versionID,
			"source_url": contentData.WebVersion.SourceURL,
			"page_title": contentData.CurrentVersion.Title,
		})

		qdrantPoints[i] = &qdrant.PointStruct{
			// Use NewID for UUIDs and NewVectors with the '...' spread operator
			Id:      qdrant.NewID(uuid.New().String()),
			Vectors: qdrant.NewVectors(vectors[i]...),
			Payload: payload,
		}
	}

	waitUpsert := true
	_, err = s.qdrantClient.Upsert(ctx, &qdrant.UpsertPoints{
		CollectionName: "website_content",
		Points:         qdrantPoints,
		Wait:           &waitUpsert,
	})
	if err != nil {
		return fmt.Errorf("failed to upsert points to Qdrant: %w", err)
	}

	// 5. Upsert chunk documents into Typesense
	log.Printf("[IngestionService] Step 5: Indexing %d documents to Typesense...", len(chunks))
	typesenseDocs := make([]interface{}, len(chunks))
	for i, chunk := range chunks {
		typesenseDocs[i] = map[string]interface{}{
			"id":              uuid.New().String(),
			"content":         chunk,
			"source_page_url": contentData.WebVersion.SourceURL,
			"page_title":      contentData.CurrentVersion.Title,
			"created_at":      contentData.Content.CreatedAt.Unix(),
		}
	}

	action := "upsert"
	_, err = s.typesenseClient.Collection("markdown_chunks").Documents().Import(ctx, typesenseDocs, &api.ImportDocumentsParams{Action: &action})
	if err != nil {
		return fmt.Errorf("failed to import documents to Typesense: %w", err)
	}

	log.Printf("[IngestionService] ✅ Finished processing content ID: %s", contentID)
	return nil
}

// chunkMarkdownByHeadings splits markdown text into chunks based on heading levels.
func (s *ingestionService) chunkMarkdownByHeadings(markdown string) []string {
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