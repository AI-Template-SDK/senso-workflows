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
	"github.com/openai/openai-go/option"
)

type dataExtractionService struct {
	cfg          *config.Config
	openAIClient *openai.Client
	costService  CostService
}

func NewDataExtractionService(cfg *config.Config) DataExtractionService {
	fmt.Printf("[NewDataExtractionService] Creating service with OpenAI key (length: %d)\n", len(cfg.OpenAIAPIKey))

	client := openai.NewClient(option.WithAPIKey(cfg.OpenAIAPIKey))

	return &dataExtractionService{
		cfg:          cfg,
		openAIClient: &client,
		costService:  NewCostService(),
	}
}

// ExtractMentions parses AI response and extracts company mentions
func (s *dataExtractionService) ExtractMentions(ctx context.Context, questionRunID uuid.UUID, response string, targetCompany string) ([]*models.QuestionRunMention, error) {
	fmt.Printf("[ExtractMentions] Processing mentions for question run %s\n", questionRunID)

	prompt := s.buildMentionsExtractionPrompt(response, targetCompany)

	// Use a model that supports structured outputs
	model := openai.ChatModelGPT4_1

	schemaParam := openai.ResponseFormatJSONSchemaJSONSchemaParam{
		Name:        "company_mentions_extraction",
		Description: openai.String("Extract mentions of financial institutions from AI response"),
		Schema:      GenerateSchema[MentionsExtractionResponse](),
		Strict:      openai.Bool(true),
	}

	// Create the extraction request with structured output
	chatResponse, err := s.openAIClient.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage("You are an expert financial services analyst specializing in credit unions and banks. Extract company mentions accurately and comprehensively."),
			openai.UserMessage(prompt),
		},
		Model: model,
		ResponseFormat: openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONSchema: &openai.ResponseFormatJSONSchemaParam{JSONSchema: schemaParam},
		},
		Temperature: openai.Float(0), // Deterministic extraction
	})

	if err != nil {
		return nil, fmt.Errorf("failed to extract mentions: %w", err)
	}

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

	// Process target company
	if extractedData.TargetCompany != nil {
		sentiment := s.normalizeSentiment(extractedData.TargetCompany.TextSentiment)
		mentions = append(mentions, &models.QuestionRunMention{
			QuestionRunMentionID: uuid.New(),
			QuestionRunID:        questionRunID,
			MentionOrg:           extractedData.TargetCompany.Name,
			MentionText:          extractedData.TargetCompany.MentionedText,
			MentionRank:          &extractedData.TargetCompany.Rank,
			MentionSentiment:     &sentiment,
			TargetOrg:            true,
			InputTokens:          &inputTokens,
			OutputTokens:         &outputTokens,
			TotalCost:            &totalCost,
			CreatedAt:            now,
			UpdatedAt:            now,
		})
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

	fmt.Printf("[ExtractMentions] Successfully extracted %d mentions\n", len(mentions))
	return mentions, nil
}

// ExtractClaims parses AI response and extracts factual claims
func (s *dataExtractionService) ExtractClaims(ctx context.Context, questionRunID uuid.UUID, response string, targetCompany string, orgWebsites []string) ([]*models.QuestionRunClaim, error) {
	fmt.Printf("[ExtractClaims] Processing claims for question run %s\n", questionRunID)

	prompt := s.buildClaimsExtractionPrompt(response, targetCompany, orgWebsites)

	// Use a model that supports structured outputs
	model := openai.ChatModelGPT4_1

	schemaParam := openai.ResponseFormatJSONSchemaJSONSchemaParam{
		Name:        "claims_extraction",
		Description: openai.String("Extract factual claims from AI response"),
		Schema:      GenerateSchema[ClaimsExtractionResponse](),
		Strict:      openai.Bool(true),
	}

	// Create the extraction request
	chatResponse, err := s.openAIClient.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage("You are an expert fact-checker. Break down the response into individual, verifiable factual claims."),
			openai.UserMessage(prompt),
		},
		Model: model,
		ResponseFormat: openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONSchema: &openai.ResponseFormatJSONSchemaParam{JSONSchema: schemaParam},
		},
		Temperature: openai.Float(0),
	})

	if err != nil {
		return nil, fmt.Errorf("failed to extract claims: %w", err)
	}

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
		sentiment := s.normalizeSentiment(claim.Sentiment)
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

	fmt.Printf("[ExtractClaims] Successfully extracted %d claims\n", len(claims))
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

