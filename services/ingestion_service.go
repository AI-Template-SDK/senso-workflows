// services/ingestion_service.go
package services

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"

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
	qdrantClient    *qdrant.Client
	typesenseClient *typesense.Client
	openAIService   OpenAIProvider
	repos           *RepositoryManager
	cfg             *config.Config
}

// CHANGED: The constructor now accepts the RepositoryManager.
func NewIngestionService(
	qdrantClient *qdrant.Client,
	typesenseClient *typesense.Client,
	openAIService OpenAIProvider,
	repos *RepositoryManager,
	cfg *config.Config,
) IngestionService {
	return &ingestionService{
		qdrantClient:    qdrantClient,
		typesenseClient: typesenseClient,
		openAIService:   openAIService,
		repos:           repos,
		cfg:             cfg,
	}
}

// ChunkAndIndexWebContent is the main method that orchestrates the entire process.
func (s *ingestionService) ChunkAndIndexWebContent(ctx context.Context, contentID, versionID string) error {
	log.Printf("[IngestionService] Starting chunking and indexing for content ID: %s", contentID)

	// 1. Fetch content directly from the database
	log.Println("[IngestionService] Step 1: Fetching content directly from database...")
	contentUUID, err := uuid.Parse(contentID)
	if err != nil {
		return fmt.Errorf("invalid content ID format: %w", err)
	}

	// Use the repository to get the content data
	contentData, err := s.repos.ContentRepo.GetContentWithCurrentVersion(ctx, contentUUID)
	if err != nil {
		return fmt.Errorf("failed to get content from database: %w", err)
	}
	if contentData == nil {
		return fmt.Errorf("content not found in database for ID %s", contentID)
	}

	// This new logic handles both 'web' and 'raw' content types safely.
	var markdownContent string
	var sourceURL string

	if contentData.WebVersion != nil {
		markdownContent = contentData.WebVersion.Markdown
		sourceURL = contentData.WebVersion.SourceURL
	} else if contentData.RawVersion != nil {
		markdownContent = contentData.RawVersion.RawText
		sourceURL = "raw_text_input" // Provide a default source for raw text
	}

	if markdownContent == "" {
		return fmt.Errorf("no markdown content found for content ID %s", contentID)
	}
	// --- END OF FIX ---

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
		payload := qdrant.NewValueMap(map[string]any{
			"text":       chunk,
			"content_id": contentID,
			"version_id": versionID,
			"source_url": sourceURL,
			"page_title": contentData.CurrentVersion.Title,
		})

		qdrantPoints[i] = &qdrant.PointStruct{
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
			"source_page_url": sourceURL, // Use the safe sourceURL variable
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