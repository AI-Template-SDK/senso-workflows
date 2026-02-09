package main

import (
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"

	"github.com/AI-Template-SDK/senso-api/pkg/database"
	"github.com/AI-Template-SDK/senso-api/pkg/models"
	"github.com/AI-Template-SDK/senso-workflows/internal/config"
	"github.com/AI-Template-SDK/senso-workflows/services"
	"github.com/google/uuid"
)

// Standalone one-off tool: intentionally duplicates DB bootstrapping from main.go
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

type missingEvalRow struct {
	orgID string
	runID uuid.UUID
}

func readMissingEvalCSV(path string) ([]missingEvalRow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	reader := csv.NewReader(f)
	reader.TrimLeadingSpace = true
	records, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}

	rows := make([]missingEvalRow, 0, len(records))
	for idx, rec := range records {
		if len(rec) < 2 {
			continue
		}

		orgID := strings.TrimSpace(rec[0])
		runIDStr := strings.TrimSpace(rec[1])
		if idx == 0 && strings.EqualFold(orgID, "org_id") {
			continue
		}
		if orgID == "" || runIDStr == "" {
			continue
		}

		runID, err := uuid.Parse(runIDStr)
		if err != nil {
			return nil, fmt.Errorf("invalid question_run_id %q on line %d: %w", runIDStr, idx+1, err)
		}

		rows = append(rows, missingEvalRow{
			orgID: orgID,
			runID: runID,
		})
	}

	return rows, nil
}

type orgContext struct {
	orgUUID        uuid.UUID
	orgName        string
	websites       []string
	nameVariations []string
}

type orgContextCall struct {
	done chan struct{}
	ctx  *orgContext
	err  error
}

type orgContextCache struct {
	mu       sync.Mutex
	data     map[string]*orgContext
	inflight map[string]*orgContextCall
}

func newOrgContextCache() *orgContextCache {
	return &orgContextCache{
		data:     make(map[string]*orgContext),
		inflight: make(map[string]*orgContextCall),
	}
}

func (c *orgContextCache) get(ctx context.Context, orgID string, loader func() (*orgContext, error)) (*orgContext, error) {
	c.mu.Lock()
	if cached, ok := c.data[orgID]; ok {
		c.mu.Unlock()
		return cached, nil
	}
	if call, ok := c.inflight[orgID]; ok {
		c.mu.Unlock()
		<-call.done
		return call.ctx, call.err
	}
	call := &orgContextCall{done: make(chan struct{})}
	c.inflight[orgID] = call
	c.mu.Unlock()

	loaded, err := loader()

	c.mu.Lock()
	if err == nil {
		c.data[orgID] = loaded
	}
	call.ctx = loaded
	call.err = err
	close(call.done)
	delete(c.inflight, orgID)
	c.mu.Unlock()

	return loaded, err
}

type evalJob struct {
	row missingEvalRow
	run *models.QuestionRun
}

type evalResult struct {
	processed          int
	created            int
	skippedExisting    int
	missingRuns        int
	emptyResponses     int
	failedResponses    int
	createdCompetitors int
	createdCitations   int
	skippedCompetitors int
	skippedCitations   int
	errors             int
}

