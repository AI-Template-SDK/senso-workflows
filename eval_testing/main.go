// eval_testing/main.go
package main

import (
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

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
	ExpectedSOV       float64 // NEW: Added ExpectedSOV
}

// TestResult holds the outcome of a single test
type TestResult struct {
	Record          GoldenRecord
	Passed          bool
	MentionPassed   bool
	SOVPassed       bool
	SentimentPassed bool
	ActualMention   bool
	ActualSentiment string
	ActualSOV       float64
	Reason          string
	Error           error
}

func main() {
	// Define and parse command-line flags
	testType := flag.String("type", "baseline", "Type of test: 'baseline' or 'improvement'")
	modelFlag := flag.String("model", "", "Override Azure deployment name (e.g., gpt-4.1-mini, gpt-5)")
	sovTolerance := flag.Float64("sov-tolerance", 10.0, "Allowed % tolerance for SOV comparison")
	flag.Parse()

	// 1. Load Configuration
	if err := godotenv.Load("../.env"); err != nil {
		if err := godotenv.Load("../dev.env"); err != nil {
			fmt.Fprintln(os.Stderr, "Warning: No .env or dev.env file found in parent directory.")
		}
	}
	cfg := config.Load()

	// Override config with model flag if provided
	if *modelFlag != "" {
		cfg.AzureOpenAIDeploymentName = *modelFlag
		// Log this override *after* logger is set up
	}

	// 2. Set up Logging
	logDir := "logs"
	if err := os.MkdirAll(logDir, 0755); err != nil {
		log.Fatalf("Failed to create log directory: %v", err)
	}
	modelForLog := cfg.AzureOpenAIDeploymentName
	if modelForLog == "" {
		modelForLog = "default-model"
	}
	safeModelName := strings.ReplaceAll(modelForLog, ":", "-")
	timestamp := time.Now().Format("20060102_150405")
	logFileName := fmt.Sprintf("%s/%s_%s_%s.log", logDir, timestamp, *testType, safeModelName)
	logFile, err := os.OpenFile(logFileName, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("Failed to open log file: %v", err)
	}
	defer logFile.Close()
	writer := io.MultiWriter(os.Stdout, logFile)
	log.SetOutput(writer)

	// log initial settings
	log.Println("--- 🚀 Starting Evaluation Test Harness ---")
	log.Printf("Test Type: %s", *testType)
	log.Printf("SOV Tolerance: %.2f%%", *sovTolerance)
	log.Printf("Log file: %s", logFileName)
	if *modelFlag != "" {
		log.Printf("Overriding config. Using model from flag: %s", *modelFlag)
	}
	log.Printf("Config loaded. Using Azure Deployment: %s", cfg.AzureOpenAIDeploymentName)

	// 3. Initialize Services
	var repoManager *services.RepositoryManager = nil
	var dataExtractionService services.DataExtractionService = nil
	orgEvaluationService := services.NewOrgEvaluationService(cfg, repoManager, dataExtractionService)
	log.Println("OrgEvaluationService initialized.")

	// 4. Load Golden Data Set
	records, err := loadGoldenData("golden_data.csv")
	if err != nil {
		log.Fatalf("Failed to load golden_data.csv: %v", err)
	}
	log.Printf("Loaded %d test records from golden_data.csv\n", len(records))

	// 5. Run Tests
	results := []TestResult{}
	for _, record := range records {
		ctx := context.Background()
		log.Printf("--- Running Test for Org: '%s' ---", record.OrgName)
		// NEW: Pass sovTolerance to runTest
		result := runTest(ctx, orgEvaluationService, record, *sovTolerance)
		results = append(results, result)
	}

	// 6. Print Summary (NEW: Updated Summary Logic)
	log.Println("\n--- 📊 Test Harness Summary ---")
	overallPassedCount := 0
	mentionPassedCount := 0
	sovPassedCount := 0
	sentimentPassedCount := 0

	for _, res := range results {
		if res.Passed {
			overallPassedCount++
			log.Printf("✅ OVERALL PASS: [%s]", res.Record.OrgName)
		} else {
			log.Printf("❌ OVERALL FAIL: [%s] - Reason(s): %s (Error: %v)",
				res.Record.OrgName,
				res.Reason,
				res.Error,
			)
			// Log detailed comparison only on failure
			log.Printf("   -> Response: \"%.50s...\"", res.Record.ResponseText)
			if !res.MentionPassed {
				log.Printf("     -> Mention Mismatch: Expected %t, Got %t", res.Record.ExpectedMention, res.ActualMention)
			}
			if !res.SOVPassed {
				log.Printf("     -> SOV Mismatch (Tolerance %.2f%%): Expected %.2f%%, Got %.2f%%", *sovTolerance, res.Record.ExpectedSOV, res.ActualSOV)
			}
			if !res.SentimentPassed {
				// Handle empty expected sentiment when mention is false
				expectedSent := res.Record.ExpectedSentiment
				if !res.Record.ExpectedMention && expectedSent == "" {
					expectedSent = "(N/A)"
				}
				actualSent := res.ActualSentiment
				if !res.ActualMention && actualSent == "" {
					actualSent = "(N/A)"
				}
				log.Printf("     -> Sentiment Mismatch: Expected '%s', Got '%s'", expectedSent, actualSent)
			}
		}
		// Count individual metric passes regardless of overall pass/fail
		if res.MentionPassed {
			mentionPassedCount++
		}
		if res.SOVPassed {
			sovPassedCount++
		}
		if res.SentimentPassed {
			sentimentPassedCount++
		}
	}

	totalTests := float64(len(results))
	overallAccuracy := (float64(overallPassedCount) / totalTests) * 100
	mentionAccuracy := (float64(mentionPassedCount) / totalTests) * 100
	sovAccuracy := (float64(sovPassedCount) / totalTests) * 100
	sentimentAccuracy := (float64(sentimentPassedCount) / totalTests) * 100

	log.Printf("---")
	log.Printf("📈 Individual Metric Accuracy:")
	log.Printf("  - Mention Accuracy:   %.2f%% (%d/%d passed)", mentionAccuracy, mentionPassedCount, len(results))
	log.Printf("  - SOV Accuracy (±%.2f%%): %.2f%% (%d/%d passed)", *sovTolerance, sovAccuracy, sovPassedCount, len(results))
	log.Printf("  - Sentiment Accuracy: %.2f%% (%d/%d passed)", sentimentAccuracy, sentimentPassedCount, len(results))
	log.Printf("---")
	log.Printf("🎯 Overall Accuracy (All metrics must pass): %.2f%% (%d/%d passed)", overallAccuracy, overallPassedCount, len(results))
	log.Printf("---")
}

