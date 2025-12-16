package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
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
	"github.com/AI-Template-SDK/senso-workflows/services"
	"github.com/google/uuid"
)

// createDatabaseClient is intentionally duplicated from main.go because this is a standalone one-off tool.
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

type perplexityChatRequest struct {
	Model    string                  `json:"model"`
	Messages []perplexityChatMessage `json:"messages"`
	// Leaving out optional params on purpose (single-use fixer).
}

type perplexityChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type perplexityChatResponse struct {
	Model string `json:"model"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
		Cost             struct {
			// Per the example payload.
			TotalCost float64 `json:"total_cost"`
		} `json:"cost"`
	} `json:"usage"`
	Citations []string `json:"citations"`
	Choices   []struct {
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
}

type perplexityClient struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
	model      string
}

func newPerplexityClientFromEnv() (*perplexityClient, error) {
	apiKey := strings.TrimSpace(os.Getenv("PERPLEXITY_API_KEY"))
	if apiKey == "" {
		return nil, fmt.Errorf("PERPLEXITY_API_KEY is not set")
	}
	baseURL := strings.TrimSpace(os.Getenv("PERPLEXITY_BASE_URL"))
	if baseURL == "" {
		baseURL = "https://api.perplexity.ai"
	}
	model := strings.TrimSpace(os.Getenv("PERPLEXITY_CHAT_MODEL"))
	if model == "" {
		model = "sonar"
	}

	return &perplexityClient{
		apiKey:  apiKey,
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		httpClient: &http.Client{
			Timeout: 5 * time.Minute,
		},
	}, nil
}

func (c *perplexityClient) chatCompletion(ctx context.Context, prompt string) (*perplexityChatResponse, error) {
	reqBody := perplexityChatRequest{
		Model: c.model,
		Messages: []perplexityChatMessage{
			{Role: "user", Content: prompt},
		},
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := c.baseURL + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("accept", "application/json")
	req.Header.Set("content-type", "application/json")
	req.Header.Set("authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var b bytes.Buffer
		_, _ = b.ReadFrom(resp.Body)
		return nil, fmt.Errorf("perplexity http %d: %s", resp.StatusCode, strings.TrimSpace(b.String()))
	}

	var out perplexityChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &out, nil
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

func isPerplexityModelName(name string) bool {
	return strings.Contains(strings.ToLower(name), "perplexity")
}

func utcTodayStart(now time.Time) time.Time {
	t := now.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// Mirrors services.formatLocationForPrompt (unexported) used by providers for localization instructions.
func formatLocationForPrompt(country string, region *string) string {
	if strings.TrimSpace(country) == "" && (region == nil || strings.TrimSpace(*region) == "") {
		return "the relevant region and country"
	}

	var parts []string
	if region != nil && strings.TrimSpace(*region) != "" {
		parts = append(parts, strings.TrimSpace(*region))
	}
	if strings.TrimSpace(country) != "" {
		parts = append(parts, strings.TrimSpace(country))
	}
	if len(parts) == 0 {
		return "the relevant region and country"
	}
	return strings.Join(parts, ", ")
}

func buildLocalizedPrompt(query string, country string, region *string) string {
	locationDescription := formatLocationForPrompt(country, region)
	return fmt.Sprintf("Ensure your response is localized to %s. Answer the following question: %s", locationDescription, query)
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
	skipped bool
	failed  bool
	err     error
	cost    float64
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
		BatchType:          "perplexity_fixer",
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

func main() {
	var (
		orgFile     = flag.String("org-file", filepath.Join(".", "example_orgs.txt"), "path to file containing org UUIDs (one per line)")
		dryRun      = flag.Bool("dry-run", true, "if true, do not write to DB (prints what would happen)")
		concurrency = flag.Int("concurrency", 5, "number of concurrent Perplexity calls/inserts per org (bounded)")
		maxOrgs     = flag.Int("max-orgs", 0, "optional max orgs to process (0 = all)")
		timeout     = flag.Duration("timeout", 30*time.Minute, "overall timeout for the script")
	)
	flag.Parse()

	// Load env vars like the main service (but this tool is intentionally standalone).
	if err := godotenv.Load(); err != nil {
		_ = godotenv.Load("dev.env")
	}
	cfg := config.Load()

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	dbClient, err := createDatabaseClient(ctx, cfg.Database)
	if err != nil {
		log.Fatalf("DB connect failed: %v", err)
	}
	defer dbClient.Close()

	repos := services.NewRepositoryManager(dbClient)
	orgService := services.NewOrgService(cfg, repos)

	var pplx *perplexityClient
	if !*dryRun {
		pplxClient, err := newPerplexityClientFromEnv()
		if err != nil {
			log.Fatalf("Perplexity client init failed: %v", err)
		}
		pplx = pplxClient
	}

	orgIDs, err := readOrgIDs(*orgFile)
	if err != nil {
		log.Fatalf("Failed reading org list: %v", err)
	}
	if *maxOrgs > 0 && *maxOrgs < len(orgIDs) {
		orgIDs = orgIDs[:*maxOrgs]
	}

	if *concurrency < 1 {
		log.Fatalf("--concurrency must be >= 1")
	}

	modelName := ""
	baseURL := ""
	if pplx != nil {
		modelName = pplx.model
		baseURL = pplx.baseURL
	}
	log.Printf("[perplexity_fixer] orgs=%d dry_run=%t concurrency=%d model=%s base_url=%s", len(orgIDs), *dryRun, *concurrency, modelName, baseURL)
	if *dryRun {
		log.Printf("[perplexity_fixer] DRY RUN MODE: no DB writes, no Perplexity calls will be made")
		log.Printf("[perplexity_fixer] To execute for real: PERPLEXITY_API_KEY=... go run ./cmd/perplexity_fixer --dry-run=false --concurrency %d", *concurrency)
	}
	todayStart := utcTodayStart(time.Now())
	log.Printf("[perplexity_fixer] todayStart(UTC)=%s", todayStart.Format(time.RFC3339))

	for idx, orgID := range orgIDs {
		log.Printf("[perplexity_fixer] (%d/%d) org=%s", idx+1, len(orgIDs), orgID)

		orgDetails, err := orgService.GetOrgDetails(ctx, orgID)
		if err != nil {
			log.Printf("[perplexity_fixer] org=%s ERROR get details: %v", orgID, err)
			continue
		}

		perplexityModels := make([]*models.GeoModel, 0)
		for _, m := range orgDetails.Models {
			if isPerplexityModelName(m.Name) {
				perplexityModels = append(perplexityModels, m)
			}
		}
		if len(perplexityModels) == 0 {
			log.Printf("[perplexity_fixer] org=%s skip (no perplexity model configured)", orgID)
			continue
		}

		orgUUID, err := uuid.Parse(orgID)
		if err != nil {
			log.Printf("[perplexity_fixer] org=%s invalid uuid: %v", orgID, err)
			continue
		}

		// Attach runs to today's org batch (create if missing; but NEVER create in dry-run).
		// Note: we only run Perplexity, so totalQuestions here is Perplexity-scoped.
		totalQuestions := len(orgDetails.Questions) * len(perplexityModels) * len(orgDetails.Locations)
		batch, err := findTodaysOrgBatch(ctx, repos, orgUUID, todayStart)
		if err != nil {
			log.Printf("[perplexity_fixer] org=%s ERROR finding today's batch: %v", orgID, err)
			continue
		}

		isExisting := batch != nil
		if !isExisting {
			if *dryRun {
				log.Printf("[perplexity_fixer] org=%s DRY RUN would create today's batch (type=perplexity_fixer total_questions=%d)", orgID, totalQuestions)
			} else {
				createdBatch, err := createOrgBatch(ctx, repos, orgUUID, totalQuestions)
				if err != nil {
					log.Printf("[perplexity_fixer] org=%s ERROR creating today's batch: %v", orgID, err)
					continue
				}
				batch = createdBatch
			}
		}

		batchIDForRuns := uuid.Nil
		batchStatus := ""
		if batch != nil {
			batchIDForRuns = batch.BatchID
			batchStatus = batch.Status
		}
		log.Printf("[perplexity_fixer] org=%s batch=%s (existing=%t status=%s)", orgID, batchIDForRuns, isExisting, batchStatus)

		createdCount := 0
		skippedExisting := 0
		failedJobs := 0

		// Build the full missing-job list first, then execute with a bounded worker pool.
		// This keeps concurrency safe (no duplicate jobs) and avoids doing DB writes inside nested loops.
		jobs := make([]runJob, 0)
		seen := make(map[string]struct{})

		for _, model := range perplexityModels {
			for _, loc := range orgDetails.Locations {
				for _, qwt := range orgDetails.Questions {
					q := qwt.Question

					runs, err := repos.QuestionRunRepo.GetByQuestion(ctx, q.GeoQuestionID)
					if err != nil {
						// Be conservative: schedule the run if we can't verify existence.
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
							batchID: batchIDForRuns,
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
						batchID: batchIDForRuns,
					})
				}
			}
		}

		if len(jobs) == 0 {
			log.Printf("[perplexity_fixer] org=%s done (no missing runs) skipped_existing=%d", orgID, skippedExisting)
			continue
		}

		log.Printf("[perplexity_fixer] org=%s missing_jobs=%d skipped_existing=%d (executing with concurrency=%d)", orgID, len(jobs), skippedExisting, *concurrency)

		jobsCh := make(chan runJob)
		resultsCh := make(chan runJobResult, len(jobs))
		var wg sync.WaitGroup

		worker := func() {
			defer wg.Done()
			for job := range jobsCh {
				if *dryRun {
					resultsCh <- runJobResult{
						job:     job,
						created: true,
						cost:    0,
					}
					continue
				}

				if job.batchID == uuid.Nil {
					resultsCh <- runJobResult{job: job, failed: true, err: fmt.Errorf("missing batch_id (unexpected nil batch in non-dry-run)")}
					continue
				}

				prompt := buildLocalizedPrompt(job.qText, job.loc.CountryCode, job.loc.RegionName)
				resp, err := pplx.chatCompletion(ctx, prompt)
				if err != nil {
					resultsCh <- runJobResult{job: job, failed: true, err: err}
					continue
				}
				if len(resp.Choices) == 0 {
					resultsCh <- runJobResult{job: job, failed: true, err: fmt.Errorf("perplexity response had 0 choices")}
					continue
				}

				content := resp.Choices[0].Message.Content
				inputTokens := resp.Usage.PromptTokens
				outputTokens := resp.Usage.CompletionTokens
				totalCost := resp.Usage.Cost.TotalCost

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
					ResponseText:  &content,
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

		var totalCost float64
		for res := range resultsCh {
			if res.failed {
				failedJobs++
				log.Printf("[perplexity_fixer] org=%s ERROR job question=%s model=%s location=%s: %v",
					orgID, res.job.qID, res.job.model.Name, res.job.loc.CountryCode, res.err)
				continue
			}
			if res.created {
				createdCount++
				totalCost += res.cost
				if *dryRun {
					log.Printf("[perplexity_fixer] DRY RUN would insert run question=%s model=%s location=%s", res.job.qID, res.job.model.Name, res.job.loc.CountryCode)
				}
			}
		}

		log.Printf("[perplexity_fixer] org=%s done created=%d skipped_existing=%d failed=%d total_cost=%.6f", orgID, createdCount, skippedExisting, failedJobs, totalCost)
	}

	log.Printf("[perplexity_fixer] done")
}
