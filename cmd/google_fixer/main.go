package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
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

// BrightData SERP API types (inlined from services/aioverview_provider.go).

type serpRequest struct {
	Zone   string `json:"zone"`
	URL    string `json:"url"`
	Format string `json:"format"`
}

type serpResponse struct {
	General    serpGeneral     `json:"general"`
	Input      serpInput       `json:"input"`
	AIOverview *serpAIOverview `json:"ai_overview"`
	Organic    []serpOrganic   `json:"organic"`
}

type serpGeneral struct {
	SearchEngine string `json:"search_engine"`
	Query        string `json:"query"`
	ResultsCnt   int    `json:"results_cnt"`
	CountryCode  string `json:"country_code"`
}

type serpInput struct {
	OriginalURL string `json:"original_url"`
	RequestID   string `json:"request_id"`
}

type serpAIOverview struct {
	Texts      []serpText      `json:"texts"`
	References []serpReference `json:"references"`
}

type serpText struct {
	Type             string     `json:"type"`
	Snippet          string     `json:"snippet"`
	Title            string     `json:"title,omitempty"`
	List             []serpText `json:"list,omitempty"`
	ReferenceIndexes []int      `json:"reference_indexes,omitempty"`
}

type serpReference struct {
	Href   string `json:"href"`
	Title  string `json:"title"`
	Source string `json:"source"`
	Index  int    `json:"index"`
}