// runTest executes the "sieve" logic and calculates individual metrics
func runTest(ctx context.Context, orgEvalSvc services.OrgEvaluationService, record GoldenRecord, sovTolerance float64) TestResult {

	result := TestResult{Record: record} // Initialize result

	// --- Step 1: Generate Name Variations ---
	nameVariations, err := orgEvalSvc.GenerateNameVariations(ctx, record.OrgName, nil)
	if err != nil {
		result.Reason = "GenerateNameVariations failed"
		result.Error = err
		// Set default values for results on early exit
		result.MentionPassed = false
		result.SOVPassed = false
		result.SentimentPassed = false
		result.ActualMention = false
		result.ActualSOV = 0.0
		result.ActualSentiment = ""
		result.Passed = false
		return result
	}
	log.Printf("[Test: %s] Generated %d name variations.", record.OrgName, len(nameVariations))

	// --- Step 2: Run Pre-filter (Loose Sieve) ---
	preFilterMentioned := false
	responseTextLower := strings.ToLower(record.ResponseText)
	for _, name := range nameVariations {
		// Basic check
		if strings.Contains(responseTextLower, strings.ToLower(name)) {
			preFilterMentioned = true
			break
		}
	}

	// Initialize actual results
	var mentionTextPtr *string = nil // Store the extracted mention text pointer

	if preFilterMentioned {
		log.Printf("[Test: %s] Pre-filter PASSED. Running LLM extraction...", record.OrgName)
		dummyUUID := uuid.New()
		evalResult, err := orgEvalSvc.ExtractOrgEvaluation(ctx, dummyUUID, dummyUUID, record.OrgName, nil, nameVariations, record.ResponseText)
		if err != nil {
			result.Reason = "ExtractOrgEvaluation failed"
			result.Error = err
			// Set default values for results on early exit
			result.MentionPassed = false
			result.SOVPassed = false
			result.SentimentPassed = false
			result.ActualMention = false // Assume false if extraction fails
			result.ActualSOV = 0.0
			result.ActualSentiment = ""
			result.Passed = false
			return result
		}

		// Check if the LLM returned non-empty mention text
		if evalResult.Evaluation != nil && evalResult.Evaluation.MentionText != nil && *evalResult.Evaluation.MentionText != "" {
			result.ActualMention = true
			mentionTextPtr = evalResult.Evaluation.MentionText // Store pointer
			if evalResult.Evaluation.Sentiment != nil {
				result.ActualSentiment = *evalResult.Evaluation.Sentiment
			} else {
				result.ActualSentiment = "" // Ensure empty string if sentiment is nil
			}
			log.Printf("[Test: %s] LLM extraction returned text. Mention: true, Sentiment: '%s'", record.OrgName, result.ActualSentiment)
		} else {
			result.ActualMention = false
			result.ActualSentiment = ""
			log.Printf("[Test: %s] LLM extraction returned EMPTY text. Setting Mention: false.", record.OrgName)
		}

	} else {
		log.Printf("[Test: %s] Pre-filter FAILED. Skipping LLM extraction. Mention: false.", record.OrgName)
		result.ActualMention = false
		result.ActualSentiment = ""
	}

	// --- Step 3: Calculate Actual SOV ---
	responseTextLen := len(record.ResponseText)
	if result.ActualMention && responseTextLen > 0 && mentionTextPtr != nil {
		mentionTextLen := len(*mentionTextPtr)
		result.ActualSOV = (float64(mentionTextLen) / float64(responseTextLen)) * 100.0
		log.Printf("[Test: %s] Calculated SOV: %.2f%% (MentionLen: %d / ResponseLen: %d)", record.OrgName, result.ActualSOV, mentionTextLen, responseTextLen)
	} else {
		result.ActualSOV = 0.0 // Set to 0 if not mentioned or response text is empty
		if result.ActualMention {
			log.Printf("[Test: %s] Calculated SOV: 0.0%% (Response length is zero or mention text missing)", record.OrgName)
		} else {
			log.Printf("[Test: %s] Calculated SOV: 0.0%% (Not mentioned)", record.OrgName)
		}
	}

	// --- Step 4: Compare Individual Metrics ---
	var reasons []string

	// Mention Check
	result.MentionPassed = (result.ActualMention == record.ExpectedMention)
	if !result.MentionPassed {
		reasons = append(reasons, fmt.Sprintf("Mention mismatch (Expected %t, Got %t)", record.ExpectedMention, result.ActualMention))
	}

	// SOV Check (only if mention was expected or actually happened)
	// Apply tolerance check
	sovDiff := math.Abs(result.ActualSOV - record.ExpectedSOV)
	result.SOVPassed = (sovDiff <= sovTolerance)
	// Edge case: If mention=false, SOV should be 0. If actual SOV is not 0, it's a fail even within tolerance.
	if !record.ExpectedMention && result.ActualSOV != 0.0 {
		result.SOVPassed = false
	}
	if !result.SOVPassed {
		reasons = append(reasons, fmt.Sprintf("SOV mismatch (Expected %.2f%%, Got %.2f%%, Diff %.2f%% > Tol %.2f%%)", record.ExpectedSOV, result.ActualSOV, sovDiff, sovTolerance))
	}

	// Sentiment Check (only relevant if mention is true for both expected and actual)
	// If ExpectedMention is false, ExpectedSentiment should be "", and ActualSentiment should also be "" if ActualMention is false.
	expectedSentimentForCheck := record.ExpectedSentiment
	if !record.ExpectedMention {
		expectedSentimentForCheck = "" // If not expected to be mentioned, sentiment is irrelevant/empty
	}
	actualSentimentForCheck := result.ActualSentiment
	if !result.ActualMention {
		actualSentimentForCheck = "" // If not actually mentioned, sentiment should be empty
	}
	result.SentimentPassed = (actualSentimentForCheck == expectedSentimentForCheck)
	if !result.SentimentPassed {
		reasons = append(reasons, fmt.Sprintf("Sentiment mismatch (Expected '%s', Got '%s')", expectedSentimentForCheck, actualSentimentForCheck))
	}

	// Determine Overall Pass/Fail
	result.Passed = result.MentionPassed && result.SOVPassed && result.SentimentPassed
	result.Reason = strings.Join(reasons, "; ")

	return result
}

