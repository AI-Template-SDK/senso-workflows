package chatgpt

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/AI-Template-SDK/senso-workflows/internal/models"
	"github.com/AI-Template-SDK/senso-workflows/internal/providers/common"
)

// RunQuestionBatch processes multiple questions in a single BrightData API call
func (p *Provider) RunQuestionBatch(ctx context.Context, queries []string, websearch bool, location *models.Location) ([]*common.AIResponse, error) {
	fmt.Printf("[ChatGPTProvider] 🚀 Making batched BrightData call for %d queries\n", len(queries))

	if len(queries) > 20 {
		return nil, fmt.Errorf("batch size %d exceeds maximum of 20", len(queries))
	}

	// 1. Submit batch job to BrightData
	snapshotID, err := p.submitBatchJob(ctx, queries, location, websearch)
	if err != nil {
		return nil, fmt.Errorf("failed to submit BrightData batch job: %w", err)
	}

	fmt.Printf("[ChatGPTProvider] 📋 Batch job submitted with snapshot ID: %s\n", snapshotID)

	// 2. Poll until completion
	if err := p.client.PollUntilComplete(ctx, snapshotID, "ChatGPTProvider"); err != nil {
		return nil, fmt.Errorf("failed to poll BrightData batch job: %w", err)
	}

	// 3. Get results
	bodyBytes, err := p.client.GetBatchResults(ctx, snapshotID, "ChatGPTProvider")
	if err != nil {
		return nil, fmt.Errorf("failed to get batch results: %w", err)
	}

	// 4. Parse results
	results, err := p.parseBatchResults(bodyBytes, snapshotID)
	if err != nil {
		return nil, err
	}

	fmt.Printf("[ChatGPTProvider] 📊 Retrieved %d results for %d queries\n", len(results), len(queries))

	// 5. Match results to queries and convert to AIResponse
	responses, err := p.matchAndConvertResults(results, queries)
	if err != nil {
		return nil, err
	}

	fmt.Printf("[ChatGPTProvider] ✅ Batch completed: %d questions processed, total cost: $%.4f\n",
		len(responses), float64(len(responses))*0.0015)

	return responses, nil
}

// submitBatchJob submits multiple queries to BrightData in a single API call
func (p *Provider) submitBatchJob(ctx context.Context, queries []string, location *models.Location, websearch bool) (string, error) {
	country := common.MapLocationToCountry(location)

	// Build input array with all queries
	inputs := make([]Input, len(queries))
	for i, query := range queries {
		inputs[i] = Input{
			URL:              "https://chatgpt.com/",
			Prompt:           query,
			Country:          country,
			WebSearch:        websearch,
			Index:            i + 1,
			AdditionalPrompt: "",
		}
	}

	payload := Request{
		Input: inputs,
	}

	return p.client.SubmitBatchJob(ctx, payload, p.datasetID)
}

// parseBatchResults parses the raw response bytes into Result structs
func (p *Provider) parseBatchResults(bodyBytes []byte, snapshotID string) ([]Result, error) {
	var results []Result
	if err := json.Unmarshal(bodyBytes, &results); err != nil {
		// Save error response for debugging
		p.client.SaveErrorResponse(bodyBytes, snapshotID, "chatgpt")
		fmt.Printf("[ChatGPTProvider] ❌ Failed to decode as array: %v\n", err)
		fmt.Printf("[ChatGPTProvider] 🔍 Response body preview (first 2000 chars):\n%s\n",
			string(bodyBytes[:common.Min(2000, len(bodyBytes))]))
		return nil, fmt.Errorf("failed to decode results: %w", err)
	}

	if len(results) == 0 {
		fmt.Printf("[ChatGPTProvider] ⚠️ Decoded successfully but got 0 results\n")
		return nil, fmt.Errorf("no results returned from BrightData")
	}

	fmt.Printf("[ChatGPTProvider] ✅ Successfully parsed %d results\n", len(results))
	return results, nil
}

