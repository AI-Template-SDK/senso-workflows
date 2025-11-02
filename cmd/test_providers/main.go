package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/AI-Template-SDK/senso-workflows/internal/config"
	"github.com/AI-Template-SDK/senso-workflows/internal/models"
	"github.com/AI-Template-SDK/senso-workflows/internal/providers"
	"github.com/AI-Template-SDK/senso-workflows/internal/providers/common"
	"github.com/AI-Template-SDK/senso-workflows/services"
	"github.com/joho/godotenv"
)

func main() {
	fmt.Println("🧪 AI Provider Test Script")
	fmt.Println("=" + string(make([]byte, 50)))

	// Load .env file if it exists
	if err := godotenv.Load(); err != nil {
		fmt.Println("⚠️  No .env file found, using environment variables")
	} else {
		fmt.Println("✅ Loaded .env file")
	}
	fmt.Println()

	// Load configuration
	cfg := loadConfig()

	// Create cost service (required for providers)
	costService := services.NewCostService()

	// Test queries
	queries := []string{
		"What are the top 3 credit unions in California?",
		"Which banks offer the best savings rates?",
		"What is the average interest rate for personal loans?",
	}

	location := &models.Location{
		Country: "US",
		Region:  strPtr("California"),
	}

	// Test different providers
	fmt.Println("\n📋 Test Configuration:")
	fmt.Printf("  - Queries: %d\n", len(queries))
	fmt.Printf("  - Location: %s, %s\n", *location.Region, location.Country)
	fmt.Println()

	// Uncomment the provider you want to test:

	// Test ChatGPT (Async)
	testProvider("chatgpt", cfg, costService, queries, location)

	// Test Perplexity (Async)
	// testProvider("perplexity", cfg, costService, queries, location)

	// Test Gemini (Async)
	// testProvider("gemini", cfg, costService, queries, location)

	// Test OpenAI (Sync)
	// testProvider("gpt-4.1", cfg, costService, queries, location)

	// Test Anthropic (Sync)
	// testProvider("claude-3-5-sonnet-20241022", cfg, costService, queries, location)
}

func testProvider(modelName string, cfg *config.Config, costService services.CostService, queries []string, location *models.Location) {
	fmt.Printf("\n🎯 Testing Provider: %s\n", modelName)
	fmt.Println(string(make([]byte, 60)))

	ctx := context.Background()

	// Create provider
	provider, err := providers.NewProvider(modelName, cfg, costService)
	if err != nil {
		fmt.Printf("❌ Failed to create provider: %v\n", err)
		return
	}

	fmt.Printf("✅ Provider created: %s\n", provider.GetProviderName())
	fmt.Printf("   - IsAsync: %t\n", provider.IsAsync())
	fmt.Printf("   - SupportsBatching: %t\n", provider.SupportsBatching())
	fmt.Printf("   - MaxBatchSize: %d\n", provider.GetMaxBatchSize())
	fmt.Println()

	// Test based on provider type
	if provider.IsAsync() {
		testAsyncProvider(ctx, provider, queries, location)
	} else {
		testSyncProvider(ctx, provider, queries, location)
	}
}

func testAsyncProvider(ctx context.Context, provider providers.AIProvider, queries []string, location *models.Location) {
	fmt.Println("🔄 Testing ASYNC provider (3-step process)")
	fmt.Println()

	startTime := time.Now()

	// Step 1: Submit batch job
	fmt.Println("📤 Step 1: Submitting batch job...")
	submitStart := time.Now()
	jobID, err := provider.SubmitBatchJob(ctx, queries, true, location)
	if err != nil {
		fmt.Printf("❌ Failed to submit batch job: %v\n", err)
		return
	}
	submitDuration := time.Since(submitStart)
	fmt.Printf("✅ Batch job submitted in %v\n", submitDuration)
	fmt.Printf("   Job ID: %s\n", jobID)
	fmt.Println()

	// Step 2: Poll until ready
	fmt.Println("⏳ Step 2: Polling for job completion...")
	pollStart := time.Now()
	pollCount := 0

	for {
		pollCount++
		status, ready, err := provider.PollJobStatus(ctx, jobID)
		if err != nil {
			fmt.Printf("❌ Failed to poll job status: %v\n", err)
			return
		}

		fmt.Printf("   Poll #%d: status=%s, ready=%t\n", pollCount, status, ready)

		if ready {
			pollDuration := time.Since(pollStart)
			fmt.Printf("✅ Job completed in %v (%d polls)\n", pollDuration, pollCount)
			break
		}

		if pollCount > 60 { // Max 10 minutes
			fmt.Printf("❌ Job timed out after %d polls\n", pollCount)
			return
		}

		time.Sleep(10 * time.Second)
	}
	fmt.Println()

	// Step 3: Retrieve results
	fmt.Println("📥 Step 3: Retrieving batch results...")
	retrieveStart := time.Now()
	responses, err := provider.RetrieveBatchResults(ctx, jobID, queries)
	if err != nil {
		fmt.Printf("❌ Failed to retrieve results: %v\n", err)
		return
	}
	retrieveDuration := time.Since(retrieveStart)
	fmt.Printf("✅ Results retrieved in %v\n", retrieveDuration)
	fmt.Println()

	// Save responses to files
	saveResponses(jobID, responses)

	// Display results
	displayResults(responses, queries)

	totalDuration := time.Since(startTime)
	fmt.Println()
	fmt.Printf("⏱️  Total Time: %v\n", totalDuration)
	fmt.Printf("   - Submit: %v\n", submitDuration)
	fmt.Printf("   - Poll: %v (%d checks)\n", time.Since(pollStart), pollCount)
	fmt.Printf("   - Retrieve: %v\n", retrieveDuration)
}

