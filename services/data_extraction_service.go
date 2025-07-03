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
			CreatedAt:            now,
			UpdatedAt:            now,
		})
	}

	fmt.Printf("[ExtractMentions] Successfully extracted %d mentions\n", len(mentions))
	return mentions, nil
}

// ExtractClaims parses AI response and extracts logical text segments as claims
func (s *dataExtractionService) ExtractClaims(ctx context.Context, questionRunID uuid.UUID, response string, targetCompany string) ([]*models.QuestionRunClaim, error) {
	fmt.Printf("[ExtractClaims] Processing claims for question run %s\n", questionRunID)

	prompt := s.buildClaimsExtractionPrompt(response, targetCompany)

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
			openai.SystemMessage("You are an expert text analyst specializing in logical text segmentation. Segment the AI response into meaningful, coherent text chunks with complete coverage."),
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

	var claims []*models.QuestionRunClaim
	now := time.Now()

	for i, claim := range extractedData.Claims {
		sentiment := s.normalizeSentiment(claim.Sentiment)
		claims = append(claims, &models.QuestionRunClaim{
			QuestionRunClaimID: uuid.New(),
			QuestionRunID:      questionRunID,
			ClaimText:          claim.ClaimText,
			ClaimOrder:         i + 1,
			Sentiment:          &sentiment,
			TargetMentioned:    &claim.TargetMentioned,
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

## ⚠️ CRITICAL: EXTRACT ONLY ACTUAL BUSINESS ENTITIES

**VALID Company/Organization** (Extract these):
- Specific corporations: "JPMorgan Chase", "Bank of America", "Wells Fargo"
- Financial institutions: "Citibank", "Goldman Sachs", "Vanguard"  
- Credit unions: "Navy Federal Credit Union", "State Employees Credit Union"
- Insurance companies: "Allstate", "Geico", "Progressive"
- Fintech companies: "PayPal", "Square", "Stripe"
- Technology companies: "Microsoft", "Apple", "Google" 
- Software products/platforms: "Salesforce", "HubSpot", "QuickBooks"
- Consulting firms: "McKinsey", "Deloitte", "PwC"
- Investment firms: "BlackRock", "Fidelity", "Charles Schwab"

**DO NOT EXTRACT** (Common mistakes to avoid):
- ❌ Lists/Rankings: "Fortune 500 companies", "Top 10 Banks", "Global 100 Most Sustainable Corporations"
- ❌ Awards/Programs: "Canada's Best 50 Corporate Citizens", "Best Places to Work"
- ❌ Generic categories: "Traditional SEO Tools", "Digital Marketing Platforms", "Cloud Services"
- ❌ Government departments: "Office of the Superintendent of Financial Institutions", "Federal Reserve"
- ❌ Industry groups: "Canadian Bankers Association", "Credit Union National Association"
- ❌ Product categories: "mobile banking apps", "investment platforms", "CRM systems"
- ❌ Descriptive phrases: "leading financial institutions", "major credit card companies"
- ❌ Academic institutions: "Harvard Business School", "Stanford University" (unless mentioned as business entities)

## TARGET COMPANY
Target to identify: %s

## EXTRACTION REQUIREMENTS

1. **Entity Name Test**: Ask yourself "Can I visit this entity's website or buy their product/service?" If NO, don't extract it.

2. **Proper Noun Test**: Must be a specific proper noun referring to a named business entity, not a category or description.

3. **Ranking**: Number companies by their FIRST appearance in the text (1 = first mentioned)

4. **Text Extraction**: 
   - Extract the COMPLETE sentence or phrase containing each mention
   - If mentioned multiple times, concatenate ALL contexts with " | " separator
   - Include 5-10 words of context around each mention for clarity
   - Preserve exact punctuation and capitalization

5. **Sentiment Analysis**:
   - "positive": Favorable language, benefits, recommendations, praise, advantages
   - "negative": Criticism, problems, disadvantages, warnings, complaints  
   - "neutral": Factual statements without clear positive/negative tone

6. **Target Company Handling**:
   - Look for ALL variations: full legal name, common name, abbreviations, brand names
   - If target company is NOT mentioned at all, set "target_company" to null
   - Include indirect references like "we", "our company" only if clearly referring to target

## EXAMPLES

**✅ CORRECT EXTRACTION**
Text: "While JPMorgan Chase leads in market share, smaller fintech companies like PayPal and Square are gaining ground. Many users prefer Venmo for peer-to-peer payments."

Extract: JPMorgan Chase, PayPal, Square, Venmo

**❌ INCORRECT EXTRACTION** 
Text: "The Global 100 Most Sustainable Corporations list includes several financial institutions. Traditional SEO tools don't work well for banks."

Do NOT extract: "Global 100 Most Sustainable Corporations", "Traditional SEO tools" 
These are categories/lists, not specific companies.

**✅ FINANCIAL SERVICES EXAMPLE**
Target: "Sunlife Financial"
Text: "Sunlife Financial competes with Manulife and Great-West Life in the Canadian insurance market. Many clients also consider RBC Insurance and TD Insurance for their coverage needs."

Expected extraction:
{
  "target_company": {
    "name": "Sunlife Financial", 
    "rank": 1,
    "mentioned_text": "Sunlife Financial competes with Manulife and Great-West Life",
    "text_sentiment": "neutral"
  },
  "competitors": [
    {
      "name": "Manulife",
      "rank": 2, 
      "mentioned_text": "competes with Manulife and Great-West Life in the Canadian insurance market",
      "text_sentiment": "neutral"
    },
    {
      "name": "Great-West Life",
      "rank": 3,
      "mentioned_text": "competes with Manulife and Great-West Life in the Canadian insurance market", 
      "text_sentiment": "neutral"
    },
    {
      "name": "RBC Insurance",
      "rank": 4,
      "mentioned_text": "Many clients also consider RBC Insurance and TD Insurance for their coverage",
      "text_sentiment": "neutral"
    },
    {
      "name": "TD Insurance",
      "rank": 5,
      "mentioned_text": "consider RBC Insurance and TD Insurance for their coverage needs",
      "text_sentiment": "neutral"
    }
  ]
}

## RESPONSE TO ANALYZE
%s

## FINAL VALIDATION CHECKLIST
Before extracting each entity, verify:
✓ Is this a specific, named business entity (not a category or list)?
✓ Could I find this company's website or purchase their products/services?
✓ Is this a proper noun referring to an actual organization?
✓ Am I avoiding generic terms, categories, and descriptive phrases?
✓ Have I excluded government departments, industry associations, and academic institutions?

Focus on QUALITY over quantity. Better to extract 3 real companies than 10 categories.`, targetCompany, response)
}

func (s *dataExtractionService) buildClaimsExtractionPrompt(response string, targetCompany string) string {
	return fmt.Sprintf(`You are an expert text segmentation specialist. Your task is to divide the AI response into comprehensive logical text segments that achieve COMPLETE COVERAGE of the entire response.

## ⚠️ CRITICAL MISSION: COMPLETE TEXT SEGMENTATION

**PRIMARY OBJECTIVE**: Every single word, sentence, and paragraph of the response MUST be included in exactly one claim segment. No text should be lost or duplicated.

**SEGMENTATION PHILOSOPHY**: 
- **From:** Atomic fact extraction  
- **To:** Logical text chunking with complete response coverage

## SEGMENTATION BOUNDARIES

**Split Points (in order of priority):**

1. **Paragraph Breaks**: Natural logical divisions in the text
2. **Organization Transitions**: When discussion shifts between different companies/brands
3. **Topic Shifts**: When subject matter changes (e.g., products → pricing → market analysis)
4. **Logical Groupings**: Keep related sentences together for coherent context

**Size Guidelines:**
- **Minimum**: Complete sentences (never fragments)
- **Typical**: 1-3 related sentences or 1 paragraph
- **Maximum**: Multi-sentence groups that share logical context
- **Key**: Meaningful chunks that preserve context and readability

## COMPLETE COVERAGE REQUIREMENTS

1. **100% Text Inclusion**: Every character of the response must belong to exactly one segment
2. **No Gaps**: No sentences or phrases should be skipped
3. **No Overlaps**: No text should appear in multiple segments
4. **Preserve Order**: Segments should follow the original text sequence
5. **Maintain Integrity**: Don't break sentences or lose context

## ANALYSIS REQUIREMENTS PER SEGMENT

For each text segment, determine:

### **SENTIMENT ANALYSIS**:
- **"positive"**: Favorable language, benefits, praise, advantages, recommendations
- **"negative"**: Criticism, problems, disadvantages, warnings, complaints, issues
- **"neutral"**: Factual statements, neutral descriptions, balanced information

### **TARGET COMPANY DETECTION**:
**Target Organization**: %s

- **true**: Segment explicitly mentions the target company by name, brand, or clear reference
- **false**: Segment discusses other companies, general topics, or industry without target mention

**Target Detection Rules**:
- Look for exact company name: "%s"
- Include common variations, abbreviations, brand names
- Include clear pronoun references ("we", "our company") if context clearly refers to target
- Do NOT mark as target mention for: generic terms, industry references, competitor discussions

## SEGMENTATION EXAMPLES

**Input Response:**
TechFlow Solutions, founded in 2018, now serves over 10,000 enterprise clients. Their flagship product processes 2.5 million API calls per day with 99.9%% uptime.

The company's revenue grew 145%% year-over-year to $50 million in 2023. This positions them well against competitors like DataCorp and StreamlineAPI.

Industry analysts rank TechFlow #3 in customer satisfaction, though some users report occasional latency issues during peak hours.

**Expected Segmentation:**
{
  "claims": [
    {
      "claim_text": "TechFlow Solutions, founded in 2018, now serves over 10,000 enterprise clients. Their flagship product processes 2.5 million API calls per day with 99.9%% uptime.",
      "sentiment": "positive",
      "target_mentioned": true
    },
    {
      "claim_text": "The company's revenue grew 145%% year-over-year to $50 million in 2023. This positions them well against competitors like DataCorp and StreamlineAPI.",
      "sentiment": "positive", 
      "target_mentioned": true
    },
    {
      "claim_text": "Industry analysts rank TechFlow #3 in customer satisfaction, though some users report occasional latency issues during peak hours.",
      "sentiment": "neutral",
      "target_mentioned": true
    }
  ]
}

## VERBATIM TEXT PRESERVATION

**CRITICAL**: Extract text segments EXACTLY as written:
- ✅ Preserve all punctuation: . , ; : ! ? " ' - —
- ✅ Maintain original capitalization
- ✅ Keep all numbers, symbols, special characters
- ✅ Include URLs and citations exactly as they appear
- ✅ Preserve spacing and line breaks within segments
- ❌ Do NOT include formatting elements (bullets, numbers, headers)
- ❌ Do NOT paraphrase, edit, or "clean up" text

## SEGMENTATION VALIDATION

Before finalizing, verify:
✓ **Complete Coverage**: Every word from original response is included in exactly one segment
✓ **Logical Boundaries**: Segments break at natural transition points
✓ **Meaningful Size**: Each segment is substantial enough for analysis
✓ **Context Preservation**: Related concepts stay together
✓ **Sentiment Accuracy**: Tone assessment reflects the text content
✓ **Target Detection**: Company mentions are accurately identified

## RESPONSE TO SEGMENT
%s

## FINAL REMINDERS
- **COMPREHENSIVE COVERAGE**: The sum of all segments must equal the complete original response
- **LOGICAL GROUPINGS**: Prioritize natural text flow and topic coherence
- **EXACT TEXT MATCHING**: Copy every character precisely, no modifications
- **CONTEXTUAL ANALYSIS**: Assess sentiment and target mentions thoughtfully
- **NO TEXT LEFT BEHIND**: Every sentence, phrase, and word must be captured

Focus on creating meaningful, complete text segments that together reconstruct the entire original response.`, targetCompany, targetCompany, response)
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

	var citations []*models.QuestionRunCitation
	now := time.Now()

	for i, citation := range extractedData.Citations {
		citations = append(citations, &models.QuestionRunCitation{
			QuestionRunCitationID: uuid.New(),
			QuestionRunClaimID:    claim.QuestionRunClaimID,
			SourceURL:             citation.SourceURL,
			CitationType:          citation.Type,
			CitationOrder:         i + 1,
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
- With ports: server.com:8080/api
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
   - http://server.com:8080/api → server.com

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
