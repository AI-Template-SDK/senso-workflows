// services/org_evaluation_service.go
package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/AI-Template-SDK/senso-api/pkg/models"
	"github.com/AI-Template-SDK/senso-api/pkg/repositories/interfaces"
	"github.com/AI-Template-SDK/senso-workflows/internal/config"
	workflowModels "github.com/AI-Template-SDK/senso-workflows/internal/models"
	"github.com/google/uuid"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/azure"
	"github.com/openai/openai-go/option"
	"golang.org/x/net/publicsuffix"
)

type orgEvaluationService struct {
	cfg          *config.Config
	openAIClient *openai.Client
	costService  CostService
	repos        *RepositoryManager
}

func NewOrgEvaluationService(cfg *config.Config, repos *RepositoryManager) OrgEvaluationService {
	fmt.Printf("[NewOrgEvaluationService] Creating service with OpenAI key (length: %d)\n", len(cfg.OpenAIAPIKey))

	var client openai.Client

	// Check if Azure configuration is available
	if cfg.AzureOpenAIEndpoint != "" && cfg.AzureOpenAIKey != "" && cfg.AzureOpenAIDeploymentName != "" {
		// Use Azure OpenAI
		client = openai.NewClient(
			azure.WithEndpoint(cfg.AzureOpenAIEndpoint, "2024-12-01-preview"),
			azure.WithAPIKey(cfg.AzureOpenAIKey),
		)
		fmt.Printf("[NewOrgEvaluationService] ✅ Using Azure OpenAI")
		fmt.Printf("[NewOrgEvaluationService]   - Endpoint: %s", cfg.AzureOpenAIEndpoint)
		fmt.Printf("[NewOrgEvaluationService]   - Deployment: %s", cfg.AzureOpenAIDeploymentName)
		fmt.Printf("[NewOrgEvaluationService]   - SDK: github.com/openai/openai-go with Azure middleware")
	} else {
		// Use standard OpenAI
		client = openai.NewClient(
			option.WithAPIKey(cfg.OpenAIAPIKey),
		)
		fmt.Printf("[NewOrgEvaluationService] ✅ Using Standard OpenAI")
		fmt.Printf("[NewOrgEvaluationService]   - API: api.openai.com")
		fmt.Printf("[NewOrgEvaluationService]   - SDK: github.com/openai/openai-go")
	}

	return &orgEvaluationService{
		cfg:          cfg,
		openAIClient: &client,
		costService:  NewCostService(),
		repos:        repos,
	}
}

// Structured response types for the new pipeline
type NameListResponse struct {
	Names []string `json:"names" jsonschema_description:"List of realistic brand name variations"`
}

type OrgEvaluationResponse struct {
	Sentiment   string `json:"sentiment" jsonschema_description:"Sentiment: positive, negative, or neutral"`
	MentionText string `json:"mention_text" jsonschema_description:"All text mentioning the organization with exact formatting preserved, separated by ||"`
}

type CompetitorListResponse struct {
	Competitors []string `json:"competitors" jsonschema_description:"List of competitor names mentioned in the response"`
}

type CitationInfo struct {
	URL  string `json:"url"`
	Type string `json:"type"` // "primary" or "secondary"
}

// GenerateNameVariations implements the get_names() function from Python
func (s *orgEvaluationService) GenerateNameVariations(ctx context.Context, orgName string, websites []string) ([]string, error) {
	fmt.Printf("[GenerateNameVariations] 🔍 Generating name variations for org: %s\n", orgName)

	websitesFormatted := ""
	for _, website := range websites {
		websitesFormatted += fmt.Sprintf("- %s\n", website)
	}

	prompt := fmt.Sprintf(`You are an expert in brand name analysis and variation generation. Your task is to generate a comprehensive list of brand name variations that a company might realistically use across different platforms, documents, and contexts.

Generate REALISTIC variations of this brand name that would actually be used by the company or found in business contexts. Focus on:

1. **Exact matches**: The brand name as provided
2. **Case variations**: lowercase, UPPERCASE, Title Case, camelCase
3. **Spacing variations**: with spaces, without spaces, with hyphens, with underscores
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

Examples:
- "Senso.ai" → Good: Senso.ai, senso.ai, SENSO.AI, Senso, senso, SENSO, SensoAI, sensoai
- "Senso.ai" → Bad: S.AI, SAI, support@senso.ai, www.senso.ai
- "Tech Corp Solutions" → Good: TCS, Tech Corp, TechCorp, Tech Corp Solutions
- "Apple" → Good: Apple, apple, APPLE (no meaningful acronym for single word)

Instructions:
- Include the original name exactly as provided
- Generate 15-25 realistic variations (quality over quantity)
- Each variation should have a clear reason for existing
- For multi-word names, consider logical acronyms using first letters
- For compound names or names with extensions (.ai, .com), consider the root word
- Avoid nonsensical permutations or made-up abbreviations

Return only the list of name variations, no explanations.

The brand name is %s

Associated websites:
%s`, "`"+orgName+"`", websitesFormatted)

	// ALWAYS use Azure SDK with gpt-4.1-mini for name variations
	model := openai.ChatModel("gpt-4.1-mini")
	fmt.Printf("[GenerateNameVariations] 🎯 Using Azure SDK with model: %s", model)

	schemaParam := openai.ResponseFormatJSONSchemaJSONSchemaParam{
		Name:        "name_variations_extraction",
		Description: openai.String("Generate realistic brand name variations"),
		Schema:      GenerateSchema[NameListResponse](),
		Strict:      openai.Bool(true),
	}

	fmt.Printf("[GenerateNameVariations] 🚀 Making AI call for name variations...")

	// Create the extraction request with structured output
	chatResponse, err := s.openAIClient.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage("You are an expert in brand name analysis and variation generation. Generate realistic brand name variations that would actually be used in business contexts."),
			openai.UserMessage(prompt),
		},
		Model: model,
		ResponseFormat: openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONSchema: &openai.ResponseFormatJSONSchemaParam{JSONSchema: schemaParam},
		},
		Temperature: openai.Float(0.3), // Low temperature for consistent variations
	})

	if err != nil {
		return nil, fmt.Errorf("failed to generate name variations: %w", err)
	}

	fmt.Printf("[GenerateNameVariations] ✅ AI call completed successfully")
	fmt.Printf("[GenerateNameVariations]   - Input tokens: %d", chatResponse.Usage.PromptTokens)
	fmt.Printf("[GenerateNameVariations]   - Output tokens: %d", chatResponse.Usage.CompletionTokens)

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

	fmt.Printf("[GenerateNameVariations] ✅ Generated %d name variations", len(extractedData.Names))
	return extractedData.Names, nil
}

