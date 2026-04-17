// cmd/org_eval_runner/main.go
//
// Standalone org evaluation pipeline runner.
// Replaces the Inngest-based orchestration with direct Go concurrency.
//
// Usage:
//
//	go run cmd/org_eval_runner/main.go --orgs-file example_orgs.txt
//	go run cmd/org_eval_runner/main.go --single-org <uuid>
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"

	"github.com/AI-Template-SDK/senso-api/pkg/database"
	"github.com/AI-Template-SDK/senso-workflows/internal/config"
	"github.com/AI-Template-SDK/senso-workflows/services"
	"github.com/AI-Template-SDK/senso-workflows/workflows"
	"github.com/google/uuid"
)

// CLI flags
var (
	orgsFile   = flag.String("orgs-file", "example_orgs.txt", "Path to file with org IDs (one per line)")
	orgWorkers = flag.Int("org-workers", 20, "Max concurrent org pipelines")
	dryRun     = flag.Bool("dry-run", false, "Print org count and exit without processing")
	singleOrg  = flag.String("single-org", "", "Process a single org ID (ignores --orgs-file)")
	dbMaxConns = flag.Int("db-max-conns", 100, "Max open database connections")
)

// OrgResult captures the outcome of processing a single org.
type OrgResult struct {
	OrgID       string
	OrgName     string
	BatchID     string
	Success     bool
	TotalJobs   int
	Completed   int
	Failed      int
	Evaluations int
	Citations   int
	Competitors int
	Cost        float64
	Duration    time.Duration
	Error       string
}

// PipelineReport aggregates results across all orgs.
type PipelineReport struct {
	mu      sync.Mutex
	Results []OrgResult
}

func (r *PipelineReport) Add(result OrgResult) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Results = append(r.Results, result)
}

func main() {
	flag.Parse()

	// Load .env
	if err := godotenv.Load(); err != nil {
		if err := godotenv.Load("dev.env"); err != nil {
			log.Printf("Note: No .env or dev.env file loaded")
		}
	}

	cfg := config.Load()

	// Read org IDs
	orgIDs, err := readOrgIDs()
	if err != nil {
		log.Fatalf("Failed to read org IDs: %v", err)
	}

	log.Printf("============================================")
	log.Printf("  ORG EVALUATION PIPELINE — STANDALONE RUNNER")
	log.Printf("============================================")
	log.Printf("Orgs to process:    %d", len(orgIDs))
	log.Printf("Org workers:        %d", *orgWorkers)
	log.Printf("DB max connections: %d", *dbMaxConns)
	log.Printf("============================================")

	if *dryRun {
		log.Printf("DRY RUN — exiting without processing")
		for i, id := range orgIDs {
			fmt.Printf("  %d. %s\n", i+1, id)
		}
		return
	}

	// Setup context with graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		log.Printf("\nReceived %v — shutting down gracefully (in-flight orgs will finish)...", sig)
		cancel()
	}()

	// Connect to database with higher connection pool
	cfg.Database.MaxOpenConns = *dbMaxConns
	cfg.Database.MaxIdleConns = *dbMaxConns / 2
	dbClient, err := createDatabaseClient(ctx, cfg.Database)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer dbClient.Close()
	log.Printf("Database connected (max conns: %d)", *dbMaxConns)

	// Initialize services
	repoManager := services.NewRepositoryManager(dbClient)
	orgService := services.NewOrgService(cfg, repoManager)
	dataExtractionService := services.NewDataExtractionService(cfg)
	orgEvalService := services.NewOrgEvaluationService(cfg, repoManager, dataExtractionService)
	usageService := services.NewUsageService(repoManager)
	log.Printf("Services initialized")

	// Run pipeline
	startTime := time.Now()
	report := runPipeline(ctx, orgIDs, orgService, orgEvalService, usageService)
	totalDuration := time.Since(startTime)

	// Print report
	printReport(report, totalDuration)
}

// readOrgIDs reads org IDs from file or single flag.
func readOrgIDs() ([]string, error) {
	if *singleOrg != "" {
		// Validate UUID
		if _, err := uuid.Parse(*singleOrg); err != nil {
			return nil, fmt.Errorf("invalid org ID %q: %w", *singleOrg, err)
		}
		return []string{*singleOrg}, nil
	}

	file, err := os.Open(*orgsFile)
	if err != nil {
		return nil, fmt.Errorf("failed to open %s: %w", *orgsFile, err)
	}
	defer file.Close()

	var ids []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if _, err := uuid.Parse(line); err != nil {
			log.Printf("Warning: skipping invalid UUID: %s", line)
			continue
		}
		ids = append(ids, line)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading file: %w", err)
	}

	if len(ids) == 0 {
		return nil, fmt.Errorf("no valid org IDs found in %s", *orgsFile)
	}

	return ids, nil
}

