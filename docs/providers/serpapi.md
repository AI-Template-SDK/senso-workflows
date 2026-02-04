# SerpApi — web search (sources/citations) to support ChatGPT-web scraping

SerpApi is **not** a ChatGPT scraping provider. It does **not** return “ChatGPT answer text”. What it *does* provide is a reliable **web search API** (Google/Bing/etc.) that returns **structured JSON** search results (links/snippets), which we can use as:

- **Citations/sources** to include in a “ChatGPT-like” cited response we assemble ourselves, or
- **Inputs** to a ChatGPT-web scraping flow (e.g. Oxylabs/Zyte/etc.) by embedding the sources into the prompt (especially when ChatGPT’s own “web search” UI feature is unavailable/unreliable).

In other words: SerpApi is a **citations engine**, not the LLM UI.

## What SerpApi offers (relevant to our use case)

- **Structured JSON** results for Google Search via:
  - `GET https://serpapi.com/search?engine=google`
- **Location + localization controls**:
  - `location` (human-readable, typically **city-level** recommended by SerpApi)
  - `gl` (country, 2-letter code) to enforce country context
  - `hl` (language)
  - `google_domain` (e.g. `google.com`)
- **Output formats**:
  - `output=json` (default)
  - `output=html` (raw HTML if you need to debug missing structured fields)
- **Asynchronous option**: `async=true` to submit and later fetch via the Search Archive API (useful for throughput).
- **Cache**: cached identical queries are free and don’t count toward quotas (per docs) unless you set `no_cache=true`.

## Core endpoint & parameters (Google Search)

### Endpoint

`GET https://serpapi.com/search`

### Required parameters

- `engine=google`
- `q=<query>`
- `api_key=<your_serpapi_key>`

### Key optional parameters for geo/language

- `location`: “from where you want the search to originate”
  - For precision, use SerpApi’s locations list (`/locations.json`) or their Locations API.
  - SerpApi notes `location` and `uule` are mutually exclusive.
- `gl`: **country** code (e.g. `us`, `uk`, `fr`)
  - SerpApi recommends using `gl` alongside `location` for more consistent country filtering.
- `hl`: UI language code (e.g. `en`, `es`)

### Useful result fields

SerpApi’s JSON output includes (varies by result type and engine):

- `organic_results[]` with `title`, `link`, `snippet`, etc.
- `search_metadata.status` (`Processing` → `Success` or `Error`)

## Example: request (cURL)

```bash
curl -G 'https://serpapi.com/search' \
  --data-urlencode 'engine=google' \
  --data-urlencode 'q=best supplements for better sleep' \
  --data-urlencode 'location=Austin, Texas, United States' \
  --data-urlencode 'gl=us' \
  --data-urlencode 'hl=en' \
  --data-urlencode 'api_key=YOUR_SERPAPI_KEY'
```

## Example: Go (extract a citations list)

This produces a simple citations array (title + URL + snippet) that matches the *shape* we want to attach to `AIResponse.Citations` (URLs) and can also be embedded into a ChatGPT prompt.

```go
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

type SerpAPIOrganicResult struct {
	Title   string `json:"title"`
	Link    string `json:"link"`
	Snippet string `json:"snippet"`
}

type SerpAPIResponse struct {
	OrganicResults []SerpAPIOrganicResult `json:"organic_results"`
}

func main() {
	apiKey := "YOUR_SERPAPI_KEY"

	q := url.Values{}
	q.Set("engine", "google")
	q.Set("q", "best supplements for better sleep")
	q.Set("location", "Austin, Texas, United States")
	q.Set("gl", "us")
	q.Set("hl", "en")
	q.Set("api_key", apiKey)

	u := "https://serpapi.com/search?" + q.Encode()

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(u)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		panic(fmt.Errorf("serpapi status %s: %s", resp.Status, string(body)))
	}

	var decoded SerpAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		panic(err)
	}

	// Minimal “citations” view
	for i, r := range decoded.OrganicResults {
		fmt.Printf("[%d] %s\n%s\n%s\n\n", i+1, r.Title, r.Link, r.Snippet)
	}
}
```

## How this helps “ChatGPT web app scraping + citations”

You have two main integration patterns:

### Pattern A: Use SerpApi as the “web search” layer, then scrape ChatGPT without browsing

1. Call SerpApi for sources (organic results).
2. Build a prompt that includes a short list of sources and instructs the model to cite them.
3. Use your ChatGPT-web scraping provider with **its web-search toggle OFF** (or not used) and scrape the answer text.
4. Return:
   - `Response`: scraped ChatGPT answer text
   - `Citations`: the SerP URLs you injected (or the subset actually cited, if you parse which ones were referenced)

This avoids depending on ChatGPT’s *own* web-search UI capability.

### Pattern B: Keep ChatGPT browsing ON (if you have it), but use SerpApi for validation/fallback

1. Ask ChatGPT (via scraping) with its web-search enabled.
2. If citations are missing/empty/low-quality, run SerpApi and either:
   - re-ask with sources included, or
   - attach SerpApi sources as supplemental citations.

## Pointers to official docs

- Google Search API docs: `https://serpapi.com/search-api`
- Supported locations + download: `https://serpapi.com/locations` (and `https://serpapi.com/locations.json`)
- Supported `gl` countries: `https://serpapi.com/google-countries` (and `https://serpapi.com/google-countries.json`)
- Location parameter guidance (blog): `https://serpapi.com/blog/location/`

