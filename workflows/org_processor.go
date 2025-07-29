// workflows/org_processor.go
package workflows

import (
	"context"
	"fmt"
	"time"

	"github.com/AI-Template-SDK/senso-api/pkg/models"
	"github.com/inngest/inngestgo"
	"github.com/inngest/inngestgo/step"

	"github.com/AI-Template-SDK/senso-api/pkg/repositories/interfaces"
	"github.com/AI-Template-SDK/senso-workflows/internal/config"
	internalModels "github.com/AI-Template-SDK/senso-workflows/internal/models"
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
			orgDetails, err := step.Run(ctx, "get-org-details", func(ctx context.Context) (*services.RealOrgDetails, error) {
				fmt.Printf("[ProcessOrg] Step 1: Getting org details from database\n")
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

			// Step 2: Process each question in the matrix
			questionRuns, err := step.Run(ctx, "process-question-matrix", func(ctx context.Context) ([]*models.QuestionRun, error) {
				fmt.Printf("[ProcessOrg] Step 2: Processing question matrix\n")

				var allRuns []*models.QuestionRun
				runIndex := 0

				// Process each question across all model×location combinations
				for _, questionWithTags := range orgDetails.Questions {
					question := questionWithTags.Question

					for _, model := range orgDetails.Models {
						for _, location := range orgDetails.Locations {
							runIndex++

							// Question Run Step
							questionRun, err := step.Run(ctx, fmt.Sprintf("question-run-%d", runIndex), func(ctx context.Context) (*models.QuestionRun, error) {
								fmt.Printf("[ProcessOrg] Running question %s with model %s at location %s\n",
									question.GeoQuestionID, model.Name, location.CountryCode)

								// Execute AI call
								aiResponse, err := p.getProvider(model.Name)
								if err != nil {
									return nil, fmt.Errorf("failed to get provider: %w", err)
								}

								workflowLocation := &internalModels.Location{
									Country: location.CountryCode,
									Region:  &location.RegionName,
								}

								response, err := aiResponse.RunQuestion(ctx, question.QuestionText, true, workflowLocation)
								if err != nil {
									return nil, fmt.Errorf("failed to run question: %w", err)
								}

								// Create question run record
								run := &models.QuestionRun{
									QuestionRunID: uuid.New(),
									GeoQuestionID: question.GeoQuestionID,
									ModelID:       model.GeoModelID,
									LocationID:    &location.OrgLocationID,
									ResponseText:  &response.Response,
									IsLatest:      false,
									CreatedAt:     time.Now(),
									UpdatedAt:     time.Now(),
								}

								if err := p.getQuestionRunRepo().Create(ctx, run); err != nil {
									return nil, fmt.Errorf("failed to create question run: %w", err)
								}

								fmt.Printf("[ProcessOrg] Successfully created question run: %s\n", run.QuestionRunID)
								return run, nil
							})
							if err != nil {
								fmt.Printf("[ProcessOrg] Error in question run %d: %v\n", runIndex, err)
								continue
							}

							// Mention Extract Step
							_, err = step.Run(ctx, fmt.Sprintf("mention-extract-%d", runIndex), func(ctx context.Context) (interface{}, error) {
								fmt.Printf("[ProcessOrg] Extracting mentions for question run %s\n", questionRun.QuestionRunID)

								mentions, err := p.getDataExtractionService().ExtractMentions(ctx, questionRun.QuestionRunID, *questionRun.ResponseText, orgDetails.TargetCompany)
								if err != nil {
									fmt.Printf("[ProcessOrg] Warning: Failed to extract mentions: %v\n", err)
									return nil, nil
								}

								if len(mentions) > 0 {
									if err := p.getMentionRepo().BulkCreate(ctx, mentions); err != nil {
										fmt.Printf("[ProcessOrg] Warning: Failed to store mentions: %v\n", err)
									} else {
										fmt.Printf("[ProcessOrg] Successfully extracted and stored %d mentions\n", len(mentions))
									}
								}

								return map[string]interface{}{"mentions_count": len(mentions)}, nil
							})
							if err != nil {
								fmt.Printf("[ProcessOrg] Error in mention extract %d: %v\n", runIndex, err)
							}

							// Claim Extract Step
							_, err = step.Run(ctx, fmt.Sprintf("claim-extract-%d", runIndex), func(ctx context.Context) (interface{}, error) {
								fmt.Printf("[ProcessOrg] Extracting claims for question run %s\n", questionRun.QuestionRunID)

								claims, err := p.getDataExtractionService().ExtractClaims(ctx, questionRun.QuestionRunID, *questionRun.ResponseText)
								if err != nil {
									fmt.Printf("[ProcessOrg] Warning: Failed to extract claims: %v\n", err)
									return nil, nil
								}

								if len(claims) > 0 {
									if err := p.getClaimRepo().BulkCreate(ctx, claims); err != nil {
										fmt.Printf("[ProcessOrg] Warning: Failed to store claims: %v\n", err)
									} else {
										fmt.Printf("[ProcessOrg] Successfully extracted and stored %d claims\n", len(claims))
									}
								}

								return map[string]interface{}{"claims_count": len(claims)}, nil
							})
							if err != nil {
								fmt.Printf("[ProcessOrg] Error in claim extract %d: %v\n", runIndex, err)
							}

							// Citation Extract Step
							_, err = step.Run(ctx, fmt.Sprintf("citation-extract-%d", runIndex), func(ctx context.Context) (interface{}, error) {
								fmt.Printf("[ProcessOrg] Extracting citations for question run %s\n", questionRun.QuestionRunID)

								// Get stored claims for this question run from database
								storedClaims, err := p.getClaimRepo().GetByRun(ctx, questionRun.QuestionRunID)
								if err != nil {
									fmt.Printf("[ProcessOrg] Warning: Failed to get stored claims for citations: %v\n", err)
									return nil, nil
								}

								if len(storedClaims) > 0 {
									citations, err := p.getDataExtractionService().ExtractCitations(ctx, storedClaims, *questionRun.ResponseText, orgDetails.Websites)
									if err != nil {
										fmt.Printf("[ProcessOrg] Warning: Failed to extract citations: %v\n", err)
										return nil, nil
									}

									if len(citations) > 0 {
										if err := p.getCitationRepo().BulkCreate(ctx, citations); err != nil {
											fmt.Printf("[ProcessOrg] Warning: Failed to store citations: %v\n", err)
										} else {
											fmt.Printf("[ProcessOrg] Successfully extracted and stored %d citations\n", len(citations))
										}
									}

									return map[string]interface{}{"citations_count": len(citations)}, nil
								}

								return map[string]interface{}{"citations_count": 0}, nil
							})
							if err != nil {
								fmt.Printf("[ProcessOrg] Error in citation extract %d: %v\n", runIndex, err)
							}

							allRuns = append(allRuns, questionRun)
						}
					}
				}

				fmt.Printf("[ProcessOrg] Completed processing %d question runs\n", len(allRuns))
				return allRuns, nil
			})
			if err != nil {
				return nil, fmt.Errorf("step 2 failed: %w", err)
			}

			// Step 3: Update Latest Flags
			_, err = step.Run(ctx, "update-latest-flags", func(ctx context.Context) (interface{}, error) {
				fmt.Printf("[ProcessOrg] Step 3: Updating latest flags for %d question runs\n", len(questionRuns))

				// Group runs by question
				runsByQuestion := make(map[uuid.UUID][]*models.QuestionRun)
				for _, run := range questionRuns {
					runsByQuestion[run.GeoQuestionID] = append(runsByQuestion[run.GeoQuestionID], run)
				}

				// Update latest flags for each question
				for questionID, runs := range runsByQuestion {
					if len(runs) == 0 {
						continue
					}

					// Find the latest run (most recent timestamp)
					var latestRun *models.QuestionRun
					for _, run := range runs {
						if latestRun == nil || run.CreatedAt.After(latestRun.CreatedAt) {
							latestRun = run
						}
					}

					// Update flags in database
					if err := p.getQuestionRunRepo().UpdateLatestFlags(ctx, questionID, latestRun.QuestionRunID); err != nil {
						fmt.Printf("[ProcessOrg] Warning: Failed to update latest flags for question %s: %v\n", questionID, err)
					} else {
						fmt.Printf("[ProcessOrg] Successfully updated latest flags for question %s\n", questionID)
					}
				}

				return map[string]interface{}{
					"questions_updated": len(runsByQuestion),
					"total_runs":        len(questionRuns),
				}, nil
			})
			if err != nil {
				return nil, fmt.Errorf("step 3 failed: %w", err)
			}

			// Step 4: Finalization
			finalResult, err := step.Run(ctx, "finalization", func(ctx context.Context) (interface{}, error) {
				fmt.Printf("[ProcessOrg] Step 4: Finalizing pipeline\n")

				result := map[string]interface{}{
					"org_id":   orgID,
					"org_name": orgDetails.Org.Name,
					"status":   "completed",
					"pipeline": "full_competitive_intelligence",
					"question_execution": map[string]interface{}{
						"total_runs":      len(questionRuns),
						"org_name":        orgDetails.Org.Name,
						"target_company":  orgDetails.TargetCompany,
						"questions_count": len(orgDetails.Questions),
						"models_count":    len(orgDetails.Models),
						"locations_count": len(orgDetails.Locations),
					},
					"completed_at": time.Now().UTC(),
				}

				fmt.Printf("[ProcessOrg] ✅ COMPLETED: Full competitive intelligence pipeline for org %s\n", orgID)
				fmt.Printf("[ProcessOrg] 📊 Data stored: question_runs, mentions, claims, citations\n")
				fmt.Printf("[ProcessOrg] 🎯 Target company: %s\n", orgDetails.TargetCompany)

				return result, nil
			})
			if err != nil {
				return nil, fmt.Errorf("step 4 failed: %w", err)
			}

			return finalResult, nil
		},
	)
	if err != nil {
		panic(fmt.Errorf("failed to create ProcessOrg function: %w", err))
	}
	return fn
}

