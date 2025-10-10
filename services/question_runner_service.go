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
		Region:  location.RegionName,
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
	}

	// BrightData ChatGPT provider
	if strings.Contains(modelLower, "chatgpt") {
		fmt.Printf("[getProvider] 🎯 Selected BrightData ChatGPT provider for model: %s", model)
		return NewBrightDataProvider(s.cfg, model, s.costService), nil
	}

	// Perplexity provider (via BrightData)
	if strings.Contains(modelLower, "perplexity") {
		fmt.Printf("[getProvider] 🎯 Selected Perplexity provider for model: %s", model)
		return NewPerplexityProvider(s.cfg, model, s.costService), nil
	}

	// Gemini provider (via BrightData)
	if strings.Contains(modelLower, "gemini") {
		fmt.Printf("[getProvider] 🎯 Selected Gemini provider for model: %s", model)
		return NewGeminiProvider(s.cfg, model, s.costService), nil
	}

	// OpenAI provider (gpt-4.1, etc.)
	if strings.Contains(modelLower, "gpt") || strings.Contains(modelLower, "4.1") {
		if s.cfg.OpenAIAPIKey == "" {
			return nil, fmt.Errorf("OpenAI API key is empty in config")
		}
		fmt.Printf("[getProvider] 🎯 Selected OpenAI provider for model: %s", model)
		return NewOpenAIProvider(s.cfg, model, s.costService), nil
	}

	// Anthropic provider
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

		// Process the question run (with cleanup to prevent duplicates)
		result, err := s.ProcessNetworkOrgQuestionRunWithCleanup(ctx, questionRunUUID, orgUUID, orgDetails.OrgName, orgDetails.Websites, questionText, responseText)
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
// Returns ALL latest runs across all models and locations (multiple runs per question)
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

	// Get ALL latest question runs for each question (one per model×location combination)
	var questionRuns []*models.QuestionRun
	for _, question := range questions {
		// Get all runs for this question
		allRuns, err := s.repos.QuestionRunRepo.GetByQuestion(ctx, question.GeoQuestionID)
		if err != nil {
			fmt.Printf("[GetLatestNetworkQuestionRuns] Warning: failed to get runs for question %s: %v\n", question.GeoQuestionID, err)
			continue
		}

		// Filter for only the latest runs (is_latest=true)
		for _, run := range allRuns {
			if run.IsLatest {
				questionRuns = append(questionRuns, run)
			}
		}
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

	fmt.Printf("[GetLatestNetworkQuestionRuns] Found %d latest question runs across all models/locations for network %s\n", len(result), networkID)
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

// GetMissingNetworkOrgQuestionRuns fetches all question runs for a network that don't have network_org_eval records for the given org
// Uses efficient single-query approach via repository method
func (s *questionRunnerService) GetMissingNetworkOrgQuestionRuns(ctx context.Context, networkID string, orgID string) ([]map[string]interface{}, error) {
	// Parse IDs to UUID
	networkUUID, err := uuid.Parse(networkID)
	if err != nil {
		return nil, fmt.Errorf("invalid network ID format: %w", err)
	}
	orgUUID, err := uuid.Parse(orgID)
	if err != nil {
		return nil, fmt.Errorf("invalid org ID format: %w", err)
	}

	fmt.Printf("[GetMissingNetworkOrgQuestionRuns] Finding missing evaluations for network %s, org %s\n", networkID, orgID)

	// Use efficient repository method to get all missing question runs in a single query
	fmt.Printf("[GetMissingNetworkOrgQuestionRuns] Calling NetworkOrgEvalRepo.GetMissingQuestionRunsForOrg...\n")
	missingRuns, err := s.repos.NetworkOrgEvalRepo.GetMissingQuestionRunsForOrg(ctx, networkUUID, orgUUID)
	if err != nil {
		fmt.Printf("[GetMissingNetworkOrgQuestionRuns] ❌ ERROR from GetMissingQuestionRunsForOrg: %v\n", err)
		fmt.Printf("[GetMissingNetworkOrgQuestionRuns] Error type: %T\n", err)
		fmt.Printf("[GetMissingNetworkOrgQuestionRuns] Network UUID: %s, Org UUID: %s\n", networkUUID, orgUUID)
		return nil, fmt.Errorf("failed to get missing question runs: %w", err)
	}
	fmt.Printf("[GetMissingNetworkOrgQuestionRuns] ✅ Repository call successful, got %d results\n", len(missingRuns))

	// Convert to map format for workflow
	fmt.Printf("[GetMissingNetworkOrgQuestionRuns] Converting %d results to map format...\n", len(missingRuns))
	var result []map[string]interface{}
	for i, run := range missingRuns {
		if run == nil {
			fmt.Printf("[GetMissingNetworkOrgQuestionRuns] ⚠️ Warning: nil run at index %d\n", i)
			continue
		}

		responseText := ""
		if run.ResponseText != nil {
			responseText = *run.ResponseText
		}

		result = append(result, map[string]interface{}{
			"question_run_id": run.QuestionRunID.String(),
			"question_text":   run.QuestionText,
			"response_text":   responseText,
		})
	}

	fmt.Printf("[GetMissingNetworkOrgQuestionRuns] ✅ Successfully found %d question runs missing evaluations for org %s\n",
		len(result), orgID)
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

// GetNetworkDetails fetches complete network data including models, locations, and questions
func (s *questionRunnerService) GetNetworkDetails(ctx context.Context, networkID string) (*NetworkDetails, error) {
	fmt.Printf("[GetNetworkDetails] Fetching network details for network: %s\n", networkID)

	// Parse networkID to UUID
	networkUUID, err := uuid.Parse(networkID)
	if err != nil {
		return nil, fmt.Errorf("invalid network ID format: %w", err)
	}

	// TODO: Add NetworkRepository when available in senso-api
	// For now, create a minimal network structure
	network := &models.Network{
		NetworkID: networkUUID,
		Name:      "Network " + networkID[:8], // Truncated for display
	}

	// Get questions for this network - this method exists
	questions, err := s.repos.GeoQuestionRepo.GetByNetworkWithTags(ctx, networkUUID)
	if err != nil {
		return nil, fmt.Errorf("failed to get network questions: %w", err)
	}

	// HARDCODED: Networks run on these 3 models (gpt-4.1, chatgpt, perplexity)
	// Model IDs don't matter since they're stored as NULL in question_runs for network questions
	hardcodedModels := []*models.GeoModel{
		{GeoModelID: uuid.New(), Name: "gpt-4.1", CreatedAt: time.Now(), UpdatedAt: time.Now()},
		{GeoModelID: uuid.New(), Name: "chatgpt", CreatedAt: time.Now(), UpdatedAt: time.Now()},
		{GeoModelID: uuid.New(), Name: "perplexity", CreatedAt: time.Now(), UpdatedAt: time.Now()},
		{GeoModelID: uuid.New(), Name: "gemini", CreatedAt: time.Now(), UpdatedAt: time.Now()},
	}

	// Fetch ONE US location from database (just to get the right type, ID won't be stored)
	// This is a workaround until OrgLocation type is properly exported
	orgs, err := s.repos.OrgRepo.List(ctx, 1, 0)
	if err != nil || len(orgs) == 0 {
		return nil, fmt.Errorf("failed to get any org for location: %w", err)
	}
	allLocs, err := s.repos.OrgLocationRepo.GetByOrg(ctx, orgs[0].OrgID)
	if err != nil || len(allLocs) == 0 {
		return nil, fmt.Errorf("failed to get locations: %w", err)
	}
	// Find US location or use first
	var usLoc *models.OrgLocation
	for _, loc := range allLocs {
		if loc.CountryCode == "US" {
			usLoc = loc
			break
		}
	}
	if usLoc == nil {
		usLoc = allLocs[0]
	}
	hardcodedLocations := []*models.OrgLocation{usLoc}

	networkDetails := &NetworkDetails{
		Network:   network,
		Models:    hardcodedModels,
		Locations: hardcodedLocations,
		Questions: questions,
	}

	fmt.Printf("[GetNetworkDetails] Successfully loaded network with %d models, %d locations, %d questions\n",
		len(hardcodedModels), len(hardcodedLocations), len(questions))
	fmt.Printf("[GetNetworkDetails] NOTE: Using 3 hardcoded models + 1 US location (IDs not stored in DB for network questions)\n")

	return networkDetails, nil
}

// GetOrCreateNetworkBatch checks if a batch exists for today, returns it if so, creates new one if not
func (s *questionRunnerService) GetOrCreateNetworkBatch(ctx context.Context, networkID uuid.UUID, totalQuestions int) (*models.QuestionRunBatch, bool, error) {
	fmt.Printf("[GetOrCreateNetworkBatch] Checking for existing batch for network: %s\n", networkID)

	// Try to get all org batches and filter for network (since there's no GetByNetwork method yet)
	// We'll check recently created batches for this network
	today := time.Now().UTC()
	todayStart := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)

	// Get network questions to find batches (workaround until GetByNetwork exists on batch repo)
	questions, err := s.repos.GeoQuestionRepo.GetByNetwork(ctx, networkID)
	if err != nil {
		fmt.Printf("[GetOrCreateNetworkBatch] Warning: Failed to get network questions: %v\n", err)
	} else if len(questions) > 0 {
		// Check ALL questions' runs to find batches (not just first question)
		seenBatches := make(map[uuid.UUID]bool)
		for _, question := range questions {
			runs, err := s.repos.QuestionRunRepo.GetByQuestion(ctx, question.GeoQuestionID)
			if err != nil {
				continue
			}

			// Check batches from these runs
			for _, run := range runs {
				if run.BatchID != nil && !seenBatches[*run.BatchID] {
					seenBatches[*run.BatchID] = true
					batch, err := s.repos.QuestionRunBatchRepo.GetByID(ctx, *run.BatchID)
					if err != nil {
						fmt.Printf("[GetOrCreateNetworkBatch] ⚠️  Skipping batch %s - failed to fetch from database: %v\n", *run.BatchID, err)
						continue
					}

					// Verify batch still exists and is valid
					if batch == nil {
						fmt.Printf("[GetOrCreateNetworkBatch] ⚠️  Skipping batch %s - batch is nil\n", *run.BatchID)
						continue
					}

					// Check if this is a network batch from today for THIS network
					// Return ANY batch from today (even completed) to avoid duplicates
					if batch.NetworkID != nil && *batch.NetworkID == networkID &&
						batch.CreatedAt.After(todayStart) {
						// Double-check: verify batch actually exists in database with a fresh query
						verifyBatch, verifyErr := s.repos.QuestionRunBatchRepo.GetByID(ctx, batch.BatchID)
						if verifyErr != nil || verifyBatch == nil {
							fmt.Printf("[GetOrCreateNetworkBatch] ⚠️  Batch %s found in question runs but doesn't exist in database, skipping\n", batch.BatchID)
							continue
						}

						fmt.Printf("[GetOrCreateNetworkBatch] ✅ Found existing batch %s from today (status: %s, completed: %d/%d)\n",
							batch.BatchID, batch.Status, batch.CompletedQuestions, batch.TotalQuestions)
						return batch, true, nil
					}
				}
			}
		}
		fmt.Printf("[GetOrCreateNetworkBatch] Checked %d questions and %d unique batches, none found from today\n", len(questions), len(seenBatches))
	}

	// No existing batch found, create new one
	fmt.Printf("[GetOrCreateNetworkBatch] No existing batch found, creating new one\n")
	batch := &models.QuestionRunBatch{
		BatchID:            uuid.New(),
		Scope:              "network",
		NetworkID:          &networkID,
		BatchType:          "manual",
		Status:             "pending",
		TotalQuestions:     totalQuestions,
		CompletedQuestions: 0,
		FailedQuestions:    0,
		IsLatest:           true,
	}

	if err := s.repos.QuestionRunBatchRepo.Create(ctx, batch); err != nil {
		return nil, false, fmt.Errorf("failed to create batch: %w", err)
	}

	fmt.Printf("[GetOrCreateNetworkBatch] Created new batch %s with %d total questions\n", batch.BatchID, totalQuestions)
	return batch, false, nil
}

// StartNetworkBatch updates batch status to running and sets started_at timestamp
func (s *questionRunnerService) StartNetworkBatch(ctx context.Context, batchID uuid.UUID) error {
	fmt.Printf("[StartNetworkBatch] Starting batch: %s\n", batchID)

	// Fetch existing batch
	batch, err := s.repos.QuestionRunBatchRepo.GetByID(ctx, batchID)
	if err != nil {
		return fmt.Errorf("failed to fetch batch: %w", err)
	}

	// Update fields
	now := time.Now()
	batch.Status = "running"
	batch.StartedAt = &now
	batch.UpdatedAt = now

	// Save updated batch
	if err := s.repos.QuestionRunBatchRepo.Update(ctx, batch); err != nil {
		return fmt.Errorf("failed to start batch: %w", err)
	}

	fmt.Printf("[StartNetworkBatch] ✅ Batch %s marked as running\n", batchID)
	return nil
}

// UpdateNetworkBatchProgress updates the question counts in the batch
func (s *questionRunnerService) UpdateNetworkBatchProgress(ctx context.Context, batchID uuid.UUID, completedCount, failedCount int) error {
	fmt.Printf("[UpdateNetworkBatchProgress] Updating batch %s: completed=%d, failed=%d\n", batchID, completedCount, failedCount)

	// Fetch existing batch
	batch, err := s.repos.QuestionRunBatchRepo.GetByID(ctx, batchID)
	if err != nil {
		return fmt.Errorf("failed to fetch batch: %w", err)
	}

	// Update fields
	batch.CompletedQuestions = completedCount
	batch.FailedQuestions = failedCount
	batch.UpdatedAt = time.Now()

	// Save updated batch
	if err := s.repos.QuestionRunBatchRepo.Update(ctx, batch); err != nil {
		return fmt.Errorf("failed to update batch progress: %w", err)
	}

	fmt.Printf("[UpdateNetworkBatchProgress] ✅ Batch %s progress updated\n", batchID)
	return nil
}

// CompleteNetworkBatch marks batch as completed and sets completion timestamp
func (s *questionRunnerService) CompleteNetworkBatch(ctx context.Context, batchID uuid.UUID, totalProcessed int, totalFailed int) error {
	fmt.Printf("[CompleteNetworkBatch] Completing batch: %s (processed=%d, failed=%d)\n", batchID, totalProcessed, totalFailed)

	// Fetch existing batch
	batch, err := s.repos.QuestionRunBatchRepo.GetByID(ctx, batchID)
	if err != nil {
		return fmt.Errorf("failed to fetch batch: %w", err)
	}

	// Update fields
	now := time.Now()
	batch.Status = "completed"
	batch.CompletedQuestions = totalProcessed
	batch.FailedQuestions = totalFailed
	batch.CompletedAt = &now
	batch.UpdatedAt = now

	// Save updated batch
	if err := s.repos.QuestionRunBatchRepo.Update(ctx, batch); err != nil {
		return fmt.Errorf("failed to complete batch: %w", err)
	}

	fmt.Printf("[CompleteNetworkBatch] ✅ Batch %s marked as completed\n", batchID)
	return nil
}

// CheckQuestionRunExists checks if a question run already exists for the given question/model/location/batch
// For network questions, we check run_model and run_country (not the UUID fields)
func (s *questionRunnerService) CheckQuestionRunExists(ctx context.Context, questionID uuid.UUID, modelName, countryCode string, batchID uuid.UUID) (*models.QuestionRun, error) {
	// Get all runs for this question
	runs, err := s.repos.QuestionRunRepo.GetByQuestion(ctx, questionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get question runs: %w", err)
	}

	// Look for a run that matches this batch AND model AND location
	// For network questions: we check run_model, run_country (string fields), not model_id/location_id (which are NULL)
	for _, run := range runs {
		if run.BatchID != nil && *run.BatchID == batchID &&
			run.RunModel != nil && *run.RunModel == modelName &&
			run.RunCountry != nil && *run.RunCountry == countryCode {
			// Found exact match: same batch, same model, same country
			return run, nil
		}
	}

	return nil, nil
}

// RunNetworkQuestionMatrix executes all network questions across models and locations with batching support
func (s *questionRunnerService) RunNetworkQuestionMatrix(ctx context.Context, networkDetails *NetworkDetails, batchID uuid.UUID) (*NetworkProcessingSummary, error) {
	fmt.Printf("[RunNetworkQuestionMatrix] 🚀 Starting question matrix for network: %s (ID: %s)\n",
		networkDetails.Network.Name, networkDetails.Network.NetworkID)
	fmt.Printf("[RunNetworkQuestionMatrix] 📋 Processing %d questions across %d models and %d locations\n",
		len(networkDetails.Questions), len(networkDetails.Models), len(networkDetails.Locations))

	summary := &NetworkProcessingSummary{
		ProcessingErrors: make([]string, 0),
	}

	// Create model-location pairs
	pairs := s.createModelLocationPairs(networkDetails.Models, networkDetails.Locations)
	fmt.Printf("[RunNetworkQuestionMatrix] Created %d model-location pairs\n", len(pairs))

	// Process each model-location pair
	allQuestionRuns := make([]*models.QuestionRun, 0)
	for pairIdx, pair := range pairs {
		fmt.Printf("[RunNetworkQuestionMatrix] 📦 Processing pair %d/%d: model=%s, location=%s\n",
			pairIdx+1, len(pairs), pair.Model.Name, pair.Location.CountryCode)

		// Get provider for this model
		provider, err := s.getProvider(pair.Model.Name)
		if err != nil {
			summary.ProcessingErrors = append(summary.ProcessingErrors,
				fmt.Sprintf("Failed to get provider for model %s: %v", pair.Model.Name, err))
			continue
		}

		// Execute questions for this pair (batched or sequential)
		questionRuns, err := s.executeQuestionsForPair(ctx, networkDetails.Questions, pair, provider, batchID, summary)
		if err != nil {
			summary.ProcessingErrors = append(summary.ProcessingErrors,
				fmt.Sprintf("Failed to execute questions for model %s, location %s: %v", pair.Model.Name, pair.Location.CountryCode, err))
			continue
		}

		allQuestionRuns = append(allQuestionRuns, questionRuns...)
		fmt.Printf("[RunNetworkQuestionMatrix] ✅ Completed pair %d/%d: created %d question runs\n",
			pairIdx+1, len(pairs), len(questionRuns))
	}

	fmt.Printf("[RunNetworkQuestionMatrix] 🎉 Question matrix completed: %d processed, $%.6f total cost\n",
		summary.TotalProcessed, summary.TotalCost)

	// Update is_latest flags for all created question runs
	if len(allQuestionRuns) > 0 {
		if err := s.updateNetworkLatestFlagsForRuns(ctx, networkDetails.Questions, allQuestionRuns); err != nil {
			return nil, fmt.Errorf("failed to update latest flags: %w", err)
		}
		fmt.Printf("[RunNetworkQuestionMatrix] ✅ Updated is_latest flags for %d question runs\n", len(allQuestionRuns))
	}

	return summary, nil
}

// createModelLocationPairs creates all unique combinations of models and locations
func (s *questionRunnerService) createModelLocationPairs(models []*models.GeoModel, locations []*models.OrgLocation) []ModelLocationPair {
	pairs := make([]ModelLocationPair, 0, len(models)*len(locations))
	for _, model := range models {
		for _, location := range locations {
			pairs = append(pairs, ModelLocationPair{
				Model:    model,
				Location: location,
			})
		}
	}
	return pairs
}

// executeQuestionsForPair executes all questions for a specific model-location pair
func (s *questionRunnerService) executeQuestionsForPair(
	ctx context.Context,
	questions []interfaces.GeoQuestionWithTags,
	pair ModelLocationPair,
	provider AIProvider,
	batchID uuid.UUID,
	summary *NetworkProcessingSummary,
) ([]*models.QuestionRun, error) {
	var questionRuns []*models.QuestionRun

	// Convert location to workflow model format
	workflowLocation := &workflowModels.Location{
		Country: pair.Location.CountryCode,
		Region:  pair.Location.RegionName,
	}

	if provider.SupportsBatching() {
		// Batch processing for BrightData/Perplexity
		maxBatchSize := provider.GetMaxBatchSize()
		fmt.Printf("[executeQuestionsForPair] 🔄 Provider supports batching (max size: %d)\n", maxBatchSize)

		// Process questions in batches
		for i := 0; i < len(questions); i += maxBatchSize {
			end := i + maxBatchSize
			if end > len(questions) {
				end = len(questions)
			}
			batch := questions[i:end]

			fmt.Printf("[executeQuestionsForPair] 📦 Processing batch %d-%d of %d questions\n", i+1, end, len(questions))

			// Execute batch
			runs, err := s.executeBatchForNetwork(ctx, batch, pair, provider, workflowLocation, batchID, summary)
			if err != nil {
				// Log error but continue with next batch instead of failing entirely
				errMsg := fmt.Sprintf("Failed to execute batch %d-%d for model %s, location %s: %v", i+1, end, pair.Model.Name, pair.Location.CountryCode, err)
				fmt.Printf("[executeQuestionsForPair] ❌ %s\n", errMsg)
				summary.ProcessingErrors = append(summary.ProcessingErrors, errMsg)
				continue // Continue with next batch
			}

			questionRuns = append(questionRuns, runs...)
		}
	} else {
		// Sequential processing for OpenAI/Anthropic
		fmt.Printf("[executeQuestionsForPair] 🔄 Provider does not support batching, processing sequentially\n")

		for idx, questionWithTags := range questions {
			question := questionWithTags.Question
			fmt.Printf("[executeQuestionsForPair] 📝 Processing question %d/%d: %s\n",
				idx+1, len(questions), question.QuestionText)

			// Execute single question
			run, err := s.executeSingleNetworkQuestion(ctx, question, pair, provider, workflowLocation, batchID, summary)
			if err != nil {
				summary.ProcessingErrors = append(summary.ProcessingErrors,
					fmt.Sprintf("Failed to execute question %s: %v", question.GeoQuestionID, err))
				continue
			}

			// run might be nil if the question failed (expected failure)
			if run != nil {
				questionRuns = append(questionRuns, run)
			}
		}
	}

	return questionRuns, nil
}

// executeBatchForNetwork executes a batch of questions using the provider's batch API
func (s *questionRunnerService) executeBatchForNetwork(
	ctx context.Context,
	batch []interfaces.GeoQuestionWithTags,
	pair ModelLocationPair,
	provider AIProvider,
	workflowLocation *workflowModels.Location,
	batchID uuid.UUID,
	summary *NetworkProcessingSummary,
) ([]*models.QuestionRun, error) {
	// Check which questions need to be executed (filter out existing ones)
	questionsToExecute := make([]interfaces.GeoQuestionWithTags, 0)
	existingRuns := make([]*models.QuestionRun, 0)

	for _, questionWithTags := range batch {
		question := questionWithTags.Question

		// Check if question run already exists for this specific model+location combination
		existingRun, err := s.CheckQuestionRunExists(ctx, question.GeoQuestionID, pair.Model.Name, pair.Location.CountryCode, batchID)
		if err != nil {
			fmt.Printf("[executeBatchForNetwork] Warning: Failed to check for existing run: %v\n", err)
			questionsToExecute = append(questionsToExecute, questionWithTags)
			continue
		}

		if existingRun != nil {
			fmt.Printf("[executeBatchForNetwork] ✓ Skipping question %s - already executed\n", question.GeoQuestionID)
			existingRuns = append(existingRuns, existingRun)
		} else {
			questionsToExecute = append(questionsToExecute, questionWithTags)
		}
	}

	// If all questions already exist, return existing runs
	if len(questionsToExecute) == 0 {
		fmt.Printf("[executeBatchForNetwork] All %d questions already executed, skipping batch API call\n", len(batch))
		return existingRuns, nil
	}

	fmt.Printf("[executeBatchForNetwork] Executing %d new questions (skipped %d existing)\n", len(questionsToExecute), len(existingRuns))

	// Extract query strings from questions that need execution
	queries := make([]string, len(questionsToExecute))
	for i, q := range questionsToExecute {
		queries[i] = q.Question.QuestionText
	}

	fmt.Printf("[executeBatchForNetwork] 🚀 Calling provider.RunQuestionBatch with %d queries\n", len(queries))

	// Execute batch API call
	responses, err := provider.RunQuestionBatch(ctx, queries, true, workflowLocation)
	if err != nil {
		fmt.Printf("[executeBatchForNetwork] ❌ Batch API call failed: %v\n", err)
		return nil, fmt.Errorf("batch API call failed: %w", err)
	}

	fmt.Printf("[executeBatchForNetwork] ✅ Batch API call succeeded, got %d responses\n", len(responses))

	if len(responses) != len(questionsToExecute) {
		errMsg := fmt.Sprintf("batch returned %d responses but expected %d", len(responses), len(questionsToExecute))
		fmt.Printf("[executeBatchForNetwork] ❌ %s\n", errMsg)
		return nil, fmt.Errorf("%s", errMsg)
	}

	// Create and store new question runs (skip failed ones)
	newQuestionRuns := make([]*models.QuestionRun, 0, len(questionsToExecute))
	for i, questionWithTags := range questionsToExecute {
		question := questionWithTags.Question
		aiResponse := responses[i]

		// Skip failed runs - don't save to DB
		if !aiResponse.ShouldProcessEvaluation {
			errorMsg := fmt.Sprintf("Question %s (%s) failed for model %s, location %s: %s",
				question.GeoQuestionID, question.QuestionText, pair.Model.Name, pair.Location.CountryCode, aiResponse.Response)
			summary.ProcessingErrors = append(summary.ProcessingErrors, errorMsg)
			fmt.Printf("[executeBatchForNetwork] ⚠️ Skipping failed question run: %s\n", errorMsg)
			continue
		}

		// For network questions: ModelID and LocationID are NULL
		// We only store RunModel, RunCountry, RunRegion as strings for informational purposes
		questionRun := &models.QuestionRun{
			QuestionRunID: uuid.New(),
			GeoQuestionID: question.GeoQuestionID,
			// ModelID:       nil,  // NULL for network questions
			// LocationID:    nil,  // NULL for network questions
			ResponseText: &aiResponse.Response,
			InputTokens:  &aiResponse.InputTokens,
			OutputTokens: &aiResponse.OutputTokens,
			TotalCost:    &aiResponse.Cost,
			BatchID:      &batchID,
			RunModel:     &pair.Model.Name,
			RunCountry:   &pair.Location.CountryCode,
			RunRegion:    pair.Location.RegionName,
			IsLatest:     true,
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		}

		// Store in database
		if err := s.repos.QuestionRunRepo.Create(ctx, questionRun); err != nil {
			return nil, fmt.Errorf("failed to store question run: %w", err)
		}

		newQuestionRuns = append(newQuestionRuns, questionRun)
		summary.TotalProcessed++
		summary.TotalCost += aiResponse.Cost
	}

	// Combine existing and new question runs
	allQuestionRuns := append(existingRuns, newQuestionRuns...)
	fmt.Printf("[executeBatchForNetwork] ✅ Successfully created %d new question runs, returning %d total runs\n", len(newQuestionRuns), len(allQuestionRuns))
	return allQuestionRuns, nil
}

// executeSingleNetworkQuestion executes a single question (for non-batching providers)
func (s *questionRunnerService) executeSingleNetworkQuestion(
	ctx context.Context,
	question *models.GeoQuestion,
	pair ModelLocationPair,
	provider AIProvider,
	workflowLocation *workflowModels.Location,
	batchID uuid.UUID,
	summary *NetworkProcessingSummary,
) (*models.QuestionRun, error) {
	// Check if question run already exists for this specific model+location combination
	existingRun, err := s.CheckQuestionRunExists(ctx, question.GeoQuestionID, pair.Model.Name, pair.Location.CountryCode, batchID)
	if err != nil {
		fmt.Printf("[executeSingleNetworkQuestion] Warning: Failed to check for existing run: %v\n", err)
		// Continue with execution if check fails
	}

	if existingRun != nil {
		fmt.Printf("[executeSingleNetworkQuestion] ✓ Skipping question %s - already executed\n", question.GeoQuestionID)
		return existingRun, nil
	}

	// Execute AI call
	aiResponse, err := provider.RunQuestion(ctx, question.QuestionText, true, workflowLocation)
	if err != nil {
		return nil, fmt.Errorf("AI call failed: %w", err)
	}

	// Skip failed runs - don't save to DB
	if !aiResponse.ShouldProcessEvaluation {
		errorMsg := fmt.Sprintf("Question %s (%s) failed for model %s, location %s: %s",
			question.GeoQuestionID, question.QuestionText, pair.Model.Name, pair.Location.CountryCode, aiResponse.Response)
		summary.ProcessingErrors = append(summary.ProcessingErrors, errorMsg)
		fmt.Printf("[executeSingleNetworkQuestion] ⚠️ Skipping failed question run: %s\n", errorMsg)
		return nil, nil // Return nil without error - this is an expected failure
	}

	// Create question run record
	// For network questions: ModelID and LocationID are NULL
	// We only store RunModel, RunCountry, RunRegion as strings for informational purposes
	questionRun := &models.QuestionRun{
		QuestionRunID: uuid.New(),
		GeoQuestionID: question.GeoQuestionID,
		// ModelID:       nil,  // NULL for network questions
		// LocationID:    nil,  // NULL for network questions
		ResponseText: &aiResponse.Response,
		InputTokens:  &aiResponse.InputTokens,
		OutputTokens: &aiResponse.OutputTokens,
		TotalCost:    &aiResponse.Cost,
		BatchID:      &batchID,
		RunModel:     &pair.Model.Name,
		RunCountry:   &pair.Location.CountryCode,
		RunRegion:    pair.Location.RegionName,
		IsLatest:     true,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	// Store in database
	if err := s.repos.QuestionRunRepo.Create(ctx, questionRun); err != nil {
		return nil, fmt.Errorf("failed to store question run: %w", err)
	}

	summary.TotalProcessed++
	summary.TotalCost += aiResponse.Cost
	return questionRun, nil
}

// updateNetworkLatestFlagsForRuns updates is_latest flags for network question runs
func (s *questionRunnerService) updateNetworkLatestFlagsForRuns(ctx context.Context, questions []interfaces.GeoQuestionWithTags, newRuns []*models.QuestionRun) error {
	if len(newRuns) == 0 {
		return nil
	}

	// Get the batch ID from the first run (all runs should have the same batch ID)
	batchID := newRuns[0].BatchID
	if batchID == nil {
		return fmt.Errorf("question runs missing batch ID")
	}

	fmt.Printf("[updateNetworkLatestFlagsForRuns] Updating is_latest flags for batch %s with %d question runs\n", batchID, len(newRuns))

	// Step 1: Mark all old question runs (from previous batches) as is_latest=false for this network
	questionIDMap := make(map[uuid.UUID]bool)
	for _, run := range newRuns {
		questionIDMap[run.GeoQuestionID] = true
	}

	// Step 1: Mark old question runs as is_latest=false
	for questionID := range questionIDMap {
		// Get all runs for this question (to find old ones)
		allRuns, err := s.repos.QuestionRunRepo.GetByQuestion(ctx, questionID)
		if err != nil {
			fmt.Printf("[updateNetworkLatestFlagsForRuns] Warning: Failed to get runs for question %s: %v\n", questionID, err)
			continue
		}

		// Mark all old runs (not in current batch) as is_latest=false
		for _, oldRun := range allRuns {
			if oldRun.BatchID == nil || *oldRun.BatchID != *batchID {
				oldRun.IsLatest = false
				oldRun.UpdatedAt = time.Now()
				if err := s.repos.QuestionRunRepo.Update(ctx, oldRun); err != nil {
					fmt.Printf("[updateNetworkLatestFlagsForRuns] Warning: Failed to mark old run %s as not latest: %v\n", oldRun.QuestionRunID, err)
				}
			}
		}
	}

	// Step 2: Mark all question runs in the NEW batch as is_latest=true
	for _, run := range newRuns {
		run.IsLatest = true
		run.UpdatedAt = time.Now()
		if err := s.repos.QuestionRunRepo.Update(ctx, run); err != nil {
			fmt.Printf("[updateNetworkLatestFlagsForRuns] Warning: Failed to mark run %s as latest: %v\n", run.QuestionRunID, err)
		}
	}

	fmt.Printf("[updateNetworkLatestFlagsForRuns] ✅ Successfully updated is_latest flags for %d question runs in batch %s\n", len(newRuns), batchID)
	return nil
}
