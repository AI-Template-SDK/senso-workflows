import json
from openai import OpenAI
from pydantic import BaseModel
from tqdm import tqdm
from typing import Literal
import re
import os

from dotenv import load_dotenv

load_dotenv()

client = OpenAI()

class NameList(BaseModel):
    names: list[str]

class MentionText(BaseModel):
    mention_text: str
    sentiment: Literal["positive", "negative", "neutral"]

class CompetitorList(BaseModel):
    competitors: list[str]

def get_names(name: str, websites: list[str]) -> list[str]:
    websites_formatted = '\n'.join(f"- {website}" for website in websites)
    
    prompt = f"""You are an expert in brand name analysis and variation generation. Your task is to generate a comprehensive list of brand name variations that a company might realistically use across different platforms, documents, and contexts.

Generate REALISTIC variations of this brand name that would actually be used by the company or found in business contexts. Focus on:

1. **Exact matches**: The brand name as provided
2. **Case variations**: lowercase, UPPERCASE, Title Case, camelCase
3. **Spacing variations**: CRITICAL for compound words - always include both spaced and unspaced versions:
   - Compound words: "SunLife" → "Sun Life", "TotalExpert" → "Total Expert"
   - With spaces, without spaces, with hyphens, with underscores
   - For ANY compound-looking word, generate the spaced version
4. **Legal/formal variations**: Including "Inc", "LLC", "Ltd", "Corp", etc. (only if realistic for this type of company)
5. **Natural shortened versions**: Logical shortened forms (e.g., "Senso.ai" → "Senso", "Microsoft Corporation" → "Microsoft")
6. **Realistic acronyms**: Only create acronyms from multi-word names where it makes sense:
   - "Bellweather Community Credit Union" → "BCCU"
   - "American Express" → "AmEx" or "AE"
   - Single word brands typically don't have meaningful acronyms
7. **Domain-based variations**: Simple domain formats without full URLs (e.g., "senso" from "senso.ai")

IMPORTANT CONSTRAINTS:
- Do NOT include full email addresses (no @domain.com formats)
- Do NOT include full website URLs (no http:// or www. formats)
- Do NOT create arbitrary abbreviations or random letter combinations
- Only create acronyms for multi-word brand names where each word contributes a letter
- Only include variations that would realistically be used in professional business contexts
- Focus on how the brand name would naturally be written, typed, or formatted

**CRITICAL: For compound words, ALWAYS generate spaced versions**

Examples:
- "Senso.ai" → Good: Senso.ai, senso.ai, SENSO.AI, Senso, senso, SENSO, SensoAI, sensoai
- "Senso.ai" → Bad: S.AI, SAI, support@senso.ai, www.senso.ai
- "SunLife" → MUST include: SunLife, Sun Life, sunlife, sun life, SUNLIFE, SUN LIFE, Sun-Life, sun-life
- "TotalExpert" → MUST include: TotalExpert, Total Expert, totalexpert, total expert, TOTALEXPERT, TOTAL EXPERT, Total-Expert, total-expert
- "Tech Corp Solutions" → Good: TCS, Tech Corp, TechCorp, Tech Corp Solutions
- "Apple" → Good: Apple, apple, APPLE (no meaningful acronym for single word)

Instructions:
- Include the original name exactly as provided
- Generate 15-25 realistic variations (quality over quantity)
- Each variation should have a clear reason for existing
- For multi-word names, consider logical acronyms using first letters
- For compound names or names with extensions (.ai, .com), consider the root word
- **MANDATORY**: If the brand name looks like a compound word (two or more words joined together), generate the spaced version
- Avoid nonsensical permutations or made-up abbreviations

Return only the list of name variations, no explanations.

The brand name is `{name}`

Associated websites:
```
{websites_formatted}
```
"""

    response = client.responses.parse(
        model="gpt-4.1-mini",
        input=[
            {
                "role": "user",
                "content": prompt,
            },
        ],
        text_format=NameList,
    )
    return response.output_parsed.names


