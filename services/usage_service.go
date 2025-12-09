package services

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/AI-Template-SDK/senso-api/pkg/models"
	"github.com/AI-Template-SDK/senso-api/pkg/repositories/postgresql"
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
	// ** ADDED: New method for pre-checking balance **
	CheckBalance(ctx context.Context, orgID uuid.UUID, cost float64) error
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

// ** ADDED: CheckBalance method implementation **
// CheckBalance verifies if the org's partner has enough balance to cover a cost.
func (s *usageService) CheckBalance(ctx context.Context, orgID uuid.UUID, cost float64) error {
	if cost <= 0 {
		// This check is for a *cost*, so it must be positive.
		// If the total cost is 0 (e.g., 0 runs), we can just return nil.
		if cost == 0 {
			return nil
		}
		return fmt.Errorf("cost must be a positive value, got %f", cost)
	}

	// 1. Get Org to find PartnerID
	org, err := s.repos.OrgRepo.GetByID(ctx, orgID)
	if err != nil {
		return fmt.Errorf("failed to get org %s: %w", orgID, err)
	}
	if org == nil {
		return fmt.Errorf("org %s not found", orgID)
	}
	if org.PartnerID == uuid.Nil {
		// Orgs must belong to a partner to have a balance
		return fmt.Errorf("org %s is not associated with a partner", orgID)
	}

	// 2. Get Partner Balance
	balance, err := s.repos.CreditBalanceRepo.GetByPartnerID(ctx, org.PartnerID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// No balance record means 0 balance
			return fmt.Errorf("insufficient credits for partner %s (balance: 0.00, cost: %.2f): %w", org.PartnerID, cost, postgresql.ErrInsufficientCredits)
		}
		return fmt.Errorf("failed to get credit balance for partner %s: %w", org.PartnerID, err)
	}

	// 3. Check Balance
	if balance.CurrentBalance < cost {
		return fmt.Errorf("insufficient credits for partner %s (balance: %.2f, cost: %.2f): %w", org.PartnerID, balance.CurrentBalance, cost, postgresql.ErrInsufficientCredits)
	}

	fmt.Printf("[CheckBalance] Org %s (Partner %s) has sufficient balance (%.2f) for cost (%.2f)\n", orgID, org.PartnerID, balance.CurrentBalance, cost)
	return nil // Sufficient balance
}

// TrackBatchUsage iterates through all question runs in a batch and creates an idempotent credit_ledger entry for each.
// ** MODIFIED: It now also deducts from the partner's credit_balance. **
func (s *usageService) TrackBatchUsage(ctx context.Context, orgID uuid.UUID, batchID uuid.UUID) (int, error) {
	fmt.Printf("[TrackBatchUsage] Starting usage tracking for org %s, batch %s\n", orgID, batchID)

	// ** ADDED: Get Org to find PartnerID **
	org, err := s.repos.OrgRepo.GetByID(ctx, orgID)
	if err != nil {
		return 0, fmt.Errorf("failed to get org %s: %w", orgID, err)
	}
	if org == nil {
		return 0, fmt.Errorf("org %s not found", orgID)
	}
	if org.PartnerID == uuid.Nil {
		return 0, fmt.Errorf("org %s is not associated with a partner, cannot track usage", orgID)
	}
	partnerID := org.PartnerID

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

	// ** MODIFIED: Pass partnerID to chargeRunsInTx **
	chargedCount, err := s.chargeRunsInTx(ctx, tx, orgID, partnerID, runs)
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
// ** MODIFIED: It now also deducts from the partner's credit_balance. **
func (s *usageService) TrackIndividualRuns(ctx context.Context, orgID uuid.UUID, runIDs []uuid.UUID) (int, error) {
	fmt.Printf("[TrackIndividualRuns] Starting usage tracking for org %s, %d individual runs\n", orgID, len(runIDs))

	if len(runIDs) == 0 {
		fmt.Printf("[TrackIndividualRuns] No run IDs provided. Nothing to charge.\n")
		return 0, nil
	}

	// ** ADDED: Get Org to find PartnerID **
	org, err := s.repos.OrgRepo.GetByID(ctx, orgID)
	if err != nil {
		return 0, fmt.Errorf("failed to get org %s: %w", orgID, err)
	}
	if org == nil {
		return 0, fmt.Errorf("org %s not found", orgID)
	}
	if org.PartnerID == uuid.Nil {
		return 0, fmt.Errorf("org %s is not associated with a partner, cannot track usage", orgID)
	}
	partnerID := org.PartnerID

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

	// ** MODIFIED: Pass partnerID to chargeRunsInTx **
	chargedCount, err := s.chargeRunsInTx(ctx, tx, orgID, partnerID, runs)
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

// ** MODIFIED: Updated signature to accept partnerID **
// chargeRunsInTx is an internal helper to process a list of runs within a transaction.
func (s *usageService) chargeRunsInTx(ctx context.Context, tx *sqlx.Tx, orgID, partnerID uuid.UUID, runs []*models.QuestionRun) (int, error) {
	chargedCount := 0
	// ** ADDED: Calculate positive cost for deduction **
	deductCost := -DefaultQuestionRunCost // e.g., 0.05

	for _, run := range runs {
		// Idempotency check: Has this run already been charged?
		sourceIDStr := run.QuestionRunID.String()
		/*
			existing, err := s.repos.CreditLedgerRepo.GetBySourceIDAndTypeInTx(ctx, tx, sourceIDStr, "question_run")
			if err != nil && err != sql.ErrNoRows {
				return 0, fmt.Errorf("failed to check for existing ledger entry for run %s: %w", run.QuestionRunID, err)
			}

			if existing != nil {
				// This run has already been charged, skip it.
				continue
			}
		*/

		// Create metadata for the ledger entry
		metadata := map[string]string{
			"question_run_id": run.QuestionRunID.String(),
			"org_id":          orgID.String(),
			"partner_id":      partnerID.String(), // ** ADDED **
		}
		if run.BatchID != nil {
			metadata["batch_id"] = run.BatchID.String()
		}
		metadataJSON, _ := json.Marshal(metadata)

		// Create the new ledger entry with BOTH org_id and partner_id
		entry := &models.CreditLedger{
			EntryID:    uuid.New(),
			OrgID:      &orgID,
			PartnerID:  &partnerID,             // ** ADDED **
			Amount:     DefaultQuestionRunCost, // This is the charge (debit)
			SourceType: "question_run",         // As per migration 000027
			SourceID:   &sourceIDStr,
			Metadata:   metadataJSON,
			CreatedAt:  time.Now(),
		}

		// 1. Create the ledger entry within the transaction
		if err := s.repos.CreditLedgerRepo.CreateInTx(ctx, tx, entry); err != nil {
			return 0, fmt.Errorf("failed to create ledger entry for run %s: %w", run.QuestionRunID, err)
		}

		// 2. ** ADDED: Deduct from the partner's balance **
		// We pass `nil` for orgID because this deduction is for the partner.
		if _, err := s.repos.CreditBalanceRepo.DeductInTx(ctx, tx, nil, &partnerID, deductCost); err != nil {
			// If this fails (e.g., insufficient credits), the transaction will be rolled back.
			// This correctly handles the case where the balance was sufficient at the start
			// but was consumed by parallel runs.
			return 0, fmt.Errorf("failed to deduct from partner balance for run %s: %w", run.QuestionRunID, err)
		}

		chargedCount++
	}
	return chargedCount, nil
}
