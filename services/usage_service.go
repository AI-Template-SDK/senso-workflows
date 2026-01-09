package services

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/AI-Template-SDK/senso-api/pkg/models"
	"github.com/AI-Template-SDK/senso-api/pkg/repositories/postgresql"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

// DefaultQuestionRunCost is the standard hard-coded cost for a single successful question run
const DefaultQuestionRunCost = 0.1

const (
	QuestionTypeOrg     = "org"
	QuestionTypeNetwork = "network"
)

// UsageService defines the interface for tracking question run usage.
type UsageService interface {
	TrackBatchUsage(ctx context.Context, orgID uuid.UUID, batchID uuid.UUID, questionType string) (int, error)
	TrackIndividualRuns(ctx context.Context, orgID uuid.UUID, runIDs []uuid.UUID, questionType string) (int, error)
	// ** ADDED: New method for pre-checking balance **
	CheckBalance(ctx context.Context, orgID uuid.UUID, totalQuestions int, questionType string) (float64, error)
	GetMarginBasedCost(ctx context.Context, run *models.QuestionRun, orgID uuid.UUID, partnerID uuid.UUID, questionType string) (float64, float64, float64, string, error)
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
// CheckBalance verifies if the org or partner has enough balance to cover a cost.
func (s *usageService) CheckBalance(ctx context.Context, orgID uuid.UUID, totalQuestions int, questionType string) (float64, error) {

	if totalQuestions <= 0 {
		return 0, fmt.Errorf("totalQuestions must be > 0")
	}

	// 1. Get Org to find PartnerID
	org, err := s.repos.OrgRepo.GetByID(ctx, orgID)
	if err != nil {
		return 0, fmt.Errorf("failed to get org %s: %w", orgID, err)
	}
	if org == nil {
		return 0, fmt.Errorf("org %s not found", orgID)
	}
	if org.PartnerID == uuid.Nil {
		// Orgs must belong to a partner to have a balance
		return 0, fmt.Errorf("org %s is not associated with a partner", orgID)
	}

	// 2. Calculate Estimated Cost
	var cost float64
	estRunPrice := DefaultQuestionRunCost // Fallback cost
	cost = (estRunPrice) * float64(totalQuestions)
	config, err := s.repos.PricingConfigRepo.GetByPartnerIDAndAction(ctx, org.PartnerID, "question_run")
	if err == nil {
		if config.WholesaleFixedPrice != nil {
			estRunPrice = config.WholesaleFixedPrice
			cost = estRunPrice * float64(totalQuestions)
		}
	}

	// 3. Get Payer Balance
	var balance models.CreditBalance
	if questionType == QuestionTypeOrg && !org.IsFreeTier {
		balance, err = s.repos.CreditBalanceRepo.GetByOrgID(ctx, orgID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				// No balance record means 0 balance
				return cost, fmt.Errorf("insufficient credits for org %s (balance: 0.00, cost: %.2f): %w", orgID, cost, postgresql.ErrInsufficientCredits)
			}
			return cost, fmt.Errorf("failed to get credit balance for org %s: %w", orgID, err)
		}
	} else if questionType == QuestionTypeNetwork || (questionType == QuestionTypeOrg && org.IsFreeTier) {
		balance, err = s.repos.CreditBalanceRepo.GetByPartnerID(ctx, org.PartnerID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				// No balance record means 0 balance
				return cost, fmt.Errorf("insufficient credits for partner %s (balance: 0.00, cost: %.2f): %w", org.PartnerID, cost, postgresql.ErrInsufficientCredits)
			}
			return cost, fmt.Errorf("failed to get credit balance for partner %s: %w", org.PartnerID, err)
		}
	} else {
		return cost, fmt.Errorf("invalid question type %s", questionType)
	}

	// 4. Check Balance
	if balance.CurrentBalance < cost {
		if questionType == QuestionTypeOrg {
			return cost, fmt.Errorf("insufficient credits for org %s (balance: %.2f, cost: %.2f): %w", orgID, balance.CurrentBalance, cost, postgresql.ErrInsufficientCredits)
		} else if questionType == QuestionTypeNetwork {
			return cost, fmt.Errorf("insufficient credits for partner %s (balance: %.2f, cost: %.2f): %w", org.PartnerID, balance.CurrentBalance, cost, postgresql.ErrInsufficientCredits)
		}
	}

	fmt.Printf("[CheckBalance] Org %s (Partner %s) has sufficient balance (%.2f) for cost (%.2f)\n", orgID, org.PartnerID, balance.CurrentBalance, cost)
	return cost, nil // Sufficient balance
}

