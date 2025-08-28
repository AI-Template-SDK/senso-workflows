// workflows/network_reeval_processor.go
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

type NetworkReevalProcessor struct {
	questionRunnerService services.QuestionRunnerService
	client                inngestgo.Client
	cfg                   *config.Config
}

func NewNetworkReevalProcessor(
	questionRunnerService services.QuestionRunnerService,
	cfg *config.Config,
) *NetworkReevalProcessor {
	return &NetworkReevalProcessor{
		questionRunnerService: questionRunnerService,
		cfg:                   cfg,
	}
}

func (p *NetworkReevalProcessor) SetClient(client inngestgo.Client) {
	p.client = client
}

func (p *NetworkReevalProcessor) ProcessNetworkReeval() inngestgo.ServableFunction {
	fn, err := inngestgo.CreateFunction(
		p.client,
		inngestgo.FunctionOpts{
			ID:      "process-network-reeval",
			Name:    "Process Network Org Data Re-evaluation - All Question Runs",
			Retries: inngestgo.IntPtr(3),
		},
		inngestgo.EventTrigger("network.org.reeval", nil),
		func(ctx context.Context, input inngestgo.Input[NetworkReevalProcessEvent]) (any, error) {
			orgID := input.Event.Data.OrgID
			fmt.Printf("[ProcessNetworkReeval] Starting network org re-evaluation for org: %s\n", orgID)

			// Step 1: Fetch org details and network
			orgDetailsResult, err := step.Run(ctx, "fetch-org-details", func(ctx context.Context) (interface{}, error) {
				fmt.Printf("[ProcessNetworkReeval] Step 1: Fetching org details and network for org: %s\n", orgID)
				orgDetails, err := p.questionRunnerService.GetOrgDetailsForNetworkProcessing(ctx, orgID)
				if err != nil {
					return nil, fmt.Errorf("failed to fetch org details: %w", err)
				}

				fmt.Printf("[ProcessNetworkReeval] Found org: %s in network: %s\n", orgDetails.OrgName, orgDetails.NetworkID)
				return map[string]interface{}{
					"org_name":     orgDetails.OrgName,
					"network_id":   orgDetails.NetworkID,
					"org_websites": orgDetails.Websites,
				}, nil
			})
			if err != nil {
				return nil, fmt.Errorf("step 1 failed: %w", err)
			}

			// Step 2: Get ALL network question runs (not just latest)
			questionRunsResult, err := step.Run(ctx, "fetch-all-network-question-runs", func(ctx context.Context) (interface{}, error) {
				fmt.Printf("[ProcessNetworkReeval] Step 2: Fetching ALL network question runs (not just latest)\n")
				orgDetailsData := orgDetailsResult.(map[string]interface{})
				networkID := orgDetailsData["network_id"].(string)

				questionRuns, err := p.questionRunnerService.GetAllNetworkQuestionRuns(ctx, networkID)
				if err != nil {
					return nil, fmt.Errorf("failed to fetch all network question runs: %w", err)
				}

				fmt.Printf("[ProcessNetworkReeval] Found %d total network question runs\n", len(questionRuns))
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
			questionRuns := questionRunsData["question_runs"].([]interface{})
			questionCount := int(questionRunsData["count"].(float64))
			orgName := orgDetailsData["org_name"].(string)
			networkID := orgDetailsData["network_id"].(string)
			orgWebsites := orgDetailsData["org_websites"].([]interface{})

			// Convert orgWebsites to string slice
			websites := make([]string, len(orgWebsites))
			for i, website := range orgWebsites {
				websites[i] = website.(string)
			}

			// Step 3: Process each question run individually with cleanup
			var allResults []interface{}
			for i, questionRunInterface := range questionRuns {
				questionRun := questionRunInterface.(map[string]interface{})
				questionRunID := questionRun["question_run_id"].(string)
				questionText := questionRun["question_text"].(string)
				responseText := questionRun["response_text"].(string)
				questionIndex := i + 1
				stepName := fmt.Sprintf("process-question-run-%d", questionIndex)

				_, err := step.Run(ctx, stepName, func(ctx context.Context) (interface{}, error) {
					fmt.Printf("[ProcessNetworkReeval] Step %d: Processing question run %d/%d: %s\n",
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

					// Process with cleanup - delete existing data before saving new
					result, err := p.questionRunnerService.ProcessNetworkOrgQuestionRunWithCleanup(ctx, questionRunUUID, orgUUID, orgName, websites, questionText, responseText)
					if err != nil {
						return nil, fmt.Errorf("failed to process question run %s: %w", questionRunID, err)
					}

					fmt.Printf("[ProcessNetworkReeval] Successfully processed question run %d/%d: %s\n",
						questionIndex, questionCount, questionRunID)

					return map[string]interface{}{
						"question_run_id": questionRunID,
						"evaluation_id":   result.Evaluation.NetworkOrgEvalID,
						"competitors":     len(result.Competitors),
						"citations":       len(result.Citations),
						"status":          "completed",
					}, nil
				})
				if err != nil {
					fmt.Printf("[ProcessNetworkReeval] Warning: Failed to process question run %d/%d: %v\n",
						questionIndex, questionCount, err)
					continue
				}

				// Track that this question run was processed
				allResults = append(allResults, map[string]interface{}{
					"question_run_id": questionRunID,
					"status":          "processed",
				})
			}

			// Final Result Summary
			finalResult := map[string]interface{}{
				"org_id":                  orgID,
				"network_id":              networkID,
				"org_name":                orgName,
				"status":                  "completed",
				"pipeline":                "network_org_reeval",
				"question_runs_processed": questionCount,
				"completed_at":            time.Now().UTC(),
			}

			fmt.Printf("[ProcessNetworkReeval] ✅ COMPLETED: Network org re-evaluation for org %s\n", orgID)
			fmt.Printf("[ProcessNetworkReeval] 📊 Data re-evaluated: network_org_evals, network_org_competitors, network_org_citations\n")

			return finalResult, nil
		},
	)
	if err != nil {
		panic(fmt.Errorf("failed to create ProcessNetworkReeval function: %w", err))
	}
	return fn
}

// Event types
type NetworkReevalProcessEvent struct {
	OrgID       string `json:"org_id"`
	TriggeredBy string `json:"triggered_by"`
	UserID      string `json:"user_id,omitempty"`
}
