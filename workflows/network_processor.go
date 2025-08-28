// workflows/network_processor.go
package workflows

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/inngest/inngestgo"
	"github.com/inngest/inngestgo/step"

	"github.com/AI-Template-SDK/senso-api/pkg/models"
	"github.com/AI-Template-SDK/senso-workflows/internal/config"
	"github.com/AI-Template-SDK/senso-workflows/services"
)

type NetworkProcessor struct {
	questionRunnerService services.QuestionRunnerService
	client                inngestgo.Client
	cfg                   *config.Config
}

func NewNetworkProcessor(
	questionRunnerService services.QuestionRunnerService,
	cfg *config.Config,
) *NetworkProcessor {
	return &NetworkProcessor{
		questionRunnerService: questionRunnerService,
		cfg:                   cfg,
	}
}

func (p *NetworkProcessor) SetClient(client inngestgo.Client) {
	p.client = client
}

func (p *NetworkProcessor) ProcessNetwork() inngestgo.ServableFunction {
	fn, err := inngestgo.CreateFunction(
		p.client,
		inngestgo.FunctionOpts{
			ID:      "process-network",
			Name:    "Process Network Questions - Question Only Pipeline",
			Retries: inngestgo.IntPtr(3),
		},
		inngestgo.EventTrigger("network.questions.process", nil),
		func(ctx context.Context, input inngestgo.Input[NetworkProcessEvent]) (any, error) {
			networkID := input.Event.Data.NetworkID
			fmt.Printf("[ProcessNetwork] Starting network questions pipeline for network: %s\n", networkID)

			// Step 1: Fetch Network Questions
			questionsResult, err := step.Run(ctx, "fetch-network-questions", func(ctx context.Context) (interface{}, error) {
				fmt.Printf("[ProcessNetwork] Step 1: Fetching network questions for network: %s\n", networkID)
				questions, err := p.questionRunnerService.GetNetworkQuestions(ctx, networkID)
				if err != nil {
					return nil, fmt.Errorf("failed to fetch network questions: %w", err)
				}

				fmt.Printf("[ProcessNetwork] Found %d network questions to process\n", len(questions))
				return map[string]interface{}{
					"questions":  questions,
					"count":      len(questions),
					"network_id": networkID,
				}, nil
			})
			if err != nil {
				return nil, fmt.Errorf("step 1 failed: %w", err)
			}

			// Extract questions from the result
			questionsData := questionsResult.(map[string]interface{})
			questions := questionsData["questions"].([]interface{})
			questionCount := int(questionsData["count"].(float64))

			// Step 2: Process each question individually
			var allRuns []interface{}
			for i, questionInterface := range questions {
				question := questionInterface.(map[string]interface{})
				questionID := question["geo_question_id"].(string)
				questionIndex := i + 1
				stepName := fmt.Sprintf("process-question-%d", questionIndex)

				_, err := step.Run(ctx, stepName, func(ctx context.Context) (interface{}, error) {
					fmt.Printf("[ProcessNetwork] Step %d: Processing question %d/%d: %s\n",
						questionIndex+1, questionIndex, questionCount, questionID)

					// Create a GeoQuestion struct from the map data
					questionUUID, err := uuid.Parse(questionID)
					if err != nil {
						return nil, fmt.Errorf("invalid question ID format: %w", err)
					}

					geoQuestion := &models.GeoQuestion{
						GeoQuestionID: questionUUID,
						QuestionText:  question["question_text"].(string),
					}

					run, err := p.questionRunnerService.ProcessNetworkQuestionOnly(ctx, geoQuestion)
					if err != nil {
						return nil, fmt.Errorf("failed to process question %s: %w", questionID, err)
					}

					fmt.Printf("[ProcessNetwork] Successfully processed question %d/%d: %s\n",
						questionIndex, questionCount, questionID)

					return map[string]interface{}{
						"question_id": questionID,
						"run_id":      run.QuestionRunID,
						"status":      "completed",
					}, nil
				})
				if err != nil {
					fmt.Printf("[ProcessNetwork] Warning: Failed to process question %d/%d: %v\n",
						questionIndex, questionCount, err)
					continue
				}

				// Track that this question was processed
				allRuns = append(allRuns, map[string]interface{}{
					"question_id": questionID,
					"status":      "processed",
				})
			}

			// Step 3: Update Latest Flags
			_, err = step.Run(ctx, "update-latest-flags", func(ctx context.Context) (interface{}, error) {
				fmt.Printf("[ProcessNetwork] Final Step: Updating latest flags for %d processed questions\n", len(allRuns))

				// Fetch the actual runs that were created and update latest flags
				if err := p.questionRunnerService.UpdateNetworkLatestFlags(ctx, networkID); err != nil {
					return nil, fmt.Errorf("failed to update latest flags: %w", err)
				}

				fmt.Printf("[ProcessNetwork] Successfully updated latest flags for network: %s\n", networkID)
				return map[string]interface{}{
					"status":     "completed",
					"total_runs": len(allRuns),
					"network_id": networkID,
				}, nil
			})
			if err != nil {
				return nil, fmt.Errorf("final step failed: %w", err)
			}

			// Final Result Summary
			finalResult := map[string]interface{}{
				"network_id":          networkID,
				"status":              "completed",
				"pipeline":            "network_questions_only",
				"questions_processed": questionCount,
				"completed_at":        time.Now().UTC(),
			}

			fmt.Printf("[ProcessNetwork] ✅ COMPLETED: Network questions pipeline for network %s\n", networkID)
			fmt.Printf("[ProcessNetwork] 📊 Data stored: question_runs (no extractions/evals)\n")

			return finalResult, nil
		},
	)
	if err != nil {
		panic(fmt.Errorf("failed to create ProcessNetwork function: %w", err))
	}
	return fn
}

// Event types
type NetworkProcessEvent struct {
	NetworkID   string `json:"network_id"`
	TriggeredBy string `json:"triggered_by"`
	UserID      string `json:"user_id,omitempty"`
}
