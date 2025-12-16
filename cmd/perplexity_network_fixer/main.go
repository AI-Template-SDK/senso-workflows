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
	"github.com/AI-Template-SDK/senso-api/pkg/repositories/interfaces"
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

type perplexityChatRequest struct {
	Model    string                  `json:"model"`
	Messages []perplexityChatMessage `json:"messages"`
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
			TotalCost float64 `json:"total_cost"`
		} `json:"cost"`
	} `json:"usage"`
	Choices []struct {
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

func isPerplexityModelName(name string) bool {
	return strings.Contains(strings.ToLower(name), "perplexity")
}

// Mirrors provider localization prompt format (region, country).
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
		BatchType:          "perplexity_network_fixer",
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

type runJob struct {
	networkID string
	qID       uuid.UUID
	qText     string
	modelName string
	country   string
	region    *string
	batchID   uuid.UUID
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
		concurrency = flag.Int("concurrency", 5, "number of concurrent Perplexity calls/inserts per network (bounded)")
		maxNetworks = flag.Int("max-networks", 0, "optional max networks to process (0 = all)")
		timeout     = flag.Duration("timeout", 30*time.Minute, "overall timeout for the script")
	)
	flag.Parse()

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

	if *concurrency < 1 {
		log.Fatalf("--concurrency must be >= 1")
	}

	var pplx *perplexityClient
	if !*dryRun {
		pplxClient, err := newPerplexityClientFromEnv()
		if err != nil {
			log.Fatalf("Perplexity client init failed: %v", err)
		}
		pplx = pplxClient
	}

	networkIDs, err := readIDs(*networkFile)
	if err != nil {
		log.Fatalf("Failed reading network list: %v", err)
	}
	if *maxNetworks > 0 && *maxNetworks < len(networkIDs) {
		networkIDs = networkIDs[:*maxNetworks]
	}

	modelName := ""
	baseURL := ""
	if pplx != nil {
		modelName = pplx.model
		baseURL = pplx.baseURL
	}
	log.Printf("[perplexity_network_fixer] networks=%d dry_run=%t concurrency=%d model=%s base_url=%s", len(networkIDs), *dryRun, *concurrency, modelName, baseURL)
	if *dryRun {
		log.Printf("[perplexity_network_fixer] DRY RUN MODE: no DB writes, no Perplexity calls will be made")
		log.Printf("[perplexity_network_fixer] To execute for real: PERPLEXITY_API_KEY=... go run ./cmd/perplexity_network_fixer --dry-run=false --concurrency %d", *concurrency)
	}

	todayStart := utcTodayStart(time.Now())
	log.Printf("[perplexity_network_fixer] todayStart(UTC)=%s", todayStart.Format(time.RFC3339))

	for idx, networkID := range networkIDs {
		log.Printf("[perplexity_network_fixer] (%d/%d) network=%s", idx+1, len(networkIDs), networkID)

		networkUUID, err := uuid.Parse(networkID)
		if err != nil {
			log.Printf("[perplexity_network_fixer] network=%s invalid uuid: %v", networkID, err)
			continue
		}

		// Determine configured network models (do NOT fallback like the pipeline).
		modelNames, err := repos.NetworkModelRepo.GetByNetworkID(ctx, networkUUID)
		if err != nil {
			log.Printf("[perplexity_network_fixer] network=%s ERROR get network models: %v", networkID, err)
			continue
		}
		if len(modelNames) == 0 {
			log.Printf("[perplexity_network_fixer] network=%s skip (no network models configured; not using fallback defaults)", networkID)
			continue
		}

		perplexityModelNames := make([]string, 0)
		for _, name := range modelNames {
			if isPerplexityModelName(name) {
				perplexityModelNames = append(perplexityModelNames, name)
			}
		}
		if len(perplexityModelNames) == 0 {
			log.Printf("[perplexity_network_fixer] network=%s skip (no perplexity model configured)", networkID)
			continue
		}

		// Load questions + locations (with same US-location fallback behavior as pipeline).
		networkQuestions, networkLocations, err := loadNetworkQuestionsAndLocations(ctx, repos, networkUUID)
		if err != nil {
			log.Printf("[perplexity_network_fixer] network=%s ERROR load questions/locations: %v", networkID, err)
			continue
		}

		// Find/create today's network batch.
		totalQuestions := len(networkQuestions) * len(perplexityModelNames) * len(networkLocations)
		batch, err := findTodaysNetworkBatch(ctx, repos, networkUUID, todayStart)
		if err != nil {
			log.Printf("[perplexity_network_fixer] network=%s ERROR finding today's batch: %v", networkID, err)
			continue
		}
		isExisting := batch != nil
		if !isExisting {
			if *dryRun {
				log.Printf("[perplexity_network_fixer] network=%s DRY RUN would create today's batch (type=perplexity_network_fixer total_questions=%d)", networkID, totalQuestions)
			} else {
				createdBatch, err := createNetworkBatch(ctx, repos, networkUUID, totalQuestions)
				if err != nil {
					log.Printf("[perplexity_network_fixer] network=%s ERROR creating today's batch: %v", networkID, err)
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
		log.Printf("[perplexity_network_fixer] network=%s batch=%s (existing=%t status=%s)", networkID, batchID, isExisting, batchStatus)

		// Build missing job list (question × model × location).
		jobs := make([]runJob, 0)
		seen := make(map[string]struct{})
		skippedExisting := 0

		for _, modelName := range perplexityModelNames {
			for _, loc := range networkLocations {
				for _, qwt := range networkQuestions {
					q := qwt.Question

					runs, err := repos.QuestionRunRepo.GetByQuestion(ctx, q.GeoQuestionID)
					if err != nil {
						key := fmt.Sprintf("%s|%s|%s|%s", q.GeoQuestionID, modelName, loc.CountryCode, regionString(loc.RegionName))
						if _, ok := seen[key]; ok {
							continue
						}
						seen[key] = struct{}{}
						jobs = append(jobs, runJob{
							networkID: networkID,
							qID:       q.GeoQuestionID,
							qText:     q.QuestionText,
							modelName: modelName,
							country:   loc.CountryCode,
							region:    loc.RegionName,
							batchID:   batchID,
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
						if *run.RunModel != modelName || *run.RunCountry != loc.CountryCode {
							continue
						}
						// If we have a region, match it; otherwise ignore region.
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

					key := fmt.Sprintf("%s|%s|%s|%s", q.GeoQuestionID, modelName, loc.CountryCode, regionString(loc.RegionName))
					if _, ok := seen[key]; ok {
						continue
					}
					seen[key] = struct{}{}

					jobs = append(jobs, runJob{
						networkID: networkID,
						qID:       q.GeoQuestionID,
						qText:     q.QuestionText,
						modelName: modelName,
						country:   loc.CountryCode,
						region:    loc.RegionName,
						batchID:   batchID,
					})
				}
			}
		}

		if len(jobs) == 0 {
			log.Printf("[perplexity_network_fixer] network=%s done (no missing runs) skipped_existing=%d", networkID, skippedExisting)
			continue
		}
		log.Printf("[perplexity_network_fixer] network=%s missing_jobs=%d skipped_existing=%d (executing with concurrency=%d)", networkID, len(jobs), skippedExisting, *concurrency)

		jobsCh := make(chan runJob)
		resultsCh := make(chan runJobResult, len(jobs))
		var wg sync.WaitGroup

		worker := func() {
			defer wg.Done()
			for job := range jobsCh {
				if *dryRun {
					resultsCh <- runJobResult{job: job, created: true, cost: 0}
					continue
				}
				if job.batchID == uuid.Nil {
					resultsCh <- runJobResult{job: job, failed: true, err: fmt.Errorf("missing batch_id (unexpected nil batch in non-dry-run)")}
					continue
				}

				prompt := buildLocalizedPrompt(job.qText, job.country, job.region)
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

				runModel := job.modelName
				runCountry := job.country

				now := time.Now()
				qr := &models.QuestionRun{
					QuestionRunID: uuid.New(),
					GeoQuestionID: job.qID,
					// Network runs use string fields, not model_id/location_id
					ResponseText: &content,
					InputTokens:  &inputTokens,
					OutputTokens: &outputTokens,
					TotalCost:    &totalCost,
					BatchID:      &job.batchID,
					RunModel:     &runModel,
					RunCountry:   &runCountry,
					RunRegion:    job.region, // may be nil (matches pipeline)
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
				log.Printf("[perplexity_network_fixer] network=%s ERROR job question=%s model=%s location=%s: %v",
					networkID, res.job.qID, res.job.modelName, res.job.country, res.err)
				continue
			}
			if res.created {
				createdCount++
				totalCost += res.cost
				if *dryRun {
					log.Printf("[perplexity_network_fixer] DRY RUN would insert run question=%s model=%s location=%s", res.job.qID, res.job.modelName, res.job.country)
				}
			}
		}

		log.Printf("[perplexity_network_fixer] network=%s done created=%d skipped_existing=%d failed=%d total_cost=%.6f", networkID, createdCount, skippedExisting, failedCount, totalCost)
	}

	log.Printf("[perplexity_network_fixer] done")
}
