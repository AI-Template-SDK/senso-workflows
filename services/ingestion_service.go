// services/ingestion_service.go
package services

import (
	"context"
	"fmt"
	"net/http"

	"github.com/AI-Template-SDK/senso-workflows/internal/config"
	"github.com/qdrant/go-client/qdrant"
	"github.com/typesense/typesense-go/v2/typesense"
)

// IngestionService defines the interface for our new service.
type IngestionService interface {
	ChunkAndIndexWebContent(ctx context.Context, contentID string) error
}

type ingestionService struct {
	qdrantClient    *qdrant.Client
	typesenseClient *typesense.Client
	openAIService   OpenAIProvider // Using the existing interface
	apiClient       *http.Client   // A client to call back to the senso-api
	cfg             *config.Config
}

// NewIngestionService creates a new IngestionService instance.
func NewIngestionService(
	qdrantClient *qdrant.Client,
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

// ChunkAndIndexWebContent will be the main method that orchestrates the entire process.
func (s *ingestionService) ChunkAndIndexWebContent(ctx context.Context, contentID string) error {
	fmt.Printf("[IngestionService] Starting chunking and indexing for content ID: %s\n", contentID)

	// 1. Fetch content from senso-api using its API client.
	//    (You will need to implement the logic to make an authenticated API call)
	//    markdownContent, err := s.fetchContentFromAPI(ctx, contentID)

	// 2. Chunk the markdown content based on your strategy.
	//    chunks := s.chunkContent(markdownContent)

	// 3. Generate embeddings for each chunk using an AI provider.
	//    vectors, err := s.openAIService.CreateEmbedding(ctx, chunks)

	// 4. Upsert chunks and vectors into Qdrant.
	//    err = s.upsertToQdrant(ctx, chunks, vectors)

	// 5. Upsert chunk documents into Typesense.
	//    err = s.upsertToTypesense(ctx, chunks)

	fmt.Printf("[IngestionService] ✅ Finished processing content ID: %s\n", contentID)
	return nil
}