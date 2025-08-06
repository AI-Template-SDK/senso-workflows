// services/openai_provider.go
package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/AI-Template-SDK/senso-workflows/internal/config"
	"github.com/AI-Template-SDK/senso-workflows/internal/models"
	"github.com/invopop/jsonschema"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

type openAIProvider struct {
	client      *openai.Client
	model       string
	costService CostService
	apiKey      string
}

func NewOpenAIProvider(cfg *config.Config, model string, costService CostService) AIProvider {
	client := openai.NewClient(
		option.WithAPIKey(cfg.OpenAIAPIKey),
	)

	return &openAIProvider{
		client:      &client,
		model:       model,
		costService: costService,
		apiKey:      cfg.OpenAIAPIKey,
	}
}

func (p *openAIProvider) GetProviderName() string {
	return "openai"
}

// QuestionResponse represents the structured output for question responses
type QuestionResponse struct {
	Answer     string   `json:"answer" jsonschema_description:"The comprehensive answer to the question"`
	KeyPoints  []string `json:"key_points" jsonschema_description:"3-5 key points from the answer"`
	Confidence string   `json:"confidence" jsonschema:"enum=high,enum=medium,enum=low" jsonschema_description:"Confidence level in the answer accuracy"`
}

// WebSearchRequest represents the request structure for OpenAI web search API
type WebSearchRequest struct {
	Model string          `json:"model"`
	Tools []WebSearchTool `json:"tools"`
	Input string          `json:"input"`
}

type WebSearchTool struct {
	Type         string          `json:"type"`
	UserLocation WebUserLocation `json:"user_location"`
}

type WebUserLocation struct {
	Type    string  `json:"type"`
	Country string  `json:"country"`
	Region  *string `json:"region,omitempty"`
	City    *string `json:"city,omitempty"`
}

// WebSearchResponse represents the response from OpenAI web search API
type WebSearchResponse struct {
	ID     string                `json:"id"`
	Object string                `json:"object"`
	Status string                `json:"status"`
	Output []WebSearchOutputItem `json:"output"`
	Usage  WebSearchUsage        `json:"usage"`
}

type WebSearchOutputItem struct {
	ID      string             `json:"id"`
	Type    string             `json:"type"`
	Status  string             `json:"status,omitempty"`
	Content []WebSearchContent `json:"content,omitempty"`
	Action  *WebSearchAction   `json:"action,omitempty"`
}

type WebSearchContent struct {
	Type        string                `json:"type"`
	Text        string                `json:"text,omitempty"`
	Annotations []WebSearchAnnotation `json:"annotations,omitempty"`
}

type WebSearchAnnotation struct {
	Type       string `json:"type"`
	StartIndex int    `json:"start_index"`
	EndIndex   int    `json:"end_index"`
	Title      string `json:"title,omitempty"`
	URL        string `json:"url,omitempty"`
}

type WebSearchAction struct {
	Type  string `json:"type"`
	Query string `json:"query,omitempty"`
	URL   string `json:"url,omitempty"`
}

type WebSearchUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

// Generate the JSON schema at initialization time
var QuestionResponseSchema = GenerateQuestionSchema[QuestionResponse]()

func GenerateQuestionSchema[T any]() interface{} {
	reflector := jsonschema.Reflector{
		AllowAdditionalProperties: false,
		DoNotReference:            true,
	}
	var v T
	schema := reflector.Reflect(v)
	return schema
}

// RunQuestion implements AIProvider using web search when enabled
func (p *openAIProvider) RunQuestion(ctx context.Context, query string, websearch bool, location *models.Location) (*AIResponse, error) {
	// Build location-aware prompt
	prompt := p.buildLocationPrompt(query, location)

	// Use web search API when websearch is enabled
	if websearch {
		return p.runWebSearch(ctx, prompt, location)
	}

	// Use structured output for non-websearch queries via SDK
	schemaParam := openai.ResponseFormatJSONSchemaJSONSchemaParam{
		Name:        "question_response",
		Description: openai.String("Structured response to the question"),
		Schema:      QuestionResponseSchema,
		Strict:      openai.Bool(true),
	}

	response, err := p.client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage("You are a helpful assistant that provides accurate, comprehensive answers to questions."),
			openai.UserMessage(prompt),
		},
		Model: openai.ChatModel(p.model),
		ResponseFormat: openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONSchema: &openai.ResponseFormatJSONSchemaParam{JSONSchema: schemaParam},
		},
		Temperature: openai.Float(0.7),
		MaxTokens:   openai.Int(2000),
	})

	if err != nil {
		return nil, fmt.Errorf("chat completion failed: %w", err)
	}

	if len(response.Choices) == 0 {
		return nil, fmt.Errorf("no response choices returned")
	}

	// Parse the structured response
	responseContent := response.Choices[0].Message.Content
	var structuredResp QuestionResponse
	if err := json.Unmarshal([]byte(responseContent), &structuredResp); err == nil {
		// Use the answer field as the main response
		responseContent = structuredResp.Answer

		// Optionally append key points
		if len(structuredResp.KeyPoints) > 0 {
			responseContent += "\n\nKey Points:\n"
			for _, point := range structuredResp.KeyPoints {
				responseContent += fmt.Sprintf("• %s\n", point)
			}
		}
	}

	result := &AIResponse{
		Response:     responseContent,
		InputTokens:  int(response.Usage.PromptTokens),
		OutputTokens: int(response.Usage.CompletionTokens),
		Cost:         p.costService.CalculateCost(p.GetProviderName(), p.model, int(response.Usage.PromptTokens), int(response.Usage.CompletionTokens), false),
	}

	return result, nil
}