// loadGoldenData reads and parses the CSV file including the new SOV column
func loadGoldenData(path string) ([]GoldenRecord, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open file %s: %w", path, err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	// NEW: Check for header and field count consistency
	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("failed to read header row: %w", err)
	}
	expectedHeaders := []string{"org_name", "response_text", "expected_mention", "expected_sentiment", "expected_sov"}
	if len(header) != len(expectedHeaders) {
		return nil, fmt.Errorf("incorrect number of columns: expected %d, got %d. Headers: %v", len(expectedHeaders), len(header), header)
	}
	// Optional: Check header names
	// for i, h := range expectedHeaders {
	// 	if strings.TrimSpace(header[i]) != h {
	// 		return nil, fmt.Errorf("incorrect header in column %d: expected '%s', got '%s'", i+1, h, header[i])
	// 	}
	// }

	rows, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("failed to read data rows: %w", err)
	}

	var records []GoldenRecord
	for i, row := range rows {
		if len(row) != len(expectedHeaders) {
			log.Printf("Warning: Skipping row %d due to incorrect column count (%d instead of %d)", i+2, len(row), len(expectedHeaders))
			continue
		}

		expectedMention, err := strconv.ParseBool(strings.TrimSpace(row[2]))
		if err != nil {
			log.Printf("Warning: Skipping row %d due to invalid boolean in expected_mention '%s': %v", i+2, row[2], err)
			continue
		}

		// NEW: Parse ExpectedSOV (column index 4)
		expectedSOV, err := strconv.ParseFloat(strings.TrimSpace(row[4]), 64)
		if err != nil {
			// If mention is false, SOV *must* be 0. Allow parsing error only in this case and default to 0.
			if expectedMention == false {
				log.Printf("Warning: Row %d expected_mention is false, defaulting expected_sov to 0.0 despite parsing error ('%s'): %v", i+2, row[4], err)
				expectedSOV = 0.0
			} else {
				log.Printf("Warning: Skipping row %d due to invalid float in expected_sov '%s': %v", i+2, row[4], err)
				continue
			}
		}
		// Ensure SOV is 0 if mention is false
		if !expectedMention && expectedSOV != 0.0 {
			log.Printf("Warning: Row %d expected_mention is false, but expected_sov is non-zero (%.2f). Setting expected_sov to 0.0.", i+2, expectedSOV)
			expectedSOV = 0.0
		}

		records = append(records, GoldenRecord{
			OrgName:           strings.TrimSpace(row[0]),
			ResponseText:      strings.TrimSpace(row[1]),
			ExpectedMention:   expectedMention,
			ExpectedSentiment: strings.TrimSpace(row[3]),
			ExpectedSOV:       expectedSOV, // Assign parsed SOV
		})
	}
	if len(records) == 0 && len(rows) > 0 {
		return nil, fmt.Errorf("no valid records found after parsing %d rows", len(rows))
	}
	return records, nil
}
