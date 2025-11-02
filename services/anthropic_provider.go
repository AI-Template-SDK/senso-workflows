// services/anthropic_provider.go
package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/AI-Template-SDK/senso-workflows/internal/config"
	"github.com/AI-Template-SDK/senso-workflows/internal/models"
	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

type anthropicProvider struct {
	client      *anthropic.Client
	model       string
	costService CostService
}

func NewAnthropicProvider(cfg *config.Config, model string, costService CostService) AIProvider {
	client := anthropic.NewClient(
		option.WithAPIKey(cfg.AnthropicAPIKey),
	)

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
	// Build location-aware prompt
	prompt := p.buildLocationPrompt(query, location)

	if websearch {
		// TODO: Implement web search when available in SDK
		return p.runStructuredSearch(ctx, prompt)
	}
	return p.runStructuredSearch(ctx, prompt)
}

func (p *anthropicProvider) runStructuredSearch(ctx context.Context, query string) (*AIResponse, error) {
	// Use JSON structured prompting
	structuredPrompt := fmt.Sprintf(`You are a knowledgeable assistant providing accurate, location-specific information about financial institutions and credit unions.

Please provide a comprehensive answer to the following question, returning ONLY a valid JSON object with this structure:

{
  "answer": "Your detailed answer here",
  "key_points": ["Key point 1", "Key point 2", "Key point 3"],
  "confidence": "high|medium|low"
}

Question: %s

Remember: Return ONLY the JSON object, no other text.`, query)

	messages := []anthropic.MessageParam{{
		Content: []anthropic.ContentBlockParamUnion{{
			OfText: &anthropic.TextBlockParam{Text: structuredPrompt},
		}},
		Role: anthropic.MessageParamRoleUser,
	}}

	response, err := p.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:       anthropic.Model(p.model),
		MaxTokens:   2000,
		Messages:    messages,
		Temperature: anthropic.Float(0.7),
	})
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}

	// Extract text from response content blocks
	fullResponse := p.extractResponseText(*response)

	// Parse the JSON response
	parsedResponse := p.parseJSONResponse(fullResponse)

	result := &AIResponse{
		Response:                parsedResponse,
		InputTokens:             int(response.Usage.InputTokens),
		OutputTokens:            int(response.Usage.OutputTokens),
		Cost:                    p.costService.CalculateCost(p.GetProviderName(), p.model, int(response.Usage.InputTokens), int(response.Usage.OutputTokens), false),
		ShouldProcessEvaluation: true,
	}

	return result, nil
}

// RunQuestionWebSearch implements AIProvider for web search without location
func (p *anthropicProvider) RunQuestionWebSearch(ctx context.Context, query string) (*AIResponse, error) {
	fmt.Printf("[RunQuestionWebSearch] 🚀 Making web search AI call for query: %s", query)

	// For Anthropic, we'll use the same approach as the regular RunQuestion but without location
	// Since Anthropic doesn't have a separate web search API, we'll use the regular API
	// with web search enabled but no location context

	// Create a neutral location for the API call
	neutralLocation := &models.Location{
		Country: "US", // Default country
	}

	// Use the existing RunQuestion method with websearch=true and neutral location
	return p.RunQuestion(ctx, query, true, neutralLocation)
}

func (p *anthropicProvider) parseJSONResponse(response string) string {
	// Try to parse the JSON response
	var structuredResp struct {
		Answer     string   `json:"answer"`
		KeyPoints  []string `json:"key_points"`
		Confidence string   `json:"confidence"`
	}

	if err := json.Unmarshal([]byte(response), &structuredResp); err != nil {
		// If parsing fails, return the raw response
		return response
	}

	// Format the parsed response
	answer := structuredResp.Answer

	if len(structuredResp.KeyPoints) > 0 {
		answer += "\n\nKey Points:\n"
		for _, point := range structuredResp.KeyPoints {
			answer += fmt.Sprintf("• %s\n", point)
		}
	}

	return answer
}

func (p *anthropicProvider) buildLocationPrompt(query string, location *models.Location) string {
	locationStr := p.formatLocation(location)

	// Add location context to the question
	return fmt.Sprintf("Answer the following question with specific information relevant to %s:\n\n%s",
		locationStr, query)
}

func (p *anthropicProvider) formatLocation(location *models.Location) string {
	if location == nil {
		return "the United States"
	}

	parts := []string{}
	if location.City != nil && *location.City != "" {
		parts = append(parts, *location.City)
	}
	if location.Region != nil && *location.Region != "" {
		parts = append(parts, *location.Region)
	}
	parts = append(parts, location.Country)

	if len(parts) == 0 {
		return "the location"
	}

	result := ""
	for i, part := range parts {
		if i > 0 {
			result += ", "
		}
		result += part
	}

	return result
}

func (p *anthropicProvider) extractResponseText(response anthropic.Message) string {
	var textParts []string

	for _, block := range response.Content {
		switch variant := block.AsAny().(type) {
		case anthropic.TextBlock:
			textParts = append(textParts, variant.Text)
		}
	}

	return strings.Join(textParts, "")
}

// IsAsync returns false for Anthropic (synchronous execution)
func (p *anthropicProvider) IsAsync() bool {
	return false
}

// SupportsBatching returns false for Anthropic (no native batching support)
func (p *anthropicProvider) SupportsBatching() bool {
	return false
}

// GetMaxBatchSize returns 1 for Anthropic (no batching)
func (p *anthropicProvider) GetMaxBatchSize() int {
	return 1
}

// RunQuestionBatch processes questions sequentially for Anthropic (no batching support)
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

// Async methods (not supported for Anthropic - synchronous provider)

// SubmitBatchJob is not supported for synchronous providers
func (p *anthropicProvider) SubmitBatchJob(ctx context.Context, queries []string, websearch bool, location *models.Location) (string, error) {
	return "", fmt.Errorf("async batch jobs not supported for Anthropic provider")
}

// PollJobStatus is not supported for synchronous providers
func (p *anthropicProvider) PollJobStatus(ctx context.Context, jobID string) (string, bool, error) {
	return "", false, fmt.Errorf("async batch jobs not supported for Anthropic provider")
}

// RetrieveBatchResults is not supported for synchronous providers
func (p *anthropicProvider) RetrieveBatchResults(ctx context.Context, jobID string, queries []string) ([]*AIResponse, error) {
	return nil, fmt.Errorf("async batch jobs not supported for Anthropic provider")
}