def get_mention_text(name: str, name_list: list[str], response: str) -> MentionText:
    prompt = f"""You are an expert in brand share-of-voice analysis. Your task is to extract ALL content that substantively discusses or describes the target brand, measuring how much of the response is genuinely ABOUT the brand (not just mentioning it).

**TARGET BRAND:** `{name}`
**Brand variations:** {', '.join(name_list)}

**SHARE-OF-VOICE EXTRACTION STRATEGY:**

This is NOT about finding every mention - it's about measuring how much content is genuinely ABOUT the target brand.

1. **Content Classification:**
   - **Substantive Content**: Paragraphs, sections, or passages that describe, explain, analyze, or discuss the brand in depth with narrative text
   - **Passing Mentions**: Brief references, comparisons, or lists where the brand is mentioned but not the focus
   - **Structured Data**: Tables, comparison charts, feature lists that contain brand info but lack narrative discussion
   - **Extract ONLY substantive narrative content** - ignore passing mentions and structured data

2. **Extraction Rules:**
   - **Dedicated Sections**: Extract complete sections with headers that focus on the brand
   - **Descriptive Paragraphs**: Extract full paragraphs that explain what the brand does, how it works, its features
   - **Analysis/Comparison**: Extract parts that analyze the brand's capabilities, positioning, or characteristics
   - **Recommendations**: Extract conclusions or recommendations specifically about the brand
   - **List Entries**: Extract numbered/bulleted list items that substantively describe the brand
   - **Introductory Context**: Extract opening sentences/paragraphs that establish context for discussing the brand
   - **Summary References**: Extract concluding summaries, bullet points, or lists that reference the brand
   - **Contextual Mentions**: Include sentences that provide important context about the brand, even if they mention other entities
   - **Multiple Parts**: Use `||` to separate distinct substantive sections

3. **What NOT to Extract:**
   - Brief mentions in lists of competitors
   - Single-sentence references without elaboration
   - Citations or source references
   - Content primarily about other topics that just mentions the brand
   - **Comparison tables or structured data** (even if they contain brand information)
   - **Table rows, columns, or cells** that list brand attributes without narrative discussion

**EXAMPLES:**

Example 1 - Full brand response:
"What does Senso.ai do? [Entire response explaining capabilities]"
→ Extract: [Full response] (high share-of-voice)

Example 2 - Dedicated section in comparison:
"## Company A does X... ## Senso.ai - AI Platform [detailed description] ## Company B does Y..."
→ Extract: [Only the Senso.ai section] (moderate share-of-voice)

Example 3 - Multiple substantive parts:
"Senso.ai offers advanced features... [other content]... In conclusion, choose Senso.ai for..."
→ Extract: "Senso.ai offers advanced features...||In conclusion, choose Senso.ai for..." (moderate share-of-voice)

Example 4 - Passing mentions only:
"Leading AI tools include ChatGPT, Senso.ai, Claude, and others in the market."
→ Extract: "" (no substantive content, just a mention)

Example 5 - Comparison table:
"| Feature | Senso.ai | Competitor | ... | Primary Goal | Transform content into AI answers | Monitor brand visibility |"
→ Extract: "" (structured comparison data, not substantive discussion)

Example 6 - List context with substantive entry:
"Here are 10 AI tools including Senso.ai: 1. Senso.ai - A platform that transforms content... Summary: - Senso.ai (included as requested)"
→ Extract: "Here are 10 AI tools including Senso.ai:||1. Senso.ai - A platform that transforms content...||Summary: - Senso.ai (included as requested)" (intro + list entry + summary)

Example 7 - Contextual disclaimer:
"Senso.ai is legitimate for AI SEO. Note that senso.ai and senso.cloud are entirely separate products."
→ Extract: Include the disclaimer (provides important context about the brand, even though it mentions another entity)

**SENTIMENT ANALYSIS GUIDELINES:**

- **Neutral**: Factual, informational, or descriptive content without clear bias
  - Explaining what a company does, their features, policies
  - Comparative analysis that's balanced
  - Technical descriptions or specifications
- **Positive**: Clear favorable language, praise, validation, or endorsement
  - Words like "excellent," "innovative," "leading," "impressive," "legitimate," "credible"
  - Highlighting advantages, superior capabilities, or success metrics
  - Validation of legitimacy or effectiveness ("appears to be legitimate," "functions as advertised")
  - Testimonials, success stories, or positive outcomes
  - Recommendations or endorsements
- **Negative**: Critical, unfavorable, or concerning language
  - Problems, issues, limitations, criticisms
  - Negative comparisons or warnings
  - Questioning legitimacy or effectiveness

**RESPONSE TO ANALYZE:**
```
{response}
```

**INSTRUCTIONS:**
- Focus on SUBSTANTIVE content about the brand, not just mentions
- Measure share-of-voice: how much content is genuinely ABOUT the target brand
- Extract complete thoughts and sections, maintain formatting
- Use `||` to separate distinct substantive parts
- **Capture ALL parts**: If the brand appears in intro, detailed discussion, AND summary, extract all three parts
- **Don't miss context**: Include introductory sentences that set up discussion of the brand
- **Don't miss conclusions**: Include summary references, bullet points, or final mentions
- If no substantive content exists (only passing mentions), return empty string for mention_text and "neutral" for sentiment
- Sentiment must be exactly one of: "positive", "negative", or "neutral" (lowercase)"""

    response = client.responses.parse(
        model="gpt-4.1",
        input=[
            {
                "role": "user",
                "content": prompt,
            },
        ],
        text_format=MentionText,
    )
    return response.output_parsed