// ExtractOrgEvaluation implements the get_mention_text() function from Python
func (s *orgEvaluationService) ExtractOrgEvaluation(ctx context.Context, questionRunID, orgID uuid.UUID, orgName string, orgWebsites []string, nameVariations []string, responseText string) (*OrgEvaluationResult, error) {
	fmt.Printf("[ExtractOrgEvaluation] 🔍 Processing org evaluation for question run %s, org %s\n", questionRunID, orgName)

	nameVariationsStr := strings.Join(nameVariations, ", ")

	prompt := fmt.Sprintf(`You are an expert text extraction specialist. The target organization IS MENTIONED in this text. Your task is to extract ALL text that mentions the organization and determine the overall sentiment.

**TARGET ORGANIZATION:** %s
**Organization name variations:** %s

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

**EXAMPLES:**

Table mention: "| TechFlow Bank | https://techflow.com | Best rates |"
→ Extract: "| TechFlow Bank | https://techflow.com | Best rates |"

Multiple mentions: "**TechFlow Bank** offers great rates. Visit TechFlow Bank today."
→ Extract: "**TechFlow Bank** offers great rates. || Visit TechFlow Bank today."

List with citation: "- TechFlow Bank (https://techflow.com) - Recommended"
→ Extract: "- TechFlow Bank (https://techflow.com) - Recommended"

**TASK 2: DETERMINE SENTIMENT**

Analyze the overall sentiment toward the target organization across all mentions:
- **positive**: Favorable language, praise, recommendations ("excellent", "best", "recommended", "leading")
- **negative**: Critical language, problems, warnings ("poor", "issues", "avoid", "problematic")
- **neutral**: Factual, descriptive, balanced content without clear bias

**RESPONSE TO ANALYZE:**
`+"`"+`
%s
`+"`"+`

**OUTPUT REQUIREMENTS:**
- mention_text: ALL extracted text with perfect formatting, separated by " || "
- sentiment: exactly one of "positive", "negative", or "neutral" (lowercase)`, "`"+orgName+"`", nameVariationsStr, responseText)

	// ALWAYS use Azure SDK with gpt-4.1 for org evaluation
	model := openai.ChatModel("gpt-4.1")
	fmt.Printf("[ExtractOrgEvaluation] 🎯 Using Azure SDK with model: %s", model)

	schemaParam := openai.ResponseFormatJSONSchemaJSONSchemaParam{
		Name:        "org_evaluation_extraction",
		Description: openai.String("Extract organization evaluation data from AI response"),
		Schema:      GenerateSchema[OrgEvaluationResponse](),
		Strict:      openai.Bool(true),
	}

	fmt.Printf("[ExtractOrgEvaluation] 🚀 Making AI call for org evaluation...")

	// Create the extraction request with structured output
	chatResponse, err := s.openAIClient.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage("You are an expert text extraction specialist. Extract mention text with perfect formatting and determine sentiment. The organization is already confirmed to be mentioned."),
			openai.UserMessage(prompt),
		},
		Model: model,
		ResponseFormat: openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONSchema: &openai.ResponseFormatJSONSchemaParam{JSONSchema: schemaParam},
		},
		Temperature: openai.Float(0.1), // Low temperature for consistent extraction
	})

	if err != nil {
		return nil, fmt.Errorf("failed to extract org evaluation: %w", err)
	}

	fmt.Printf("[ExtractOrgEvaluation] ✅ AI call completed successfully")
	fmt.Printf("[ExtractOrgEvaluation]   - Input tokens: %d", chatResponse.Usage.PromptTokens)
	fmt.Printf("[ExtractOrgEvaluation]   - Output tokens: %d", chatResponse.Usage.CompletionTokens)

	// Parse the response
	if len(chatResponse.Choices) == 0 {
		return nil, fmt.Errorf("no response choices returned from OpenAI")
	}

	responseContent := chatResponse.Choices[0].Message.Content

	// Parse the structured response
	var extractedData OrgEvaluationResponse
	if err := json.Unmarshal([]byte(responseContent), &extractedData); err != nil {
		return nil, fmt.Errorf("failed to parse org evaluation response: %w", err)
	}

	// Capture token and cost data from the AI call
	inputTokens := int(chatResponse.Usage.PromptTokens)
	outputTokens := int(chatResponse.Usage.CompletionTokens)
	totalCost := s.costService.CalculateCost("openai", string(model), inputTokens, outputTokens, false)

	// Create the org evaluation model
	now := time.Now()
	orgEval := &models.OrgEval{
		OrgEvalID:     uuid.New(),
		QuestionRunID: questionRunID,
		OrgID:         orgID,
		Mentioned:     true,  // We already know it's mentioned from pre-filtering
		Citation:      false, // Will be set by separate citation extraction if needed
		InputTokens:   &inputTokens,
		OutputTokens:  &outputTokens,
		TotalCost:     &totalCost,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	// Set the LLM-extracted fields
	if extractedData.Sentiment != "" {
		orgEval.Sentiment = &extractedData.Sentiment
	}
	if extractedData.MentionText != "" {
		orgEval.MentionText = &extractedData.MentionText
	}
	// Set mention rank to 1 since we know it's mentioned and prominent enough to trigger extraction
	mentionRank := 1
	orgEval.MentionRank = &mentionRank

	fmt.Printf("[ExtractOrgEvaluation] ✅ Created org evaluation: mentioned=true, sentiment=%s, mention_text_length=%d",
		extractedData.Sentiment, len(extractedData.MentionText))

	return &OrgEvaluationResult{
		Evaluation:   orgEval,
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		TotalCost:    totalCost,
	}, nil
}

