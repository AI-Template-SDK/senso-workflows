package services

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/AI-Template-SDK/senso-api/pkg/models"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

// DefaultQuestionRunCost is the standard hard-coded cost for a single successful question run
// This is a debit (a charge), so the value is negative.
const DefaultQuestionRunCost = -0.05

// UsageService defines the interface for tracking question run usage.
type UsageService interface {
	TrackBatchUsage(ctx context.Context, orgID uuid.UUID, batchID uuid.UUID) (int, error)
	TrackIndividualRuns(ctx context.Context, orgID uuid.UUID, runIDs []uuid.UUID) (int, error)
}

type usageService struct {
	repos *RepositoryManager
}

// NewUsageService creates a new UsageService instance.
func NewUsageService(repos *RepositoryManager) UsageService {
	return &usageService{
		repos: repos,
	}
}

// TrackBatchUsage iterates through all question runs in a batch and creates an idempotent credit_ledger entry for each.
// It skips runs that have already been charged.
// It assumes any run found in the DB is successful, as failed runs are not saved.
// It returns the number of new runs charged.
func (s *usageService) TrackBatchUsage(ctx context.Context, orgID uuid.UUID, batchID uuid.UUID) (int, error) {
	fmt.Printf("[TrackBatchUsage] Starting usage tracking for org %s, batch %s\n", orgID, batchID)

	// Get all question runs for the batch.
	runs, err := s.repos.QuestionRunRepo.GetByBatch(ctx, batchID)
	if err != nil {
		return 0, fmt.Errorf("failed to get question runs for batch %s: %w", batchID, err)
	}

	if len(runs) == 0 {
		fmt.Printf("[TrackBatchUsage] No question runs found for batch %s. Nothing to charge.\n", batchID)
		return 0, nil
	}

	// Begin a database transaction
	tx, err := s.repos.BeginTx(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback() // Rollback on error

	chargedCount, err := s.chargeRunsInTx(ctx, tx, orgID, runs)
	if err != nil {
		return 0, err // Error already formatted
	}

	// Commit the transaction
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit transaction: %w", err)
	}

	fmt.Printf("[TrackBatchUsage] Successfully charged %d new question runs for batch %s\n", chargedCount, batchID)
	return chargedCount, nil
}

// TrackIndividualRuns iterates through a specific list of question run IDs and creates an idempotent credit_ledger entry for each.
// This is used by the network processor which doesn't operate on a single batch.
// It returns the number of new runs charged.
func (s *usageService) TrackIndividualRuns(ctx context.Context, orgID uuid.UUID, runIDs []uuid.UUID) (int, error) {
	fmt.Printf("[TrackIndividualRuns] Starting usage tracking for org %s, %d individual runs\n", orgID, len(runIDs))

	if len(runIDs) == 0 {
		fmt.Printf("[TrackIndividualRuns] No run IDs provided. Nothing to charge.\n")
		return 0, nil
	}

	// Fetch the full QuestionRun models from the IDs
	runs, err := s.repos.QuestionRunRepo.GetByIDs(ctx, runIDs)
	if err != nil {
		return 0, fmt.Errorf("failed to get question run details: %w", err)
	}

	// Begin a database transaction
	tx, err := s.repos.BeginTx(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback() // Rollback on error

	chargedCount, err := s.chargeRunsInTx(ctx, tx, orgID, runs)
	if err != nil {
		return 0, err // Error already formatted
	}

	// Commit the transaction
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit transaction: %w", err)
	}

	fmt.Printf("[TrackIndividualRuns] Successfully charged %d new question runs for org %s\n", chargedCount, orgID)
	return chargedCount, nil
}

// chargeRunsInTx is an internal helper to process a list of runs within a transaction.
func (s *usageService) chargeRunsInTx(ctx context.Context, tx *sqlx.Tx, orgID uuid.UUID, runs []*models.QuestionRun) (int, error) {
	chargedCount := 0
	for _, run := range runs {
		// Idempotency check: Has this run already been charged?
		sourceIDStr := run.QuestionRunID.String()
		existing, err := s.repos.CreditLedgerRepo.GetBySourceIDAndTypeInTx(ctx, tx, sourceIDStr, "question_run")
		if err != nil && err != sql.ErrNoRows {
			return 0, fmt.Errorf("failed to check for existing ledger entry for run %s: %w", run.QuestionRunID, err)
		}

		if existing != nil {
			// This run has already been charged, skip it.
			continue
		}

		// Create metadata for the ledger entry
		metadata := map[string]string{
			"question_run_id": run.QuestionRunID.String(),
			"org_id":          orgID.String(),
		}
		if run.BatchID != nil {
			metadata["batch_id"] = run.BatchID.String()
		}
		metadataJSON, _ := json.Marshal(metadata)

		// Create the new ledger entry
		entry := &models.CreditLedger{
			EntryID:    uuid.New(),
			OrgID:      &orgID,
			Amount:     DefaultQuestionRunCost, // This is the charge (debit)
			SourceType: "question_run",         // As per migration 000027
			SourceID:   &sourceIDStr,
			Metadata:   metadataJSON,
			CreatedAt:  time.Now(),
		}

		// Create the entry within the transaction
		if err := s.repos.CreditLedgerRepo.CreateInTx(ctx, tx, entry); err != nil {
			return 0, fmt.Errorf("failed to create ledger entry for run %s: %w", run.QuestionRunID, err)
		}
		chargedCount++
	}
	return chargedCount, nil
}
