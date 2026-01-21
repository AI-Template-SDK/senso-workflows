package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"

	"github.com/AI-Template-SDK/senso-api/pkg/database"
	"github.com/AI-Template-SDK/senso-api/pkg/models"
	"github.com/AI-Template-SDK/senso-workflows/internal/config"
	workflowModels "github.com/AI-Template-SDK/senso-workflows/internal/models"
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

func readOrgIDs(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func utcTodayStart(now time.Time) time.Time {
	t := now.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func modelNameMatches(candidate, desired string) bool {
	c := strings.ToLower(strings.TrimSpace(candidate))
	d := strings.ToLower(strings.TrimSpace(desired))
	if c == d {
		return true
	}
	// Helpful when DB stores e.g. "gpt-5.2-mini" and user passes "gpt-5.2"
	return strings.Contains(c, d)
}

func modelNameContains(candidate, substr string) bool {
	c := strings.ToLower(strings.TrimSpace(candidate))
	s := strings.ToLower(strings.TrimSpace(substr))
	if s == "" {
		return false
	}
	return strings.Contains(c, s)
}

func findTodaysOrgBatch(ctx context.Context, repos *services.RepositoryManager, orgUUID uuid.UUID, todayStart time.Time) (*models.QuestionRunBatch, error) {
	batches, err := repos.QuestionRunBatchRepo.GetByOrg(ctx, orgUUID)
	if err != nil {
		return nil, err
	}
	var newestToday *models.QuestionRunBatch
	for _, b := range batches {
		if b == nil {
			continue
		}
		if b.CreatedAt.Before(todayStart) {
			continue
		}
		if newestToday == nil || b.CreatedAt.After(newestToday.CreatedAt) {
			newestToday = b
		}
	}
	return newestToday, nil
}

func createOrgBatch(ctx context.Context, repos *services.RepositoryManager, orgUUID uuid.UUID, totalQuestions int) (*models.QuestionRunBatch, error) {
	now := time.Now()
	batch := &models.QuestionRunBatch{
		BatchID:            uuid.New(),
		Scope:              "org",
		OrgID:              &orgUUID,
		BatchType:          "openai_fixer",
		Status:             "running",
		TotalQuestions:     totalQuestions,
		CompletedQuestions: 0,
		FailedQuestions:    0,
		IsLatest:           true,
		StartedAt:          &now,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if err := repos.QuestionRunBatchRepo.Create(ctx, batch); err != nil {
		return nil, err
	}
	return batch, nil
}

type runJob struct {
	orgID   string
	qID     uuid.UUID
	qText   string
	model   *models.GeoModel
	loc     *models.OrgLocation
	batchID uuid.UUID
}

type runJobResult struct {
	job     runJob
	created bool
	failed  bool
	err     error
	cost    float64
}

func main() {
	var (
		orgFile         = flag.String("org-file", filepath.Join(".", "example_orgs.txt"), "path to file containing org UUIDs (one per line)")
		dryRun          = flag.Bool("dry-run", true, "if true, do not write to DB (prints what would happen)")
		concurrency     = flag.Int("concurrency", 5, "number of concurrent OpenAI calls/inserts per org (bounded)")
		maxOrgs         = flag.Int("max-orgs", 0, "optional max orgs to process (0 = all)")
		timeout         = flag.Duration("timeout", 30*time.Minute, "overall timeout for the script")
		writeModelMatch = flag.String("write-model", "chatgpt", "geo_models name (or substring) to backfill (e.g. 'chatgpt'); runs will be written using that model_id/name")
		apiModel        = flag.String("api-model", "gpt-5.2", "OpenAI model to use at runtime via Responses API (web search enabled)")
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

	var provider services.AIProvider
	if !*dryRun {
		// Azure-only: web search is required and must be executed via Azure OpenAI.
		if strings.TrimSpace(cfg.AzureOpenAIEndpoint) == "" || strings.TrimSpace(cfg.AzureOpenAIKey) == "" || strings.TrimSpace(cfg.AzureOpenAIDeploymentName) == "" {
			log.Fatalf("AZURE_OPENAI_ENDPOINT, AZURE_OPENAI_KEY, and AZURE_OPENAI_DEPLOYMENT_NAME are required for live runs (Azure-only; web search required)")
		}
		provider = services.NewOpenAIProvider(cfg, *apiModel, services.NewCostService())
	}

	orgIDs, err := readOrgIDs(*orgFile)
	if err != nil {
		log.Fatalf("Failed reading org list: %v", err)
	}
	if *maxOrgs > 0 && *maxOrgs < len(orgIDs) {
		orgIDs = orgIDs[:*maxOrgs]
	}

	log.Printf("[openai_fixer] orgs=%d dry_run=%t concurrency=%d write_model_match=%s api_model=%s", len(orgIDs), *dryRun, *concurrency, *writeModelMatch, *apiModel)
	if *dryRun {
		log.Printf("[openai_fixer] DRY RUN MODE: no DB writes, no OpenAI calls will be made")
		log.Printf("[openai_fixer] To execute for real: AZURE_OPENAI_ENDPOINT=... AZURE_OPENAI_KEY=... AZURE_OPENAI_DEPLOYMENT_NAME=... go run ./cmd/openai_fixer --dry-run=false --write-model %s --api-model %s --concurrency %d", *writeModelMatch, *apiModel, *concurrency)
	}
	todayStart := utcTodayStart(time.Now())
	log.Printf("[openai_fixer] todayStart(UTC)=%s", todayStart.Format(time.RFC3339))

	for idx, orgID := range orgIDs {
		log.Printf("[openai_fixer] (%d/%d) org=%s", idx+1, len(orgIDs), orgID)

		orgDetails, err := orgService.GetOrgDetails(ctx, orgID)
		if err != nil {
			log.Printf("[openai_fixer] org=%s ERROR get details: %v", orgID, err)
			continue
		}

		// Backfill ALL org geo_models that match write-model (typically "chatgpt").
		selectedModels := make([]*models.GeoModel, 0)
		for _, m := range orgDetails.Models {
			if modelNameContains(m.Name, *writeModelMatch) || modelNameMatches(m.Name, *writeModelMatch) {
				selectedModels = append(selectedModels, m)
			}
		}
		if len(selectedModels) == 0 {
			log.Printf("[openai_fixer] org=%s skip (no geo model matching %q configured on org)", orgID, *writeModelMatch)
			continue
		}

		orgUUID, err := uuid.Parse(orgID)
		if err != nil {
			log.Printf("[openai_fixer] org=%s invalid uuid: %v", orgID, err)
			continue
		}

		// Attach runs to today's org batch (create if missing; but NEVER create in dry-run).
		totalQuestions := len(orgDetails.Questions) * len(selectedModels) * len(orgDetails.Locations)
		batch, err := findTodaysOrgBatch(ctx, repos, orgUUID, todayStart)
		if err != nil {
			log.Printf("[openai_fixer] org=%s ERROR finding today's batch: %v", orgID, err)
			continue
		}

		isExisting := batch != nil
		if !isExisting {
			if *dryRun {
				log.Printf("[openai_fixer] org=%s DRY RUN would create today's batch (type=openai_fixer total_questions=%d)", orgID, totalQuestions)
			} else {
				createdBatch, err := createOrgBatch(ctx, repos, orgUUID, totalQuestions)
				if err != nil {
					log.Printf("[openai_fixer] org=%s ERROR creating today's batch: %v", orgID, err)
					continue
				}
				batch = createdBatch
			}
		}

		batchID := uuid.Nil
		batchStatus := ""
		if batch != nil {
			batchID = batch.BatchID
			batchStatus = batch.Status
		}
		log.Printf("[openai_fixer] org=%s batch=%s (existing=%t status=%s)", orgID, batchID, isExisting, batchStatus)

		// Build missing jobs for question × model × location (write-model(s)).
		jobs := make([]runJob, 0)
		seen := make(map[string]struct{})
		skippedExisting := 0

		for _, model := range selectedModels {
			for _, loc := range orgDetails.Locations {
				for _, qwt := range orgDetails.Questions {
					q := qwt.Question

					runs, err := repos.QuestionRunRepo.GetByQuestion(ctx, q.GeoQuestionID)
					if err != nil {
						// Be conservative: schedule if we can't verify.
						key := fmt.Sprintf("%s|%s|%s", q.GeoQuestionID, model.GeoModelID, loc.OrgLocationID)
						if _, ok := seen[key]; ok {
							continue
						}
						seen[key] = struct{}{}
						jobs = append(jobs, runJob{
							orgID:   orgID,
							qID:     q.GeoQuestionID,
							qText:   q.QuestionText,
							model:   model,
							loc:     loc,
							batchID: batchID,
						})
						continue
					}

					found := false
					for _, run := range runs {
						if run.CreatedAt.Before(todayStart) {
							continue
						}
						if run.ModelID == nil || run.LocationID == nil {
							continue
						}
						if *run.ModelID == model.GeoModelID && *run.LocationID == loc.OrgLocationID {
							found = true
							break
						}
					}

					if found {
						skippedExisting++
						continue
					}

					key := fmt.Sprintf("%s|%s|%s", q.GeoQuestionID, model.GeoModelID, loc.OrgLocationID)
					if _, ok := seen[key]; ok {
						continue
					}
					seen[key] = struct{}{}

					jobs = append(jobs, runJob{
						orgID:   orgID,
						qID:     q.GeoQuestionID,
						qText:   q.QuestionText,
						model:   model,
						loc:     loc,
						batchID: batchID,
					})
				}
			}
		}

		if len(jobs) == 0 {
			log.Printf("[openai_fixer] org=%s done (no missing runs) skipped_existing=%d", orgID, skippedExisting)
			continue
		}
		log.Printf("[openai_fixer] org=%s missing_jobs=%d skipped_existing=%d (executing with concurrency=%d)", orgID, len(jobs), skippedExisting, *concurrency)

		jobsCh := make(chan runJob)
		resultsCh := make(chan runJobResult, len(jobs))
		var wg sync.WaitGroup

		worker := func() {
			defer wg.Done()
			for job := range jobsCh {
				if *dryRun {
					resultsCh <- runJobResult{job: job, created: true}
					continue
				}
				if job.batchID == uuid.Nil {
					resultsCh <- runJobResult{job: job, failed: true, err: fmt.Errorf("missing batch_id (unexpected nil batch in non-dry-run)")}
					continue
				}

				loc := &workflowModels.Location{
					Country: job.loc.CountryCode,
					Region:  job.loc.RegionName,
				}

				aiResp, err := provider.RunQuestion(ctx, job.qText, true, loc) // web search ON
				if err != nil {
					resultsCh <- runJobResult{job: job, failed: true, err: err}
					continue
				}

				responseText := aiResp.Response
				inputTokens := aiResp.InputTokens
				outputTokens := aiResp.OutputTokens
				totalCost := aiResp.Cost

				runModel := job.model.Name
				runCountry := job.loc.CountryCode
				var runRegion *string
				if job.loc.RegionName != nil {
					runRegion = job.loc.RegionName
				} else {
					empty := ""
					runRegion = &empty
				}

				now := time.Now()
				qr := &models.QuestionRun{
					QuestionRunID: uuid.New(),
					GeoQuestionID: job.qID,
					ModelID:       &job.model.GeoModelID,
					LocationID:    &job.loc.OrgLocationID,
					ResponseText:  &responseText,
					InputTokens:   &inputTokens,
					OutputTokens:  &outputTokens,
					TotalCost:     &totalCost,
					BatchID:       &job.batchID,
					RunModel:      &runModel,
					RunCountry:    &runCountry,
					RunRegion:     runRegion,
					IsLatest:      true,
					CreatedAt:     now,
					UpdatedAt:     now,
				}

				if err := repos.QuestionRunRepo.Create(ctx, qr); err != nil {
					resultsCh <- runJobResult{job: job, failed: true, err: err}
					continue
				}

				resultsCh <- runJobResult{job: job, created: true, cost: totalCost}
			}
		}

		for i := 0; i < *concurrency; i++ {
			wg.Add(1)
			go worker()
		}

		go func() {
			for _, j := range jobs {
				jobsCh <- j
			}
			close(jobsCh)
		}()

		go func() {
			wg.Wait()
			close(resultsCh)
		}()

		createdCount := 0
		failedCount := 0
		var totalCost float64

		for res := range resultsCh {
			if res.failed {
				failedCount++
				log.Printf("[openai_fixer] org=%s ERROR job question=%s location=%s: %v",
					orgID, res.job.qID, res.job.loc.CountryCode, res.err)
				continue
			}
			if res.created {
				createdCount++
				totalCost += res.cost
				if *dryRun {
					log.Printf("[openai_fixer] DRY RUN would insert run question=%s model=%s location=%s", res.job.qID, res.job.model.Name, res.job.loc.CountryCode)
				}
			}
		}

		log.Printf("[openai_fixer] org=%s done created=%d skipped_existing=%d failed=%d total_cost=%.6f", orgID, createdCount, skippedExisting, failedCount, totalCost)
	}

	log.Printf("[openai_fixer] done")
}
