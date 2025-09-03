package workflows

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	inngestgo "github.com/inngest/inngestgo"
	"github.com/inngest/inngestgo/step"

	"github.com/AI-Template-SDK/senso-workflows/internal/config"
	"github.com/AI-Template-SDK/senso-workflows/services"
)

// OrgReevalProcessor handles org re-evaluation workflows
type OrgReevalProcessor struct {
	client               inngestgo.Client
	orgService           services.OrgService
	orgEvaluationService services.OrgEvaluationService
}

// NewOrgReevalProcessor creates a new org re-evaluation processor
func NewOrgReevalProcessor(cfg *config.Config, orgService services.OrgService, orgEvaluationService services.OrgEvaluationService) *OrgReevalProcessor {
	return &OrgReevalProcessor{
		orgService:           orgService,
		orgEvaluationService: orgEvaluationService,
	}
}

// SetClient sets the Inngest client for this processor
func (p *OrgReevalProcessor) SetClient(client inngestgo.Client) {
	p.client = client
}

// OrgReevalProcessEvent represents the event data for org re-evaluation processing
type OrgReevalProcessEvent struct {
	OrgID       string `json:"org_id"`
	TriggeredBy string `json:"triggered_by,omitempty"`
	UserID      string `json:"user_id,omitempty"`
}