// matchAndConvertResults matches results to queries and converts to AIResponse
func (p *Provider) matchAndConvertResults(results []Result, queries []string) ([]*common.AIResponse, error) {
	// Build result map by index
	resultMap := make(map[int]*Result)
	hasValidIndices := true

	for i := range results {
		// Extract index - check both top-level and input.index (for error results)
		index := results[i].Index
		if index == 0 && results[i].Input != nil {
			index = results[i].Input.Index
		}

		// Get prompt for logging
		promptPreview := results[i].Prompt
		if promptPreview == "" && results[i].Input != nil {
			promptPreview = results[i].Input.Prompt
		}
		if len(promptPreview) > 50 {
			promptPreview = promptPreview[:50] + "..."
		}
		fmt.Printf("[ChatGPTProvider] 🔍 Result %d: index=%d, prompt='%s', hasError=%t\n",
			i, index, promptPreview, results[i].Error != "")

		// Validate index
		if index < 1 || index > len(queries) {
			fmt.Printf("[ChatGPTProvider] ⚠️ Result %d has invalid index %d (expected 1-%d)\n", i, index, len(queries))
			hasValidIndices = false
			break
		}

		// Check for duplicates
		if _, exists := resultMap[index]; exists {
			fmt.Printf("[ChatGPTProvider] ⚠️ Duplicate result index: %d\n", index)
			hasValidIndices = false
			break
		}

		resultMap[index] = &results[i]
	}

	// Verify we have all results
	if hasValidIndices && len(resultMap) != len(queries) {
		fmt.Printf("[ChatGPTProvider] ⚠️ Expected %d results but got %d with valid indices\n", len(queries), len(resultMap))
		hasValidIndices = false
	}

	// Build responses array
	responses := make([]*common.AIResponse, len(queries))

	if hasValidIndices {
		// Use index-based mapping
		fmt.Printf("[ChatGPTProvider] ✅ Using index-based result mapping\n")
		for i := range queries {
			queryIndex := i + 1
			result, exists := resultMap[queryIndex]
			if !exists {
				return nil, fmt.Errorf("missing result for query index %d", queryIndex)
			}
			responses[i] = p.convertResultToResponse(result, queryIndex)
		}
	} else {
		// Fallback: match by prompt text
		fmt.Printf("[ChatGPTProvider] 🔍 Using prompt-based result matching\n")

		allResults := make(map[string]*Result)
		for i := range results {
			prompt := results[i].Prompt
			if prompt == "" && results[i].Input != nil {
				prompt = results[i].Input.Prompt
			}
			if prompt != "" {
				allResults[prompt] = &results[i]
			}
		}

		fmt.Printf("[ChatGPTProvider] 📊 Built result map with %d prompts\n", len(allResults))

		for i, query := range queries {
			result, exists := allResults[query]
			if !exists {
				return nil, fmt.Errorf("no result found for query: %q", query)
			}
			responses[i] = p.convertResultToResponse(result, i+1)
			fmt.Printf("[ChatGPTProvider] ✓ Matched query %d by prompt text\n", i+1)
		}
	}

	return responses, nil
}

// convertResultToResponse converts a Result to an AIResponse
func (p *Provider) convertResultToResponse(result *Result, displayIndex int) *common.AIResponse {
	// Parse citations
	var citations []string
	if result.Citations != nil {
		switch v := result.Citations.(type) {
		case []interface{}:
			for _, citation := range v {
				if str, ok := citation.(string); ok {
					citations = append(citations, str)
				}
			}
		case string:
			if v != "" {
				citations = []string{v}
			}
		}
	}

	// Handle response
	var responseText string
	var shouldProcessEvaluation bool

	if result.Error != "" {
		responseText = "Question run failed for this model and location"
		shouldProcessEvaluation = false
		fmt.Printf("[ChatGPTProvider] ⚠️ Question %d returned error: %s\n", displayIndex, result.Error)
	} else if result.AnswerTextMarkdown == "" {
		responseText = "Question run failed for this model and location"
		shouldProcessEvaluation = false
		fmt.Printf("[ChatGPTProvider] ⚠️ Question %d returned empty answer_text_markdown\n", displayIndex)
	} else {
		responseText = result.AnswerTextMarkdown
		shouldProcessEvaluation = true
	}

	if !shouldProcessEvaluation {
		citations = []string{}
	}

	return &common.AIResponse{
		Response:                responseText,
		InputTokens:             0,
		OutputTokens:            0,
		Cost:                    0.0015,
		Citations:               citations,
		ShouldProcessEvaluation: shouldProcessEvaluation,
	}
}