def get_competitors(target_org: str, response: str) -> list[str]:
    prompt = f"""You are an expert in competitive analysis and brand identification. Your task is to identify ALL competitor brands, companies, products, or services mentioned in the response text that are NOT the target organization.

**TARGET ORGANIZATION:** `{target_org}`

**COMPETITOR IDENTIFICATION RULES:**

1. **What to Include:**
   - Company names (e.g., "Microsoft", "Google", "Apple")
   - Product names (e.g., "ChatGPT", "Claude", "Gemini", "Perplexity")
   - Service names (e.g., "Ahrefs Brand Radar", "Surfer SEO AI Tracker")
   - Platform names (e.g., "LinkedIn", "Facebook", "Twitter")
   - Tool names (e.g., "Profound", "Promptmonitor", "Writesonic GEO Platform")
   - Any branded entity that could be considered competition or alternative

2. **What to Exclude:**
   - The target organization itself and its variations
   - Generic terms (e.g., "AI tools", "analytics platforms", "search engines")
   - Non-competitive entities (e.g., "users", "customers", "developers")
   - Technical terms or concepts (e.g., "machine learning", "natural language processing")
   - Industry terms (e.g., "credit unions", "financial services")

3. **Extraction Guidelines:**
   - Extract the most commonly used or official name for each competitor
   - If a company has multiple products mentioned, list each product separately
   - Remove duplicates and variations of the same entity
   - Focus on entities that could be considered alternatives or competitors
   - Include both direct competitors and indirect competitors mentioned

**EXAMPLES:**

Example 1: "Leading AI tools include ChatGPT, Claude, Gemini, and Senso.ai for content optimization."
→ Extract: ["ChatGPT", "Claude", "Gemini"] (exclude Senso.ai as it's the target)

Example 2: "Microsoft's Azure competes with Google Cloud and Amazon Web Services in the enterprise market."
→ Extract: ["Microsoft", "Azure", "Google Cloud", "Amazon Web Services"]

Example 3: "Popular analytics platforms like Google Analytics, Adobe Analytics, and Mixpanel offer similar features."
→ Extract: ["Google Analytics", "Adobe Analytics", "Mixpanel"]

**RESPONSE TO ANALYZE:**
```
{response}
```

**INSTRUCTIONS:**
- Return only the list of competitor names
- Use the most recognizable/official name for each competitor
- Remove any duplicates or very similar variations
- If no competitors are mentioned, return an empty list
- Do not include the target organization or generic terms"""

    response_obj = client.responses.parse(
        model="gpt-4.1-mini",
        input=[
            {
                "role": "user",
                "content": prompt,
            },
        ],
        text_format=CompetitorList,
    )
    return response_obj.output_parsed.competitors


def find_all_json_files(directory: str) -> list[str]:
    files = []
    for file in os.listdir(directory):
        if file.endswith(".json"):
            files.append(os.path.join(directory, file))
    return files


def is_mention_length_within_tolerance(generated_len: int, target_len: int, tolerance: float = 0.15) -> bool:
    """Check if generated mention length is within 10% tolerance of target length"""
    if target_len == 0:
        return generated_len == 0
    
    lower_bound = target_len * (1 - tolerance)
    upper_bound = target_len * (1 + tolerance)
    return lower_bound <= generated_len <= upper_bound


