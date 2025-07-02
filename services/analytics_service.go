// services/analytics_service.go
package services

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/AI-Template-SDK/senso-api/pkg/repositories/interfaces"
	"github.com/AI-Template-SDK/senso-workflows/internal/config"
	workflowModels "github.com/AI-Template-SDK/senso-workflows/internal/models"
	"github.com/google/uuid"
)

type analyticsService struct {
	cfg        *config.Config
	httpClient *http.Client
	repos      *RepositoryManager
}

func NewAnalyticsService(cfg *config.Config, repos *RepositoryManager) AnalyticsService {
	return &analyticsService{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		repos: repos,
	}
}

func (s *analyticsService) RunExtract(ctx context.Context, questionResult *workflowModels.QuestionResult, orgID string, targetCompany string) (*workflowModels.ExtractResult, error) {
	fmt.Printf("[RunExtract] Processing extract for question %s\n", questionResult.QuestionID)

	// Debug the config
	if s.cfg == nil {
		fmt.Println("[RunExtract] ERROR: Config is nil!")
	} else if s.cfg.OpenAIAPIKey == "" {
		fmt.Println("[RunExtract] ERROR: OpenAI API key is empty in config!")
	} else if s.cfg.OpenAIAPIKey == "test-api-key" {
		fmt.Println("[RunExtract] ERROR: OpenAI API key is 'test-api-key' in config!")
	} else {
		fmt.Printf("[RunExtract] Config has OpenAI API key (length: %d, first 10 chars: %s)\n",
			len(s.cfg.OpenAIAPIKey), s.cfg.OpenAIAPIKey[:10])
	}

	// Create a fresh extract service (just like question runner creates fresh providers)
	extractService := NewExtractService(s.cfg)

	// Get the best response from the question runs
	bestResponse := s.getBestResponse(questionResult)
	if bestResponse == "" {
		return &workflowModels.ExtractResult{
			QuestionID: questionResult.QuestionID,
			Error:      "No valid response found in question runs",
			Timestamp:  time.Now().UTC(),
		}, nil
	}

	// Find the original question text (using metadata or first run)
	questionText := "Unknown question"
	if runs := questionResult.Runs; len(runs) > 0 {
		// In a real implementation, you'd store the question text
		questionText = fmt.Sprintf("Question ID: %s", questionResult.QuestionID)
	}

	// Extract company mentions
	result, err := extractService.ExtractCompanyMentions(ctx, questionText, bestResponse, targetCompany, orgID)
	if err != nil {
		return nil, fmt.Errorf("failed to extract company mentions: %w", err)
	}

	// Set the question ID
	result.QuestionID = questionResult.QuestionID

	fmt.Printf("[RunExtract] Completed extract for question %s - found target: %v, competitors: %d\n",
		questionResult.QuestionID, result.TargetCompany != nil, len(result.Competitors))

	return result, nil
}

func (s *analyticsService) getBestResponse(questionResult *workflowModels.QuestionResult) string {
	// First try to use the aggregated response
	if questionResult.Response != "" {
		return questionResult.Response
	}

	// Otherwise, find the best individual run response
	// Prioritize: successful runs > with web search > by model preference
	var bestRun *workflowModels.QuestionRun
	for _, run := range questionResult.Runs {
		if run.Error != "" || run.Response == "" {
			continue
		}

		if bestRun == nil {
			bestRun = run
			continue
		}

		// Prefer web search results
		if run.WebSearch && !bestRun.WebSearch {
			bestRun = run
		}
	}

	if bestRun != nil {
		return bestRun.Response
	}

	return ""
}

// CalculateAnalytics generates analytics using real database queries
func (s *analyticsService) CalculateAnalytics(ctx context.Context, orgID uuid.UUID, startDate, endDate time.Time) (*workflowModels.Analytics, error) {
	fmt.Printf("[CalculateAnalytics] Processing analytics for org: %s\n", orgID)

	// Use existing repository analytics methods for real database metrics
	mentionsAnalytics, err := s.repos.QuestionRunRepo.GetMentionsAnalytics(ctx, orgID, startDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("failed to get mentions analytics: %w", err)
	}

	sovAnalytics, err := s.repos.QuestionRunRepo.GetShareOfVoiceAnalytics(ctx, orgID, startDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("failed to get share of voice analytics: %w", err)
	}

	competitiveAnalytics, err := s.repos.QuestionRunRepo.GetCompetitiveAnalytics(ctx, orgID)
	if err != nil {
		return nil, fmt.Errorf("failed to get competitive analytics: %w", err)
	}

	// Calculate aggregated metrics
	visibility := s.calculateVisibility(mentionsAnalytics)
	avgShareOfVoice := s.calculateAvgSOV(sovAnalytics)
	sentiment := s.calculateSentiment(competitiveAnalytics)
	totalCompetitors := float64(len(competitiveAnalytics))

	analytics := &workflowModels.Analytics{
		Metrics: map[string]float64{
			"visibility":        visibility,
			"share_of_voice":    avgShareOfVoice,
			"sentiment":         sentiment,
			"total_competitors": totalCompetitors,
		},
		Insights:  s.generateInsights(mentionsAnalytics, sovAnalytics, competitiveAnalytics),
		Timestamp: time.Now().UTC(),
	}

	fmt.Printf("[CalculateAnalytics] Analytics summary:\n")
	fmt.Printf("  - Visibility: %.1f%%\n", visibility)
	fmt.Printf("  - Share of Voice: %.1f%%\n", avgShareOfVoice)
	fmt.Printf("  - Sentiment: %.1f%%\n", sentiment)

	return analytics, nil
}

