// workflows/network_org_reeval_processor.go
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

// NetworkOrgReevalProcessor handles network org re-evaluation workflows using org evaluation methodology
type NetworkOrgReevalProcessor struct {
	client                inngestgo.Client
	orgService            services.OrgService
	orgEvaluationService  services.OrgEvaluationService
	questionRunnerService services.QuestionRunnerService
	cfg                   *config.Config
}

// NewNetworkOrgReevalProcessor creates a new network org re-evaluation processor
func NewNetworkOrgReevalProcessor(cfg *config.Config, orgService services.OrgService, orgEvaluationService services.OrgEvaluationService, questionRunnerService services.QuestionRunnerService) *NetworkOrgReevalProcessor {
	return &NetworkOrgReevalProcessor{
		orgService:            orgService,
		orgEvaluationService:  orgEvaluationService,
		questionRunnerService: questionRunnerService,
		cfg:                   cfg,
	}
}

// SetClient sets the Inngest client for this processor
func (p *NetworkOrgReevalProcessor) SetClient(client inngestgo.Client) {
	p.client = client
}

// NetworkOrgReevalProcessEvent represents the event data for network org re-evaluation processing
type NetworkOrgReevalProcessEvent struct {
	OrgID       string `json:"org_id"`
	TriggeredBy string `json:"triggered_by,omitempty"`
	UserID      string `json:"user_id,omitempty"`
}

