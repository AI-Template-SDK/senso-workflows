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
	repos      *services.RepositoryManager
	client     inngestgo.Client
}

func NewScheduledProcessor(orgService services.OrgService, repos *services.RepositoryManager) *ScheduledProcessor {
	return &ScheduledProcessor{
		orgService: orgService,
		repos:      repos,
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

			// Monday is zero
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

			// Step 2: Loop over each org and trigger an idempotent step-run for each.
			// This ensures if the workflow fails, it only retries sends that didn't complete.
			for _, orgID := range orgIDs {
				// Create a unique step name for each org
				stepName := fmt.Sprintf("trigger-org-eval-%s", orgID.String())

				// This step.Run is now *inside* the loop and is idempotent per-org
				_, err := step.Run(ctx, stepName, func(ctx context.Context) (interface{}, error) {
					evt := inngestgo.Event{
						Name: "org.evaluation.process",
						Data: map[string]interface{}{
							"org_id":       orgID.String(),
							"triggered_by": "automatic_scheduler",
						},
					}
					// Send the single event
					return p.client.Send(ctx, evt)
				})

				if err != nil {
					// Log the error but continue processing other orgs
					fmt.Printf("Warning: Failed to send event for org %s: %v\n", orgID.String(), err)
					// Do not return the error, to allow other orgs to process
				}
			}

			return map[string]interface{}{
				"execution_date":   now.Format("2006-01-02"),
				"weekday":          now.Weekday().String(),
				"dow_value":        dayOfWeek,
				"total_orgs_found": len(orgIDs),
				"orgs_processed":   orgIDs,
				"message":          fmt.Sprintf("Triggered %d org evaluation pipelines for %s (DOW %d)", len(orgIDs), now.Weekday().String(), dayOfWeek),
			}, nil
		},
	)

	if err != nil {
		fmt.Printf("Failed to create daily org processor function: %v\n", err)
	}

	return fn
}

func (p *ScheduledProcessor) DailyNetworkProcessor() inngestgo.ServableFunction {
	fn, err := inngestgo.CreateFunction(
		p.client,
		inngestgo.FunctionOpts{
			ID:   "daily-network-processor",
			Name: "Daily Network Processor - Weekly Cycle",
		},
		inngestgo.CronTrigger("0 3 * * *"), // Every day at 3 AM UTC (1 hour after org processor)
		func(ctx context.Context, input inngestgo.Input[any]) (any, error) {

			// Monday is zero
			// Go's logic: Sunday=0, Monday=1, ... Saturday=6
			// Conversion: (time.Now().Weekday() + 6) % 7
			now := time.Now()
			dayOfWeek := (now.Weekday() + 6) % 7

			// Step 1: Get networks scheduled for this day of the week
			networkIDs, err := step.Run(ctx, "get-scheduled-networks", func(ctx context.Context) ([]uuid.UUID, error) {
				return p.repos.NetworkScheduleRepo.GetNetworkIDsByDOW(ctx, int(dayOfWeek))
			})
			if err != nil {
				return nil, fmt.Errorf("failed to get scheduled networks for DOW %d: %w", dayOfWeek, err)
			}

			if len(networkIDs) == 0 {
				return map[string]interface{}{
					"execution_date":       now.Format("2006-01-02"),
					"weekday":              now.Weekday().String(),
					"dow_value":            dayOfWeek,
					"total_networks_found": 0,
					"message":              fmt.Sprintf("No networks scheduled for %s (DOW %d)", now.Weekday().String(), dayOfWeek),
				}, nil
			}

			// Step 2: Loop over each network and trigger an idempotent step-run for each.
			// This ensures if the workflow fails, it only retries sends that didn't complete.
			for _, networkID := range networkIDs {
				// Create a unique step name for each network
				stepName := fmt.Sprintf("trigger-network-eval-%s", networkID.String())

				// This step.Run is now *inside* the loop and is idempotent per-network
				_, err := step.Run(ctx, stepName, func(ctx context.Context) (interface{}, error) {
					evt := inngestgo.Event{
						Name: "network.questions.process",
						Data: map[string]interface{}{
							"network_id":   networkID.String(),
							"triggered_by": "automatic_scheduler",
						},
					}
					// Send the single event
					return p.client.Send(ctx, evt)
				})

				if err != nil {
					// Log the error but continue processing other networks
					fmt.Printf("Warning: Failed to send event for network %s: %v\n", networkID.String(), err)
					// Do not return the error, to allow other networks to process
				}
			}

			return map[string]interface{}{
				"execution_date":       now.Format("2006-01-02"),
				"weekday":              now.Weekday().String(),
				"dow_value":            dayOfWeek,
				"total_networks_found": len(networkIDs),
				"networks_processed":   networkIDs,
				"message":              fmt.Sprintf("Triggered %d network evaluation pipelines for %s (DOW %d)", len(networkIDs), now.Weekday().String(), dayOfWeek),
			}, nil
		},
	)

	if err != nil {
		fmt.Printf("Failed to create daily network processor function: %v\n", err)
	}

	return fn
}
