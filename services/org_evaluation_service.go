// services/org_evaluation_service.go
package services

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/url"
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
	"mvdan.cc/xurls/v2"
)

type orgEvaluationService struct {
	cfg                   *config.Config
	openAIClient          *openai.Client
	costService           CostService
	repos                 *RepositoryManager
	dataExtractionService DataExtractionService
}

func NewOrgEvaluationService(cfg *config.Config, repos *RepositoryManager, dataExtractionService DataExtractionService) OrgEvaluationService {
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
		cfg:                   cfg,
		openAIClient:          &client,
		costService:           NewCostService(),
		repos:                 repos,
		dataExtractionService: dataExtractionService,
	}
}

// Structured response types for the new pipeline
type NameListResponse struct {
	Names []string `json:"names" jsonschema_description:"List of realistic brand name variations"`
}

type OrgEvaluationResponse struct {
	IsMentionVerified bool   `json:"is_mention_verified" jsonschema_description:"Boolean indicating if the TARGET organization (not just generic terms) is specifically mentioned."`
	Sentiment         string `json:"sentiment" jsonschema_description:"Sentiment: positive, negative, or neutral. Return null or empty if is_mention_verified is false."`
	MentionText       string `json:"mention_text" jsonschema_description:"All text mentioning the organization with exact formatting preserved, separated by ||. Return null or empty if is_mention_verified is false."`
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

	// Use configured model for name variations
	var model openai.ChatModel
	if s.cfg.AzureOpenAIDeploymentName != "" {
		// Use Azure with configured deployment
		model = openai.ChatModel(s.cfg.AzureOpenAIDeploymentName)
		fmt.Printf("[GenerateNameVariations] 🎯 Using Azure SDK with model: %s\n", s.cfg.AzureOpenAIDeploymentName)
	} else {
		model = openai.ChatModel("gpt-4.1-mini")
		fmt.Printf("[GenerateNameVariations] 🎯 Using Standard OpenAI model: gpt-4.1-mini\n")
	}

	schemaParam := openai.ResponseFormatJSONSchemaJSONSchemaParam{
		Name:        "name_variations_extraction",
		Description: openai.String("Generate realistic brand name variations"),
		Schema:      GenerateSchema[NameListResponse](),
		Strict:      openai.Bool(true),
	}

	fmt.Printf("[GenerateNameVariations] 🚀 Making AI call for name variations...")

	// Create the extraction request with structured output
	params := openai.ChatCompletionNewParams{
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage("You are an expert in brand name analysis and variation generation. Generate realistic brand name variations that would actually be used in business contexts."),
			openai.UserMessage(prompt),
		},
		Model: model,
		ResponseFormat: openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONSchema: &openai.ResponseFormatJSONSchemaParam{JSONSchema: schemaParam},
		},
	}

	if !strings.HasPrefix(string(model), "gpt-5") {
		params.Temperature = openai.Float(0.3) // Keep low for consistency in extraction when verified
		fmt.Printf("[GenerateNameVariations] Setting temperature to 0.3 for model %s\n", model)
	} else {
		params.ReasoningEffort = "low"
		fmt.Printf("[GenerateNameVariations] Skipping temperature setting for model gpt-5\n")
	}

	chatResponse, err := s.openAIClient.Chat.Completions.New(ctx, params)

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

	// --- MODIFIED PROMPT ---
	// Added TASK 0 for verification and stricter rules
	prompt := fmt.Sprintf(`You are an expert text analysis and extraction specialist. You are being run because a preliminary check found potential mentions of the target organization based on name variations. Your primary task is to **verify** if the **specific TARGET ORGANIZATION** is genuinely mentioned, distinguishing it from generic terms or other similarly named entities, and then extract relevant details ONLY IF verified.

**TARGET ORGANIZATION:** %s
**Potentially relevant name variations:** %s

**TASK 0: VERIFY MENTION (CRITICAL FIRST STEP)**
1. Carefully read the "RESPONSE TO ANALYZE" below.
2. Determine if the text *specifically* mentions the **TARGET ORGANIZATION** (%s) or one of its highly probable variations (like "%s Inc.", "%s.com").
3. **CRUCIAL:** Be strict. Ignore mentions of *generic terms* that might overlap with the name (e.g., if the target is "Community Credit Union", ignore generic uses of "community" or "credit union" unless they clearly refer to the specific target entity). Also ignore mentions of *different organizations* with similar names.
4. Set the 'is_mention_verified' field to 'true' ONLY if you are confident the specific TARGET ORGANIZATION is mentioned. Otherwise, set it to 'false'.

**TASK 1: EXTRACT MENTION TEXT (ONLY if is_mention_verified is true)**
* If 'is_mention_verified' is true, find EVERY occurrence where the verified target organization is mentioned.
* Extract the text with perfect formatting preservation.
* **EXTRACTION RULES:**
    - **PRESERVE EXACT FORMATTING**: Copy character-for-character (punctuation, markdown, spacing, etc.).
    - **INCLUDE CITATIONS**: Always include URLs/links appearing with mentions.
    - **ALL FORMATS**: Extract from paragraphs, lists, tables, etc.
    - **COMPLETE CONTEXT**: Extract the full sentence/paragraph/section containing the mention.
    - **AGGREGATION**: Use " || " (space-pipe-pipe-space) between separate occurrences.
* If 'is_mention_verified' is false, return null or an empty string for 'mention_text'.

**TASK 2: DETERMINE SENTIMENT (ONLY if is_mention_verified is true)**
* If 'is_mention_verified' is true, analyze the overall sentiment toward the verified target organization across all extracted mentions.
* Use exactly one of: "positive", "negative", "neutral".
* If 'is_mention_verified' is false, return null or an empty string for 'sentiment'.

**EXAMPLES of Verification:**
* Target: "Community Financial Credit Union"
    * Text: "...building a strong community..." -> is_mention_verified: false (generic term)
    * Text: "...many credit unions offer loans..." -> is_mention_verified: false (generic term)
    * Text: "...at Community Financial Credit Union, we offer..." -> is_mention_verified: true (specific match)
    * Text: "...better than First Community Credit Union..." -> is_mention_verified: false (different org)
* Target: "Senso.ai"
    * Text: "...the field of AI is growing..." -> is_mention_verified: false (generic term)
    * Text: "...visit senso.ai for details..." -> is_mention_verified: true (specific match)


**RESPONSE TO ANALYZE:**
`+"`"+`
%s
`+"`"+`

**OUTPUT REQUIREMENTS (JSON Schema):**
- is_mention_verified: boolean (true only if specific target org is mentioned)
- mention_text: string (ALL extracted text if verified, null/empty otherwise)
- sentiment: string ("positive", "negative", "neutral" if verified, null/empty otherwise)`,
		"`"+orgName+"`", "`"+nameVariationsStr+"`", "`"+orgName+"`", orgName, orgName, responseText) // Added orgName multiple times for prompt clarity
	// --- END MODIFIED PROMPT ---

	// Model Selection (using config value, tracking name)
	var model openai.ChatModel
	modelName := ""
	if s.cfg.AzureOpenAIDeploymentName != "" {
		model = openai.ChatModel(s.cfg.AzureOpenAIDeploymentName)
		modelName = s.cfg.AzureOpenAIDeploymentName
		fmt.Printf("[ExtractOrgEvaluation] 🎯 Using Azure OpenAI deployment: %s\n", modelName)
	} else {
		model = openai.ChatModelGPT4_1 // Fallback
		modelName = string(openai.ChatModelGPT4_1)
		fmt.Printf("[ExtractOrgEvaluation] ⚠️ Azure deployment not set, falling back to Standard OpenAI model: %s\n", modelName)
	}

	// Schema uses the MODIFIED OrgEvaluationResponse struct
	schemaParam := openai.ResponseFormatJSONSchemaJSONSchemaParam{
		Name:        "org_mention_verification_extraction", // Updated name slightly
		Description: openai.String("Verify specific org mention and extract data ONLY IF verified."),
		Schema:      GenerateSchema[OrgEvaluationResponse](), // Uses the updated struct
		Strict:      openai.Bool(true),
	}

	fmt.Printf("[ExtractOrgEvaluation] 🚀 Making AI call for org evaluation (verification + extraction)...")

	// Create API call parameters
	params := openai.ChatCompletionNewParams{
		Messages: []openai.ChatCompletionMessageParamUnion{
			// Updated System Message
			openai.SystemMessage("You are an expert text analysis specialist. Verify if the specific target organization is mentioned (distinguishing from generic terms). If verified, extract mention text and sentiment accurately. If not verified, return false for verification and empty/null for other fields."),
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
		fmt.Printf("[ExtractOrgEvaluation] Setting temperature to 0.1 for model %s\n", modelName)
	} else {
		params.ReasoningEffort = "low"
		fmt.Printf("[ExtractOrgEvaluation] Skipping temperature setting for model gpt-5\n")
	}

	chatResponse, err := s.openAIClient.Chat.Completions.New(ctx, params)

	if err != nil {
		// Log the raw error for debugging
		fmt.Printf("[ExtractOrgEvaluation] ❌ AI call failed: %v\n", err)
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
	fmt.Printf("[ExtractOrgEvaluation] Raw AI Response: %s\n", responseContent) // Log raw response

	// Parse the structured response (using MODIFIED struct)
	var extractedData OrgEvaluationResponse
	if err := json.Unmarshal([]byte(responseContent), &extractedData); err != nil {
		fmt.Printf("[ExtractOrgEvaluation] ❌ Failed to parse JSON response: %v\n", err)
		fmt.Printf("[ExtractOrgEvaluation]   Raw content was: %s\n", responseContent)
		return nil, fmt.Errorf("failed to parse org evaluation response: %w. Raw content: %s", err, responseContent)
	}

	// Capture token and cost data
	inputTokens := int(chatResponse.Usage.PromptTokens)
	outputTokens := int(chatResponse.Usage.CompletionTokens)
	totalCost := s.costService.CalculateCost("openai", modelName, inputTokens, outputTokens, false)

	// Create the org evaluation model
	now := time.Now()
	orgEval := &models.OrgEval{
		OrgEvalID:     uuid.New(),
		QuestionRunID: questionRunID,
		OrgID:         orgID,
		Mentioned:     false, // Default to false, verify below
		Citation:      false,
		InputTokens:   &inputTokens,
		OutputTokens:  &outputTokens,
		TotalCost:     &totalCost,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	// --- SECONDARY VERIFICATION LOGIC ---
	// Check both the explicit verification flag AND if mention text is non-empty
	// The system will now only consider an organization "mentioned" if the AI both explicitly confirms the verification and provides extracted text.
	if extractedData.IsMentionVerified && extractedData.MentionText != "" {
		orgEval.Mentioned = true // Set Mentioned to true ONLY if explicitly verified AND text exists
		fmt.Printf("[ExtractOrgEvaluation] ✅ Mention VERIFIED by LLM. Mentioned=true\n")

		// Set sentiment and text only if verified
		if extractedData.Sentiment != "" {
			orgEval.Sentiment = &extractedData.Sentiment
		}
		orgEval.MentionText = &extractedData.MentionText // Already checked non-empty

		// Set mention rank to 1 since it's verified and extracted
		mentionRank := 1
		orgEval.MentionRank = &mentionRank

		fmt.Printf("[ExtractOrgEvaluation]   Sentiment='%s', MentionTextLength=%d\n",
			safeDerefString(orgEval.Sentiment), len(*orgEval.MentionText))

	} else {
		// If verification is false OR mention text is empty, ensure Mentioned is false
		orgEval.Mentioned = false
		orgEval.MentionText = nil // Ensure text is null/empty
		orgEval.Sentiment = nil   // Ensure sentiment is null/empty
		orgEval.MentionRank = nil // Ensure rank is null/empty

		if !extractedData.IsMentionVerified {
			fmt.Printf("[ExtractOrgEvaluation] ⚠️ Mention NOT VERIFIED by LLM (is_mention_verified=false). Mentioned=false\n")
		} else { // MentionText must be empty if we reached here
			fmt.Printf("[ExtractOrgEvaluation] ⚠️ Mention VERIFIED by LLM, but MentionText is EMPTY. Treating as Mentioned=false\n")
		}
	}
	// --- END SECONDARY VERIFICATION LOGIC ---

	return &OrgEvaluationResult{
		Evaluation:   orgEval,
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		TotalCost:    totalCost,
	}, nil
}

// Helper function to safely dereference string pointers for logging
func safeDerefString(s *string) string {
	if s == nil {
		return "<nil>"
	}
	if *s == "" {
		return "<empty>"
	}
	return *s
}

// ExtractCompetitors implements the get_competitors() function from Python
func (s *orgEvaluationService) ExtractCompetitors(ctx context.Context, questionRunID, orgID uuid.UUID, orgName string, responseText string) (*CompetitorExtractionResult, error) {
	fmt.Printf("[ExtractCompetitors] 🔍 Processing competitors for question run %s, org %s\n", questionRunID, orgName)

	prompt := fmt.Sprintf("You are an expert in competitive analysis and brand identification. Your task is to identify ALL competitor brands, companies, products, or services mentioned in the response text that are NOT the target organization.\n\n**TARGET ORGANIZATION:** %s\n\n**COMPETITOR IDENTIFICATION RULES:**\n\n1. **What to Include:**\n   - Company names (e.g., \"Microsoft\", \"Google\", \"Apple\")\n   - Product names (e.g., \"ChatGPT\", \"Claude\", \"Gemini\", \"Perplexity\")\n   - Service names (e.g., \"Ahrefs Brand Radar\", \"Surfer SEO AI Tracker\")\n   - Platform names (e.g., \"LinkedIn\", \"Facebook\", \"Twitter\")\n   - Tool names (e.g., \"Profound\", \"Promptmonitor\", \"Writesonic GEO Platform\")\n   - Any branded entity that could be considered competition or alternative\n\n2. **What to Exclude:**\n   - The target organization itself and its variations\n   - Generic terms (e.g., \"AI tools\", \"analytics platforms\", \"search engines\")\n   - Non-competitive entities (e.g., \"users\", \"customers\", \"developers\")\n   - Technical terms or concepts (e.g., \"machine learning\", \"natural language processing\")\n   - Industry terms (e.g., \"credit unions\", \"financial services\")\n\n3. **Extraction Guidelines:**\n   - Extract the most commonly used or official name for each competitor\n   - If a company has multiple products mentioned, list each product separately\n   - Remove duplicates and variations of the same entity\n   - Focus on entities that could be considered alternatives or competitors\n   - Include both direct competitors and indirect competitors mentioned\n\n**EXAMPLES:**\n\nExample 1: \"Leading AI tools include ChatGPT, Claude, Gemini, and Senso.ai for content optimization.\"\n→ Extract: [\"ChatGPT\", \"Claude\", \"Gemini\"] (exclude Senso.ai as it's the target)\n\nExample 2: \"Microsoft's Azure competes with Google Cloud and Amazon Web Services in the enterprise market.\"\n→ Extract: [\"Microsoft\", \"Azure\", \"Google Cloud\", \"Amazon Web Services\"]\n\nExample 3: \"Popular analytics platforms like Google Analytics, Adobe Analytics, and Mixpanel offer similar features.\"\n→ Extract: [\"Google Analytics\", \"Adobe Analytics\", \"Mixpanel\"]\n\n**RESPONSE TO ANALYZE:**\n```\n%s\n```\n\n**INSTRUCTIONS:**\n- Return only the list of competitor names\n- Use the most recognizable/official name for each competitor\n- Remove any duplicates or very similar variations\n- If no competitors are mentioned, return an empty list\n- Do not include the target organization or generic terms", "`"+orgName+"`", responseText)

	// Use gpt-4.1-mini for competitors
	var model openai.ChatModel
	if s.cfg.AzureOpenAIDeploymentName != "" {
		// Use Azure with mini model
		model = openai.ChatModel("gpt-4.1-mini")
		fmt.Printf("[ExtractCompetitors] 🎯 Using Azure SDK with model: gpt-4.1-mini\n")
	} else {
		model = openai.ChatModel("gpt-4.1-mini")
		fmt.Printf("[ExtractCompetitors] 🎯 Using Standard OpenAI model: gpt-4.1-mini\n")
	}

	schemaParam := openai.ResponseFormatJSONSchemaJSONSchemaParam{
		Name:        "competitor_extraction",
		Description: openai.String("Extract competitor names from AI response"),
		Schema:      GenerateSchema[CompetitorListResponse](),
		Strict:      openai.Bool(true),
	}

	fmt.Printf("[ExtractCompetitors] 🚀 Making AI call for competitor extraction...")

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
		fmt.Printf("[ExtractCompetitors] Setting temperature to 0.1 for model %s\n", model)
	} else {
		params.ReasoningEffort = "low"
		fmt.Printf("[ExtractCompetitors] Skipping temperature setting for model gpt-5\n")
	}

	chatResponse, err := s.openAIClient.Chat.Completions.New(ctx, params)

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

// ExtractCitations implements the extract_citations() function from Python with new logic
func (s *orgEvaluationService) ExtractCitations(ctx context.Context, questionRunID, orgID uuid.UUID, responseText string, orgWebsites []string) (*CitationExtractionResult, error) {
	fmt.Printf("[ExtractCitations] 🔍 Processing citations for question run %s, org %s\n", questionRunID, orgID)

	var citations []*models.OrgCitation
	seenURLs := make(map[string]bool)
	now := time.Now()

	// Image extensions to skip
	imageExtensions := []string{
		".png", ".jpg", ".jpeg", ".gif", ".bmp", ".svg", ".webp",
	}

	// Use Strict() to only find URLs with a scheme (http://, https://)
	matches := xurls.Strict().FindAllString(responseText, -1)

	for _, match := range matches {
		// 1. Start with the raw match
		urlStr := strings.TrimSpace(match)

		// 3. Parse the URL
		u, err := url.Parse(urlStr)
		if err != nil {
			fmt.Printf("[ExtractCitations] ⚠️ Skipping unparseable URL: %s\n", urlStr)
			continue
		}

		// 4. Check for valid scheme (ENG-167: "MATCH ON HTTP")
		if u.Scheme != "http" && u.Scheme != "https" {
			continue // Skip mailto:, ftp:, etc.
		}

		// 5. Clean the URL
		// - Remove "www."
		u.Host = strings.TrimPrefix(u.Hostname(), "www.")
		// - Remove all UTM parameters (case-insensitive)
		q := u.Query()
		for param := range q {
			if strings.HasPrefix(strings.ToLower(param), "utm_") {
				q.Del(param)
			}
		}
		u.RawQuery = q.Encode()
		// - Reconstruct and remove trailing slash
		finalURL := u.String()
		finalURL = strings.TrimRight(finalURL, "/")

		// 6. Check for duplicates (using the cleaned URL)
		if finalURL == "" || seenURLs[finalURL] {
			continue
		}

		// 7. Check for image links
		pathLower := strings.ToLower(u.Path)
		isImage := false
		for _, ext := range imageExtensions {
			if strings.HasSuffix(pathLower, ext) {
				isImage = true
				break
			}
		}
		if isImage {
			fmt.Printf("[ExtractCitations] ⚠️ Skipping image URL: %s\n", finalURL)
			continue // We skip image links entirely
		}

		// --- CHANGE 1: Create the citation object *before* the dead link check ---
		// We need to create it now so we can set its DeadLink flag.

		// Determine if this is a primary or secondary citation
		citationType := "secondary" // Default to secondary
		if isPrimaryDomain(finalURL, orgWebsites) {
			citationType = "primary"
		}

		citation := &models.OrgCitation{
			OrgCitationID: uuid.New(),
			QuestionRunID: questionRunID,
			OrgID:         orgID,
			URL:           finalURL,
			Type:          citationType,
			DeadLink:      false, // Default to false
			CreatedAt:     now,
			UpdatedAt:     now,
		}

		// NOTE: Dead-link GET checks are temporarily disabled to avoid slowdowns.
		// citation.DeadLink remains false unless set elsewhere.

		// --- CHANGE 3: Always add the citation (dead or not) ---
		// The `continue` statements for dead links are gone.
		// We now add the citation to the list regardless of its status.
		citations = append(citations, citation)
		seenURLs[finalURL] = true

		// Add small randomized delay to avoid rate limiting (10-50ms)
		time.Sleep(time.Duration(10+rand.Intn(40)) * time.Millisecond)
	}

	fmt.Printf("[ExtractCitations] ✅ Extracted %d citations (incl. dead) (%d primary, %d secondary)",
		len(citations),
		countCitationsByType(citations, "primary"),
		countCitationsByType(citations, "secondary"))

	// Citations extraction itself doesn't use AI, so cost is 0
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

	// PHASE 1: Generate name variations ONCE for the entire org
	fmt.Printf("[RunQuestionMatrixWithOrgEvaluation] 🔍 Generating name variations for org: %s\n", orgDetails.Org.Name)
	nameVariations, err := s.GenerateNameVariations(ctx, orgDetails.Org.Name, orgDetails.Websites)
	if err != nil {
		return nil, fmt.Errorf("failed to generate name variations: %w", err)
	}
	fmt.Printf("[RunQuestionMatrixWithOrgEvaluation] ✅ Generated %d name variations for org\n", len(nameVariations))

	// PHASE 2: Execute all questions grouped by Model-Location pairs
	fmt.Printf("[RunQuestionMatrixWithOrgEvaluation] 🚀 PHASE 2: Executing questions (batched by model-location)\n")
	allQuestionRuns, err := s.executeAllQuestions(ctx, orgDetails, batchID, summary)
	if err != nil {
		return nil, fmt.Errorf("failed to execute questions: %w", err)
	}
	fmt.Printf("[RunQuestionMatrixWithOrgEvaluation] ✅ Completed question execution: %d question runs created\n", len(allQuestionRuns))

	// PHASE 3: Process extractions for all completed question runs
	fmt.Printf("[RunQuestionMatrixWithOrgEvaluation] 🔍 PHASE 3: Processing extractions for %d question runs\n", len(allQuestionRuns))
	err = s.processAllExtractions(ctx, allQuestionRuns, orgDetails.Org.OrgID, orgDetails.Org.Name, orgDetails.Websites, nameVariations, batchID, summary)
	if err != nil {
		return nil, fmt.Errorf("failed to process extractions: %w", err)
	}

	fmt.Printf("[RunQuestionMatrixWithOrgEvaluation] 🎉 Question matrix completed: %d processed, %d evaluations, %d citations, %d competitors, $%.6f total cost\n",
		summary.TotalProcessed, summary.TotalEvaluations, summary.TotalCitations, summary.TotalCompetitors, summary.TotalCost)

	// Update is_latest flags for all created question runs
	if len(allQuestionRuns) > 0 {
		if err := s.updateLatestFlags(ctx, orgDetails.Questions, allQuestionRuns); err != nil {
			return nil, fmt.Errorf("failed to update latest flags: %w", err)
		}
		fmt.Printf("[RunQuestionMatrixWithOrgEvaluation] ✅ Updated is_latest flags for %d question runs\n", len(allQuestionRuns))
	}

	return summary, nil
}

// ModelLocationPair represents a unique combination of model and location
type ModelLocationPair struct {
	Model    *models.GeoModel
	Location *models.OrgLocation
}

// executeAllQuestions executes all questions grouped by model-location pairs (PHASE 2)
func (s *orgEvaluationService) executeAllQuestions(ctx context.Context, orgDetails *RealOrgDetails, batchID uuid.UUID, summary *OrgEvaluationSummary) ([]*models.QuestionRun, error) {
	var allQuestionRuns []*models.QuestionRun

	// Create model-location pairs
	pairs := s.createModelLocationPairs(orgDetails.Models, orgDetails.Locations)
	fmt.Printf("[executeAllQuestions] Created %d model-location pairs\n", len(pairs))

	// Process each model-location pair
	for pairIdx, pair := range pairs {
		fmt.Printf("[executeAllQuestions] 📦 Processing pair %d/%d: model=%s, location=%s\n",
			pairIdx+1, len(pairs), pair.Model.Name, pair.Location.CountryCode)

		// Get provider for this model
		provider, err := s.getProvider(pair.Model.Name)
		if err != nil {
			summary.ProcessingErrors = append(summary.ProcessingErrors,
				fmt.Sprintf("Failed to get provider for model %s: %v", pair.Model.Name, err))
			continue
		}

		// Execute questions for this pair (batched or sequential)
		questionRuns, err := s.executeQuestionsForPair(ctx, orgDetails.Questions, pair, provider, batchID, summary)
		if err != nil {
			summary.ProcessingErrors = append(summary.ProcessingErrors,
				fmt.Sprintf("Failed to execute questions for model %s, location %s: %v", pair.Model.Name, pair.Location.CountryCode, err))
			continue
		}

		allQuestionRuns = append(allQuestionRuns, questionRuns...)
		fmt.Printf("[executeAllQuestions] ✅ Completed pair %d/%d: created %d question runs\n",
			pairIdx+1, len(pairs), len(questionRuns))
	}

	return allQuestionRuns, nil
}

// createModelLocationPairs creates all unique combinations of models and locations
func (s *orgEvaluationService) createModelLocationPairs(models []*models.GeoModel, locations []*models.OrgLocation) []ModelLocationPair {
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
func (s *orgEvaluationService) executeQuestionsForPair(
	ctx context.Context,
	questions []interfaces.GeoQuestionWithTags,
	pair ModelLocationPair,
	provider AIProvider,
	batchID uuid.UUID,
	summary *OrgEvaluationSummary,
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
			runs, err := s.executeBatch(ctx, batch, pair, provider, workflowLocation, batchID, summary)
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
			run, err := s.executeSingleQuestion(ctx, question, pair, provider, workflowLocation, batchID, summary)
			if err != nil {
				summary.ProcessingErrors = append(summary.ProcessingErrors,
					fmt.Sprintf("Failed to execute question %s: %v", question.GeoQuestionID, err))
				continue
			}

			questionRuns = append(questionRuns, run)
		}
	}

	return questionRuns, nil
}

// executeBatch executes a batch of questions using the provider's batch API
func (s *orgEvaluationService) executeBatch(
	ctx context.Context,
	batch []interfaces.GeoQuestionWithTags,
	pair ModelLocationPair,
	provider AIProvider,
	workflowLocation *workflowModels.Location,
	batchID uuid.UUID,
	summary *OrgEvaluationSummary,
) ([]*models.QuestionRun, error) {
	// Check which questions need to be executed (filter out existing ones)
	questionsToExecute := make([]interfaces.GeoQuestionWithTags, 0)
	existingRuns := make([]*models.QuestionRun, 0)

	for _, questionWithTags := range batch {
		question := questionWithTags.Question

		// Check if question run already exists
		existingRun, err := s.CheckQuestionRunExists(ctx, question.GeoQuestionID, pair.Model.GeoModelID, pair.Location.OrgLocationID, batchID)
		if err != nil {
			fmt.Printf("[executeBatch] Warning: Failed to check for existing run: %v\n", err)
			questionsToExecute = append(questionsToExecute, questionWithTags)
			continue
		}

		if existingRun != nil {
			fmt.Printf("[executeBatch] ✓ Skipping question %s - already executed\n", question.GeoQuestionID)
			existingRuns = append(existingRuns, existingRun)
		} else {
			questionsToExecute = append(questionsToExecute, questionWithTags)
		}
	}

	// If all questions already exist, return existing runs
	if len(questionsToExecute) == 0 {
		fmt.Printf("[executeBatch] All %d questions already executed, skipping batch API call\n", len(batch))
		return existingRuns, nil
	}

	fmt.Printf("[executeBatch] Executing %d new questions (skipped %d existing)\n", len(questionsToExecute), len(existingRuns))

	// Extract query strings from questions that need execution
	queries := make([]string, len(questionsToExecute))
	for i, q := range questionsToExecute {
		queries[i] = q.Question.QuestionText
	}

	fmt.Printf("[executeBatch] 🚀 Calling provider.RunQuestionBatch with %d queries\n", len(queries))

	// Execute batch API call
	responses, err := provider.RunQuestionBatch(ctx, queries, true, workflowLocation)
	if err != nil {
		fmt.Printf("[executeBatch] ❌ Batch API call failed: %v\n", err)
		return nil, fmt.Errorf("batch API call failed: %w", err)
	}

	fmt.Printf("[executeBatch] ✅ Batch API call succeeded, got %d responses\n", len(responses))

	if len(responses) != len(questionsToExecute) {
		errMsg := fmt.Sprintf("batch returned %d responses but expected %d", len(responses), len(questionsToExecute))
		fmt.Printf("[executeBatch] ❌ %s\n", errMsg)
		return nil, fmt.Errorf("%s", errMsg)
	}

	// Create and store new question runs
	newQuestionRuns := make([]*models.QuestionRun, len(questionsToExecute))
	for i, questionWithTags := range questionsToExecute {
		question := questionWithTags.Question
		aiResponse := responses[i]

		questionRun := &models.QuestionRun{
			QuestionRunID: uuid.New(),
			GeoQuestionID: question.GeoQuestionID,
			ModelID:       &pair.Model.GeoModelID,
			LocationID:    &pair.Location.OrgLocationID,
			ResponseText:  &aiResponse.Response,
			InputTokens:   &aiResponse.InputTokens,
			OutputTokens:  &aiResponse.OutputTokens,
			TotalCost:     &aiResponse.Cost,
			BatchID:       &batchID,
			RunModel:      &pair.Model.Name,
			RunCountry:    &pair.Location.CountryCode,
			RunRegion:     pair.Location.RegionName,
			IsLatest:      true,
			CreatedAt:     time.Now(),
			UpdatedAt:     time.Now(),
		}

		// Store in database
		if err := s.repos.QuestionRunRepo.Create(ctx, questionRun); err != nil {
			return nil, fmt.Errorf("failed to store question run: %w", err)
		}

		newQuestionRuns[i] = questionRun
		summary.TotalProcessed++
	}

	// Combine existing and new question runs
	allQuestionRuns := append(existingRuns, newQuestionRuns...)
	fmt.Printf("[executeBatch] ✅ Successfully created %d new question runs, returning %d total runs\n", len(newQuestionRuns), len(allQuestionRuns))
	return allQuestionRuns, nil
}

// executeSingleQuestion executes a single question (for non-batching providers)
func (s *orgEvaluationService) executeSingleQuestion(
	ctx context.Context,
	question *models.GeoQuestion,
	pair ModelLocationPair,
	provider AIProvider,
	workflowLocation *workflowModels.Location,
	batchID uuid.UUID,
	summary *OrgEvaluationSummary,
) (*models.QuestionRun, error) {
	// Check if question run already exists
	existingRun, err := s.CheckQuestionRunExists(ctx, question.GeoQuestionID, pair.Model.GeoModelID, pair.Location.OrgLocationID, batchID)
	if err != nil {
		fmt.Printf("[executeSingleQuestion] Warning: Failed to check for existing run: %v\n", err)
		// Continue with execution if check fails
	}

	if existingRun != nil {
		fmt.Printf("[executeSingleQuestion] ✓ Skipping question %s - already executed\n", question.GeoQuestionID)
		return existingRun, nil
	}

	// Execute AI call
	aiResponse, err := provider.RunQuestion(ctx, question.QuestionText, true, workflowLocation)
	if err != nil {
		return nil, fmt.Errorf("AI call failed: %w", err)
	}

	// Create question run record
	questionRun := &models.QuestionRun{
		QuestionRunID: uuid.New(),
		GeoQuestionID: question.GeoQuestionID,
		ModelID:       &pair.Model.GeoModelID,
		LocationID:    &pair.Location.OrgLocationID,
		ResponseText:  &aiResponse.Response,
		InputTokens:   &aiResponse.InputTokens,
		OutputTokens:  &aiResponse.OutputTokens,
		TotalCost:     &aiResponse.Cost,
		BatchID:       &batchID,
		RunModel:      &pair.Model.Name,
		RunCountry:    &pair.Location.CountryCode,
		RunRegion:     pair.Location.RegionName,
		IsLatest:      true,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	// Store in database
	if err := s.repos.QuestionRunRepo.Create(ctx, questionRun); err != nil {
		return nil, fmt.Errorf("failed to store question run: %w", err)
	}

	summary.TotalProcessed++
	return questionRun, nil
}

// processAllExtractions processes extractions for all question runs (PHASE 3)
func (s *orgEvaluationService) processAllExtractions(
	ctx context.Context,
	questionRuns []*models.QuestionRun,
	orgID uuid.UUID,
	orgName string,
	websites []string,
	nameVariations []string,
	batchID uuid.UUID,
	summary *OrgEvaluationSummary,
) error {
	fmt.Printf("[processAllExtractions] Processing extractions for %d question runs\n", len(questionRuns))

	for idx, questionRun := range questionRuns {
		fmt.Printf("[processAllExtractions] 🔍 Processing extraction %d/%d for question run %s\n",
			idx+1, len(questionRuns), questionRun.QuestionRunID)

		// Check if extractions already exist
		// If org_eval exists, skip extraction entirely (even if citations/competitors are missing)
		hasEval, hasCitations, hasCompetitors, err := s.CheckExtractionsExist(ctx, questionRun.QuestionRunID, orgID)
		if err != nil {
			fmt.Printf("[processAllExtractions] Warning: Failed to check for existing extractions: %v\n", err)
			// Continue with extraction if check fails
		} else if hasEval {
			fmt.Printf("[processAllExtractions] ✓ Skipping extraction for question run %s - org_eval already exists (citations:%t competitors:%t)\n",
				questionRun.QuestionRunID, hasCitations, hasCompetitors)
			// Update batch as completed (since extraction was already done)
			if updateErr := s.UpdateBatchProgress(ctx, batchID, 1, 0); updateErr != nil {
				fmt.Printf("[processAllExtractions] Warning: Failed to update batch progress: %v\n", updateErr)
			}
			continue
		}

		err = s.processQuestionRunWithOrgEvaluation(ctx, questionRun, orgID, orgName, websites, nameVariations, summary)
		if err != nil {
			summary.ProcessingErrors = append(summary.ProcessingErrors,
				fmt.Sprintf("Failed to process org evaluation for question run %s: %v", questionRun.QuestionRunID, err))
			// Update batch with failed question
			if updateErr := s.UpdateBatchProgress(ctx, batchID, 0, 1); updateErr != nil {
				fmt.Printf("[processAllExtractions] Warning: Failed to update batch progress: %v\n", updateErr)
			}
		} else {
			// Update batch with completed question
			if updateErr := s.UpdateBatchProgress(ctx, batchID, 1, 0); updateErr != nil {
				fmt.Printf("[processAllExtractions] Warning: Failed to update batch progress: %v\n", updateErr)
			}
		}
	}

	return nil
}

// executeAICall performs the actual AI model call using the proper AIProvider system with web search
func (s *orgEvaluationService) executeAICall(ctx context.Context, questionText, modelName string, location *models.OrgLocation) (*AIResponse, error) {
	fmt.Printf("[executeAICall] 🚀 Making AI call for model: %s\n", modelName)

	// Convert location to workflow model format
	workflowLocation := &workflowModels.Location{
		Country: location.CountryCode,
		Region:  location.RegionName,
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
	}

	// BrightData ChatGPT provider
	if strings.Contains(modelLower, "chatgpt") {
		fmt.Printf("[getProvider] 🎯 Selected BrightData ChatGPT provider for model: %s\n", model)
		return NewBrightDataProvider(s.cfg, model, s.costService), nil
	}

	// Perplexity provider (via BrightData)
	if strings.Contains(modelLower, "perplexity") {
		fmt.Printf("[getProvider] 🎯 Selected Perplexity provider for model: %s\n", model)
		return NewPerplexityProvider(s.cfg, model, s.costService), nil
	}

	// Gemini provider (via BrightData)
	if strings.Contains(modelLower, "gemini") {
		fmt.Printf("[getProvider] 🎯 Selected Gemini provider for model: %s\n", model)
		return NewGeminiProvider(s.cfg, model, s.costService), nil
	}

	// Linkup provider
	if strings.Contains(modelLower, "linkup") {
		if s.cfg.LinkupAPIKey == "" {
			return nil, fmt.Errorf("Linkup API key is empty in config")
		}
		fmt.Printf("[getProvider] 🎯 Selected Linkup provider for model: %s\n", model)
		return NewLinkupProvider(s.cfg, model, s.costService), nil
	}

	// OpenAI provider (gpt-4.1, etc.)
	if strings.Contains(modelLower, "gpt") || strings.Contains(modelLower, "4.1") {
		if s.cfg.OpenAIAPIKey == "" {
			return nil, fmt.Errorf("OpenAI API key is empty in config")
		}
		fmt.Printf("[getProvider] 🎯 Selected OpenAI provider for model: %s\n", model)
		return NewOpenAIProvider(s.cfg, model, s.costService), nil
	}

	// Anthropic provider
	if strings.Contains(modelLower, "claude") || strings.Contains(modelLower, "sonnet") || strings.Contains(modelLower, "opus") || strings.Contains(modelLower, "haiku") {
		fmt.Printf("[getProvider] 🎯 Selected Anthropic provider for model: %s\n", model)
		return NewAnthropicProvider(s.cfg, model, s.costService), nil
	}

	return nil, fmt.Errorf("unsupported model: %s", model)
}

// updateLatestFlags manages the is_latest flags for batch processing
// For org evaluation batches, we need to mark ALL runs in the new batch as is_latest=true
// because each represents a unique (question, model, location) combination
func (s *orgEvaluationService) updateLatestFlags(ctx context.Context, questions []interfaces.GeoQuestionWithTags, newRuns []*models.QuestionRun) error {
	if len(newRuns) == 0 {
		return nil
	}

	// Get the batch ID from the first run (all runs should have the same batch ID)
	batchID := newRuns[0].BatchID
	if batchID == nil {
		return fmt.Errorf("question runs missing batch ID")
	}

	fmt.Printf("[updateLatestFlags] Updating is_latest flags for batch %s with %d question runs\n", batchID, len(newRuns))

	// Step 1: Mark all old question runs (from previous batches) as is_latest=false for this org
	// We need to get all question IDs that were processed in this batch
	questionIDMap := make(map[uuid.UUID]bool)
	for _, run := range newRuns {
		questionIDMap[run.GeoQuestionID] = true
	}

	questionIDs := make([]uuid.UUID, 0, len(questionIDMap))
	for qID := range questionIDMap {
		questionIDs = append(questionIDs, qID)
	}

	// Step 1: Mark old question runs as is_latest=false
	// For each question in this batch, get all old runs and mark them as not latest
	for questionID := range questionIDMap {
		// Get all runs for this question (to find old ones)
		allRuns, err := s.repos.QuestionRunRepo.GetByQuestion(ctx, questionID)
		if err != nil {
			fmt.Printf("[updateLatestFlags] Warning: Failed to get runs for question %s: %v\n", questionID, err)
			continue
		}

		// Mark all old runs (not in current batch) as is_latest=false
		for _, oldRun := range allRuns {
			if oldRun.BatchID == nil || *oldRun.BatchID != *batchID {
				oldRun.IsLatest = false
				oldRun.UpdatedAt = time.Now()
				if err := s.repos.QuestionRunRepo.Update(ctx, oldRun); err != nil {
					fmt.Printf("[updateLatestFlags] Warning: Failed to mark old run %s as not latest: %v\n", oldRun.QuestionRunID, err)
				}
			}
		}
	}

	// Step 2: Mark all question runs in the NEW batch as is_latest=true
	for _, run := range newRuns {
		run.IsLatest = true
		run.UpdatedAt = time.Now()
		if err := s.repos.QuestionRunRepo.Update(ctx, run); err != nil {
			fmt.Printf("[updateLatestFlags] Warning: Failed to mark run %s as latest: %v\n", run.QuestionRunID, err)
		}
	}

	fmt.Printf("[updateLatestFlags] ✅ Successfully updated is_latest flags for %d question runs in batch %s\n", len(newRuns), batchID)
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

	// Step 1: Mark all old batches for this org as is_latest=false
	if batch.OrgID != nil {
		// Get all batches for this org
		allBatches, err := s.repos.QuestionRunBatchRepo.GetByOrg(ctx, *batch.OrgID)
		if err != nil {
			fmt.Printf("[CompleteBatch] Warning: Failed to get org batches: %v\n", err)
		} else {
			// Mark all old batches (except current) as is_latest=false
			for _, oldBatch := range allBatches {
				if oldBatch.BatchID != batchID && oldBatch.IsLatest {
					oldBatch.IsLatest = false
					oldBatch.UpdatedAt = time.Now()
					if err := s.repos.QuestionRunBatchRepo.Update(ctx, oldBatch); err != nil {
						fmt.Printf("[CompleteBatch] Warning: Failed to mark old batch %s as not latest: %v\n", oldBatch.BatchID, err)
					}
				}
			}
			fmt.Printf("[CompleteBatch] ✅ Marked %d old batches as is_latest=false\n", len(allBatches)-1)
		}
	}

	// Step 2: Mark current batch as is_latest=true
	batch.IsLatest = true

	return s.repos.QuestionRunBatchRepo.Update(ctx, batch)
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
				// Dereference RegionName pointer for string field
				locationName := ""
				if location.RegionName != nil {
					locationName = *location.RegionName
				}

				job := &QuestionJob{
					QuestionID:   question.GeoQuestionID,
					ModelID:      model.GeoModelID,
					LocationID:   location.OrgLocationID,
					QuestionText: question.QuestionText,
					ModelName:    model.Name,
					LocationCode: location.CountryCode,
					LocationName: locationName,
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
	// Convert string LocationName back to pointer for RegionName field
	var regionNamePtr *string
	if job.LocationName != "" {
		regionNamePtr = &job.LocationName
	}

	location := &models.OrgLocation{
		OrgLocationID: job.LocationID,
		CountryCode:   job.LocationCode,
		RegionName:    regionNamePtr,
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

// ProcessNetworkOrgQuestionRunReeval processes a single network question run with org evaluation methodology but saves to network_org_* tables
func (s *orgEvaluationService) ProcessNetworkOrgQuestionRunReeval(ctx context.Context, questionRunID uuid.UUID, orgID uuid.UUID, orgName string, websites []string, nameVariations []string, questionText, responseText string) (*OrgReevalResult, error) {
	fmt.Printf("[ProcessNetworkOrgQuestionRunReeval] Processing network question run %s for org %s using org evaluation methodology\n", questionRunID, orgID)

	result := &OrgReevalResult{
		QuestionRunID: questionRunID,
		Status:        "failed", // Default to failed, will update on success
	}

	// Step 1: Clean up existing network_org_* data for this question run + org
	fmt.Printf("[ProcessNetworkOrgQuestionRunReeval] Cleaning up existing network org data for question run %s and org %s\n", questionRunID, orgID)

	if err := s.repos.NetworkOrgEvalRepo.DeleteByQuestionRunAndOrg(ctx, questionRunID, orgID); err != nil {
		result.ErrorMessage = fmt.Sprintf("Failed to cleanup network org evaluations: %v", err)
		return result, nil
	}

	if err := s.repos.NetworkOrgCompetitorRepo.DeleteByQuestionRunAndOrg(ctx, questionRunID, orgID); err != nil {
		result.ErrorMessage = fmt.Sprintf("Failed to cleanup network org competitors: %v", err)
		return result, nil
	}

	if err := s.repos.NetworkOrgCitationRepo.DeleteByQuestionRunAndOrg(ctx, questionRunID, orgID); err != nil {
		result.ErrorMessage = fmt.Sprintf("Failed to cleanup network org citations: %v", err)
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

	fmt.Printf("[ProcessNetworkOrgQuestionRunReeval] Mention detected: %t\n", mentioned)

	// Step 3: Conditionally run org evaluation LLM (if mentioned) but extract to network org format
	if mentioned {
		// Use the data extraction service to extract network org data with org evaluation methodology
		// Pass pre-generated nameVariations to avoid redundant API call
		extractionResult, err := s.dataExtractionService.ExtractNetworkOrgData(ctx, questionRunID, orgID, orgName, websites, questionText, responseText, nameVariations)
		if err != nil {
			result.ErrorMessage = fmt.Sprintf("Network org evaluation failed: %v", err)
			return result, nil
		}

		// Store the evaluation in the network_org_evals table
		if err := s.repos.NetworkOrgEvalRepo.Create(ctx, extractionResult.Evaluation); err != nil {
			result.ErrorMessage = fmt.Sprintf("Failed to store network org evaluation: %v", err)
			return result, nil
		}

		// Store competitors in network_org_competitors table
		for _, competitor := range extractionResult.Competitors {
			if err := s.repos.NetworkOrgCompetitorRepo.Create(ctx, competitor); err != nil {
				result.ErrorMessage = fmt.Sprintf("Failed to store network org competitor %s: %v", competitor.Name, err)
				return result, nil
			}
		}

		// Store citations in network_org_citations table
		for _, citation := range extractionResult.Citations {
			if err := s.repos.NetworkOrgCitationRepo.Create(ctx, citation); err != nil {
				result.ErrorMessage = fmt.Sprintf("Failed to store network org citation %s: %v", citation.URL, err)
				return result, nil
			}
		}

		result.HasEvaluation = true
		result.CompetitorCount = len(extractionResult.Competitors)
		result.CitationCount = len(extractionResult.Citations)
		// Use actual cost tracking from NetworkOrgExtractionResult
		result.TotalCost = extractionResult.TotalCost

		fmt.Printf("[ProcessNetworkOrgQuestionRunReeval] ✅ Network org evaluation completed and stored: 1 eval, %d competitors, %d citations, $%.6f cost\n",
			result.CompetitorCount, result.CitationCount, result.TotalCost)
	} else {
		// Create minimal network org eval record indicating no mention
		networkOrgEval := &models.NetworkOrgEval{
			NetworkOrgEvalID: uuid.New(),
			QuestionRunID:    questionRunID,
			OrgID:            orgID,
			Mentioned:        false,
			Citation:         false,
			Sentiment:        nil,
			MentionText:      nil,
			MentionRank:      nil,
			CreatedAt:        time.Now(),
			UpdatedAt:        time.Now(),
		}

		if err := s.repos.NetworkOrgEvalRepo.Create(ctx, networkOrgEval); err != nil {
			result.ErrorMessage = fmt.Sprintf("Failed to create minimal network org eval: %v", err)
			return result, nil
		}

		fmt.Printf("[ProcessNetworkOrgQuestionRunReeval] ✅ Created minimal network org eval (no mention)\n")
	}

	// Success!
	result.Status = "completed"
	fmt.Printf("[ProcessNetworkOrgQuestionRunReeval] ✅ Successfully processed network question run %s with enhanced methodology\n", questionRunID)

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

	// Skip extraction if this was a failed question run
	// Failed runs have the placeholder text from the provider
	if responseText == "Question run failed for this model and location" {
		fmt.Printf("[processQuestionRunWithOrgEvaluation] ⚠️ Skipping extraction for failed question run %s\n", questionRun.QuestionRunID)
		// Create a minimal evaluation record to mark it as processed
		now := time.Now()
		orgEval := &models.OrgEval{
			OrgEvalID:     uuid.New(),
			QuestionRunID: questionRun.QuestionRunID,
			OrgID:         orgID,
			Mentioned:     false,
			Citation:      false,
			CreatedAt:     now,
			UpdatedAt:     now,
		}
		if err := s.repos.OrgEvalRepo.Create(ctx, orgEval); err != nil {
			return fmt.Errorf("failed to store minimal evaluation for failed run: %w", err)
		}
		summary.TotalEvaluations++
		return nil // Successfully handled (by skipping)
	}

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

// GetOrCreateTodaysBatch checks if a batch exists for today, returns it if so, creates new one if not
func (s *orgEvaluationService) GetOrCreateTodaysBatch(ctx context.Context, orgID uuid.UUID, totalQuestions int) (*models.QuestionRunBatch, bool, error) {
	fmt.Printf("[GetOrCreateTodaysBatch] Checking for existing batch for org: %s\n", orgID)

	// Try to find an existing batch from today that's not completed
	today := time.Now().UTC()
	todayStart := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)

	// Fetch batches directly for this org (do not infer from question runs)
	batches, err := s.repos.QuestionRunBatchRepo.GetByOrg(ctx, orgID)
	if err != nil {
		fmt.Printf("[GetOrCreateTodaysBatch] Warning: Failed to get org batches: %v\n", err)
	} else {
		for _, batch := range batches {
			if batch == nil {
				continue
			}
			// Return ANY batch from today (even completed) to avoid duplicates
			if batch.CreatedAt.After(todayStart) {
				fmt.Printf("[GetOrCreateTodaysBatch] ✅ Found existing batch %s from today (status: %s, completed: %d/%d)\n",
					batch.BatchID, batch.Status, batch.CompletedQuestions, batch.TotalQuestions)
				return batch, true, nil
			}
		}
		fmt.Printf("[GetOrCreateTodaysBatch] Checked %d batches, none found from today\n", len(batches))
	}

	// No existing batch found, create new one
	fmt.Printf("[GetOrCreateTodaysBatch] No existing batch found, creating new one\n")
	batch := &models.QuestionRunBatch{
		BatchID:            uuid.New(),
		Scope:              "org",
		OrgID:              &orgID,
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

	fmt.Printf("[GetOrCreateTodaysBatch] Created new batch %s with %d total questions\n", batch.BatchID, totalQuestions)
	return batch, false, nil
}

// CheckQuestionRunExists checks if a question run already exists for the given question/model/location/batch
func (s *orgEvaluationService) CheckQuestionRunExists(ctx context.Context, questionID, modelID, locationID, batchID uuid.UUID) (*models.QuestionRun, error) {
	// Get all runs for this question
	runs, err := s.repos.QuestionRunRepo.GetByQuestion(ctx, questionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get question runs: %w", err)
	}

	// Look for a run that matches this batch AND model AND location
	// For org questions: check model_id and location_id (UUID fields, not NULL)
	for _, run := range runs {
		if run.BatchID != nil && *run.BatchID == batchID &&
			run.ModelID != nil && *run.ModelID == modelID &&
			run.LocationID != nil && *run.LocationID == locationID {
			// Found exact match: same batch, same model, same location
			return run, nil
		}
	}

	return nil, nil
}

// CheckExtractionsExist checks if org evaluations/citations/competitors have been extracted for a question run
func (s *orgEvaluationService) CheckExtractionsExist(ctx context.Context, questionRunID, orgID uuid.UUID) (bool, bool, bool, error) {
	// Check org_eval
	eval, err := s.repos.OrgEvalRepo.GetByQuestionRunAndOrg(ctx, questionRunID, orgID)
	if err != nil {
		return false, false, false, fmt.Errorf("failed to check org eval: %w", err)
	}
	hasEval := eval != nil

	// Check org_citations (check if any exist)
	citations, err := s.repos.OrgCitationRepo.GetByQuestionRunAndOrg(ctx, questionRunID, orgID)
	if err != nil {
		return false, false, false, fmt.Errorf("failed to check citations: %w", err)
	}
	hasCitations := len(citations) > 0

	// Check org_competitors (check if any exist)
	competitors, err := s.repos.OrgCompetitorRepo.GetByQuestionRunAndOrg(ctx, questionRunID, orgID)
	if err != nil {
		return false, false, false, fmt.Errorf("failed to check competitors: %w", err)
	}
	hasCompetitors := len(competitors) > 0

	return hasEval, hasCitations, hasCompetitors, nil
}
