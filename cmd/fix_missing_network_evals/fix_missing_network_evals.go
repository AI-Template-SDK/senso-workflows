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

// Standalone one-off tool: intentionally duplicates DB bootstrapping from main.go.
// This is the network-eval counterpart of cmd/fix_missing_org_evals: it backfills
// missing network_org_eval (plus network_org_competitor / network_org_citation)
// records for question runs listed in a CSV.
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
	networkID string
	orgID     string
	runID     uuid.UUID
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
		if len(rec) < 3 {
			continue
		}

		networkID := strings.TrimSpace(rec[0])
		orgID := strings.TrimSpace(rec[1])
		runIDStr := strings.TrimSpace(rec[2])
		if idx == 0 && strings.EqualFold(networkID, "network_id") {
			continue
		}
		if networkID == "" || orgID == "" || runIDStr == "" {
			continue
		}

		runID, err := uuid.Parse(runIDStr)
		if err != nil {
			return nil, fmt.Errorf("invalid question_run_id %q on line %d: %w", runIDStr, idx+1, err)
		}

		rows = append(rows, missingEvalRow{
			networkID: networkID,
			orgID:     orgID,
			runID:     runID,
		})
	}

	return rows, nil
}

// resourceCache is a small singleflight cache so concurrent workers load each
// org context / question text exactly once.
type cacheCall[T any] struct {
	done  chan struct{}
	value T
	err   error
}

type resourceCache[T any] struct {
	mu       sync.Mutex
	data     map[string]T
	inflight map[string]*cacheCall[T]
}

func newResourceCache[T any]() *resourceCache[T] {
	return &resourceCache[T]{
		data:     make(map[string]T),
		inflight: make(map[string]*cacheCall[T]),
	}
}

func (c *resourceCache[T]) get(key string, loader func() (T, error)) (T, error) {
	c.mu.Lock()
	if cached, ok := c.data[key]; ok {
		c.mu.Unlock()
		return cached, nil
	}
	if call, ok := c.inflight[key]; ok {
		c.mu.Unlock()
		<-call.done
		return call.value, call.err
	}
	call := &cacheCall[T]{done: make(chan struct{})}
	c.inflight[key] = call
	c.mu.Unlock()

	value, err := loader()

	c.mu.Lock()
	if err == nil {
		c.data[key] = value
	}
	call.value = value
	call.err = err
	close(call.done)
	delete(c.inflight, key)
	c.mu.Unlock()

	return value, err
}

type orgContext struct {
	orgUUID        uuid.UUID
	orgName        string
	websites       []string
	nameVariations []string
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
	failedPlaceholders int
	createdCompetitors int
	createdCitations   int
	skippedCompetitors int
	skippedCitations   int
	errors             int
}

// failedPlaceholderText is the response stored when an AI model call failed; such
// runs cannot be evaluated, so we only ever write a minimal "not mentioned" eval.
const failedPlaceholderText = "This prompt didn’t complete successfully due to a temporary AI model limitation. You were not charged for this prompt. We'll re-try in the next run."

