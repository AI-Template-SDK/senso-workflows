# Scrapeless — LLM Chat Scraper (ChatGPT answers + web search + citations)

Scrapeless provides a **ChatGPT-specific scraping API** (their “LLM Chat Scraper”) that is designed to capture:

- prompt → response text (markdown),
- optional **web search** enrichment,
- **citations / content references** (source URLs),
- optional SERP/search results and extracted links,
- optional geo (country).

This matches our desired shape much more closely than “proxy-only” vendors: it behaves like a **prompt API** but is positioned as **scraping** rather than calling the official OpenAI API.

## How it works

Scrapeless uses an **async task** pattern:

1. **Create a task** (`actor = "scraper.chatgpt"`) with input fields like `prompt`, `country`, `web_search`.
2. **Retrieve the result** by `taskId` (or use a webhook so you don’t have to poll).

Scrapeless docs/blog mention results are retained briefly (commonly cited as **~5 minutes**), and their lifecycle/status docs also mention an eventual expiration (410).

## Authentication

Use the `x-api-token` header with your Scrapeless API key.

Example from their API docs:

- `x-api-token: <api-key>`

## Endpoints

Scrapeless publishes multiple API surfaces; for the LLM Chat Scraper, you’ll see:

- **Create task**:
  - `POST https://api.scrapeless.com/api/v1/scraper/request` (API docs)
  - Their blog examples also show a `/api/v2/...` variant on some hosts.
- **Get result**:
  - `GET https://api.scrapeless.com/api/v1/scraper/result/{taskId}` (API docs)

If there’s any mismatch between docs/blog, follow their API reference first.

## Request shape (ChatGPT actor)

### Create task payload

Top-level fields:

- `actor`: `"scraper.chatgpt"`
- `input`:
  - `prompt` (string, required)
  - `country` (string, required in their blog/docs; typically ISO-2 like `US`)
  - `web_search` (bool, optional): enable web search enrichment
- `webhook` (optional):
  - `url`: HTTPS endpoint to push the finished result

Example from Scrapeless blog (shape):

```json
{
  "actor": "scraper.chatgpt",
  "input": {
    "prompt": "Most reliable proxy service for data extraction",
    "country": "US",
    "web_search": true
  },
  "webhook": { "url": "https://your-webhook.example.com" }
}
```

## Response shape (what to extract)

Scrapeless’ ChatGPT scraper docs describe response fields like:

- `prompt`: original prompt
- `result_text`: markdown-formatted response
- `model`: model used (example: `gpt-5-1`)
- `web_search`: whether search ran
- `links`: extracted links
- `search_result`: search results (title/url/snippet/source)
- `content_references`: **citations** / references included in the answer (title/url/source)

## Example: cURL (create + poll)

```bash
# Create task
curl --location --request POST 'https://api.scrapeless.com/api/v1/scraper/request' \
  --header 'x-api-token: YOUR_API_KEY' \
  --header 'Content-Type: application/json' \
  --data-raw '{
    "actor": "scraper.chatgpt",
    "input": {
      "prompt": "best supplements for better sleep. include sources with URLs.",
      "country": "US",
      "web_search": true
    }
  }'

# Then poll for the result (taskId returned by create response)
curl --location --request GET 'https://api.scrapeless.com/api/v1/scraper/result/TASK_ID' \
  --header 'x-api-token: YOUR_API_KEY'
```

## Example: Go (create task + poll until finished)

This mirrors the pattern we use for other providers: submit, poll, parse `result_text` and citations into `[]string`.

```go
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type ScrapelessCreateRequest struct {
	Actor   string                 `json:"actor"`
	Input   map[string]interface{} `json:"input"`
	Webhook *struct {
		URL string `json:"url"`
	} `json:"webhook,omitempty"`
	Async bool `json:"async,omitempty"`
}

type ScrapelessTaskEnvelope struct {
	TaskID     string          `json:"task_id"`
	Status     string          `json:"status"`  // pending/running/success/failed (per docs)
	Message    string          `json:"message"` // error message
	TaskResult json.RawMessage `json:"task_result"`
}

type ScrapelessChatGPTResult struct {
	Prompt            string `json:"prompt"`
	ResultText        string `json:"result_text"`
	Model             string `json:"model"`
	WebSearch         bool   `json:"web_search"`
	ContentReferences []struct {
		Source string `json:"source"`
		Title  string `json:"title"`
		URL    string `json:"url"`
	} `json:"content_references"`
	Links []string `json:"links"`
}

func doJSON(ctx context.Context, client *http.Client, method, urlStr, apiKey string, body any, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, urlStr, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-token", apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("scrapeless status %s: %s", resp.Status, string(b))
	}

	return json.NewDecoder(resp.Body).Decode(out)
}

func main() {
	apiKey := "YOUR_API_KEY"

	client := &http.Client{Timeout: 60 * time.Second}
	ctx := context.Background()

	// 1) Create task
	create := ScrapelessCreateRequest{
		Actor: "scraper.chatgpt",
		Input: map[string]interface{}{
			"prompt":     "best supplements for better sleep. include sources with URLs.",
			"country":    "US",
			"web_search": true,
		},
	}

	var created ScrapelessTaskEnvelope
	if err := doJSON(ctx, client, "POST", "https://api.scrapeless.com/api/v1/scraper/request", apiKey, create, &created); err != nil {
		panic(err)
	}
	if created.TaskID == "" {
		panic("missing task_id in create response")
	}

	// 2) Poll for result
	taskURL := "https://api.scrapeless.com/api/v1/scraper/result/" + created.TaskID

	deadline := time.Now().Add(2 * time.Minute)
	for {
		if time.Now().After(deadline) {
			panic("timeout waiting for task")
		}

		var env ScrapelessTaskEnvelope
		if err := doJSON(ctx, client, "GET", taskURL, apiKey, nil, &env); err != nil {
			panic(err)
		}

		switch env.Status {
		case "success":
			var r ScrapelessChatGPTResult
			if err := json.Unmarshal(env.TaskResult, &r); err != nil {
				panic(err)
			}

			fmt.Println("model:", r.Model)
			fmt.Println("answer:", r.ResultText)
			fmt.Println("citations:")
			for _, c := range r.ContentReferences {
				fmt.Println("-", c.URL)
			}
			return
		case "failed":
			panic(fmt.Errorf("task failed: %s", env.Message))
		default:
			time.Sleep(2 * time.Second)
		}
	}
}
```

## Practical notes / constraints

- **Citations depend on `web_search`** and the model’s behavior; always code defensively (citations may be empty).
- **Async workflow**: prefer webhooks for throughput rather than polling.
- **Country values**: docs/blog show `US`-style values; use Scrapeless’ supported countries list if you need full coverage.

## Pointers to official docs

- LLM Chat Scraper overview: `https://docs.scrapeless.com/en/llm-chat-scraper/quickstart/introduction/`
- ChatGPT scraper fields: `https://docs.scrapeless.com/en/llm-chat-scraper/scrapers/chatgpt/`
- Task lifecycle/status codes: `https://docs.scrapeless.com/en/llm-chat-scraper/quickstart/task-lifecycle/`
- API reference (Scraper Request / Result): `https://apidocs.scrapeless.com/api-11949852` and `https://apidocs.scrapeless.com/api-11949853`
- Blog walkthrough (examples): `https://www.scrapeless.com/en/blog/scrapeless-llm-chat-scraper`

