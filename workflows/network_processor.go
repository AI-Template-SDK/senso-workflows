// workflows/network_processor.go
package workflows

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/inngest/inngestgo"
	"github.com/inngest/inngestgo/step"

	"github.com/AI-Template-SDK/senso-workflows/internal/config"
	"github.com/AI-Template-SDK/senso-workflows/services"
)

type NetworkProcessor struct {
	questionRunnerService services.QuestionRunnerService
	client                inngestgo.Client
	cfg                   *config.Config
}

func NewNetworkProcessor(
	questionRunnerService services.QuestionRunnerService,
	cfg *config.Config,
) *NetworkProcessor {
	return &NetworkProcessor{
		questionRunnerService: questionRunnerService,
		cfg:                   cfg,
	}
}

func (p *NetworkProcessor) SetClient(client inngestgo.Client) {
	p.client = client
}

func (p *NetworkProcessor) ProcessNetwork() inngestgo.ServableFunction {
	fn, err := inngestgo.CreateFunction(
		p.client,
		inngestgo.FunctionOpts{
			ID:      "process-network",
			Name:    "Process Network Questions - Multi-Model/Location Pipeline with Batching",
			Retries: inngestgo.IntPtr(3),
		},
		inngestgo.EventTrigger("network.questions.process", nil),
		func(ctx context.Context, input inngestgo.Input[NetworkProcessEvent]) (any, error) {
			networkID := input.Event.Data.NetworkID
			fmt.Printf("[ProcessNetwork] 🚀 Starting network questions pipeline for network: %s\n", networkID)

			// Step 1: Get or Create Today's Batch (with resume support)
			batchData, err := step.Run(ctx, "get-or-create-batch", func(ctx context.Context) (interface{}, error) {
				fmt.Printf("[ProcessNetwork] Step 1: Getting or creating batch for network: %s\n", networkID)

				networkUUID, err := uuid.Parse(networkID)
				if err != nil {
					return nil, fmt.Errorf("invalid network ID: %w", err)
				}

				// First get network details to calculate total questions
				networkDetails, err := p.questionRunnerService.GetNetworkDetails(ctx, networkID)
				if err != nil {
					return nil, fmt.Errorf("failed to get network details: %w", err)
				}

				// Calculate total questions: questions × models × locations
				totalQuestions := len(networkDetails.Questions) * len(networkDetails.Models) * len(networkDetails.Locations)

				fmt.Printf("[ProcessNetwork] Loaded network with %d models, %d locations, %d questions\n",
					len(networkDetails.Models), len(networkDetails.Locations), len(networkDetails.Questions))

				// Use new resume-aware method
				batch, isExisting, err := p.questionRunnerService.GetOrCreateNetworkBatch(ctx, networkUUID, totalQuestions)
				if err != nil {
					return nil, fmt.Errorf("failed to get or create batch: %w", err)
				}

				if isExisting {
					fmt.Printf("[ProcessNetwork] ✅ Resuming existing batch %s (status: %s)\n", batch.BatchID, batch.Status)
				} else {
					fmt.Printf("[ProcessNetwork] ✅ Created new batch %s with %d total questions\n", batch.BatchID, totalQuestions)
				}

				return map[string]interface{}{
					"batch_id":        batch.BatchID.String(),
					"total_questions": totalQuestions,
					"network_id":      networkID,
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

			// Step 2: Start Batch Processing (only if new or pending)
			_, err = step.Run(ctx, "start-batch-processing", func(ctx context.Context) (interface{}, error) {
				batchUUID, err := uuid.Parse(batchID)
				if err != nil {
					return nil, fmt.Errorf("invalid batch ID: %w", err)
				}

				// Only start if this is a new batch
				if !isExistingBatch {
					fmt.Printf("[ProcessNetwork] Step 2: Starting batch processing for new batch: %s\n", batchID)
					// Start batch by updating status to 'running' and setting started_at timestamp
					if err := p.questionRunnerService.StartNetworkBatch(ctx, batchUUID); err != nil {
						return nil, fmt.Errorf("failed to start batch: %w", err)
					}
					fmt.Printf("[ProcessNetwork] ✅ Batch %s marked as running\n", batchID)
				} else {
					fmt.Printf("[ProcessNetwork] Step 2: Resuming existing batch: %s\n", batchID)
				}

				return map[string]interface{}{
					"batch_id": batchID,
					"status":   "running",
				}, nil
			})
			if err != nil {
				return nil, fmt.Errorf("step 2 failed: %w", err)
			}

			// Step 3: Run Question Matrix (processes across all models and locations with batching)
			processingData, err := step.Run(ctx, "run-question-matrix", func(ctx context.Context) (interface{}, error) {
				fmt.Printf("[ProcessNetwork] Step 3: Running question matrix for network: %s\n", networkID)

				batchUUID, err := uuid.Parse(batchID)
				if err != nil {
					return nil, fmt.Errorf("invalid batch ID: %w", err)
				}

				// Fetch network details
				networkDetails, err := p.questionRunnerService.GetNetworkDetails(ctx, networkID)
				if err != nil {
					return nil, fmt.Errorf("failed to get network details: %w", err)
				}

				fmt.Printf("[ProcessNetwork] Loaded network with %d models, %d locations, %d questions\n",
					len(networkDetails.Models), len(networkDetails.Locations), len(networkDetails.Questions))

				// Execute the entire matrix with batching and resume support
				summary, err := p.questionRunnerService.RunNetworkQuestionMatrix(ctx, networkDetails, batchUUID)
				if err != nil {
					return nil, fmt.Errorf("failed to run question matrix: %w", err)
				}

				fmt.Printf("[ProcessNetwork] ✅ Question matrix completed: %d processed, $%.6f total cost\n",
					summary.TotalProcessed, summary.TotalCost)

				// Update batch progress with completed counts
				failedCount := len(summary.ProcessingErrors)
				if err := p.questionRunnerService.UpdateNetworkBatchProgress(ctx, batchUUID, summary.TotalProcessed, failedCount); err != nil {
					fmt.Printf("[ProcessNetwork] Warning: Failed to update batch progress: %v\n", err)
					// Don't fail the step, just log the warning
				}

				return map[string]interface{}{
					"total_processed":   summary.TotalProcessed,
					"total_cost":        summary.TotalCost,
					"processing_errors": summary.ProcessingErrors,
					"models_used":       len(networkDetails.Models),
					"locations_used":    len(networkDetails.Locations),
				}, nil
			})
			if err != nil {
				return nil, fmt.Errorf("step 3 failed: %w", err)
			}

			processingSummary := processingData.(map[string]interface{})

			// Step 4: Update Latest Flags
			_, err = step.Run(ctx, "update-latest-flags", func(ctx context.Context) (interface{}, error) {
				fmt.Printf("[ProcessNetwork] Step 4: Updating latest flags for network questions\n")

				// Fetch the actual runs that were created and update latest flags
				if err := p.questionRunnerService.UpdateNetworkLatestFlags(ctx, networkID); err != nil {
					return nil, fmt.Errorf("failed to update latest flags: %w", err)
				}

				fmt.Printf("[ProcessNetwork] ✅ Successfully updated latest flags for network: %s\n", networkID)
				return map[string]interface{}{
					"status":     "completed",
					"network_id": networkID,
				}, nil
			})
			if err != nil {
				return nil, fmt.Errorf("step 4 failed: %w", err)
			}

			// Step 5: Complete Batch
			_, err = step.Run(ctx, "complete-batch", func(ctx context.Context) (interface{}, error) {
				fmt.Printf("[ProcessNetwork] Step 5: Completing batch: %s\n", batchID)

				batchUUID, err := uuid.Parse(batchID)
				if err != nil {
					return nil, fmt.Errorf("invalid batch ID: %w", err)
				}

				// Calculate failed questions from processing errors
				totalProcessed := int(processingSummary["total_processed"].(float64))
				processingErrorsList := processingSummary["processing_errors"].([]interface{})
				totalFailed := len(processingErrorsList)

				// Mark batch as completed with final counts and completion timestamp
				if err := p.questionRunnerService.CompleteNetworkBatch(ctx, batchUUID, totalProcessed, totalFailed); err != nil {
					return nil, fmt.Errorf("failed to complete batch: %w", err)
				}

				fmt.Printf("[ProcessNetwork] ✅ Batch %s completed successfully (processed=%d, failed=%d)\n", batchID, totalProcessed, totalFailed)
				return map[string]interface{}{
					"batch_id": batchID,
					"status":   "completed",
				}, nil
			})
			if err != nil {
				return nil, fmt.Errorf("step 5 failed: %w", err)
			}

			// Final Result Summary
			finalResult := map[string]interface{}{
				"network_id":          networkID,
				"batch_id":            batchID,
				"status":              "completed",
				"pipeline":            "network_questions_multi_model",
				"questions_processed": processingSummary["total_processed"],
				"total_cost":          processingSummary["total_cost"],
				"processing_errors":   processingSummary["processing_errors"],
				"models_used":         processingSummary["models_used"],
				"locations_used":      processingSummary["locations_used"],
				"completed_at":        time.Now().UTC(),
			}

			fmt.Printf("[ProcessNetwork] 🎉 COMPLETED: Network questions pipeline for network %s\n", networkID)
			fmt.Printf("[ProcessNetwork] 📊 Summary: %d processed, $%.6f cost, %d models × %d locations\n",
				processingSummary["total_processed"], processingSummary["total_cost"],
				processingSummary["models_used"], processingSummary["locations_used"])

			return finalResult, nil
		},
	)
	if err != nil {
		panic(fmt.Errorf("failed to create ProcessNetwork function: %w", err))
	}
	return fn
}

// Event types
type NetworkProcessEvent struct {
	NetworkID   string `json:"network_id"`
	TriggeredBy string `json:"triggered_by"`
	UserID      string `json:"user_id,omitempty"`
}