// ExtractCompetitors implements the get_competitors() function from Python
func (s *orgEvaluationService) ExtractCompetitors(ctx context.Context, questionRunID, orgID uuid.UUID, orgName string, responseText string) (*CompetitorExtractionResult, error) {
	fmt.Printf("[ExtractCompetitors] 🔍 Processing competitors for question run %s, org %s\n", questionRunID, orgName)

	prompt := fmt.Sprintf("You are an expert in competitive analysis and brand identification. Your task is to identify ALL competitor brands, companies, products, or services mentioned in the response text that are NOT the target organization.\n\n**TARGET ORGANIZATION:** %s\n\n**COMPETITOR IDENTIFICATION RULES:**\n\n1. **What to Include:**\n   - Company names (e.g., \"Microsoft\", \"Google\", \"Apple\")\n   - Product names (e.g., \"ChatGPT\", \"Claude\", \"Gemini\", \"Perplexity\")\n   - Service names (e.g., \"Ahrefs Brand Radar\", \"Surfer SEO AI Tracker\")\n   - Platform names (e.g., \"LinkedIn\", \"Facebook\", \"Twitter\")\n   - Tool names (e.g., \"Profound\", \"Promptmonitor\", \"Writesonic GEO Platform\")\n   - Any branded entity that could be considered competition or alternative\n\n2. **What to Exclude:**\n   - The target organization itself and its variations\n   - Generic terms (e.g., \"AI tools\", \"analytics platforms\", \"search engines\")\n   - Non-competitive entities (e.g., \"users\", \"customers\", \"developers\")\n   - Technical terms or concepts (e.g., \"machine learning\", \"natural language processing\")\n   - Industry terms (e.g., \"credit unions\", \"financial services\")\n\n3. **Extraction Guidelines:**\n   - Extract the most commonly used or official name for each competitor\n   - If a company has multiple products mentioned, list each product separately\n   - Remove duplicates and variations of the same entity\n   - Focus on entities that could be considered alternatives or competitors\n   - Include both direct competitors and indirect competitors mentioned\n\n**EXAMPLES:**\n\nExample 1: \"Leading AI tools include ChatGPT, Claude, Gemini, and Senso.ai for content optimization.\"\n→ Extract: [\"ChatGPT\", \"Claude\", \"Gemini\"] (exclude Senso.ai as it's the target)\n\nExample 2: \"Microsoft's Azure competes with Google Cloud and Amazon Web Services in the enterprise market.\"\n→ Extract: [\"Microsoft\", \"Azure\", \"Google Cloud\", \"Amazon Web Services\"]\n\nExample 3: \"Popular analytics platforms like Google Analytics, Adobe Analytics, and Mixpanel offer similar features.\"\n→ Extract: [\"Google Analytics\", \"Adobe Analytics\", \"Mixpanel\"]\n\n**RESPONSE TO ANALYZE:**\n```\n%s\n```\n\n**INSTRUCTIONS:**\n- Return only the list of competitor names\n- Use the most recognizable/official name for each competitor\n- Remove any duplicates or very similar variations\n- If no competitors are mentioned, return an empty list\n- Do not include the target organization or generic terms", "`"+orgName+"`", responseText)

	// ALWAYS use Azure SDK with gpt-4.1-mini for competitors
	model := openai.ChatModel("gpt-4.1-mini")
	fmt.Printf("[ExtractCompetitors] 🎯 Using Azure SDK with model: %s", model)

	schemaParam := openai.ResponseFormatJSONSchemaJSONSchemaParam{
		Name:        "competitor_extraction",
		Description: openai.String("Extract competitor names from AI response"),
		Schema:      GenerateSchema[CompetitorListResponse](),
		Strict:      openai.Bool(true),
	}

	fmt.Printf("[ExtractCompetitors] 🚀 Making AI call for competitor extraction...")

	// Create the extraction request with structured output
	chatResponse, err := s.openAIClient.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage("You are an expert in competitive analysis and brand identification. Extract competitor names accurately and comprehensively."),
			openai.UserMessage(prompt),
		},
		Model: model,
		ResponseFormat: openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONSchema: &openai.ResponseFormatJSONSchemaParam{JSONSchema: schemaParam},
		},
		Temperature: openai.Float(0.1), // Low temperature for consistent extraction
	})

	if err != nil {
		return nil, fmt.Errorf("failed to extract competitors: %w", err)
	}

	fmt.Printf("[ExtractCompetitors] ✅ AI call completed successfully")
	fmt.Printf("[ExtractCompetitors]   - Input tokens: %d", chatResponse.Usage.PromptTokens)
	fmt.Printf("[ExtractCompetitors]   - Output tokens: %d", chatResponse.Usage.CompletionTokens)

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
	var competitors []*models.OrgCompetitor
	now := time.Now()

	for _, competitorName := range extractedData.Competitors {
		if strings.TrimSpace(competitorName) == "" {
			continue // Skip empty names
		}

		competitor := &models.OrgCompetitor{
			OrgCompetitorID: uuid.New(),
			QuestionRunID:   questionRunID,
			OrgID:           orgID,
			Name:            strings.TrimSpace(competitorName),
			InputTokens:     &inputTokens,
			OutputTokens:    &outputTokens,
			TotalCost:       &totalCost,
			CreatedAt:       now,
			UpdatedAt:       now,
		}

		competitors = append(competitors, competitor)
	}

	fmt.Printf("[ExtractCompetitors] ✅ Extracted %d competitors", len(competitors))
	return &CompetitorExtractionResult{
		Competitors:  competitors,
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		TotalCost:    totalCost,
	}, nil
}

// ExtractCitations implements the extract_citations() function from Python
func (s *orgEvaluationService) ExtractCitations(ctx context.Context, questionRunID, orgID uuid.UUID, responseText string, orgWebsites []string) (*CitationExtractionResult, error) {
	fmt.Printf("[ExtractCitations] 🔍 Processing citations for question run %s, org %s\n", questionRunID, orgID)

	// Regex pattern to match URLs (Go-compatible version of Python regex)
	// Original Python used negative lookahead (?!www) which Go doesn't support
	// Workaround: Split into explicit alternatives instead of using negative lookahead
	citationPattern := regexp.MustCompile(`https?://www\.[a-zA-Z0-9][a-zA-Z0-9-]+[a-zA-Z0-9]\.[^\s,.)}\]]{2,}|https?://[a-zA-Z0-9][a-zA-Z0-9-]+[a-zA-Z0-9]\.[^\s,.)}\]]{2,}|www\.[a-zA-Z0-9][a-zA-Z0-9-]+[a-zA-Z0-9]\.[^\s,.)}\]]{2,}|[a-zA-Z0-9-]+\.[a-zA-Z]{2,6}\.[^\s,.)}\]]{2,}`)

	// Find all potential citations
	matches := citationPattern.FindAllString(responseText, -1)

	var citations []*models.OrgCitation
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

		citation := &models.OrgCitation{
			OrgCitationID: uuid.New(),
			QuestionRunID: questionRunID,
			OrgID:         orgID,
			URL:           url,
			Type:          citationType,
			CreatedAt:     now,
			UpdatedAt:     now,
		}

		citations = append(citations, citation)
		seenURLs[url] = true
	}

	fmt.Printf("[ExtractCitations] ✅ Extracted %d citations (%d primary, %d secondary)",
		len(citations),
		countCitationsByType(citations, "primary"),
		countCitationsByType(citations, "secondary"))

	// Citations use regex (no AI cost), but we return consistent structure
	return &CitationExtractionResult{
		Citations:    citations,
		InputTokens:  0,
		OutputTokens: 0,
		TotalCost:    0.0,
	}, nil
}

