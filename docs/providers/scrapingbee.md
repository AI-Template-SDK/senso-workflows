# ScrapingBee — scraping ChatGPT-like answers + (sometimes) citations, and/or automating the ChatGPT web UI

This guide focuses on using **ScrapingBee** to get **answer text + citations/sources** without using the official OpenAI API.

ScrapingBee offers two distinct approaches:

1. **ScrapingBee “GPT API” (`/api/v1/chatgpt`)**: send a **prompt** (optionally **country_code** and **search**) and receive a **structured JSON response** with `results_text` / `results_markdown` / `results_json`. ScrapingBee notes citations are **not returned 100% of the time** and are embedded in the rendered output.
2. **ScrapingBee “HTML API” (`/api/v1`)**: a **headless browser fetcher** with `render_js` + `js_scenario` (click/fill/wait/evaluate) + proxies/geo. This can be used to **automate the ChatGPT web app UI** if you can supply valid **login cookies**, but you must build your own selectors + parsing.

If your goal is “API in → JSON out”, start with **(1)**.

## Option 1: ScrapingBee GPT API (`/api/v1/chatgpt`)

### Endpoint

- `GET https://app.scrapingbee.com/api/v1/chatgpt`

### Authentication

- Pass `api_key` as a query parameter.

### Key parameters (for our use case)

- **`prompt`** (required): the prompt to send.
- **`search`** (optional, default `false`): “Enable web search to enhance the GPT response”.
- **`country_code`** (optional): country the request should originate from (ISO 3166-1, e.g. `us`, `gb`, `de`).
- **`add_html`** (optional): include `full_html` in response (useful for parsing citation pills).

### Output (what to extract)

The GPT API returns **formatted JSON** including:

- `results_text`: plaintext answer
- `results_markdown`: markdown answer (ScrapingBee notes citations appear at the bottom here when present)
- `results_json`: a structured representation of the markdown
- `full_html` (only if `add_html=true`)

### Citations support (important)

ScrapingBee explicitly notes:

- Citations are **not returned 100% of the time**.
- When present, citations are visible in `results_markdown`.
- If you request full HTML, they mention citation elements like `data-testid="webpage-citation-pill"` (useful if you want to parse citations more reliably from HTML than markdown).

### Example: cURL (prompt + web search + country)

```bash
curl -G 'https://app.scrapingbee.com/api/v1/chatgpt' \
  --data-urlencode 'api_key=YOUR_API_KEY' \
  --data-urlencode 'prompt=best supplements for better sleep. include sources with URLs.' \
  --data-urlencode 'search=true' \
  --data-urlencode 'country_code=us' \
  --data-urlencode 'add_html=true'
```

### Example: Go (fetch answer + extract citation URLs heuristically)

This is intentionally simple: ScrapingBee does **not** return a dedicated `citations[]` array, so we extract URLs from `results_markdown` as a best-effort.

```go
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"time"
)

type ScrapingBeeGPTResponse struct {
	LLMModel       string          `json:"llm_model"`
	Prompt         string          `json:"prompt"`
	ResultsMarkdown string         `json:"results_markdown"`
	ResultsText    string          `json:"results_text"`
	ResultsJSON    json.RawMessage `json:"results_json"`
	FullHTML       string          `json:"full_html"`
}

func extractMarkdownURLs(md string) []string {
	// Find URLs inside markdown links: [text](https://example.com)
	re := regexp.MustCompile(`\]\((https?://[^)]+)\)`)
	matches := re.FindAllStringSubmatch(md, -1)

	seen := map[string]struct{}{}
	var out []string
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		u := m[1]
		if _, ok := seen[u]; ok {
			continue
		}
		seen[u] = struct{}{}
		out = append(out, u)
	}
	return out
}