func processJob(
	ctx context.Context,
	job evalJob,
	dryRun bool,
	repos *services.RepositoryManager,
	orgService services.OrgService,
	orgEvaluationService services.OrgEvaluationService,
	cache *orgContextCache,
) evalResult {
	res := evalResult{processed: 1}

	if ctx.Err() != nil {
		res.errors++
		return res
	}

	if job.run == nil {
		log.Printf("[fix_missing_org_evals] org=%s run=%s missing in DB", job.row.orgID, job.row.runID)
		res.missingRuns++
		return res
	}

	orgUUID, err := uuid.Parse(job.row.orgID)
	if err != nil {
		log.Printf("[fix_missing_org_evals] org=%s invalid uuid: %v", job.row.orgID, err)
		res.errors++
		return res
	}

	evals, err := repos.OrgEvalRepo.GetByQuestionRunAndOrg(ctx, job.row.runID, orgUUID)
	if err != nil {
		log.Printf("[fix_missing_org_evals] org=%s run=%s ERROR checking evals: %v", job.row.orgID, job.row.runID, err)
		res.errors++
		return res
	}
	hasEval := len(evals) > 0
	if hasEval {
		res.skippedExisting++
	}

	if job.run.ResponseText == nil || strings.TrimSpace(*job.run.ResponseText) == "" {
		log.Printf("[fix_missing_org_evals] org=%s run=%s missing response text", job.row.orgID, job.row.runID)
		res.emptyResponses++
		return res
	}

	responseText := *job.run.ResponseText
	isFailedPlaceholder := responseText == "This prompt didn’t complete successfully due to a temporary AI model limitation. You were not charged for this prompt. We'll re-try in the next run."

	if dryRun {
		if !hasEval {
			action := "would_create_eval"
			if isFailedPlaceholder {
				action = "would_create_minimal_eval_for_failed_run"
			}
			log.Printf("[fix_missing_org_evals] org=%s run=%s %s", job.row.orgID, job.row.runID, action)
			res.created++
		} else {
			log.Printf("[fix_missing_org_evals] org=%s run=%s eval_exists_skip", job.row.orgID, job.row.runID)
		}
		if !isFailedPlaceholder {
			log.Printf("[fix_missing_org_evals] org=%s run=%s would_extract_competitors_and_citations", job.row.orgID, job.row.runID)
		}
		return res
	}

	if !hasEval {
		if isFailedPlaceholder {
			now := time.Now()
			orgEval := &models.OrgEval{
				OrgEvalID:     uuid.New(),
				QuestionRunID: job.row.runID,
				OrgID:         orgUUID,
				Mentioned:     false,
				Citation:      false,
				CreatedAt:     now,
				UpdatedAt:     now,
			}
			if err := repos.OrgEvalRepo.Create(ctx, orgEval); err != nil {
				log.Printf("[fix_missing_org_evals] org=%s run=%s ERROR create minimal eval: %v", job.row.orgID, job.row.runID, err)
				res.errors++
				return res
			}
			res.failedResponses++
			res.created++
		} else {
			orgCtx, err := cache.get(ctx, job.row.orgID, func() (*orgContext, error) {
				orgDetails, err := orgService.GetOrgDetails(ctx, job.row.orgID)
				if err != nil {
					return nil, fmt.Errorf("get org details: %w", err)
				}
				nameVariations, err := orgEvaluationService.GenerateNameVariations(ctx, orgDetails.Org.Name, orgDetails.Websites)
				if err != nil {
					return nil, fmt.Errorf("generate name variations: %w", err)
				}
				return &orgContext{
					orgUUID:        orgUUID,
					orgName:        orgDetails.Org.Name,
					websites:       orgDetails.Websites,
					nameVariations: nameVariations,
				}, nil
			})
			if err != nil {
				log.Printf("[fix_missing_org_evals] org=%s run=%s ERROR loading org context: %v", job.row.orgID, job.row.runID, err)
				res.errors++
				return res
			}

			mentioned := false
			responseLower := strings.ToLower(responseText)
			for _, name := range orgCtx.nameVariations {
				if strings.Contains(responseLower, strings.ToLower(name)) {
					mentioned = true
					break
				}
			}

			if mentioned {
				evalResult, err := orgEvaluationService.ExtractOrgEvaluation(ctx, job.row.runID, orgCtx.orgUUID, orgCtx.orgName, orgCtx.websites, orgCtx.nameVariations, responseText)
				if err != nil {
					log.Printf("[fix_missing_org_evals] org=%s run=%s ERROR extract org eval: %v", job.row.orgID, job.row.runID, err)
					res.errors++
					return res
				}
				if err := repos.OrgEvalRepo.Create(ctx, evalResult.Evaluation); err != nil {
					log.Printf("[fix_missing_org_evals] org=%s run=%s ERROR store org eval: %v", job.row.orgID, job.row.runID, err)
					res.errors++
					return res
				}
			} else {
				now := time.Now()
				orgEval := &models.OrgEval{
					OrgEvalID:     uuid.New(),
					QuestionRunID: job.row.runID,
					OrgID:         orgUUID,
					Mentioned:     false,
					Citation:      false,
					CreatedAt:     now,
					UpdatedAt:     now,
				}
				if err := repos.OrgEvalRepo.Create(ctx, orgEval); err != nil {
					log.Printf("[fix_missing_org_evals] org=%s run=%s ERROR store minimal eval: %v", job.row.orgID, job.row.runID, err)
					res.errors++
					return res
				}
			}

			res.created++
		}
	}
	if isFailedPlaceholder {
		return res
	}

	orgCtx, err := cache.get(ctx, job.row.orgID, func() (*orgContext, error) {
		orgDetails, err := orgService.GetOrgDetails(ctx, job.row.orgID)
		if err != nil {
			return nil, fmt.Errorf("get org details: %w", err)
		}
		nameVariations, err := orgEvaluationService.GenerateNameVariations(ctx, orgDetails.Org.Name, orgDetails.Websites)
		if err != nil {
			return nil, fmt.Errorf("generate name variations: %w", err)
		}
		return &orgContext{
			orgUUID:        orgUUID,
			orgName:        orgDetails.Org.Name,
			websites:       orgDetails.Websites,
			nameVariations: nameVariations,
		}, nil
	})
	if err != nil {
		log.Printf("[fix_missing_org_evals] org=%s run=%s ERROR loading org context: %v", job.row.orgID, job.row.runID, err)
		res.errors++
		return res
	}

	competitors, err := repos.OrgCompetitorRepo.GetByQuestionRunAndOrg(ctx, job.row.runID, orgCtx.orgUUID)
	if err != nil {
		log.Printf("[fix_missing_org_evals] org=%s run=%s ERROR checking competitors: %v", job.row.orgID, job.row.runID, err)
		res.errors++
		return res
	}
	if len(competitors) > 0 {
		res.skippedCompetitors++
	} else {
		competitorResult, err := orgEvaluationService.ExtractCompetitors(ctx, job.row.runID, orgCtx.orgUUID, orgCtx.orgName, responseText)
		if err != nil {
			log.Printf("[fix_missing_org_evals] org=%s run=%s ERROR extract competitors: %v", job.row.orgID, job.row.runID, err)
			res.errors++
			return res
		}
		for _, competitor := range competitorResult.Competitors {
			if err := repos.OrgCompetitorRepo.Create(ctx, competitor); err != nil {
				log.Printf("[fix_missing_org_evals] org=%s run=%s ERROR store competitor %s: %v", job.row.orgID, job.row.runID, competitor.Name, err)
				res.errors++
				return res
			}
			res.createdCompetitors++
		}
	}

	citations, err := repos.OrgCitationRepo.GetByQuestionRunAndOrg(ctx, job.row.runID, orgCtx.orgUUID)
	if err != nil {
		log.Printf("[fix_missing_org_evals] org=%s run=%s ERROR checking citations: %v", job.row.orgID, job.row.runID, err)
		res.errors++
		return res
	}
	if len(citations) > 0 {
		res.skippedCitations++
	} else {
		citationResult, err := orgEvaluationService.ExtractCitations(ctx, job.row.runID, orgCtx.orgUUID, responseText, orgCtx.websites)
		if err != nil {
			log.Printf("[fix_missing_org_evals] org=%s run=%s ERROR extract citations: %v", job.row.orgID, job.row.runID, err)
			res.errors++
			return res
		}
		for _, citation := range citationResult.Citations {
			if err := repos.OrgCitationRepo.Create(ctx, citation); err != nil {
				log.Printf("[fix_missing_org_evals] org=%s run=%s ERROR store citation %s: %v", job.row.orgID, job.row.runID, citation.URL, err)
				res.errors++
				return res
			}
			res.createdCitations++
		}
	}

	return res
}