func minimalNetworkOrgEval(runID, orgUUID uuid.UUID) *models.NetworkOrgEval {
	now := time.Now()
	return &models.NetworkOrgEval{
		NetworkOrgEvalID: uuid.New(),
		QuestionRunID:    runID,
		OrgID:            orgUUID,
		Mentioned:        false,
		Citation:         false,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
}

func processJob(
	ctx context.Context,
	job evalJob,
	dryRun bool,
	repos *services.RepositoryManager,
	orgService services.OrgService,
	dataExtractionService services.DataExtractionService,
	orgCache *resourceCache[*orgContext],
	questionCache *resourceCache[string],
) evalResult {
	res := evalResult{processed: 1}

	if ctx.Err() != nil {
		res.errors++
		return res
	}

	if job.run == nil {
		log.Printf("[fix_missing_network_evals] network=%s org=%s run=%s missing in DB", job.row.networkID, job.row.orgID, job.row.runID)
		res.missingRuns++
		return res
	}

	orgUUID, err := uuid.Parse(job.row.orgID)
	if err != nil {
		log.Printf("[fix_missing_network_evals] org=%s invalid uuid: %v", job.row.orgID, err)
		res.errors++
		return res
	}

	evals, err := repos.NetworkOrgEvalRepo.GetByQuestionRunAndOrg(ctx, job.row.runID, orgUUID)
	if err != nil {
		log.Printf("[fix_missing_network_evals] org=%s run=%s ERROR checking evals: %v", job.row.orgID, job.row.runID, err)
		res.errors++
		return res
	}
	if len(evals) > 0 {
		res.skippedExisting++
		return res
	}

	if job.run.ResponseText == nil || strings.TrimSpace(*job.run.ResponseText) == "" {
		log.Printf("[fix_missing_network_evals] org=%s run=%s missing response text", job.row.orgID, job.row.runID)
		res.emptyResponses++
		return res
	}

	responseText := *job.run.ResponseText
	isFailedPlaceholder := responseText == failedPlaceholderText

	if dryRun {
		if isFailedPlaceholder {
			log.Printf("[fix_missing_network_evals] org=%s run=%s would_create_minimal_eval_for_failed_run", job.row.orgID, job.row.runID)
		} else {
			log.Printf("[fix_missing_network_evals] org=%s run=%s would_create_network_eval_competitors_and_citations", job.row.orgID, job.row.runID)
		}
		res.created++
		return res
	}

	// Failed-run placeholders carry no real content; store a minimal eval and stop.
	if isFailedPlaceholder {
		if err := repos.NetworkOrgEvalRepo.Create(ctx, minimalNetworkOrgEval(job.row.runID, orgUUID)); err != nil {
			log.Printf("[fix_missing_network_evals] org=%s run=%s ERROR create minimal eval: %v", job.row.orgID, job.row.runID, err)
			res.errors++
			return res
		}
		res.failedPlaceholders++
		res.created++
		return res
	}

	orgCtx, err := orgCache.get(job.row.orgID, func() (*orgContext, error) {
		orgDetails, err := orgService.GetOrgDetails(ctx, job.row.orgID)
		if err != nil {
			return nil, fmt.Errorf("get org details: %w", err)
		}
		nameVariations, err := dataExtractionService.GenerateNameVariations(ctx, orgDetails.Org.Name, orgDetails.Websites)
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
		log.Printf("[fix_missing_network_evals] org=%s run=%s ERROR loading org context: %v", job.row.orgID, job.row.runID, err)
		res.errors++
		return res
	}

	// Question text is only used to sharpen the evaluation prompt; a lookup
	// failure is non-fatal so we still extract competitors and citations.
	questionText, err := questionCache.get(job.run.GeoQuestionID.String(), func() (string, error) {
		question, err := repos.GeoQuestionRepo.GetByID(ctx, job.run.GeoQuestionID)
		if err != nil {
			return "", err
		}
		return question.QuestionText, nil
	})
	if err != nil {
		log.Printf("[fix_missing_network_evals] org=%s run=%s WARN could not load question text: %v", job.row.orgID, job.row.runID, err)
		questionText = ""
	}

	result, err := dataExtractionService.ExtractNetworkOrgData(ctx, job.row.runID, orgCtx.orgUUID, orgCtx.orgName, orgCtx.websites, questionText, responseText, orgCtx.nameVariations)
	if err != nil {
		log.Printf("[fix_missing_network_evals] org=%s run=%s ERROR extract network org data: %v", job.row.orgID, job.row.runID, err)
		res.errors++
		return res
	}

	if result.Evaluation == nil {
		log.Printf("[fix_missing_network_evals] org=%s run=%s ERROR extraction returned no evaluation", job.row.orgID, job.row.runID)
		res.errors++
		return res
	}
	if err := repos.NetworkOrgEvalRepo.Create(ctx, result.Evaluation); err != nil {
		log.Printf("[fix_missing_network_evals] org=%s run=%s ERROR store network eval: %v", job.row.orgID, job.row.runID, err)
		res.errors++
		return res
	}
	res.created++

	// Competitors and citations are extracted alongside the eval. Skip storing
	// them if a prior partial run already left rows for this run+org.
	competitors, err := repos.NetworkOrgCompetitorRepo.GetByQuestionRunAndOrg(ctx, job.row.runID, orgCtx.orgUUID)
	if err != nil {
		log.Printf("[fix_missing_network_evals] org=%s run=%s ERROR checking competitors: %v", job.row.orgID, job.row.runID, err)
		res.errors++
		return res
	}
	if len(competitors) > 0 {
		res.skippedCompetitors++
	} else {
		for _, competitor := range result.Competitors {
			if err := repos.NetworkOrgCompetitorRepo.Create(ctx, competitor); err != nil {
				log.Printf("[fix_missing_network_evals] org=%s run=%s ERROR store competitor %s: %v", job.row.orgID, job.row.runID, competitor.Name, err)
				res.errors++
				return res
			}
			res.createdCompetitors++
		}
	}

	citations, err := repos.NetworkOrgCitationRepo.GetByQuestionRunAndOrg(ctx, job.row.runID, orgCtx.orgUUID)
	if err != nil {
		log.Printf("[fix_missing_network_evals] org=%s run=%s ERROR checking citations: %v", job.row.orgID, job.row.runID, err)
		res.errors++
		return res
	}
	if len(citations) > 0 {
		res.skippedCitations++
	} else {
		for _, citation := range result.Citations {
			if err := repos.NetworkOrgCitationRepo.Create(ctx, citation); err != nil {
				log.Printf("[fix_missing_network_evals] org=%s run=%s ERROR store citation %s: %v", job.row.orgID, job.row.runID, citation.URL, err)
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
		csvPath       = flag.String("csv", filepath.Join(".", "runs_missing_network_evals.csv"), "path to CSV with network_id,org_id,question_run_id columns")
		dryRun        = flag.Bool("dry-run", true, "if true, do not write to DB or call OpenAI (prints what would happen)")
		networkIDArg  = flag.String("network-id", "", "optional network UUID to scope the run")
		orgIDArg      = flag.String("org-id", "", "optional org UUID to scope the run")
		maxRuns       = flag.Int("max-runs", 0, "optional max rows to process across all networks/orgs (0 = all)")
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

	rows, err := readMissingEvalCSV(*csvPath)
	if err != nil {
		log.Fatalf("Failed reading CSV: %v", err)
	}
	if len(rows) == 0 {
		log.Printf("No rows found in %s", *csvPath)
		return
	}

	// Filter by optional scoping flags and dedupe by org+run (a network_org_eval
	// is keyed by question_run_id + org_id, so the same pair must run only once).
	filtered := make([]missingEvalRow, 0, len(rows))
	seen := make(map[string]struct{})
	for _, row := range rows {
		if *networkIDArg != "" && row.networkID != *networkIDArg {
			continue
		}
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

	log.Printf("[fix_missing_network_evals] rows=%d dry_run=%t max_runs=%d concurrency=%d csv=%s", len(filtered), *dryRun, *maxRuns, *concurrency, *csvPath)
	if *dryRun {
		log.Printf("[fix_missing_network_evals] DRY RUN MODE: no DB writes, no OpenAI calls will be made")
		log.Printf("[fix_missing_network_evals] To execute for real: go run ./cmd/fix_missing_network_evals --dry-run=false --csv %s --concurrency %d", *csvPath, *concurrency)
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

	orgCache := newResourceCache[*orgContext]()
	questionCache := newResourceCache[string]()
	jobs := make(chan evalJob)
	results := make(chan evalResult)

	var wg sync.WaitGroup
	var processedCount int64
	for i := 0; i < *concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				res := processJob(ctx, job, *dryRun, repos, orgService, dataExtractionService, orgCache, questionCache)
				current := atomic.AddInt64(&processedCount, int64(res.processed))
				if *progressEvery > 0 && current%int64(*progressEvery) == 0 {
					log.Printf("[fix_missing_network_evals] progress %d/%d", current, len(filtered))
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
		total.failedPlaceholders += res.failedPlaceholders
		total.createdCompetitors += res.createdCompetitors
		total.createdCitations += res.createdCitations
		total.skippedCompetitors += res.skippedCompetitors
		total.skippedCitations += res.skippedCitations
		total.errors += res.errors
	}

	log.Printf("[fix_missing_network_evals] complete processed=%d created=%d skipped_existing=%d missing_runs=%d empty_responses=%d failed_placeholders=%d competitors=%d citations=%d skipped_competitors=%d skipped_citations=%d errors=%d",
		total.processed, total.created, total.skippedExisting, total.missingRuns, total.emptyResponses, total.failedPlaceholders, total.createdCompetitors, total.createdCitations, total.skippedCompetitors, total.skippedCitations, total.errors)
}