func (p *OrgReevalProcessor) ProcessOrgReeval() inngestgo.ServableFunction {
	fn, err := inngestgo.CreateFunction(
		p.client,
		inngestgo.FunctionOpts{
			ID:      "process-org-reeval-all",
			Name:    "Process Organization Re-evaluation - All Question Runs",
			Retries: inngestgo.IntPtr(3),
		},
		inngestgo.EventTrigger("org.reeval.all.process", nil),
		func(ctx context.Context, input inngestgo.Input[OrgReevalProcessEvent]) (any, error) {
			orgID := input.Event.Data.OrgID
			fmt.Printf("[ProcessOrgReeval] Starting org re-evaluation for ALL question runs for org: %s\n", orgID)

			// Step 1: Fetch Org Details
			orgDetailsResult, err := step.Run(ctx, "fetch-org-details", func(ctx context.Context) (interface{}, error) {
				fmt.Printf("[ProcessOrgReeval] Step 1: Fetching org details for org: %s\n", orgID)

				orgDetails, err := p.orgService.GetOrgDetails(ctx, orgID)
				if err != nil {
					return nil, fmt.Errorf("failed to get org details: %w", err)
				}

				fmt.Printf("[ProcessOrgReeval] Successfully loaded org: %s with %d questions, %d websites\n",
					orgDetails.Org.Name, len(orgDetails.Questions), len(orgDetails.Websites))

				return map[string]interface{}{
					"org_id":   orgID,
					"org_name": orgDetails.Org.Name,
					"websites": orgDetails.Websites,
				}, nil
			})
			if err != nil {
				return nil, fmt.Errorf("step 1 failed: %w", err)
			}

			orgDetailsData := orgDetailsResult.(map[string]interface{})
			orgName := orgDetailsData["org_name"].(string)

			// Convert []interface{} to []string for websites
			websitesInterface := orgDetailsData["websites"].([]interface{})
			websites := make([]string, len(websitesInterface))
			for i, v := range websitesInterface {
				websites[i] = v.(string)
			}

			// Step 2: Generate Name Variations
			nameVariationsResult, err := step.Run(ctx, "generate-name-variations", func(ctx context.Context) (interface{}, error) {
				fmt.Printf("[ProcessOrgReeval] Step 2: Generating name variations for org: %s\n", orgName)

				// Generate name variations once for the entire org
				nameVariations, err := p.orgEvaluationService.GenerateNameVariations(ctx, orgName, websites)
				if err != nil {
					return nil, fmt.Errorf("failed to generate name variations: %w", err)
				}

				fmt.Printf("[ProcessOrgReeval] ✅ Generated %d name variations\n", len(nameVariations))
				return map[string]interface{}{
					"name_variations": nameVariations,
				}, nil
			})
			if err != nil {
				return nil, fmt.Errorf("step 2 failed: %w", err)
			}

			nameVariationsData := nameVariationsResult.(map[string]interface{})

			// Convert []interface{} to []string for name variations
			nameVariationsInterface := nameVariationsData["name_variations"].([]interface{})
			nameVariations := make([]string, len(nameVariationsInterface))
			for i, v := range nameVariationsInterface {
				nameVariations[i] = v.(string)
			}

			// Step 3: Fetch ALL Question Runs
			questionRunsResult, err := step.Run(ctx, "fetch-all-question-runs", func(ctx context.Context) (interface{}, error) {
				fmt.Printf("[ProcessOrgReeval] Step 3: Fetching ALL question runs for org: %s\n", orgName)

				orgUUID, err := uuid.Parse(orgID)
				if err != nil {
					return nil, fmt.Errorf("invalid org ID: %w", err)
				}

				// Get ALL question runs for this org
				questionRuns, err := p.orgEvaluationService.GetAllOrgQuestionRuns(ctx, orgUUID)
				if err != nil {
					return nil, fmt.Errorf("failed to get all question runs: %w", err)
				}

				fmt.Printf("[ProcessOrgReeval] ✅ Found %d total question runs to re-evaluate\n", len(questionRuns))
				return map[string]interface{}{
					"question_runs": questionRuns,
					"total_runs":    len(questionRuns),
				}, nil
			})
			if err != nil {
				return nil, fmt.Errorf("step 3 failed: %w", err)
			}

			questionRunsData := questionRunsResult.(map[string]interface{})
			questionRuns := questionRunsData["question_runs"].([]interface{})
			totalRuns := int(questionRunsData["total_runs"].(float64))

			// Steps 4-N: Process Each Question Run Individually
			var allResults []interface{}
			for i, questionRunInterface := range questionRuns {
				questionRunData := questionRunInterface.(map[string]interface{})
				runIndex := i + 1
				stepName := fmt.Sprintf("process-question-run-%d", runIndex)

				result, err := step.Run(ctx, stepName, func(ctx context.Context) (interface{}, error) {
					fmt.Printf("[ProcessOrgReeval] Step %d: Processing question run %d/%d\n", runIndex+3, runIndex, totalRuns)

					// Parse question run data
					questionRunID, _ := uuid.Parse(questionRunData["question_run_id"].(string))
					questionText := questionRunData["question_text"].(string)
					responseText := questionRunData["response_text"].(string)

					// Parse org ID
					orgUUID, err := uuid.Parse(orgID)
					if err != nil {
						return nil, fmt.Errorf("invalid org ID: %w", err)
					}

					// Process the question run re-evaluation
					result, err := p.orgEvaluationService.ProcessOrgQuestionRunReeval(ctx, questionRunID, orgUUID, orgName, websites, nameVariations, questionText, responseText)
					if err != nil {
						return nil, fmt.Errorf("failed to process question run re-evaluation: %w", err)
					}

					fmt.Printf("[ProcessOrgReeval] ✅ Completed question run %d/%d: %s\n", runIndex, totalRuns, result.Status)

					return map[string]interface{}{
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
					fmt.Printf("[ProcessOrgReeval] Warning: Failed to process question run %d/%d: %v\n", runIndex, totalRuns, err)
					continue
				}

				// Track that this question run was processed
				allResults = append(allResults, result)
			}

			// Calculate final summary from all results
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

			// Step N+1: Generate Processing Summary
			finalResult, err := step.Run(ctx, "generate-processing-summary", func(ctx context.Context) (interface{}, error) {
				fmt.Printf("[ProcessOrgReeval] Step N+1: Generating processing summary\n")

				// Calculate processing time
				endTime := time.Now()

				summary := map[string]interface{}{
					"org_id":            orgID,
					"org_name":          orgName,
					"processing_time":   endTime.Format("2006-01-02 15:04:05"),
					"total_processed":   processingResult["total_processed"],
					"total_evaluations": processingResult["total_evaluations"],
					"total_citations":   processingResult["total_citations"],
					"total_competitors": processingResult["total_competitors"],
					"total_cost":        processingResult["total_cost"],
					"processing_errors": processingResult["processing_errors"],
					"pipeline_version":  "org_reeval_v1.0",
					"status":            "completed",
				}

				fmt.Printf("[ProcessOrgReeval] 🎉 Org re-evaluation pipeline completed successfully for org: %s\n", orgName)
				fmt.Printf("[ProcessOrgReeval] 📊 Summary: %d evaluations, %d citations, %d competitors processed\n",
					processingResult["total_evaluations"], processingResult["total_citations"], processingResult["total_competitors"])

				return summary, nil
			})
			if err != nil {
				return nil, fmt.Errorf("generate summary step failed: %w", err)
			}

			return finalResult, nil
		},
	)

	if err != nil {
		panic(fmt.Sprintf("Failed to create ProcessOrgReeval function: %v", err))
	}

	return fn
}
