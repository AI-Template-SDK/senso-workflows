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
	"github.com/jmoiron/sqlx"
)

// RepositoryManager manages all database repositories
type RepositoryManager struct {
	db                       *database.Client
	OrgRepo                  interfaces.OrgRepository
	GeoQuestionRepo          interfaces.GeoQuestionRepository
	GeoModelRepo             interfaces.GeoModelRepository
	OrgLocationRepo          interfaces.OrgLocationRepository
	OrgWebsiteRepo           interfaces.OrgWebsiteRepository
	GeoProfileRepo           interfaces.GeoProfileRepository
	QuestionRunRepo          interfaces.QuestionRunRepository
	MentionRepo              interfaces.QuestionRunMentionRepository
	ClaimRepo                interfaces.QuestionRunClaimRepository
	CitationRepo             interfaces.QuestionRunCitationRepository
	NetworkOrgEvalRepo       interfaces.NetworkOrgEvalRepository
	NetworkOrgCompetitorRepo interfaces.NetworkOrgCompetitorRepository
	NetworkOrgCitationRepo   interfaces.NetworkOrgCitationRepository
	// New org evaluation repositories
	OrgEvalRepo       interfaces.OrgEvalRepository
	OrgCitationRepo   interfaces.OrgCitationRepository
	OrgCompetitorRepo interfaces.OrgCompetitorRepository
	// Question run batch repository
	QuestionRunBatchRepo interfaces.QuestionRunBatchRepository
	// Credit ledger repository
	CreditLedgerRepo interfaces.CreditLedgerRepository
	// ** ADDED: Credit balance repository **
	CreditBalanceRepo interfaces.CreditBalanceRepository
	// Organization schedule repository
	OrgScheduleRepo interfaces.OrgScheduleRepository
	// Network schedule repository
	NetworkScheduleRepo interfaces.NetworkScheduleRepository
}

// NewRepositoryManager creates a new repository manager with all repositories
func NewRepositoryManager(db *database.Client) *RepositoryManager {
	return &RepositoryManager{
		db:                       db,
		OrgRepo:                  postgresql.NewOrgRepo(db),
		GeoQuestionRepo:          postgresql.NewGeoQuestionRepo(db),
		GeoModelRepo:             postgresql.NewGeoModelRepo(db),
		OrgLocationRepo:          postgresql.NewOrgLocationRepo(db),
		OrgWebsiteRepo:           postgresql.NewOrgWebsiteRepo(db),
		GeoProfileRepo:           postgresql.NewGeoProfileRepo(db),
		QuestionRunRepo:          postgresql.NewQuestionRunRepo(db),
		MentionRepo:              postgresql.NewQuestionRunMentionRepo(db),
		ClaimRepo:                postgresql.NewQuestionRunClaimRepo(db),
		CitationRepo:             postgresql.NewQuestionRunCitationRepo(db),
		NetworkOrgEvalRepo:       postgresql.NewNetworkOrgEvalRepo(db),
		NetworkOrgCompetitorRepo: postgresql.NewNetworkOrgCompetitorRepo(db),
		NetworkOrgCitationRepo:   postgresql.NewNetworkOrgCitationRepo(db),
		// New org evaluation repositories
		OrgEvalRepo:       postgresql.NewOrgEvalRepo(db),
		OrgCitationRepo:   postgresql.NewOrgCitationRepo(db),
		OrgCompetitorRepo: postgresql.NewOrgCompetitorRepo(db),
		// Question run batch repository
		QuestionRunBatchRepo: postgresql.NewQuestionRunBatchRepo(db),
		// Credit ledger repository
		CreditLedgerRepo: postgresql.NewCreditLedgerRepo(db),
		// ** ADDED: Credit balance repository **
		CreditBalanceRepo: postgresql.NewCreditBalanceRepo(db),
		// Organization schedule repository
		OrgScheduleRepo: postgresql.NewOrgScheduleRepo(db),
		// Network schedule repository
		NetworkScheduleRepo: postgresql.NewNetworkScheduleRepo(db),
	}
}

