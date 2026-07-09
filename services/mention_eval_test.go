//go:build llmeval

// Behavioral eval for the org mention/extraction PROMPTS against a golden set of
// real + adversarial cases. Unlike normal unit tests this hits the live LLM, so
// it is gated behind the `llmeval` build tag and excluded from CI. Run manually
// when changing any extraction prompt or model:
//
//	make eval
//	# or:
//	go test -tags=llmeval ./services -run TestMentionGolden -v
//
// It replicates the exact production decision (GenerateNameVariations -> substring
// pre-filter -> ExtractOrgEvaluation) and fails if precision/recall/accuracy fall
// below the thresholds below.
package services

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/AI-Template-SDK/senso-workflows/internal/config"
	"github.com/google/uuid"
	"github.com/joho/godotenv"
)

// Pass thresholds. Tune as the golden set grows. Recall guards the regression we
// hit (real mentions marked "No Mention"); precision guards over-correction.
const (
	minRecall    = 0.90 // of expected-true cases, how many we correctly flag
	minPrecision = 0.90 // of the cases we flag true, how many really are
	minAccuracy  = 0.90 // overall correct / total
	evalWorkers  = 5    // concurrent cases (each makes LLM calls)
)

type goldenCase struct {
	ID              string   `json:"id"`
	OrgName         string   `json:"org_name"`
	Websites        []string `json:"websites"`
	Response        string   `json:"response"`
	ExpectMentioned bool     `json:"expect_mentioned"`
	Source          string   `json:"source"`
	Note            string   `json:"note"`
}

type goldenFile struct {
	Cases []goldenCase `json:"cases"`
}

func loadEvalConfig(t *testing.T) *config.Config {
	// Load .env from repo root (tests run in ./services).
	_ = godotenv.Load("../.env")
	_ = godotenv.Load(".env")
	cfg := config.Load()
	if cfg.OpenAIAPIKey == "" && cfg.AzureOpenAIKey == "" {
		t.Skip("no OPENAI_API_KEY / AZURE_OPENAI_KEY configured; skipping LLM eval")
	}
	return cfg
}

func loadGolden(t *testing.T) []goldenCase {
	b, err := os.ReadFile(filepath.Join("testdata", "mention_golden.json"))
	if err != nil {
		t.Fatalf("read golden set: %v", err)
	}
	var gf goldenFile
	if err := json.Unmarshal(b, &gf); err != nil {
		t.Fatalf("parse golden set: %v", err)
	}
	if len(gf.Cases) == 0 {
		t.Fatal("golden set is empty")
	}
	return gf.Cases
}

// decideMentioned replicates the production decision path exactly.
func decideMentioned(ctx context.Context, svc OrgEvaluationService, orgName string, websites, nameVariations []string, response string) (bool, error) {
	lower := strings.ToLower(response)
	prefilter := false
	for _, v := range nameVariations {
		if v != "" && strings.Contains(lower, strings.ToLower(v)) {
			prefilter = true
			break
		}
	}
	if !prefilter {
		return false, nil // pre-filter miss -> not mentioned, no LLM call (matches prod)
	}
	res, err := svc.ExtractOrgEvaluation(ctx, uuid.New(), uuid.New(), orgName, websites, nameVariations, response)
	if err != nil {
		return false, err
	}
	return res.Evaluation.Mentioned, nil
}

func TestMentionGolden(t *testing.T) {
	cfg := loadEvalConfig(t)
	cases := loadGolden(t)
	ctx := context.Background()

	svc := NewOrgEvaluationService(cfg, nil, nil)

	// Generate name variations once per distinct org (as production does per batch).
	type orgKey struct {
		name string
		web  string
	}
	varCache := map[orgKey][]string{}
	var cacheMu sync.Mutex
	getVars := func(name string, websites []string) ([]string, error) {
		k := orgKey{name, strings.Join(websites, ",")}
		cacheMu.Lock()
		if v, ok := varCache[k]; ok {
			cacheMu.Unlock()
			return v, nil
		}
		cacheMu.Unlock()
		v, err := svc.GenerateNameVariations(ctx, name, websites)
		if err != nil {
			return nil, err
		}
		cacheMu.Lock()
		varCache[k] = v
		cacheMu.Unlock()
		return v, nil
	}

	type result struct {
		c       goldenCase
		got     bool
		correct bool
		err     error
	}
	results := make([]result, len(cases))

	sem := make(chan struct{}, evalWorkers)
	var wg sync.WaitGroup
	for i, c := range cases {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, c goldenCase) {
			defer wg.Done()
			defer func() { <-sem }()
			vars, err := getVars(c.OrgName, c.Websites)
			if err != nil {
				results[i] = result{c: c, err: fmt.Errorf("name variations: %w", err)}
				return
			}
			got, err := decideMentioned(ctx, svc, c.OrgName, c.Websites, vars, c.Response)
			results[i] = result{c: c, got: got, correct: err == nil && got == c.ExpectMentioned, err: err}
		}(i, c)
	}
	wg.Wait()

	// Tally.
	var tp, fp, tn, fn, errs int
	var failures []string
	for _, r := range results {
		if r.err != nil {
			errs++
			failures = append(failures, fmt.Sprintf("  [ERROR] %-32s %v", r.c.ID, r.err))
			continue
		}
		switch {
		case r.c.ExpectMentioned && r.got:
			tp++
		case r.c.ExpectMentioned && !r.got:
			fn++
			failures = append(failures, fmt.Sprintf("  [FN] %-32s expected mentioned=true, got false  (%s)", r.c.ID, r.c.Note))
		case !r.c.ExpectMentioned && r.got:
			fp++
			failures = append(failures, fmt.Sprintf("  [FP] %-32s expected mentioned=false, got true  (%s)", r.c.ID, r.c.Note))
		default:
			tn++
		}
	}

	total := len(results)
	recall := ratio(tp, tp+fn)
	precision := ratio(tp, tp+fp)
	accuracy := ratio(tp+tn, total)

	sort.Strings(failures)
	t.Logf("\n=== Mention golden eval (%d cases) ===\n"+
		"TP=%d FP=%d TN=%d FN=%d errors=%d\n"+
		"recall=%.2f (min %.2f)  precision=%.2f (min %.2f)  accuracy=%.2f (min %.2f)\n%s",
		total, tp, fp, tn, fn, errs, recall, minRecall, precision, minPrecision, accuracy, minAccuracy,
		strings.Join(append([]string{"failures:"}, failures...), "\n"))

	if errs > 0 {
		t.Fatalf("%d case(s) errored during eval", errs)
	}
	if recall < minRecall {
		t.Errorf("recall %.2f < %.2f", recall, minRecall)
	}
	if precision < minPrecision {
		t.Errorf("precision %.2f < %.2f", precision, minPrecision)
	}
	if accuracy < minAccuracy {
		t.Errorf("accuracy %.2f < %.2f", accuracy, minAccuracy)
	}
}

func ratio(n, d int) float64 {
	if d == 0 {
		return 1
	}
	return float64(n) / float64(d)
}
