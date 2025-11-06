// services/data_extraction_service.go
package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/AI-Template-SDK/senso-api/pkg/models"
	"github.com/AI-Template-SDK/senso-workflows/internal/config"
	"github.com/google/uuid"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/azure"
	"github.com/openai/openai-go/option"
	"mvdan.cc/xurls/v2"
)

type dataExtractionService struct {
	cfg          *config.Config
	openAIClient *openai.Client
	costService  CostService
}

func NewDataExtractionService(cfg *config.Config) DataExtractionService {
	fmt.Printf("[NewDataExtractionService] Creating service with OpenAI key (length: %d)\n", len(cfg.OpenAIAPIKey))

	var client openai.Client

	// Check if Azure configuration is available
	if cfg.AzureOpenAIEndpoint != "" && cfg.AzureOpenAIKey != "" && cfg.AzureOpenAIDeploymentName != "" {
		// Use Azure OpenAI
		client = openai.NewClient(
			azure.WithEndpoint(cfg.AzureOpenAIEndpoint, "2024-12-01-preview"),
			azure.WithAPIKey(cfg.AzureOpenAIKey),
		)
		fmt.Printf("[NewDataExtractionService] ✅ Using Azure OpenAI")
		fmt.Printf("[NewDataExtractionService]   - Endpoint: %s", cfg.AzureOpenAIEndpoint)
		fmt.Printf("[NewDataExtractionService]   - Deployment: %s", cfg.AzureOpenAIDeploymentName)
		fmt.Printf("[NewDataExtractionService]   - SDK: github.com/openai/openai-go with Azure middleware")
	} else {
		// Use standard OpenAI
		client = openai.NewClient(
			option.WithAPIKey(cfg.OpenAIAPIKey),
		)
		fmt.Printf("[NewDataExtractionService] ✅ Using Standard OpenAI")
		fmt.Printf("[NewDataExtractionService]   - API: api.openai.com")
		fmt.Printf("[NewDataExtractionService]   - SDK: github.com/openai/openai-go")
	}

	return &dataExtractionService{
		cfg:          cfg,
		openAIClient: &client,
		costService:  NewCostService(),
	}
}

// ExtractMentions parses AI response and extracts company mentions
func (s *dataExtractionService) ExtractMentions(ctx context.Context, questionRunID uuid.UUID, response string, targetCompany string, orgWebsites []string) ([]*models.QuestionRunMention, error) {
	fmt.Printf("[ExtractMentions] 🔍 Processing mentions for question run %s", questionRunID)

	prompt := s.buildMentionsExtractionPrompt(response, targetCompany, orgWebsites)

	// Use a model that supports structured outputs
	var model openai.ChatModel
	if s.cfg.AzureOpenAIDeploymentName != "" {
		// Use Azure deployment name
		model = openai.ChatModel(s.cfg.AzureOpenAIDeploymentName)
		fmt.Printf("[ExtractMentions] 🎯 Using Azure OpenAI deployment: %s", s.cfg.AzureOpenAIDeploymentName)
	} else {
		// Use standard OpenAI model
		model = openai.ChatModelGPT4_1
		fmt.Printf("[ExtractMentions] 🎯 Using Standard OpenAI model: %s", model)
	}

	schemaParam := openai.ResponseFormatJSONSchemaJSONSchemaParam{
		Name:        "company_mentions_extraction",
		Description: openai.String("Extract mentions of financial institutions from AI response"),
		Schema:      GenerateSchema[MentionsExtractionResponse](),
		Strict:      openai.Bool(true),
	}

	fmt.Printf("[ExtractMentions] 🚀 Making AI call for mentions extraction...")

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
		fmt.Printf("[ExtractMentions] Setting temperature to 0.1 for model %s\n", model)
	} else {
		params.ReasoningEffort = "low"
		fmt.Printf("[ExtractMentions] Skipping temperature setting for model gpt-5\n")
	}

	chatResponse, err := s.openAIClient.Chat.Completions.New(ctx, params)

	if err != nil {
		return nil, fmt.Errorf("failed to extract mentions: %w", err)
	}

	fmt.Printf("[ExtractMentions] ✅ AI call completed successfully")
	fmt.Printf("[ExtractMentions]   - Input tokens: %d", chatResponse.Usage.PromptTokens)
	fmt.Printf("[ExtractMentions]   - Output tokens: %d", chatResponse.Usage.CompletionTokens)

	// Parse the response
	if len(chatResponse.Choices) == 0 {
		return nil, fmt.Errorf("no response choices returned from OpenAI")
	}

	responseContent := chatResponse.Choices[0].Message.Content

	// Parse the structured response
	var extractedData MentionsExtractionResponse
	if err := json.Unmarshal([]byte(responseContent), &extractedData); err != nil {
		return nil, fmt.Errorf("failed to parse mentions extraction response: %w", err)
	}

	// Capture token and cost data from the AI call
	inputTokens := int(chatResponse.Usage.PromptTokens)
	outputTokens := int(chatResponse.Usage.CompletionTokens)
	totalCost := s.costService.CalculateCost("openai", string(model), inputTokens, outputTokens, false)

	var mentions []*models.QuestionRunMention
	now := time.Now()

	// Process target company - only create a row if mentioned_text is non-empty and not "null"
	if extractedData.TargetCompany != nil {
		rawMentionText := extractedData.TargetCompany.MentionedText
		trimmedLower := strings.ToLower(strings.TrimSpace(rawMentionText))
		if trimmedLower != "" && trimmedLower != "null" {
			sentiment := s.normalizeSentiment(extractedData.TargetCompany.TextSentiment)
			mentions = append(mentions, &models.QuestionRunMention{
				QuestionRunMentionID: uuid.New(),
				QuestionRunID:        questionRunID,
				MentionOrg:           extractedData.TargetCompany.Name,
				MentionText:          rawMentionText,
				MentionRank:          &extractedData.TargetCompany.Rank,
				MentionSentiment:     &sentiment,
				TargetOrg:            true,
				InputTokens:          &inputTokens,
				OutputTokens:         &outputTokens,
				TotalCost:            &totalCost,
				CreatedAt:            now,
				UpdatedAt:            now,
			})
		} else {
			fmt.Printf("[ExtractMentions] Skipping target_company mention due to empty/invalid mentioned_text: '%s'", rawMentionText)
		}
	}

	// Process competitors
	for _, comp := range extractedData.Competitors {
		sentiment := s.normalizeSentiment(comp.TextSentiment)
		mentions = append(mentions, &models.QuestionRunMention{
			QuestionRunMentionID: uuid.New(),
			QuestionRunID:        questionRunID,
			MentionOrg:           comp.Name,
			MentionText:          comp.MentionedText,
			MentionRank:          &comp.Rank,
			MentionSentiment:     &sentiment,
			TargetOrg:            false,
			InputTokens:          &inputTokens,
			OutputTokens:         &outputTokens,
			TotalCost:            &totalCost,
			CreatedAt:            now,
			UpdatedAt:            now,
		})
	}

	fmt.Printf("[ExtractMentions] ✅ Successfully extracted %d mentions", len(mentions))
	return mentions, nil
}

