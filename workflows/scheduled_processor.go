// workflows/scheduled_processor.go
package workflows

import (
	"context"
	"fmt"
	"time"

	"github.com/inngest/inngestgo"
	"github.com/inngest/inngestgo/step"

	"github.com/AI-Template-SDK/senso-workflows/internal/models"
	"github.com/AI-Template-SDK/senso-workflows/services"
)

type ScheduledProcessor struct {
	orgService services.OrgService
	client     inngestgo.Client
}

func NewScheduledProcessor(orgService services.OrgService) *ScheduledProcessor {
	return &ScheduledProcessor{
		orgService: orgService,
	}
}

func (p *ScheduledProcessor) SetClient(client inngestgo.Client) {
	p.client = client
}

func (p *ScheduledProcessor) DailyOrgProcessor() inngestgo.ServableFunction {
	fn, err := inngestgo.CreateFunction(
		p.client,
		inngestgo.FunctionOpts{
			ID:   "daily-org-processor",
			Name: "Daily Organization Processor - Weekly Cycle",
		},
		inngestgo.CronTrigger("0 2 * * *"), // Every day at 2 AM UTC
		func(ctx context.Context, input inngestgo.Input[any]) (any, error) {
			now := time.Now()
			dayOfWeek := now.Weekday()

			// Step 1: Get organizations created on this day of the week
			orgs, err := step.Run(ctx, "get-orgs-by-weekday", func(ctx context.Context) ([]*models.OrgSummary, error) {
				return p.orgService.GetOrgsByCreationWeekday(ctx, dayOfWeek)
			})
			if err != nil {
				return nil, fmt.Errorf("failed to get orgs for %s: %w", dayOfWeek.String(), err)
			}

			// Step 2: Process results
			eventsSent := []string{}

			for _, org := range orgs {
				orgIDStr := org.ID.String()

				// Record which orgs should be processed
				// The actual triggering happens outside this function
				eventsSent = append(eventsSent, orgIDStr)
			}

			return map[string]interface{}{
				"execution_date":   now.Format("2006-01-02"),
				"weekday":          dayOfWeek.String(),
				"total_orgs_found": len(orgs),
				"orgs_to_process":  eventsSent,
				"message":          fmt.Sprintf("Found %d organizations to process for %s", len(orgs), dayOfWeek.String()),
			}, nil
		},
	)

	if err != nil {
		// Log error and return a no-op function
		fmt.Printf("Failed to create daily org processor function: %v\n", err)
	}

	return fn
}

// Helper function to calculate weeks since creation
func weeksSince(createdAt, now time.Time) int {
	duration := now.Sub(createdAt)
	return int(duration.Hours() / 24 / 7)
}
