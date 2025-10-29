package providers

import (
	"fmt"
	"strings"

	"github.com/AI-Template-SDK/senso-workflows/internal/config"
	"github.com/AI-Template-SDK/senso-workflows/internal/providers/chatgpt"
	"github.com/AI-Template-SDK/senso-workflows/services"
)

// NewProvider creates the appropriate AI provider based on the model name
func NewProvider(modelName string, cfg *config.Config, costService services.CostService) (AIProvider, error) {
	modelLower := strings.ToLower(modelName)

	// ChatGPT provider (via BrightData)
	if strings.Contains(modelLower, "chatgpt") {
		fmt.Printf("[ProviderFactory] 🎯 Selected ChatGPT provider for model: %s\n", modelName)
		return chatgpt.NewProvider(cfg, modelName, costService), nil
	}

	// Perplexity provider (via BrightData)
	if strings.Contains(modelLower, "perplexity") {
		fmt.Printf("[ProviderFactory] 🎯 Selected Perplexity provider for model: %s\n", modelName)
		// TODO: Implement perplexity.NewProvider
		return nil, fmt.Errorf("perplexity provider not yet implemented")
	}

	// Gemini provider (via BrightData)
	if strings.Contains(modelLower, "gemini") {
		fmt.Printf("[ProviderFactory] 🎯 Selected Gemini provider for model: %s\n", modelName)
		// TODO: Implement gemini.NewProvider
		return nil, fmt.Errorf("gemini provider not yet implemented")
	}

	// OpenAI provider (gpt-4.1, etc.)
	if strings.Contains(modelLower, "gpt") || strings.Contains(modelLower, "4.1") {
		if cfg.OpenAIAPIKey == "" {
			return nil, fmt.Errorf("OpenAI API key is empty in config")
		}
		fmt.Printf("[ProviderFactory] 🎯 Selected OpenAI provider for model: %s\n", modelName)
		// TODO: Implement openai.NewProvider
		return nil, fmt.Errorf("openai provider not yet implemented")
	}

	// Anthropic provider
	if strings.Contains(modelLower, "claude") || strings.Contains(modelLower, "sonnet") ||
		strings.Contains(modelLower, "opus") || strings.Contains(modelLower, "haiku") {
		fmt.Printf("[ProviderFactory] 🎯 Selected Anthropic provider for model: %s\n", modelName)
		// TODO: Implement anthropic.NewProvider
		return nil, fmt.Errorf("anthropic provider not yet implemented")
	}

	return nil, fmt.Errorf("unsupported model: %s", modelName)
}