func main() {
	var (
		csvPath       = flag.String("csv", filepath.Join(".", "runs_missing_evals.csv"), "path to CSV with org_id,question_run_id,eval_count columns")
		dryRun        = flag.Bool("dry-run", true, "if true, do not write to DB or call OpenAI (prints what would happen)")
		orgIDArg      = flag.String("org-id", "", "optional org UUID to scope the run")
		maxRuns       = flag.Int("max-runs", 0, "optional max runs to process across all orgs (0 = all)")
		timeout       = flag.Duration("timeout", 60*time.Minute, "overall timeout for the script")
		concurrency   = flag.Int("concurrency", 20, "number of concurrent extractions to run")
		progressEvery = flag.Int("progress-every", 50, "log progress every N processed rows")
	)
	flag.Parse()

	// Load env vars like the main service (but this tool is intentionally standalone).
	if err := godotenv.Load(); err != nil {
		_ = godotenv.Load("dev.env")
	}
	cfg := config.Load()

	if *concurrency < 1 {
		log.Fatalf("--concurrency must be >= 1")
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	dbClient, err := createDatabaseClient(ctx, cfg.Database)
	if err != nil {
		log.Fatalf("DB connect failed: %v", err)
	}
	defer dbClient.Close()

	repos := services.NewRepositoryManager(dbClient)
	orgService := services.NewOrgService(cfg, repos)
	dataExtractionService := services.NewDataExtractionService(cfg)
	orgEvaluationService := services.NewOrgEvaluationService(cfg, repos, dataExtractionService)

	rows, err := readMissingEvalCSV(*csvPath)
	if err != nil {
		log.Fatalf("Failed reading CSV: %v", err)
	}
	if len(rows) == 0 {
		log.Printf("No rows found in %s", *csvPath)
		return
	}

	filtered := make([]missingEvalRow, 0, len(rows))
	seen := make(map[string]struct{})
	for _, row := range rows {
		if *orgIDArg != "" && row.orgID != *orgIDArg {
			continue
		}
		key := row.orgID + "|" + row.runID.String()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		filtered = append(filtered, row)
		if *maxRuns > 0 && len(filtered) >= *maxRuns {
			break
		}
	}
	if len(filtered) == 0 {
		log.Printf("No matching rows after filtering in %s", *csvPath)
		return
	}

	log.Printf("[fix_missing_org_evals] rows=%d dry_run=%t max_runs=%d concurrency=%d csv=%s", len(filtered), *dryRun, *maxRuns, *concurrency, *csvPath)
	if *dryRun {
		log.Printf("[fix_missing_org_evals] DRY RUN MODE: no DB writes, no OpenAI calls will be made")
		log.Printf("[fix_missing_org_evals] To execute for real: go run ./cmd/fix_missing_org_evals --dry-run=false --csv %s --concurrency %d", *csvPath, *concurrency)
	}

	runIDs := make([]uuid.UUID, 0, len(filtered))
	runIDSet := make(map[uuid.UUID]struct{})
	for _, row := range filtered {
		if _, ok := runIDSet[row.runID]; ok {
			continue
		}
		runIDSet[row.runID] = struct{}{}
		runIDs = append(runIDs, row.runID)
	}

	runs, err := repos.QuestionRunRepo.GetByIDs(ctx, runIDs)
	if err != nil {
		log.Fatalf("Failed fetching question runs: %v", err)
	}
	runByID := make(map[uuid.UUID]*models.QuestionRun, len(runs))
	for _, run := range runs {
		runByID[run.QuestionRunID] = run
	}

	cache := newOrgContextCache()
	jobs := make(chan evalJob)
	results := make(chan evalResult)

	var wg sync.WaitGroup
	var processedCount int64
	for i := 0; i < *concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				res := processJob(ctx, job, *dryRun, repos, orgService, orgEvaluationService, cache)
				current := atomic.AddInt64(&processedCount, int64(res.processed))
				if *progressEvery > 0 && current%int64(*progressEvery) == 0 {
					log.Printf("[fix_missing_org_evals] progress %d/%d", current, len(filtered))
				}
				results <- res
			}
		}()
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	go func() {
		for _, row := range filtered {
			jobs <- evalJob{
				row: row,
				run: runByID[row.runID],
			}
		}
		close(jobs)
	}()

	total := evalResult{}
	for res := range results {
		total.processed += res.processed
		total.created += res.created
		total.skippedExisting += res.skippedExisting
		total.missingRuns += res.missingRuns
		total.emptyResponses += res.emptyResponses
		total.failedResponses += res.failedResponses
		total.createdCompetitors += res.createdCompetitors
		total.createdCitations += res.createdCitations
		total.skippedCompetitors += res.skippedCompetitors
		total.skippedCitations += res.skippedCitations
		total.errors += res.errors
	}

	log.Printf("[fix_missing_org_evals] complete processed=%d created=%d skipped_existing=%d missing_runs=%d empty_responses=%d failed_placeholders=%d competitors=%d citations=%d skipped_competitors=%d skipped_citations=%d errors=%d",
		total.processed, total.created, total.skippedExisting, total.missingRuns, total.emptyResponses, total.failedResponses, total.createdCompetitors, total.createdCitations, total.skippedCompetitors, total.skippedCitations, total.errors)
}