// BeginTx starts a database transaction
func (rm *RepositoryManager) BeginTx(ctx context.Context) (*sqlx.Tx, error) {
	return rm.db.BeginTxx(ctx, nil)
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

// NetworkDetails contains complete network data from database
type NetworkDetails struct {
	Network   *models.Network
	Models    []*models.GeoModel
	Locations []*models.OrgLocation // Networks can use same location structure as orgs
	Questions []interfaces.GeoQuestionWithTags
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
	RunQuestion(ctx context.Context, query string, websearch bool, location *workflowModels.Location) (*AIResponse, error)
	RunQuestionWebSearch(ctx context.Context, query string) (*AIResponse, error)

	// Batch processing support
	SupportsBatching() bool
	GetMaxBatchSize() int
	RunQuestionBatch(ctx context.Context, queries []string, websearch bool, location *workflowModels.Location) ([]*AIResponse, error)
}

// AIResponse contains the response from an AI provider
type AIResponse struct {
	Response                string
	InputTokens             int
	OutputTokens            int
	Cost                    float64
	Citations               []string
	ShouldProcessEvaluation bool
}

// NetworkOrgProcessingResult represents the result of processing network org data
type NetworkOrgProcessingResult struct {
	OrgID        string
	NetworkID    string
	QuestionRuns int
	Evaluations  int
	Competitors  int
	Citations    int
	Status       string
	Error        error
}

// NetworkOrgExtractionResult represents the extracted data for a network org (with cost tracking)
type NetworkOrgExtractionResult struct {
	Evaluation   *models.NetworkOrgEval
	Competitors  []*models.NetworkOrgCompetitor
	Citations    []*models.NetworkOrgCitation
	InputTokens  int     // Total input tokens used (from all AI calls)
	OutputTokens int     // Total output tokens used (from all AI calls)
	TotalCost    float64 // Total cost of all AI calls
}

// NetworkOrgEvaluationResult represents the result of extracting network org evaluation
type NetworkOrgEvaluationResult struct {
	Evaluation   *models.NetworkOrgEval
	InputTokens  int
	OutputTokens int
	TotalCost    float64
}

// NetworkOrgCompetitorResult represents the result of extracting network org competitors
type NetworkOrgCompetitorResult struct {
	Competitors  []*models.NetworkOrgCompetitor
	InputTokens  int
	OutputTokens int
	TotalCost    float64
}

// NetworkOrgCitationResult represents the result of extracting network org citations
type NetworkOrgCitationResult struct {
	Citations    []*models.NetworkOrgCitation
	InputTokens  int
	OutputTokens int
	TotalCost    float64
}

// OrgDetailsForNetworkProcessing contains org details needed for network processing
type OrgDetailsForNetworkProcessing struct {
	OrgID     string
	OrgName   string
	NetworkID string
	Websites  []string
}

// OrgService interface for organization operations
type OrgService interface {
	GetOrgDetails(ctx context.Context, orgID string) (*RealOrgDetails, error)
	GetOrgsByCreationWeekday(ctx context.Context, weekday time.Weekday) ([]*workflowModels.OrgSummary, error)
	GetOrgIDsByScheduledDOW(ctx context.Context, dow int) ([]uuid.UUID, error)
	GetOrgsScheduledForDate(ctx context.Context, date time.Time) ([]string, error)
	GetOrgCountByWeekday(ctx context.Context) (map[string]int, error)
}

// Updated QuestionRunnerService interface for database persistence
type QuestionRunnerService interface {
	RunQuestionMatrix(ctx context.Context, orgDetails *RealOrgDetails) ([]*models.QuestionRun, error)
	ProcessSingleQuestion(ctx context.Context, question *models.GeoQuestion, model *models.GeoModel, location *models.OrgLocation, targetCompany string, orgWebsites []string) (*models.QuestionRun, error)
	RunNetworkQuestionsQuestionOnly(ctx context.Context, networkID string) ([]*models.QuestionRun, error)
	GetNetworkQuestions(ctx context.Context, networkID string) ([]*models.GeoQuestion, error)
	ProcessNetworkQuestionOnly(ctx context.Context, question *models.GeoQuestion) (*models.QuestionRun, error)
	UpdateNetworkLatestFlags(ctx context.Context, networkID string) error
	RunNetworkOrgProcessing(ctx context.Context, orgID string) ([]*NetworkOrgProcessingResult, error)
	GetOrgDetailsForNetworkProcessing(ctx context.Context, orgID string) (*OrgDetailsForNetworkProcessing, error)
	GetLatestNetworkQuestionRuns(ctx context.Context, networkID string) ([]map[string]interface{}, error)
	GetAllNetworkQuestionRuns(ctx context.Context, networkID string) ([]map[string]interface{}, error)
	GetMissingNetworkOrgQuestionRuns(ctx context.Context, networkID string, orgID string) ([]map[string]interface{}, error)
	ProcessNetworkOrgQuestionRun(ctx context.Context, questionRunID uuid.UUID, orgID uuid.UUID, orgName string, orgWebsites []string, questionText string, responseText string) (*NetworkOrgExtractionResult, error)
	ProcessNetworkOrgQuestionRunWithCleanup(ctx context.Context, questionRunID uuid.UUID, orgID uuid.UUID, orgName string, orgWebsites []string, questionText string, responseText string) (*NetworkOrgExtractionResult, error)

	// Network batch processing with multi-model/location support
	GetNetworkDetails(ctx context.Context, networkID string) (*NetworkDetails, error)
	RunNetworkQuestionMatrix(ctx context.Context, networkDetails *NetworkDetails, batchID uuid.UUID) (*NetworkProcessingSummary, error)
	GetOrCreateNetworkBatch(ctx context.Context, networkID uuid.UUID, totalQuestions int) (*models.QuestionRunBatch, bool, error)
	StartNetworkBatch(ctx context.Context, batchID uuid.UUID) error
	UpdateNetworkBatchProgress(ctx context.Context, batchID uuid.UUID, completedCount, failedCount int) error
	CompleteNetworkBatch(ctx context.Context, batchID uuid.UUID, totalProcessed int, totalFailed int) error
	CheckQuestionRunExists(ctx context.Context, questionID uuid.UUID, modelName, countryCode string, batchID uuid.UUID) (*models.QuestionRun, error)
}

// New DataExtractionService interface for parsing AI responses
type DataExtractionService interface {
	ExtractMentions(ctx context.Context, questionRunID uuid.UUID, response string, targetCompany string, orgWebsites []string) ([]*models.QuestionRunMention, error)
	ExtractClaims(ctx context.Context, questionRunID uuid.UUID, response string, targetCompany string, orgWebsites []string) ([]*models.QuestionRunClaim, error)
	ExtractCitations(ctx context.Context, claims []*models.QuestionRunClaim, response string, orgWebsites []string) ([]*models.QuestionRunCitation, error)
	CalculateMetrics(ctx context.Context, mentions []*models.QuestionRunMention, response string, targetCompany string) (*CompetitiveMetrics, error)
	ExtractNetworkOrgData(ctx context.Context, questionRunID uuid.UUID, orgID uuid.UUID, orgName string, orgWebsites []string, questionText string, responseText string) (*NetworkOrgExtractionResult, error)
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

// NEW: OrgEvaluationService interface for the new org evaluation pipeline
type OrgEvaluationService interface {
	GenerateNameVariations(ctx context.Context, orgName string, websites []string) ([]string, error)
	ExtractOrgEvaluation(ctx context.Context, questionRunID, orgID uuid.UUID, orgName string, orgWebsites []string, nameVariations []string, responseText string) (*OrgEvaluationResult, error)
	ExtractCompetitors(ctx context.Context, questionRunID, orgID uuid.UUID, orgName string, responseText string) (*CompetitorExtractionResult, error)
	ExtractCitations(ctx context.Context, questionRunID, orgID uuid.UUID, responseText string, orgWebsites []string) (*CitationExtractionResult, error)
	ProcessOrgQuestionRuns(ctx context.Context, orgID uuid.UUID, orgName string, orgWebsites []string, questionRuns []*models.QuestionRun) (*OrgEvaluationSummary, error)
	RunQuestionMatrixWithOrgEvaluation(ctx context.Context, orgDetails *RealOrgDetails, batchID uuid.UUID) (*OrgEvaluationSummary, error)
	// Batch management methods
	GetOrCreateTodaysBatch(ctx context.Context, orgID uuid.UUID, totalQuestions int) (*models.QuestionRunBatch, bool, error)
	CreateBatch(ctx context.Context, batch *models.QuestionRunBatch) error
	StartBatch(ctx context.Context, batchID uuid.UUID) error
	CompleteBatch(ctx context.Context, batchID uuid.UUID) error
	FailBatch(ctx context.Context, batchID uuid.UUID) error
	UpdateBatchProgress(ctx context.Context, batchID uuid.UUID, completed, failed int) error
	// Question matrix breakdown methods
	CalculateQuestionMatrix(ctx context.Context, orgDetails *RealOrgDetails) ([]*QuestionJob, error)
	ProcessSingleQuestionJob(ctx context.Context, job *QuestionJob, orgID uuid.UUID, orgName string, websites []string, nameVariations []string, batchID uuid.UUID) (*QuestionJobResult, error)
	UpdateLatestFlagsForBatch(ctx context.Context, batchID uuid.UUID) error
	// Org re-evaluation methods
	GetAllOrgQuestionRuns(ctx context.Context, orgID uuid.UUID) ([]*OrgQuestionRun, error)
	ProcessOrgQuestionRunReeval(ctx context.Context, questionRunID uuid.UUID, orgID uuid.UUID, orgName string, websites []string, nameVariations []string, questionText, responseText string) (*OrgReevalResult, error)
	// Network org re-evaluation methods
	ProcessNetworkOrgQuestionRunReeval(ctx context.Context, questionRunID uuid.UUID, orgID uuid.UUID, orgName string, websites []string, nameVariations []string, questionText, responseText string) (*OrgReevalResult, error)
	RunOrgReEvaluation(ctx context.Context, orgID uuid.UUID) (*OrgReevalSummary, error)
}

// NEW: Result types for org evaluation
type OrgEvaluationResult struct {
	Evaluation   *models.OrgEval
	InputTokens  int
	OutputTokens int
	TotalCost    float64
}

type CompetitorExtractionResult struct {
	Competitors  []*models.OrgCompetitor
	InputTokens  int
	OutputTokens int
	TotalCost    float64
}

type CitationExtractionResult struct {
	Citations    []*models.OrgCitation
	InputTokens  int
	OutputTokens int
	TotalCost    float64
}

type OrgEvaluationSummary struct {
	TotalProcessed   int
	TotalEvaluations int
	TotalCitations   int
	TotalCompetitors int
	TotalCost        float64
	ProcessingErrors []string
}

// NetworkProcessingSummary represents the summary of network question processing
type NetworkProcessingSummary struct {
	TotalProcessed   int
	TotalCost        float64
	ProcessingErrors []string
}

// QuestionJob represents a single question×model×location combination to process
type QuestionJob struct {
	QuestionID   uuid.UUID `json:"question_id"`
	ModelID      uuid.UUID `json:"model_id"`
	LocationID   uuid.UUID `json:"location_id"`
	QuestionText string    `json:"question_text"`
	ModelName    string    `json:"model_name"`
	LocationCode string    `json:"location_code"`
	LocationName string    `json:"location_name"`
	JobIndex     int       `json:"job_index"`
	TotalJobs    int       `json:"total_jobs"`
}

// QuestionJobResult represents the result of processing a single question job
type QuestionJobResult struct {
	QuestionRunID   uuid.UUID `json:"question_run_id"`
	JobIndex        int       `json:"job_index"`
	Status          string    `json:"status"` // "completed" or "failed"
	HasEvaluation   bool      `json:"has_evaluation"`
	CompetitorCount int       `json:"competitor_count"`
	CitationCount   int       `json:"citation_count"`
	TotalCost       float64   `json:"total_cost"`
	ErrorMessage    string    `json:"error_message,omitempty"`
}

// OrgQuestionRun represents an existing question run for re-evaluation
type OrgQuestionRun struct {
	QuestionRunID uuid.UUID `json:"question_run_id"`
	QuestionText  string    `json:"question_text"`
	ResponseText  string    `json:"response_text"`
	GeoQuestionID uuid.UUID `json:"geo_question_id"`
}

// OrgReevalResult represents the result of re-evaluating a single question run
type OrgReevalResult struct {
	QuestionRunID   uuid.UUID `json:"question_run_id"`
	HasEvaluation   bool      `json:"has_evaluation"`
	CompetitorCount int       `json:"competitor_count"`
	CitationCount   int       `json:"citation_count"`
	TotalCost       float64   `json:"total_cost"`
	Status          string    `json:"status"` // "completed" or "failed"
	ErrorMessage    string    `json:"error_message,omitempty"`
}

// OrgReevalSummary represents the summary of org re-evaluation processing
type OrgReevalSummary struct {
	TotalProcessed   int      `json:"total_processed"`
	TotalEvaluations int      `json:"total_evaluations"`
	TotalCitations   int      `json:"total_citations"`
	TotalCompetitors int      `json:"total_competitors"`
	TotalCost        float64  `json:"total_cost"`
	ProcessingErrors []string `json:"processing_errors"`
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
	ClaimSentiment  string `json:"claim_sentiment"`
	TargetMentioned bool   `json:"target_mentioned"`
}

type CitationsExtractionResponse struct {
	Citations []CitationExtract `json:"citations"`
}

type CitationExtract struct {
	SourceURL *string `json:"source_url"`
	Type      string  `json:"type"`
}

// GenerateSchema generates a JSON schema for structured outputs
func GenerateSchema[T any]() interface{} {
	reflector := jsonschema.Reflector{
		AllowAdditionalProperties: false,
		DoNotReference:            true,
	}

	var zero T
	schema := reflector.Reflect(zero)

	// Convert to the format expected by OpenAI
	result := map[string]interface{}{
		"type":       "object",
		"properties": schema.Properties,
		"required":   schema.Required,
	}

	if schema.AdditionalProperties != nil {
		result["additionalProperties"] = false
	}

	return result
}
