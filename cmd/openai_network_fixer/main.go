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
	"github.com/AI-Template-SDK/senso-api/pkg/repositories/interfaces"
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

func readIDs(path string) ([]string, error) {
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

func modelNameContains(candidate, substr string) bool {
	c := strings.ToLower(strings.TrimSpace(candidate))
	s := strings.ToLower(strings.TrimSpace(substr))
	if s == "" {
		return false
	}
	return strings.Contains(c, s)
}

func regionString(region *string) string {
	if region == nil {
		return ""
	}
	return *region
}

func loadNetworkQuestionsAndLocations(
	ctx context.Context,
	repos *services.RepositoryManager,
	networkUUID uuid.UUID,
) ([]interfaces.GeoQuestionWithTags, []*models.OrgLocation, error) {
	questions, err := repos.GeoQuestionRepo.GetByNetworkWithTags(ctx, networkUUID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get network questions: %w", err)
	}

	networkLocations, err := repos.NetworkLocationRepo.GetByNetwork(ctx, networkUUID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get network locations: %w", err)
	}

	// Convert NetworkLocation to OrgLocation-like structs (IDs are not used/stored for network runs).
	var locations []*models.OrgLocation
	if len(networkLocations) == 0 {
		locations = []*models.OrgLocation{
			{
				OrgLocationID: uuid.New(),
				OrgID:         uuid.Nil,
				CountryCode:   "US",
				RegionName:    nil,
				CreatedAt:     time.Now(),
				UpdatedAt:     time.Now(),
			},
		}
	} else {
		locations = make([]*models.OrgLocation, len(networkLocations))
		for i, nl := range networkLocations {
			locations[i] = &models.OrgLocation{
				OrgLocationID: uuid.New(),
				OrgID:         uuid.Nil,
				CountryCode:   nl.CountryCode,
				RegionName:    nl.RegionName,
				CreatedAt:     nl.CreatedAt,
				UpdatedAt:     nl.UpdatedAt,
			}
		}
	}

	return questions, locations, nil
}

func findTodaysNetworkBatch(ctx context.Context, repos *services.RepositoryManager, networkUUID uuid.UUID, todayStart time.Time) (*models.QuestionRunBatch, error) {
	questions, err := repos.GeoQuestionRepo.GetByNetwork(ctx, networkUUID)
	if err != nil {
		return nil, fmt.Errorf("failed to get network questions: %w", err)
	}
	seen := make(map[uuid.UUID]struct{})
	var newest *models.QuestionRunBatch

	for _, q := range questions {
		runs, err := repos.QuestionRunRepo.GetByQuestion(ctx, q.GeoQuestionID)
		if err != nil {
			continue
		}
		for _, run := range runs {
			if run.BatchID == nil {
				continue
			}
			if _, ok := seen[*run.BatchID]; ok {
				continue
			}
			seen[*run.BatchID] = struct{}{}
			b, err := repos.QuestionRunBatchRepo.GetByID(ctx, *run.BatchID)
			if err != nil || b == nil {
				continue
			}
			if b.NetworkID == nil || *b.NetworkID != networkUUID {
				continue
			}
			if b.CreatedAt.Before(todayStart) {
				continue
			}
			if newest == nil || b.CreatedAt.After(newest.CreatedAt) {
				newest = b
			}
		}
	}
	return newest, nil
}

func createNetworkBatch(ctx context.Context, repos *services.RepositoryManager, networkUUID uuid.UUID, totalQuestions int) (*models.QuestionRunBatch, error) {
	now := time.Now()
	b := &models.QuestionRunBatch{
		BatchID:            uuid.New(),
		Scope:              "network",
		NetworkID:          &networkUUID,
		BatchType:          "openai_network_fixer",
		Status:             "running",
		TotalQuestions:     totalQuestions,
		CompletedQuestions: 0,
		FailedQuestions:    0,
		IsLatest:           true,
		StartedAt:          &now,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if err := repos.QuestionRunBatchRepo.Create(ctx, b); err != nil {
		return nil, err
	}
	return b, nil
}

type runJob struct {
	networkID  string
	qID        uuid.UUID
	qText      string
	writeModel string
	country    string
	region     *string
	batchID    uuid.UUID
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
		networkFile = flag.String("network-file", filepath.Join(".", "example_networks.txt"), "path to file containing network UUIDs (one per line)")
		dryRun      = flag.Bool("dry-run", true, "if true, do not write to DB (prints what would happen)")
		concurrency = flag.Int("concurrency", 5, "number of concurrent OpenAI calls/inserts per network (bounded)")
		maxNetworks = flag.Int("max-networks", 0, "optional max networks to process (0 = all)")
		timeout     = flag.Duration("timeout", 30*time.Minute, "overall timeout for the script")
		writeModel  = flag.String("write-model", "chatgpt", "network model name (or substring) to backfill into question_runs.run_model (e.g. 'chatgpt')")
		apiModel    = flag.String("api-model", "gpt-5.2", "OpenAI model to use at runtime via Responses API (web search enabled)")
	)
	flag.Parse()

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

	var provider services.AIProvider
	if !*dryRun {
		// Azure-only: web search is required and must be executed via Azure OpenAI.
		if strings.TrimSpace(cfg.AzureOpenAIEndpoint) == "" || strings.TrimSpace(cfg.AzureOpenAIKey) == "" || strings.TrimSpace(cfg.AzureOpenAIDeploymentName) == "" {
			log.Fatalf("AZURE_OPENAI_ENDPOINT, AZURE_OPENAI_KEY, and AZURE_OPENAI_DEPLOYMENT_NAME are required for live runs (Azure-only; web search required)")
		}
		provider = services.NewOpenAIProvider(cfg, *apiModel, services.NewCostService())
	}

	networkIDs, err := readIDs(*networkFile)
	if err != nil {
		log.Fatalf("Failed reading network list: %v", err)
	}
	if *maxNetworks > 0 && *maxNetworks < len(networkIDs) {
		networkIDs = networkIDs[:*maxNetworks]
	}

	log.Printf("[openai_network_fixer] networks=%d dry_run=%t concurrency=%d write_model=%s api_model=%s", len(networkIDs), *dryRun, *concurrency, *writeModel, *apiModel)
	if *dryRun {
		log.Printf("[openai_network_fixer] DRY RUN MODE: no DB writes, no OpenAI calls will be made")
		log.Printf("[openai_network_fixer] To execute for real: AZURE_OPENAI_ENDPOINT=... AZURE_OPENAI_KEY=... AZURE_OPENAI_DEPLOYMENT_NAME=... go run ./cmd/openai_network_fixer --dry-run=false --write-model %s --api-model %s --concurrency %d", *writeModel, *apiModel, *concurrency)
	}

	todayStart := utcTodayStart(time.Now())
	log.Printf("[openai_network_fixer] todayStart(UTC)=%s", todayStart.Format(time.RFC3339))

	for idx, networkID := range networkIDs {
		log.Printf("[openai_network_fixer] (%d/%d) network=%s", idx+1, len(networkIDs), networkID)

		networkUUID, err := uuid.Parse(networkID)
		if err != nil {
			log.Printf("[openai_network_fixer] network=%s invalid uuid: %v", networkID, err)
			continue
		}

		// Determine configured network models (do NOT fallback).
		modelNames, err := repos.NetworkModelRepo.GetByNetworkID(ctx, networkUUID)
		if err != nil {
			log.Printf("[openai_network_fixer] network=%s ERROR get network models: %v", networkID, err)
			continue
		}
		if len(modelNames) == 0 {
			log.Printf("[openai_network_fixer] network=%s skip (no network models configured; not using fallback defaults)", networkID)
			continue
		}

		writeModels := make([]string, 0)
		for _, name := range modelNames {
			if modelNameContains(name, *writeModel) {
				writeModels = append(writeModels, name)
			}
		}
		if len(writeModels) == 0 {
			log.Printf("[openai_network_fixer] network=%s skip (no network model matching %q configured)", networkID, *writeModel)
			continue
		}

		networkQuestions, networkLocations, err := loadNetworkQuestionsAndLocations(ctx, repos, networkUUID)
		if err != nil {
			log.Printf("[openai_network_fixer] network=%s ERROR load questions/locations: %v", networkID, err)
			continue
		}

		totalQuestions := len(networkQuestions) * len(writeModels) * len(networkLocations)

		batch, err := findTodaysNetworkBatch(ctx, repos, networkUUID, todayStart)
		if err != nil {
			log.Printf("[openai_network_fixer] network=%s ERROR finding today's batch: %v", networkID, err)
			continue
		}
		isExisting := batch != nil
		if !isExisting {
			if *dryRun {
				log.Printf("[openai_network_fixer] network=%s DRY RUN would create today's batch (type=openai_network_fixer total_questions=%d)", networkID, totalQuestions)
			} else {
				createdBatch, err := createNetworkBatch(ctx, repos, networkUUID, totalQuestions)
				if err != nil {
					log.Printf("[openai_network_fixer] network=%s ERROR creating today's batch: %v", networkID, err)
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
		log.Printf("[openai_network_fixer] network=%s batch=%s (existing=%t status=%s)", networkID, batchID, isExisting, batchStatus)

		jobs := make([]runJob, 0)
		seen := make(map[string]struct{})
		skippedExisting := 0

		for _, writeModelName := range writeModels {
			for _, loc := range networkLocations {
				for _, qwt := range networkQuestions {
					q := qwt.Question

					runs, err := repos.QuestionRunRepo.GetByQuestion(ctx, q.GeoQuestionID)
					if err != nil {
						key := fmt.Sprintf("%s|%s|%s|%s", q.GeoQuestionID, writeModelName, loc.CountryCode, regionString(loc.RegionName))
						if _, ok := seen[key]; ok {
							continue
						}
						seen[key] = struct{}{}
						jobs = append(jobs, runJob{
							networkID:  networkID,
							qID:        q.GeoQuestionID,
							qText:      q.QuestionText,
							writeModel: writeModelName,
							country:    loc.CountryCode,
							region:     loc.RegionName,
							batchID:    batchID,
						})
						continue
					}

					found := false
					for _, run := range runs {
						if run.CreatedAt.Before(todayStart) {
							continue
						}
						if run.RunModel == nil || run.RunCountry == nil {
							continue
						}
						if *run.RunModel != writeModelName || *run.RunCountry != loc.CountryCode {
							continue
						}
						if loc.RegionName != nil {
							if run.RunRegion == nil || *run.RunRegion != *loc.RegionName {
								continue
							}
						}
						found = true
						break
					}

					if found {
						skippedExisting++
						continue
					}

					key := fmt.Sprintf("%s|%s|%s|%s", q.GeoQuestionID, writeModelName, loc.CountryCode, regionString(loc.RegionName))
					if _, ok := seen[key]; ok {
						continue
					}
					seen[key] = struct{}{}

					jobs = append(jobs, runJob{
						networkID:  networkID,
						qID:        q.GeoQuestionID,
						qText:      q.QuestionText,
						writeModel: writeModelName,
						country:    loc.CountryCode,
						region:     loc.RegionName,
						batchID:    batchID,
					})
				}
			}
		}

		if len(jobs) == 0 {
			log.Printf("[openai_network_fixer] network=%s done (no missing runs) skipped_existing=%d", networkID, skippedExisting)
			continue
		}
		log.Printf("[openai_network_fixer] network=%s missing_jobs=%d skipped_existing=%d (executing with concurrency=%d)", networkID, len(jobs), skippedExisting, *concurrency)

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
					Country: job.country,
					Region:  job.region,
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

				runModel := job.writeModel
				runCountry := job.country

				now := time.Now()
				qr := &models.QuestionRun{
					QuestionRunID: uuid.New(),
					GeoQuestionID: job.qID,
					// Network runs use string fields, not model_id/location_id
					ResponseText: &responseText,
					InputTokens:  &inputTokens,
					OutputTokens: &outputTokens,
					TotalCost:    &totalCost,
					BatchID:      &job.batchID,
					RunModel:     &runModel,
					RunCountry:   &runCountry,
					RunRegion:    job.region,
					IsLatest:     true,
					CreatedAt:    now,
					UpdatedAt:    now,
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
				log.Printf("[openai_network_fixer] network=%s ERROR job question=%s model=%s location=%s: %v",
					networkID, res.job.qID, res.job.writeModel, res.job.country, res.err)
				continue
			}
			if res.created {
				createdCount++
				totalCost += res.cost
				if *dryRun {
					log.Printf("[openai_network_fixer] DRY RUN would insert run question=%s model=%s location=%s", res.job.qID, res.job.writeModel, res.job.country)
				}
			}
		}

		log.Printf("[openai_network_fixer] network=%s done created=%d skipped_existing=%d failed=%d total_cost=%.6f", networkID, createdCount, skippedExisting, failedCount, totalCost)
	}

	log.Printf("[openai_network_fixer] done")
}
