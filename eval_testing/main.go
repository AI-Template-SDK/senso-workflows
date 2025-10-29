// eval_testing/main.go
package main

import (
	"context"
	"encoding/csv"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/AI-Template-SDK/senso-workflows/internal/config"
	"github.com/AI-Template-SDK/senso-workflows/services"
	"github.com/google/uuid"
	"github.com/joho/godotenv"
)

// GoldenRecord holds one test case from the CSV
type GoldenRecord struct {
	OrgName           string
	ResponseText      string
	ExpectedMention   bool
	ExpectedSentiment string
}

// TestResult holds the outcome of a single test
type TestResult struct {
	Record          GoldenRecord
	Passed          bool
	ActualMention   bool
	ActualSentiment string
	Reason          string
	Error           error
}

func main() {
	log.Println("--- Starting Evaluation Test Harness ---")

	// 1. Load Configuration (from root .env)
	// We are in eval_testing, so we go up one level
	if err := godotenv.Load("../.env"); err != nil {
		if err := godotenv.Load("../dev.env"); err != nil {
			log.Printf("Warning: No .env or dev.env file found in parent directory: %v", err)
		} else {
			log.Printf("Loaded ../dev.env file")
		}
	} else {
		log.Printf("Loaded ../.env file")
	}
	cfg := config.Load()
	log.Printf("Config loaded. Using Azure Deployment: %s", cfg.AzureOpenAIDeploymentName)

	// 2. Initialize ONLY the services we need
	// We don't need a real DB connection, so repoManager is nil.
	// This works because GenerateNameVariations and ExtractOrgEvaluation
	// only use the cfg and openAIClient, not the database repos.
	var repoManager *services.RepositoryManager = nil
	var dataExtractionService services.DataExtractionService = nil // Not used by the functions we're testing

	orgEvaluationService := services.NewOrgEvaluationService(cfg, repoManager, dataExtractionService)
	log.Println("OrgEvaluationService initialized.")

	// 3. Load Golden Data Set
	records, err := loadGoldenData("golden_data.csv")
	if err != nil {
		log.Fatalf("Failed to load golden_data.csv: %v", err)
	}
	log.Printf("Loaded %d test records from golden_data.csv\n", len(records))

	// 4. Run Tests
	results := []TestResult{}
	for _, record := range records {
		ctx := context.Background()
		log.Printf("--- Running Test for Org: '%s' ---", record.OrgName)
		result := runTest(ctx, orgEvaluationService, record)
		results = append(results, result)
	}

	// 5. Print Summary
	log.Println("--- Test Harness Summary ---")
	passedCount := 0
	for _, res := range results {
		if res.Passed {
			passedCount++
			log.Printf("✅ PASS: [%s]", res.Record.OrgName)
		} else {
			log.Printf(
				"❌ FAIL: [%s] - Reason: %s (Error: %v)",
				res.Record.OrgName,
				res.Reason,
				res.Error,
			)
			log.Printf("   -> Response: \"%.50s...\"", res.Record.ResponseText)
			log.Printf("   -> Expected Mention: %t, Got: %t", res.Record.ExpectedMention, res.ActualMention)
			log.Printf("   -> Expected Sentiment: '%s', Got: '%s'", res.Record.ExpectedSentiment, res.ActualSentiment)
		}
	}
	accuracy := (float64(passedCount) / float64(len(results))) * 100
	log.Printf("--- \nOverall Accuracy: %.2f%% (%d/%d passed) ---", accuracy, passedCount, len(results))
}

// runTest executes the "sieve" logic Tom described [cite: 70, 71]
func runTest(ctx context.Context, orgEvalSvc services.OrgEvaluationService, record GoldenRecord) TestResult {
	// --- Step 1: Generate Name Variations ---
	// This is the first part of the check
	nameVariations, err := orgEvalSvc.GenerateNameVariations(ctx, record.OrgName, nil) // Pass nil for websites
	if err != nil {
		return TestResult{Record: record, Passed: false, Reason: "GenerateNameVariations failed", Error: err}
	}
	log.Printf("[Test: %s] Generated %d name variations.", record.OrgName, len(nameVariations))

	// --- Step 2: Run Pre-filter (Loose Sieve) ---
	// This replicates the logic in processQuestionRunWithOrgEvaluation
	preFilterMentioned := false
	responseTextLower := strings.ToLower(record.ResponseText)
	for _, name := range nameVariations {
		if strings.Contains(responseTextLower, strings.ToLower(name)) {
			preFilterMentioned = true
			break
		}
	}

	var actualMention bool = false
	var actualSentiment string = ""

	// --- Step 3: Run LLM Extraction (Tight Sieve) ---
	// Only run if the pre-filter passes [cite: 43]
	if preFilterMentioned {
		log.Printf("[Test: %s] Pre-filter PASSED. Running LLM extraction...", record.OrgName)
		// We need dummy IDs for the service function signature
		dummyUUID := uuid.New()
		evalResult, err := orgEvalSvc.ExtractOrgEvaluation(ctx, dummyUUID, dummyUUID, record.OrgName, nil, nameVariations, record.ResponseText)
		if err != nil {
			return TestResult{Record: record, Passed: false, Reason: "ExtractOrgEvaluation failed", Error: err}
		}

		// --- THIS IS THE NEW LOGIC TOM REQUESTED ---
		// Check if the LLM returned empty text, and if so, set mention to false
		if evalResult.Evaluation.MentionText != nil && *evalResult.Evaluation.MentionText != "" {
			actualMention = true
			if evalResult.Evaluation.Sentiment != nil {
				actualSentiment = *evalResult.Evaluation.Sentiment
			}
			log.Printf("[Test: %s] LLM extraction returned text. Mention: true, Sentiment: '%s'", record.OrgName, actualSentiment)
		} else {
			// This is the fix! If the LLM returns no text, it's not a real mention.
			actualMention = false
			actualSentiment = ""
			log.Printf("[Test: %s] LLM extraction returned EMPTY text. Setting Mention: false.", record.OrgName)
		}

	} else {
		log.Printf("[Test: %s] Pre-filter FAILED. Skipping LLM extraction. Mention: false.", record.OrgName)
		actualMention = false
		actualSentiment = ""
	}

	// --- Step 4: Compare Results ---
	passed := (actualMention == record.ExpectedMention) && (actualSentiment == record.ExpectedSentiment)
	reason := ""
	if !passed {
		if actualMention != record.ExpectedMention {
			reason = fmt.Sprintf("Mention mismatch: expected %t, got %t", record.ExpectedMention, actualMention)
		} else {
			reason = fmt.Sprintf("Sentiment mismatch: expected '%s', got '%s'", record.ExpectedSentiment, actualSentiment)
		}
	}
	return TestResult{Record: record, Passed: passed, ActualMention: actualMention, ActualSentiment: actualSentiment, Reason: reason}
}

// loadGoldenData reads and parses the CSV file
func loadGoldenData(path string) ([]GoldenRecord, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader := csv.NewReader(file)
	rows, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}

	var records []GoldenRecord
	// Skip header row (i=0)
	for i := 1; i < len(rows); i++ {
		row := rows[i]
		expectedMention, _ := strconv.ParseBool(row[2])

		records = append(records, GoldenRecord{
			OrgName:           row[0],
			ResponseText:      row[1],
			ExpectedMention:   expectedMention,
			ExpectedSentiment: row[3],
		})
	}
	return records, nil
}