func main() {
	apiKey := "YOUR_API_KEY"

	q := url.Values{}
	q.Set("api_key", apiKey)
	q.Set("prompt", "best supplements for better sleep. include sources with URLs.")
	q.Set("search", "true")
	q.Set("country_code", "us")
	q.Set("add_html", "false") // set true if you want to parse citation pills from HTML

	u := "https://app.scrapingbee.com/api/v1/chatgpt?" + q.Encode()

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(u)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		panic(fmt.Errorf("scrapingbee status %s: %s", resp.Status, string(b)))
	}

	var decoded ScrapingBeeGPTResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		panic(err)
	}

	fmt.Println("model:", decoded.LLMModel)
	fmt.Println("answer:", decoded.ResultsText)

	citations := extractMarkdownURLs(decoded.ResultsMarkdown)
	fmt.Println("citations:")
	for _, c := range citations {
		fmt.Println("-", c)
	}
}
```

## Option 2: ScrapingBee HTML API (`/api/v1`) for ChatGPT web UI automation

### Reality check

The ChatGPT web app (`chatgpt.com`) is a dynamic, logged-in SPA. ScrapingBee’s HTML API can:

- render JS (`render_js=true`, default),
- wait for elements (`wait_for`),
- run scripted UI actions (`js_scenario`),
- pass cookies (`cookies` parameter),
- use residential/stealth proxies + geo (`premium_proxy`, `stealth_proxy`, `country_code`),
- keep a stable IP (`session_id`).

But ScrapingBee’s HTML API **does not** provide a first-class “ChatGPT prompt” abstraction the way Oxylabs does; you must:

- supply valid auth cookies, and
- maintain selectors that change with ChatGPT UI updates, and
- parse response text + citations from the returned HTML.

### Core endpoint

- `GET https://app.scrapingbee.com/api/v1`

### Useful parameters for ChatGPT UI scraping

- `render_js=true` (default): required for SPA rendering
- `wait_for=<css or xpath>`: wait for prompt box / answer completion indicator
- `js_scenario=<stringified JSON>`: click/fill/wait/evaluate (scenario max ~40s per docs)
- `cookies=<string>`: pass cookies (format described in ScrapingBee docs; usually easiest to start with a single cookie, then expand)
- `premium_proxy=true` + `country_code=us|gb|de...`: geolocate via residential proxies
- `session_id=<int>`: “Route multiple API requests through the same IP address” (helps multi-step flows)
- `json_response=true`: recommended when debugging `js_scenario` (returns `js_scenario_report`)

### Example: conceptual UI automation request

This is a **template** only — selectors and required cookies will vary.

```bash
JS_SCENARIO='{"instructions":[
  {"wait_for":"textarea"},
  {"click":"textarea"},
  {"fill":["textarea","Give me a 5-bullet summary and include sources with URLs."]},
  {"wait":500},
  {"evaluate":"document.querySelector(\"textarea\").dispatchEvent(new KeyboardEvent(\"keydown\", {key: \"Enter\", bubbles:true}))"},
  {"wait":8000}
]}'

curl -G 'https://app.scrapingbee.com/api/v1' \
  --data-urlencode 'api_key=YOUR_API_KEY' \
  --data-urlencode 'url=https://chatgpt.com/' \
  --data-urlencode 'render_js=true' \
  --data-urlencode 'premium_proxy=true' \
  --data-urlencode 'country_code=us' \
  --data-urlencode "js_scenario=$JS_SCENARIO" \
  --data-urlencode 'json_response=true'
```

Then parse the returned HTML/text to extract:

- assistant message text,
- citation anchors / “source” pill links.

## Proxy mode (alternative access method)

ScrapingBee also offers **proxy mode** endpoints:

- HTTP: `proxy.scrapingbee.com:8886`
- HTTPS: `proxy.scrapingbee.com:8887`
- SOCKS5: `socks.scrapingbee.com:8888`

In proxy mode:

- username = `YOUR-API-KEY`
- password = **parameters joined by `&`**, e.g. `render_js=False&premium_proxy=True`

This can be convenient if you want to run your own browser automation and just route traffic through ScrapingBee, but be careful: **every subresource request** from a full browser can consume credits unless you aggressively block resources.

## Pointers to official docs

- HTML API (rendering, js_scenario, cookies, geo, session_id): `https://www.scrapingbee.com/documentation/`
- JavaScript scenario (actions + timeout): `https://scrapingbee.com/documentation/js-scenario`
- Supported geo country codes: `https://scrapingbee.com/documentation/country_codes`
- Proxy mode: `https://scrapingbee.com/documentation/proxy-mode`
- GPT API (`/api/v1/chatgpt` + `search`): `https://www.scrapingbee.com/documentation/chatgpt/`

