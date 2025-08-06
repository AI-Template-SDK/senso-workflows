package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/invopop/jsonschema"
	"github.com/joho/godotenv"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/azure"
)

// AnalysisResult defines the structured data we expect from the OpenAI model.
// JSonschema tags are used to generate the JSON schema for the model's response.
type AnalysisResult struct {
	OrgMentions bool     `json:"org_mentions" jsonschema_description:"True if the specified organization is mentioned, otherwise false."`
	Sentiment   string   `json:"sentiment" jsonschema:"enum=positive,enum=neutral,enum=negative" jsonschema_description:"The overall sentiment of the text towards the organization."`
	Citations   []string `json:"citations" jsonschema_description:"Sentences from the text that directly mention or refer to the organization."`
}

// generateSchema creates a JSON schema from a given Go struct.
// The OpenAI API uses this schema to format its response.
func generateSchema[T any]() interface{} {
	// These reflector options are important for compatibility with OpenAI's
	// structured output feature.
	reflector := jsonschema.Reflector{
		AllowAdditionalProperties: false, // Must be false for OpenAI
		DoNotReference:            true,  // Helps create a self-contained schema
	}
	var v T
	schema := reflector.Reflect(&v)
	return schema
}

// analyzeTextForOrg sends text to Azure OpenAI and gets a structured analysis back.
func analyzeTextForOrg(ctx context.Context, text, organization, azureEndpoint, azureApiKey, azureDeploymentName string) (*AnalysisResult, error) {
	// 1. Configure the Azure OpenAI client
	// It uses the azure.WithEndpoint and azure.WithAPIKey helpers from the SDK.
	// The deployment name is passed as the model name in the request,
	// and the Azure middleware handles routing it correctly.
	client := openai.NewClient(
		azure.WithEndpoint(azureEndpoint, "2024-12-01-preview"), // Use a recent, stable API version
		azure.WithAPIKey(azureApiKey),
	)

	// 2. Generate the JSON schema for our desired output structure.
	analysisSchema := generateSchema[AnalysisResult]()

	// 3. Define the response format using the generated schema.
	// We give it a name and description to help the model understand the task.
	// 'Strict: true' tells the model it MUST conform to the schema.
	schemaParam := openai.ResponseFormatJSONSchemaJSONSchemaParam{
		Name:        "text_analysis",
		Description: openai.String("Analyze text for mentions of a specific organization."),
		Schema:      analysisSchema,
		Strict:      openai.Bool(true),
	}

	// 4. Construct the prompt for the model.
	// This prompt clearly instructs the model on its task, providing the text
	// and the organization to look for.
	prompt := fmt.Sprintf(
		"Analyze the following text to determine if it mentions the organization '%s'. "+
			"Based on the text, determine the sentiment towards the organization and extract any sentences that serve as citations or direct mentions. "+
			"Text to analyze: \"%s\"",
		organization,
		text,
	)

	// 5. Create and send the chat completion request.
	req := openai.ChatCompletionNewParams{
		Model: azureDeploymentName, // Your Azure deployment name
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.UserMessage(prompt),
		},
		ResponseFormat: openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONSchema: &openai.ResponseFormatJSONSchemaParam{JSONSchema: schemaParam},
		},
	}

	log.Println("Sending request to Azure OpenAI...")
	resp, err := client.Chat.Completions.New(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("chat completion failed: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("no choices returned from API")
	}

	// 6. The model's response is a JSON string. Unmarshal it into our Go struct.
	var result AnalysisResult
	responseContent := resp.Choices[0].Message.Content
	log.Println("Received raw JSON response:", responseContent)

	err = json.Unmarshal([]byte(responseContent), &result)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON response: %w", err)
	}

	return &result, nil
}

func main() {
	// Load environment variables from a .env file for easy configuration.
	err := godotenv.Load()
	if err != nil {
		log.Println("Warning: .env file not found, relying on existing environment variables.")
	}

	// Retrieve Azure credentials and configuration from environment variables.
	azureOpenAIEndpoint := os.Getenv("AZURE_OPENAI_ENDPOINT")
	azureOpenAIKey := os.Getenv("AZURE_OPENAI_API_KEY")
	azureDeploymentName := os.Getenv("AZURE_OPENAI_DEPLOYMENT_NAME") // The name of your model deployment in Azure

	if azureOpenAIEndpoint == "" || azureOpenAIKey == "" || azureDeploymentName == "" {
		log.Fatal("Please set AZURE_OPENAI_ENDPOINT, AZURE_OPENAI_API_KEY, and AZURE_OPENAI_DEPLOYMENT_NAME environment variables.")
	}

	// Example usage
	organization := "Innovatech"
	textToAnalyze := "Innovatech has released its quarterly earnings, showing strong growth. " +
		"Analysts are positive about the company's future prospects. " +
		"A recent report highlighted that Innovatech's new AI platform is revolutionizing the industry. " +
		"However, some critics have raised concerns about data privacy."

	ctx := context.Background()
	analysis, err := analyzeTextForOrg(ctx, textToAnalyze, organization, azureOpenAIEndpoint, azureOpenAIKey, azureDeploymentName)
	if err != nil {
		log.Fatalf("Error analyzing text: %v", err)
	}

	// Print the structured results
	fmt.Println("\n--- Analysis Results ---")
	fmt.Printf("Organization Mentioned: %t\n", analysis.OrgMentions)
	fmt.Printf("Sentiment: %s\n", analysis.Sentiment)
	fmt.Println("Citations:")
	if len(analysis.Citations) > 0 {
		for i, citation := range analysis.Citations {
			fmt.Printf("  %d: %s\n", i+1, citation)
		}
	} else {
		fmt.Println("  None found.")
	}
	fmt.Println("------------------------")
}
