# Infatica — residential proxies (geo + sessions) for scraping the ChatGPT web app

Infatica is **not** a “ChatGPT prompt API” provider. It primarily provides **residential/mobile/datacenter proxies** plus a separate **Web Scraper API** for fetching/rendering pages.

For our requirement (“hit an API with a prompt + optional location and get back structured response text + citations from the **ChatGPT web app**”), Infatica fits as the **network/proxy layer** you use with **your own browser automation** (Playwright/Chromedp/Selenium) to:

- run ChatGPT web UI flows from a chosen country/city,
- keep the same exit IP via **sessions** (sticky),
- reduce bot detection / geo restrictions.

## What Infatica offers (relevant to our use case)

### 1) Shared proxy access (Residential/Mobile/Datacenter)

From Infatica’s “API tool” docs:

- **Proxy host**: `pool.infatica.io` (GeoDNS — resolves to nearest gateway)
- **Ports**:
  - Default: `10000`
  - For simultaneous requests, use **10000–10999**
  - Infatica notes: **each port corresponds to a unique IP address**
- **Protocols**: **HTTP** and **SOCKS5** recommended; HTTPS exists but not recommended for performance
- **Auth**: username/password (from Infatica dashboard “API Tool” credentials)

### 2) Geo-targeting + sticky sessions via *username modifiers*

Infatica supports “targeting knobs” by appending segments to the **proxy username** (login). Common ones:

- **Country**: `_<c_XX>` → `xxx_c_US`
- **Subdivision/region**: `_<sd_ID>` (IDs via Client API)
- **City**: `_<city_City-Name>` (spaces replaced with `-`)
- **ISP**: `_<isp_ID>` (IDs via Client API)
- **ASN**: `_<asn_NUMBER>`
- **ZIP** (US-only per docs): `_<zip_10001>` (must include country)
- **Session ID**: `_<s_ID>` (reuse same exit IP while online; inactive timeout ~60 minutes)
- **Session TTL/rotation**: `_<ttl_10s|15m|1h>` (requires session id)

Example (country + city + session + TTL):

- `xxx_c_US_city_New-York_s_100_ttl_15m`

## Practical ChatGPT scraping flow (how we’d use Infatica)

Because ChatGPT is a dynamic, logged-in SPA, the “proxy only” piece is not enough. The workable approach is:

1. **Obtain ChatGPT auth cookies** for `chatgpt.com` (from a logged-in browser profile / secure cookie store).
2. **Launch a headless browser** (Playwright/Chromedp) using an **Infatica proxy** configured for:
   - country/city (optional),
   - a sticky `session id` (recommended),
   - a TTL if you want deterministic IP rotation.
3. **Load `https://chatgpt.com/`**, apply cookies, and navigate to the chat UI.
4. **Enable ChatGPT web search in the UI** (if the account supports it) and submit the prompt.
5. **Extract structured output**:
   - **Response text**: parse the final assistant message content from the DOM (or intercept the internal network response, if your automation supports it).
   - **Citations/sources**: when “web search” is used, citations typically appear as linked sources in the UI. Extract `href`s and titles/snippets from the response region.

## Infatica proxy usage examples

### Example: cURL using the proxy (sanity check)

```bash
curl -v \
  -x "xxx_c_US_s_100_ttl_15m:yyy@pool.infatica.io:10000" \
  "https://ip-api.com/json"
```

### Example: Go `net/http` client with Infatica proxy

This is useful for simple requests (debugging geo/session), but **won’t scrape ChatGPT** by itself (ChatGPT requires browser automation).

```go
package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

func main() {
	// Username modifiers: country + session + TTL (all encoded into username)
	proxyUser := "xxx_c_US_s_100_ttl_15m"
	proxyPass := "yyy"

	proxyURL, err := url.Parse(fmt.Sprintf("http://%s:%s@pool.infatica.io:10000", proxyUser, proxyPass))
	if err != nil {
		panic(err)
	}

	tr := &http.Transport{
		Proxy: http.ProxyURL(proxyURL),
	}

	client := &http.Client{
		Transport: tr,
		Timeout:   30 * time.Second,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "GET", "https://ip-api.com/json", nil)
	resp, err := client.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	b, _ := io.ReadAll(resp.Body)
	fmt.Println(string(b))
}
```

### Example: building an Infatica proxy username (Go helper)

```go
package main

import "strings"

type InfaticaProxyOpts struct {
	BaseUser string // "xxx"
	Country  string // "US"
	City     string // "New York" -> "New-York"
	Session  string // "100" (any alphanumeric)
	TTL      string // "15m" / "30s" / "1h"
}

func BuildInfaticaUser(o InfaticaProxyOpts) string {
	u := o.BaseUser
	if o.Country != "" {
		u += "_c_" + strings.ToUpper(o.Country)
	}
	if o.City != "" {
		city := strings.ReplaceAll(o.City, " ", "-")
		u += "_city_" + city
	}
	if o.Session != "" {
		u += "_s_" + o.Session
		if o.TTL != "" {
			u += "_ttl_" + o.TTL
		}
	}
	return u
}
```

## Infatica Web Scraper API (separate product)

Infatica also provides a **Web Scraper API** with endpoints like:

- `POST https://scrape.infatica.io/` (fetch HTML; may return base64 HTML in JSON)
- `POST https://scrape.infatica.io/render` (fetch + **render JS**; returns HTML)
- `POST https://scrape.infatica.io/serp` (Google SERP scraping; advanced plan)
- `POST https://scrape.infatica.io/gemini` / `.../perplexity` (AI search endpoints; advanced plan)

**Important limitation for ChatGPT scraping:** these endpoints (as documented) fetch or render a URL, but they **do not provide action scripting** (click/type/wait) to submit a prompt in ChatGPT’s UI. They’re useful for:

- scraping general web pages via a localized proxy,
- scraping SERPs / AI search HTML (as a citations layer),
- rendering JS for public pages.

### Example: Render a page in a specific country

```bash
curl -X POST "https://scrape.infatica.io/render" \
  -H "Content-Type: application/json" \
  -H "X-API-Key: YOUR_SECRET_API_KEY" \
  -d '{
    "url": "https://www.example.com",
    "country": "US",
    "return_html": false
  }'
```

## Notes for making ChatGPT scraping reliable with Infatica

- **Use sticky sessions** (`_s_...`) so the ChatGPT session (cookies) and IP are stable during multi-step UI flows.
- **Prefer SOCKS5 or HTTP** for performance (Infatica docs recommend avoiding HTTPS proxy mode).
- **Parallelism**: spread concurrent browser runs across ports `10000–10999` (Infatica notes each port maps to a unique IP).
- **Auth**: avoid IP whitelisting unless you control all egress; Infatica notes whitelists can override other proxy list behavior.

## Pointers to official docs

- API tool (host formats, geo modifiers, sessions, TTL): `https://infatica.io/documentation/api-tool`
- Residential proxy overview (rotation concepts): `https://infatica.io/documentation/residential`
- Web Scraper API (fetch/render endpoints): `https://infatica.io/documentation/scraper-api`