// ProcessOrgQuestionRuns processes all question runs for an organization
func (s *orgEvaluationService) ProcessOrgQuestionRuns(ctx context.Context, orgID uuid.UUID, orgName string, orgWebsites []string, questionRuns []*models.QuestionRun) (*OrgEvaluationSummary, error) {
	fmt.Printf("[ProcessOrgQuestionRuns] 🔄 Processing %d question runs for org %s\n", len(questionRuns), orgName)

	summary := &OrgEvaluationSummary{
		ProcessingErrors: make([]string, 0),
	}

	for _, questionRun := range questionRuns {
		// Skip question runs without response text
		if questionRun.ResponseText == nil || *questionRun.ResponseText == "" {
			summary.ProcessingErrors = append(summary.ProcessingErrors,
				fmt.Sprintf("Question run %s has no response text", questionRun.QuestionRunID))
			continue
		}

		responseText := *questionRun.ResponseText
		summary.TotalProcessed++

		// Generate name variations for this org
		nameVariations, err := s.GenerateNameVariations(ctx, orgName, orgWebsites)
		if err != nil {
			summary.ProcessingErrors = append(summary.ProcessingErrors,
				fmt.Sprintf("Failed to generate name variations for question run %s: %v", questionRun.QuestionRunID, err))
			continue
		}

		// Extract org evaluation
		evalResult, err := s.ExtractOrgEvaluation(ctx, questionRun.QuestionRunID, orgID, orgName, orgWebsites, nameVariations, responseText)
		if err != nil {
			summary.ProcessingErrors = append(summary.ProcessingErrors,
				fmt.Sprintf("Failed to extract evaluation for question run %s: %v", questionRun.QuestionRunID, err))
		} else {
			// Store evaluation in database
			if err := s.repos.OrgEvalRepo.Create(ctx, evalResult.Evaluation); err != nil {
				summary.ProcessingErrors = append(summary.ProcessingErrors,
					fmt.Sprintf("Failed to store evaluation for question run %s: %v", questionRun.QuestionRunID, err))
			} else {
				summary.TotalEvaluations++
				summary.TotalCost += evalResult.TotalCost
			}
		}

		// Extract competitors
		competitorResult, err := s.ExtractCompetitors(ctx, questionRun.QuestionRunID, orgID, orgName, responseText)
		if err != nil {
			summary.ProcessingErrors = append(summary.ProcessingErrors,
				fmt.Sprintf("Failed to extract competitors for question run %s: %v", questionRun.QuestionRunID, err))
		} else {
			// Store competitors in database
			for _, competitor := range competitorResult.Competitors {
				if err := s.repos.OrgCompetitorRepo.Create(ctx, competitor); err != nil {
					summary.ProcessingErrors = append(summary.ProcessingErrors,
						fmt.Sprintf("Failed to store competitor %s for question run %s: %v", competitor.Name, questionRun.QuestionRunID, err))
				} else {
					summary.TotalCompetitors++
				}
			}
			summary.TotalCost += competitorResult.TotalCost
		}

		// Extract citations
		citationResult, err := s.ExtractCitations(ctx, questionRun.QuestionRunID, orgID, responseText, orgWebsites)
		if err != nil {
			summary.ProcessingErrors = append(summary.ProcessingErrors,
				fmt.Sprintf("Failed to extract citations for question run %s: %v", questionRun.QuestionRunID, err))
		} else {
			// Store citations in database
			for _, citation := range citationResult.Citations {
				if err := s.repos.OrgCitationRepo.Create(ctx, citation); err != nil {
					summary.ProcessingErrors = append(summary.ProcessingErrors,
						fmt.Sprintf("Failed to store citation %s for question run %s: %v", citation.URL, questionRun.QuestionRunID, err))
				} else {
					summary.TotalCitations++
				}
			}
			summary.TotalCost += citationResult.TotalCost
		}

		fmt.Printf("[ProcessOrgQuestionRuns] ✅ Processed question run %s", questionRun.QuestionRunID)
	}

	fmt.Printf("[ProcessOrgQuestionRuns] 🎉 Processing complete: %d processed, %d evaluations, %d citations, %d competitors, $%.6f total cost, %d errors",
		summary.TotalProcessed, summary.TotalEvaluations, summary.TotalCitations, summary.TotalCompetitors, summary.TotalCost, len(summary.ProcessingErrors))

	return summary, nil
}

// RunQuestionMatrixWithOrgEvaluation executes questions and processes with org evaluation methodology
func (s *orgEvaluationService) RunQuestionMatrixWithOrgEvaluation(ctx context.Context, orgDetails *RealOrgDetails, batchID uuid.UUID) (*OrgEvaluationSummary, error) {
	fmt.Printf("[RunQuestionMatrixWithOrgEvaluation] 🚀 Starting question matrix with org evaluation for org: %s (ID: %s)\n",
		orgDetails.Org.Name, orgDetails.Org.OrgID)
	fmt.Printf("[RunQuestionMatrixWithOrgEvaluation] 📋 Processing %d org-specific questions across %d models and %d locations\n",
		len(orgDetails.Questions), len(orgDetails.Models), len(orgDetails.Locations))

	summary := &OrgEvaluationSummary{
		ProcessingErrors: make([]string, 0),
	}

	// Step 1: Generate name variations ONCE for the entire org (not per question)
	fmt.Printf("[RunQuestionMatrixWithOrgEvaluation] 🔍 Generating name variations for org: %s\n", orgDetails.Org.Name)
	nameVariations, err := s.GenerateNameVariations(ctx, orgDetails.Org.Name, orgDetails.Websites)
	if err != nil {
		return nil, fmt.Errorf("failed to generate name variations: %w", err)
	}
	fmt.Printf("[RunQuestionMatrixWithOrgEvaluation] ✅ Generated %d name variations for org\n", len(nameVariations))

	// Track all created question runs for is_latest flag management
	var allRuns []*models.QuestionRun

	// Process each question across all model×location combinations
	// NOTE: orgDetails.Questions contains ONLY questions belonging to this specific org
	// (filtered by GeoQuestionRepo.GetByOrgWithTags() in OrgService.GetOrgDetails())
	for i, questionWithTags := range orgDetails.Questions {
		question := questionWithTags.Question
		fmt.Printf("[RunQuestionMatrixWithOrgEvaluation] 📝 Question %d/%d: %s (ID: %s)\n",
			i+1, len(orgDetails.Questions), question.QuestionText, question.GeoQuestionID)

		// Process across all model×location combinations for this question
		for _, model := range orgDetails.Models {
			for _, location := range orgDetails.Locations {
				fmt.Printf("[RunQuestionMatrixWithOrgEvaluation] 🔄 Processing question %s with model %s at location %s\n",
					question.GeoQuestionID, model.Name, location.CountryCode)

				// Execute AI call to get response
				aiResponse, err := s.executeAICall(ctx, question.QuestionText, model.Name, location)
				if err != nil {
					summary.ProcessingErrors = append(summary.ProcessingErrors,
						fmt.Sprintf("Failed to execute AI call for question %s with model %s: %v", question.GeoQuestionID, model.Name, err))
					continue
				}

				// Create question run record
				questionRun := &models.QuestionRun{
					QuestionRunID: uuid.New(),
					GeoQuestionID: question.GeoQuestionID,
					ModelID:       &model.GeoModelID,
					LocationID:    &location.OrgLocationID,
					ResponseText:  &aiResponse.Response,
					InputTokens:   &aiResponse.InputTokens,
					OutputTokens:  &aiResponse.OutputTokens,
					TotalCost:     &aiResponse.Cost,
					BatchID:       &batchID,              // Link to batch
					RunModel:      &model.Name,           // Model name string
					RunCountry:    &location.CountryCode, // Country code string
					RunRegion:     &location.RegionName,  // Region name string
					IsLatest:      true,                  // Mark as latest for now
					CreatedAt:     time.Now(),
					UpdatedAt:     time.Now(),
				}

				// Store question run in database
				if err := s.repos.QuestionRunRepo.Create(ctx, questionRun); err != nil {
					summary.ProcessingErrors = append(summary.ProcessingErrors,
						fmt.Sprintf("Failed to store question run for question %s: %v", question.GeoQuestionID, err))
					continue
				}

				// Track this run for is_latest flag management
				allRuns = append(allRuns, questionRun)

				fmt.Printf("[RunQuestionMatrixWithOrgEvaluation] ✅ Stored question run %s for org %s\n",
					questionRun.QuestionRunID, orgDetails.Org.OrgID)
				summary.TotalProcessed++

				// Now process with org evaluation methodology using pre-generated name variations
				err = s.processQuestionRunWithOrgEvaluation(ctx, questionRun, orgDetails.Org.OrgID, orgDetails.Org.Name, orgDetails.Websites, nameVariations, summary)
				if err != nil {
					summary.ProcessingErrors = append(summary.ProcessingErrors,
						fmt.Sprintf("Failed to process org evaluation for question run %s: %v", questionRun.QuestionRunID, err))
					// Update batch with failed question
					if updateErr := s.UpdateBatchProgress(ctx, batchID, 0, 1); updateErr != nil {
						fmt.Printf("[RunQuestionMatrixWithOrgEvaluation] Warning: Failed to update batch progress: %v\n", updateErr)
					}
				} else {
					// Update batch with completed question
					if updateErr := s.UpdateBatchProgress(ctx, batchID, 1, 0); updateErr != nil {
						fmt.Printf("[RunQuestionMatrixWithOrgEvaluation] Warning: Failed to update batch progress: %v\n", updateErr)
					}
				}

				fmt.Printf("[RunQuestionMatrixWithOrgEvaluation] ✅ Completed processing for question %s with model %s\n",
					question.GeoQuestionID, model.Name)
			}
		}
	}

	fmt.Printf("[RunQuestionMatrixWithOrgEvaluation] 🎉 Question matrix completed: %d processed, %d evaluations, %d citations, %d competitors, $%.6f total cost\n",
		summary.TotalProcessed, summary.TotalEvaluations, summary.TotalCitations, summary.TotalCompetitors, summary.TotalCost)

	// Update is_latest flags for all created question runs
	if len(allRuns) > 0 {
		if err := s.updateLatestFlags(ctx, orgDetails.Questions, allRuns); err != nil {
			return nil, fmt.Errorf("failed to update latest flags: %w", err)
		}
		fmt.Printf("[RunQuestionMatrixWithOrgEvaluation] ✅ Updated is_latest flags for %d question runs\n", len(allRuns))
	}

	return summary, nil
}