// ExtractClaims parses AI response and extracts factual claims
func (s *dataExtractionService) ExtractClaims(ctx context.Context, questionRunID uuid.UUID, response string, targetCompany string, orgWebsites []string) ([]*models.QuestionRunClaim, error) {
	fmt.Printf("[ExtractClaims] 🔍 Processing claims for question run %s", questionRunID)

	prompt := s.buildClaimsExtractionPrompt(response, targetCompany, orgWebsites)

	// Use a model that supports structured outputs
	var model openai.ChatModel
	if s.cfg.AzureOpenAIDeploymentName != "" {
		// Use Azure deployment name
		model = openai.ChatModel(s.cfg.AzureOpenAIDeploymentName)
		fmt.Printf("[ExtractClaims] 🎯 Using Azure OpenAI deployment: %s", s.cfg.AzureOpenAIDeploymentName)
	} else {
		// Use standard OpenAI model
		model = openai.ChatModelGPT4_1
		fmt.Printf("[ExtractClaims] 🎯 Using Standard OpenAI model: %s", model)
	}

	schemaParam := openai.ResponseFormatJSONSchemaJSONSchemaParam{
		Name:        "claims_extraction",
		Description: openai.String("Extract factual claims from AI response"),
		Schema:      GenerateSchema[ClaimsExtractionResponse](),
		Strict:      openai.Bool(true),
	}

	fmt.Printf("[ExtractClaims] 🚀 Making AI call for claims extraction...")

	// Create the extraction request
	params := openai.ChatCompletionNewParams{
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage("You are an expert fact-checker. Break down the response into individual, verifiable factual claims."),
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
		fmt.Printf("[ExtractClaims] Setting temperature to 0.1 for model %s\n", model)
	} else {
		params.ReasoningEffort = "low"
		fmt.Printf("[ExtractClaims] Skipping temperature setting for model gpt-5\n")
	}

	chatResponse, err := s.openAIClient.Chat.Completions.New(ctx, params)

	if err != nil {
		return nil, fmt.Errorf("failed to extract claims: %w", err)
	}

	fmt.Printf("[ExtractClaims] ✅ AI call completed successfully")
	fmt.Printf("[ExtractClaims]   - Input tokens: %d", chatResponse.Usage.PromptTokens)
	fmt.Printf("[ExtractClaims]   - Output tokens: %d", chatResponse.Usage.CompletionTokens)

	// Parse the response
	if len(chatResponse.Choices) == 0 {
		return nil, fmt.Errorf("no response choices returned from OpenAI")
	}

	responseContent := chatResponse.Choices[0].Message.Content

	var extractedData ClaimsExtractionResponse
	if err := json.Unmarshal([]byte(responseContent), &extractedData); err != nil {
		return nil, fmt.Errorf("failed to parse claims extraction response: %w", err)
	}

	// Capture token and cost data from the AI call
	inputTokens := int(chatResponse.Usage.PromptTokens)
	outputTokens := int(chatResponse.Usage.CompletionTokens)
	totalCost := s.costService.CalculateCost("openai", string(model), inputTokens, outputTokens, false)

	var claims []*models.QuestionRunClaim
	now := time.Now()

	for i, claim := range extractedData.Claims {
		sentiment := s.normalizeSentiment(claim.ClaimSentiment)
		targetMentioned := claim.TargetMentioned

		claims = append(claims, &models.QuestionRunClaim{
			QuestionRunClaimID: uuid.New(),
			QuestionRunID:      questionRunID,
			ClaimText:          claim.ClaimText,
			ClaimOrder:         i + 1,
			Sentiment:          &sentiment,
			TargetMentioned:    &targetMentioned,
			InputTokens:        &inputTokens,
			OutputTokens:       &outputTokens,
			TotalCost:          &totalCost,
			CreatedAt:          now,
			UpdatedAt:          now,
		})
	}

	fmt.Printf("[ExtractClaims] ✅ Successfully extracted %d claims", len(claims))
	return claims, nil
}

// ExtractCitations parses AI response and finds citations for claims
func (s *dataExtractionService) ExtractCitations(ctx context.Context, claims []*models.QuestionRunClaim, response string, orgWebsites []string) ([]*models.QuestionRunCitation, error) {
	fmt.Printf("[ExtractCitations] Processing citations for %d claims\n", len(claims))

	var allCitations []*models.QuestionRunCitation

	// Process each claim individually to find its citations
	for _, claim := range claims {
		citations, err := s.extractCitationsForClaim(ctx, claim, response, orgWebsites)
		if err != nil {
			fmt.Printf("[ExtractCitations] Warning: Failed to extract citations for claim %s: %v\n", claim.QuestionRunClaimID, err)
			continue
		}
		allCitations = append(allCitations, citations...)
	}

	fmt.Printf("[ExtractCitations] Successfully extracted %d total citations\n", len(allCitations))
	return allCitations, nil
}

// CalculateMetrics computes competitive intelligence metrics from mentions
func (s *dataExtractionService) CalculateMetrics(ctx context.Context, mentions []*models.QuestionRunMention, response string, targetCompany string) (*CompetitiveMetrics, error) {
	var targetMention *models.QuestionRunMention

	// Find target company mention that actually has content
	for _, mention := range mentions {
		if mention.TargetOrg && mention.MentionText != "" && mention.MentionOrg != "" {
			targetMention = mention
			break
		}
	}

	metrics := &CompetitiveMetrics{
		TargetMentioned: targetMention != nil,
	}

	if targetMention != nil {
		// Calculate share of voice (decimal format, not percentage)
		responseLen := float64(len(response))
		targetTextLen := float64(len(targetMention.MentionText))
		if responseLen > 0 {
			shareOfVoice := targetTextLen / responseLen
			metrics.ShareOfVoice = &shareOfVoice
		}

		// Target rank from mention (ensure it's not null)
		if targetMention.MentionRank != nil {
			metrics.TargetRank = targetMention.MentionRank
		} else {
			// Default rank if somehow null
			defaultRank := 1
			metrics.TargetRank = &defaultRank
		}

		// Target sentiment (convert string to float for database)
		if targetMention.MentionSentiment != nil {
			sentimentFloat := s.convertSentimentToFloat(*targetMention.MentionSentiment)
			metrics.TargetSentiment = &sentimentFloat
		}
	}

	return metrics, nil
}

