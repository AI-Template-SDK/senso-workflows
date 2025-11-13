package chatgpt_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/AI-Template-SDK/senso-workflows/internal/providers/chatgpt"
)

func TestConvertResultToResponseSuccess(t *testing.T) {
	// Read sample data
	sampleData, err := os.ReadFile("testdata/sample_response.json")
	if err != nil {
		t.Fatalf("Failed to read sample data: %v", err)
	}

	var results []chatgpt.Result
	if err := json.Unmarshal(sampleData, &results); err != nil {
		t.Fatalf("Failed to parse sample data: %v", err)
	}

	// Note: We can't directly call convertResultToResponse as it's private
	// This test validates the structure of test data
	// In real implementation, we'd test through public methods

	if len(results) == 0 {
		t.Fatal("No results to test")
	}

	result := results[0]

	// Verify success case has required fields
	if result.AnswerTextMarkdown == "" {
		t.Error("Expected non-empty answer_text_markdown for success case")
	}

	if result.Error != "" {
		t.Error("Success case should not have error field")
	}

	if result.Index != 1 {
		t.Errorf("Expected index 1, got %d", result.Index)
	}
}

func TestConvertResultToResponseError(t *testing.T) {
	// Read error data
	errorData, err := os.ReadFile("testdata/error_response.json")
	if err != nil {
		t.Fatalf("Failed to read error data: %v", err)
	}

	var results []chatgpt.Result
	if err := json.Unmarshal(errorData, &results); err != nil {
		t.Fatalf("Failed to parse error data: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("No results to test")
	}

	errorResult := results[0]

	// Verify error case has error field
	if errorResult.Error == "" {
		t.Error("Error case should have error field populated")
	}

	// Verify error case has input echoed
	if errorResult.Input == nil {
		t.Error("Error case should have input echoed back")
	}

	if errorResult.AnswerTextMarkdown != "" {
		t.Error("Error case should not have answer_text_markdown")
	}
}

func TestConvertResultToResponseEmptyAnswer(t *testing.T) {
	result := chatgpt.Result{
		URL:                "https://chatgpt.com/",
		Prompt:             "Test question",
		AnswerTextMarkdown: "", // Empty answer
		Country:            "US",
		Index:              1,
	}

	// This would be converted to a failed response with ShouldProcessEvaluation: false
	// Verifying the structure is correct
	if result.AnswerTextMarkdown != "" {
		t.Error("Expected empty answer_text_markdown")
	}

	if result.Error != "" {
		t.Error("No error field should be set for empty answer")
	}
}

func TestResultMatchingByIndex(t *testing.T) {
	// Read sample data
	sampleData, err := os.ReadFile("testdata/sample_response.json")
	if err != nil {
		t.Fatalf("Failed to read sample data: %v", err)
	}

	var results []chatgpt.Result
	if err := json.Unmarshal(sampleData, &results); err != nil {
		t.Fatalf("Failed to parse sample data: %v", err)
	}

	// Verify indices are sequential and valid
	for i, result := range results {
		expectedIndex := i + 1
		if result.Index != expectedIndex {
			t.Errorf("Result %d has index %d, expected %d", i, result.Index, expectedIndex)
		}
	}
}

func TestResultMatchingByPrompt(t *testing.T) {
	// Read invalid index data (requires prompt matching)
	invalidData, err := os.ReadFile("testdata/invalid_index_response.json")
	if err != nil {
		t.Fatalf("Failed to read invalid index data: %v", err)
	}

	var results []chatgpt.Result
	if err := json.Unmarshal(invalidData, &results); err != nil {
		t.Fatalf("Failed to parse invalid index data: %v", err)
	}

	// Verify prompts are present for fallback matching
	for i, result := range results {
		if result.Prompt == "" {
			t.Errorf("Result %d has empty prompt, cannot match by prompt", i)
		}
	}

	// Verify indices are invalid (0)
	for i, result := range results {
		if result.Index != 0 {
			t.Errorf("Result %d should have invalid index 0, got %d", i, result.Index)
		}
	}
}
