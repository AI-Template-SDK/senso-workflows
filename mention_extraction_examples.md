# Mention Extraction — Prompt, Input, Output Examples

This document shows what the LLM is given and what it produces when `senso-workflows` extracts **company mentions** from an AI response. The active code path is:

- `services/data_extraction_service.go` → `ExtractMentions()` (line 60)
- Prompt builder: `buildMentionsExtractionPrompt()` (line 810)
- Structured output schema: `MentionsExtractionResponse` in `services/interfaces.go` (line 403)

The call goes to OpenAI `gpt-4.1` (or an Azure deployment) with `temperature=0.1` and `response_format = json_schema (strict)`, so the LLM is forced to return JSON matching the schema:

```jsonc
{
  "target_company": {                  // or null if the target wasn't mentioned
    "name": "string",
    "rank": 1,                         // order-of-first-appearance
    "mentioned_text": "string",        // concatenation of all occurrences joined by "  ||  "
    "text_sentiment": "positive|negative|neutral|mixed"
  },
  "competitors": [
    { "name": "...", "rank": 2, "mentioned_text": "...", "text_sentiment": "..." }
  ]
}
```

Each example below shows three things:
1. **The prompt** — exactly what is sent to the LLM (system message + the rendered user prompt).
2. **The input text** — the AI-response text being analyzed (this is what gets interpolated into the `RESPONSE TEXT` section of the prompt).
3. **The output** — the structured JSON the LLM returns.

---

## Example 1 — Target mentioned alongside several competitors

**Function inputs**
- `targetCompany`: `Connexus Credit Union`
- `orgWebsites`: `["connexuscu.org"]`
- `response`: see the *Input text* section below.

### 1) Prompt

**System message:**
```
You are an expert financial services analyst specializing in credit unions and banks. Extract company mentions accurately and comprehensively.
```

**User message (rendered prompt):**
```
You are an expert competitive intelligence analyst extracting SPECIFIC COMPANY AND BRAND mentions from the RESPONSE TEXT ONLY.

## CRITICAL RULES
1) Target aggregation: If the target organization "Connexus Credit Union" appears anywhere in the RESPONSE TEXT, collect EVERY occurrence.
   - Output ONE "mentioned_text" string that concatenates ALL distinct occurrences in order of appearance.
   - Use the exact delimiter:  ||  (space, two pipes, space) between occurrences.
   - Example: "DoorDash offers 24/7 support.  ||  Visit merchants.doordash.com for help.  ||  Phone support is available."

2) Span definition: An occurrence = the full sentence or bullet line that explicitly mentions the company, or a directly adjacent sentence that clearly continues the same thought.
   - If consecutive sentences reference the company as one continuous thought, keep them together as ONE occurrence.
   - Do not include unrelated surrounding text.

3) Variations allowed: Match common name variants, abbreviations, and brand/product names.

4) Domains are SECONDARY signals (not required):
   - Use domain mentions only to support detection when the explicit name is absent.
   - Count a domain as a valid mention only if it clearly belongs to the target organization.
   - Any subdomain of the listed roots counts (e.g., www., help., merchants.).
   - When both name and domain appear together, merge them into a single occurrence for that location.
   - Do NOT infer from partial/generic domain strings that could match other entities.
## ORGANIZATION DOMAINS (SUPPORTING SIGNALS, NOT PRIMARY):
- connexuscu.org


5) Exclusions: Ignore any companies that appear in these instructions; analyze ONLY the text in the RESPONSE TEXT section.

6) De-duplication: If the same sentence appears more than once, include it only once.

7) Quality goal: Prefer including more valid occurrences over missing any. Do not omit valid target mentions.

## Output policy
- target_company: null if not mentioned anywhere; otherwise include name/rank/sentiment.
- mentioned_text: concatenation of ALL target occurrences using " || ".
- For competitors, apply the same span logic (concatenate their occurrences with " || ").

## Checklist before finalizing
- Did you search the entire RESPONSE TEXT for EVERY target occurrence?
- Is each occurrence a full sentence/bullet or continuous thought?
- Did you use " || " exactly as the delimiter?
- If using a domain, does it clearly belong to the target and is it used only as a supporting signal when the name is not present?
- Did you avoid adding any text not present in the RESPONSE TEXT?

## RESPONSE TEXT (analyze ONLY this):
"""
<<< input text goes here — see section 2 below >>>
"""
```

### 2) Input text

