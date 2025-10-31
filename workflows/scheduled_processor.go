// workflows/scheduled_processor.go
package workflows

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/inngest/inngestgo"
	"github.com/inngest/inngestgo/step"

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

			// Tom's logic: "if Monday is zero"
			// Go's logic: Sunday=0, Monday=1, ... Saturday=6
			// Conversion: (time.Now().Weekday() + 6) % 7
			now := time.Now()
			dayOfWeek := (now.Weekday() + 6) % 7

			// Step 1: Get organizations scheduled for this day of the week
			orgIDs, err := step.Run(ctx, "get-scheduled-orgs", func(ctx context.Context) ([]uuid.UUID, error) {
				return p.orgService.GetOrgIDsByScheduledDOW(ctx, int(dayOfWeek))
			})
			if err != nil {
				return nil, fmt.Errorf("failed to get scheduled orgs for DOW %d: %w", dayOfWeek, err)
			}

			if len(orgIDs) == 0 {
				return map[string]interface{}{
					"execution_date":   now.Format("2006-01-02"),
					"weekday":          now.Weekday().String(),
					"dow_value":        dayOfWeek,
					"total_orgs_found": 0,
					"message":          fmt.Sprintf("No organizations scheduled for %s (DOW %d)", now.Weekday().String(), dayOfWeek),
				}, nil
			}

			// Step 2: Trigger dummy pipelines asynchronously for testing
			_, err = step.Run(ctx, "trigger-dummy-evaluations", func(ctx context.Context) (interface{}, error) {
				// Send events asynchronously - the scheduler does NOT wait for completion
				for _, orgID := range orgIDs {
					evt := inngestgo.Event{
						Name: "dummy.org.process", // USING DUMMY WORKFLOW FOR TESTING
						Data: map[string]interface{}{
							"org_id":       orgID.String(),
							"triggered_by": "automatic_scheduler",
						},
					}

					// Send the event (fire and forget)
					_, err := p.client.Send(ctx, evt)
					if err != nil {
						fmt.Printf("Warning: Failed to send event for org %s: %v\n", orgID.String(), err)
						// Continue processing other orgs even if one fails
					}
				}
				return map[string]interface{}{"events_sent": len(orgIDs)}, nil
			})
			if err != nil {
				return nil, fmt.Errorf("failed to trigger dummy evaluations: %w", err)
			}

			return map[string]interface{}{
				"execution_date":   now.Format("2006-01-02"),
				"weekday":          now.Weekday().String(),
				"dow_value":        dayOfWeek,
				"total_orgs_found": len(orgIDs),
				"orgs_processed":   orgIDs,
				"message":          fmt.Sprintf("Triggered %d dummy evaluation pipelines for %s (DOW %d)", len(orgIDs), now.Weekday().String(), dayOfWeek),
			}, nil
		},
	)

	if err != nil {
		fmt.Printf("Failed to create daily org processor function: %v\n", err)
	}

	return fn
}

// Helper function to calculate weeks since creation
func weeksSince(createdAt, now time.Time) int {
	duration := now.Sub(createdAt)
	return int(duration.Hours() / 24 / 7)
}
