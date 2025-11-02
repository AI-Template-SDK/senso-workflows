package providers_test

import (
	"strings"
	"testing"

	"github.com/AI-Template-SDK/senso-workflows/internal/providers"
	"github.com/AI-Template-SDK/senso-workflows/internal/providers/testutil"
)

func TestFactoryCreatesCorrectProvider(t *testing.T) {
	tests := []struct {
		modelName        string
		expectedProvider string
		shouldError      bool
	}{
		{"chatgpt", "chatgpt", false},
		{"chatgpt-4", "chatgpt", false},
		{"ChatGPT", "chatgpt", false},
		{"perplexity", "", true},        // Not implemented yet
		{"gemini", "", true},            // Not implemented yet
		{"gpt-4.1", "", true},           // Not implemented yet
		{"claude-3-5-sonnet", "", true}, // Not implemented yet
		{"unsupported-model", "", true},
		{"", "", true},
	}

	cfg := testutil.SampleConfig()
	costService := testutil.NewMockCostService()

	for _, tt := range tests {
		t.Run(tt.modelName, func(t *testing.T) {
			provider, err := providers.NewProvider(tt.modelName, cfg, costService)

			if tt.shouldError {
				if err == nil {
					t.Errorf("Expected error for model %s, but got none", tt.modelName)
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error for model %s: %v", tt.modelName, err)
				return
			}

			if provider == nil {
				t.Errorf("Provider is nil for model %s", tt.modelName)
				return
			}

			if provider.GetProviderName() != tt.expectedProvider {
				t.Errorf("Expected provider %s, got %s", tt.expectedProvider, provider.GetProviderName())
			}
		})
	}
}

func TestFactoryModelNamePatterns(t *testing.T) {
	tests := []struct {
		modelName string
		pattern   string
	}{
		{"chatgpt", "chatgpt"},
		{"chatgpt-4", "chatgpt"},
		{"CHATGPT", "chatgpt"},
		{"my-chatgpt-model", "chatgpt"},
	}

	cfg := testutil.SampleConfig()
	costService := testutil.NewMockCostService()

	for _, tt := range tests {
		t.Run(tt.modelName, func(t *testing.T) {
			provider, err := providers.NewProvider(tt.modelName, cfg, costService)
			if err != nil {
				if !strings.Contains(err.Error(), "not yet implemented") {
					t.Errorf("Unexpected error: %v", err)
				}
				return
			}

			if provider.GetProviderName() != tt.pattern {
				t.Errorf("Model %s should match pattern %s, got provider %s",
					tt.modelName, tt.pattern, provider.GetProviderName())
			}
		})
	}
}

func TestFactoryWithNilConfig(t *testing.T) {
	costService := testutil.NewMockCostService()

	// This should not panic (provider should handle nil config gracefully)
	provider, err := providers.NewProvider("chatgpt", nil, costService)
	if err != nil {
		t.Logf("Factory returned error for nil config: %v", err)
		return
	}

	if provider == nil {
		t.Error("Provider should not be nil even with nil config")
	}
}

func TestFactoryWithNilCostService(t *testing.T) {
	cfg := testutil.SampleConfig()

	// This should not panic
	provider, err := providers.NewProvider("chatgpt", cfg, nil)
	if err != nil {
		t.Errorf("Should handle nil cost service: %v", err)
	}

	if provider == nil {
		t.Error("Provider should not be nil even with nil cost service")
	}
}