```
If you're looking for the best credit unions in the Midwest for auto loans, here are several strong options:

1. Connexus Credit Union offers some of the lowest auto loan rates in Wisconsin, with APRs starting around 4.74% for well-qualified borrowers.
2. Summit Credit Union is another solid choice, particularly known for its member-first approach and competitive new car rates.
3. Royal Credit Union has a strong presence in western Wisconsin and offers flexible terms up to 84 months.
4. Landmark Credit Union, the largest credit union in Wisconsin, regularly advertises promotional rates and has a wide branch network.

Connexus in particular stands out because membership is open nationally through a small donation to the Connexus Association, making it an attractive option even for borrowers outside its home region.
```

### 3) Output

```json
{
  "target_company": {
    "name": "Connexus Credit Union",
    "rank": 1,
    "mentioned_text": "Connexus Credit Union offers some of the lowest auto loan rates in Wisconsin, with APRs starting around 4.74% for well-qualified borrowers.  ||  Connexus in particular stands out because membership is open nationally through a small donation to the Connexus Association, making it an attractive option even for borrowers outside its home region.",
    "text_sentiment": "positive"
  },
  "competitors": [
    {
      "name": "Summit Credit Union",
      "rank": 2,
      "mentioned_text": "Summit Credit Union is another solid choice, particularly known for its member-first approach and competitive new car rates.",
      "text_sentiment": "positive"
    },
    {
      "name": "Royal Credit Union",
      "rank": 3,
      "mentioned_text": "Royal Credit Union has a strong presence in western Wisconsin and offers flexible terms up to 84 months.",
      "text_sentiment": "positive"
    },
    {
      "name": "Landmark Credit Union",
      "rank": 4,
      "mentioned_text": "Landmark Credit Union, the largest credit union in Wisconsin, regularly advertises promotional rates and has a wide branch network.",
      "text_sentiment": "positive"
    }
  ]
}
```

---

## Example 2 — Target NOT mentioned (only competitors found)

**Function inputs**
- `targetCompany`: `Patelco Credit Union`
- `orgWebsites`: `["patelco.org"]`
- `response`: see the *Input text* section below.

### 1) Prompt

**System message:**
```
You are an expert financial services analyst specializing in credit unions and banks. Extract company mentions accurately and comprehensively.
```

**User message (rendered prompt — same template, with `Patelco Credit Union` and `patelco.org` interpolated):**
```
You are an expert competitive intelligence analyst extracting SPECIFIC COMPANY AND BRAND mentions from the RESPONSE TEXT ONLY.

## CRITICAL RULES
1) Target aggregation: If the target organization "Patelco Credit Union" appears anywhere in the RESPONSE TEXT, collect EVERY occurrence.
   ...
## ORGANIZATION DOMAINS (SUPPORTING SIGNALS, NOT PRIMARY):
- patelco.org

(...same rules 5–7, output policy, and checklist as Example 1...)

## RESPONSE TEXT (analyze ONLY this):
"""
<<< input text goes here — see section 2 below >>>
"""
```

### 2) Input text

```
For first-time homebuyers in California, a few credit unions consistently get strong reviews:

- Golden 1 Credit Union offers a First-Time Homebuyer Program with reduced down payment options and flexible underwriting.
- SchoolsFirst Federal Credit Union has dedicated mortgage counselors who specialize in working with educators and first-time buyers.
- Logix Federal Credit Union is well-regarded for low closing costs and competitive 30-year fixed rates.

You should compare APRs, closing costs, and any first-time-buyer grants or rate discounts before deciding.
```

### 3) Output

```json
{
  "target_company": null,
  "competitors": [
    {
      "name": "Golden 1 Credit Union",
      "rank": 1,
      "mentioned_text": "Golden 1 Credit Union offers a First-Time Homebuyer Program with reduced down payment options and flexible underwriting.",
      "text_sentiment": "positive"
    },
    {
      "name": "SchoolsFirst Federal Credit Union",
      "rank": 2,
      "mentioned_text": "SchoolsFirst Federal Credit Union has dedicated mortgage counselors who specialize in working with educators and first-time buyers.",
      "text_sentiment": "positive"
    },
    {
      "name": "Logix Federal Credit Union",
      "rank": 3,
      "mentioned_text": "Logix Federal Credit Union is well-regarded for low closing costs and competitive 30-year fixed rates.",
      "text_sentiment": "positive"
    }
  ]
}
```

> Note: because `target_company` is `null`, the Go code at `data_extraction_service.go:139-160` skips writing a target-org row to `question_run_mentions`. Only the three competitor rows are persisted.

---

## Example 3 — Target mentioned multiple times + a domain-only mention (shows the `  ||  ` delimiter)