// runWebSearch uses OpenAI's web search API directly
func (p *openAIProvider) runWebSearch(ctx context.Context, query string, location *models.Location) (*AIResponse, error) {
	// Convert our location to web search format
	userLocation := WebUserLocation{
		Type:    "approximate",
		Country: strings.ToUpper(location.Country), // API expects uppercase country codes
	}
	if location.Region != nil && *location.Region != "" {
		userLocation.Region = location.Region
	}
	if location.City != nil && *location.City != "" {
		userLocation.City = location.City
	}

	// Prepare the request
	requestBody := WebSearchRequest{
		Model: p.model,
		Tools: []WebSearchTool{
			{
				Type:         "web_search_preview",
				UserLocation: userLocation,
			},
		},
		Input: query,
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Make the HTTP request to OpenAI web search API
	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.openai.com/v1/responses", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("web search request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("web search API returned status %d", resp.StatusCode)
	}

	// Parse the response
	var webSearchResp WebSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&webSearchResp); err != nil {
		return nil, fmt.Errorf("failed to decode web search response: %w", err)
	}

	// Extract the final message content from the response
	responseText := ""
	for _, output := range webSearchResp.Output {
		if output.Type == "message" && len(output.Content) > 0 {
			for _, content := range output.Content {
				if content.Type == "output_text" {
					responseText = content.Text
					break
				}
			}
			if responseText != "" {
				break
			}
		}
	}

	if responseText == "" {
		return nil, fmt.Errorf("no message content found in web search response")
	}

	result := &AIResponse{
		Response:     responseText,
		InputTokens:  webSearchResp.Usage.InputTokens,
		OutputTokens: webSearchResp.Usage.OutputTokens,
		Cost:         p.costService.CalculateCost(p.GetProviderName(), p.model, webSearchResp.Usage.InputTokens, webSearchResp.Usage.OutputTokens, true),
	}

	return result, nil
}

func (p *openAIProvider) buildLocationPrompt(query string, location *models.Location) string {
	locationStr := p.formatLocation(location)

	// Add location context to the question
	return fmt.Sprintf("Answer the following question with specific information relevant to %s:\n\n%s",
		locationStr, query)
}

func (p *openAIProvider) formatLocation(location *models.Location) string {
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

func (p *openAIProvider) CreateEmbedding(ctx context.Context, texts []string, model string) (*EmbeddingResult, error) {
	if len(texts) == 0 {
		return &EmbeddingResult{
			Vectors:      [][]float32{},
			InputTokens:  0,
			OutputTokens: 0,
			Cost:         0,
			Model:        model,
		}, nil
	}

	// The openai-go client expects a specific model type from its library
	var embeddingModel openai.EmbeddingModel
	switch model {
	case "text-embedding-ada-002":
		embeddingModel = openai.EmbeddingModelTextEmbeddingAda002
	case "text-embedding-3-small":
		embeddingModel = openai.EmbeddingModelTextEmbedding3Small
	case "text-embedding-3-large":
		embeddingModel = openai.EmbeddingModelTextEmbedding3Large
	default:
		return nil, fmt.Errorf("unsupported embedding model: %s", model)
	}

	// Prepare the request parameters
	params := openai.EmbeddingNewParams{
		Input: openai.EmbeddingNewParamsInputUnion{
			OfArrayOfStrings: texts,
		},
		Model: embeddingModel,
	}

	// Make the API call
	resp, err := p.client.Embeddings.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("openai embedding request failed: %w", err)
	}

	// Extract the float vectors from the response
	embeddings := make([][]float32, len(resp.Data))
	for i, data := range resp.Data {
		// The library returns float64, so we convert to float32
		float32Embedding := make([]float32, len(data.Embedding))
		for j, val := range data.Embedding {
			float32Embedding[j] = float32(val)
		}
		embeddings[i] = float32Embedding
	}

	// Calculate cost using the cost service
	cost := p.costService.CalculateCost(p.GetProviderName(), model, int(resp.Usage.PromptTokens), 0, false)

	return &EmbeddingResult{
		Vectors:      embeddings,
		InputTokens:  int(resp.Usage.PromptTokens),
		OutputTokens: 0, // Embeddings don't have output tokens
		Cost:         cost,
		Model:        model,
	}, nil
}
