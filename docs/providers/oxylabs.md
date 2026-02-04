# Oxylabs (Web Scraper API) — scraping ChatGPT web app with web search + citations

This guide focuses on using **Oxylabs Web Scraper API** to scrape the **ChatGPT web app** (not the OpenAI API) by submitting a prompt, optionally choosing a geo, triggering **ChatGPT “Web Search”**, and receiving a **structured JSON** response containing **response text** and **citations**.

## What Oxylabs offers (relevant to our use case)

- **ChatGPT-specific source**: `source: "chatgpt"` is designed to submit prompts and retrieve conversational responses.
- **Built-in “Web Search” toggle**: `search: true` “clicks the associated interface button” to trigger web search inside ChatGPT.
- **Structured output**: when `parse: true`, results include fields like:
  - `content.response_text` (plain text)
  - `content.markdown_text` / `content.markdown_json`
  - `content.citations` (citation links with URL + text)
  - `content.llm_model` (model used)
- **Geo targeting**: `geo_location` can be set (e.g. `United States`) to submit the prompt “from” that country.
- **Browser automation primitives (generic source)**: `render: "html"` plus `browser_instructions` (click/input/wait/scroll/fetch_resource) exist for sites where you need explicit interaction. For `source: "chatgpt"`, Oxylabs already implements the interaction flow.

## Key API concepts

### Authentication

Oxylabs uses **HTTP Basic Auth** with an API `USERNAME` and `PASSWORD` created in the Oxylabs dashboard.

### Endpoint (Realtime)

Use the synchronous “Realtime” endpoint:

- `POST https://realtime.oxylabs.io/v1/queries`

### Core request fields for ChatGPT

- **`source`**: must be `"chatgpt"`
- **`prompt`**: the text prompt (Oxylabs docs say < 4000 symbols)
- **`search`**: boolean; triggers ChatGPT web search
- **`parse`**: boolean; when `true` returns parsed structured result including citations
- **`geo_location`**: optional; country name (e.g. `"United States"`)

Notes from Oxylabs docs:

- **Batch requests are not supported** for `source: "chatgpt"`.
- **JavaScript rendering is enforced** for `chatgpt`.

## Example: cURL (prompt + web search + geo)

```bash
curl 'https://realtime.oxylabs.io/v1/queries' \
  --user 'USERNAME:PASSWORD' \
  -H 'Content-Type: application/json' \
  -d '{
    "source": "chatgpt",
    "prompt": "best supplements for better sleep",
    "parse": true,
    "search": true,
    "geo_location": "United States"
  }'
```

## Example: Go (fits our repo’s provider style)

This mirrors the Bright Data pattern in `services/brightdata_provider.go`: make a single HTTP request, decode JSON, and extract `response_text` + `citations`.

```go
package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type OxylabsChatGPTRequest struct {
	Source      string `json:"source"`
	Prompt      string `json:"prompt"`
	Parse       bool   `json:"parse"`
	Search      bool   `json:"search"`
	GeoLocation string `json:"geo_location,omitempty"`
}

type OxylabsCitation struct {
	URL  string `json:"url"`
	Text string `json:"text"`
}

type OxylabsChatGPTParsedContent struct {
	Prompt       string            `json:"prompt"`
	LLMModel     string            `json:"llm_model"`
	ResponseText string            `json:"response_text"`
	MarkdownText string            `json:"markdown_text"`
	Citations    []OxylabsCitation `json:"citations"`
}

type OxylabsChatGPTResult struct {
	Content OxylabsChatGPTParsedContent `json:"content"`
	URL     string                      `json:"url"`
	Page    int                         `json:"page"`
	JobID   string                      `json:"job_id"`
	Status  int                         `json:"status_code"`
}

type OxylabsChatGPTResponse struct {
	Results []OxylabsChatGPTResult `json:"results"`
}

func main() {
	username := "USERNAME"
	password := "PASSWORD"

	reqBody := OxylabsChatGPTRequest{
		Source:      "chatgpt",
		Prompt:      "best supplements for better sleep",
		Parse:       true,
		Search:      true,
		GeoLocation: "United States",
	}

	b, _ := json.Marshal(reqBody)

	httpReq, _ := http.NewRequest("POST", "https://realtime.oxylabs.io/v1/queries", bytes.NewReader(b))
	httpReq.Header.Set("Content-Type", "application/json")

	// Basic auth
	auth := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
	httpReq.Header.Set("Authorization", "Basic "+auth)

	client := &http.Client{Timeout: 180 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		panic(fmt.Errorf("unexpected status: %s", resp.Status))
	}

	var decoded OxylabsChatGPTResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		panic(err)
	}
	if len(decoded.Results) == 0 {
		panic("no results")
	}

	r := decoded.Results[0]
	fmt.Println("model:", r.Content.LLMModel)
	fmt.Println("response_text:", r.Content.ResponseText)
	fmt.Println("citations:")
	for _, c := range r.Content.Citations {
		fmt.Printf("- %s (%s)\n", c.Text, c.URL)
	}
}
```

## Output shape (what to extract)

When `parse: true`, the parsed payload includes a `results[]` array where the first item has:

- `results[0].content.response_text` — the main answer text
- `results[0].content.citations[]` — the citation list (URL + label text)
- `results[0].content.markdown_text` — useful if you want markdown formatting
- `results[0].content.llm_model` — record which ChatGPT model the UI used

## Location support

For `geo_location`, Oxylabs accepts many country names (e.g. `Germany`, `United States`). Use it to localize the ChatGPT session and/or search results.

## Practical notes / constraints

- **This is scraping ChatGPT’s web UI**. Expect occasional UI/anti-bot changes. Build retries and monitor parse status codes.
- **Latency**: rendered/browser-driven workflows can take longer; Oxylabs recommends client timeouts around **180 seconds** for rendered jobs.
- **No batching** for `source: "chatgpt"`: parallelize at your worker layer (like we do today) if you need throughput.

## Pointers to official docs

- Web Scraper API overview: `https://developers.oxylabs.io/scraper-apis/web-scraper-api`
- ChatGPT target: `https://developers.oxylabs.io/scraping-solutions/web-scraper-api/targets/chatgpt`
- Proxy location / `geo_location`: `https://developers.oxylabs.io/scraper-apis/web-scraper-api/features/localization/proxy-location`
- JavaScript rendering / `render`: `https://developers.oxylabs.io/scraper-apis/web-scraper-api/features/js-rendering-and-browser-control/javascript-rendering`
- Browser instructions (generic automation): `https://developers.oxylabs.io/scraping-solutions/web-scraper-api/features/js-rendering-and-browser-control/browser-instructions`