// runPipeline processes all orgs with bounded concurrency.
func runPipeline(
	ctx context.Context,
	orgIDs []string,
	orgService services.OrgService,
	orgEvalService services.OrgEvaluationService,
	usageService services.UsageService,
) *PipelineReport {
	report := &PipelineReport{}
	sem := make(chan struct{}, *orgWorkers)
	var wg sync.WaitGroup

	var completedOrgs int64
	totalOrgs := len(orgIDs)

	for _, orgID := range orgIDs {
		// Check for cancellation before starting new org
		select {
		case <-ctx.Done():
			log.Printf("Context cancelled — skipping remaining orgs")
			goto done
		default:
		}

		wg.Add(1)
		sem <- struct{}{} // acquire worker slot

		go func(id string) {
			defer wg.Done()
			defer func() { <-sem }() // release slot

			result := processOrg(ctx, id, orgService, orgEvalService, usageService)
			report.Add(result)

			n := atomic.AddInt64(&completedOrgs, 1)
			status := "OK"
			if !result.Success {
				status = "FAIL"
			}
			log.Printf("[%d/%d] %s org=%s name=%q batch=%s jobs=%d/%d evals=%d cost=$%.4f dur=%s err=%s",
				n, totalOrgs, status, result.OrgID, result.OrgName, result.BatchID,
				result.Completed, result.TotalJobs, result.Evaluations,
				result.Cost, result.Duration.Round(time.Second), result.Error)
		}(orgID)
	}

done:
	wg.Wait()
	return report
}

// processOrg runs the full evaluation pipeline for a single org.
// This mirrors the Inngest workflow steps 1-6 but runs inline.
func processOrg(
	ctx context.Context,
	orgID string,
	orgService services.OrgService,
	orgEvalService services.OrgEvaluationService,
	usageService services.UsageService,
) OrgResult {
	start := time.Now()
	result := OrgResult{OrgID: orgID}

	defer func() {
		result.Duration = time.Since(start)
		if r := recover(); r != nil {
			result.Success = false
			result.Error = fmt.Sprintf("panic: %v", r)
		}
	}()

	// Check context
	if ctx.Err() != nil {
		result.Error = "cancelled"
		return result
	}

	// Step 1: Get org details
	orgDetails, err := orgService.GetOrgDetails(ctx, orgID)
	if err != nil {
		result.Error = fmt.Sprintf("get org details: %v", err)
		reportSlack(orgID, "", "get org details", err)
		return result
	}
	result.OrgName = orgDetails.Org.Name

	orgUUID, _ := uuid.Parse(orgID)
	totalQuestions := len(orgDetails.Questions) * len(orgDetails.Models) * len(orgDetails.Locations)
	result.TotalJobs = totalQuestions

	if totalQuestions == 0 {
		result.Success = true
		result.Error = "no questions to process"
		return result
	}

	// Step 2: Get or create batch
	batch, isExisting, err := orgEvalService.GetOrCreateTodaysBatch(ctx, orgUUID, totalQuestions)
	if err != nil {
		result.Error = fmt.Sprintf("get/create batch: %v", err)
		reportSlack(orgID, result.OrgName, "get/create batch", err)
		return result
	}
	result.BatchID = batch.BatchID.String()

	// If batch is fully completed, skip everything.
	// If completed but with missing jobs (CompletedQuestions < TotalQuestions), re-enter the pipeline to retry.
	if batch.Status == "completed" && batch.CompletedQuestions >= batch.TotalQuestions {
		log.Printf("[%s] Batch %s already completed (%d/%d) — skipping", orgID[:8], result.BatchID[:8], batch.CompletedQuestions, batch.TotalQuestions)
		result.Success = true
		result.Completed = batch.CompletedQuestions
		return result
	}
	if batch.Status == "completed" && batch.CompletedQuestions < batch.TotalQuestions {
		log.Printf("[%s] Batch %s completed but incomplete (%d/%d) — retrying missing jobs", orgID[:8], result.BatchID[:8], batch.CompletedQuestions, batch.TotalQuestions)
		// Reset batch to running so we can process missing jobs
		if err := orgEvalService.StartBatch(ctx, batch.BatchID); err != nil {
			log.Printf("[%s] Warning: failed to reset batch to running: %v", orgID[:8], err)
		}
		isExisting = true // treat as resumed
	}

	// Step 3: Check balance
	_, err = usageService.CheckBalance(ctx, orgUUID, totalQuestions, "org")
	if err != nil {
		_ = orgEvalService.FailBatch(ctx, batch.BatchID)
		result.Error = fmt.Sprintf("insufficient balance: %v", err)
		reportSlack(orgID, result.OrgName, "balance check", err)
		return result
	}

	// Step 4: Start batch (only if new)
	if !isExisting {
		if err := orgEvalService.StartBatch(ctx, batch.BatchID); err != nil {
			result.Error = fmt.Sprintf("start batch: %v", err)
			reportSlack(orgID, result.OrgName, "start batch", err)
			return result
		}
	}

	// Step 5: Run full question matrix (Phase 1: name variations, Phase 2: batched question execution, Phase 3: extractions)
	// This handles dedup internally — skips existing question runs and extractions.
	summary, err := orgEvalService.RunQuestionMatrixWithOrgEvaluation(ctx, orgDetails, batch.BatchID)
	if err != nil {
		_ = orgEvalService.FailBatch(ctx, batch.BatchID)
		result.Error = fmt.Sprintf("question matrix: %v", err)
		reportSlack(orgID, result.OrgName, "question matrix", err)
		return result
	}

	// Map summary to result
	result.Completed = summary.TotalProcessed
	result.Failed = len(summary.ProcessingErrors)
	result.Evaluations = summary.TotalEvaluations
	result.Citations = summary.TotalCitations
	result.Competitors = summary.TotalCompetitors
	result.Cost = summary.TotalCost

	// Step 6: Track usage
	_, err = usageService.TrackBatchUsage(ctx, orgUUID, batch.BatchID, "org")
	if err != nil {
		log.Printf("[%s] Warning: usage tracking failed: %v", orgID[:8], err)
	}

	// Step 7: Complete batch
	if err := orgEvalService.CompleteBatch(ctx, batch.BatchID); err != nil {
		result.Error = fmt.Sprintf("complete batch: %v", err)
		reportSlack(orgID, result.OrgName, "complete batch", err)
		return result
	}

	result.Success = true
	return result
}