// TrackBatchUsage iterates through all question runs in a batch and creates an idempotent credit_ledger entry for each.
// ** MODIFIED: It now also deducts from the partner's credit_balance. **
func (s *usageService) TrackBatchUsage(ctx context.Context, orgID uuid.UUID, batchID uuid.UUID, questionType string) (int, error) {
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
	chargedCount, err := s.chargeRunsInTx(ctx, tx, orgID, partnerID, runs, questionType)
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
func (s *usageService) TrackIndividualRuns(ctx context.Context, orgID uuid.UUID, runIDs []uuid.UUID, questionType string) (int, error) {
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
	chargedCount, err := s.chargeRunsInTx(ctx, tx, orgID, partnerID, runs, questionType)
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

func (s *usageService) GetMarginBasedCost(ctx context.Context, run *models.QuestionRun, orgID uuid.UUID, partnerID uuid.UUID, questionType string) (float64, float64, float64, string, error) {
	salesPrice := DefaultQuestionRunCost // Default Question Run Price
	var runCost float64
	var evalCost float64
	var margin float64
	mode := "fixed"
	if run.TotalCost != nil {
		runCost = *run.TotalCost
	}

	if questionType == QuestionTypeOrg {
		evals, err := s.repos.OrgEvalRepo.GetByQuestionRunAndOrg(ctx, run.QuestionRunID, orgID)
		if err != nil {
			return 0, 0, 0, mode, fmt.Errorf("failed to get eval cost for question run %s: %w", run.QuestionRunID, err)
		}
		var latest *models.OrgEval
		for _, eval := range evals {
			if eval == nil {
				continue
			}
			if latest == nil || eval.CreatedAt.After(latest.CreatedAt) {
				latest = eval
			}
		}
		if latest != nil && latest.TotalCost != nil {
			evalCost = *latest.TotalCost
		}
	}

	if questionType == QuestionTypeNetwork {
		evals, err := s.repos.NetworkOrgEvalRepo.GetByQuestionRunAndOrg(ctx, run.QuestionRunID, orgID)
		if err != nil {
			return 0, 0, 0, mode, fmt.Errorf("failed to get eval cost for question run %s: %w", run.QuestionRunID, err)
		}
		var latest *models.NetworkOrgEval
		for _, eval := range evals {
			if eval == nil {
				continue
			}
			if latest == nil || eval.CreatedAt.After(latest.CreatedAt) {
				latest = eval
			}
		}
		if latest != nil && latest.TotalCost != nil {
			evalCost = *latest.TotalCost
		}
	}

	totalCost := runCost + evalCost
	if totalCost > 0 {
		config, err := s.repos.PricingConfigRepo.GetByPartnerIDAndAction(ctx, partnerID, "question_run")
		if err == nil {
			if config.WholesaleMarginPct != nil {
				margin = *config.WholesaleMarginPct
			}
			if margin >= 1 || margin < 0 {
				return 0, 0, 0, mode, fmt.Errorf("partner margin for this org is invalid. Must be between 0 and 1. %s", orgID)
			}

			salesPrice = totalCost / (1 - margin)
			mode = "dynamic"
		}
	}

	// If using fixed pricing, calculate effective margin
	if mode == "fixed" {
		margin = (salesPrice - totalCost) / salesPrice
	}

	return salesPrice, totalCost, margin, mode, nil
}

// ** MODIFIED: Updated signature to accept partnerID **
// chargeRunsInTx is an internal helper to process a list of runs within a transaction.
func (s *usageService) chargeRunsInTx(ctx context.Context, tx *sqlx.Tx, orgID, partnerID uuid.UUID, runs []*models.QuestionRun, questionType string) (int, error) {
	chargedCount := 0

	for _, run := range runs {
		// Idempotency check: Has this run already been charged?
		sourceIDStr := run.QuestionRunID.String()
		// Removed: Outdated Idempotency check
		/*
			existing, err := s.repos.CreditLedgerRepo.GetBySourceIDAndTypeInTx(ctx, tx, sourceIDStr, "question_run")
			if err != nil && err != sql.ErrNoRows {
				return 0, fmt.Errorf("failed to check for existing ledger entry for run %s: %w", run.QuestionRunID, err)
			}

			if existing != nil {
				// This run has already been charged, skip it.
				continue
		}*/

		// 1. Get Margin Based Run Cost
		marginBasedCost, actualCost, marginPct, mode, err := s.GetMarginBasedCost(ctx, run, orgID, partnerID, questionType)
		if err != nil {
			return 0, fmt.Errorf("failed to get margin based cost for question run %s: %w", run.QuestionRunID, err)
		}

		// Create metadata for the ledger entry
		metadata := map[string]string{
			"question_run_id": run.QuestionRunID.String(),
			"org_id":          orgID.String(),
			"partner_id":      partnerID.String(), // ** ADDED **
			"senso_cost":      strconv.FormatFloat(actualCost, 'f', -1, 64),
			"actualcharge":    strconv.FormatFloat(marginBasedCost, 'f', -1, 64),
			"margin_pct":      strconv.FormatFloat(marginPct, 'f', -1, 64),
			"pricing_mode":    mode,
		}
		if run.BatchID != nil {
			metadata["batch_id"] = run.BatchID.String()
		}
		metadataJSON, _ := json.Marshal(metadata)

		// 2. Check if org is free tier
		org, err := s.repos.OrgRepo.GetByID(ctx, orgID)
		if err != nil {
			return 0, fmt.Errorf("failed to get org for question run %s: %w", orgID, err)
		}
		chargePartnerBalance := questionType == QuestionTypeNetwork || (questionType == QuestionTypeOrg && org.isFreeTier)

		if !chargePartnerBalance {
			// Charge the Org
			// Create the new ledger entry with BOTH org_id and partner_id
			entry := &models.CreditLedger{
				EntryID:    uuid.New(),
				OrgID:      &orgID,
				PartnerID:  &partnerID,       // ** ADDED **
				Amount:     -marginBasedCost, // This is the charge (debit)
				SourceType: "question_run",   // As per migration 000027
				SourceID:   &sourceIDStr,
				Metadata:   metadataJSON,
				PayerType:  "ORG",
				CreatedAt:  time.Now(),
			}

			// 3. Create the ledger entry within the transaction
			if err := s.repos.CreditLedgerRepo.CreateInTx(ctx, tx, entry); err != nil {
				return 0, fmt.Errorf("failed to create ledger entry for run %s: %w", run.QuestionRunID, err)
			}

			// 4. Deduct from the partner's balance
			// We pass `nil` for orgID because this deduction is for the partner.
			if _, err := s.repos.CreditBalanceRepo.DeductInTx(ctx, tx, nil, &partnerID, marginBasedCost); err != nil {
				// If this fails (e.g., insufficient credits), the transaction will be rolled back.
				// This correctly handles the case where the balance was sufficient at the start
				// but was consumed by parallel runs.
				return 0, fmt.Errorf("failed to deduct from partner balance for run %s: %w", run.QuestionRunID, err)
			}
			chargedCount++
		} else {

			// Create the new ledger entry with BOTH org_id and partner_id
			// Network Questions always charge the Partner
			entry := &models.CreditLedger{
				EntryID:    uuid.New(),
				OrgID:      &orgID,
				PartnerID:  &partnerID,       // ** ADDED **
				Amount:     -marginBasedCost, // This is the charge (debit)
				SourceType: "question_run",   // As per migration 000027
				SourceID:   &sourceIDStr,
				Metadata:   metadataJSON,
				PayerType:  "PARTNER",
				CreatedAt:  time.Now(),
			}

			// 2. Create the ledger entry within the transaction
			if err := s.repos.CreditLedgerRepo.CreateInTx(ctx, tx, entry); err != nil {
				return 0, fmt.Errorf("failed to create ledger entry for run %s: %w", run.QuestionRunID, err)
			}

			// 3. ** ADDED: Deduct from the partner's balance **
			// We pass `nil` for orgID because this deduction is for the partner.
			if _, err := s.repos.CreditBalanceRepo.DeductInTx(ctx, tx, nil, &partnerID, marginBasedCost); err != nil {
				// If this fails (e.g., insufficient credits), the transaction will be rolled back.
				// This correctly handles the case where the balance was sufficient at the start
				// but was consumed by parallel runs.
				return 0, fmt.Errorf("failed to deduct from partner balance for run %s: %w", run.QuestionRunID, err)
			}
			chargedCount++
		}

	}
	return chargedCount, nil
}
