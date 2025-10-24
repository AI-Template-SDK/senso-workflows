// workflows/org_evaluation_processor.go
package workflows

import (
	"context"
	"fmt"

	"github.com/inngest/inngestgo"
	"github.com/inngest/inngestgo/step"

	"github.com/AI-Template-SDK/senso-workflows/internal/config"
	"github.com/AI-Template-SDK/senso-workflows/services"
	"github.com/google/uuid"
)

type OrgEvaluationProcessor struct {
	orgService           services.OrgService
	orgEvaluationService services.OrgEvaluationService
	usageService         services.UsageService
	client               inngestgo.Client
	cfg                  *config.Config
}

func NewOrgEvaluationProcessor(
	orgService services.OrgService,
	orgEvaluationService services.OrgEvaluationService,
	usageService services.UsageService,
	cfg *config.Config,
) *OrgEvaluationProcessor {
	return &OrgEvaluationProcessor{
		orgService:           orgService,
		orgEvaluationService: orgEvaluationService,
		usageService:         usageService,
		cfg:                  cfg,
	}
}

func (p *OrgEvaluationProcessor) SetClient(client inngestgo.Client) {
	p.client = client
}

// OrgEvaluationProcessEvent represents the event data for org evaluation processing
type OrgEvaluationProcessEvent struct {
	OrgID       string `json:"org_id"`
	TriggeredBy string `json:"triggered_by,omitempty"`
	UserID      string `json:"user_id,omitempty"`
}

