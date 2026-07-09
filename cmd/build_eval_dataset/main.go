// build_eval_dataset sources real question-run responses from the prod DB and
// emits labeled candidate cases for the mention golden set
// (services/testdata/mention_golden.json).
//
// Labels are HEURISTIC and must be human-reviewed before merging into the
// golden file:
//   - positive (expect_mentioned=true):  response contains the org's canonical
//     brand name (its display name with any trailing (…)/| qualifier stripped)
//   - negative (expect_mentioned=false): response contains none of the brand's
//     significant word tokens
//
// Cases whose label is ambiguous (brand token present but not the canonical
// name — e.g. a competitor, a different org, or a generic word) are skipped so
// the operator only reviews high-confidence candidates.
//
// Usage:
//
//	go run ./cmd/build_eval_dataset --org-ids <uuid[,uuid...]> --pos 4 --neg 3 --out candidates.json
//
// Then review candidates.json and paste the good cases into
// services/testdata/mention_golden.json (setting source:"prod").
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/AI-Template-SDK/senso-api/pkg/database"
	"github.com/AI-Template-SDK/senso-workflows/internal/config"
	"github.com/AI-Template-SDK/senso-workflows/services"
	"github.com/jmoiron/sqlx"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
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

func stripQualifier(name string) string {
	for _, sep := range []string{"(", "|"} {
		if i := strings.Index(name, sep); i > 0 {
			name = name[:i]
		}
	}
	return strings.TrimRight(strings.TrimSpace(name), ",")
}

// significantTokens returns lowercase words of length >= 4 from the canonical
// name, excluding a few generic industry words that overlap many responses.
func significantTokens(canonical string) []string {
	stop := map[string]bool{
		"credit": true, "union": true, "bank": true, "financial": true,
		"community": true, "company": true, "group": true, "services": true,
		"solutions": true, "association": true, "national": true, "federal": true,
	}
	var out []string
	for _, w := range strings.Fields(strings.ToLower(canonical)) {
		w = strings.Trim(w, ".,&-")
		if len(w) >= 4 && !stop[w] {
			out = append(out, w)
		}
	}
	return out
}

type runRow struct {
	ID    string `db:"question_run_id"`
	Model string `db:"run_model"`
	Resp  string `db:"response_text"`
}

func main() {
	var (
		orgIDs  = flag.String("org-ids", "", "comma-separated org UUIDs to sample")
		pos     = flag.Int("pos", 4, "positive candidates per org")
		neg     = flag.Int("neg", 3, "negative candidates per org")
		maxLen  = flag.Int("max-response-len", 6000, "skip responses longer than this (keep the file readable)")
		outPath = flag.String("out", "eval_candidates.json", "output JSON path")
	)
	flag.Parse()
	if strings.TrimSpace(*orgIDs) == "" {
		log.Fatal("--org-ids is required")
	}

	_ = godotenv.Load()
	cfg := config.Load()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	connStr := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		cfg.Database.Host, cfg.Database.Port, cfg.Database.User, cfg.Database.Password, cfg.Database.Name, cfg.Database.SSLMode)
	db, err := sqlx.ConnectContext(ctx, "postgres", connStr)
	if err != nil {
		log.Fatalf("db connect: %v", err)
	}
	defer db.Close()
	repos := services.NewRepositoryManager(&database.Client{DB: db})
	orgSvc := services.NewOrgService(cfg, repos)

	var cases []goldenCase
	for _, orgID := range strings.Split(*orgIDs, ",") {
		orgID = strings.TrimSpace(orgID)
		if orgID == "" {
			continue
		}
		details, err := orgSvc.GetOrgDetails(ctx, orgID)
		if err != nil {
			log.Printf("org %s: GetOrgDetails failed: %v", orgID, err)
			continue
		}
		name := details.Org.Name
		canonical := stripQualifier(name)
		tokens := significantTokens(canonical)
		if len(tokens) == 0 {
			log.Printf("org %s (%q): no significant tokens, skipping negatives", orgID, name)
		}

		// Positives: canonical name literally present.
		var posRows []runRow
		_ = db.SelectContext(ctx, &posRows, `
			SELECT r.question_run_id, coalesce(r.run_model,'') run_model, r.response_text
			FROM question_runs r
			WHERE r.geo_question_id IN (SELECT geo_question_id FROM geo_questions WHERE org_id=$1)
			  AND r.is_latest AND r.response_text IS NOT NULL
			  AND length(r.response_text) <= $2
			  AND position(lower($3) in lower(r.response_text)) > 0
			ORDER BY length(r.response_text) DESC LIMIT $4`,
			orgID, *maxLen, canonical, *pos)
		for i, r := range posRows {
			cases = append(cases, goldenCase{
				ID: fmt.Sprintf("prod-%s-pos-%d", short(orgID), i+1), OrgName: name,
				Websites: details.Websites, Response: r.Resp, ExpectMentioned: true, Source: "prod",
				Note: fmt.Sprintf("AUTO-LABELED true (canonical %q present, model=%s). REVIEW.", canonical, r.Model),
			})
		}

		// Negatives: none of the significant brand tokens appear.
		if len(tokens) > 0 {
			var conds []string
			args := []any{orgID, *maxLen}
			for _, tk := range tokens {
				args = append(args, "%"+tk+"%")
				conds = append(conds, fmt.Sprintf("r.response_text NOT ILIKE $%d", len(args)))
			}
			args = append(args, *neg)
			var negRows []runRow
			q := fmt.Sprintf(`
				SELECT r.question_run_id, coalesce(r.run_model,'') run_model, r.response_text
				FROM question_runs r
				WHERE r.geo_question_id IN (SELECT geo_question_id FROM geo_questions WHERE org_id=$1)
				  AND r.is_latest AND r.response_text IS NOT NULL
				  AND length(r.response_text) <= $2 AND %s
				ORDER BY length(r.response_text) DESC LIMIT $%d`,
				strings.Join(conds, " AND "), len(args))
			_ = db.SelectContext(ctx, &negRows, q, args...)
			for i, r := range negRows {
				cases = append(cases, goldenCase{
					ID: fmt.Sprintf("prod-%s-neg-%d", short(orgID), i+1), OrgName: name,
					Websites: details.Websites, Response: r.Resp, ExpectMentioned: false, Source: "prod",
					Note: fmt.Sprintf("AUTO-LABELED false (no brand tokens %v, model=%s). REVIEW.", tokens, r.Model),
				})
			}
		}
		log.Printf("org %q: sampled candidates", name)
	}

	out := map[string]any{
		"_comment": "AUTO-GENERATED candidates from build_eval_dataset. Review each label, then merge good ones into services/testdata/mention_golden.json.",
		"cases":    cases,
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	if err := os.WriteFile(*outPath, b, 0o644); err != nil {
		log.Fatalf("write %s: %v", *outPath, err)
	}
	log.Printf("wrote %d candidate cases to %s", len(cases), *outPath)
}

func short(id string) string {
	if len(id) >= 8 {
		return id[:8]
	}
	return id
}