// executeAICall performs the actual AI model call using the proper AIProvider system with web search
func (s *orgEvaluationService) executeAICall(ctx context.Context, questionText, modelName string, location *models.OrgLocation) (*AIResponse, error) {
	fmt.Printf("[executeAICall] 🚀 Making AI call for model: %s\n", modelName)

	// Convert location to workflow model format
	workflowLocation := &workflowModels.Location{
		Country: location.CountryCode,
		Region:  &location.RegionName,
	}

	// Get the appropriate AI provider (same logic as QuestionRunnerService)
	provider, err := s.getProvider(modelName)
	if err != nil {
		return nil, fmt.Errorf("failed to get provider: %w", err)
	}

	// Enable web search for question execution
	webSearch := true
	fmt.Printf("[executeAICall] 🌐 Web search enabled: %t\n", webSearch)

	// Execute the AI call with web search
	response, err := provider.RunQuestion(ctx, questionText, webSearch, workflowLocation)
	if err != nil {
		return nil, fmt.Errorf("failed to run question: %w", err)
	}

	fmt.Printf("[executeAICall] ✅ AI call completed successfully\n")
	fmt.Printf("[executeAICall]   - Input tokens: %d\n", response.InputTokens)
	fmt.Printf("[executeAICall]   - Output tokens: %d\n", response.OutputTokens)
	fmt.Printf("[executeAICall]   - Cost: $%.6f\n", response.Cost)

	return response, nil
}

// getProvider returns the appropriate AI provider for the model (same logic as QuestionRunnerService)
func (s *orgEvaluationService) getProvider(model string) (AIProvider, error) {
	modelLower := strings.ToLower(model)

	// Debug the config
	if s.cfg == nil {
		return nil, fmt.Errorf("config is nil")
	} else if s.cfg.OpenAIAPIKey == "" {
		return nil, fmt.Errorf("OpenAI API key is empty in config")
	}

	// Determine provider based on model name
	if strings.Contains(modelLower, "gpt") || strings.Contains(modelLower, "4.1") {
		fmt.Printf("[getProvider] 🎯 Selected OpenAI provider for model: %s\n", model)
		return NewOpenAIProvider(s.cfg, model, s.costService), nil
	}

	if strings.Contains(modelLower, "claude") || strings.Contains(modelLower, "sonnet") || strings.Contains(modelLower, "opus") || strings.Contains(modelLower, "haiku") {
		fmt.Printf("[getProvider] 🎯 Selected Anthropic provider for model: %s\n", model)
		return NewAnthropicProvider(s.cfg, model, s.costService), nil
	}

	return nil, fmt.Errorf("unsupported model: %s", model)
}