// QuestionProcessingQueue represents the processing queue for question runs
type QuestionProcessingQueue struct {
	OrgID           string                    `json:"org_id"`
	OrgName         string                    `json:"org_name"`
	TargetCompany   string                    `json:"target_company"`
	TotalRuns       int                       `json:"total_runs"`
	CompletedRuns   int                       `json:"completed_runs"`
	FailedRuns      int                       `json:"failed_runs"`
	QuestionRuns    []*models.QuestionRun     `json:"question_runs"`
	ProcessingItems []*QuestionProcessingItem `json:"processing_items"`
	Status          string                    `json:"status"`
	CreatedAt       time.Time                 `json:"created_at"`
	CompletedAt     *time.Time                `json:"completed_at,omitempty"`
}

// QuestionProcessingItem represents a single question run to be processed
type QuestionProcessingItem struct {
	QuestionID   uuid.UUID `json:"question_id"`
	QuestionText string    `json:"question_text"`
	ModelID      uuid.UUID `json:"model_id"`
	ModelName    string    `json:"model_name"`
	LocationID   uuid.UUID `json:"location_id"`
	LocationCode string    `json:"location_code"`
	Status       string    `json:"status"` // pending, completed, failed
}

// Event types
type OrgProcessEvent struct {
	OrgID         string `json:"org_id"`
	TriggeredBy   string `json:"triggered_by"`
	UserID        string `json:"user_id,omitempty"`
	ScheduledDate string `json:"scheduled_date,omitempty"`
}

