# Zyte (Zyte API) — scraping the ChatGPT web app with browser automation + sessions

This guide focuses on using **Zyte API** to scrape the **ChatGPT web app** (not the OpenAI API) by driving the UI via **browser rendering + actions**, then extracting **answer text** and **citations/sources** from either:

- the rendered DOM (`browserHtml`), or
- captured network responses (`networkCapture`) during the interaction.

Unlike Oxylabs’ `source: "chatgpt"`, Zyte does **not** provide a ChatGPT-specific “prompt” parameter. You must **automate the web page**.

## What Zyte offers (relevant to our use case)

- **Browser-rendered HTML**: request `browserHtml: true` to get the rendered DOM as a string.
- **Actions**: run an action sequence (`actions`) to click/type/wait, etc. Total browser execution time is limited (docs note a 60s limit for actions).
- **Network capture**: capture XHR/fetch responses during rendering/actions (optionally including bodies as base64), which can be easier than DOM scraping for SPA apps.
- **Cookies**:
  - `requestCookies` to provide cookies
  - `responseCookies: true` to receive cookies set during the request
- **Sessions**: reuse IP + cookie jar + network stack across requests with `session.id` (client-managed sessions) or `sessionContext` (server-managed).
  - **Important**: Zyte sessions do **not** keep a real browser tab/process alive; they only make requests look like the same “organic” session to the target site.
- **Geolocation (country only)**: `geolocation` is ISO 3166-1 alpha-2 (e.g. `"US"`, `"GB"`). Zyte FAQ notes more granular targeting is usually done with cookies/actions.
- **IP type**: `ipType` can be `datacenter` or `residential` (residential has extra requirements/cost considerations per docs).

## The core endpoint and auth

- **Endpoint**: `POST https://api.zyte.com/v1/extract`
- **Auth**: HTTP Basic Auth where username is your **Zyte API key** and password is empty.

Example header from Zyte reference:

- `Authorization: Basic base64(API_KEY + ":")`

## Practical reality check for ChatGPT scraping

- **ChatGPT is a logged-in web app**. Without valid auth cookies you will typically get a login/landing page, not an answer.
- To scrape responses with citations, you generally need:
  - an authenticated ChatGPT session (cookies), and
  - the ChatGPT “web search / browsing” capability enabled in the UI (varies by account/product),
  - UI automation to submit prompts and wait for results.

If your requirements are “prompt + optional location → JSON with response text + citations”, Zyte can be used to implement the *scraping layer*, but you will need to build/maintain the ChatGPT-specific selectors + parsing logic.

## Recommended flow (high level)

1. **Choose a country** (optional): set `geolocation: "US"` (country-only).
2. **Maintain session continuity**:
   - Use `session: { "id": "<uuid-v4>" }` to keep the same IP/cookie jar across steps.
3. **Provide auth cookies**:
   - Pass `requestCookies` for the `chatgpt.com` domain (exported from a logged-in browser session).
4. **Run browser request with actions**:
   - Wait for the prompt input element.
   - Type the prompt.
   - Send it (Enter / click Send).
   - Wait for a “done” condition:
     - either a DOM selector that indicates the answer finished, or
     - a network response pattern that indicates completion.
5. **Extract output**:
   - Parse `browserHtml` for the answer + citation anchors, or
   - Use `networkCapture` to capture the JSON payload(s) behind the UI and parse citations from there.

## Example: simplest “get rendered DOM” request (no actions)

```bash
curl \
  --user "YOUR_ZYTE_API_KEY:" \
  --header "Content-Type: application/json" \
  --data '{
    "url": "https://chatgpt.com/",
    "browserHtml": true
  }' \
  --compressed \
  "https://api.zyte.com/v1/extract"
```

This will likely return an unauthenticated page unless you provide cookies.

## Example: send cookies + reuse a session

This shows the *shape* of cookies Zyte expects. You must supply real cookie values.

```bash
curl \
  --user "YOUR_ZYTE_API_KEY:" \
  --header "Content-Type: application/json" \
  --data '{
    "url": "https://chatgpt.com/",
    "browserHtml": true,
    "geolocation": "US",
    "session": { "id": "2f7a1a3f-2f68-4b2c-8f59-6caa2f5c0c9e" },
    "requestCookies": [
      { "name": "__Secure-next-auth.session-token", "value": "REDACTED", "domain": "chatgpt.com" }
    ],
    "responseCookies": true
  }' \
  --compressed \
  "https://api.zyte.com/v1/extract"
```