func testSyncProvider(ctx context.Context, provider providers.AIProvider, queries []string, location *models.Location) {
	fmt.Println("⚡ Testing SYNC provider (1-step process)")
	fmt.Println()

	startTime := time.Now()

	// Generate a jobID for sync providers (using timestamp)
	jobID := fmt.Sprintf("sync_%d", time.Now().Unix())

	// Single call
	fmt.Println("🚀 Executing batch request...")
	responses, err := provider.RunQuestionBatch(ctx, queries, true, location)
	if err != nil {
		fmt.Printf("❌ Failed to run batch: %v\n", err)
		return
	}
	duration := time.Since(startTime)
	fmt.Printf("✅ Batch completed in %v\n", duration)
	fmt.Println()

	// Save responses to files
	saveResponses(jobID, responses)

	// Display results
	displayResults(responses, queries)

	fmt.Println()
	fmt.Printf("⏱️  Total Time: %v\n", duration)
}

func displayResults(responses []*common.AIResponse, queries []string) {
	fmt.Printf("📊 Results Summary:\n")
	fmt.Printf("   - Total responses: %d\n", len(responses))
	fmt.Println()

	totalCost := 0.0
	successCount := 0

	for i, resp := range responses {
		fmt.Printf("Question %d: %s\n", i+1, truncate(queries[i], 60))
		fmt.Printf("  Status: ")
		if resp.ShouldProcessEvaluation {
			fmt.Printf("✅ Success\n")
			successCount++
		} else {
			fmt.Printf("❌ Failed\n")
		}
		fmt.Printf("  Response: %s\n", truncate(resp.Response, 100))
		fmt.Printf("  Tokens: %d input, %d output\n", resp.InputTokens, resp.OutputTokens)
		fmt.Printf("  Cost: $%.6f\n", resp.Cost)
		fmt.Println()

		totalCost += resp.Cost
	}

	fmt.Printf("💰 Total Cost: $%.6f\n", totalCost)
	fmt.Printf("✅ Success Rate: %d/%d (%.1f%%)\n", successCount, len(responses), float64(successCount)/float64(len(responses))*100)
}

func loadConfig() *config.Config {
	cfg := &config.Config{
		// BrightData (for ChatGPT/Perplexity/Gemini)
		BrightDataAPIKey:    getEnv("BRIGHTDATA_API_KEY", ""),
		BrightDataDatasetID: getEnv("BRIGHTDATA_DATASET_ID", ""),
		PerplexityDatasetID: getEnv("PERPLEXITY_DATASET_ID", ""),
		GeminiDatasetID:     getEnv("GEMINI_DATASET_ID", ""),

		// OpenAI
		OpenAIAPIKey: getEnv("OPENAI_API_KEY", ""),

		// Azure OpenAI (optional)
		AzureOpenAIEndpoint:       getEnv("AZURE_OPENAI_ENDPOINT", ""),
		AzureOpenAIKey:            getEnv("AZURE_OPENAI_KEY", ""),
		AzureOpenAIDeploymentName: getEnv("AZURE_OPENAI_DEPLOYMENT_NAME", ""),

		// Anthropic
		AnthropicAPIKey: getEnv("ANTHROPIC_API_KEY", ""),
	}

	return cfg
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func strPtr(s string) *string {
	return &s
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func saveResponses(jobID string, responses []*common.AIResponse) {
	fmt.Println("💾 Saving responses to files...")
	for i, resp := range responses {
		filename := fmt.Sprintf("%s_%d.json", jobID, i+1)
		data, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			fmt.Printf("  ❌ Failed to marshal response %d: %v\n", i+1, err)
			continue
		}
		if err := os.WriteFile(filename, data, 0644); err != nil {
			fmt.Printf("  ❌ Failed to save %s: %v\n", filename, err)
			continue
		}
		fmt.Printf("  ✅ Saved: %s\n", filename)
	}
	fmt.Println()
}
