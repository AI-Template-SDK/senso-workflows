// services/cost_service.go
package services

import "strings"

type costService struct{}

func NewCostService() CostService {
	return &costService{}
}

// Cost per 1M tokens
var costPerToken = map[string]struct{ input, output float64 }{
	"gpt-5":                    {input: 1.25, output: 10.00},
	"gpt-5-mini":               {input: 0.25, output: 2.00},
	"gpt-4.1":                  {input: 3.00, output: 12.00},
	"gpt-4.1-mini":             {input: 0.80, output: 3.20},
	"gpt-4o-2024-08-06":        {input: 2.50, output: 10.00}, // GPT-4o structured outputs pricing
	"claude-sonnet-4-20250514": {input: 3.00, output: 15.00},
	"sonar":                    {input: 1.00, output: 1.00}, // Perplexity Sonar pricing (estimated)
}

// Cost per 1000 web searches
var costPerWebSearch = map[string]float64{
	"openai":     35.00,
	"anthropic":  10.00,
	"perplexity": 8.00,
}

func (s *costService) CalculateCost(provider string, model string, inputTokens int, outputTokens int, websearch bool) float64 {
	// Calculate token costs
	modelCosts, exists := costPerToken[model]
	if !exists {
		// Default to GPT-4.1 costs if model not found
		modelCosts = costPerToken["gpt-4.1"]
	}

	inputCost := (float64(inputTokens) / 1_000_000.0) * modelCosts.input
	outputCost := (float64(outputTokens) / 1_000_000.0) * modelCosts.output
	totalCost := inputCost + outputCost

	// Add web search cost if applicable
	if websearch {
		providerKey := s.getProviderKey(provider)
		if searchCost, exists := costPerWebSearch[providerKey]; exists {
			totalCost += searchCost / 1000.0
		}
	}

	return totalCost
}

func (s *costService) getProviderKey(provider string) string {
	provider = strings.ToLower(provider)
	if strings.Contains(provider, "openai") || strings.Contains(provider, "gpt") {
		return "openai"
	}
	if strings.Contains(provider, "anthropic") || strings.Contains(provider, "claude") {
		return "anthropic"
	}
	if strings.Contains(provider, "perplexity") || strings.Contains(provider, "sonar") {
		return "perplexity"
	}
	return "openai" // default
}
