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
		Response:     parsedResponse,
		InputTokens:  int(response.Usage.InputTokens),
		OutputTokens: int(response.Usage.OutputTokens),
		Cost:         p.costService.CalculateCost(p.GetProviderName(), p.model, int(response.Usage.InputTokens), int(response.Usage.OutputTokens), false),
	}

	return result, nil
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
