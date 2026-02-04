# Decodo (Smartproxy) — Site Unblocker proxy + Web Scraping API (limits for ChatGPT)

Decodo (formerly Smartproxy) is strongest as an **unblocker/proxy layer** (geo + sticky sessions + optional JS rendering) you can put in front of your own scraping / browser automation.

For our goal (“prompt + optional location → JSON with ChatGPT web app response + citations”), Decodo is typically **not** a one-shot “prompt API” like Oxylabs. Instead:

- Use **Site Unblocker** as the proxy/unblocker for your **own browser automation** that drives the ChatGPT UI (login cookies + click/type/wait + parse answer + citations).
- Use **Web Scraping API** for **public pages** (SERPs, articles, etc.) as a citations layer — but Decodo explicitly notes they **do not support scraping post-login data**, which makes it a poor fit for scraping `chatgpt.com` via that API.

## Option 1: Site Unblocker (proxy-like)

### Endpoint + auth

Decodo Site Unblocker is used like an authenticated proxy:

- **Proxy endpoint**: `unblock.decodo.com:60000`
- **Auth**: username/password from Dashboard → Site Unblocker → API Playground

Docs note you may need to disable TLS verification in some clients (their examples use `curl -k`).

### Sticky sessions (keep same IP ~10 minutes)

Use either:

- Header: **`X-SU-Session-Id: <random string>`** (sticky for ~10 minutes), or
- Username modifier: `username-session_id-session1` (per docs)

### Geo targeting (country/city/state/coords)

Header: **`X-SU-Geo`** with values like:

- Country: `Germany`
- City: `Berlin, Germany` or `New York,New York,United States`
- State: `Arizona, United States`
- Coordinates: `lat: 40.8448, lng: -73.8654, rad: 20000`

You can also set geo via username modifier: `username-geo-Germany`.

### JavaScript rendering (Site Unblocker)

Decodo docs describe enabling JS rendering via an `X-SU-*` header to return:

- rendered HTML, or
- a PNG screenshot.

In practice, this helps for JS-heavy sites, but **it is not a substitute for browser actions** needed to submit prompts inside ChatGPT’s UI.

### Headers + cookies pass-through

- Most headers pass through to the target.
- Some headers are intercepted unless you prefix them with `X-SU-Custom-`:
  - `host`, `accept`, `connection`, `cache-control`
- Cookies can be passed directly via the `Cookie` header.
- Forcing behavior:
  - `X-SU-Force-Headers: 1`
  - `X-SU-Force-Cookies: 1`
  - Docs warn forced headers/cookies are charged even if the request fails.

## How Site Unblocker fits “ChatGPT web app scraping”

Site Unblocker can be used as the **proxy** for Playwright/Chromedp/Selenium to:

- run the ChatGPT web UI from specific geos,
- keep an IP stable while you load the app, set cookies, submit prompt, and wait for citations to render.

You still need:

- a valid logged-in ChatGPT cookie jar (or an automated login flow),
- UI automation to type the prompt and enable web search in ChatGPT,
- DOM parsing to extract:
  - response text
  - citation URLs (sources)

## Example: cURL via Site Unblocker (geo + session)

```bash
curl -k -v -x unblock.decodo.com:60000 \
  -U "USERNAME:PASSWORD" \
  -H "X-SU-Geo: Germany" \
  -H "X-SU-Session-Id: random123" \
  "https://ip.decodo.com/"
```

## Example: Go `net/http` using Site Unblocker as a proxy

This is useful for validating geo/session routing. For actual ChatGPT scraping you’ll use a browser automation stack, but the proxy wiring is the same idea.

```go
package main

import (
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

func main() {
	username := "USERNAME"
	password := "PASSWORD"

	proxyURL, err := url.Parse(fmt.Sprintf("http://%s:%s@unblock.decodo.com:60000", username, password))
	if err != nil {
		panic(err)
	}

	tr := &http.Transport{
		Proxy: http.ProxyURL(proxyURL),
		// Decodo examples often disable TLS verification (curl -k). Keep this only if needed.
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}

	client := &http.Client{
		Transport: tr,
		Timeout:   30 * time.Second,
	}

	req, _ := http.NewRequest("GET", "https://ip.decodo.com/", nil)
	req.Header.Set("X-SU-Geo", "Germany")
	req.Header.Set("X-SU-Session-Id", "random123")

	resp, err := client.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	b, _ := io.ReadAll(resp.Body)
	fmt.Println(string(b))
}
```

## Option 2: Decodo Web Scraping API (Core/Advanced)

### Key limitation for ChatGPT

Decodo’s Web Scraping API documentation explicitly states:

- **“We Do Not Support Scraping Post-login Data”**

Since ChatGPT generally requires authentication to get answers/citations, this API is usually **not suitable** for scraping `chatgpt.com` responses.

### When it is still useful in this project

Use Web Scraping API to build a **citations/sources layer** (public web):

- scrape SERPs or public articles,
- return HTML/Markdown,
- optionally collect XHR/fetch lists (`xhr=true`) for debugging dynamic sites.

### Endpoint + rendering

Docs show:

- `POST https://scraper-api.decodo.com/v2/scrape`
- Auth: `Authorization: Basic <token>`
- JavaScript rendering: set `"headless": "html"` (Advanced plan)

### Common parameters (subset)

- `url` (or `query` for some templates)
- `target` (e.g. `universal`, or a template like `google_search`)
- `headless`: `html` (render) or `png` (screenshot)
- `geo`: country/city/state/coords (note capital case for country names in docs)
- `headers`, `cookies` (+ `force_headers`, `force_cookies`)
- `session_id`: sticky IP up to ~10 minutes
- `markdown`: convert HTML → Markdown
- `xhr`: return XHR/fetch list

## Pointers to official docs

- Site Unblocker quick start: `https://help.decodo.com/docs/introduction-site-unblocker`
- Site Unblocker sessions: `https://help.decodo.com/docs/site-unblocker-session`
- Site Unblocker geo: `https://help.decodo.com/docs/site-unblocker-geo-location`
- Site Unblocker headers/cookies forcing: `https://help.decodo.com/docs/site-unblocker-headers`
- Web Scraping API intro (includes “no post-login” note): `https://help.decodo.com/docs/web-scraping-api-introduction`
- Web Scraping API parameters: `https://help.decodo.com/docs/web-scraping-api-parameters`
- Web Scraping API JS rendering: `https://help.decodo.com/docs/web-scraping-api-javascript-rendering`

