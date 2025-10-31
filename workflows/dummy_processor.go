// workflows/dummy_processor.go
package workflows

import (
	"context"
	"fmt"

	"github.com/inngest/inngestgo"
	"github.com/inngest/inngestgo/step"
)

// DummyProcessor is for testing the scheduler
type DummyProcessor struct {
	client inngestgo.Client
}

// NewDummyProcessor creates a new dummy processor
func NewDummyProcessor() *DummyProcessor {
	return &DummyProcessor{}
}

// SetClient sets the inngest client
func (p *DummyProcessor) SetClient(client inngestgo.Client) {
	p.client = client
}

// DummyProcessEvent represents the input event for dummy processing
type DummyProcessEvent struct {
	OrgID       string `json:"org_id"`
	TriggeredBy string `json:"triggered_by,omitempty"`
}

// ProcessDummy is a test workflow that just logs its input
func (p *DummyProcessor) ProcessDummy() inngestgo.ServableFunction {
	fn, err := inngestgo.CreateFunction(
		p.client,
		inngestgo.FunctionOpts{
			ID:   "dummy-org-processor",
			Name: "Dummy Org Processor (Scheduler Test)",
		},
		inngestgo.EventTrigger("dummy.org.process", nil), // <-- This matches the scheduler
		func(ctx context.Context, input inngestgo.Input[DummyProcessEvent]) (any, error) {
			orgID := input.Event.Data.OrgID

			// This step just logs and finishes, per Tom's suggestion [cite: 73]
			summary, err := step.Run(ctx, "log-dummy-run", func(ctx context.Context) (string, error) {
				logMessage := fmt.Sprintf("Dummy workflow triggered successfully for org_id: %s", orgID)
				fmt.Println(logMessage)
				return logMessage, nil
			})

			if err != nil {
				return nil, err
			}

			return map[string]interface{}{"status": "success", "summary": summary}, nil
		},
	)

	if err != nil {
		fmt.Printf("Failed to create dummy processor function: %v\n", err)
	}

	return fn
}
