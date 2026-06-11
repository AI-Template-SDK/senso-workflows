// Command reclassify_citations is a one-off backfill that restores the OLD
// citation classification: a citation is "primary" when its base domain (eTLD+1)
// matches one of the organization's websites (org_websites), otherwise "secondary".
//
// Background: the tracked-source classifier (citationclass + org_tracked_sources)
// replaced the org_websites domain match. For orgs whose org_tracked_sources was
// never backfilled, every citation collapsed to "secondary", breaking owned/primary
// domain matching. The live write path has been reverted; this tool repairs the
// rows that were already written with the broken classification.
//
// Tables repaired:
//   - org_citations          (type:  recompute primary/secondary, restamp host/base_domain)
//   - network_org_citations  (type:  recompute primary/secondary, restamp host/base_domain)
//   - question_run_citations  (citation_type: only rows wrongly promoted to 'tracked')
//
// The classification helpers (getBaseDomain / isPrimaryDomain) are duplicated from
// services on purpose — like the other one-off tools in cmd/, this is intentionally
// standalone and must not drift onto the live request path.
//
// Usage:
//
//	go run ./cmd/reclassify_citations            # dry-run, reports what WOULD change
//	go run ./cmd/reclassify_citations -apply     # actually writes the updates
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/joho/godotenv"
	"github.com/lib/pq"
	"golang.org/x/net/publicsuffix"

	"github.com/AI-Template-SDK/senso-workflows/internal/config"
	"github.com/google/uuid"
)

// pageSize controls keyset pagination over the large citation tables.
const pageSize = 20000

// bulkUpdateType rewrites the tier column for the given ids in a single round-trip,
// using unnest() arrays instead of one UPDATE per row (the prior per-row approach
// was ~100ms/row over the DB tunnel — hours for hundreds of thousands of rows).
func bulkUpdateType(ctx context.Context, tx *sqlx.Tx, table, pkCol, setCol string, ids, types []string) error {
	if len(ids) == 0 {
		return nil
	}
	q := fmt.Sprintf(
		`UPDATE %s AS t SET %s = d.typ, updated_at = now()
		 FROM (SELECT unnest($1::uuid[]) AS id, unnest($2::text[]) AS typ) AS d
		 WHERE t.%s = d.id`, table, setCol, pkCol)
	if _, err := tx.ExecContext(ctx, q, pq.Array(ids), pq.Array(types)); err != nil {
		return fmt.Errorf("bulk update %s: %w", table, err)
	}
	return nil
}

func connect(ctx context.Context, cfg config.DatabaseConfig) (*sqlx.DB, error) {
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
	return db, nil
}

// getBaseDomain extracts the base domain (eTLD+1) from a URL using publicsuffix.
// Duplicated from services.getBaseDomain to keep this one-off tool standalone.
func getBaseDomain(urlStr string) (string, error) {
	if !strings.HasPrefix(urlStr, "http://") && !strings.HasPrefix(urlStr, "https://") {
		urlStr = "https://" + urlStr
	}
	u, err := url.Parse(urlStr)
	if err != nil {
		return "", fmt.Errorf("failed to parse URL %s: %w", urlStr, err)
	}
	hostname := u.Hostname()
	if hostname == "" {
		return "", fmt.Errorf("no hostname found in URL: %s", urlStr)
	}
	baseDomain, err := publicsuffix.EffectiveTLDPlusOne(hostname)
	if err != nil {
		return "", fmt.Errorf("failed to get base domain for %s: %w", hostname, err)
	}
	return baseDomain, nil
}

// isPrimaryDomain reports whether the citation URL's base domain matches any of the
// org's website domains. Duplicated from services.isPrimaryDomain.
func isPrimaryDomain(citationURL string, orgDomains []string) bool {
	citationBase, err := getBaseDomain(citationURL)
	if err != nil {
		return false
	}
	for _, orgDomain := range orgDomains {
		orgBase, err := getBaseDomain(orgDomain)
		if err != nil {
			continue
		}
		if strings.EqualFold(citationBase, orgBase) {
			return true
		}
	}
	return false
}