## Example: browser actions to submit a prompt (template)

You must adapt selectors to ChatGPT’s current DOM. Start by running a request that returns `browserHtml`, inspect it, then tune selectors and wait conditions.

```bash
curl \
  --user "YOUR_ZYTE_API_KEY:" \
  --header "Content-Type: application/json" \
  --data '{
    "url": "https://chatgpt.com/",
    "browserHtml": true,
    "geolocation": "US",
    "session": { "id": "2f7a1a3f-2f68-4b2c-8f59-6caa2f5c0c9e" },
    "requestCookies": [
      { "name": "__Secure-next-auth.session-token", "value": "REDACTED", "domain": "chatgpt.com" }
    ],
    "actions": [
      { "action": "waitForSelector", "selector": { "type": "css", "value": "textarea" } },
      { "action": "click", "selector": { "type": "css", "value": "textarea" } },
      { "action": "type", "selector": { "type": "css", "value": "textarea" }, "text": "Give me a JSON answer with 5 bullet points and cite sources with URLs." },
      { "action": "keyPress", "key": "Enter" },
      { "action": "waitForTimeout", "timeout": 3 }
    ]
  }' \
  --compressed \
  "https://api.zyte.com/v1/extract"
```

### Tips for making actions robust

- Prefer `waitForSelector` over fixed timeouts where possible.
- For SPA apps, often the most reliable “done” signal is **a network response** rather than a DOM state.
- If you struggle to locate stable selectors, consider using an `evaluate` action to extract content with JavaScript and inject it into a hidden DOM node (similar to Zyte’s shadow DOM example).

## Example: capture network responses during the run

If ChatGPT UI calls an internal JSON endpoint for message content/citations, you can often capture it via `networkCapture` and parse it on your side.

This example captures up to 10 matching responses and includes response bodies (base64):

```bash
curl \
  --user "YOUR_ZYTE_API_KEY:" \
  --header "Content-Type: application/json" \
  --data '{
    "url": "https://chatgpt.com/",
    "browserHtml": true,
    "session": { "id": "2f7a1a3f-2f68-4b2c-8f59-6caa2f5c0c9e" },
    "requestCookies": [
      { "name": "__Secure-next-auth.session-token", "value": "REDACTED", "domain": "chatgpt.com" }
    ],
    "networkCapture": [
      {
        "filterType": "url",
        "matchType": "contains",
        "value": "/backend-api/",
        "httpResponseBody": true
      }
    ],
    "actions": [
      { "action": "waitForSelector", "selector": { "type": "css", "value": "textarea" } },
      { "action": "type", "selector": { "type": "css", "value": "textarea" }, "text": "Summarize today’s news about X and include citations." },
      { "action": "keyPress", "key": "Enter" },
      { "action": "waitForTimeout", "timeout": 5 }
    ]
  }' \
  --compressed \
  "https://api.zyte.com/v1/extract"
```

In the response JSON, inspect `networkCapture[]`. When `httpResponseBody` is present, it is base64-encoded; decode it and parse as JSON/text as appropriate.

## Location limitations (important)

- `geolocation` supports **country only** (ISO 3166-1 alpha-2).
- Zyte FAQ suggests more granular “city/ZIP/state” targeting is typically done using **cookies** and/or site-specific actions (e.g. `setLocation` if supported by the site; otherwise custom UI interactions).

## Proxy mode (usually not what you want for ChatGPT prompting)

Zyte also offers a proxy-mode endpoint (`api.zyte.com:8011`), but:

- proxy mode does **not** support actions or network capture
- Zyte docs explicitly recommend using Zyte API browser automation instead of combining proxy mode with external browser automation tools

## Pointers to official docs

- Browser automation overview: `https://docs.zyte.com/zyte-api/usage/browser.html`
- Shared features (geolocation, ipType, cookies, sessions): `https://docs.zyte.com/zyte-api/usage/features.html`
- API reference (`POST /v1/extract`): `https://docs.zyte.com/zyte-api/usage/reference.html`
- Proxy mode (migration / proxy-style integration): `https://docs.zyte.com/zyte-api/usage/proxy-mode.html`

