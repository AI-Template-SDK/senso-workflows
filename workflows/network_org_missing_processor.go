// workflows/network_org_missing_processor.go
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

type NetworkOrgMissingProcessor struct {
	questionRunnerService services.QuestionRunnerService
	client                inngestgo.Client
	cfg                   *config.Config
}

func NewNetworkOrgMissingProcessor(
	questionRunnerService services.QuestionRunnerService,
	cfg *config.Config,
) *NetworkOrgMissingProcessor {
	return &NetworkOrgMissingProcessor{
		questionRunnerService: questionRunnerService,
		cfg:                   cfg,
	}
}

func (p *NetworkOrgMissingProcessor) SetClient(client inngestgo.Client) {
	p.client = client
}

func (p *NetworkOrgMissingProcessor) ProcessNetworkOrgMissing() inngestgo.ServableFunction {
	fn, err := inngestgo.CreateFunction(
		p.client,
		inngestgo.FunctionOpts{
			ID:      "process-network-org-missing",
			Name:    "Process Network Org Missing Evaluations",
			Retries: inngestgo.IntPtr(3),
		},
		inngestgo.EventTrigger("network.org.missing.process", nil),
		func(ctx context.Context, input inngestgo.Input[NetworkOrgMissingProcessEvent]) (any, error) {
			orgID := input.Event.Data.OrgID
			fmt.Printf("[ProcessNetworkOrgMissing] Starting network org missing evaluation processing for org: %s\n", orgID)

			// Step 1: Fetch org details and network
			orgDetailsResult, err := step.Run(ctx, "fetch-org-details", func(ctx context.Context) (interface{}, error) {
				fmt.Printf("[ProcessNetworkOrgMissing] Step 1: Fetching org details and network for org: %s\n", orgID)
				orgDetails, err := p.questionRunnerService.GetOrgDetailsForNetworkProcessing(ctx, orgID)
				if err != nil {
					return nil, fmt.Errorf("failed to fetch org details: %w", err)
				}

				fmt.Printf("[ProcessNetworkOrgMissing] Found org: %s in network: %s\n", orgDetails.OrgName, orgDetails.NetworkID)
				return map[string]interface{}{
					"org_name":     orgDetails.OrgName,
					"network_id":   orgDetails.NetworkID,
					"org_websites": orgDetails.Websites,
				}, nil
			})
			if err != nil {
				return nil, fmt.Errorf("step 1 failed: %w", err)
			}

			// Step 2: Get question runs missing network_org_eval records
			questionRunsResult, err := step.Run(ctx, "fetch-missing-question-runs", func(ctx context.Context) (interface{}, error) {
				fmt.Printf("[ProcessNetworkOrgMissing] Step 2: Fetching question runs missing evaluations\n")
				orgDetailsData := orgDetailsResult.(map[string]interface{})
				networkID := orgDetailsData["network_id"].(string)

				questionRuns, err := p.questionRunnerService.GetMissingNetworkOrgQuestionRuns(ctx, networkID, orgID)
				if err != nil {
					return nil, fmt.Errorf("failed to fetch missing question runs: %w", err)
				}

				fmt.Printf("[ProcessNetworkOrgMissing] Found %d question runs missing evaluations\n", len(questionRuns))
				return map[string]interface{}{
					"question_runs": questionRuns,
					"count":         len(questionRuns),
				}, nil
			})
			if err != nil {
				return nil, fmt.Errorf("step 2 failed: %w", err)
			}

			// Extract data from results
			orgDetailsData := orgDetailsResult.(map[string]interface{})
			questionRunsData := questionRunsResult.(map[string]interface{})
			questionCount := int(questionRunsData["count"].(float64))
			orgName := orgDetailsData["org_name"].(string)
			networkID := orgDetailsData["network_id"].(string)
			orgWebsites := orgDetailsData["org_websites"].([]interface{})

			// Convert orgWebsites to string slice
			websites := make([]string, len(orgWebsites))
			for i, website := range orgWebsites {
				websites[i] = website.(string)
			}

			// If no missing evaluations, return early
			if questionCount == 0 {
				fmt.Printf("[ProcessNetworkOrgMissing] ✅ No missing evaluations found for org %s\n", orgID)
				return map[string]interface{}{
					"org_id":                  orgID,
					"network_id":              networkID,
					"org_name":                orgName,
					"status":                  "completed",
					"pipeline":                "network_org_missing_processing",
					"question_runs_processed": 0,
					"message":                 "No missing evaluations to process",
					"completed_at":            time.Now().UTC(),
				}, nil
			}

			// Extract question runs (only after we know there are results)
			questionRuns := questionRunsData["question_runs"].([]interface{})

			// Step 3: Process each question run individually
			var allResults []interface{}
			totalCost := 0.0
			totalCompetitors := 0
			totalCitations := 0

			for i, questionRunInterface := range questionRuns {
				questionRun := questionRunInterface.(map[string]interface{})
				questionRunID := questionRun["question_run_id"].(string)
				questionText := questionRun["question_text"].(string)
				responseText := questionRun["response_text"].(string)
				questionIndex := i + 1
				stepName := fmt.Sprintf("process-question-run-%d", questionIndex)

				stepResult, err := step.Run(ctx, stepName, func(ctx context.Context) (interface{}, error) {
					fmt.Printf("[ProcessNetworkOrgMissing] Step %d: Processing question run %d/%d: %s\n",
						questionIndex+2, questionIndex, questionCount, questionRunID)

					// Parse UUIDs
					questionRunUUID, err := uuid.Parse(questionRunID)
					if err != nil {
						return nil, fmt.Errorf("invalid question run ID format: %w", err)
					}
					orgUUID, err := uuid.Parse(orgID)
					if err != nil {
						return nil, fmt.Errorf("invalid org ID format: %w", err)
					}

					// Extract network org data (with cleanup to prevent duplicates)
					result, err := p.questionRunnerService.ProcessNetworkOrgQuestionRunWithCleanup(ctx, questionRunUUID, orgUUID, orgName, websites, questionText, responseText)
					if err != nil {
						return nil, fmt.Errorf("failed to process question run %s: %w", questionRunID, err)
					}

					fmt.Printf("[ProcessNetworkOrgMissing] Successfully processed question run %d/%d: %s (cost: $%.6f)\n",
						questionIndex, questionCount, questionRunID, result.TotalCost)

					return map[string]interface{}{
						"question_run_id": questionRunID,
						"evaluation_id":   result.Evaluation.NetworkOrgEvalID,
						"competitors":     len(result.Competitors),
						"citations":       len(result.Citations),
						"total_cost":      result.TotalCost,
						"status":          "completed",
					}, nil
				})
				if err != nil {
					fmt.Printf("[ProcessNetworkOrgMissing] Warning: Failed to process question run %d/%d: %v\n",
						questionIndex, questionCount, err)
					continue
				}

				// Extract step result data and accumulate costs
				if stepResultMap, ok := stepResult.(map[string]interface{}); ok {
					if cost, ok := stepResultMap["total_cost"].(float64); ok {
						totalCost += cost
					}
					if competitors, ok := stepResultMap["competitors"].(int); ok {
						totalCompetitors += competitors
					}
					if citations, ok := stepResultMap["citations"].(int); ok {
						totalCitations += citations
					}
				}

				// Track that this question run was processed
				allResults = append(allResults, map[string]interface{}{
					"question_run_id": questionRunID,
					"status":          "processed",
				})
			}

			// SEBASTIAN HERE
			// TODO: Determine how many questions we run in this batch for this org/networkthen create a usage record for each question run and tie the metadata to the
			// specific question run. Ensure idempotency of the usage record creation so we don't create duplicate usage records.

			// Final Result Summary
			finalResult := map[string]interface{}{
				"org_id":                  orgID,
				"network_id":              networkID,
				"org_name":                orgName,
				"status":                  "completed",
				"pipeline":                "network_org_missing_processing",
				"question_runs_processed": questionCount,
				"total_competitors":       totalCompetitors,
				"total_citations":         totalCitations,
				"total_cost":              totalCost,
				"completed_at":            time.Now().UTC(),
			}

			fmt.Printf("[ProcessNetworkOrgMissing] ✅ COMPLETED: Network org missing evaluation processing for org %s\n", orgID)
			fmt.Printf("[ProcessNetworkOrgMissing] 📊 Data stored: %d missing evaluations processed\n", questionCount)
			fmt.Printf("[ProcessNetworkOrgMissing] 📊 Extractions: %d competitors, %d citations\n", totalCompetitors, totalCitations)
			fmt.Printf("[ProcessNetworkOrgMissing] 💰 Total cost: $%.6f\n", totalCost)
			fmt.Printf("[ProcessNetworkOrgMissing] 📊 Tables updated: network_org_evals, network_org_competitors, network_org_citations\n")

			return finalResult, nil
		},
	)
	if err != nil {
		panic(fmt.Errorf("failed to create ProcessNetworkOrgMissing function: %w", err))
	}
	return fn
}

// Event types
type NetworkOrgMissingProcessEvent struct {
	OrgID       string `json:"org_id"`
	TriggeredBy string `json:"triggered_by"`
	UserID      string `json:"user_id,omitempty"`
}