func (p *OrgProcessor) ProcessSingleQuestionRun() inngestgo.ServableFunction {
	fn, err := inngestgo.CreateFunction(
		p.client,
		inngestgo.FunctionOpts{
			ID:      "process-single-question-run",
			Name:    "Process Single Question Run - Micro-steps Pipeline",
			Retries: inngestgo.IntPtr(3),
		},
		inngestgo.EventTrigger("question.run", nil),
		func(ctx context.Context, input inngestgo.Input[QuestionRunEvent]) (any, error) {
			event := input.Event.Data
			fmt.Printf("[ProcessSingleQuestionRun] Starting micro-steps pipeline for question: %s, model: %s, location: %s\n",
				event.QuestionID, event.ModelName, event.LocationCode)

			// Step 1: Execute AI Call
			aiResponse, err := step.Run(ctx, "execute-ai-call", func(ctx context.Context) (*services.AIResponse, error) {
				fmt.Printf("[ProcessSingleQuestionRun] Step 1: Executing AI call for question: %s\n", event.QuestionID)

				// Convert location to workflow model format
				workflowLocation := &internalModels.Location{
					Country: event.LocationCode,
					Region:  &event.RegionName,
				}

				// Get the appropriate AI provider
				provider, err := p.getProvider(event.ModelName)
				if err != nil {
					return nil, fmt.Errorf("failed to get provider: %w", err)
				}

				// Execute the AI call
				response, err := provider.RunQuestion(ctx, event.QuestionText, true, workflowLocation)
				if err != nil {
					return nil, fmt.Errorf("failed to run question: %w", err)
				}

				fmt.Printf("[ProcessSingleQuestionRun] Successfully executed AI call with %d input tokens, %d output tokens, cost: $%.4f\n",
					response.InputTokens, response.OutputTokens, response.Cost)
				return response, nil
			})
			if err != nil {
				return nil, fmt.Errorf("step 1 failed: %w", err)
			}

			// Step 2: Create Initial Question Run Record
			questionRun, err := step.Run(ctx, "create-question-run", func(ctx context.Context) (*models.QuestionRun, error) {
				fmt.Printf("[ProcessSingleQuestionRun] Step 2: Creating question run record\n")

				run := &models.QuestionRun{
					QuestionRunID: uuid.New(),
					GeoQuestionID: event.QuestionID,
					ModelID:       event.ModelID,
					LocationID:    &event.LocationID,
					ResponseText:  &aiResponse.Response,
					IsLatest:      false, // Will be set later
					CreatedAt:     time.Now(),
					UpdatedAt:     time.Now(),
				}

				// Store initial run in database
				if err := p.getQuestionRunRepo().Create(ctx, run); err != nil {
					return nil, fmt.Errorf("failed to create question run: %w", err)
				}

				fmt.Printf("[ProcessSingleQuestionRun] Successfully created question run: %s\n", run.QuestionRunID)
				return run, nil
			})
			if err != nil {
				return nil, fmt.Errorf("step 2 failed: %w", err)
			}

			// Step 3: Extract Mentions
			mentions, err := step.Run(ctx, "extract-mentions", func(ctx context.Context) ([]*models.QuestionRunMention, error) {
				fmt.Printf("[ProcessSingleQuestionRun] Step 3: Extracting mentions from AI response\n")

				mentions, err := p.getDataExtractionService().ExtractMentions(ctx, questionRun.QuestionRunID, aiResponse.Response, event.TargetCompany)
				if err != nil {
					fmt.Printf("[ProcessSingleQuestionRun] Warning: Failed to extract mentions: %v\n", err)
					return []*models.QuestionRunMention{}, nil
				}

				if len(mentions) > 0 {
					if err := p.getMentionRepo().BulkCreate(ctx, mentions); err != nil {
						fmt.Printf("[ProcessSingleQuestionRun] Warning: Failed to store mentions: %v\n", err)
					} else {
						fmt.Printf("[ProcessSingleQuestionRun] Successfully extracted and stored %d mentions\n", len(mentions))
					}
				}

				return mentions, nil
			})
			if err != nil {
				return nil, fmt.Errorf("step 3 failed: %w", err)
			}

			// Step 4: Extract Claims
			claims, err := step.Run(ctx, "extract-claims", func(ctx context.Context) ([]*models.QuestionRunClaim, error) {
				fmt.Printf("[ProcessSingleQuestionRun] Step 4: Extracting claims from AI response\n")

				claims, err := p.getDataExtractionService().ExtractClaims(ctx, questionRun.QuestionRunID, aiResponse.Response)
				if err != nil {
					fmt.Printf("[ProcessSingleQuestionRun] Warning: Failed to extract claims: %v\n", err)
					return []*models.QuestionRunClaim{}, nil
				}

				if len(claims) > 0 {
					if err := p.getClaimRepo().BulkCreate(ctx, claims); err != nil {
						fmt.Printf("[ProcessSingleQuestionRun] Warning: Failed to store claims: %v\n", err)
					} else {
						fmt.Printf("[ProcessSingleQuestionRun] Successfully extracted and stored %d claims\n", len(claims))
					}
				}

				return claims, nil
			})
			if err != nil {
				return nil, fmt.Errorf("step 4 failed: %w", err)
			}

			// Step 5: Extract Citations (only if we have claims)
			var citations []*models.QuestionRunCitation
			if len(claims) > 0 {
				citations, err = step.Run(ctx, "extract-citations", func(ctx context.Context) ([]*models.QuestionRunCitation, error) {
					fmt.Printf("[ProcessSingleQuestionRun] Step 5: Extracting citations for %d claims\n", len(claims))

					citations, err := p.getDataExtractionService().ExtractCitations(ctx, claims, aiResponse.Response, event.OrgWebsites)
					if err != nil {
						fmt.Printf("[ProcessSingleQuestionRun] Warning: Failed to extract citations: %v\n", err)
						return []*models.QuestionRunCitation{}, nil
					}

					if len(citations) > 0 {
						if err := p.getCitationRepo().BulkCreate(ctx, citations); err != nil {
							fmt.Printf("[ProcessSingleQuestionRun] Warning: Failed to store citations: %v\n", err)
						} else {
							fmt.Printf("[ProcessSingleQuestionRun] Successfully extracted and stored %d citations\n", len(citations))
						}
					}

					return citations, nil
				})
				if err != nil {
					return nil, fmt.Errorf("step 5 failed: %w", err)
				}
			}

			// Step 6: Calculate Competitive Metrics
			var metrics *services.CompetitiveMetrics
			if len(mentions) > 0 {
				metrics, err = step.Run(ctx, "calculate-metrics", func(ctx context.Context) (*services.CompetitiveMetrics, error) {
					fmt.Printf("[ProcessSingleQuestionRun] Step 6: Calculating competitive metrics\n")

					metrics, err := p.getDataExtractionService().CalculateMetrics(ctx, mentions, aiResponse.Response, event.TargetCompany)
					if err != nil {
						fmt.Printf("[ProcessSingleQuestionRun] Warning: Failed to calculate metrics: %v\n", err)
						return &services.CompetitiveMetrics{}, nil
					}

					// Update question run with metrics
					questionRun.TargetMentioned = metrics.TargetMentioned
					questionRun.TargetSOV = metrics.ShareOfVoice
					questionRun.TargetRank = metrics.TargetRank
					questionRun.TargetSentiment = metrics.TargetSentiment

					if err := p.getQuestionRunRepo().Update(ctx, questionRun); err != nil {
						fmt.Printf("[ProcessSingleQuestionRun] Warning: Failed to update run with metrics: %v\n", err)
					} else {
						fmt.Printf("[ProcessSingleQuestionRun] Successfully calculated and stored competitive metrics\n")
					}

					return metrics, nil
				})
				if err != nil {
					return nil, fmt.Errorf("step 6 failed: %w", err)
				}
			}

			// Final Result Summary
			finalResult := map[string]interface{}{
				"question_run_id": questionRun.QuestionRunID,
				"question_id":     event.QuestionID,
				"model_name":      event.ModelName,
				"location_code":   event.LocationCode,
				"status":          "completed",
				"ai_response": map[string]interface{}{
					"input_tokens":  aiResponse.InputTokens,
					"output_tokens": aiResponse.OutputTokens,
					"cost":          aiResponse.Cost,
				},
				"extracted_data": map[string]interface{}{
					"mentions_count":  len(mentions),
					"claims_count":    len(claims),
					"citations_count": len(citations),
					"has_metrics":     metrics != nil,
				},
				"completed_at": time.Now().UTC(),
			}

			fmt.Printf("[ProcessSingleQuestionRun] ✅ COMPLETED: Micro-steps pipeline for question %s\n", event.QuestionID)
			return finalResult, nil
		},
	)
	if err != nil {
		panic(fmt.Errorf("failed to create ProcessSingleQuestionRun function: %w", err))
	}
	return fn
}

