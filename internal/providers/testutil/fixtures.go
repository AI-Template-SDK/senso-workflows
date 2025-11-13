package testutil

import (
	"github.com/AI-Template-SDK/senso-workflows/internal/config"
	"github.com/AI-Template-SDK/senso-workflows/internal/models"
)

// SampleConfig returns a test configuration
func SampleConfig() *config.Config {
	return &config.Config{
		BrightDataAPIKey:    "test-api-key",
		BrightDataDatasetID: "test-dataset-id",
		PerplexityDatasetID: "test-perplexity-id",
		GeminiDatasetID:     "test-gemini-id",
		OpenAIAPIKey:        "test-openai-key",
		AnthropicAPIKey:     "test-anthropic-key",
	}
}

// SampleLocation returns a test location
func SampleLocation() *models.Location {
	region := "California"
	city := "San Francisco"
	return &models.Location{
		Country: "US",
		Region:  &region,
		City:    &city,
	}
}

// SampleQueries returns test queries
func SampleQueries() []string {
	return []string{
		"What are the top credit unions?",
		"Which banks have the best rates?",
		"What is the average loan interest rate?",
	}
}

// SampleBrightDataResponse returns a mock BrightData response
func SampleBrightDataResponse() string {
	return `[
		{
			"url": "https://chatgpt.com/",
			"prompt": "What are the top credit unions?",
			"answer_text_markdown": "Here are the top credit unions: 1. Navy Federal 2. State Employees 3. Pentagon Federal",
			"citations": ["https://example.com/credit-unions"],
			"country": "US",
			"web_search_triggered": true,
			"index": 1
		},
		{
			"url": "https://chatgpt.com/",
			"prompt": "Which banks have the best rates?",
			"answer_text_markdown": "The banks with the best rates include: Chase, Bank of America, and Wells Fargo.",
			"citations": ["https://example.com/rates"],
			"country": "US",
			"web_search_triggered": true,
			"index": 2
		},
		{
			"url": "https://chatgpt.com/",
			"prompt": "What is the average loan interest rate?",
			"answer_text_markdown": "The average personal loan interest rate is approximately 11.5%.",
			"citations": null,
			"country": "US",
			"web_search_triggered": true,
			"index": 3
		}
	]`
}

// SampleErrorResponse returns a mock error response from BrightData
func SampleErrorResponse() string {
	return `[
		{
			"error": "Request timeout",
			"input": {
				"url": "https://chatgpt.com/",
				"prompt": "What are the top credit unions?",
				"country": "US",
				"index": 1,
				"web_search": true,
				"additional_prompt": ""
			}
		}
	]`
}

// SampleStatusResponse returns a mock status response (building)
func SampleStatusResponse() string {
	return `{
		"status": "building",
		"message": "Snapshot is still being built"
	}`
}

// SampleProgressResponse returns a mock progress response
func SampleProgressResponse() string {
	return `{
		"status": "running",
		"snapshot_id": "test-snapshot-123",
		"dataset_id": "gd_abc123"
	}`
}

// SampleReadyProgressResponse returns a ready progress response
func SampleReadyProgressResponse() string {
	return `{
		"status": "ready",
		"snapshot_id": "test-snapshot-123",
		"dataset_id": "gd_abc123"
	}`
}
