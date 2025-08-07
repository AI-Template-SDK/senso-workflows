// services/question_runner_service.go
package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/AI-Template-SDK/senso-api/pkg/models"
	"github.com/AI-Template-SDK/senso-api/pkg/repositories/interfaces"
	"github.com/AI-Template-SDK/senso-workflows/internal/config"
	workflowModels "github.com/AI-Template-SDK/senso-workflows/internal/models"
	"github.com/google/uuid"
)

type questionRunnerService struct {
	cfg                   *config.Config
	costService           CostService
	repos                 *RepositoryManager
	dataExtractionService DataExtractionService
}

func NewQuestionRunnerService(cfg *config.Config, repos *RepositoryManager, dataExtractionService DataExtractionService) QuestionRunnerService {
	return &questionRunnerService{
		cfg:                   cfg,
		costService:           NewCostService(),
		repos:                 repos,
		dataExtractionService: dataExtractionService,
	}
}

// RunQuestionMatrix processes all questions across models and locations, storing results in database
func (s *questionRunnerService) RunQuestionMatrix(ctx context.Context, orgDetails *RealOrgDetails) ([]*models.QuestionRun, error) {
	fmt.Printf("[RunQuestionMatrix] Processing %d questions across %d models and %d locations\n",
		len(orgDetails.Questions), len(orgDetails.Models), len(orgDetails.Locations))

	var allRuns []*models.QuestionRun

	// Process each question
	for _, questionWithTags := range orgDetails.Questions {
		question := questionWithTags.Question

		// Process across all model×location combinations for this question
		for _, model := range orgDetails.Models {
			for _, location := range orgDetails.Locations {
				// Process single question run with full pipeline
				run, err := s.ProcessSingleQuestion(ctx, question, model, location, orgDetails.TargetCompany, orgDetails.Websites)
				if err != nil {
					fmt.Printf("[RunQuestionMatrix] Error processing question %s with model %s at location %s: %v\n",
						question.GeoQuestionID, model.Name, location.CountryCode, err)
					continue
				}

				allRuns = append(allRuns, run)
				fmt.Printf("[RunQuestionMatrix] Successfully processed question %s with model %s at location %s\n",
					question.GeoQuestionID, model.Name, location.CountryCode)
			}
		}
	}

	// Update latest flags for all questions
	if err := s.updateLatestFlags(ctx, orgDetails.Questions, allRuns); err != nil {
		fmt.Printf("[RunQuestionMatrix] Warning: Failed to update latest flags: %v\n", err)
	}

	fmt.Printf("[RunQuestionMatrix] Completed processing: %d total runs created\n", len(allRuns))
	return allRuns, nil
}

