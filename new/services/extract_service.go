// services/extract_service.go
package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/AI-Template-SDK/senso-workflows/internal/config"
	"github.com/AI-Template-SDK/senso-workflows/internal/models"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/azure"
	"github.com/openai/openai-go/option"
)

type extractService struct {
	cfg          *config.Config
	openAIClient *openai.Client
	costService  CostService
}

func NewExtractService(cfg *config.Config) ExtractService {
	fmt.Printf("[NewExtractService] Creating service with OpenAI key (length: %d)\n", len(cfg.OpenAIAPIKey))

	var client openai.Client

	// Check if Azure configuration is available
	if cfg.AzureOpenAIEndpoint != "" && cfg.AzureOpenAIKey != "" && cfg.AzureOpenAIDeploymentName != "" {
		// Use Azure OpenAI
		client = openai.NewClient(
			azure.WithEndpoint(cfg.AzureOpenAIEndpoint, "2024-12-01-preview"),
			azure.WithAPIKey(cfg.AzureOpenAIKey),
		)
		fmt.Printf("[NewExtractService] ✅ Using Azure OpenAI")
		fmt.Printf("[NewExtractService]   - Endpoint: %s", cfg.AzureOpenAIEndpoint)
		fmt.Printf("[NewExtractService]   - Deployment: %s", cfg.AzureOpenAIDeploymentName)
		fmt.Printf("[NewExtractService]   - SDK: github.com/openai/openai-go with Azure middleware")
	} else {
		// Use standard OpenAI
		client = openai.NewClient(
			option.WithAPIKey(cfg.OpenAIAPIKey),
		)
		fmt.Printf("[NewExtractService] ✅ Using Standard OpenAI")
		fmt.Printf("[NewExtractService]   - API: api.openai.com")
		fmt.Printf("[NewExtractService]   - SDK: github.com/openai/openai-go")
	}

	return &extractService{
		cfg:          cfg,
		openAIClient: &client,
		costService:  NewCostService(),
	}
}

// ExtractResponse represents the structured output from OpenAI
type ExtractResponse struct {
	TargetCompany *CompanyExtract  `json:"target_company" jsonschema_description:"The target company if mentioned in the response, null if not mentioned"`
	Competitors   []CompanyExtract `json:"competitors" jsonschema_description:"List of competitor credit unions or banks mentioned"`
}

// Generate the JSON schema at initialization time
var ExtractResponseSchema = GenerateSchema[ExtractResponse]()

func (s *extractService) ExtractCompanyMentions(ctx context.Context, question string, response string, targetCompany string, orgID string) (*models.ExtractResult, error) {
	prompt := s.buildExtractionPrompt(question, response, targetCompany)

	// Determine which model to use
	var model openai.ChatModel
	if s.cfg.AzureOpenAIDeploymentName != "" {
		// Use Azure deployment name
		model = openai.ChatModel(s.cfg.AzureOpenAIDeploymentName)
	} else {
		// Use standard OpenAI model
		model = openai.ChatModel("gpt-4.1")
	}

	schemaParam := openai.ResponseFormatJSONSchemaJSONSchemaParam{
		Name:        "company_extraction",
		Description: openai.String("Extract mentions of financial institutions from AI response"),
		Schema:      ExtractResponseSchema,
		Strict:      openai.Bool(true),
	}

	// Create the extraction request with structured output
	params := openai.ChatCompletionNewParams{
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage("You are an expert financial services analyst specializing in credit unions and banks. Extract company mentions accurately and comprehensively."),
			openai.UserMessage(prompt),
		},
		Model: model,
		ResponseFormat: openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONSchema: &openai.ResponseFormatJSONSchemaParam{JSONSchema: schemaParam},
		},
	}

	// Conditional Temperature Setting
	if !strings.HasPrefix(string(model), "gpt-5") {
		params.Temperature = openai.Float(0.1) // Keep low for consistency in extraction when verified
		fmt.Printf("[ExtractCompanyMentions] Setting temperature to 0.1 for model %s\n", model)
	} else {
		params.ReasoningEffort = "low"
		fmt.Printf("[ExtractCompanyMentions] Skipping temperature setting for model gpt-5\n")
	}

	chatResponse, err := s.openAIClient.Chat.Completions.New(ctx, params)

	if err != nil {
		return &models.ExtractResult{
			QuestionID: "", // Will be set by caller
			Error:      fmt.Sprintf("Failed to extract: %v", err),
			Timestamp:  time.Now().UTC(),
		}, nil
	}

	// Parse the response
	result := &models.ExtractResult{
		Model:        model,
		InputTokens:  int(chatResponse.Usage.PromptTokens),
		OutputTokens: int(chatResponse.Usage.CompletionTokens),
		Timestamp:    time.Now().UTC(),
	}

	// Calculate extraction cost
	result.ExtractionCost = s.costService.CalculateCost("openai", model, result.InputTokens, result.OutputTokens, false)

	// Get the response content
	if len(chatResponse.Choices) == 0 {
		result.Error = "No response choices returned from OpenAI"
		return result, nil
	}

	responseContent := chatResponse.Choices[0].Message.Content

	// Parse the structured response
	var extractedData ExtractResponse
	if err := json.Unmarshal([]byte(responseContent), &extractedData); err != nil {
		// This should never happen with structured outputs
		result.Error = fmt.Sprintf("Failed to parse extraction response: %v", err)
		fmt.Printf("[ExtractCompanyMentions] Failed to parse JSON: %v\nResponse: %s\n", err, responseContent)
		return result, nil
	}

	// Convert to result format
	if extractedData.TargetCompany != nil {
		result.TargetCompany = &models.CompanyMention{
			Name:          extractedData.TargetCompany.Name,
			Rank:          extractedData.TargetCompany.Rank,
			MentionedText: extractedData.TargetCompany.MentionedText,
			TextSentiment: extractedData.TargetCompany.TextSentiment,
		}
	}

	// Convert competitors
	result.Competitors = make([]models.CompanyMention, len(extractedData.Competitors))
	for i, comp := range extractedData.Competitors {
		result.Competitors[i] = models.CompanyMention{
			Name:          comp.Name,
			Rank:          comp.Rank,
			MentionedText: comp.MentionedText,
			TextSentiment: comp.TextSentiment,
		}
	}

	fmt.Printf("[ExtractCompanyMentions] Successfully extracted: target=%v, competitors=%d\n",
		result.TargetCompany != nil, len(result.Competitors))

	return result, nil
}

func (s *extractService) buildExtractionPrompt(question, response, targetCompany string) string {
	return fmt.Sprintf(`## Target Company: %s

## Context
You need to extract mentions of the target company "%s" and any competitor credit unions or banks from the following AI response.

## Key Rules:
- Ranking is by order of first appearance (1st mentioned = rank 1)
- Extract ALL text mentioning each company (if mentioned multiple times, separate with " | ")
- Only extract credit unions and banks as competitors
- If target company not mentioned, set "target_company" to null
- Copy mentioned text EXACTLY as written
- Sentiment must be one of: positive, negative, neutral, mixed

## Question Asked:
%s

## Response to Analyze:
%s`,
		targetCompany, targetCompany, question, response)
}