func (p *NetworkOrgReevalProcessor) ProcessNetworkOrgReeval() inngestgo.ServableFunction {
	fn, err := inngestgo.CreateFunction(
		p.client,
		inngestgo.FunctionOpts{
			ID:      "process-network-org-reeval-enhanced",
			Name:    "Process Network Org Re-evaluation - Enhanced with Org Evaluation Methodology",
			Retries: inngestgo.IntPtr(3),
		},
		inngestgo.EventTrigger("network.org.reeval.enhanced", nil),
		func(ctx context.Context, input inngestgo.Input[NetworkOrgReevalProcessEvent]) (any, error) {
			orgID := input.Event.Data.OrgID
			fmt.Printf("[ProcessNetworkOrgReevalEnhanced] Starting enhanced network org re-evaluation for org: %s\n", orgID)

			// Step 1: Fetch org details and network
			orgDetailsResult, err := step.Run(ctx, "fetch-org-details", func(ctx context.Context) (interface{}, error) {
				fmt.Printf("[ProcessNetworkOrgReevalEnhanced] Step 1: Fetching org details and network for org: %s\n", orgID)
				orgDetails, err := p.questionRunnerService.GetOrgDetailsForNetworkProcessing(ctx, orgID)
				if err != nil {
					return nil, fmt.Errorf("failed to fetch org details: %w", err)
				}

				fmt.Printf("[ProcessNetworkOrgReevalEnhanced] Found org: %s in network: %s\n", orgDetails.OrgName, orgDetails.NetworkID)
				return map[string]interface{}{
					"org_name":     orgDetails.OrgName,
					"network_id":   orgDetails.NetworkID,
					"org_websites": orgDetails.Websites,
				}, nil
			})
			if err != nil {
				return nil, fmt.Errorf("step 1 failed: %w", err)
			}

			orgDetailsData := orgDetailsResult.(map[string]interface{})
			orgName := orgDetailsData["org_name"].(string)
			networkID := orgDetailsData["network_id"].(string)

			// Convert []interface{} to []string for websites
			websitesInterface := orgDetailsData["org_websites"].([]interface{})
			websites := make([]string, len(websitesInterface))
			for i, v := range websitesInterface {
				websites[i] = v.(string)
			}

			// Step 2: Generate Name Variations (FROM ORG REEVAL METHODOLOGY)
			nameVariationsResult, err := step.Run(ctx, "generate-name-variations", func(ctx context.Context) (interface{}, error) {
				fmt.Printf("[ProcessNetworkOrgReevalEnhanced] Step 2: Generating name variations for org: %s\n", orgName)

				// Generate name variations once for the entire org using org evaluation methodology
				nameVariations, err := p.orgEvaluationService.GenerateNameVariations(ctx, orgName, websites)
				if err != nil {
					return nil, fmt.Errorf("failed to generate name variations: %w", err)
				}

				fmt.Printf("[ProcessNetworkOrgReevalEnhanced] ✅ Generated %d name variations\n", len(nameVariations))
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

			// Step 3: Fetch ALL network question runs
			questionRunsResult, err := step.Run(ctx, "fetch-all-network-question-runs", func(ctx context.Context) (interface{}, error) {
				fmt.Printf("[ProcessNetworkOrgReevalEnhanced] Step 3: Fetching ALL network question runs for network: %s\n", networkID)

				questionRuns, err := p.questionRunnerService.GetAllNetworkQuestionRuns(ctx, networkID)
				if err != nil {
					return nil, fmt.Errorf("failed to fetch all network question runs: %w", err)
				}

				fmt.Printf("[ProcessNetworkOrgReevalEnhanced] ✅ Found %d total network question runs to re-evaluate\n", len(questionRuns))
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

			// Steps 4-N: Process Each Question Run Individually with Enhanced Methodology
			var allResults []interface{}
			for i, questionRunInterface := range questionRuns {
				questionRunData := questionRunInterface.(map[string]interface{})
				runIndex := i + 1
				stepName := fmt.Sprintf("process-question-run-%d", runIndex)

				result, err := step.Run(ctx, stepName, func(ctx context.Context) (interface{}, error) {
					fmt.Printf("[ProcessNetworkOrgReevalEnhanced] Step %d: Processing question run %d/%d with enhanced methodology\n", runIndex+3, runIndex, totalRuns)

					// Parse question run data
					questionRunID, _ := uuid.Parse(questionRunData["question_run_id"].(string))
					questionText := questionRunData["question_text"].(string)
					responseText := questionRunData["response_text"].(string)

					// Parse org ID
					orgUUID, err := uuid.Parse(orgID)
					if err != nil {
						return nil, fmt.Errorf("invalid org ID: %w", err)
					}

					// Process the question run re-evaluation using enhanced org evaluation methodology
					result, err := p.orgEvaluationService.ProcessNetworkOrgQuestionRunReeval(ctx, questionRunID, orgUUID, orgName, websites, nameVariations, questionText, responseText)
					if err != nil {
						return nil, fmt.Errorf("failed to process network org question run re-evaluation: %w", err)
					}

					fmt.Printf("[ProcessNetworkOrgReevalEnhanced] ✅ Completed question run %d/%d: %s\n", runIndex, totalRuns, result.Status)

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
					fmt.Printf("[ProcessNetworkOrgReevalEnhanced] Warning: Failed to process question run %d/%d: %v\n", runIndex, totalRuns, err)
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
				fmt.Printf("[ProcessNetworkOrgReevalEnhanced] Step N+1: Generating processing summary\n")

				// Calculate processing time
				endTime := time.Now()

				summary := map[string]interface{}{
					"org_id":            orgID,
					"org_name":          orgName,
					"network_id":        networkID,
					"processing_time":   endTime.Format("2006-01-02 15:04:05"),
					"total_processed":   processingResult["total_processed"],
					"total_evaluations": processingResult["total_evaluations"],
					"total_citations":   processingResult["total_citations"],
					"total_competitors": processingResult["total_competitors"],
					"total_cost":        processingResult["total_cost"],
					"processing_errors": processingResult["processing_errors"],
					"pipeline_version":  "network_org_reeval_enhanced_v1.0",
					"methodology":       "org_evaluation_enhanced",
					"status":            "completed",
				}

				fmt.Printf("[ProcessNetworkOrgReevalEnhanced] 🎉 Enhanced network org re-evaluation pipeline completed successfully for org: %s\n", orgName)
				fmt.Printf("[ProcessNetworkOrgReevalEnhanced] 📊 Summary: %d evaluations, %d citations, %d competitors processed using enhanced methodology\n",
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
		panic(fmt.Sprintf("Failed to create ProcessNetworkOrgReevalEnhanced function: %v", err))
	}

	return fn
}
