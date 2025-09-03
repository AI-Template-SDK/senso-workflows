// workflows/org_evaluation_processor.go
package workflows

import (
	"context"
	"fmt"
	"time"

	"github.com/inngest/inngestgo"
	"github.com/inngest/inngestgo/step"

	"github.com/AI-Template-SDK/senso-api/pkg/models"
	"github.com/AI-Template-SDK/senso-workflows/internal/config"
	"github.com/AI-Template-SDK/senso-workflows/services"
	"github.com/google/uuid"
)

type OrgEvaluationProcessor struct {
	orgService           services.OrgService
	orgEvaluationService services.OrgEvaluationService
	client               inngestgo.Client
	cfg                  *config.Config
}

func NewOrgEvaluationProcessor(
	orgService services.OrgService,
	orgEvaluationService services.OrgEvaluationService,
	cfg *config.Config,
) *OrgEvaluationProcessor {
	return &OrgEvaluationProcessor{
		orgService:           orgService,
		orgEvaluationService: orgEvaluationService,
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

			// Step 1: Create Question Run Batch
			batchData, err := step.Run(ctx, "create-question-run-batch", func(ctx context.Context) (interface{}, error) {
				fmt.Printf("[ProcessOrgEvaluation] Step 1: Creating question run batch for org: %s\n", orgID)

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

				// Create batch with pending status
				batch := &models.QuestionRunBatch{
					BatchID:            uuid.New(),
					Scope:              "org",
					OrgID:              &orgUUID,
					BatchType:          "manual",
					Status:             "pending",
					TotalQuestions:     totalQuestions,
					CompletedQuestions: 0,
					FailedQuestions:    0,
					IsLatest:           true,
				}

				if err := p.orgEvaluationService.CreateBatch(ctx, batch); err != nil {
					return nil, fmt.Errorf("failed to create question run batch: %w", err)
				}

				fmt.Printf("[ProcessOrgEvaluation] ✅ Created batch %s with %d total questions\n", batch.BatchID, totalQuestions)
				return map[string]interface{}{
					"batch_id":        batch.BatchID.String(),
					"total_questions": totalQuestions,
					"org_id":          orgID,
				}, nil
			})
			if err != nil {
				return nil, fmt.Errorf("step 1 failed: %w", err)
			}

			batchInfo := batchData.(map[string]interface{})
			batchID := batchInfo["batch_id"].(string)

			// Step 2: Start Batch Processing
			_, err = step.Run(ctx, "start-batch-processing", func(ctx context.Context) (interface{}, error) {
				fmt.Printf("[ProcessOrgEvaluation] Step 2: Starting batch processing for batch: %s\n", batchID)

				batchUUID, err := uuid.Parse(batchID)
				if err != nil {
					return nil, fmt.Errorf("invalid batch ID: %w", err)
				}

				// Update batch to running status
				if err := p.orgEvaluationService.StartBatch(ctx, batchUUID); err != nil {
					return nil, fmt.Errorf("failed to start batch: %w", err)
				}

				fmt.Printf("[ProcessOrgEvaluation] ✅ Batch %s status updated to running\n", batchID)
				return map[string]interface{}{
					"batch_id": batchID,
					"status":   "running",
				}, nil
			})
			if err != nil {
				return nil, fmt.Errorf("step 2 failed: %w", err)
			}

			// Step 3: Fetch Organization Details
			orgData, err := step.Run(ctx, "fetch-org-details", func(ctx context.Context) (interface{}, error) {
				fmt.Printf("[ProcessOrgEvaluation] Step 3: Fetching org details for org: %s\n", orgID)

				orgDetails, err := p.orgService.GetOrgDetails(ctx, orgID)
				if err != nil {
					return nil, fmt.Errorf("failed to get org details: %w", err)
				}

				fmt.Printf("[ProcessOrgEvaluation] Successfully loaded org: %s with %d models, %d locations, %d questions\n",
					orgDetails.Org.Name, len(orgDetails.Models), len(orgDetails.Locations), len(orgDetails.Questions))

				return map[string]interface{}{
					"org_id":          orgID,
					"org_name":        orgDetails.Org.Name,
					"target_company":  orgDetails.TargetCompany,
					"websites":        orgDetails.Websites,
					"models_count":    len(orgDetails.Models),
					"locations_count": len(orgDetails.Locations),
					"questions_count": len(orgDetails.Questions),
				}, nil
			})
			if err != nil {
				return nil, fmt.Errorf("step 3 failed: %w", err)
			}

			// Extract data from step 3
			orgDataMap := orgData.(map[string]interface{})
			orgName := orgDataMap["org_name"].(string)
			targetCompany := orgDataMap["target_company"].(string)

			// Step 4: Generate Name Variations
			nameVariationsResult, err := step.Run(ctx, "generate-name-variations", func(ctx context.Context) (interface{}, error) {
				fmt.Printf("[ProcessOrgEvaluation] Step 4: Generating name variations for org: %s\n", orgName)

				// Get org details to get websites
				orgDetails, err := p.orgService.GetOrgDetails(ctx, orgID)
				if err != nil {
					return nil, fmt.Errorf("failed to get org details: %w", err)
				}

				// Generate name variations once for the entire org
				nameVariations, err := p.orgEvaluationService.GenerateNameVariations(ctx, orgDetails.Org.Name, orgDetails.Websites)
				if err != nil {
					return nil, fmt.Errorf("failed to generate name variations: %w", err)
				}

				fmt.Printf("[ProcessOrgEvaluation] ✅ Generated %d name variations\n", len(nameVariations))
				return map[string]interface{}{
					"name_variations": nameVariations,
					"org_name":        orgDetails.Org.Name,
					"websites":        orgDetails.Websites,
				}, nil
			})
			if err != nil {
				return nil, fmt.Errorf("step 4 failed: %w", err)
			}

			nameVariationsData := nameVariationsResult.(map[string]interface{})

			// Convert []interface{} to []string for name variations
			nameVariationsInterface := nameVariationsData["name_variations"].([]interface{})
			nameVariations := make([]string, len(nameVariationsInterface))
			for i, v := range nameVariationsInterface {
				nameVariations[i] = v.(string)
			}

			// Convert []interface{} to []string for websites
			websitesInterface := nameVariationsData["websites"].([]interface{})
			websites := make([]string, len(websitesInterface))
			for i, v := range websitesInterface {
				websites[i] = v.(string)
			}

			// Step 5: Calculate Question Matrix
			questionJobsResult, err := step.Run(ctx, "calculate-question-matrix", func(ctx context.Context) (interface{}, error) {
				fmt.Printf("[ProcessOrgEvaluation] Step 5: Calculating question matrix for org: %s\n", orgName)

				// Get org details for question matrix calculation
				orgDetails, err := p.orgService.GetOrgDetails(ctx, orgID)
				if err != nil {
					return nil, fmt.Errorf("failed to get org details: %w", err)
				}

				// Calculate question jobs
				jobs, err := p.orgEvaluationService.CalculateQuestionMatrix(ctx, orgDetails)
				if err != nil {
					return nil, fmt.Errorf("failed to calculate question matrix: %w", err)
				}

				fmt.Printf("[ProcessOrgEvaluation] ✅ Created %d question jobs\n", len(jobs))
				return map[string]interface{}{
					"jobs":       jobs,
					"total_jobs": len(jobs),
				}, nil
			})
			if err != nil {
				return nil, fmt.Errorf("step 5 failed: %w", err)
			}

			questionJobsData := questionJobsResult.(map[string]interface{})
			jobs := questionJobsData["jobs"].([]interface{})
			totalJobs := int(questionJobsData["total_jobs"].(float64))

			// Steps 6-N: Process Each Question Job Individually
			var allResults []interface{}
			for i, jobInterface := range jobs {
				jobData := jobInterface.(map[string]interface{})
				jobIndex := i + 1
				stepName := fmt.Sprintf("process-question-job-%d", jobIndex)

				result, err := step.Run(ctx, stepName, func(ctx context.Context) (interface{}, error) {
					fmt.Printf("[ProcessOrgEvaluation] Step %d: Processing question job %d/%d\n", jobIndex+5, jobIndex, totalJobs)

					// Parse job data
					questionID, _ := uuid.Parse(jobData["question_id"].(string))
					modelID, _ := uuid.Parse(jobData["model_id"].(string))
					locationID, _ := uuid.Parse(jobData["location_id"].(string))

					job := &services.QuestionJob{
						QuestionID:   questionID,
						ModelID:      modelID,
						LocationID:   locationID,
						QuestionText: jobData["question_text"].(string),
						ModelName:    jobData["model_name"].(string),
						LocationCode: jobData["location_code"].(string),
						LocationName: jobData["location_name"].(string),
						JobIndex:     int(jobData["job_index"].(float64)),
						TotalJobs:    int(jobData["total_jobs"].(float64)),
					}

					// Parse batch ID and org ID
					batchUUID, err := uuid.Parse(batchID)
					if err != nil {
						return nil, fmt.Errorf("invalid batch ID: %w", err)
					}

					orgUUID, err := uuid.Parse(orgID)
					if err != nil {
						return nil, fmt.Errorf("invalid org ID: %w", err)
					}

					// Process the single question job
					result, err := p.orgEvaluationService.ProcessSingleQuestionJob(ctx, job, orgUUID, orgName, websites, nameVariations, batchUUID)
					if err != nil {
						return nil, fmt.Errorf("failed to process question job: %w", err)
					}

					// Update batch progress based on result
					if result.Status == "completed" {
						if updateErr := p.orgEvaluationService.UpdateBatchProgress(ctx, batchUUID, 1, 0); updateErr != nil {
							fmt.Printf("[ProcessOrgEvaluation] Warning: Failed to update batch progress: %v\n", updateErr)
						}
					} else {
						if updateErr := p.orgEvaluationService.UpdateBatchProgress(ctx, batchUUID, 0, 1); updateErr != nil {
							fmt.Printf("[ProcessOrgEvaluation] Warning: Failed to update batch progress: %v\n", updateErr)
						}
					}

					fmt.Printf("[ProcessOrgEvaluation] ✅ Completed question job %d/%d: %s\n", jobIndex, totalJobs, result.Status)

					return map[string]interface{}{
						"job_index":        result.JobIndex,
						"question_run_id":  result.QuestionRunID.String(),
						"status":           result.Status,
						"has_evaluation":   result.HasEvaluation,
						"competitor_count": result.CompetitorCount,
						"citation_count":   result.CitationCount,
						"total_cost":       result.TotalCost,
						"error_message":    result.ErrorMessage,
					}, nil
				})
				if err != nil {
					fmt.Printf("[ProcessOrgEvaluation] Warning: Failed to process question job %d/%d: %v\n", jobIndex, totalJobs, err)
					continue
				}

				// Track that this job was processed
				allResults = append(allResults, result)
			}

			// Calculate final summary from all job results
			processingResult := map[string]interface{}{
				"total_processed":   0,
				"total_evaluations": 0,
				"total_citations":   0,
				"total_competitors": 0,
				"total_cost":        0.0,
				"processing_errors": []string{},
			}

			for _, resultInterface := range allResults {
				resultData := resultInterface.(map[string]interface{})
				if resultData["status"].(string) == "completed" {
					processingResult["total_processed"] = processingResult["total_processed"].(int) + 1
					if resultData["has_evaluation"].(bool) {
						processingResult["total_evaluations"] = processingResult["total_evaluations"].(int) + 1
					}
					processingResult["total_citations"] = processingResult["total_citations"].(int) + int(resultData["citation_count"].(float64))
					processingResult["total_competitors"] = processingResult["total_competitors"].(int) + int(resultData["competitor_count"].(float64))
					processingResult["total_cost"] = processingResult["total_cost"].(float64) + resultData["total_cost"].(float64)
				}
			}

			// Step N+1: Update Latest Flags
			_, err = step.Run(ctx, "update-latest-flags", func(ctx context.Context) (interface{}, error) {
				fmt.Printf("[ProcessOrgEvaluation] Step N+1: Updating latest flags for batch: %s\n", batchID)

				batchUUID, err := uuid.Parse(batchID)
				if err != nil {
					return nil, fmt.Errorf("invalid batch ID: %w", err)
				}

				if err := p.orgEvaluationService.UpdateLatestFlagsForBatch(ctx, batchUUID); err != nil {
					return nil, fmt.Errorf("failed to update latest flags: %w", err)
				}

				fmt.Printf("[ProcessOrgEvaluation] ✅ Updated latest flags for batch %s\n", batchID)
				return map[string]interface{}{
					"batch_id": batchID,
					"status":   "flags_updated",
				}, nil
			})
			if err != nil {
				return nil, fmt.Errorf("update latest flags step failed: %w", err)
			}

			// Step N+2: Complete Batch
			_, err = step.Run(ctx, "complete-batch", func(ctx context.Context) (interface{}, error) {
				fmt.Printf("[ProcessOrgEvaluation] Step N+2: Completing batch: %s\n", batchID)

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
				return nil, fmt.Errorf("complete batch step failed: %w", err)
			}

			// Step N+3: Generate Processing Summary
			finalResult, err := step.Run(ctx, "generate-processing-summary", func(ctx context.Context) (interface{}, error) {
				fmt.Printf("[ProcessOrgEvaluation] Step N+3: Generating processing summary\n")

				processingData := processingResult

				// Calculate processing time
				endTime := time.Now()

				summary := map[string]interface{}{
					"org_id":            orgID,
					"org_name":          orgName,
					"target_company":    targetCompany,
					"processing_time":   endTime.Format("2006-01-02 15:04:05"),
					"total_processed":   processingData["total_processed"],
					"total_evaluations": processingData["total_evaluations"],
					"total_citations":   processingData["total_citations"],
					"total_competitors": processingData["total_competitors"],
					"total_cost":        processingData["total_cost"],
					"processing_errors": processingData["processing_errors"],
					"pipeline_version":  "org_evaluation_v1.0",
					"status":            "completed",
				}

				fmt.Printf("[ProcessOrgEvaluation] 🎉 Org evaluation pipeline completed successfully for org: %s\n", orgName)
				fmt.Printf("[ProcessOrgEvaluation] 📊 Summary: %d evaluations, %d citations, %d competitors processed\n",
					processingData["total_evaluations"], processingData["total_citations"], processingData["total_competitors"])

				return summary, nil
			})
			if err != nil {
				return nil, fmt.Errorf("generate summary step failed: %w", err)
			}

			return finalResult, nil
		},
	)

	if err != nil {
		panic(fmt.Sprintf("Failed to create ProcessOrgEvaluation function: %v", err))
	}

	return fn
}
