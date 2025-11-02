package chatgpt_test

import (
	"testing"

	"github.com/AI-Template-SDK/senso-workflows/internal/providers/chatgpt"
	"github.com/AI-Template-SDK/senso-workflows/internal/providers/testutil"
)

func TestProviderMetadata(t *testing.T) {
	cfg := testutil.SampleConfig()
	costService := testutil.NewMockCostService()
	provider := chatgpt.NewProvider(cfg, "chatgpt", costService)

	tests := []struct {
		name     string
		got      interface{}
		expected interface{}
	}{
		{"GetProviderName", provider.GetProviderName(), "chatgpt"},
		{"IsAsync", provider.IsAsync(), true},
		{"SupportsBatching", provider.SupportsBatching(), true},
		{"GetMaxBatchSize", provider.GetMaxBatchSize(), 20},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("%s = %v, want %v", tt.name, tt.got, tt.expected)
			}
		})
	}
}

func TestNewProvider(t *testing.T) {
	cfg := testutil.SampleConfig()
	costService := testutil.NewMockCostService()

	provider := chatgpt.NewProvider(cfg, "chatgpt", costService)

	if provider == nil {
		t.Fatal("NewProvider returned nil")
	}

	// Verify provider implements the interface correctly
	if provider.GetProviderName() != "chatgpt" {
		t.Errorf("Expected provider name 'chatgpt', got '%s'", provider.GetProviderName())
	}
}

func TestProviderWithEmptyDatasetID(t *testing.T) {
	cfg := testutil.SampleConfig()
	cfg.BrightDataDatasetID = "" // Empty dataset ID
	costService := testutil.NewMockCostService()

	provider := chatgpt.NewProvider(cfg, "chatgpt", costService)

	// Should create provider but operations will fail
	if provider == nil {
		t.Fatal("NewProvider should not return nil even with empty dataset ID")
	}
}