func (s *analyticsService) PushAnalytics(ctx context.Context, orgID string, analytics *workflowModels.Analytics) (*workflowModels.PushResult, error) {
	fmt.Printf("[PushAnalytics] Pushing analytics for org: %s\n", orgID)

	// Analytics have already been calculated from the database in CalculateAnalytics
	// The data is already stored in question_runs, mentions, claims, and citations tables
	// This method would typically push to an external dashboard or API

	// For now, let's log what we would push
	if analytics != nil {
		fmt.Printf("[PushAnalytics] Analytics summary:\n")
		fmt.Printf("  - Metrics: %+v\n", analytics.Metrics)
		fmt.Printf("  - Insights: %d insights generated\n", len(analytics.Insights))
		fmt.Printf("  - Timestamp: %s\n", analytics.Timestamp)
	}

	// In production, this would call an external API endpoint like:
	// POST {APPLICATION_API_URL}/api/organizations/{orgID}/analytics
	// with the analytics payload

	result := &workflowModels.PushResult{
		Success: true,
		Message: fmt.Sprintf("Analytics calculated and stored in database for org %s. External push simulated.", orgID),
	}

	fmt.Printf("[PushAnalytics] Successfully completed analytics pipeline\n")
	return result, nil
}

// Helper methods for calculating metrics

func (s *analyticsService) calculateVisibility(mentionsAnalytics []interfaces.MentionsAnalytics) float64 {
	if len(mentionsAnalytics) == 0 {
		return 0.0
	}

	totalRuns := 0
	totalMentions := 0
	for _, ma := range mentionsAnalytics {
		totalRuns += ma.TotalRuns
		totalMentions += ma.RunsWithMentions
	}

	if totalRuns == 0 {
		return 0.0
	}

	return (float64(totalMentions) / float64(totalRuns)) * 100
}

func (s *analyticsService) calculateAvgSOV(sovAnalytics []interfaces.ShareOfVoiceAnalytics) float64 {
	if len(sovAnalytics) == 0 {
		return 0.0
	}

	totalSOV := 0.0
	count := 0
	for _, sov := range sovAnalytics {
		if sov.ShareOfVoicePercentage > 0 {
			totalSOV += sov.ShareOfVoicePercentage
			count++
		}
	}

	if count == 0 {
		return 0.0
	}

	return totalSOV / float64(count)
}

func (s *analyticsService) calculateSentiment(competitiveAnalytics []interfaces.CompetitiveAnalytics) float64 {
	if len(competitiveAnalytics) == 0 {
		return 0.0
	}

	// Find the target organization's sentiment
	for _, ca := range competitiveAnalytics {
		if ca.IsTargetOrg {
			// Convert sentiment score to percentage (assuming 0.0-1.0 scale)
			return ca.AverageSentiment * 100
		}
	}

	return 0.0
}

func (s *analyticsService) generateInsights(mentionsAnalytics []interfaces.MentionsAnalytics, sovAnalytics []interfaces.ShareOfVoiceAnalytics, competitiveAnalytics []interfaces.CompetitiveAnalytics) []string {
	var insights []string

	// Visibility insights
	visibility := s.calculateVisibility(mentionsAnalytics)
	insights = append(insights, fmt.Sprintf("Target company mentioned in %.1f%% of responses (visibility)", visibility))

	// Share of voice insights
	avgSOV := s.calculateAvgSOV(sovAnalytics)
	insights = append(insights, fmt.Sprintf("Average share of voice: %.1f%% of response text", avgSOV))

	// Sentiment insights
	sentiment := s.calculateSentiment(competitiveAnalytics)
	insights = append(insights, fmt.Sprintf("Average sentiment score: %.1f%%", sentiment))

	// Competitive insights
	totalCompetitors := 0
	for _, ca := range competitiveAnalytics {
		if !ca.IsTargetOrg {
			totalCompetitors++
		}
	}
	insights = append(insights, fmt.Sprintf("Found %d competitor mentions across all responses", totalCompetitors))

	// Data quality insights
	totalQuestions := 0
	for _, ma := range mentionsAnalytics {
		totalQuestions += ma.TotalRuns
	}
	insights = append(insights, fmt.Sprintf("Analyzed %d total question runs", totalQuestions))

	return insights
}