// ProcessSingleQuestion handles the complete pipeline for one question run
func (s *questionRunnerService) ProcessSingleQuestion(ctx context.Context, question *models.GeoQuestion, model *models.GeoModel, location *models.OrgLocation, targetCompany string, orgWebsites []string) (*models.QuestionRun, error) {
	fmt.Printf("[ProcessSingleQuestion] Processing question %s with model %s\n", question.GeoQuestionID, model.Name)

	// 1. Execute AI call
	aiResponse, err := s.executeAICall(ctx, question.QuestionText, model.Name, location)
	if err != nil {
		return nil, fmt.Errorf("AI call failed: %w", err)
	}

	// 2. Create initial question run record
	run := &models.QuestionRun{
		QuestionRunID: uuid.New(),
		GeoQuestionID: question.GeoQuestionID,
		ModelID:       model.GeoModelID,
		LocationID:    &location.OrgLocationID,
		ResponseText:  &aiResponse.Response,
		InputTokens:   &aiResponse.InputTokens,
		OutputTokens:  &aiResponse.OutputTokens,
		TotalCost:     &aiResponse.Cost,
		IsLatest:      false, // Will be set later
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	// Store initial run in database
	if err := s.repos.QuestionRunRepo.Create(ctx, run); err != nil {
		return nil, fmt.Errorf("failed to create question run: %w", err)
	}

	// 3. Extract mentions
	mentions, err := s.dataExtractionService.ExtractMentions(ctx, run.QuestionRunID, aiResponse.Response, targetCompany)
	if err != nil {
		fmt.Printf("[ProcessSingleQuestion] Warning: Failed to extract mentions: %v\n", err)
	} else if len(mentions) > 0 {
		if err := s.repos.MentionRepo.BulkCreate(ctx, mentions); err != nil {
			fmt.Printf("[ProcessSingleQuestion] Warning: Failed to store mentions: %v\n", err)
		}
	}

	// 4. Extract claims
	claims, err := s.dataExtractionService.ExtractClaims(ctx, run.QuestionRunID, aiResponse.Response, targetCompany, orgWebsites)
	if err != nil {
		fmt.Printf("[ProcessSingleQuestion] Warning: Failed to extract claims: %v\n", err)
	} else if len(claims) > 0 {
		if err := s.repos.ClaimRepo.BulkCreate(ctx, claims); err != nil {
			fmt.Printf("[ProcessSingleQuestion] Warning: Failed to store claims: %v\n", err)
		}

		// 5. Extract citations for claims - now passing org websites
		citations, err := s.dataExtractionService.ExtractCitations(ctx, claims, aiResponse.Response, orgWebsites)
		if err != nil {
			fmt.Printf("[ProcessSingleQuestion] Warning: Failed to extract citations: %v\n", err)
		} else if len(citations) > 0 {
			if err := s.repos.CitationRepo.BulkCreate(ctx, citations); err != nil {
				fmt.Printf("[ProcessSingleQuestion] Warning: Failed to store citations: %v\n", err)
			}
		}
	}

	// 6. Calculate competitive metrics
	if len(mentions) > 0 {
		metrics, err := s.dataExtractionService.CalculateMetrics(ctx, mentions, aiResponse.Response, targetCompany)
		if err != nil {
			fmt.Printf("[ProcessSingleQuestion] Warning: Failed to calculate metrics: %v\n", err)
		} else {
			// Update question run with metrics
			run.TargetMentioned = metrics.TargetMentioned
			run.TargetSOV = metrics.ShareOfVoice
			run.TargetRank = metrics.TargetRank
			run.TargetSentiment = metrics.TargetSentiment

			if err := s.repos.QuestionRunRepo.Update(ctx, run); err != nil {
				fmt.Printf("[ProcessSingleQuestion] Warning: Failed to update run with metrics: %v\n", err)
			}
		}
	}

	fmt.Printf("[ProcessSingleQuestion] Successfully completed full pipeline for question %s\n", question.GeoQuestionID)
	return run, nil
}

// executeAICall performs the actual AI model call
func (s *questionRunnerService) executeAICall(ctx context.Context, questionText, modelName string, location *models.OrgLocation) (*AIResponse, error) {
	fmt.Printf("[executeAICall] 🚀 Making AI call for model: %s", modelName)

	// Convert location to workflow model format
	workflowLocation := &workflowModels.Location{
		Country: location.CountryCode,
		Region:  &location.RegionName,
	}

	// Get the appropriate AI provider
	provider, err := s.getProvider(modelName)
	if err != nil {
		return nil, fmt.Errorf("failed to get provider: %w", err)
	}

	// Determine if web search should be enabled (for now, disable it)
	webSearch := true
	fmt.Printf("[executeAICall] 🌐 Web search enabled: %t", webSearch)

	// Execute the AI call
	response, err := provider.RunQuestion(ctx, questionText, webSearch, workflowLocation)
	if err != nil {
		return nil, fmt.Errorf("failed to run question: %w", err)
	}

	fmt.Printf("[executeAICall] ✅ AI call completed successfully")
	fmt.Printf("[executeAICall]   - Input tokens: %d", response.InputTokens)
	fmt.Printf("[executeAICall]   - Output tokens: %d", response.OutputTokens)
	fmt.Printf("[executeAICall]   - Cost: $%.6f", response.Cost)

	return response, nil
}

// getProvider returns the appropriate AI provider for the model
func (s *questionRunnerService) getProvider(model string) (AIProvider, error) {
	modelLower := strings.ToLower(model)

	// Debug the config
	if s.cfg == nil {
		return nil, fmt.Errorf("config is nil")
	} else if s.cfg.OpenAIAPIKey == "" {
		return nil, fmt.Errorf("OpenAI API key is empty in config")
	}

	// Determine provider based on model name
	if strings.Contains(modelLower, "gpt") || strings.Contains(modelLower, "4.1") {
		fmt.Printf("[getProvider] 🎯 Selected OpenAI provider for model: %s", model)
		return NewOpenAIProvider(s.cfg, model, s.costService), nil
	}

	if strings.Contains(modelLower, "claude") || strings.Contains(modelLower, "sonnet") || strings.Contains(modelLower, "opus") || strings.Contains(modelLower, "haiku") {
		fmt.Printf("[getProvider] 🎯 Selected Anthropic provider for model: %s", model)
		return NewAnthropicProvider(s.cfg, model, s.costService), nil
	}

	return nil, fmt.Errorf("unsupported model: %s", model)
}

// updateLatestFlags manages the is_latest and is_second_latest flags
func (s *questionRunnerService) updateLatestFlags(ctx context.Context, questions []interfaces.GeoQuestionWithTags, newRuns []*models.QuestionRun) error {
	// Group runs by question
	runsByQuestion := make(map[uuid.UUID][]*models.QuestionRun)
	for _, run := range newRuns {
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
		if err := s.repos.QuestionRunRepo.UpdateLatestFlags(ctx, questionID, latestRun.QuestionRunID); err != nil {
			return fmt.Errorf("failed to update latest flags for question %s: %w", questionID, err)
		}
	}

	return nil
}