def extract_citations(response: str, primary_websites: list[str]) -> list[dict]:
    """
    Extract citations from response text using regex and classify as primary or secondary.
    
    Args:
        response: The response text to extract citations from
        primary_websites: List of primary website domains (e.g., ["https://www.senso.ai", "https://senso.ai"])
    
    Returns:
        List of citation dictionaries with 'url' and 'type' keys
    """
    # Regex pattern to match URLs in parentheses (common citation format)
    # Matches patterns like (domain.com), (https://domain.com), (www.domain.com)
    citation_pattern = re.compile(r'https?://(?:www\.|(?!www))[a-zA-Z0-9][a-zA-Z0-9-]+[a-zA-Z0-9]\.[^\s,.)}\]]{2,}|www\.[a-zA-Z0-9][a-zA-Z0-9-]+[a-zA-Z0-9]\.[^\s,.)}\]]{2,}|https?://[a-zA-Z0-9]+\.[^\s,.)}\]]{2,}|[a-zA-Z0-9-]+\.[a-zA-Z]{2,6}\.[^\s,.)}\]]{2,}')

    # Find all potential citations
    matches = re.findall(citation_pattern, response)
    
    citations = []
    seen_urls = set()  # To avoid duplicates
    
    for match in matches:
        # Clean up the match
        url = match.strip()
        
        # Skip if empty or already seen
        if not url or url in seen_urls:
            continue
            
        # Normalize URL for comparison
        normalized_url = url.lower()
        if not normalized_url.startswith(('http://', 'https://')):
            normalized_url = 'https://' + normalized_url
        
        # Remove trailing slashes and common parameters for comparison
        normalized_url = normalized_url.rstrip('/')
        
        # Determine if this is a primary or secondary citation
        citation_type = "secondary"  # Default to secondary
        
        for primary_site in primary_websites:
            primary_normalized = primary_site.lower().rstrip('/')
            
            # Check if the citation URL matches or is a subdomain of primary sites
            if (normalized_url == primary_normalized or 
                normalized_url.startswith(primary_normalized + '/') or
                primary_normalized.replace('https://www.', 'https://') == normalized_url or
                primary_normalized.replace('https://', 'https://www.') == normalized_url):
                citation_type = "primary"
                break
        
        citations.append({
            "url": url,
            "type": citation_type
        })
        
        seen_urls.add(url)
    
    return citations