func (p *OrgEvaluationProcessor) ProcessOrgEvaluation() inngestgo.ServableFunction {
	fn, err := inngestgo.CreateFunction(
		p.client,
		inngestgo.FunctionOpts{
			ID:      "process-org-evaluation",
			Name:    "Process Organization Evaluation - Advanced Brand Analysis Pipeline",
			Retries: inngestgo.IntPtr(3),
		},
		inngestgo.EventTrigger("org.evaluation.process", nil),
		func(ctx context.Context, input inngestgo.Input[OrgEvaluationProcessEvent]) (any, error) {
			orgID := input.Event.Data.OrgID
			fmt.Printf("[ProcessOrgEvaluation] Starting advanced brand analysis pipeline for org: %s\n", orgID)

			// Step 1: Get or Create Today's Batch (with resume support)
			batchData, err := step.Run(ctx, "get-or-create-batch", func(ctx context.Context) (interface{}, error) {
				fmt.Printf("[ProcessOrgEvaluation] Step 1: Getting or creating batch for org: %s\n", orgID)

				orgUUID, err := uuid.Parse(orgID)
				if err != nil {
					return nil, fmt.Errorf("invalid org ID: %w", err)
				}

				// First get org details to calculate total questions
				orgDetails, err := p.orgService.GetOrgDetails(ctx, orgID)
				if err != nil {
					return nil, fmt.Errorf("failed to get org details: %w", err)
				}

				totalQuestions := len(orgDetails.Questions) * len(orgDetails.Models) * len(orgDetails.Locations)

				// Use new resume-aware method
				batch, isExisting, err := p.orgEvaluationService.GetOrCreateTodaysBatch(ctx, orgUUID, totalQuestions)
				if err != nil {
					return nil, fmt.Errorf("failed to get or create batch: %w", err)
				}

				if isExisting {
					fmt.Printf("[ProcessOrgEvaluation] ✅ Resuming existing batch %s (status: %s)\n", batch.BatchID, batch.Status)
				} else {
					fmt.Printf("[ProcessOrgEvaluation] ✅ Created new batch %s with %d total questions\n", batch.BatchID, totalQuestions)
				}

				return map[string]interface{}{
					"batch_id":        batch.BatchID.String(),
					"total_questions": totalQuestions,
					"org_id":          orgID,
					"is_existing":     isExisting,
					"batch_status":    batch.Status,
				}, nil
			})
			if err != nil {
				return nil, fmt.Errorf("step 1 failed: %w", err)
			}

			batchInfo := batchData.(map[string]interface{})
			batchID := batchInfo["batch_id"].(string)
			isExistingBatch := batchInfo["is_existing"].(bool)
			_ = batchInfo["batch_status"].(string) // Unused for now

			// Step 2: Start Batch Processing (only if new or pending)
			_, err = step.Run(ctx, "start-batch-processing", func(ctx context.Context) (interface{}, error) {
				batchUUID, err := uuid.Parse(batchID)
				if err != nil {
					return nil, fmt.Errorf("invalid batch ID: %w", err)
				}

				// Only start if this is a new batch
				if !isExistingBatch {
					fmt.Printf("[ProcessOrgEvaluation] Step 2: Starting batch processing for new batch: %s\n", batchID)
					if err := p.orgEvaluationService.StartBatch(ctx, batchUUID); err != nil {
						return nil, fmt.Errorf("failed to start batch: %w", err)
					}
					fmt.Printf("[ProcessOrgEvaluation] ✅ Batch %s status updated to running\n", batchID)
				} else {
					fmt.Printf("[ProcessOrgEvaluation] Step 2: Resuming existing batch: %s\n", batchID)
				}

				return map[string]interface{}{
					"batch_id": batchID,
					"status":   "running",
				}, nil
			})
			if err != nil {
				return nil, fmt.Errorf("step 2 failed: %w", err)
			}

			// Step 3: Run Question Matrix with Org Evaluation (BATCHED APPROACH)
			processingData, err := step.Run(ctx, "run-question-matrix-with-evaluation", func(ctx context.Context) (interface{}, error) {
				fmt.Printf("[ProcessOrgEvaluation] Step 3: Running question matrix with org evaluation for org: %s\n", orgID)

				batchUUID, err := uuid.Parse(batchID)
				if err != nil {
					return nil, fmt.Errorf("invalid batch ID: %w", err)
				}

				// Fetch org details
				orgDetails, err := p.orgService.GetOrgDetails(ctx, orgID)
				if err != nil {
					return nil, fmt.Errorf("failed to get org details: %w", err)
				}

				fmt.Printf("[ProcessOrgEvaluation] Successfully loaded org: %s with %d models, %d locations, %d questions\n",
					orgDetails.Org.Name, len(orgDetails.Models), len(orgDetails.Locations), len(orgDetails.Questions))

				// Execute the entire matrix with batching and resume support
				// This single call handles:
				// - Name variation generation
				// - Question execution (batched for BrightData/Perplexity)
				// - Extraction processing (org_eval, citations, competitors)
				summary, err := p.orgEvaluationService.RunQuestionMatrixWithOrgEvaluation(ctx, orgDetails, batchUUID)
				if err != nil {
					return nil, fmt.Errorf("failed to run question matrix: %w", err)
				}

				fmt.Printf("[ProcessOrgEvaluation] ✅ Question matrix completed: %d processed, %d evaluations, %d citations, %d competitors, $%.6f total cost\n",
					summary.TotalProcessed, summary.TotalEvaluations, summary.TotalCitations, summary.TotalCompetitors, summary.TotalCost)

				return map[string]interface{}{
					"total_processed":   summary.TotalProcessed,
					"total_evaluations": summary.TotalEvaluations,
					"total_citations":   summary.TotalCitations,
					"total_competitors": summary.TotalCompetitors,
					"total_cost":        summary.TotalCost,
					"errors":            summary.ProcessingErrors,
				}, nil
			})
			if err != nil {
				return nil, fmt.Errorf("step 3 failed: %w", err)
			}

			processingSummary := processingData.(map[string]interface{})

			// Step 4: Track Usage for Successful Runs
			usageData, err := step.Run(ctx, "track-usage", func(ctx context.Context) (interface{}, error) {
				fmt.Printf("[ProcessOrgEvaluation] Step 4: Tracking usage for batch: %s\n", batchID)

				batchUUID, err := uuid.Parse(batchID)
				if err != nil {
					return nil, fmt.Errorf("invalid batch ID: %w", err)
				}
				orgUUID, err := uuid.Parse(orgID)
				if err != nil {
					return nil, fmt.Errorf("invalid org ID: %w", err)
				}

				// Call the usage service to create idempotent ledger entries
				// This service internally fetches all successful runs for the batch and charges for them.
				chargedCount, err := p.usageService.TrackBatchUsage(ctx, orgUUID, batchUUID)
				if err != nil {
					return nil, fmt.Errorf("failed to track usage: %w", err)
				}

				fmt.Printf("[ProcessOrgEvaluation] ✅ Usage tracking completed: %d new runs charged\n", chargedCount)
				return map[string]interface{}{
					"charged_runs": chargedCount,
				}, nil
			})
			if err != nil {
				// Log the error but don't fail the entire pipeline
				fmt.Printf("[ProcessOrgEvaluation] Warning: Step 4 (track-usage) failed: %v\n", err)
			}

			// Step 5: Complete Batch
			_, err = step.Run(ctx, "complete-batch", func(ctx context.Context) (interface{}, error) {
				fmt.Printf("[ProcessOrgEvaluation] Step 4: Completing batch: %s\n", batchID)

				batchUUID, err := uuid.Parse(batchID)
				if err != nil {
					return nil, fmt.Errorf("invalid batch ID: %w", err)
				}

				if err := p.orgEvaluationService.CompleteBatch(ctx, batchUUID); err != nil {
					return nil, fmt.Errorf("failed to complete batch: %w", err)
				}

				fmt.Printf("[ProcessOrgEvaluation] ✅ Batch %s completed successfully\n", batchID)
				return map[string]interface{}{
					"batch_id": batchID,
					"status":   "completed",
				}, nil
			})
			if err != nil {
				return nil, fmt.Errorf("step 4 failed: %w", err)
			}

			// Step 6: Generate Processing Summary (was Step 5)
			finalResult := map[string]interface{}{
				"org_id":            orgID,
				"batch_id":          batchID,
				"total_processed":   processingSummary["total_processed"],
				"total_evaluations": processingSummary["total_evaluations"],
				"total_citations":   processingSummary["total_citations"],
				"total_competitors": processingSummary["total_competitors"],
				"total_cost":        processingSummary["total_cost"],
				"processing_errors": processingSummary["errors"],
				"status":            "completed",
			}
			if usageData != nil {
				finalResult["usage_data"] = usageData
			}

			fmt.Printf("[ProcessOrgEvaluation] 🎉 Org evaluation pipeline completed for org: %s\n", orgID)
			fmt.Printf("[ProcessOrgEvaluation] Summary: %d processed, %d evaluations, %d citations, %d competitors, $%.6f cost\n",
				processingSummary["total_processed"], processingSummary["total_evaluations"],
				processingSummary["total_citations"], processingSummary["total_competitors"],
				processingSummary["total_cost"])

			return finalResult, nil
		},
	)

	if err != nil {
		panic(fmt.Sprintf("Failed to create ProcessOrgEvaluation function: %v", err))
	}

	return fn
}
