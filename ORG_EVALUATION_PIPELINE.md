# Org Evaluation Pipeline

## Overview

The Org Evaluation Pipeline is a new, advanced brand analysis system that executes questions with web search and processes the responses using enhanced methodology from the Python `run.py` script. This pipeline runs in parallel to the existing competitive intelligence workflows, providing advanced brand mention analysis, competitor identification, and citation extraction using the new org evaluation methodology.

## Key Features

### 1. **Name Variation Generation**
- Generates realistic brand name variations using AI
- Includes case variations, spacing variations, legal forms, acronyms
- Based on organization name and associated websites
- Implements the `get_names()` functionality from Python `run.py`

### 2. **Advanced Mention Extraction**
- Extracts substantive content about target organizations
- Measures share-of-voice (how much content is genuinely ABOUT the brand)
- Analyzes sentiment (positive, negative, neutral)
- Ranks mention prominence
- Implements the `get_mention_text()` functionality from Python `run.py`

### 3. **Competitor Identification**
- Identifies competitor brands, products, and services
- Excludes generic terms and non-competitive entities
- Extracts both direct and indirect competitors
- Implements the `get_competitors()` functionality from Python `run.py`

### 4. **Citation Analysis**
- Extracts URLs from response text using regex
- Classifies citations as "primary" (org's own domains) or "secondary" (external)
- Matches against organization's known website domains
- Implements the `extract_citations()` functionality from Python `run.py`

## Architecture

### Components

1. **OrgEvaluationService** (`services/org_evaluation_service.go`)
   - Core service implementing all extraction logic
   - Uses Azure OpenAI SDK with structured outputs
   - Handles cost tracking and token usage

2. **OrgEvaluationProcessor** (`workflows/org_evaluation_processor.go`)
   - Inngest workflow processor
   - Orchestrates the evaluation pipeline
   - Triggered by `"org.evaluation.process"` events

3. **Database Models**
   - `org_evals`: Organization evaluation results
   - `org_citations`: Extracted citations with classification
   - `org_competitors`: Identified competitor names

### Data Flow

```
1. Trigger Event (org.evaluation.process)
   ↓
2. Fetch Org Details (Questions, Models, Locations)
   ↓
3. Execute Question Matrix:
   - For each Question × Model × Location:
     - Execute AI Call with Web Search
     - Store Question Run
     - Generate Name Variations (AI)
     - Extract Org Evaluation (AI)
     - Extract Competitors (AI)
     - Extract Citations (Regex)
     - Store in org_evals, org_citations, org_competitors
   ↓
4. Generate Processing Summary
```

## Database Schema

### org_evals
```sql
- org_eval_id (UUID, PK)
- question_run_id (UUID, FK)
- org_id (UUID, FK)
- mentioned (BOOLEAN)
- citation (BOOLEAN)
- sentiment (TEXT) -- "positive", "negative", "neutral"
- mention_text (TEXT) -- Substantive content about org
- mention_rank (INTEGER) -- Prominence ranking
- input_tokens, output_tokens, total_cost (Cost tracking)
- created_at, updated_at, deleted_at
```

### org_citations
```sql
- org_citation_id (UUID, PK)
- question_run_id (UUID, FK)
- org_id (UUID, FK)
- url (TEXT)
- type (TEXT) -- "primary" or "secondary"
- created_at, updated_at, deleted_at
```

### org_competitors
```sql
- org_competitor_id (UUID, PK)
- question_run_id (UUID, FK)
- org_id (UUID, FK)
- name (TEXT) -- Competitor name
- created_at, updated_at, deleted_at
```

## Usage

### Triggering the Pipeline

#### Via Python Trigger Scripts

**Bulk Processing (All Orgs):**
```bash
python trigger_org_evaluation_workflow.py
```

**Single Org Testing:**
```bash
python trigger_single_org_evaluation.py <org_id>
# Example:
python trigger_single_org_evaluation.py 123e4567-e89b-12d3-a456-426614174000
```

#### Via API Endpoint
```bash
curl -X POST http://localhost:8080/test/trigger-org-evaluation
```

#### Via Inngest Event
```go
evt := inngestgo.Event{
    Name: "org.evaluation.process",
    Data: map[string]interface{}{
        "org_id":       "your-org-uuid",
        "triggered_by": "api_call",
        "user_id":      "user-uuid",
    },
}
client.Send(ctx, evt)
```

### Service Methods

#### Generate Name Variations
```go
variations, err := orgEvaluationService.GenerateNameVariations(
    ctx, 
    "Senso.ai", 
    []string{"https://senso.ai", "https://www.senso.ai"}
)
```

#### Extract Org Evaluation
```go
result, err := orgEvaluationService.ExtractOrgEvaluation(
    ctx,
    questionRunID,
    orgID,
    "Senso.ai",
    []string{"https://senso.ai"},
    responseText
)
```

#### Extract Competitors
```go
competitors, err := orgEvaluationService.ExtractCompetitors(
    ctx,
    questionRunID,
    orgID,
    "Senso.ai",
    responseText
)
```

#### Extract Citations
```go
citations, err := orgEvaluationService.ExtractCitations(
    ctx,
    questionRunID,
    orgID,
    responseText,
    []string{"https://senso.ai"}
)
```

## Configuration

### Azure OpenAI Setup
The pipeline uses the same Azure OpenAI configuration as existing services:

```env
AZURE_OPENAI_ENDPOINT=your-endpoint
AZURE_OPENAI_KEY=your-key
AZURE_OPENAI_DEPLOYMENT_NAME=your-deployment
```

### Model Selection
- **Name Variations**: GPT-4o-mini (cost-effective for simple generation)
- **Org Evaluation**: GPT-4o (high-quality analysis required)
- **Competitors**: GPT-4o-mini (straightforward extraction)
- **Citations**: Regex-based (no AI required)

## Trigger Files

### Available Trigger Scripts

1. **`trigger_org_evaluation_workflow.py`**
   - Processes all organizations from `example_orgs.txt`
   - Bulk execution for production runs
   - Enhanced logging and progress tracking

2. **`trigger_single_org_evaluation.py`**
   - Processes a single organization by ID
   - Perfect for testing and debugging
   - Command-line argument support

### Configuration

Both trigger files use the same configuration:
```python
ORG_FILE = "example_orgs.txt"          # File containing org IDs
TRIGGERED_BY = "manual"                # Trigger source identifier
USER_ID = None                         # Optional user ID
SCHEDULED_DATE = None                  # Optional scheduling
INNGEST_URL = "http://localhost:8288/e/dev"  # Inngest endpoint
```

### Example Output

```
============================================================
🚀 ORG EVALUATION PIPELINE TRIGGER
============================================================
📋 Processing 3 organizations
🎯 Event: org.evaluation.process
🔗 Endpoint: http://localhost:8288/e/dev
👤 Triggered by: manual
============================================================

[1/3] Triggering org evaluation for: 123e4567-e89b-12d3-a456-426614174000
✅ Org Evaluation: 123e4567-e89b-12d3-a456-426614174000 | Status: 200
Response: {'ids': ['01JGXXX...'], 'status': 'ok'}

[2/3] Triggering org evaluation for: 456e7890-e89b-12d3-a456-426614174001
✅ Org Evaluation: 456e7890-e89b-12d3-a456-426614174001 | Status: 200
Response: {'ids': ['01JGYYY...'], 'status': 'ok'}
```

## Integration Points

### Repository Manager
The pipeline integrates with the existing repository manager:

```go
// Added to RepositoryManager
OrgEvalRepo       interfaces.OrgEvalRepository
OrgCitationRepo   interfaces.OrgCitationRepository  
OrgCompetitorRepo interfaces.OrgCompetitorRepository
```

### Service Registration
```go
// In main.go
orgEvaluationService := services.NewOrgEvaluationService(cfg, repoManager)
orgEvaluationProcessor := workflows.NewOrgEvaluationProcessor(
    orgService,
    orgEvaluationService,
    cfg,
)
```

## Monitoring & Logging

### Log Patterns
```
[GenerateNameVariations] 🔍 Generating name variations for org: Senso.ai
[ExtractOrgEvaluation] 🔍 Processing org evaluation for question run abc-123
[ExtractCompetitors] 🔍 Processing competitors for question run abc-123
[ExtractCitations] 🔍 Processing citations for question run abc-123
[ProcessOrgEvaluation] 🎉 Org evaluation pipeline completed successfully
```

### Cost Tracking
All AI operations include:
- Input/output token counts
- Cost calculation per operation
- Total cost aggregation in processing summary

## Differences from Existing Pipeline

| Aspect | Existing Pipeline | New Org Evaluation Pipeline |
|--------|------------------|------------------------------|
| **Purpose** | Competitive intelligence | Organization-specific brand analysis |
| **Input** | Questions → AI responses → Mentions/Claims | Questions → AI responses → Org Evaluations |
| **Trigger** | `"org.process"` | `"org.evaluation.process"` |
| **Tables** | `question_run_mentions`, `question_run_claims`, `question_run_citations` | `org_evals`, `org_citations`, `org_competitors` |
| **Focus** | General mentions & claims | Substantive brand content & share-of-voice |
| **Methodology** | Existing extraction logic | Advanced Python `run.py` methodology |
| **AI Calls** | Question execution + Data extraction | Question execution + Enhanced evaluation |

## Future Enhancements

1. **Database Integration**: Complete the `ProcessOrgQuestionRunsFromDatabase` method
2. **Batch Processing**: Add support for processing multiple orgs
3. **Analytics Dashboard**: Create reporting for org evaluation metrics
4. **Real-time Processing**: Process new question runs automatically
5. **Performance Optimization**: Implement caching and parallel processing

## Troubleshooting

### Common Issues

1. **"Method not yet implemented"**: The database integration is not complete
2. **Azure model errors**: Check Azure OpenAI deployment configuration
3. **Cost tracking issues**: Verify cost service configuration
4. **Repository errors**: Ensure org evaluation repositories are properly initialized

### Debug Mode
Enable detailed logging by checking the service initialization logs for Azure/OpenAI configuration details.

## Security Considerations

- All AI calls use the same security model as existing services
- No additional API keys or endpoints required
- Database access follows existing repository patterns
- Cost tracking prevents runaway expenses

## Performance

### Expected Processing Times
- **Name Variations**: ~2-3 seconds per org
- **Org Evaluation**: ~5-8 seconds per question run
- **Competitors**: ~2-3 seconds per question run  
- **Citations**: <1 second per question run (regex-based)

### Scalability
- Processes question runs sequentially to avoid API rate limits
- Includes error handling and graceful degradation
- Supports batch processing of multiple question runs per org 