// Helper methods
func (s *dataExtractionService) buildMentionsExtractionPrompt(response, targetCompany string) string {
	return fmt.Sprintf(`You are an expert competitive intelligence analyst focused on extracting SPECIFIC COMPANY AND BRAND NAMES from financial services content.

## 🚨 MOST CRITICAL RULE: ONLY EXTRACT WHAT IS ACTUALLY MENTIONED

**You MUST only extract companies that are explicitly mentioned in the response text below.**
**IGNORE any company names that appear in these instructions - they are examples only.**
**The ONLY text you should analyze is in the "RESPONSE TEXT TO ANALYZE" section at the bottom.**

## ⚠️ EXTRACTION CRITERIA: VALID Business Entities Only

**EXTRACT These** (Only if actually mentioned in the response text):
- Specific corporations, financial institutions, credit unions
- Insurance companies, fintech companies, technology companies
- Software products/platforms, consulting firms, investment firms

**NEVER EXTRACT** (Common mistakes):
- ❌ Lists/Rankings: "Fortune 500 companies", "Top 10 Banks"
- ❌ Awards/Programs: "Best Corporate Citizens"
- ❌ Generic categories: "SEO Tools", "Digital Marketing Platforms"
- ❌ Government departments, Industry groups, Product categories
- ❌ Descriptive phrases: "leading financial institutions"

## TARGET ORGANIZATION ANALYSIS

**Your target organization for this analysis is: "%s"**

## CRITICAL EXTRACTION LOGIC

**For the Target Organization:**
- IF this organization (or recognizable variations) appears in the RESPONSE TEXT → create target_company record
- IF this organization does NOT appear in the RESPONSE TEXT → set target_company to null
- IGNORE the fact that you see this organization name in these instructions

**Target Organization Variations to Look For:**
- Short name/brand: Remove domain extensions (.com, .ai, .io), legal suffixes (Inc, LLC, Corp)
- Common abbreviations and shortened versions
- Minor spelling or formatting variations

**For Competitors:**
- ONLY extract companies explicitly named in the RESPONSE TEXT
- Each must be a specific business entity with an official website
- Each extraction must be traceable to specific text in the response

**Quality Control:**
- Better to extract 0 companies than to extract non-existent mentions
- Every extraction must be found in the RESPONSE TEXT, not in these instructions
- When in doubt, exclude rather than include

## EXTRACTION REQUIREMENTS

1. **Presence Test**: Company must be explicitly mentioned in the response text
2. **Entity Name Test**: Must be a specific business entity, not a category
3. **Ranking**: Number companies by their FIRST appearance in the text
4. **Text Extraction**: Extract the complete sentence/phrase containing each mention
5. **Sentiment Analysis**: "positive", "negative", or "neutral"

## EXAMPLES OF CORRECT BEHAVIOR

**Example A: Target IS in response text**
Target Organization: "TechCorp Inc"
Response Text: "TechCorp's new platform competes with Salesforce and HubSpot."
Result: target_company = TechCorp record, competitors = [Salesforce, HubSpot]

**Example B: Target is NOT in response text**
Target Organization: "TechCorp Inc"  
Response Text: "Salesforce and HubSpot dominate the CRM market."
Result: target_company = null, competitors = [Salesforce, HubSpot]

**Example C: No companies in response text**
Target Organization: "TechCorp Inc"
Response Text: "The software industry faces many challenges."
Result: target_company = null, competitors = []

## ⚠️ RESPONSE TEXT TO ANALYZE (ANALYZE ONLY THIS TEXT) ⚠️
"""
%s
"""

## FINAL REMINDER
- Look ONLY at the response text above
- The target organization "%s" should only be extracted if it appears in that response text
- Do not be influenced by seeing company names in these instructions`, targetCompany, response, targetCompany)
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
	prompt := s.buildCitationsExtractionPrompt(claim.ClaimText, response, orgWebsites)

	// Use a model that supports structured outputs
	model := openai.ChatModelGPT4_1

	schemaParam := openai.ResponseFormatJSONSchemaJSONSchemaParam{
		Name:        "citations_extraction",
		Description: openai.String("Extract citations for a specific claim"),
		Schema:      GenerateSchema[CitationsExtractionResponse](),
		Strict:      openai.Bool(true),
	}

	chatResponse, err := s.openAIClient.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage("You are an expert researcher specializing in citation extraction and URL analysis. Extract URLs exactly as they appear in the text."),
			openai.UserMessage(prompt),
		},
		Model: model,
		ResponseFormat: openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONSchema: &openai.ResponseFormatJSONSchemaParam{JSONSchema: schemaParam},
		},
		Temperature: openai.Float(0),
	})

	if err != nil {
		return nil, fmt.Errorf("failed to extract citations: %w", err)
	}

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

%s## DOMAIN CLASSIFICATION SYSTEM

**PRIMARY CITATION**: URL domain matches organization's official domains (listed above)
- Exact domain match: example.com → https://example.com/page ✓
- Subdomain match: blog.example.com, docs.example.com, www.example.com ✓  
- Protocol ignored: http vs https doesn't matter
- Path/parameters ignored: any path after domain counts
- Case insensitive: Example.com = example.com

**SECONDARY CITATION**: Any other valid URL that doesn't match org domains
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

## DOMAIN EXTRACTION & MATCHING LOGIC

1. **Extract base domain from URL**:
   - https://blog.example.com/post → blog.example.com
   - www.subdomain.site.org/page → subdomain.site.org
   - http://server.com:8000/api → server.com

2. **Compare against org domains**:
   - Remove protocols (http://, https://)
   - Remove www. prefix for comparison
   - Check if URL domain ends with any org domain
   - example.com matches: example.com, www.example.com, blog.example.com
   - example.com does NOT match: notexample.com, example.com.evil.com

3. **Classification logic**:
   - If domain match found → PRIMARY
   - If no domain match → SECONDARY
   - If no URL found → empty array

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

## FINAL CRITICAL REMINDERS
- **PROXIMITY IS EVERYTHING**: Only extract URLs that are contextually close to the claim
- **EMPTY ARRAYS ARE NORMAL**: Most claims will have no citations - this is expected
- **CONSERVATIVE OVER AGGRESSIVE**: Better to miss a citation than assign incorrectly
- **CONTEXT MATTERS**: Look at where the claim appears in the response, not the entire response
- **NO CITATION PRESSURE**: Don't feel compelled to find citations for every claim

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
