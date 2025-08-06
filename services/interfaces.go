// services/interfaces.go
package services

import (
	"context"
	"time"

	"github.com/AI-Template-SDK/senso-api/pkg/database"
	"github.com/AI-Template-SDK/senso-api/pkg/models"
	"github.com/AI-Template-SDK/senso-api/pkg/repositories/interfaces"
	"github.com/AI-Template-SDK/senso-api/pkg/repositories/postgresql"
	workflowModels "github.com/AI-Template-SDK/senso-workflows/internal/models"
	"github.com/google/uuid"
	"github.com/invopop/jsonschema"
)

// RepositoryManager manages all database repositories
type RepositoryManager struct {
	OrgRepo         interfaces.OrgRepository
	GeoQuestionRepo interfaces.GeoQuestionRepository
	GeoModelRepo    interfaces.GeoModelRepository
	OrgLocationRepo interfaces.OrgLocationRepository
	OrgWebsiteRepo  interfaces.OrgWebsiteRepository
	GeoProfileRepo  interfaces.GeoProfileRepository
	QuestionRunRepo interfaces.QuestionRunRepository
	MentionRepo     interfaces.QuestionRunMentionRepository
	ClaimRepo       interfaces.QuestionRunClaimRepository
	CitationRepo    interfaces.QuestionRunCitationRepository
}

// NewRepositoryManager creates a new repository manager with all repositories
func NewRepositoryManager(db *database.Client) *RepositoryManager {
	return &RepositoryManager{
		OrgRepo:         postgresql.NewOrgRepo(db),
		GeoQuestionRepo: postgresql.NewGeoQuestionRepo(db),
		GeoModelRepo:    postgresql.NewGeoModelRepo(db),
		OrgLocationRepo: postgresql.NewOrgLocationRepo(db),
		OrgWebsiteRepo:  postgresql.NewOrgWebsiteRepo(db),
		GeoProfileRepo:  postgresql.NewGeoProfileRepo(db),
		QuestionRunRepo: postgresql.NewQuestionRunRepo(db),
		MentionRepo:     postgresql.NewQuestionRunMentionRepo(db),
		ClaimRepo:       postgresql.NewQuestionRunClaimRepo(db),
		CitationRepo:    postgresql.NewQuestionRunCitationRepo(db),
	}
}

// RealOrgDetails contains complete organization data from database
type RealOrgDetails struct {
	Org           *models.Org
	Models        []*models.GeoModel
	Locations     []*models.OrgLocation
	Questions     []interfaces.GeoQuestionWithTags
	TargetCompany string // From geo profile
	Profiles      []*models.GeoProfile
	Websites      []string // Organization website URLs for citation classification
}

// CompetitiveMetrics contains calculated competitive intelligence metrics
type CompetitiveMetrics struct {
	TargetMentioned bool
	ShareOfVoice    *float64
	TargetRank      *int
	TargetSentiment *float64
}

// ExtractedData contains all extracted data from AI responses
type ExtractedData struct {
	Mentions  []*models.QuestionRunMention
	Claims    []*models.QuestionRunClaim
	Citations []*models.QuestionRunCitation
}

// AIProvider interface for different AI models
type AIProvider interface {
	RunQuestion(ctx context.Context, question string, webSearch bool, location *workflowModels.Location) (*AIResponse, error)
}

type AIResponse struct {
	Response     string
	InputTokens  int
	OutputTokens int
	Cost         float64
}

// Updated OrgService interface for database operations
type OrgService interface {
	GetOrgDetails(ctx context.Context, orgID string) (*RealOrgDetails, error)
	GetOrgsByCreationWeekday(ctx context.Context, weekday time.Weekday) ([]*workflowModels.OrgSummary, error)
	GetOrgsScheduledForDate(ctx context.Context, date time.Time) ([]string, error)
	GetOrgCountByWeekday(ctx context.Context) (map[string]int, error)
}

// Updated QuestionRunnerService interface for database persistence
type QuestionRunnerService interface {
	RunQuestionMatrix(ctx context.Context, orgDetails *RealOrgDetails) ([]*models.QuestionRun, error)
	ProcessSingleQuestion(ctx context.Context, question *models.GeoQuestion, model *models.GeoModel, location *models.OrgLocation, targetCompany string, orgWebsites []string) (*models.QuestionRun, error)
}

// New DataExtractionService interface for parsing AI responses
type DataExtractionService interface {
	ExtractMentions(ctx context.Context, questionRunID uuid.UUID, response string, targetCompany string) ([]*models.QuestionRunMention, error)
	ExtractClaims(ctx context.Context, questionRunID uuid.UUID, response string, targetCompany string, orgWebsites []string) ([]*models.QuestionRunClaim, error)
	ExtractCitations(ctx context.Context, claims []*models.QuestionRunClaim, response string, orgWebsites []string) ([]*models.QuestionRunCitation, error)
	CalculateMetrics(ctx context.Context, mentions []*models.QuestionRunMention, response string, targetCompany string) (*CompetitiveMetrics, error)
}

// Updated AnalyticsService interface for database-driven analytics
type AnalyticsService interface {
	CalculateAnalytics(ctx context.Context, orgID uuid.UUID, startDate, endDate time.Time) (*workflowModels.Analytics, error)
	PushAnalytics(ctx context.Context, orgID string, analytics *workflowModels.Analytics) (*workflowModels.PushResult, error)
}

type CostService interface {
	CalculateCost(provider, model string, inputTokens, outputTokens int, webSearch bool) float64
}

type ExtractService interface {
	ExtractCompanyMentions(ctx context.Context, question string, response string, targetCompany string, orgID string) (*workflowModels.ExtractResult, error)
}

// Structured output types for AI extraction
type MentionsExtractionResponse struct {
	TargetCompany *CompanyExtract  `json:"target_company"`
	Competitors   []CompanyExtract `json:"competitors"`
}

type CompanyExtract struct {
	Name          string `json:"name"`
	Rank          int    `json:"rank"`
	MentionedText string `json:"mentioned_text"`
	TextSentiment string `json:"text_sentiment"`
}

type ClaimsExtractionResponse struct {
	Claims []ClaimExtract `json:"claims"`
}

type ClaimExtract struct {
	ClaimText       string `json:"claim_text"`
	Sentiment       string `json:"sentiment"`        // "positive", "negative", "neutral"
	TargetMentioned bool   `json:"target_mentioned"` // true if target company is mentioned in this claim
}

type CitationsExtractionResponse struct {
	Citations []CitationExtract `json:"citations"`
}

type CitationExtract struct {
	SourceURL *string `json:"source_url"`
	Type      string  `json:"type"`
}

// GenerateSchema generates JSON schema for structured outputs
func GenerateSchema[T any]() interface{} {
	reflector := jsonschema.Reflector{
		AllowAdditionalProperties: false,
		DoNotReference:            true,
	}
	var v T
	schema := reflector.Reflect(v)
	return schema
}