if __name__ == "__main__":
    # Get all JSON files from the data directory
    json_files = find_all_json_files("data")
    total_questions = len(json_files)
    
    if total_questions == 0:
        print("No JSON files found in data directory!")
        exit(1)
    
    correct_mentions = 0
    correct_sentiments = 0
    correct_mention_lengths = 0
    correct_citations = 0
    correct_competitors = 0
    failed_questions = []
    
    print(f"Running accuracy evaluation on {total_questions} files...\n")
    
    for i, json_file in enumerate(tqdm(json_files, desc="Processing questions", unit="question"), 1):
        with open(json_file, "r") as f:
            data = json.load(f)
        
        websites = ["https://www.senso.ai", "https://senso.ai", "https://geo.senso.ai"]
        names = get_names("Senso.ai", websites)
        
        # Check if organization is mentioned
        mentioned = False
        for name in names:
            if name.lower() in data["response"].lower():
                mentioned = True
                break
        
        # Get mention text and sentiment only if mentioned
        if mentioned:
            resp = get_mention_text("Senso.ai", names, data["response"])
        else:
            # Create a mock response for non-mentioned cases
            from types import SimpleNamespace
            resp = SimpleNamespace(mention_text="", sentiment="neutral")
        
        # Extract citations
        extracted_citations = extract_citations(data["response"], websites)
        
        # Extract competitors
        extracted_competitors = get_competitors("Senso.ai", data["response"])
        
        # Extract data from nested structure
        data_section = data.get("data", data)  # Fallback to root if "data" key doesn't exist
        
        # Evaluate accuracy
        mention_correct = mentioned == data_section["target_mentioned"]
        sentiment_correct = resp.sentiment.lower() == data_section.get("target_sentiment", "neutral").lower()
        mention_length_correct = is_mention_length_within_tolerance(
            len(resp.mention_text), 
            len(data_section["mention_text"])
        )
        
        # Evaluate citations - just check count matches
        target_citations = data_section.get("citations", [])
        citations_correct = len(extracted_citations) == len(target_citations)
        
        # Evaluate competitors - check if extracted competitors match target competitors
        target_competitors = data_section.get("competitors", [])
        target_competitor_names = [comp.get("name", comp) if isinstance(comp, dict) else comp for comp in target_competitors]
        competitors_correct = set(extracted_competitors) == set(target_competitor_names)
        
        # Update counters
        if mention_correct:
            correct_mentions += 1
        if sentiment_correct:
            correct_sentiments += 1
        if mention_length_correct:
            correct_mention_lengths += 1
        if citations_correct:
            correct_citations += 1
        if competitors_correct:
            correct_competitors += 1
        
        # Track failed questions for detailed output
        question_passed = mention_correct and sentiment_correct and mention_length_correct and citations_correct and competitors_correct
        if not question_passed:
            failed_questions.append({
                'question': os.path.basename(json_file),  # Use filename instead of number
                'mention_correct': mention_correct,
                'sentiment_correct': sentiment_correct,
                'mention_length_correct': mention_length_correct,
                'citations_correct': citations_correct,
                'competitors_correct': competitors_correct,
                'generated_mentioned': mentioned,
                'target_mentioned': data_section["target_mentioned"],
                'generated_sentiment': resp.sentiment,
                'target_sentiment': data_section.get("target_sentiment", "neutral"),
                'generated_mention_len': len(resp.mention_text),
                'target_mention_len': len(data_section["mention_text"]),
                'generated_citations_count': len(extracted_citations),
                'target_citations_count': len(target_citations),
                'extracted_citations': extracted_citations,
                'target_citations': target_citations,
                'extracted_competitors': extracted_competitors,
                'target_competitors': target_competitor_names
            })
    
    # Print high-level accuracy metrics
    print("=" * 50)
    print("ACCURACY METRICS")
    print("=" * 50)
    print(f"Organization Mention Detection: {correct_mentions}/{total_questions} ({correct_mentions/total_questions*100:.1f}%)")
    print(f"Sentiment Analysis: {correct_sentiments}/{total_questions} ({correct_sentiments/total_questions*100:.1f}%)")
    print(f"Mention Text Length (±15%): {correct_mention_lengths}/{total_questions} ({correct_mention_lengths/total_questions*100:.1f}%)")
    print(f"Citation Extraction: {correct_citations}/{total_questions} ({correct_citations/total_questions*100:.1f}%)")
    print(f"Competitor Extraction: {correct_competitors}/{total_questions} ({correct_competitors/total_questions*100:.1f}%)")
    
    # Calculate overall accuracy (all five metrics must be correct)
    overall_correct = total_questions - len(failed_questions)
    print(f"Overall Accuracy (all metrics): {overall_correct}/{total_questions} ({overall_correct/total_questions*100:.1f}%)")
    
    # Show details for failed questions only
    if failed_questions:
        print("\n" + "=" * 50)
        print("FAILED QUESTIONS DETAILS")
        print("=" * 50)
        
        for failed in failed_questions:
            print(f"\nFile {failed['question']} - FAILED")
            print("-" * 30)
            
            if not failed['mention_correct']:
                print(f"❌ Mention Detection: Generated={failed['generated_mentioned']}, Target={failed['target_mentioned']}")
            else:
                print(f"✅ Mention Detection: {failed['generated_mentioned']}")
            
            if not failed['sentiment_correct']:
                print(f"❌ Sentiment: Generated='{failed['generated_sentiment']}', Target='{failed['target_sentiment']}'")
            else:
                print(f"✅ Sentiment: {failed['generated_sentiment']}")
            
            if not failed['mention_length_correct']:
                print(f"❌ Mention Length: Generated={failed['generated_mention_len']}, Target={failed['target_mention_len']} (±15% tolerance)")
            else:
                print(f"✅ Mention Length: {failed['generated_mention_len']} (within tolerance)")
            
            if not failed['citations_correct']:
                print(f"❌ Citation Extraction: Generated={failed['generated_citations_count']}, Target={failed['target_citations_count']}")
                print(f"Generated Citations: {failed['extracted_citations']}")
                print(f"Target Citations: {failed['target_citations']}")
            else:
                print(f"✅ Citation Extraction: {failed['generated_citations_count']} citations")
            
            if not failed['competitors_correct']:
                print(f"❌ Competitor Extraction: Generated={failed['extracted_competitors']}, Target={failed['target_competitors']}")
            else:
                print(f"✅ Competitor Extraction: {len(failed['extracted_competitors'])} competitors")
    else:
        print("\n🎉 All questions passed!")