**Function inputs**
- `targetCompany`: `Mountain America Credit Union`
- `orgWebsites`: `["macu.com"]`
- `response`: see the *Input text* section below.

### 1) Prompt

**System message:**
```
You are an expert financial services analyst specializing in credit unions and banks. Extract company mentions accurately and comprehensively.
```

**User message (rendered prompt — same template, with `Mountain America Credit Union` and `macu.com` interpolated):**
```
You are an expert competitive intelligence analyst extracting SPECIFIC COMPANY AND BRAND mentions from the RESPONSE TEXT ONLY.

## CRITICAL RULES
1) Target aggregation: If the target organization "Mountain America Credit Union" appears anywhere in the RESPONSE TEXT, collect EVERY occurrence.
   ...
## ORGANIZATION DOMAINS (SUPPORTING SIGNALS, NOT PRIMARY):
- macu.com

(...same rules 5–7, output policy, and checklist as Example 1...)

## RESPONSE TEXT (analyze ONLY this):
"""
<<< input text goes here — see section 2 below >>>
"""
```

### 2) Input text

```
Mountain America Credit Union is one of the largest credit unions headquartered in Utah, serving members across the Intermountain West. It is consistently rated among the top credit unions in the region for member satisfaction.

Mountain America offers a full suite of products including checking, savings, auto loans, mortgages, and business banking. Members frequently highlight the mobile app and the ability to manage accounts through macu.com.

Compared to America First Credit Union, also based in Utah, Mountain America tends to focus more on wealth-building tools, while America First is often praised for its branch network. Goldenwest Credit Union is another nearby option, though smaller in scale.

Some online reviews mention occasional long wait times at branch locations during peak hours, but overall sentiment toward Mountain America is positive.
```

### 3) Output

```json
{
  "target_company": {
    "name": "Mountain America Credit Union",
    "rank": 1,
    "mentioned_text": "Mountain America Credit Union is one of the largest credit unions headquartered in Utah, serving members across the Intermountain West. It is consistently rated among the top credit unions in the region for member satisfaction.  ||  Mountain America offers a full suite of products including checking, savings, auto loans, mortgages, and business banking. Members frequently highlight the mobile app and the ability to manage accounts through macu.com.  ||  Compared to America First Credit Union, also based in Utah, Mountain America tends to focus more on wealth-building tools, while America First is often praised for its branch network.  ||  Some online reviews mention occasional long wait times at branch locations during peak hours, but overall sentiment toward Mountain America is positive.",
    "text_sentiment": "mixed"
  },
  "competitors": [
    {
      "name": "America First Credit Union",
      "rank": 2,
      "mentioned_text": "Compared to America First Credit Union, also based in Utah, Mountain America tends to focus more on wealth-building tools, while America First is often praised for its branch network.",
      "text_sentiment": "positive"
    },
    {
      "name": "Goldenwest Credit Union",
      "rank": 3,
      "mentioned_text": "Goldenwest Credit Union is another nearby option, though smaller in scale.",
      "text_sentiment": "neutral"
    }
  ]
}
```

Things to notice in this example:
- The target was mentioned in **four** separate spans, all joined into one `mentioned_text` string using the exact delimiter `  ||  ` (space, two pipes, space).
- The third span shows the rule from the prompt: when the company name and a domain (`macu.com`) appear together in the same area, they are merged into a single occurrence rather than counted twice.
- Sentiment is `mixed` because the response includes both positive points (member satisfaction, product suite) and a negative one (long wait times).

---

## Recap of what the LLM is given and what it returns

| | What is sent |
|---|---|
| **System message** | A short role primer: "You are an expert financial services analyst…" |
| **User message** | The rendered `buildMentionsExtractionPrompt` template, with `targetCompany`, `orgWebsites`, and the AI response text interpolated into the `RESPONSE TEXT` block. |
| **Response format** | A strict JSON schema (`MentionsExtractionResponse`) — the LLM must return valid JSON with `target_company` and `competitors`. |
| **Model / temperature** | `gpt-4.1` (or Azure deployment), `temperature=0.1` for consistency. |

| | What comes back |
|---|---|
| **`target_company`** | `null` if not mentioned, else `{ name, rank, mentioned_text, text_sentiment }`. |
| **`competitors`** | Array of the same shape, ranked by order of first appearance. |
| **`mentioned_text`** | All occurrences for that org joined with `  ||  `. |
| **`text_sentiment`** | One of `positive`, `negative`, `neutral`, `mixed`. |