// Helper methods to access repositories and services
func (p *OrgProcessor) getProvider(modelName string) (services.AIProvider, error) {
	return p.questionRunnerService.GetProvider(modelName)
}

func (p *OrgProcessor) getDataExtractionService() services.DataExtractionService {
	return p.questionRunnerService.GetDataExtractionService()
}

func (p *OrgProcessor) getQuestionRunRepo() interfaces.QuestionRunRepository {
	return p.questionRunnerService.GetQuestionRunRepo()
}

func (p *OrgProcessor) getMentionRepo() interfaces.QuestionRunMentionRepository {
	return p.questionRunnerService.GetMentionRepo()
}

func (p *OrgProcessor) getClaimRepo() interfaces.QuestionRunClaimRepository {
	return p.questionRunnerService.GetClaimRepo()
}

func (p *OrgProcessor) getCitationRepo() interfaces.QuestionRunCitationRepository {
	return p.questionRunnerService.GetCitationRepo()
}

// QuestionRunEvent represents the event data for processing a single question run
type QuestionRunEvent struct {
	QuestionID    uuid.UUID `json:"question_id"`
	QuestionText  string    `json:"question_text"`
	ModelID       uuid.UUID `json:"model_id"`
	ModelName     string    `json:"model_name"`
	LocationID    uuid.UUID `json:"location_id"`
	LocationCode  string    `json:"location_code"`
	RegionName    string    `json:"region_name"`
	TargetCompany string    `json:"target_company"`
	OrgWebsites   []string  `json:"org_websites"`
	OrgID         string    `json:"org_id"`
}
