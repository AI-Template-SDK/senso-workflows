// services/webscraping_tracking_service.go
package services

import (
	"context"
	"fmt"
	"time"

	"github.com/AI-Template-SDK/senso-api/pkg/models"
	"github.com/AI-Template-SDK/senso-api/pkg/repositories/interfaces"
	"github.com/google/uuid"
)

// WebscrapingTrackingService handles tracking of web scraping and crawling operations
type WebscrapingTrackingService interface {
	RecordRun(ctx context.Context, orgID uuid.UUID, sourceURL, pageTitle, operationType, eventName string, chunksProcessed int, inputTokens *int, totalCost *float64, modelUsed *string, status string, errorMessage *string) error
	GetStatsByOrgID(ctx context.Context, orgID uuid.UUID) (*interfaces.WebscrapingRunStats, error)
	GetRunsByOrgID(ctx context.Context, orgID uuid.UUID, limit, offset int) ([]*models.WebscrapingRun, error)
}

type webscrapingTrackingService struct {
	webscrapingRunRepo interfaces.WebscrapingRunRepository
}

// NewWebscrapingTrackingService creates a new WebscrapingTrackingService
func NewWebscrapingTrackingService(webscrapingRunRepo interfaces.WebscrapingRunRepository) WebscrapingTrackingService {
	return &webscrapingTrackingService{
		webscrapingRunRepo: webscrapingRunRepo,
	}
}

// RecordRun records a webscraping operation in the database
func (s *webscrapingTrackingService) RecordRun(ctx context.Context, orgID uuid.UUID, sourceURL, pageTitle, operationType, eventName string, chunksProcessed int, inputTokens *int, totalCost *float64, modelUsed *string, status string, errorMessage *string) error {
	run := &models.WebscrapingRun{
		WebscrapingRunID: uuid.New(),
		OrgID:            orgID,
		SourceURL:        sourceURL,
		PageTitle:        &pageTitle,
		OperationType:    operationType,
		EventName:        eventName,
		ChunksProcessed:  chunksProcessed,
		InputTokens:      inputTokens,
		TotalCost:        totalCost,
		ModelUsed:        modelUsed,
		Status:           status,
		ErrorMessage:     errorMessage,
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}

	if err := s.webscrapingRunRepo.Create(ctx, run); err != nil {
		return fmt.Errorf("failed to record webscraping run: %w", err)
	}

	fmt.Printf("[WebscrapingTrackingService] Recorded %s operation for URL %s: %d chunks, %d input tokens, cost $%.6f\n",
		operationType, sourceURL, chunksProcessed, inputTokens, totalCost)

	return nil
}

// GetStatsByOrgID retrieves aggregated statistics for an organization
func (s *webscrapingTrackingService) GetStatsByOrgID(ctx context.Context, orgID uuid.UUID) (*interfaces.WebscrapingRunStats, error) {
	stats, err := s.webscrapingRunRepo.GetStatsByOrgID(ctx, orgID)
	if err != nil {
		return nil, fmt.Errorf("failed to get webscraping stats: %w", err)
	}
	return stats, nil
}

// GetRunsByOrgID retrieves webscraping runs for an organization with pagination
func (s *webscrapingTrackingService) GetRunsByOrgID(ctx context.Context, orgID uuid.UUID, limit, offset int) ([]*models.WebscrapingRun, error) {
	runs, err := s.webscrapingRunRepo.GetByOrgID(ctx, orgID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to get webscraping runs: %w", err)
	}
	return runs, nil
}