// updateLatestFlags manages the is_latest and is_second_latest flags
func (s *orgEvaluationService) updateLatestFlags(ctx context.Context, questions []interfaces.GeoQuestionWithTags, newRuns []*models.QuestionRun) error {
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

// CreateBatch creates a new question run batch
func (s *orgEvaluationService) CreateBatch(ctx context.Context, batch *models.QuestionRunBatch) error {
	return s.repos.QuestionRunBatchRepo.Create(ctx, batch)
}

// StartBatch updates a batch to running status
func (s *orgEvaluationService) StartBatch(ctx context.Context, batchID uuid.UUID) error {
	batch, err := s.repos.QuestionRunBatchRepo.GetByID(ctx, batchID)
	if err != nil {
		return fmt.Errorf("failed to get batch: %w", err)
	}

	now := time.Now()
	batch.Status = "running"
	batch.StartedAt = &now

	return s.repos.QuestionRunBatchRepo.Update(ctx, batch)
}

// CompleteBatch updates a batch to completed status and manages latest flags
func (s *orgEvaluationService) CompleteBatch(ctx context.Context, batchID uuid.UUID) error {
	batch, err := s.repos.QuestionRunBatchRepo.GetByID(ctx, batchID)
	if err != nil {
		return fmt.Errorf("failed to get batch: %w", err)
	}

	now := time.Now()
	batch.Status = "completed"
	batch.CompletedAt = &now

	if err := s.repos.QuestionRunBatchRepo.Update(ctx, batch); err != nil {
		return fmt.Errorf("failed to update batch: %w", err)
	}

	// Update latest flags for batches
	return s.repos.QuestionRunBatchRepo.UpdateLatestFlags(ctx, batch.Scope, batch.OrgID, batch.NetworkID, batch.BatchType, batchID)
}

// FailBatch updates a batch to failed status
func (s *orgEvaluationService) FailBatch(ctx context.Context, batchID uuid.UUID) error {
	batch, err := s.repos.QuestionRunBatchRepo.GetByID(ctx, batchID)
	if err != nil {
		return fmt.Errorf("failed to get batch: %w", err)
	}

	batch.Status = "failed"
	return s.repos.QuestionRunBatchRepo.Update(ctx, batch)
}

// UpdateBatchProgress updates the completed and failed question counts
func (s *orgEvaluationService) UpdateBatchProgress(ctx context.Context, batchID uuid.UUID, completed, failed int) error {
	batch, err := s.repos.QuestionRunBatchRepo.GetByID(ctx, batchID)
	if err != nil {
		return fmt.Errorf("failed to get batch: %w", err)
	}

	batch.CompletedQuestions += completed
	batch.FailedQuestions += failed

	return s.repos.QuestionRunBatchRepo.Update(ctx, batch)
}

// CalculateQuestionMatrix breaks down the question matrix into individual jobs
func (s *orgEvaluationService) CalculateQuestionMatrix(ctx context.Context, orgDetails *RealOrgDetails) ([]*QuestionJob, error) {
	var jobs []*QuestionJob
	jobIndex := 1

	// Calculate total jobs
	totalJobs := len(orgDetails.Questions) * len(orgDetails.Models) * len(orgDetails.Locations)

	// Create a job for each question×model×location combination
	for _, questionWithTags := range orgDetails.Questions {
		question := questionWithTags.Question
		for _, model := range orgDetails.Models {
			for _, location := range orgDetails.Locations {
				job := &QuestionJob{
					QuestionID:   question.GeoQuestionID,
					ModelID:      model.GeoModelID,
					LocationID:   location.OrgLocationID,
					QuestionText: question.QuestionText,
					ModelName:    model.Name,
					LocationCode: location.CountryCode,
					LocationName: location.RegionName,
					JobIndex:     jobIndex,
					TotalJobs:    totalJobs,
				}
				jobs = append(jobs, job)
				jobIndex++
			}
		}
	}

	fmt.Printf("[CalculateQuestionMatrix] Created %d question jobs for org\n", len(jobs))
	return jobs, nil
}

// ProcessSingleQuestionJob processes one question×model×location combination
func (s *orgEvaluationService) ProcessSingleQuestionJob(ctx context.Context, job *QuestionJob, orgID uuid.UUID, orgName string, websites []string, nameVariations []string, batchID uuid.UUID) (*QuestionJobResult, error) {
	fmt.Printf("[ProcessSingleQuestionJob] Processing job %d/%d: Question %s with model %s at location %s\n",
		job.JobIndex, job.TotalJobs, job.QuestionID, job.ModelName, job.LocationCode)

	result := &QuestionJobResult{
		JobIndex: job.JobIndex,
		Status:   "failed", // Default to failed, will update on success
	}

	// Create location struct for AI call
	location := &models.OrgLocation{
		OrgLocationID: job.LocationID,
		CountryCode:   job.LocationCode,
		RegionName:    job.LocationName,
	}

	// Execute AI call to get response
	aiResponse, err := s.executeAICall(ctx, job.QuestionText, job.ModelName, location)
	if err != nil {
		result.ErrorMessage = fmt.Sprintf("AI call failed: %v", err)
		return result, nil // Return result with failed status, don't error the step
	}

	// Create question run record
	questionRun := &models.QuestionRun{
		QuestionRunID: uuid.New(),
		GeoQuestionID: job.QuestionID,
		ModelID:       &job.ModelID,
		LocationID:    &job.LocationID,
		ResponseText:  &aiResponse.Response,
		InputTokens:   &aiResponse.InputTokens,
		OutputTokens:  &aiResponse.OutputTokens,
		TotalCost:     &aiResponse.Cost,
		BatchID:       &batchID,          // Link to batch
		RunModel:      &job.ModelName,    // Model name string
		RunCountry:    &job.LocationCode, // Country code string
		RunRegion:     &job.LocationName, // Region name string
		IsLatest:      true,              // Mark as latest for now
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	// Store question run in database
	if err := s.repos.QuestionRunRepo.Create(ctx, questionRun); err != nil {
		result.ErrorMessage = fmt.Sprintf("Failed to store question run: %v", err)
		return result, nil // Return result with failed status
	}

	result.QuestionRunID = questionRun.QuestionRunID
	result.TotalCost = aiResponse.Cost

	// Process with org evaluation methodology using pre-generated name variations
	// Create a summary to track this single job's results
	jobSummary := &OrgEvaluationSummary{
		ProcessingErrors: make([]string, 0),
	}

	err = s.processQuestionRunWithOrgEvaluation(ctx, questionRun, orgID, orgName, websites, nameVariations, jobSummary)
	if err != nil {
		result.ErrorMessage = fmt.Sprintf("Org evaluation failed: %v", err)
		return result, nil // Return result with failed status
	}

	// Update result with success data
	result.Status = "completed"
	result.HasEvaluation = jobSummary.TotalEvaluations > 0
	result.CompetitorCount = jobSummary.TotalCompetitors
	result.CitationCount = jobSummary.TotalCitations
	result.TotalCost += jobSummary.TotalCost // Add evaluation costs

	fmt.Printf("[ProcessSingleQuestionJob] ✅ Completed job %d/%d: Question run %s\n",
		job.JobIndex, job.TotalJobs, questionRun.QuestionRunID)

	return result, nil
}

// UpdateLatestFlagsForBatch updates is_latest flags for all question runs in a batch
func (s *orgEvaluationService) UpdateLatestFlagsForBatch(ctx context.Context, batchID uuid.UUID) error {
	// Get all question runs for this batch
	questionRuns, err := s.repos.QuestionRunRepo.GetByBatch(ctx, batchID)
	if err != nil {
		return fmt.Errorf("failed to get question runs for batch: %w", err)
	}

	if len(questionRuns) == 0 {
		return nil // No runs to update
	}

	// Get the questions for these runs (we need this for updateLatestFlags)
	// For now, we'll get org details to get the questions
	// This is a bit inefficient but maintains compatibility with existing updateLatestFlags method
	if len(questionRuns) > 0 {
		// Get org ID from first question run
		batch, err := s.repos.QuestionRunBatchRepo.GetByID(ctx, batchID)
		if err != nil {
			return fmt.Errorf("failed to get batch: %w", err)
		}

		if batch.OrgID == nil {
			return fmt.Errorf("batch has no org ID")
		}

		// Get org details to get questions (needed for updateLatestFlags signature)
		_, err = s.repos.OrgRepo.GetByID(ctx, *batch.OrgID)
		if err != nil {
			return fmt.Errorf("failed to get org details: %w", err)
		}

		// Get questions for this org
		questions, err := s.repos.GeoQuestionRepo.GetByOrgWithTags(ctx, *batch.OrgID)
		if err != nil {
			return fmt.Errorf("failed to get org questions: %w", err)
		}

		// Call existing updateLatestFlags method
		return s.updateLatestFlags(ctx, questions, questionRuns)
	}

	return nil
}

// GetAllOrgQuestionRuns fetches ALL question runs for an org's geo questions
func (s *orgEvaluationService) GetAllOrgQuestionRuns(ctx context.Context, orgID uuid.UUID) ([]*OrgQuestionRun, error) {
	fmt.Printf("[GetAllOrgQuestionRuns] Fetching all question runs for org: %s\n", orgID)

	// Get org's geo questions
	questions, err := s.repos.GeoQuestionRepo.GetByOrgWithTags(ctx, orgID)
	if err != nil {
		return nil, fmt.Errorf("failed to get org questions: %w", err)
	}

	// Get ALL question runs for each question
	var allQuestionRuns []*models.QuestionRun
	for _, questionWithTags := range questions {
		runs, err := s.repos.QuestionRunRepo.GetByQuestion(ctx, questionWithTags.Question.GeoQuestionID)
		if err != nil {
			fmt.Printf("[GetAllOrgQuestionRuns] Warning: failed to get runs for question %s: %v\n", questionWithTags.Question.GeoQuestionID, err)
			continue
		}
		allQuestionRuns = append(allQuestionRuns, runs...)
	}

	// Convert to workflow format
	var result []*OrgQuestionRun
	for _, run := range allQuestionRuns {
		// Get question text
		question, err := s.repos.GeoQuestionRepo.GetByID(ctx, run.GeoQuestionID)
		if err != nil {
			fmt.Printf("[GetAllOrgQuestionRuns] Warning: failed to get question for run %s: %v\n", run.QuestionRunID, err)
			continue
		}

		responseText := ""
		if run.ResponseText != nil {
			responseText = *run.ResponseText
		}

		result = append(result, &OrgQuestionRun{
			QuestionRunID: run.QuestionRunID,
			QuestionText:  question.QuestionText,
			ResponseText:  responseText,
			GeoQuestionID: run.GeoQuestionID,
		})
	}

	fmt.Printf("[GetAllOrgQuestionRuns] Found %d total question runs for org %s across %d questions\n",
		len(result), orgID, len(questions))
	return result, nil
}

// ProcessOrgQuestionRunReeval processes a single existing question run with org evaluation and cleanup
func (s *orgEvaluationService) ProcessOrgQuestionRunReeval(ctx context.Context, questionRunID uuid.UUID, orgID uuid.UUID, orgName string, websites []string, nameVariations []string, questionText, responseText string) (*OrgReevalResult, error) {
	fmt.Printf("[ProcessOrgQuestionRunReeval] Processing question run %s for org %s\n", questionRunID, orgID)

	result := &OrgReevalResult{
		QuestionRunID: questionRunID,
		Status:        "failed", // Default to failed, will update on success
	}

	// Step 1: Clean up existing data for this question run + org
	fmt.Printf("[ProcessOrgQuestionRunReeval] Cleaning up existing data for question run %s\n", questionRunID)

	if err := s.repos.OrgEvalRepo.DeleteByQuestionRunAndOrg(ctx, questionRunID, orgID); err != nil {
		result.ErrorMessage = fmt.Sprintf("Failed to cleanup org evaluations: %v", err)
		return result, nil
	}

	if err := s.repos.OrgCompetitorRepo.DeleteByQuestionRunAndOrg(ctx, questionRunID, orgID); err != nil {
		result.ErrorMessage = fmt.Sprintf("Failed to cleanup org competitors: %v", err)
		return result, nil
	}

	if err := s.repos.OrgCitationRepo.DeleteByQuestionRunAndOrg(ctx, questionRunID, orgID); err != nil {
		result.ErrorMessage = fmt.Sprintf("Failed to cleanup org citations: %v", err)
		return result, nil
	}

	// Step 2: Check for mentions using name variations
	mentioned := false
	responseTextLower := strings.ToLower(responseText)
	for _, variation := range nameVariations {
		if strings.Contains(responseTextLower, strings.ToLower(variation)) {
			mentioned = true
			break
		}
	}

	fmt.Printf("[ProcessOrgQuestionRunReeval] Mention detected: %t\n", mentioned)

	// Step 3: Conditionally run org evaluation LLM (if mentioned)
	if mentioned {
		evalResult, err := s.ExtractOrgEvaluation(ctx, questionRunID, orgID, orgName, websites, nameVariations, responseText)
		if err != nil {
			result.ErrorMessage = fmt.Sprintf("Org evaluation failed: %v", err)
			return result, nil
		}

		// CRITICAL: Store the evaluation in the database
		if err := s.repos.OrgEvalRepo.Create(ctx, evalResult.Evaluation); err != nil {
			result.ErrorMessage = fmt.Sprintf("Failed to store org evaluation: %v", err)
			return result, nil
		}

		result.HasEvaluation = true
		result.TotalCost += evalResult.TotalCost
		fmt.Printf("[ProcessOrgQuestionRunReeval] ✅ Org evaluation completed and stored with cost $%.6f\n", evalResult.TotalCost)
	} else {
		// Create minimal org eval record indicating no mention
		mentionText := ""
		inputTokens := 0
		outputTokens := 0
		totalCost := 0.0

		orgEval := &models.OrgEval{
			OrgEvalID:     uuid.New(),
			QuestionRunID: questionRunID,
			OrgID:         orgID,
			MentionText:   &mentionText,
			Sentiment:     nil, // No sentiment can be inferred if org is not mentioned
			InputTokens:   &inputTokens,
			OutputTokens:  &outputTokens,
			TotalCost:     &totalCost,
		}
		if err := s.repos.OrgEvalRepo.Create(ctx, orgEval); err != nil {
			result.ErrorMessage = fmt.Sprintf("Failed to create minimal org eval: %v", err)
			return result, nil
		}
		fmt.Printf("[ProcessOrgQuestionRunReeval] ✅ Created minimal org eval (no mention)\n")
	}

	// Step 4: Always run competitor extraction
	competitorResult, err := s.ExtractCompetitors(ctx, questionRunID, orgID, orgName, responseText)
	if err != nil {
		result.ErrorMessage = fmt.Sprintf("Competitor extraction failed: %v", err)
		return result, nil
	}

	// CRITICAL: Store competitors in database
	for _, competitor := range competitorResult.Competitors {
		if err := s.repos.OrgCompetitorRepo.Create(ctx, competitor); err != nil {
			result.ErrorMessage = fmt.Sprintf("Failed to store competitor %s: %v", competitor.Name, err)
			return result, nil
		}
	}

	result.CompetitorCount = len(competitorResult.Competitors)
	result.TotalCost += competitorResult.TotalCost
	fmt.Printf("[ProcessOrgQuestionRunReeval] ✅ Extracted and stored %d competitors with cost $%.6f\n",
		len(competitorResult.Competitors), competitorResult.TotalCost)

	// Step 5: Always run citation extraction
	citationResult, err := s.ExtractCitations(ctx, questionRunID, orgID, responseText, websites)
	if err != nil {
		result.ErrorMessage = fmt.Sprintf("Citation extraction failed: %v", err)
		return result, nil
	}

	// CRITICAL: Store citations in database
	for _, citation := range citationResult.Citations {
		if err := s.repos.OrgCitationRepo.Create(ctx, citation); err != nil {
			result.ErrorMessage = fmt.Sprintf("Failed to store citation %s: %v", citation.URL, err)
			return result, nil
		}
	}

	result.CitationCount = len(citationResult.Citations)
	result.TotalCost += citationResult.TotalCost
	fmt.Printf("[ProcessOrgQuestionRunReeval] ✅ Extracted and stored %d citations with cost $%.6f\n",
		len(citationResult.Citations), citationResult.TotalCost)

	// Success!
	result.Status = "completed"
	fmt.Printf("[ProcessOrgQuestionRunReeval] ✅ Successfully processed question run %s with total cost $%.6f\n",
		questionRunID, result.TotalCost)

	return result, nil
}

// RunOrgReEvaluation orchestrates the complete org re-evaluation process
func (s *orgEvaluationService) RunOrgReEvaluation(ctx context.Context, orgID uuid.UUID) (*OrgReevalSummary, error) {
	fmt.Printf("[RunOrgReEvaluation] Starting org re-evaluation for org: %s\n", orgID)

	summary := &OrgReevalSummary{
		ProcessingErrors: make([]string, 0),
	}

	// This method is mainly for direct service calls, not used in the granular workflow
	// The workflow handles the orchestration with individual steps
	return summary, fmt.Errorf("use the workflow for granular processing")
}

// processQuestionRunWithOrgEvaluation processes a single question run with org evaluation methodology
func (s *orgEvaluationService) processQuestionRunWithOrgEvaluation(ctx context.Context, questionRun *models.QuestionRun, orgID uuid.UUID, orgName string, orgWebsites []string, nameVariations []string, summary *OrgEvaluationSummary) error {
	if questionRun.ResponseText == nil || *questionRun.ResponseText == "" {
		return fmt.Errorf("question run has no response text")
	}

	responseText := *questionRun.ResponseText

	// Step 1: Check if organization is mentioned using pre-generated name variations
	mentioned := false
	responseTextLower := strings.ToLower(responseText)
	for _, name := range nameVariations {
		if strings.Contains(responseTextLower, strings.ToLower(name)) {
			mentioned = true
			break
		}
	}

	fmt.Printf("[processQuestionRunWithOrgEvaluation] Organization mentioned: %t (checked %d name variations)\n", mentioned, len(nameVariations))

	// Step 3: Extract org evaluation ONLY if mentioned (following Python logic)
	if mentioned {
		evalResult, err := s.ExtractOrgEvaluation(ctx, questionRun.QuestionRunID, orgID, orgName, orgWebsites, nameVariations, responseText)
		if err != nil {
			return fmt.Errorf("failed to extract evaluation: %w", err)
		}

		// Store evaluation in database
		if err := s.repos.OrgEvalRepo.Create(ctx, evalResult.Evaluation); err != nil {
			return fmt.Errorf("failed to store evaluation: %w", err)
		}

		summary.TotalEvaluations++
		summary.TotalCost += evalResult.TotalCost
		fmt.Printf("[processQuestionRunWithOrgEvaluation] ✅ Org evaluation extracted and stored\n")
	} else {
		// Create a minimal evaluation record for non-mentioned cases (following Python logic)
		now := time.Now()
		orgEval := &models.OrgEval{
			OrgEvalID:     uuid.New(),
			QuestionRunID: questionRun.QuestionRunID,
			OrgID:         orgID,
			Mentioned:     false,
			Citation:      false, // Will be determined by citation extraction
			CreatedAt:     now,
			UpdatedAt:     now,
		}

		// No sentiment can be inferred if org is not mentioned
		orgEval.Sentiment = nil

		// Store minimal evaluation
		if err := s.repos.OrgEvalRepo.Create(ctx, orgEval); err != nil {
			return fmt.Errorf("failed to store minimal evaluation: %w", err)
		}

		summary.TotalEvaluations++
		fmt.Printf("[processQuestionRunWithOrgEvaluation] ✅ Minimal evaluation stored (not mentioned)\n")
	}

	// Step 2: ALWAYS extract competitors (regardless of mention status)
	competitorResult, err := s.ExtractCompetitors(ctx, questionRun.QuestionRunID, orgID, orgName, responseText)
	if err != nil {
		return fmt.Errorf("failed to extract competitors: %w", err)
	}

	// Store competitors in database
	for _, competitor := range competitorResult.Competitors {
		if err := s.repos.OrgCompetitorRepo.Create(ctx, competitor); err != nil {
			return fmt.Errorf("failed to store competitor %s: %w", competitor.Name, err)
		}
		summary.TotalCompetitors++
	}

	summary.TotalCost += competitorResult.TotalCost
	fmt.Printf("[processQuestionRunWithOrgEvaluation] ✅ Extracted %d competitors (cost: $%.6f)\n", len(competitorResult.Competitors), competitorResult.TotalCost)

	// Step 3: ALWAYS extract citations (regardless of mention status)
	citationResult, err := s.ExtractCitations(ctx, questionRun.QuestionRunID, orgID, responseText, orgWebsites)
	if err != nil {
		return fmt.Errorf("failed to extract citations: %w", err)
	}

	// Store citations in database
	for _, citation := range citationResult.Citations {
		if err := s.repos.OrgCitationRepo.Create(ctx, citation); err != nil {
			return fmt.Errorf("failed to store citation %s: %w", citation.URL, err)
		}
		summary.TotalCitations++
	}

	summary.TotalCost += citationResult.TotalCost
	fmt.Printf("[processQuestionRunWithOrgEvaluation] ✅ Extracted %d citations (cost: $%.6f)\n", len(citationResult.Citations), citationResult.TotalCost)

	// Step 4: Update citation flag in org evaluation if we found primary citations
	if len(citationResult.Citations) > 0 {
		// Check if any citations are primary (from org's own domains)
		hasPrimaryCitation := false
		for _, citation := range citationResult.Citations {
			if citation.Type == "primary" {
				hasPrimaryCitation = true
				break
			}
		}

		// Update the org evaluation record to set citation flag
		if hasPrimaryCitation {
			// We need to update the evaluation we just created
			// For now, we'll leave this as a future enhancement since we'd need to
			// modify the evaluation record after creation
			fmt.Printf("[processQuestionRunWithOrgEvaluation] 📝 Found primary citations - citation flag could be updated\n")
		}
	}

	return nil
}

// Helper function to count citations by type
func countCitationsByType(citations []*models.OrgCitation, citationType string) int {
	count := 0
	for _, citation := range citations {
		if citation.Type == citationType {
			count++
		}
	}
	return count
}

// getBaseDomain extracts the base domain (eTLD+1) from a URL using publicsuffix
func getBaseDomain(urlStr string) (string, error) {
	// Handle URLs without protocol
	if !strings.HasPrefix(urlStr, "http://") && !strings.HasPrefix(urlStr, "https://") {
		urlStr = "https://" + urlStr
	}

	// Parse URL to get hostname
	u, err := url.Parse(urlStr)
	if err != nil {
		return "", fmt.Errorf("failed to parse URL %s: %w", urlStr, err)
	}

	hostname := u.Hostname()
	if hostname == "" {
		return "", fmt.Errorf("no hostname found in URL: %s", urlStr)
	}

	// Extract the effective TLD+1 (base domain)
	baseDomain, err := publicsuffix.EffectiveTLDPlusOne(hostname)
	if err != nil {
		return "", fmt.Errorf("failed to get base domain for %s: %w", hostname, err)
	}

	return baseDomain, nil
}

// isPrimaryDomain checks if a citation URL belongs to any of the organization's domains
func isPrimaryDomain(citationURL string, orgDomains []string) bool {
	citationBase, err := getBaseDomain(citationURL)
	if err != nil {
		// If we can't parse the citation URL, default to secondary
		return false
	}

	for _, orgDomain := range orgDomains {
		orgBase, err := getBaseDomain(orgDomain)
		if err != nil {
			// If we can't parse the org domain, skip it
			continue
		}

		// Case-insensitive comparison of base domains
		if strings.EqualFold(citationBase, orgBase) {
			return true
		}
	}
	return false
}
