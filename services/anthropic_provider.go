// services/anthropic_provider.go
package services

import (
	"context"
	"fmt"
	"strings"

	"github.com/AI-Template-SDK/senso-workflows/internal/config"
	"github.com/AI-Template-SDK/senso-workflows/internal/models"
	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/packages/param"
)

// defaultAnthropicModel is the model used when no model override is provided.
// Sonnet 4.6 is Anthropic's most capable Sonnet model and includes native web search support.
const defaultAnthropicModel = "claude-sonnet-4-6"

type anthropicProvider struct {
	client      *anthropic.Client
	model       string
	costService CostService
}

func NewAnthropicProvider(cfg *config.Config, model string, costService CostService) AIProvider {
	client := anthropic.NewClient(
		option.WithAPIKey(cfg.AnthropicAPIKey),
	)

	model = strings.TrimSpace(model)
	if model == "" {
		model = defaultAnthropicModel
	}

	fmt.Printf("[NewAnthropicProvider] ✅ Created Anthropic provider\n")
	fmt.Printf("[NewAnthropicProvider]   - Model: %s\n", model)

	return &anthropicProvider{
		client:      &client,
		model:       model,
		costService: costService,
	}
}

func (p *anthropicProvider) GetProviderName() string {
	return "anthropic"
}

func (p *anthropicProvider) RunQuestion(ctx context.Context, query string, websearch bool, location *models.Location) (*AIResponse, error) {
	prompt := p.buildLocationPrompt(query, location)
	return p.runSearch(ctx, prompt, websearch, location)
}

// RunQuestionWebSearch implements AIProvider for a web search call without location context.
func (p *anthropicProvider) RunQuestionWebSearch(ctx context.Context, query string) (*AIResponse, error) {
	fmt.Printf("[AnthropicProvider] 🚀 Running web search call for query: %s\n", query)

	neutralLocation := &models.Location{Country: "US"}
	return p.runSearch(ctx, query, true, neutralLocation)
}

// runSearch performs a Claude completion with optional native web search.
// When websearch is true the API enables the web_search tool and we collect any
// returned citations into AIResponse.Citations.
func (p *anthropicProvider) runSearch(ctx context.Context, query string, websearch bool, location *models.Location) (*AIResponse, error) {
	systemPrompt := "You are a knowledgeable assistant. When the user asks a question, provide a comprehensive, accurate, well-organized answer. When web search is available, use it to ground claims in current, authoritative sources and cite them."

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(p.model),
		MaxTokens: 4000,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(query)),
		},
		System: []anthropic.TextBlockParam{
			{Text: systemPrompt},
		},
		Temperature: anthropic.Float(0.7),
	}

	if websearch {
		webSearchTool := anthropic.WebSearchTool20250305Param{
			MaxUses: param.NewOpt[int64](5),
		}
		if location != nil {
			normalized := normalizeLocation(location)
			userLocation := anthropic.UserLocationParam{}
			if normalized.CountryCode != "" {
				userLocation.Country = param.NewOpt(normalized.CountryCode)
			}
			if normalized.Region != nil && *normalized.Region != "" {
				userLocation.Region = param.NewOpt(*normalized.Region)
			}
			if normalized.City != nil && *normalized.City != "" {
				userLocation.City = param.NewOpt(*normalized.City)
			}
			webSearchTool.UserLocation = userLocation
		}

		params.Tools = []anthropic.ToolUnionParam{
			{OfWebSearchTool20250305: &webSearchTool},
		}
	}

	response, err := p.client.Messages.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("anthropic messages.New failed: %w", err)
	}

	responseText, citations := p.extractTextAndCitations(*response)
	if strings.TrimSpace(responseText) == "" {
		return &AIResponse{
			Response:                "This prompt didn’t complete successfully due to a temporary AI model limitation. You were not charged for this prompt. We'll re-try in the next run.",
			Citations:               []string{},
			ShouldProcessEvaluation: false,
		}, nil
	}

	cost := p.costService.CalculateCost(p.GetProviderName(), p.model, int(response.Usage.InputTokens), int(response.Usage.OutputTokens), websearch)

	fmt.Printf("[AnthropicProvider] ✅ Call completed\n")
	fmt.Printf("[AnthropicProvider]   - Response length: %d characters\n", len(responseText))
	fmt.Printf("[AnthropicProvider]   - Citations: %d\n", len(citations))
	fmt.Printf("[AnthropicProvider]   - Web search: %t\n", websearch)
	fmt.Printf("[AnthropicProvider]   - Input tokens: %d\n", response.Usage.InputTokens)
	fmt.Printf("[AnthropicProvider]   - Output tokens: %d\n", response.Usage.OutputTokens)
	fmt.Printf("[AnthropicProvider]   - Cost: $%.6f\n", cost)

	return &AIResponse{
		Response:                responseText,
		InputTokens:             int(response.Usage.InputTokens),
		OutputTokens:            int(response.Usage.OutputTokens),
		Cost:                    cost,
		Citations:               citations,
		ShouldProcessEvaluation: true,
	}, nil
}

// extractTextAndCitations walks the response content blocks, joining text blocks
// and de-duplicating citation URLs returned by the web_search tool.
func (p *anthropicProvider) extractTextAndCitations(response anthropic.Message) (string, []string) {
	var textParts []string
	seen := make(map[string]bool)
	citations := []string{}

	for _, block := range response.Content {
		switch variant := block.AsAny().(type) {
		case anthropic.TextBlock:
			textParts = append(textParts, variant.Text)
			for _, cit := range variant.Citations {
				url := strings.TrimSpace(cit.URL)
				if url == "" || !strings.HasPrefix(url, "http") || seen[url] {
					continue
				}
				seen[url] = true
				citations = append(citations, url)
			}
		}
	}

	return strings.Join(textParts, ""), citations
}

func (p *anthropicProvider) buildLocationPrompt(query string, location *models.Location) string {
	locationStr := formatLocationForPrompt(location)
	return fmt.Sprintf("Answer the following question with specific information relevant to %s:\n\n%s",
		locationStr, query)
}

// SupportsBatching returns false for Anthropic (no native batching support).
func (p *anthropicProvider) SupportsBatching() bool {
	return false
}

// GetMaxBatchSize returns 1 for Anthropic (no batching).
func (p *anthropicProvider) GetMaxBatchSize() int {
	return 1
}

// RunQuestionBatch processes questions sequentially for Anthropic.
func (p *anthropicProvider) RunQuestionBatch(ctx context.Context, queries []string, websearch bool, location *models.Location) ([]*AIResponse, error) {
	fmt.Printf("[AnthropicProvider] 🔄 Processing %d questions sequentially (no batching support)\n", len(queries))

	responses := make([]*AIResponse, len(queries))
	for i, query := range queries {
		response, err := p.RunQuestion(ctx, query, websearch, location)
		if err != nil {
			return nil, fmt.Errorf("failed to process question %d: %w", i+1, err)
		}
		responses[i] = response
	}

	return responses, nil
}
