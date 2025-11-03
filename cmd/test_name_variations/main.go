// cmd/test_name_variations/main.go
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/AI-Template-SDK/senso-workflows/internal/config"
	"github.com/AI-Template-SDK/senso-workflows/services"
	"github.com/joho/godotenv"
)

func main() {
	fmt.Println("=== Testing GenerateNameVariations Service Function ===")

	// Load .env file
	if err := godotenv.Load(); err != nil {
		log.Printf("⚠️  Warning: .env file not found, using existing environment variables")
	} else {
		fmt.Println("✅ Loaded .env file")
	}

	// Load configuration from environment
	cfg := config.Load()

	// Validate OpenAI/Azure configuration
	if cfg.OpenAIAPIKey == "" && cfg.AzureOpenAIKey == "" {
		log.Fatal("❌ Error: Either OPENAI_API_KEY or AZURE_OPENAI_KEY must be set")
	}

	fmt.Println("✅ Configuration loaded")
	if cfg.AzureOpenAIEndpoint != "" {
		fmt.Printf("   Using Azure OpenAI (Endpoint: %s)\n", cfg.AzureOpenAIEndpoint)
	} else {
		fmt.Printf("   Using Standard OpenAI\n")
	}
	fmt.Println()

	// Create the service without database dependencies
	// We pass nil for repositories and dataExtractionService since we're only testing name variations
	service := services.NewOrgEvaluationService(cfg, nil, nil)

	// Test cases
	testCases := []struct {
		OrgName  string
		Websites []string
	}{
		{
			OrgName:  "No Makeup Makeup",
			Websites: []string{"https://nomakeupmakeup.com/"},
		},
	}

	ctx := context.Background()

	// Allow user to specify which test case to run via command line arg
	testIndex := -1 // -1 means run all
	if len(os.Args) > 1 {
		fmt.Sscanf(os.Args[1], "%d", &testIndex)
	}

	if testIndex >= 0 && testIndex < len(testCases) {
		// Run single test case
		runTest(ctx, service, testCases[testIndex], testIndex+1)
	} else {
		// Run all test cases
		for i, tc := range testCases {
			runTest(ctx, service, tc, i+1)
			if i < len(testCases)-1 {
				fmt.Println("\n" + strings.Repeat("-", 80) + "\n")
			}
		}
	}

	fmt.Println("\n=== Testing Complete ===")
}

func runTest(ctx context.Context, service services.OrgEvaluationService, tc struct {
	OrgName  string
	Websites []string
}, testNum int) {
	fmt.Printf("📝 Test Case #%d\n", testNum)
	fmt.Printf("   Organization: %s\n", tc.OrgName)
	fmt.Printf("   Websites: %v\n", tc.Websites)
	fmt.Println()

	// Call the service function
	variations, err := service.GenerateNameVariations(ctx, tc.OrgName, tc.Websites)
	if err != nil {
		log.Printf("❌ Error generating variations: %v\n", err)
		return
	}

	// Display results
	fmt.Printf("✅ Successfully generated %d name variations:\n", len(variations))
	for i, variation := range variations {
		fmt.Printf("   %2d. %s\n", i+1, variation)
	}
}
