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
	orgService            OrgService
}

func NewQuestionRunnerService(cfg *config.Config, repos *RepositoryManager, dataExtractionService DataExtractionService, orgService OrgService) QuestionRunnerService {
	return &questionRunnerService{
		cfg:                   cfg,
		costService:           NewCostService(),
		repos:                 repos,
		dataExtractionService: dataExtractionService,
		orgService:            orgService,
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
		ModelID:       &model.GeoModelID,
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
	mentions, err := s.dataExtractionService.ExtractMentions(ctx, run.QuestionRunID, aiResponse.Response, targetCompany, orgWebsites)
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

// updateNetworkLatestFlags manages the is_latest flags for network question runs
func (s *questionRunnerService) updateNetworkLatestFlags(ctx context.Context, newRuns []*models.QuestionRun) error {
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

// RunNetworkQuestionsQuestionOnly processes all network questions with gpt-4.1, storing results in database
func (s *questionRunnerService) RunNetworkQuestionsQuestionOnly(ctx context.Context, networkID string) ([]*models.QuestionRun, error) {
	fmt.Printf("[RunNetworkQuestionsQuestionOnly] Processing network questions for network: %s\n", networkID)

	// Parse networkID to UUID
	networkUUID, err := uuid.Parse(networkID)
	if err != nil {
		return nil, fmt.Errorf("invalid network ID format: %w", err)
	}

	// Get network questions
	questions, err := s.repos.GeoQuestionRepo.GetByNetwork(ctx, networkUUID)
	if err != nil {
		return nil, fmt.Errorf("failed to get network questions: %w", err)
	}

	fmt.Printf("[RunNetworkQuestionsQuestionOnly] Found %d network questions\n", len(questions))

	var allRuns []*models.QuestionRun

	// Process each question
	for _, question := range questions {
		// Process single network question run (question only, no extractions/evals)
		run, err := s.ProcessNetworkQuestionOnly(ctx, question)
		if err != nil {
			fmt.Printf("[RunNetworkQuestionsQuestionOnly] Error processing question %s: %v\n",
				question.GeoQuestionID, err)
			continue
		}

		allRuns = append(allRuns, run)
		fmt.Printf("[RunNetworkQuestionsQuestionOnly] Successfully processed question %s\n",
			question.GeoQuestionID)
	}

	// Update latest flags for all network question runs
	if len(allRuns) > 0 {
		if err := s.updateNetworkLatestFlags(ctx, allRuns); err != nil {
			fmt.Printf("[RunNetworkQuestionsQuestionOnly] Warning: failed to update latest flags: %v\n", err)
		}
	}

	fmt.Printf("[RunNetworkQuestionsQuestionOnly] Completed processing: %d total runs created\n", len(allRuns))
	return allRuns, nil
}

// ProcessNetworkQuestionOnly handles the question-only pipeline for one network question run
func (s *questionRunnerService) ProcessNetworkQuestionOnly(ctx context.Context, question *models.GeoQuestion) (*models.QuestionRun, error) {
	fmt.Printf("[ProcessNetworkQuestionOnly] Processing question %s\n", question.GeoQuestionID)

	// Execute AI call with websearch (no location)
	aiResponse, err := s.executeNetworkAICall(ctx, question.QuestionText)
	if err != nil {
		return nil, fmt.Errorf("AI call failed: %w", err)
	}

	// Create question run record (no model_id, no location_id for network questions)
	// Network questions don't have mentions, SOV, or other metrics - leave them null
	runModel := "gpt-4.1"
	run := &models.QuestionRun{
		QuestionRunID: uuid.New(),
		GeoQuestionID: question.GeoQuestionID,
		ResponseText:  &aiResponse.Response,
		InputTokens:   &aiResponse.InputTokens,
		OutputTokens:  &aiResponse.OutputTokens,
		TotalCost:     &aiResponse.Cost,
		RunModel:      &runModel, // Set to gpt-4.1 for network runs
		IsLatest:      true,      // Set to true initially, will be updated by updateNetworkLatestFlags
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
		// All other fields (mentions, SOV, etc.) remain null for network questions
		// RunCountry and RunRegion remain null for network questions
	}

	// Store run in database
	if err := s.repos.QuestionRunRepo.Create(ctx, run); err != nil {
		return nil, fmt.Errorf("failed to create question run: %w", err)
	}

	fmt.Printf("[ProcessNetworkQuestionOnly] Successfully completed question-only pipeline for question %s\n", question.GeoQuestionID)
	return run, nil
}

// executeNetworkAICall performs the actual AI model call for network questions (websearch, no location)
func (s *questionRunnerService) executeNetworkAICall(ctx context.Context, questionText string) (*AIResponse, error) {
	fmt.Printf("[executeNetworkAICall] 🚀 Making AI call for network question with gpt-4.1")

	// Get the OpenAI provider for gpt-4.1
	provider, err := s.getProvider("gpt-4.1")
	if err != nil {
		return nil, fmt.Errorf("failed to get provider: %w", err)
	}

	// Execute the AI call with websearch (no location)
	response, err := provider.RunQuestionWebSearch(ctx, questionText)
	if err != nil {
		return nil, fmt.Errorf("failed to run question: %w", err)
	}

	fmt.Printf("[executeNetworkAICall] ✅ AI call completed successfully")
	fmt.Printf("[executeNetworkAICall]   - Input tokens: %d", response.InputTokens)
	fmt.Printf("[executeNetworkAICall]   - Output tokens: %d", response.OutputTokens)
	fmt.Printf("[executeNetworkAICall]   - Cost: $%.6f", response.Cost)

	return response, nil
}

// GetNetworkQuestions fetches all network-scoped questions for a given network ID
func (s *questionRunnerService) GetNetworkQuestions(ctx context.Context, networkID string) ([]*models.GeoQuestion, error) {
	// Parse networkID to UUID
	networkUUID, err := uuid.Parse(networkID)
	if err != nil {
		return nil, fmt.Errorf("invalid network ID format: %w", err)
	}

	// Get network questions
	questions, err := s.repos.GeoQuestionRepo.GetByNetwork(ctx, networkUUID)
	if err != nil {
		return nil, fmt.Errorf("failed to get network questions: %w", err)
	}

	fmt.Printf("[GetNetworkQuestions] Found %d network questions for network: %s\n", len(questions), networkID)
	return questions, nil
}

// UpdateNetworkLatestFlags updates the is_latest flags for all questions in a network
func (s *questionRunnerService) UpdateNetworkLatestFlags(ctx context.Context, networkID string) error {
	// Parse networkID to UUID
	networkUUID, err := uuid.Parse(networkID)
	if err != nil {
		return fmt.Errorf("invalid network ID format: %w", err)
	}

	// Get network questions first
	questions, err := s.repos.GeoQuestionRepo.GetByNetwork(ctx, networkUUID)
	if err != nil {
		return fmt.Errorf("failed to get network questions: %w", err)
	}

	if len(questions) == 0 {
		fmt.Printf("[UpdateNetworkLatestFlags] No questions found for network: %s\n", networkID)
		return nil
	}

	// Update latest flags for each question
	for _, question := range questions {
		// Get all runs for this question
		runs, err := s.repos.QuestionRunRepo.GetByQuestion(ctx, question.GeoQuestionID)
		if err != nil {
			fmt.Printf("[UpdateNetworkLatestFlags] Warning: failed to get runs for question %s: %v\n",
				question.GeoQuestionID, err)
			continue
		}

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

		if latestRun == nil {
			continue
		}

		// Update latest flags using the existing method
		if err := s.repos.QuestionRunRepo.UpdateLatestFlags(ctx, question.GeoQuestionID, latestRun.QuestionRunID); err != nil {
			fmt.Printf("[UpdateNetworkLatestFlags] Warning: failed to update latest flags for question %s: %v\n",
				question.GeoQuestionID, err)
		}
	}

	fmt.Printf("[UpdateNetworkLatestFlags] Successfully updated latest flags for %d questions in network: %s\n",
		len(questions), networkID)
	return nil
}

// RunNetworkOrgProcessing processes network org data extraction for a given org
func (s *questionRunnerService) RunNetworkOrgProcessing(ctx context.Context, orgID string) ([]*NetworkOrgProcessingResult, error) {
	fmt.Printf("[RunNetworkOrgProcessing] Starting network org processing for org: %s\n", orgID)

	// Get org details and network
	orgDetails, err := s.GetOrgDetailsForNetworkProcessing(ctx, orgID)
	if err != nil {
		return nil, fmt.Errorf("failed to get org details: %w", err)
	}

	// Get latest network question runs
	questionRuns, err := s.GetLatestNetworkQuestionRuns(ctx, orgDetails.NetworkID)
	if err != nil {
		return nil, fmt.Errorf("failed to get network question runs: %w", err)
	}

	fmt.Printf("[RunNetworkOrgProcessing] Processing %d question runs for org %s in network %s\n",
		len(questionRuns), orgDetails.OrgName, orgDetails.NetworkID)

	var results []*NetworkOrgProcessingResult
	totalEvaluations := 0
	totalCompetitors := 0
	totalCitations := 0

	// Process each question run
	for _, questionRun := range questionRuns {
		questionRunID := questionRun["question_run_id"].(string)
		questionText := questionRun["question_text"].(string)
		responseText := questionRun["response_text"].(string)

		// Parse UUIDs
		questionRunUUID, err := uuid.Parse(questionRunID)
		if err != nil {
			fmt.Printf("[RunNetworkOrgProcessing] Warning: invalid question run ID %s: %v\n", questionRunID, err)
			continue
		}
		orgUUID, err := uuid.Parse(orgID)
		if err != nil {
			fmt.Printf("[RunNetworkOrgProcessing] Warning: invalid org ID %s: %v\n", orgID, err)
			continue
		}

		// Process the question run
		result, err := s.ProcessNetworkOrgQuestionRun(ctx, questionRunUUID, orgUUID, orgDetails.OrgName, orgDetails.Websites, questionText, responseText)
		if err != nil {
			fmt.Printf("[RunNetworkOrgProcessing] Warning: failed to process question run %s: %v\n", questionRunID, err)
			continue
		}

		totalEvaluations++
		totalCompetitors += len(result.Competitors)
		totalCitations += len(result.Citations)

		fmt.Printf("[RunNetworkOrgProcessing] Successfully processed question run %s\n", questionRunID)
	}

	// Create summary result
	summaryResult := &NetworkOrgProcessingResult{
		OrgID:        orgID,
		NetworkID:    orgDetails.NetworkID,
		QuestionRuns: len(questionRuns),
		Evaluations:  totalEvaluations,
		Competitors:  totalCompetitors,
		Citations:    totalCitations,
		Status:       "completed",
	}
	results = append(results, summaryResult)

	fmt.Printf("[RunNetworkOrgProcessing] Completed processing for org %s: %d evaluations, %d competitors, %d citations\n",
		orgDetails.OrgName, totalEvaluations, totalCompetitors, totalCitations)

	return results, nil
}

// GetOrgDetailsForNetworkProcessing fetches org details needed for network processing
func (s *questionRunnerService) GetOrgDetailsForNetworkProcessing(ctx context.Context, orgID string) (*OrgDetailsForNetworkProcessing, error) {
	// Get org details from org service
	orgDetails, err := s.orgService.GetOrgDetails(ctx, orgID)
	if err != nil {
		return nil, fmt.Errorf("failed to get org details: %w", err)
	}

	// Get org websites
	websites := make([]string, len(orgDetails.Websites))
	copy(websites, orgDetails.Websites)

	// Get network ID from org
	networkID := orgDetails.Org.NetworkID.String()

	return &OrgDetailsForNetworkProcessing{
		OrgID:     orgID,
		OrgName:   orgDetails.Org.Name,
		NetworkID: networkID,
		Websites:  websites,
	}, nil
}

// GetLatestNetworkQuestionRuns fetches the latest question runs for a network
func (s *questionRunnerService) GetLatestNetworkQuestionRuns(ctx context.Context, networkID string) ([]map[string]interface{}, error) {
	// Parse networkID to UUID
	networkUUID, err := uuid.Parse(networkID)
	if err != nil {
		return nil, fmt.Errorf("invalid network ID format: %w", err)
	}

	// Get network questions first, then get latest runs for each question
	questions, err := s.repos.GeoQuestionRepo.GetByNetwork(ctx, networkUUID)
	if err != nil {
		return nil, fmt.Errorf("failed to get network questions: %w", err)
	}

	// Get latest question run for each question
	var questionRuns []*models.QuestionRun
	for _, question := range questions {
		latestRun, err := s.repos.QuestionRunRepo.GetLatestByQuestion(ctx, question.GeoQuestionID)
		if err != nil {
			fmt.Printf("[GetLatestNetworkQuestionRuns] Warning: failed to get latest run for question %s: %v\n", question.GeoQuestionID, err)
			continue
		}
		questionRuns = append(questionRuns, latestRun)
	}

	// Convert to map format for workflow
	var result []map[string]interface{}
	for _, run := range questionRuns {
		// Get the question text for this run
		question, err := s.repos.GeoQuestionRepo.GetByID(ctx, run.GeoQuestionID)
		if err != nil {
			fmt.Printf("[GetLatestNetworkQuestionRuns] Warning: failed to get question for run %s: %v\n", run.QuestionRunID, err)
			continue
		}

		responseText := ""
		if run.ResponseText != nil {
			responseText = *run.ResponseText
		}

		result = append(result, map[string]interface{}{
			"question_run_id": run.QuestionRunID.String(),
			"question_text":   question.QuestionText,
			"response_text":   responseText,
		})
	}

	fmt.Printf("[GetLatestNetworkQuestionRuns] Found %d latest question runs for network %s\n", len(result), networkID)
	return result, nil
}

// ProcessNetworkOrgQuestionRun processes a single question run for network org data extraction
func (s *questionRunnerService) ProcessNetworkOrgQuestionRun(ctx context.Context, questionRunID uuid.UUID, orgID uuid.UUID, orgName string, orgWebsites []string, questionText string, responseText string) (*NetworkOrgExtractionResult, error) {
	fmt.Printf("[ProcessNetworkOrgQuestionRun] Processing question run %s for org %s\n", questionRunID, orgName)

	// Extract network org data using the data extraction service
	result, err := s.dataExtractionService.ExtractNetworkOrgData(ctx, questionRunID, orgID, orgName, orgWebsites, questionText, responseText)
	if err != nil {
		return nil, fmt.Errorf("failed to extract network org data: %w", err)
	}

	// Store the evaluation
	if result.Evaluation != nil {
		if err := s.repos.NetworkOrgEvalRepo.Create(ctx, result.Evaluation); err != nil {
			fmt.Printf("[ProcessNetworkOrgQuestionRun] Warning: failed to store evaluation: %v\n", err)
		}
	}

	// Store competitors
	if len(result.Competitors) > 0 {
		for _, competitor := range result.Competitors {
			if err := s.repos.NetworkOrgCompetitorRepo.Create(ctx, competitor); err != nil {
				fmt.Printf("[ProcessNetworkOrgQuestionRun] Warning: failed to store competitor: %v\n", err)
			}
		}
	}

	// Store citations
	if len(result.Citations) > 0 {
		for _, citation := range result.Citations {
			if err := s.repos.NetworkOrgCitationRepo.Create(ctx, citation); err != nil {
				fmt.Printf("[ProcessNetworkOrgQuestionRun] Warning: failed to store citation: %v\n", err)
			}
		}
	}

	fmt.Printf("[ProcessNetworkOrgQuestionRun] Successfully processed question run %s: 1 evaluation, %d competitors, %d citations\n",
		questionRunID, len(result.Competitors), len(result.Citations))

	return result, nil
}

// GetAllNetworkQuestionRuns fetches ALL question runs for a network (not just latest)
func (s *questionRunnerService) GetAllNetworkQuestionRuns(ctx context.Context, networkID string) ([]map[string]interface{}, error) {
	// Parse networkID to UUID
	networkUUID, err := uuid.Parse(networkID)
	if err != nil {
		return nil, fmt.Errorf("invalid network ID format: %w", err)
	}

	// Get network questions first, then get all runs for each question
	questions, err := s.repos.GeoQuestionRepo.GetByNetwork(ctx, networkUUID)
	if err != nil {
		return nil, fmt.Errorf("failed to get network questions: %w", err)
	}

	// Get all question runs for each question
	var allQuestionRuns []*models.QuestionRun
	for _, question := range questions {
		runs, err := s.repos.QuestionRunRepo.GetByQuestion(ctx, question.GeoQuestionID)
		if err != nil {
			fmt.Printf("[GetAllNetworkQuestionRuns] Warning: failed to get runs for question %s: %v\n", question.GeoQuestionID, err)
			continue
		}
		allQuestionRuns = append(allQuestionRuns, runs...)
	}

	// Convert to map format for workflow
	var result []map[string]interface{}
	for _, run := range allQuestionRuns {
		// Get the question text for this run
		question, err := s.repos.GeoQuestionRepo.GetByID(ctx, run.GeoQuestionID)
		if err != nil {
			fmt.Printf("[GetAllNetworkQuestionRuns] Warning: failed to get question for run %s: %v\n", run.QuestionRunID, err)
			continue
		}

		responseText := ""
		if run.ResponseText != nil {
			responseText = *run.ResponseText
		}

		result = append(result, map[string]interface{}{
			"question_run_id": run.QuestionRunID.String(),
			"question_text":   question.QuestionText,
			"response_text":   responseText,
		})
	}

	fmt.Printf("[GetAllNetworkQuestionRuns] Found %d total question runs for network %s\n", len(result), networkID)
	return result, nil
}

// ProcessNetworkOrgQuestionRunWithCleanup processes a single question run for network org data extraction
// and deletes any existing eval/citation/competitor data for that org+question run before saving new results
func (s *questionRunnerService) ProcessNetworkOrgQuestionRunWithCleanup(ctx context.Context, questionRunID uuid.UUID, orgID uuid.UUID, orgName string, orgWebsites []string, questionText string, responseText string) (*NetworkOrgExtractionResult, error) {
	fmt.Printf("[ProcessNetworkOrgQuestionRunWithCleanup] Processing question run %s for org %s with cleanup\n", questionRunID, orgName)

	// Step 1: Delete existing data for this org+question run combination
	fmt.Printf("[ProcessNetworkOrgQuestionRunWithCleanup] Cleaning up existing data for org %s, question run %s\n", orgID, questionRunID)

	// Delete existing evaluations
	if err := s.repos.NetworkOrgEvalRepo.DeleteByQuestionRunAndOrg(ctx, questionRunID, orgID); err != nil {
		fmt.Printf("[ProcessNetworkOrgQuestionRunWithCleanup] Warning: failed to delete existing evaluations: %v\n", err)
	}

	// Delete existing competitors
	if err := s.repos.NetworkOrgCompetitorRepo.DeleteByQuestionRunAndOrg(ctx, questionRunID, orgID); err != nil {
		fmt.Printf("[ProcessNetworkOrgQuestionRunWithCleanup] Warning: failed to delete existing competitors: %v\n", err)
	}

	// Delete existing citations
	if err := s.repos.NetworkOrgCitationRepo.DeleteByQuestionRunAndOrg(ctx, questionRunID, orgID); err != nil {
		fmt.Printf("[ProcessNetworkOrgQuestionRunWithCleanup] Warning: failed to delete existing citations: %v\n", err)
	}

	fmt.Printf("[ProcessNetworkOrgQuestionRunWithCleanup] Cleanup completed for org %s, question run %s\n", orgID, questionRunID)

	// Step 2: Extract network org data using the data extraction service
	result, err := s.dataExtractionService.ExtractNetworkOrgData(ctx, questionRunID, orgID, orgName, orgWebsites, questionText, responseText)
	if err != nil {
		return nil, fmt.Errorf("failed to extract network org data: %w", err)
	}

	// Step 3: Store the new evaluation
	if result.Evaluation != nil {
		if err := s.repos.NetworkOrgEvalRepo.Create(ctx, result.Evaluation); err != nil {
			return nil, fmt.Errorf("failed to store evaluation: %w", err)
		}
	}

	// Step 4: Store new competitors
	if len(result.Competitors) > 0 {
		for _, competitor := range result.Competitors {
			if err := s.repos.NetworkOrgCompetitorRepo.Create(ctx, competitor); err != nil {
				return nil, fmt.Errorf("failed to store competitor: %w", err)
			}
		}
	}

	// Step 5: Store new citations
	if len(result.Citations) > 0 {
		for _, citation := range result.Citations {
			if err := s.repos.NetworkOrgCitationRepo.Create(ctx, citation); err != nil {
				return nil, fmt.Errorf("failed to store citation: %w", err)
			}
		}
	}

	fmt.Printf("[ProcessNetworkOrgQuestionRunWithCleanup] Successfully processed question run %s: 1 evaluation, %d competitors, %d citations\n",
		questionRunID, len(result.Competitors), len(result.Citations))

	return result, nil
}
