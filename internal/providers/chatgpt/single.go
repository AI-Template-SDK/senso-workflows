package chatgpt

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/AI-Template-SDK/senso-workflows/internal/models"
	"github.com/AI-Template-SDK/senso-workflows/internal/providers/common"
)

// RunQuestion processes a single question (legacy/fallback method)
// For ChatGPT, this is primarily for compatibility - batching should be used instead
func (p *Provider) RunQuestion(ctx context.Context, query string, websearch bool, location *models.Location) (*common.AIResponse, error) {
	fmt.Printf("[ChatGPTProvider] 🚀 Making single BrightData call for query\n")

	// 1. Submit job to BrightData
	snapshotID, err := p.submitSingleJob(ctx, query, location, websearch)
	if err != nil {
		return nil, fmt.Errorf("failed to submit BrightData job: %w", err)
	}

	fmt.Printf("[ChatGPTProvider] 📋 Job submitted with snapshot ID: %s\n", snapshotID)

	// 2. Poll until completion
	if err := p.client.PollUntilComplete(ctx, snapshotID, "ChatGPTProvider"); err != nil {
		return nil, fmt.Errorf("failed to poll BrightData job: %w", err)
	}

	// 3. Get results
	bodyBytes, err := p.client.GetBatchResults(ctx, snapshotID, "ChatGPTProvider")
	if err != nil {
		return nil, fmt.Errorf("failed to get results: %w", err)
	}

	// 4. Parse single result
	result, err := p.parseSingleResult(bodyBytes, snapshotID)
	if err != nil {
		return nil, err
	}

	// 5. Convert to AIResponse
	return p.convertResultToResponse(result, 1), nil
}

// RunQuestionWebSearch runs a single question with web search enabled (default US location)
func (p *Provider) RunQuestionWebSearch(ctx context.Context, query string) (*common.AIResponse, error) {
	defaultLocation := &models.Location{
		Country: "US",
	}
	return p.RunQuestion(ctx, query, true, defaultLocation)
}

// submitSingleJob submits a single query to BrightData
func (p *Provider) submitSingleJob(ctx context.Context, query string, location *models.Location, websearch bool) (string, error) {
	country := common.MapLocationToCountry(location)

	payload := Request{
		Input: []Input{
			{
				URL:              "https://chatgpt.com/",
				Prompt:           query,
				Country:          country,
				WebSearch:        websearch,
				Index:            1,
				AdditionalPrompt: "",
			},
		},
	}

	return p.client.SubmitBatchJob(ctx, payload, p.datasetID)
}

// parseSingleResult parses a single result from the response
func (p *Provider) parseSingleResult(bodyBytes []byte, snapshotID string) (*Result, error) {
	var results []Result
	if err := json.Unmarshal(bodyBytes, &results); err != nil {
		p.client.SaveErrorResponse(bodyBytes, snapshotID, "chatgpt")
		fmt.Printf("[ChatGPTProvider] ❌ Failed to decode results: %v\n", err)
		return nil, fmt.Errorf("failed to decode results: %w", err)
	}

	if len(results) == 0 {
		return nil, fmt.Errorf("no results returned from BrightData")
	}

	// Return the first (and should be only) result
	return &results[0], nil
}