type serpOrganic struct {
	Link        string `json:"link"`
	Source      string `json:"source"`
	DisplayLink string `json:"display_link"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Rank        int    `json:"rank"`
	GlobalRank  int    `json:"global_rank"`
}

type serpResult struct {
	responseText string
	citations    []string
	cost         float64
}

type brightdataClient struct {
	apiKey     string
	zone       string
	baseURL    string
	httpClient *http.Client
}

func newBrightdataClientFromEnv() (*brightdataClient, error) {
	apiKey := strings.TrimSpace(os.Getenv("BRIGHTDATA_SERP_API_KEY"))
	if apiKey == "" {
		return nil, fmt.Errorf("BRIGHTDATA_SERP_API_KEY is not set")
	}
	return &brightdataClient{
		apiKey:  apiKey,
		zone:    "serp_api1",
		baseURL: "https://api.brightdata.com/request",
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}, nil
}

func normalizeCountryCode(code string) string {
	c := strings.ToUpper(strings.TrimSpace(code))
	if c == "" {
		return "US"
	}
	if c == "UK" {
		return "GB"
	}
	return c
}

func (bd *brightdataClient) search(ctx context.Context, query string, countryCode string) (*serpResult, error) {
	cc := normalizeCountryCode(countryCode)
	searchURL := fmt.Sprintf(
		"https://www.google.com/search?q=%s&gl=%s&brd_json=1&brd_ai_overview=2",
		url.QueryEscape(query), cc,
	)

	payload, err := json.Marshal(serpRequest{
		Zone:   bd.zone,
		URL:    searchURL,
		Format: "raw",
	})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	maxRetries := 3
	var lastErr error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, bd.baseURL, bytes.NewReader(payload))
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+bd.apiKey)
		req.Header.Set("Content-Type", "application/json")

		resp, err := bd.httpClient.Do(req)
		if err != nil {
			lastErr = err
			if attempt < maxRetries {
				time.Sleep(2 * time.Second)
				continue
			}
			break
		}
		defer resp.Body.Close()

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			bodyBytes, _ := io.ReadAll(resp.Body)
			lastErr = fmt.Errorf("brightdata http %d: %s", resp.StatusCode, strings.TrimSpace(string(bodyBytes)))
			if attempt < maxRetries {
				time.Sleep(2 * time.Second)
				continue
			}
			break
		}

		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("read response: %w", err)
		}

		var parsed serpResponse
		if err := json.Unmarshal(bodyBytes, &parsed); err != nil {
			return nil, fmt.Errorf("decode response: %w", err)
		}

		return processAIOverviewResponse(&parsed), nil
	}

	return nil, fmt.Errorf("request failed after %d attempts: %w", maxRetries, lastErr)
}

func processAIOverviewResponse(result *serpResponse) *serpResult {
	var responseText string

	if result.AIOverview == nil || len(result.AIOverview.Texts) == 0 {
		responseText = "No AI Overview was generated for this query. Google did not provide an AI-generated summary for this search."
	} else {
		var parts []string
		for _, t := range result.AIOverview.Texts {
			if s := extractTextBlock(t); s != "" {
				parts = append(parts, s)
			}
		}
		responseText = strings.Join(parts, "\n\n")
	}

	var citations []string
	if len(result.Organic) > 0 {
		var sources []string
		for _, o := range result.Organic {
			if o.Link != "" {
				sources = append(sources, o.Link)
				citations = append(citations, o.Link)
			}
		}
		if len(sources) > 0 {
			responseText += "\n\nSources:\n"
			for _, s := range sources {
				responseText += fmt.Sprintf("- %s\n", s)
			}
		}
	}

	return &serpResult{
		responseText: responseText,
		citations:    citations,
		cost:         0.0015, // $1.50/1000 requests
	}
}

func extractTextBlock(t serpText) string {
	switch t.Type {
	case "paragraph":
		return t.Snippet
	case "list":
		var parts []string
		if t.Title != "" {
			parts = append(parts, t.Title)
		}
		for _, item := range t.List {
			if item.Snippet != "" {
				parts = append(parts, "- "+item.Snippet)
			}
		}
		return strings.Join(parts, "\n")
	default:
		return t.Snippet
	}
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
		BatchType:          "google_fixer",
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
		concurrency     = flag.Int("concurrency", 3, "number of concurrent AI Overview calls/inserts per org (bounded)")
		maxOrgs         = flag.Int("max-orgs", 0, "optional max orgs to process (0 = all)")
		timeout         = flag.Duration("timeout", 30*time.Minute, "overall timeout for the script")
		writeModelMatch = flag.String("write-model", "aioverview", "geo_models name (or substring) to backfill (e.g. 'aioverview'); runs will be written using that model_id/name")
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

	var bd *brightdataClient
	if !*dryRun {
		client, err := newBrightdataClientFromEnv()
		if err != nil {
			log.Fatalf("BrightData client init failed: %v", err)
		}
		bd = client
	}

	orgIDs, err := readOrgIDs(*orgFile)
	if err != nil {
		log.Fatalf("Failed reading org list: %v", err)
	}
	if *maxOrgs > 0 && *maxOrgs < len(orgIDs) {
		orgIDs = orgIDs[:*maxOrgs]
	}

	log.Printf("[google_fixer] orgs=%d dry_run=%t concurrency=%d write_model_match=%s", len(orgIDs), *dryRun, *concurrency, *writeModelMatch)
	if *dryRun {
		log.Printf("[google_fixer] DRY RUN MODE: no DB writes, no AI Overview calls will be made")
		log.Printf("[google_fixer] To execute for real: go run ./cmd/google_fixer --dry-run=false --write-model %s --concurrency %d", *writeModelMatch, *concurrency)
	}
	todayStart := utcTodayStart(time.Now())
	log.Printf("[google_fixer] todayStart(UTC)=%s", todayStart.Format(time.RFC3339))

	for idx, orgID := range orgIDs {
		log.Printf("[google_fixer] (%d/%d) org=%s", idx+1, len(orgIDs), orgID)

		orgDetails, err := orgService.GetOrgDetails(ctx, orgID)
		if err != nil {
			log.Printf("[google_fixer] org=%s ERROR get details: %v", orgID, err)
			continue
		}

		// Backfill ALL org geo_models that match write-model (typically "aioverview").
		selectedModels := make([]*models.GeoModel, 0)
		for _, m := range orgDetails.Models {
			if modelNameContains(m.Name, *writeModelMatch) || modelNameMatches(m.Name, *writeModelMatch) {
				selectedModels = append(selectedModels, m)
			}
		}
		if len(selectedModels) == 0 {
			log.Printf("[google_fixer] org=%s skip (no geo model matching %q configured on org)", orgID, *writeModelMatch)
			continue
		}

		orgUUID, err := uuid.Parse(orgID)
		if err != nil {
			log.Printf("[google_fixer] org=%s invalid uuid: %v", orgID, err)
			continue
		}

		// Attach runs to today's org batch (create if missing; but NEVER create in dry-run).
		totalQuestions := len(orgDetails.Questions) * len(selectedModels) * len(orgDetails.Locations)
		batch, err := findTodaysOrgBatch(ctx, repos, orgUUID, todayStart)
		if err != nil {
			log.Printf("[google_fixer] org=%s ERROR finding today's batch: %v", orgID, err)
			continue
		}

		isExisting := batch != nil
		if !isExisting {
			if *dryRun {
				log.Printf("[google_fixer] org=%s DRY RUN would create today's batch (type=google_fixer total_questions=%d)", orgID, totalQuestions)
			} else {
				createdBatch, err := createOrgBatch(ctx, repos, orgUUID, totalQuestions)
				if err != nil {
					log.Printf("[google_fixer] org=%s ERROR creating today's batch: %v", orgID, err)
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
		log.Printf("[google_fixer] org=%s batch=%s (existing=%t status=%s)", orgID, batchID, isExisting, batchStatus)

		// Build missing jobs for question x model x location (write-model(s)).
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
			log.Printf("[google_fixer] org=%s done (no missing runs) skipped_existing=%d", orgID, skippedExisting)
			continue
		}
		log.Printf("[google_fixer] org=%s missing_jobs=%d skipped_existing=%d (executing with concurrency=%d)", orgID, len(jobs), skippedExisting, *concurrency)

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

				result, err := bd.search(ctx, job.qText, job.loc.CountryCode)
				if err != nil {
					resultsCh <- runJobResult{job: job, failed: true, err: err}
					continue
				}

				responseText := result.responseText
				inputTokens := 0
				outputTokens := 0
				totalCost := result.cost

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
				log.Printf("[google_fixer] org=%s ERROR job question=%s location=%s: %v",
					orgID, res.job.qID, res.job.loc.CountryCode, res.err)
				continue
			}
			if res.created {
				createdCount++
				totalCost += res.cost
				if *dryRun {
					log.Printf("[google_fixer] DRY RUN would insert run question=%s model=%s location=%s", res.job.qID, res.job.model.Name, res.job.loc.CountryCode)
				}
			}
		}

		log.Printf("[google_fixer] org=%s done created=%d skipped_existing=%d failed=%d total_cost=%.6f", orgID, createdCount, skippedExisting, failedCount, totalCost)
	}

	log.Printf("[google_fixer] done")
}
