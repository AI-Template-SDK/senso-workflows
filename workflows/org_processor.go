// workflows/org_processor.go
package workflows

import (
	"context"
	"fmt"
	"time"

	"github.com/inngest/inngestgo"
	"github.com/inngest/inngestgo/step"

	"github.com/AI-Template-SDK/senso-workflows/internal/config"
	"github.com/AI-Template-SDK/senso-workflows/services"
	"github.com/google/uuid"
)

type OrgProcessor struct {
	orgService            services.OrgService
	analyticsService      services.AnalyticsService
	questionRunnerService services.QuestionRunnerService
	client                inngestgo.Client
	cfg                   *config.Config
}

func NewOrgProcessor(
	orgService services.OrgService,
	analyticsService services.AnalyticsService,
	questionRunnerService services.QuestionRunnerService,
	cfg *config.Config,
) *OrgProcessor {
	return &OrgProcessor{
		orgService:            orgService,
		analyticsService:      analyticsService,
		questionRunnerService: questionRunnerService,
		cfg:                   cfg,
	}
}

func (p *OrgProcessor) SetClient(client inngestgo.Client) {
	p.client = client
}

func (p *OrgProcessor) ProcessOrg() inngestgo.ServableFunction {
	fn, err := inngestgo.CreateFunction(
		p.client,
		inngestgo.FunctionOpts{
			ID:      "process-org",
			Name:    "Process Organization - Full Competitive Intelligence Pipeline",
			Retries: inngestgo.IntPtr(3),
		},
		inngestgo.EventTrigger("org.process", nil),
		func(ctx context.Context, input inngestgo.Input[OrgProcessEvent]) (any, error) {
			orgID := input.Event.Data.OrgID
			fmt.Printf("[ProcessOrg] Starting full competitive intelligence pipeline for org: %s\n", orgID)

			// Step 1: Get Real Org Data from Database
			orgDetails, err := step.Run(ctx, "get-real-org-data", func(ctx context.Context) (*services.RealOrgDetails, error) {
				fmt.Printf("[ProcessOrg] Step 1: Fetching real org data from database\n")
				details, err := p.orgService.GetOrgDetails(ctx, orgID)
				if err != nil {
					return nil, fmt.Errorf("failed to get org details: %w", err)
				}

				fmt.Printf("[ProcessOrg] Successfully loaded org: %s with %d models, %d locations, %d questions\n",
					details.Org.Name, len(details.Models), len(details.Locations), len(details.Questions))
				return details, nil
			})
			if err != nil {
				return nil, fmt.Errorf("step 1 failed: %w", err)
			}

			// Step 2: Execute Question Matrix & Store Question Runs
			questionRuns, err := step.Run(ctx, "execute-and-store-question-matrix", func(ctx context.Context) (interface{}, error) {
				fmt.Printf("[ProcessOrg] Step 2: Executing AI calls and storing question runs\n")
				runs, err := p.questionRunnerService.RunQuestionMatrix(ctx, orgDetails)
				if err != nil {
					return nil, fmt.Errorf("failed to run question matrix: %w", err)
				}

				fmt.Printf("[ProcessOrg] Successfully processed %d question runs with full data extraction\n", len(runs))
				return map[string]interface{}{
					"total_runs":      len(runs),
					"org_name":        orgDetails.Org.Name,
					"target_company":  orgDetails.TargetCompany,
					"questions_count": len(orgDetails.Questions),
					"models_count":    len(orgDetails.Models),
					"locations_count": len(orgDetails.Locations),
				}, nil
			})
			if err != nil {
				return nil, fmt.Errorf("step 2 failed: %w", err)
			}

			// Step 3: Generate Real Database Analytics
			analytics, err := step.Run(ctx, "generate-database-analytics", func(ctx context.Context) (interface{}, error) {
				fmt.Printf("[ProcessOrg] Step 3: Generating analytics from database\n")

				// Use a date range for analytics (last 30 days)
				endDate := time.Now()
				startDate := endDate.AddDate(0, 0, -30)

				orgUUID, err := uuid.Parse(orgID)
				if err != nil {
					return nil, fmt.Errorf("invalid org ID: %w", err)
				}

				analytics, err := p.analyticsService.CalculateAnalytics(ctx, orgUUID, startDate, endDate)
				if err != nil {
					return nil, fmt.Errorf("failed to calculate analytics: %w", err)
				}

				fmt.Printf("[ProcessOrg] Successfully generated analytics with %d metrics and %d insights\n",
					len(analytics.Metrics), len(analytics.Insights))
				return analytics, nil
			})
			if err != nil {
				return nil, fmt.Errorf("step 3 failed: %w", err)
			}

			// Step 4: Push Analytics Results
			pushResult, err := step.Run(ctx, "push-analytics-results", func(ctx context.Context) (interface{}, error) {
				fmt.Printf("[ProcessOrg] Step 4: Pushing analytics results\n")

				result, err := p.analyticsService.PushAnalytics(ctx, orgID, nil) // TODO: Fix this cast
				if err != nil {
					return nil, fmt.Errorf("failed to push analytics: %w", err)
				}

				fmt.Printf("[ProcessOrg] Successfully pushed analytics: %s\n", result.Message)
				return result, nil
			})
			if err != nil {
				return nil, fmt.Errorf("step 4 failed: %w", err)
			}

			// Final Result Summary
			finalResult := map[string]interface{}{
				"org_id":             orgID,
				"org_name":           orgDetails.Org.Name,
				"status":             "completed",
				"pipeline":           "full_competitive_intelligence",
				"question_execution": questionRuns,
				"analytics":          analytics,
				"push_result":        pushResult,
				"completed_at":       time.Now().UTC(),
			}

			fmt.Printf("[ProcessOrg] ✅ COMPLETED: Full competitive intelligence pipeline for org %s\n", orgID)
			fmt.Printf("[ProcessOrg] 📊 Data stored: question_runs, mentions, claims, citations, analytics\n")
			fmt.Printf("[ProcessOrg] 🎯 Target company: %s\n", orgDetails.TargetCompany)

			return finalResult, nil
		},
	)
	if err != nil {
		panic(fmt.Errorf("failed to create ProcessOrg function: %w", err))
	}
	return fn
}

// Event types
type OrgProcessEvent struct {
	OrgID         string `json:"org_id"`
	TriggeredBy   string `json:"triggered_by"`
	UserID        string `json:"user_id,omitempty"`
	ScheduledDate string `json:"scheduled_date,omitempty"`
}