// reportSlack is a best-effort Slack notification on failure.
func reportSlack(orgID, orgName, step string, err error) {
	if reportErr := workflows.ReportPipelineFailureToSlack("org eval runner", orgID, orgName, step, err); reportErr != nil {
		log.Printf("Warning: Slack report failed: %v", reportErr)
	}
}

// printReport outputs the final pipeline summary.
func printReport(report *PipelineReport, totalDuration time.Duration) {
	report.mu.Lock()
	defer report.mu.Unlock()

	var totalOrgs, successOrgs, failedOrgs int
	var totalJobs, totalCompleted, totalFailed int
	var totalEvals, totalCitations, totalCompetitors int
	var totalCost float64

	for _, r := range report.Results {
		totalOrgs++
		if r.Success {
			successOrgs++
		} else {
			failedOrgs++
		}
		totalJobs += r.TotalJobs
		totalCompleted += r.Completed
		totalFailed += r.Failed
		totalEvals += r.Evaluations
		totalCitations += r.Citations
		totalCompetitors += r.Competitors
		totalCost += r.Cost
	}

	fmt.Println()
	fmt.Println("============================================")
	fmt.Println("           PIPELINE COMPLETE")
	fmt.Println("============================================")
	fmt.Printf("  Total orgs:        %d\n", totalOrgs)
	fmt.Printf("  Successful:        %d\n", successOrgs)
	fmt.Printf("  Failed:            %d\n", failedOrgs)
	fmt.Printf("  Total jobs:        %d\n", totalJobs)
	fmt.Printf("  Completed:         %d\n", totalCompleted)
	fmt.Printf("  Failed jobs:       %d\n", totalFailed)
	fmt.Printf("  Evaluations:       %d\n", totalEvals)
	fmt.Printf("  Citations:         %d\n", totalCitations)
	fmt.Printf("  Competitors:       %d\n", totalCompetitors)
	fmt.Printf("  Total cost:        $%.4f\n", totalCost)
	fmt.Printf("  Duration:          %s\n", totalDuration.Round(time.Second))
	fmt.Println("============================================")

	// Print failed orgs
	if failedOrgs > 0 {
		fmt.Println()
		fmt.Println("FAILED ORGS:")
		for _, r := range report.Results {
			if !r.Success {
				fmt.Printf("  - %s (%s): %s\n", r.OrgID, r.OrgName, r.Error)
			}
		}
	}
}

// createDatabaseClient creates a database connection (same pattern as main.go and other cmd/ tools).
func createDatabaseClient(ctx context.Context, cfg config.DatabaseConfig) (*database.Client, error) {
	connStr := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		cfg.Host, cfg.Port, cfg.User, cfg.Password, cfg.Name, cfg.SSLMode,
	)

	db, err := sqlx.ConnectContext(ctx, "postgres", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	db.SetConnMaxLifetime(time.Duration(cfg.ConnMaxLifetime) * time.Second)

	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &database.Client{DB: db}, nil
}
