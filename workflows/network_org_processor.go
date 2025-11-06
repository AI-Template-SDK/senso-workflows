// workflows/network_org_processor.go
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

type NetworkOrgProcessor struct {
	questionRunnerService services.QuestionRunnerService
	client                inngestgo.Client
	cfg                   *config.Config
}

func NewNetworkOrgProcessor(
	questionRunnerService services.QuestionRunnerService,
	cfg *config.Config,
) *NetworkOrgProcessor {
	return &NetworkOrgProcessor{
		questionRunnerService: questionRunnerService,
		cfg:                   cfg,
	}
}

func (p *NetworkOrgProcessor) SetClient(client inngestgo.Client) {
	p.client = client
}

func (p *NetworkOrgProcessor) ProcessNetworkOrg() inngestgo.ServableFunction {
	fn, err := inngestgo.CreateFunction(
		p.client,
		inngestgo.FunctionOpts{
			ID:      "process-network-org",
			Name:    "Process Network Org Data Extraction",
			Retries: inngestgo.IntPtr(3),
		},
		inngestgo.EventTrigger("network.org.process", nil),
		func(ctx context.Context, input inngestgo.Input[NetworkOrgProcessEvent]) (any, error) {
			orgID := input.Event.Data.OrgID
			fmt.Printf("[ProcessNetworkOrg] Starting network org processing for org: %s\n", orgID)

			// Step 1: Fetch org details and network
			orgDetailsResult, err := step.Run(ctx, "fetch-org-details", func(ctx context.Context) (interface{}, error) {
				fmt.Printf("[ProcessNetworkOrg] Step 1: Fetching org details and network for org: %s\n", orgID)
				orgDetails, err := p.questionRunnerService.GetOrgDetailsForNetworkProcessing(ctx, orgID)
				if err != nil {
					return nil, fmt.Errorf("failed to fetch org details: %w", err)
				}

				fmt.Printf("[ProcessNetworkOrg] Found org: %s in network: %s\n", orgDetails.OrgName, orgDetails.NetworkID)
				return map[string]interface{}{
					"org_name":     orgDetails.OrgName,
					"network_id":   orgDetails.NetworkID,
					"org_websites": orgDetails.Websites,
				}, nil
			})
			if err != nil {
				return nil, fmt.Errorf("step 1 failed: %w", err)
			}

			// Step 2: Get latest network question runs
			questionRunsResult, err := step.Run(ctx, "fetch-network-question-runs", func(ctx context.Context) (interface{}, error) {
				fmt.Printf("[ProcessNetworkOrg] Step 2: Fetching latest network question runs\n")
				orgDetailsData := orgDetailsResult.(map[string]interface{})
				networkID := orgDetailsData["network_id"].(string)

				questionRuns, err := p.questionRunnerService.GetLatestNetworkQuestionRuns(ctx, networkID)
				if err != nil {
					return nil, fmt.Errorf("failed to fetch network question runs: %w", err)
				}

				fmt.Printf("[ProcessNetworkOrg] Found %d latest network question runs\n", len(questionRuns))
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

			// Step 2.5: Generate name variations ONCE for this org (before processing question runs)
			nameVariationsResult, err := step.Run(ctx, "generate-name-variations", func(ctx context.Context) (interface{}, error) {
				fmt.Printf("[ProcessNetworkOrg] Step 2.5: Generating name variations for org: %s\n", orgName)
				variations, err := p.questionRunnerService.GenerateOrgNameVariations(ctx, orgName, websites)
				if err != nil {
					return nil, fmt.Errorf("failed to generate name variations: %w", err)
				}
				fmt.Printf("[ProcessNetworkOrg] ✅ Generated %d name variations\n", len(variations))
				return variations, nil
			})
			if err != nil {
				return nil, fmt.Errorf("step 2.5 failed: %w", err)
			}
			nameVariations := nameVariationsResult.([]interface{})

			// Convert interface{} slice to []string
			nameVariationsStr := make([]string, len(nameVariations))
			for i, v := range nameVariations {
				nameVariationsStr[i] = v.(string)
			}

			// Step 3: Process each question run individually (with pre-generated name variations)
			var allResults []interface{}
			for i, questionRunInterface := range questionRuns {
				questionRun := questionRunInterface.(map[string]interface{})
				questionRunID := questionRun["question_run_id"].(string)
				questionText := questionRun["question_text"].(string)
				responseText := questionRun["response_text"].(string)
				questionIndex := i + 1
				stepName := fmt.Sprintf("process-question-run-%d", questionIndex)

				_, err := step.Run(ctx, stepName, func(ctx context.Context) (interface{}, error) {
					fmt.Printf("[ProcessNetworkOrg] Step %d: Processing question run %d/%d: %s\n",
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

					// Extract network org data (with cleanup to prevent duplicates and pre-generated name variations)
					result, err := p.questionRunnerService.ProcessNetworkOrgQuestionRunWithCleanup(ctx, questionRunUUID, orgUUID, orgName, websites, nameVariationsStr, questionText, responseText)
					if err != nil {
						return nil, fmt.Errorf("failed to process question run %s: %w", questionRunID, err)
					}

					fmt.Printf("[ProcessNetworkOrg] Successfully processed question run %d/%d: %s\n",
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
					fmt.Printf("[ProcessNetworkOrg] Warning: Failed to process question run %d/%d: %v\n",
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
				"pipeline":                "network_org_processing",
				"question_runs_processed": questionCount,
				"completed_at":            time.Now().UTC(),
			}

			fmt.Printf("[ProcessNetworkOrg] ✅ COMPLETED: Network org processing for org %s\n", orgID)
			fmt.Printf("[ProcessNetworkOrg] 📊 Data stored: network_org_evals, network_org_competitors, network_org_citations\n")

			return finalResult, nil
		},
	)
	if err != nil {
		panic(fmt.Errorf("failed to create ProcessNetworkOrg function: %w", err))
	}
	return fn
}

// Event types
type NetworkOrgProcessEvent struct {
	OrgID       string `json:"org_id"`
	TriggeredBy string `json:"triggered_by"`
	UserID      string `json:"user_id,omitempty"`
}