// ExtractNetworkOrgEvaluation extracts network org evaluation data (similar to ExtractOrgEvaluation but for network tables)
func (s *dataExtractionService) ExtractNetworkOrgEvaluation(ctx context.Context, questionRunID uuid.UUID, orgID uuid.UUID, orgName string, orgWebsites []string, nameVariations []string, questionText string, responseText string) (*NetworkOrgEvaluationResult, error) {
	fmt.Printf("[ExtractNetworkOrgEvaluation] 🔍 Processing network org evaluation for question run %s, org %s\n", questionRunID, orgName)

	nameVariationsStr := strings.Join(nameVariations, ", ")
	websitesList := ""
	if len(orgWebsites) > 0 {
		websitesList = "\n## ORGANIZATION DOMAINS (SUPPORTING SIGNALS):\n"
		for _, website := range orgWebsites {
			websitesList += fmt.Sprintf("- %s\n", website)
		}
		websitesList += "\n"
	}

	prompt := fmt.Sprintf(`You are an expert text extraction specialist. The target organization IS MENTIONED in this text. Your task is to extract ALL text that mentions the organization and determine the overall sentiment.

**TARGET ORGANIZATION:** %s
**Organization name variations:** %s
%s
**QUESTION:** %s

**TASK 1: EXTRACT MENTION TEXT**

Find EVERY occurrence where the target organization is mentioned by name (including variations) and extract the text with perfect formatting preservation.

**EXTRACTION RULES:**
- **PRESERVE EXACT FORMATTING**: Copy text character-for-character including:
  - All punctuation marks (periods, commas, colons, semicolons, etc.)
  - All markdown formatting (**, *, ##, [], (), etc.)
  - All spacing, line breaks, and indentation
  - All special characters and symbols
- **INCLUDE CITATIONS**: Always include URLs, links, and citation references that appear with mentions
- **ALL FORMATS**: Extract from paragraphs, lists, tables, headers, footnotes, structured data
- **COMPLETE CONTEXT**: Extract the full sentence/paragraph/section that contains the mention

**SPAN DEFINITION:**
- **Single occurrence** = Complete sentence, bullet point, table row, or logical text unit that mentions the organization
- **Adjacent context** = If consecutive sentences form one continuous thought about the organization, keep them together
- **Aggregation** = Use exact delimiter " || " (space-pipe-pipe-space) between separate occurrences

**TASK 2: DETERMINE SENTIMENT**

Analyze the overall sentiment toward the target organization across all mentions:
- **positive**: Favorable language, praise, recommendations ("excellent", "best", "recommended", "leading")
- **negative**: Critical language, problems, warnings ("poor", "issues", "avoid", "problematic")
- **neutral**: Factual, descriptive, balanced content without clear bias

**TASK 3: CITATION CHECK**

Determine if the response contains URLs that relate to this specific organization:
- Set citation=true if URLs from organization's domains are present OR if external URLs mention the organization
- Set citation=false if no relevant URLs found

**TASK 4: MENTION RANK**

Assign prominence ranking (1=most prominent, higher numbers=less prominent, 0=not mentioned)

**RESPONSE TO ANALYZE:**
`+"`"+`
%s
`+"`"+`

**OUTPUT REQUIREMENTS:**
- mentioned: true (we already confirmed it's mentioned)
- mention_text: ALL extracted text with perfect formatting, separated by " || "
- sentiment: exactly one of "positive", "negative", or "neutral" (lowercase)
- citation: true or false
- mention_rank: integer (1 for most prominent)`, "`"+orgName+"`", nameVariationsStr, websitesList, questionText, responseText)

	// Use Azure or standard OpenAI with gpt-4.1
	var model openai.ChatModel
	if s.cfg.AzureOpenAIDeploymentName != "" {
		model = openai.ChatModel(s.cfg.AzureOpenAIDeploymentName)
		fmt.Printf("[ExtractNetworkOrgEvaluation] 🎯 Using Azure OpenAI deployment: %s\n", s.cfg.AzureOpenAIDeploymentName)
	} else {
		model = openai.ChatModelGPT4_1
		fmt.Printf("[ExtractNetworkOrgEvaluation] 🎯 Using Standard OpenAI model: %s\n", model)
	}

	schemaParam := openai.ResponseFormatJSONSchemaJSONSchemaParam{
		Name:        "network_org_evaluation_extraction",
		Description: openai.String("Extract network organization evaluation data from AI response"),
		Schema:      GenerateSchema[NetworkOrgEvaluationResponse](),
		Strict:      openai.Bool(true),
	}

	fmt.Printf("[ExtractNetworkOrgEvaluation] 🚀 Making AI call for network org evaluation...\n")

	// Create the extraction request with structured output
	params := openai.ChatCompletionNewParams{
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage("You are an expert text extraction specialist. Extract mention text with perfect formatting and determine sentiment. The organization is already confirmed to be mentioned."),
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
		fmt.Printf("[ExtractNetworkOrgEvaluation] Setting temperature to 0.1 for model %s\n", model)
	} else {
		params.ReasoningEffort = "low"
		fmt.Printf("[ExtractNetworkOrgEvaluation] Skipping temperature setting for model gpt-5\n")
	}

	chatResponse, err := s.openAIClient.Chat.Completions.New(ctx, params)

	if err != nil {
		return nil, fmt.Errorf("failed to extract network org evaluation: %w", err)
	}

	fmt.Printf("[ExtractNetworkOrgEvaluation] ✅ AI call completed successfully\n")
	fmt.Printf("[ExtractNetworkOrgEvaluation]   - Input tokens: %d\n", chatResponse.Usage.PromptTokens)
	fmt.Printf("[ExtractNetworkOrgEvaluation]   - Output tokens: %d\n", chatResponse.Usage.CompletionTokens)

	// Parse the response
	if len(chatResponse.Choices) == 0 {
		return nil, fmt.Errorf("no response choices returned from OpenAI")
	}

	responseContent := chatResponse.Choices[0].Message.Content

	// Parse the structured response
	var extractedData NetworkOrgEvaluationResponse
	if err := json.Unmarshal([]byte(responseContent), &extractedData); err != nil {
		return nil, fmt.Errorf("failed to parse network org evaluation response: %w", err)
	}

	// Capture token and cost data from the AI call
	inputTokens := int(chatResponse.Usage.PromptTokens)
	outputTokens := int(chatResponse.Usage.CompletionTokens)
	totalCost := s.costService.CalculateCost("openai", string(model), inputTokens, outputTokens, false)

	// Create the network org evaluation model
	now := time.Now()
	networkOrgEval := &models.NetworkOrgEval{
		NetworkOrgEvalID: uuid.New(),
		QuestionRunID:    questionRunID,
		OrgID:            orgID,
		Mentioned:        true, // We already know it's mentioned from pre-filtering
		Citation:         extractedData.Citation,
		Sentiment:        stringPtr(extractedData.Sentiment),
		MentionText:      stringPtr(extractedData.MentionText),
		MentionRank:      intPtr(extractedData.MentionRank),
		InputTokens:      intPtr(inputTokens),
		OutputTokens:     intPtr(outputTokens),
		TotalCost:        float64Ptr(totalCost),
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	fmt.Printf("[ExtractNetworkOrgEvaluation] ✅ Created network org evaluation: mentioned=true, sentiment=%s, citation=%t, mention_rank=%d\n",
		extractedData.Sentiment, extractedData.Citation, extractedData.MentionRank)

	return &NetworkOrgEvaluationResult{
		Evaluation:   networkOrgEval,
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		TotalCost:    totalCost,
	}, nil
}

// ExtractNetworkOrgCompetitors extracts competitors for network org processing (separate AI call with gpt-4.1-mini)
func (s *dataExtractionService) ExtractNetworkOrgCompetitors(ctx context.Context, questionRunID uuid.UUID, orgID uuid.UUID, orgName string, responseText string) (*NetworkOrgCompetitorResult, error) {
	fmt.Printf("[ExtractNetworkOrgCompetitors] 🔍 Processing competitors for network org question run %s, org %s\n", questionRunID, orgName)

	prompt := fmt.Sprintf("You are an expert in competitive analysis and brand identification. Your task is to identify ALL competitor brands, companies, products, or services mentioned in the response text that are NOT the target organization.\n\n**TARGET ORGANIZATION:** %s\n\n**COMPETITOR IDENTIFICATION RULES:**\n\n1. **What to Include:**\n   - Company names (e.g., \"Microsoft\", \"Google\", \"Apple\")\n   - Product names (e.g., \"ChatGPT\", \"Claude\", \"Gemini\", \"Perplexity\")\n   - Service names (e.g., \"Ahrefs Brand Radar\", \"Surfer SEO AI Tracker\")\n   - Platform names (e.g., \"LinkedIn\", \"Facebook\", \"Twitter\")\n   - Tool names (e.g., \"Profound\", \"Promptmonitor\", \"Writesonic GEO Platform\")\n   - Any branded entity that could be considered competition or alternative\n\n2. **What to Exclude:**\n   - The target organization itself and its variations\n   - Generic terms (e.g., \"AI tools\", \"analytics platforms\", \"search engines\")\n   - Non-competitive entities (e.g., \"users\", \"customers\", \"developers\")\n   - Technical terms or concepts (e.g., \"machine learning\", \"natural language processing\")\n   - Industry terms (e.g., \"credit unions\", \"financial services\")\n\n3. **Extraction Guidelines:**\n   - Extract the most commonly used or official name for each competitor\n   - If a company has multiple products mentioned, list each product separately\n   - Remove duplicates and variations of the same entity\n   - Focus on entities that could be considered alternatives or competitors\n   - Include both direct competitors and indirect competitors mentioned\n\n**EXAMPLES:**\n\nExample 1: \"Leading AI tools include ChatGPT, Claude, Gemini, and Senso.ai for content optimization.\"\n→ Extract: [\"ChatGPT\", \"Claude\", \"Gemini\"] (exclude Senso.ai as it's the target)\n\nExample 2: \"Microsoft's Azure competes with Google Cloud and Amazon Web Services in the enterprise market.\"\n→ Extract: [\"Microsoft\", \"Azure\", \"Google Cloud\", \"Amazon Web Services\"]\n\nExample 3: \"Popular analytics platforms like Google Analytics, Adobe Analytics, and Mixpanel offer similar features.\"\n→ Extract: [\"Google Analytics\", \"Adobe Analytics\", \"Mixpanel\"]\n\n**RESPONSE TO ANALYZE:**\n```\n%s\n```\n\n**INSTRUCTIONS:**\n- Return only the list of competitor names\n- Use the most recognizable/official name for each competitor\n- Remove any duplicates or very similar variations\n- If no competitors are mentioned, return an empty list\n- Do not include the target organization or generic terms", "`"+orgName+"`", responseText)

	// ALWAYS use gpt-4.1-mini for competitors (cost-effective)
	var model openai.ChatModel
	if s.cfg.AzureOpenAIDeploymentName != "" {
		// Use Azure with mini model
		model = openai.ChatModel("gpt-4.1-mini")
		fmt.Printf("[ExtractNetworkOrgCompetitors] 🎯 Using Azure SDK with model: gpt-4.1-mini\n")
	} else {
		model = openai.ChatModel("gpt-4.1-mini")
		fmt.Printf("[ExtractNetworkOrgCompetitors] 🎯 Using Standard OpenAI model: gpt-4.1-mini\n")
	}

	schemaParam := openai.ResponseFormatJSONSchemaJSONSchemaParam{
		Name:        "network_org_competitor_extraction",
		Description: openai.String("Extract competitor names from AI response for network org processing"),
		Schema:      GenerateSchema[CompetitorListResponse](),
		Strict:      openai.Bool(true),
	}

	fmt.Printf("[ExtractNetworkOrgCompetitors] 🚀 Making AI call for competitor extraction...\n")

	// Create the extraction request with structured output
	params := openai.ChatCompletionNewParams{
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage("You are an expert in competitive analysis and brand identification. Extract competitor names accurately and comprehensively."),
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
		fmt.Printf("[ExtractNetworkOrgCompetitors] Setting temperature to 0.1 for model %s\n", model)
	} else {
		params.ReasoningEffort = "low"
		fmt.Printf("[ExtractNetworkOrgCompetitors] Skipping temperature setting for model gpt-5\n")
	}

	chatResponse, err := s.openAIClient.Chat.Completions.New(ctx, params)

	if err != nil {
		return nil, fmt.Errorf("failed to extract network org competitors: %w", err)
	}

	fmt.Printf("[ExtractNetworkOrgCompetitors] ✅ AI call completed successfully\n")
	fmt.Printf("[ExtractNetworkOrgCompetitors]   - Input tokens: %d\n", chatResponse.Usage.PromptTokens)
	fmt.Printf("[ExtractNetworkOrgCompetitors]   - Output tokens: %d\n", chatResponse.Usage.CompletionTokens)

	// Calculate cost
	inputTokens := int(chatResponse.Usage.PromptTokens)
	outputTokens := int(chatResponse.Usage.CompletionTokens)
	totalCost := s.costService.CalculateCost("openai", string(model), inputTokens, outputTokens, false)

	// Parse the response
	if len(chatResponse.Choices) == 0 {
		return nil, fmt.Errorf("no response choices returned from OpenAI")
	}

	responseContent := chatResponse.Choices[0].Message.Content

	// Parse the structured response
	var extractedData CompetitorListResponse
	if err := json.Unmarshal([]byte(responseContent), &extractedData); err != nil {
		return nil, fmt.Errorf("failed to parse competitors response: %w", err)
	}

	// Create competitor models with cost tracking
	var competitors []*models.NetworkOrgCompetitor
	now := time.Now()

	for _, competitorName := range extractedData.Competitors {
		if strings.TrimSpace(competitorName) == "" {
			continue // Skip empty names
		}

		competitor := &models.NetworkOrgCompetitor{
			NetworkOrgCompetitorID: uuid.New(),
			QuestionRunID:          questionRunID,
			OrgID:                  orgID,
			Name:                   strings.TrimSpace(competitorName),
			InputTokens:            &inputTokens,
			OutputTokens:           &outputTokens,
			TotalCost:              &totalCost,
			CreatedAt:              now,
			UpdatedAt:              now,
		}

		competitors = append(competitors, competitor)
	}

	fmt.Printf("[ExtractNetworkOrgCompetitors] ✅ Extracted %d competitors (cost: $%.6f)\n", len(competitors), totalCost)
	return &NetworkOrgCompetitorResult{
		Competitors:  competitors,
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		TotalCost:    totalCost,
	}, nil
}

// ExtractNetworkOrgCitations extracts citations using regex (no AI call, reliable URL extraction)
func (s *dataExtractionService) ExtractNetworkOrgCitations(ctx context.Context, questionRunID uuid.UUID, orgID uuid.UUID, responseText string, orgWebsites []string) (*NetworkOrgCitationResult, error) {
	fmt.Printf("[ExtractNetworkOrgCitations] 🔍 Processing citations for network org question run %s\n", questionRunID)

	// Use xurls relaxed mode to find all URLs in the text (same as org evaluation)
	matches := xurls.Relaxed().FindAllString(responseText, -1)

	var citations []*models.NetworkOrgCitation
	seenURLs := make(map[string]bool)
	now := time.Now()

	for _, match := range matches {
		// Clean up the match
		url := strings.TrimSpace(match)

		// Skip if empty or already seen
		if url == "" || seenURLs[url] {
			continue
		}

		// Normalize URL for comparison
		normalizedURL := strings.ToLower(url)
		if !strings.HasPrefix(normalizedURL, "http://") && !strings.HasPrefix(normalizedURL, "https://") {
			normalizedURL = "https://" + normalizedURL
		}

		// Remove trailing slashes for comparison
		normalizedURL = strings.TrimRight(normalizedURL, "/")

		// Determine if this is a primary or secondary citation using proper domain parsing
		citationType := "secondary" // Default to secondary
		if isPrimaryDomain(normalizedURL, orgWebsites) {
			citationType = "primary"
		}

		citation := &models.NetworkOrgCitation{
			NetworkOrgCitationID: uuid.New(),
			QuestionRunID:        questionRunID,
			OrgID:                orgID,
			URL:                  url,
			Type:                 citationType,
			CreatedAt:            now,
			UpdatedAt:            now,
		}

		citations = append(citations, citation)
		seenURLs[url] = true
	}

	primaryCount := 0
	secondaryCount := 0
	for _, citation := range citations {
		if citation.Type == "primary" {
			primaryCount++
		} else {
			secondaryCount++
		}
	}

	fmt.Printf("[ExtractNetworkOrgCitations] ✅ Extracted %d citations (%d primary, %d secondary) - REGEX-BASED\n",
		len(citations), primaryCount, secondaryCount)

	// Citations use regex (no AI cost)
	return &NetworkOrgCitationResult{
		Citations:    citations,
		InputTokens:  0,
		OutputTokens: 0,
		TotalCost:    0.0,
	}, nil
}

// ExtractNetworkOrgData is the main entry point that orchestrates the extraction process
// This method has been UPDATED to use separate extraction methods like the org evaluation pipeline:
// 1. Generate name variations (once) - or use pre-generated ones if provided
// 2. Check if organization is mentioned
// 3. Extract evaluation: ONLY if mentioned (AI with gpt-4.1), otherwise create minimal record
// 4. Extract competitors: ALWAYS (AI with gpt-4.1-mini) - regardless of mention status
// 5. Extract citations: ALWAYS (regex-based) - regardless of mention status
func (s *dataExtractionService) ExtractNetworkOrgData(ctx context.Context, questionRunID uuid.UUID, orgID uuid.UUID, orgName string, orgWebsites []string, questionText string, responseText string, nameVariations []string) (*NetworkOrgExtractionResult, error) {
	fmt.Printf("[ExtractNetworkOrgData] 🔍 Processing network org data for question run %s, org %s\n", questionRunID, orgName)
	fmt.Printf("[ExtractNetworkOrgData] 🎯 Using NEW THREE-METHOD APPROACH (like org evaluation pipeline)\n")

	// Step 1: Generate name variations for mention detection (if not provided)
	if len(nameVariations) == 0 {
		fmt.Printf("[ExtractNetworkOrgData] Generating name variations for org: %s\n", orgName)
		var err error
		nameVariations, err = s.generateNameVariations(ctx, orgName, orgWebsites)
		if err != nil {
			return nil, fmt.Errorf("failed to generate name variations: %w", err)
		}
		fmt.Printf("[ExtractNetworkOrgData] ✅ Generated %d name variations\n", len(nameVariations))
	} else {
		fmt.Printf("[ExtractNetworkOrgData] ✅ Using %d pre-generated name variations\n", len(nameVariations))
	}

	// Step 2: Check if organization is mentioned (using name variations)
	mentioned := false
	responseTextLower := strings.ToLower(responseText)
	for _, name := range nameVariations {
		if strings.Contains(responseTextLower, strings.ToLower(name)) {
			mentioned = true
			break
		}
	}
	fmt.Printf("[ExtractNetworkOrgData] Organization mentioned: %t\n", mentioned)

	// Initialize cost tracking
	totalInputTokens := 0
	totalOutputTokens := 0
	totalCost := 0.0

	var evaluation *models.NetworkOrgEval
	var competitors []*models.NetworkOrgCompetitor
	var citations []*models.NetworkOrgCitation

	// Step 3: Extract evaluation ONLY if mentioned (following org evaluation logic)
	if mentioned {
		fmt.Printf("[ExtractNetworkOrgData] 📊 Step 1/3: Extracting evaluation (AI call with gpt-4.1)...\n")
		evalResult, err := s.ExtractNetworkOrgEvaluation(ctx, questionRunID, orgID, orgName, orgWebsites, nameVariations, questionText, responseText)
		if err != nil {
			return nil, fmt.Errorf("failed to extract network org evaluation: %w", err)
		}
		evaluation = evalResult.Evaluation
		totalInputTokens += evalResult.InputTokens
		totalOutputTokens += evalResult.OutputTokens
		totalCost += evalResult.TotalCost
		fmt.Printf("[ExtractNetworkOrgData] ✅ Evaluation extracted (cost: $%.6f)\n", evalResult.TotalCost)
	} else {
		// Create minimal evaluation for non-mentioned case
		fmt.Printf("[ExtractNetworkOrgData] ⚪ Organization not mentioned - creating minimal evaluation\n")
		now := time.Now()
		evaluation = &models.NetworkOrgEval{
			NetworkOrgEvalID: uuid.New(),
			QuestionRunID:    questionRunID,
			OrgID:            orgID,
			Mentioned:        false,
			Citation:         false, // Will be determined by citation extraction below
			Sentiment:        nil,
			MentionText:      nil,
			MentionRank:      nil,
			InputTokens:      nil,
			OutputTokens:     nil,
			TotalCost:        nil,
			CreatedAt:        now,
			UpdatedAt:        now,
		}
		fmt.Printf("[ExtractNetworkOrgData] ✅ Minimal evaluation created\n")
	}

	// Step 4: ALWAYS extract competitors (regardless of mention status - following org evaluation logic)
	fmt.Printf("[ExtractNetworkOrgData] 🏢 Step 2/3: Extracting competitors (AI call with gpt-4.1-mini)...\n")
	competitorResult, err := s.ExtractNetworkOrgCompetitors(ctx, questionRunID, orgID, orgName, responseText)
	if err != nil {
		return nil, fmt.Errorf("failed to extract network org competitors: %w", err)
	}
	competitors = competitorResult.Competitors
	totalInputTokens += competitorResult.InputTokens
	totalOutputTokens += competitorResult.OutputTokens
	totalCost += competitorResult.TotalCost
	fmt.Printf("[ExtractNetworkOrgData] ✅ %d competitors extracted (cost: $%.6f)\n", len(competitors), competitorResult.TotalCost)

	// Step 5: ALWAYS extract citations (regardless of mention status - following org evaluation logic)
	fmt.Printf("[ExtractNetworkOrgData] 🔗 Step 3/3: Extracting citations (regex-based, no AI cost)...\n")
	citationResult, err := s.ExtractNetworkOrgCitations(ctx, questionRunID, orgID, responseText, orgWebsites)
	if err != nil {
		return nil, fmt.Errorf("failed to extract network org citations: %w", err)
	}
	citations = citationResult.Citations
	// Citations have no cost (regex-based)
	fmt.Printf("[ExtractNetworkOrgData] ✅ %d citations extracted (regex, $0.00 cost)\n", len(citations))

	fmt.Printf("[ExtractNetworkOrgData] 🎉 COMPLETE: 1 evaluation, %d competitors, %d citations | Total cost: $%.6f\n",
		len(competitors), len(citations), totalCost)

	return &NetworkOrgExtractionResult{
		Evaluation:   evaluation,
		Competitors:  competitors,
		Citations:    citations,
		InputTokens:  totalInputTokens,
		OutputTokens: totalOutputTokens,
		TotalCost:    totalCost,
	}, nil
}

// Helper methods
func (s *dataExtractionService) buildMentionsExtractionPrompt(response, targetCompany string, orgWebsites []string) string {
	websitesList := ""
	if len(orgWebsites) > 0 {
		websitesList = "## ORGANIZATION DOMAINS (SUPPORTING SIGNALS, NOT PRIMARY):\n"
		for _, website := range orgWebsites {
			websitesList += fmt.Sprintf("- %s\n", website)
		}
		websitesList += "\n"
	}

	return fmt.Sprintf(`You are an expert competitive intelligence analyst extracting SPECIFIC COMPANY AND BRAND mentions from the RESPONSE TEXT ONLY.

## CRITICAL RULES
1) Target aggregation: If the target organization "%s" appears anywhere in the RESPONSE TEXT, collect EVERY occurrence.
   - Output ONE "mentioned_text" string that concatenates ALL distinct occurrences in order of appearance.
   - Use the exact delimiter:  ||  (space, two pipes, space) between occurrences.
   - Example: "DoorDash offers 24/7 support.  ||  Visit merchants.doordash.com for help.  ||  Phone support is available."

2) Span definition: An occurrence = the full sentence or bullet line that explicitly mentions the company, or a directly adjacent sentence that clearly continues the same thought.
   - If consecutive sentences reference the company as one continuous thought, keep them together as ONE occurrence.
   - Do not include unrelated surrounding text.

3) Variations allowed: Match common name variants, abbreviations, and brand/product names.

4) Domains are SECONDARY signals (not required):
   - Use domain mentions only to support detection when the explicit name is absent.
   - Count a domain as a valid mention only if it clearly belongs to the target organization.
   - Any subdomain of the listed roots counts (e.g., www., help., merchants.).
   - When both name and domain appear together, merge them into a single occurrence for that location.
   - Do NOT infer from partial/generic domain strings that could match other entities.
%s

5) Exclusions: Ignore any companies that appear in these instructions; analyze ONLY the text in the RESPONSE TEXT section.

6) De-duplication: If the same sentence appears more than once, include it only once.

7) Quality goal: Prefer including more valid occurrences over missing any. Do not omit valid target mentions.

## Output policy
- target_company: null if not mentioned anywhere; otherwise include name/rank/sentiment.
- mentioned_text: concatenation of ALL target occurrences using " || ".
- For competitors, apply the same span logic (concatenate their occurrences with " || ").

## Checklist before finalizing
- Did you search the entire RESPONSE TEXT for EVERY target occurrence?
- Is each occurrence a full sentence/bullet or continuous thought?
- Did you use " || " exactly as the delimiter?
- If using a domain, does it clearly belong to the target and is it used only as a supporting signal when the name is not present?
- Did you avoid adding any text not present in the RESPONSE TEXT?

## RESPONSE TEXT (analyze ONLY this):
"""
%s
"""`, targetCompany, websitesList, response)
}

func (s *dataExtractionService) buildClaimsExtractionPrompt(response, targetCompany string, orgWebsites []string) string {
	websitesList := ""
	if len(orgWebsites) > 0 {
		websitesList = "## ORGANIZATION DOMAINS (PRIMARY CLASSIFICATION):\n"
		for _, website := range orgWebsites {
			websitesList += fmt.Sprintf("- %s\n", website)
		}
		websitesList += "\n"
	}

	return fmt.Sprintf(`You are an expert fact-checker and information extraction specialist. Your task is to extract INDIVIDUAL factual claims from an AI response, breaking down complex statements into atomic, verifiable facts.

## CRITICAL INSTRUCTIONS: GRANULAR & VERBATIM EXTRACTION
⚠️ THREE KEY REQUIREMENTS:

1. **EXTRACT INDIVIDUAL CLAIMS**: Break down sentences containing multiple facts into separate claims. Each claim should contain exactly ONE verifiable fact.

2. **VERBATIM COPYING**: Extract claims EXACTLY as written in the source text. Do not:
   - Paraphrase or reword
   - Fix grammar or spelling
   - Add punctuation or capitalization
   - Remove any characters
   - Clean up formatting
   
Copy and paste the EXACT text fragments, but split them at natural fact boundaries.

3. **SENTIMENT & TARGET ANALYSIS**: For each claim, analyze:
   - **Sentiment**: positive, negative, or neutral based on the tone and language used
   - **Target Mentioned**: true if the target company "%s" is mentioned in this specific claim
   
   **IMPORTANT**: Extract ALL factual claims regardless of whether the target company is mentioned or not. The target_mentioned field is just for tracking purposes - do not filter claims based on target company presence.

## TARGET COMPANY INFORMATION

**Company Name**: "%s"
%s**Detection Criteria**:
- Exact name matches (case-insensitive)
- Common variations and abbreviations
- Brand names and subsidiaries  
- Indirect references ("we", "our company") only if clearly referring to target
- Website domain matches (if any of the organization domains are mentioned)

## WHAT CONSTITUTES A CLAIM

**Factual Claim**: A statement that can be verified as true or false. Claims typically include:
- Statistical statements ("X has 50%% market share")
- Comparative statements ("A is better/larger/faster than B")
- Feature descriptions ("Product X includes Y feature")
- Historical facts ("Company was founded in 2010")
- Current states ("Organization operates in 12 countries")
- Capabilities ("System can process 1000 transactions per second")
- Quantifiable attributes (prices, sizes, counts, percentages)

**NOT Claims** (Do not extract):
- Opinions without factual basis ("seems good", "might be useful")
- Vague generalizations ("many people think", "it's commonly believed")
- Questions or hypotheticals
- Pure subjective assessments ("beautiful design", "excellent choice")
- Future predictions without specific commitments

## SENTIMENT ANALYSIS GUIDELINES

**Positive**: Favorable language, benefits, recommendations, praise, advantages, success indicators
**Negative**: Criticism, problems, disadvantages, warnings, complaints, failure indicators  
**Neutral**: Factual statements without clear positive/negative tone, balanced information



## EXTRACTION RULES

1. **Individual Claim Extraction**:
   - Extract complete paragraphs or substantial text blocks as single claims
   - Keep related sentences together in the same claim
   - Only split when there's a clear topic shift or unrelated information
   - Aim for larger, more comprehensive claims rather than individual sentences
   - **PREFER PARAGRAPHS**: Extract entire paragraphs as single claims when possible
   - **AVOID OVER-SPLITTING**: Don't break up naturally flowing text into tiny pieces

2. **Preserve Context & Completeness**:
   - Include sufficient context to make each claim understandable and complete
   - Keep subjects with their predicates
   - Keep numbers with their units and context
   - Include any URLs or citations that appear within the claim text

3. **Splitting Guidelines**:
   - **KEEP TOGETHER**: Related sentences, consecutive statements, and flowing paragraphs
   - **ONLY SPLIT**: When there's a clear topic shift, new subject, or unrelated information
   - **PREFER LARGER BLOCKS**: Extract substantial text chunks rather than individual sentences
   - **PARAGRAPH-FIRST**: Start with entire paragraphs, only split if absolutely necessary
   - **AVOID CONJUNCTION SPLITTING**: Don't split at "and", "but", "or" unless topics are completely different

4. **Exact Character Matching**:
   - Preserve all punctuation marks (.,;:!?"'-—)
   - Keep original capitalization
   - Include any numbers, symbols, or special characters
   - Maintain spacing exactly as in original
   - Include URLs and citations exactly as they appear
   - EXCLUDE formatting elements like bullet points, numbering, headers from claim text

5. **No Artificial Limits**:
   - Extract ALL verifiable claims found in the response
   - No minimum or maximum number - extract every individual fact
   - Focus on meaningful, complete factual assertions
   - Don't prioritize or filter - include all factual assertions
   - **CRITICAL**: Extract claims whether or not they mention the target company - target_mentioned is for tracking only

## EXAMPLES

Given this response:
"TechFlow Solutions, founded in 2018, now serves over 10,000 enterprise clients. Their flagship product processes 2.5 million API calls per day with 99.9%% uptime. The company's revenue grew 145%% year-over-year to $50 million in 2023. According to their documentation (https://docs.techflow.com/metrics), TechFlow offers 24/7 phone support in 15 languages. Industry analysts rank them #3 in customer satisfaction."

Target company: "TechFlow Solutions"

Correct extraction (PREFERRED - KEEP PARAGRAPH TOGETHER):
[
  {
    "claim_text": "TechFlow Solutions, founded in 2018, now serves over 10,000 enterprise clients. Their flagship product processes 2.5 million API calls per day with 99.9%% uptime. The company's revenue grew 145%% year-over-year to $50 million in 2023. According to their documentation (https://docs.techflow.com/metrics), TechFlow offers 24/7 phone support in 15 languages. Industry analysts rank them #3 in customer satisfaction.",
    "sentiment": "positive",
    "target_mentioned": true
  }
]

Alternative acceptable extraction (ONLY if there are clear topic shifts):
[
  {
    "claim_text": "TechFlow Solutions, founded in 2018, now serves over 10,000 enterprise clients. Their flagship product processes 2.5 million API calls per day with 99.9%% uptime.",
    "sentiment": "positive",
    "target_mentioned": true
  },
  {
    "claim_text": "The company's revenue grew 145%% year-over-year to $50 million in 2023. According to their documentation (https://docs.techflow.com/metrics), TechFlow offers 24/7 phone support in 15 languages. Industry analysts rank them #3 in customer satisfaction.",
    "sentiment": "positive",
    "target_mentioned": true
  }
]

## RESPONSE TO ANALYZE
%s

## FINAL CHECKLIST
Before submitting each claim, verify:
✓ Is this EXACTLY as written in the source? (character-for-character match)
✓ Is this a complete, meaningful statement with sufficient context?
✓ Can this be verified as true or false?
✓ Have I preserved ALL punctuation and formatting?
✓ Did I resist the urge to "clean up" or "improve" the text?
✓ Did I correctly identify the sentiment (positive/negative/neutral)?
✓ Did I check if the target company "%s" is mentioned in this claim?
✓ Did I extract ALL factual claims regardless of target company presence?
✓ Is this claim substantial enough to be meaningful on its own?

Remember: Your role is extraction, not editing. The downstream system requires exact text matches.`, targetCompany, targetCompany, websitesList, response, targetCompany)
}

func (s *dataExtractionService) extractCitationsForClaim(ctx context.Context, claim *models.QuestionRunClaim, response string, orgWebsites []string) ([]*models.QuestionRunCitation, error) {
	fmt.Printf("[extractCitationsForClaim] 🔍 Processing citations for claim %s", claim.QuestionRunClaimID)

	prompt := s.buildCitationsExtractionPrompt(claim.ClaimText, response, orgWebsites)

	// Use a model that supports structured outputs
	var model openai.ChatModel
	if s.cfg.AzureOpenAIDeploymentName != "" {
		// Use Azure deployment name
		model = openai.ChatModel(s.cfg.AzureOpenAIDeploymentName)
		fmt.Printf("[extractCitationsForClaim] 🎯 Using Azure OpenAI deployment: %s", s.cfg.AzureOpenAIDeploymentName)
	} else {
		// Use standard OpenAI model
		model = openai.ChatModelGPT4_1
		fmt.Printf("[extractCitationsForClaim] 🎯 Using Standard OpenAI model: %s", model)
	}

	schemaParam := openai.ResponseFormatJSONSchemaJSONSchemaParam{
		Name:        "citations_extraction",
		Description: openai.String("Extract citations for a specific claim"),
		Schema:      GenerateSchema[CitationsExtractionResponse](),
		Strict:      openai.Bool(true),
	}

	fmt.Printf("[extractCitationsForClaim] 🚀 Making AI call for citations extraction...")

	params := openai.ChatCompletionNewParams{
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage("You are an expert researcher specializing in citation extraction and URL analysis. Extract URLs exactly as they appear in the text."),
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
		fmt.Printf("[extractCitationsForClaim] Setting temperature to 0.1 for model %s\n", model)
	} else {
		params.ReasoningEffort = "low"
		fmt.Printf("[extractCitationsForClaim] Skipping temperature setting for model gpt-5\n")
	}

	chatResponse, err := s.openAIClient.Chat.Completions.New(ctx, params)

	if err != nil {
		return nil, fmt.Errorf("failed to extract citations: %w", err)
	}

	fmt.Printf("[extractCitationsForClaim] ✅ AI call completed successfully")
	fmt.Printf("[extractCitationsForClaim]   - Input tokens: %d", chatResponse.Usage.PromptTokens)
	fmt.Printf("[extractCitationsForClaim]   - Output tokens: %d", chatResponse.Usage.CompletionTokens)

	if len(chatResponse.Choices) == 0 {
		return []*models.QuestionRunCitation{}, nil
	}

	responseContent := chatResponse.Choices[0].Message.Content

	var extractedData CitationsExtractionResponse
	if err := json.Unmarshal([]byte(responseContent), &extractedData); err != nil {
		return nil, fmt.Errorf("failed to parse citations extraction response: %w", err)
	}

	// Capture token and cost data from the AI call
	inputTokens := int(chatResponse.Usage.PromptTokens)
	outputTokens := int(chatResponse.Usage.CompletionTokens)
	totalCost := s.costService.CalculateCost("openai", string(model), inputTokens, outputTokens, false)

	var citations []*models.QuestionRunCitation
	now := time.Now()

	for i, citation := range extractedData.Citations {
		citations = append(citations, &models.QuestionRunCitation{
			QuestionRunCitationID: uuid.New(),
			QuestionRunClaimID:    claim.QuestionRunClaimID,
			SourceURL:             citation.SourceURL,
			CitationType:          citation.Type,
			CitationOrder:         i + 1,
			InputTokens:           &inputTokens,
			OutputTokens:          &outputTokens,
			TotalCost:             &totalCost,
			CreatedAt:             now,
			UpdatedAt:             now,
		})
	}

	fmt.Printf("[extractCitationsForClaim] ✅ Successfully extracted %d citations", len(citations))
	return citations, nil
}

func (s *dataExtractionService) buildCitationsExtractionPrompt(claimText, response string, orgWebsites []string) string {
	websitesList := ""
	if len(orgWebsites) > 0 {
		websitesList = "## ORGANIZATION DOMAINS (PRIMARY CLASSIFICATION):\n"
		for _, website := range orgWebsites {
			websitesList += fmt.Sprintf("- %s\n", website)
		}
		websitesList += "\n"
	}

	return fmt.Sprintf(`You are a precise citation extraction specialist. Your task is to find URLs that are DIRECTLY ASSOCIATED with the specific claim by being in the same contextual area of the response.

## ⚠️ CRITICAL RULES - READ CAREFULLY

1. **LOCATION-BASED EXTRACTION ONLY**: Only extract URLs that appear in the IMMEDIATE CONTEXT of the claim:
   - Same sentence as the claim
   - Same paragraph as the claim  
   - Immediately following the claim (within 1-2 sentences)
   - Parenthetical citations directly after the claim

2. **STRICT PROXIMITY RULE**: If a URL appears elsewhere in the response but NOT near the specific claim, DO NOT extract it.

3. **CONSERVATIVE APPROACH**: When in doubt, return empty array. It's better to miss a citation than to incorrectly assign one.

4. **NO CITATION REQUIREMENTS**: Many claims will have NO citations. This is perfectly normal and expected.

## VERBATIM URL EXTRACTION
When you do find a relevant URL, extract it EXACTLY as it appears:
- Copy every character, space, symbol, and typo EXACTLY
- Include all protocols (http://, https://, ftp://, etc.)
- Preserve all parameters (?param=value&other=data)
- Keep all anchors (#section)
- Maintain all slashes, dots, and special characters

%s## ⚠️ CRITICAL DOMAIN CLASSIFICATION SYSTEM - BE EXTREMELY PRECISE

**PRIMARY CITATION**: URL domain EXACTLY matches organization's official domains (listed above)
- **EXACT DOMAIN MATCH**: The URL's base domain must be IDENTICAL to one of the org domains
- **SUBDOMAIN MATCH**: URL domain must END WITH the org domain (e.g., blog.senso.ai matches senso.ai)
- **CASE INSENSITIVE**: Example.com = example.com
- **PROTOCOL IGNORED**: http:// vs https:// doesn't matter
- **PATH IGNORED**: Any path after domain doesn't affect matching

**SECONDARY CITATION**: Any other valid URL that does NOT match org domains
- News sites, research papers, government sites, academic sources
- Competitor websites, industry publications
- Social media, forums, documentation sites
- ANY URL that isn't from the organization's domains

**NO CITATION**: Return empty array when:
- Zero URLs found near the specific claim
- URLs exist elsewhere in response but not in claim's immediate context
- Only email addresses (person@domain.com)
- Only domain mentions without URL structure (just "google.com" as text)
- Only phone numbers or other non-web references

## URL DETECTION MASTERY

**What Counts as URL**:
- Full URLs: https://example.com/path
- Protocol-less: www.example.com/page, example.com/path
- Subdomains: blog.example.com, api.subdomain.example.com
- With parameters: site.com/page?id=123&type=data
- With anchors: example.com/doc#section-5
- With ports: server.com:8000/api
- IP addresses: 192.168.1.1/path, http://10.0.0.1:3000
- Even malformed: example.com//double-slash/page

**URL Context Patterns**:
- Parenthetical: "According to the study (https://research.com/paper)"
- Inline: "Visit our site at www.company.com for details"
- Formatted: "Source: https://news.com/article"
- Reference style: "See [1] https://example.com"
- Embedded in text: "The https://api.service.com endpoint provides..."

## ⚠️ CRITICAL DOMAIN EXTRACTION & MATCHING LOGIC - BE EXTREMELY CAREFUL

**STEP 1: Extract base domain from URL**
- https://blog.example.com/post → blog.example.com
- www.subdomain.site.org/page → subdomain.site.org
- http://server.com:8000/api → server.com
- https://api.subdomain.example.com/v1/endpoint → api.subdomain.example.com

**STEP 2: Compare against org domains (BE EXTREMELY PRECISE)**
- Remove protocols (http://, https://)
- Remove www. prefix for comparison
- Check if URL domain ENDS WITH any org domain (exact suffix match)
- Check if URL domain is IDENTICAL to any org domain

**STEP 3: Classification logic (BE CONSERVATIVE)**
- If domain EXACTLY matches org domain → PRIMARY
- If domain ENDS WITH org domain (subdomain) → PRIMARY
- If NO match found → SECONDARY
- If uncertain → SECONDARY (be conservative)

**CRITICAL EXAMPLES:**
- Org domain: senso.ai
- https://senso.ai/page → PRIMARY ✓
- https://www.senso.ai/page → PRIMARY ✓
- https://blog.senso.ai/page → PRIMARY ✓
- https://api.senso.ai/page → PRIMARY ✓
- https://cuinsights.com/blog → SECONDARY ✗ (completely different domain)
- https://senso-ai.com/page → SECONDARY ✗ (different domain, not subdomain)
- https://senso.ai.evil.com/page → SECONDARY ✗ (not ending with senso.ai)
- https://senso-ai.org/page → SECONDARY ✗ (different TLD)
- https://senso.ai.com/page → SECONDARY ✗ (different domain structure)

## PROXIMITY-BASED EXTRACTION EXAMPLES

**Example 1 - URLs in Same Sentence (EXTRACT)**
Claim: "Our internal analysis (https://docs.techflow.com/reports/q4-2024.pdf) shows 45%% growth"
Response context: "Our internal analysis (https://docs.techflow.com/reports/q4-2024.pdf) shows 45%% growth. This aligns with industry trends. Later in the document, we discuss partnerships with www.unrelated-site.com."

Expected output:
[
  {
    "source_url": "https://docs.techflow.com/reports/q4-2024.pdf",
    "type": "primary"
  }
]

**Example 1b - Different Domain (SECONDARY)**
Claim: "According to industry research (https://cuinsights.com/blog/story1), market growth is strong"
Response context: "According to industry research (https://cuinsights.com/blog/story1), market growth is strong. This aligns with our internal data."

Expected output:
[
  {
    "source_url": "https://cuinsights.com/blog/story1",
    "type": "secondary"
  }
]

**Example 2 - URL in Different Paragraph (DO NOT EXTRACT)**
Claim: "TechFlow is a leading provider of AI solutions"
Response context: "TechFlow is a leading provider of AI solutions. The company has grown significantly over the past year.

In other news, a recent study (https://research.com/ai-trends) showed market growth. TechFlow was not mentioned in this study."

Expected output: []

**Example 3 - No URLs Near Claim (DO NOT EXTRACT)**
Claim: "The company's revenue grew 145%% year-over-year"
Response context: "The company was founded in 2010 (https://company.com/history). The company's revenue grew 145%% year-over-year. This represents strong performance in the market."

Expected output: []

**Example 4 - URL Immediately After Claim (EXTRACT)**
Claim: "According to industry research, AI adoption has increased 300%%"
Response context: "According to industry research, AI adoption has increased 300%% (https://research.org/ai-report-2024). This trend is expected to continue."

Expected output:
[
  {
    "source_url": "https://research.org/ai-report-2024",
    "type": "secondary"
  }
]

**Example 5 - Multiple Claims, One Citation (CAREFUL ASSIGNMENT)**
Claim A: "AI is transforming industries"
Claim B: "Companies are investing heavily in AI"
Claim C: "The market is expected to grow significantly"
Response context: "AI is transforming industries. Companies are investing heavily in AI. The market is expected to grow significantly (https://market-analysis.com/ai-growth)."

For Claim A: Expected output: []
For Claim B: Expected output: []
For Claim C: Expected output: [{"source_url": "https://market-analysis.com/ai-growth", "type": "secondary"}]

## TARGET CLAIM TO ANALYZE
%s

## FULL RESPONSE TO SEARCH FOR URLS
%s

## EXTRACTION VERIFICATION CHECKLIST
Before finalizing each URL:
✓ Is this URL actually NEAR the specific claim in the response text?
✓ Am I searching only the immediate context around the claim, not the entire response?
✓ Did I copy the URL character-for-character with zero modifications?
✓ Did I correctly classify the domain type (primary vs secondary)?
✓ Am I comfortable returning empty array if no URLs are near this claim?

## ⚠️ CRITICAL DOMAIN VERIFICATION CHECKLIST
Before classifying as PRIMARY, verify:
✓ Does the URL domain EXACTLY match one of the org domains listed above?
✓ Does the URL domain END WITH one of the org domains (for subdomains)?
✓ Is this a genuine subdomain (e.g., blog.senso.ai for senso.ai)?
✓ Is this NOT a completely different domain (e.g., cuinsights.com ≠ senso.ai)?
✓ Is this NOT a similar but different domain (e.g., senso-ai.com ≠ senso.ai)?
✓ When in doubt about domain matching, classify as SECONDARY

## FINAL CRITICAL REMINDERS
- **PROXIMITY IS EVERYTHING**: Only extract URLs that are contextually close to the claim
- **EMPTY ARRAYS ARE NORMAL**: Most claims will have no citations - this is expected
- **CONSERVATIVE OVER AGGRESSIVE**: Better to miss a citation than assign incorrectly
- **CONTEXT MATTERS**: Look at where the claim appears in the response, not the entire response
- **NO CITATION PRESSURE**: Don't feel compelled to find citations for every claim
- **DOMAIN PRECISION IS CRITICAL**: Only classify as PRIMARY if domain EXACTLY matches org domains
- **WHEN IN DOUBT, SECONDARY**: If uncertain about domain matching, classify as secondary

## METHODOLOGY
1. Locate the exact claim text within the full response
2. Examine only the immediate surrounding context (same paragraph/sentence)
3. Extract URLs only from that localized area
4. Return empty array if no URLs found in that context`, websitesList, claimText, response)
}

// normalizeSentiment ensures sentiment is a valid enum value, defaulting to "neutral" for invalid inputs
func (s *dataExtractionService) normalizeSentiment(sentiment string) string {
	// Trim whitespace and convert to lowercase for comparison
	normalized := strings.TrimSpace(strings.ToLower(sentiment))

	switch normalized {
	case "positive":
		return "positive"
	case "negative":
		return "negative"
	case "neutral":
		return "neutral"
	default:
		// Handle empty strings, null values, whitespace, or any other invalid values
		return "neutral"
	}
}

func (s *dataExtractionService) convertSentimentToFloat(sentiment string) float64 {
	switch strings.ToLower(sentiment) {
	case "positive":
		return 1.0
	case "neutral":
		return 0.5
	case "negative":
		return 0.0
	default:
		return 0.5 // Default to neutral
	}
}

// GenerateNameVariations is the public method that generates brand name variations for mention detection
func (s *dataExtractionService) GenerateNameVariations(ctx context.Context, orgName string, websites []string) ([]string, error) {
	return s.generateNameVariations(ctx, orgName, websites)
}

// generateNameVariations is the internal implementation
func (s *dataExtractionService) generateNameVariations(ctx context.Context, orgName string, websites []string) ([]string, error) {
	fmt.Printf("[generateNameVariations] 🔍 Generating name variations for org: %s\n", orgName)

	websitesFormatted := ""
	for _, website := range websites {
		websitesFormatted += fmt.Sprintf("- %s\n", website)
	}

	prompt := fmt.Sprintf(`You are an expert in brand name analysis and variation generation. Your task is to generate a comprehensive list of brand name variations that a company might realistically use across different platforms, documents, and contexts.

Generate REALISTIC variations of this brand name that would actually be used by the company or found in business contexts. Focus on:

1. **Exact matches**: The brand name as provided
2. **Case variations**: lowercase, UPPERCASE, Title Case, camelCase
3. **Spacing variations**: CRITICAL for compound words - always include both spaced and unspaced versions:
   - Compound words: "SunLife" → "Sun Life", "TotalExpert" → "Total Expert"
   - With spaces, without spaces, with hyphens, with underscores
   - For ANY compound-looking word, generate the spaced version
4. **Legal/formal variations**: Including "Inc", "LLC", "Ltd", "Corp", etc. (only if realistic for this type of company)
5. **Natural shortened versions**: Logical shortened forms (e.g., "Senso.ai" → "Senso", "Microsoft Corporation" → "Microsoft")
6. **Realistic acronyms**: Only create acronyms from multi-word names where it makes sense:
   - "Bellweather Community Credit Union" → "BCCU"
   - "American Express" → "AmEx" or "AE"
   - Single word brands typically don't have meaningful acronyms
7. **Domain-based variations**: Simple domain formats without full URLs (e.g., "senso" from "senso.ai")

IMPORTANT CONSTRAINTS:
- Do NOT include full email addresses (no @domain.com formats)
- Do NOT include full website URLs (no http:// or www. formats)
- Do NOT create arbitrary abbreviations or random letter combinations
- Only create acronyms for multi-word brand names where each word contributes a letter
- Only include variations that would realistically be used in professional business contexts
- Focus on how the brand name would naturally be written, typed, or formatted

**CRITICAL: For compound words, ALWAYS generate spaced versions**

Examples:
- "Senso.ai" → Good: Senso.ai, senso.ai, SENSO.AI, Senso, senso, SENSO, SensoAI, sensoai
- "Senso.ai" → Bad: S.AI, SAI, support@senso.ai, www.senso.ai
- "SunLife" → MUST include: SunLife, Sun Life, sunlife, sun life, SUNLIFE, SUN LIFE, Sun-Life, sun-life
- "TotalExpert" → MUST include: TotalExpert, Total Expert, totalexpert, total expert, TOTALEXPERT, TOTAL EXPERT, Total-Expert, total-expert
- "Tech Corp Solutions" → Good: TCS, Tech Corp, TechCorp, Tech Corp Solutions
- "Apple" → Good: Apple, apple, APPLE (no meaningful acronym for single word)

Instructions:
- Include the original name exactly as provided
- Generate 15-25 realistic variations (quality over quantity)
- Each variation should have a clear reason for existing
- For multi-word names, consider logical acronyms using first letters
- For compound names or names with extensions (.ai, .com), consider the root word
- **MANDATORY**: If the brand name looks like a compound word (two or more words joined together), generate the spaced version
- Avoid nonsensical permutations or made-up abbreviations

Return only the list of name variations, no explanations.

The brand name is %s

Associated websites:
%s`, "`"+orgName+"`", websitesFormatted)

	// Use gpt-4.1-mini for name variations (cost-effective)
	var model openai.ChatModel
	if s.cfg.AzureOpenAIDeploymentName != "" {
		model = openai.ChatModel("gpt-5")
		fmt.Printf("[generateNameVariations] 🎯 Using Azure SDK with model: gpt-4.1-mini\n")
	} else {
		model = openai.ChatModel("gpt-5")
		fmt.Printf("[generateNameVariations] 🎯 Using Standard OpenAI model: gpt-4.1-mini\n")
	}

	schemaParam := openai.ResponseFormatJSONSchemaJSONSchemaParam{
		Name:        "name_variations_extraction",
		Description: openai.String("Generate realistic brand name variations"),
		Schema:      GenerateSchema[NameListResponse](),
		Strict:      openai.Bool(true),
	}

	fmt.Printf("[generateNameVariations] 🚀 Making AI call for name variations...\n")

	// Create the extraction request with structured output
	params := openai.ChatCompletionNewParams{
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage("You are an expert in brand name analysis and variation generation. Generate realistic brand name variations that would actually be used in business contexts."),
			openai.UserMessage(prompt),
		},
		Model:               model,
		MaxCompletionTokens: openai.Int(5000), // Prevent truncation of JSON response (Azure-compatible parameter)
		ResponseFormat: openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONSchema: &openai.ResponseFormatJSONSchemaParam{JSONSchema: schemaParam},
		},
	}

	// Conditional Temperature Setting
	if !strings.HasPrefix(string(model), "gpt-5") {
		params.Temperature = openai.Float(0.3) // Keep low for consistency in extraction when verified
		fmt.Printf("[generateNameVariations] Setting temperature to 0.3 for model %s\n", model)
	} else {
		params.ReasoningEffort = "low"
		fmt.Printf("[generateNameVariations] Skipping temperature setting for model gpt-5\n")
	}

	chatResponse, err := s.openAIClient.Chat.Completions.New(ctx, params)

	if err != nil {
		return nil, fmt.Errorf("failed to generate name variations: %w", err)
	}

	fmt.Printf("[generateNameVariations] ✅ AI call completed successfully\n")
	fmt.Printf("[generateNameVariations]   - Input tokens: %d\n", chatResponse.Usage.PromptTokens)
	fmt.Printf("[generateNameVariations]   - Output tokens: %d\n", chatResponse.Usage.CompletionTokens)

	// Parse the response
	if len(chatResponse.Choices) == 0 {
		return nil, fmt.Errorf("no response choices returned from OpenAI")
	}

	responseContent := chatResponse.Choices[0].Message.Content

	// Parse the structured response
	var extractedData NameListResponse
	if err := json.Unmarshal([]byte(responseContent), &extractedData); err != nil {
		return nil, fmt.Errorf("failed to parse name variations response: %w", err)
	}

	fmt.Printf("[generateNameVariations] ✅ Generated %d name variations\n", len(extractedData.Names))
	return extractedData.Names, nil
}

// Response types for network org extraction
type NetworkOrgEvaluationResponse struct {
	MentionText string `json:"mention_text"`
	Sentiment   string `json:"sentiment"`
	Citation    bool   `json:"citation"`
	MentionRank int    `json:"mention_rank"`
}

// Note: NameListResponse and CompetitorListResponse are defined in org_evaluation_service.go

// Helper functions for pointer types
func stringPtr(s string) *string {
	return &s
}

func intPtr(i int) *int {
	return &i
}

func float64Ptr(f float64) *float64 {
	return &f
}