// classifyType returns the old-logic tier for a citation URL.
func classifyType(citationURL string, websites []string) string {
	if isPrimaryDomain(citationURL, websites) {
		return "primary"
	}
	return "secondary"
}

// loadOrgWebsites builds org_id -> []website-url from org_websites.
func loadOrgWebsites(ctx context.Context, db *sqlx.DB) (map[uuid.UUID][]string, error) {
	rows, err := db.QueryxContext(ctx, `SELECT org_id, url FROM org_websites WHERE deleted_at IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[uuid.UUID][]string)
	for rows.Next() {
		var orgID uuid.UUID
		var u string
		if err := rows.Scan(&orgID, &u); err != nil {
			return nil, err
		}
		out[orgID] = append(out[orgID], u)
	}
	return out, rows.Err()
}

// reclassifyOrgLike repairs the tier column on org_citations / network_org_citations.
// Both share the same shape: <pk>, org_id, url, type. Only rows whose tier actually
// changes are written (host/base_domain were stamped correctly by the live code and
// are not the bug), and writes are bulked one statement per page.
func reclassifyOrgLike(ctx context.Context, db *sqlx.DB, table, pkCol string, websites map[uuid.UUID][]string, cutoff time.Time, apply bool) error {
	log.Printf("[%s] starting (apply=%v, since=%s)", table, apply, cutoff.Format(time.RFC3339))

	var (
		lastID           = uuid.Nil
		scanned, flipped int
		missingWebsites  int
		sampleLogsLeft   = 10
		typeFlips        = map[string]int{}
	)

	for {
		q := fmt.Sprintf(
			`SELECT %s, org_id, url, type FROM %s
			 WHERE deleted_at IS NULL AND created_at >= $1 AND %s > $2
			 ORDER BY %s ASC LIMIT $3`, pkCol, table, pkCol, pkCol)
		rows, err := db.QueryxContext(ctx, q, cutoff, lastID, pageSize)
		if err != nil {
			return fmt.Errorf("query %s: %w", table, err)
		}

		var (
			pageIDs    []string
			pageTypes  []string
			rowsInPage int
		)
		for rows.Next() {
			var id, orgID uuid.UUID
			var u, typ string
			if err := rows.Scan(&id, &orgID, &u, &typ); err != nil {
				rows.Close()
				return fmt.Errorf("scan %s: %w", table, err)
			}
			rowsInPage++
			scanned++
			lastID = id

			sites, ok := websites[orgID]
			if !ok || len(sites) == 0 {
				missingWebsites++
			}

			newType := classifyType(u, sites)
			if newType == typ {
				continue // tier already correct — skip (host/base_domain untouched)
			}

			flipped++
			typeFlips[typ+"->"+newType]++
			pageIDs = append(pageIDs, id.String())
			pageTypes = append(pageTypes, newType)
			if sampleLogsLeft > 0 {
				log.Printf("[%s] %s  %q  %s -> %s", table, id, u, typ, newType)
				sampleLogsLeft--
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()

		if rowsInPage == 0 {
			break
		}

		if apply && len(pageIDs) > 0 {
			tx, err := db.BeginTxx(ctx, nil)
			if err != nil {
				return err
			}
			if err := bulkUpdateType(ctx, tx, table, pkCol, "type", pageIDs, pageTypes); err != nil {
				_ = tx.Rollback()
				return err
			}
			if err := tx.Commit(); err != nil {
				return err
			}
		}

		log.Printf("[%s] progress: scanned=%d flipped=%d", table, scanned, flipped)
	}

	log.Printf("[%s] DONE scanned=%d flipped=%d (%v) rows_with_no_org_websites=%d",
		table, scanned, flipped, typeFlips, missingWebsites)
	return nil
}

type claimCitRow struct {
	id    uuid.UUID
	orgID uuid.UUID
	url   string
	typ   string
}

// reclassifyClaimCitations repairs question_run_citations rows that the regression
// promoted to 'tracked'. Their primary/secondary value originally came from the LLM;
// only the 'tracked' promotion was new, so we only touch 'tracked' rows and map them
// back to primary/secondary via the org_websites domain match. org_id is reached via
// claim -> question_run -> geo_question.
func reclassifyClaimCitations(ctx context.Context, db *sqlx.DB, websites map[uuid.UUID][]string, cutoff time.Time, apply bool) error {
	const table = "question_run_citations"
	log.Printf("[%s] starting (apply=%v, since=%s) — only citation_type='tracked'", table, apply, cutoff.Format(time.RFC3339))

	q := `
		SELECT c.question_run_citation_id, gq.org_id, c.source_url
		FROM question_run_citations c
		JOIN question_run_claims cl ON cl.question_run_claim_id = c.question_run_claim_id
		JOIN question_runs r ON r.question_run_id = cl.question_run_id
		JOIN geo_questions gq ON gq.geo_question_id = r.geo_question_id
		WHERE c.deleted_at IS NULL
		  AND c.created_at >= $1
		  AND c.citation_type = 'tracked'
		  AND c.source_url IS NOT NULL
		  AND gq.org_id IS NOT NULL`

	rows, err := db.QueryxContext(ctx, q, cutoff)
	if err != nil {
		return fmt.Errorf("query %s: %w", table, err)
	}

	var candidates []claimCitRow
	for rows.Next() {
		var r claimCitRow
		if err := rows.Scan(&r.id, &r.orgID, &r.url); err != nil {
			rows.Close()
			return fmt.Errorf("scan %s: %w", table, err)
		}
		r.typ = "tracked"
		candidates = append(candidates, r)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	log.Printf("[%s] found %d 'tracked' rows to remap", table, len(candidates))

	var ids, types []string
	for i, r := range candidates {
		newType := classifyType(r.url, websites[r.orgID])
		ids = append(ids, r.id.String())
		types = append(types, newType)
		if i < 10 {
			log.Printf("[%s] %s  %q  tracked -> %s", table, r.id, r.url, newType)
		}
	}

	if apply && len(ids) > 0 {
		tx, err := db.BeginTxx(ctx, nil)
		if err != nil {
			return err
		}
		if err := bulkUpdateType(ctx, tx, table, "question_run_citation_id", "citation_type", ids, types); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}

	log.Printf("[%s] DONE candidates=%d changed=%d", table, len(candidates), len(ids))
	return nil
}

func main() {
	apply := flag.Bool("apply", false, "actually write updates (default is a dry-run that only reports)")
	days := flag.Int("days", 10, "only process citations created within the last N days")
	flag.Parse()

	cutoff := time.Now().UTC().AddDate(0, 0, -*days)

	if err := godotenv.Load(); err != nil {
		_ = godotenv.Load("dev.env")
	}
	cfg := config.Load()

	ctx := context.Background()
	db, err := connect(ctx, cfg.Database)
	if err != nil {
		log.Fatalf("db connect: %v", err)
	}
	defer db.Close()

	if !*apply {
		log.Printf("=== DRY RUN — no rows will be modified. Re-run with -apply to write. ===")
	} else {
		log.Printf("=== APPLY — rows WILL be updated. ===")
	}
	log.Printf("window: citations created on/after %s (last %d days)", cutoff.Format(time.RFC3339), *days)

	websites, err := loadOrgWebsites(ctx, db)
	if err != nil {
		log.Fatalf("load org_websites: %v", err)
	}
	log.Printf("loaded websites for %d orgs", len(websites))

	if err := reclassifyOrgLike(ctx, db, "org_citations", "org_citation_id", websites, cutoff, *apply); err != nil {
		log.Fatalf("org_citations: %v", err)
	}
	if err := reclassifyOrgLike(ctx, db, "network_org_citations", "network_org_citation_id", websites, cutoff, *apply); err != nil {
		log.Fatalf("network_org_citations: %v", err)
	}
	if err := reclassifyClaimCitations(ctx, db, websites, cutoff, *apply); err != nil {
		log.Fatalf("question_run_citations: %v", err)
	}

	log.Printf("all done")
}
