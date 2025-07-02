# Senso Workflows

A workflow orchestration service built with Go and Inngest that automates question/extract processing for organizations in the Senso platform. It supports both manual triggering and automated weekly scheduling.

## Overview

Senso Workflows is a microservice that:
- Processes organizations through a series of questions using ChatGPT
- Extracts insights from the responses
- Calculates analytics based on the results
- Pushes the analytics back to the main application
- Supports both manual (event-driven) and scheduled (weekly cycle) execution

## Environment Configuration

This project uses environment variables for configuration. We support multiple ways to set them:

### Local Development

For local development, the app looks for environment files in this order:
1. `.env` (create your own from the examples below)
2. `dev.env` (included with sensible defaults for local development)

To get started quickly with local development:
```bash
# The included dev.env file has everything configured for local development
# Just make sure Inngest Dev Server is running:
npx inngest-cli@latest dev

# Then build and run the app:
go build
./senso-workflows
```

### Production

For production, copy `prod.env.example` to `.env` and update with your real values:
```bash
cp prod.env.example .env
# Edit .env with your production values
```

The main differences between development and production:
- **Development**: Uses local Inngest Dev Server (`INNGEST_BASE_URL=http://localhost:8288`)
- **Production**: Uses Inngest Cloud (no `INNGEST_BASE_URL` needed)
- **Development**: Can use dummy API keys for testing
- **Production**: Requires real API keys and proper authentication

## Features

### 🚀 Dual Execution Modes

1. **Manual Triggering**: Organizations can be processed on-demand via event triggers
2. **Scheduled Processing**: Organizations are automatically processed weekly based on their creation day

### 🔄 Workflow Steps

1. Fetch organization details
2. Retrieve organization-specific questions
3. Run questions through ChatGPT (in parallel)
4. Extract insights from responses (in parallel)
5. Calculate analytics
6. Push results back to the main application

### 📊 Built-in Monitoring

- Weekly load distribution analyzer
- Performance metrics tracking
- Failure handling and retries

## Architecture

```
┌─────────────────┐     ┌──────────────┐     ┌─────────────┐
│ External Apps   │────>│   Inngest    │────>│  Workflows  │
│ (Manual Trigger)│     │   Events     │     │  Processor  │
└─────────────────┘     └──────────────┘     └──────┬───────┘
                                                     │
┌─────────────────┐     ┌──────────────┐            │
│ Cron Scheduler  │────>│   Inngest    │────────────┘
│ (Daily 2AM UTC) │     │   Scheduler  │
└─────────────────┘     └──────────────┘

                        ┌──────────────┐
                        │   Services   │
                        ├──────────────┤
                        │ Org Service  │
                        │ ChatGPT API  │
                        │ Analytics    │
                        └──────────────┘
```

## Prerequisites

- Go 1.24 or higher
- Git
- Access to private AI-Template-SDK repositories
- Inngest account and API keys
- OpenAI API key

## Environment Variables

```bash
# Required
INNGEST_EVENT_KEY=your-inngest-event-key
INNGEST_SIGNING_KEY=your-inngest-signing-key
OPENAI_API_KEY=your-openai-api-key
APPLICATION_API_URL=https://your-app-api.com
DATABASE_URL=your-database-connection-string
API_TOKEN=your-api-authentication-token

# Optional
PORT=8080                    # Default: 8080
ENVIRONMENT=production       # Default: development
```

## Setup

This project depends on private GitHub repositories. Before you can build or run this project, you need to configure your development environment.

**⚠️ Important: Please read the [Private Repository Setup Guide](./PRIVATE_REPOS_SETUP.md) before proceeding.**

## Quick Start

After completing the private repository setup:

```bash
# Clone the repository
git clone git@github.com:AI-Template-SDK/senso-workflows.git
cd senso-workflows

# Download dependencies
go mod download

# Build the project
go build

# Run the project
./senso-workflows
```

## API Endpoints

### Health Check
```bash
GET /health
```
Returns the service health status.

### Inngest Webhook
```bash
POST /api/inngest
```
Webhook endpoint for Inngest to communicate with the service.

## Triggering Workflows

### Manual Trigger

Send an event to Inngest to process a specific organization:

```json
{
  "name": "org.process",
  "data": {
    "org_id": "org-uuid-here",
    "triggered_by": "manual",
    "user_id": "user-uuid-here"
  }
}
```

### Scheduled Processing

Organizations are automatically processed based on their creation weekday:
- Monday creations → Processed every Monday
- Tuesday creations → Processed every Tuesday
- And so on...

The scheduler runs daily at 2 AM UTC and triggers processing for all active organizations created on that weekday.

## Development

### Running Locally

```bash
# Set environment variables
export INNGEST_EVENT_KEY=your-key
export OPENAI_API_KEY=your-key
# ... other required vars

# Run in development mode
go run main.go
```

### Building Docker Image

```bash
docker build -t senso-workflows .
docker run -p 8080:8080 \
  -e INNGEST_EVENT_KEY=your-key \
  -e OPENAI_API_KEY=your-key \
  senso-workflows
```

## Project Structure

```
senso-workflows/
├── main.go                   # Application entry point
├── internal/
│   ├── config/               # Configuration management
│   │   └── config.go
│   └── models/               # Data models
│       └── models.go
├── services/                 # External service integrations
│   ├── interfaces.go         # Service interfaces
│   └── org_service.go        # Organization service
├── workflows/                # Inngest workflow definitions
│   ├── org_processor.go      # Main org processing workflow
│   ├── scheduled_processor.go # Scheduled workflow trigger
│   └── monitoring.go         # Load monitoring workflow
├── Dockerfile                # Container configuration
├── go.mod                    # Go module definition
├── go.sum                    # Go module checksums
├── README.md                 # This file
└── PRIVATE_REPOS_SETUP.md    # Private repo setup guide
```

## Workflow Details

### ProcessOrg Workflow

Processes a single organization through the question/extract pipeline:

1. **Concurrency**: Limited to 50 concurrent org processes
2. **Retries**: Automatic retry up to 3 times on failure
3. **Parallelization**: Questions and extracts run in parallel for efficiency
4. **Event-driven**: Triggered by `org.process` events

### DailyOrgProcessor Workflow

Automatically triggers org processing based on creation weekday:

1. **Schedule**: Runs daily at 2 AM UTC
2. **Selection**: Finds all active orgs created on the current weekday
3. **Triggering**: Sends individual process events for each org
4. **Tracking**: Records success/failure for each triggered org

### WeeklyLoadAnalyzer Workflow

Monitors and reports on workflow load distribution:

1. **Schedule**: Runs weekly on Sundays at midnight
2. **Analysis**: Calculates org distribution across weekdays
3. **Recommendations**: Suggests load balancing strategies if needed

## Dependencies

- `github.com/inngest/inngestgo` - Workflow orchestration
- `github.com/AI-Template-SDK/senso-api` - Core API models and database interfaces
- `github.com/google/uuid` - UUID generation

## Data Models

This service uses the `Org` model from `senso-api` for organization data and defines workflow-specific models:

- **Question**: Represents questions to be processed by ChatGPT
- **QuestionResult**: Stores the AI-generated responses
- **ExtractResult**: Contains extracted insights from responses
- **Analytics**: Aggregated analytics data
- **OrgSummary**: Lightweight org data for scheduling

## Monitoring and Debugging

1. **Inngest Dashboard**: Monitor workflow executions, view logs, and debug failures
2. **Health Endpoint**: Check service status at `/health`
3. **Structured Logging**: All workflows include detailed logging
4. **Metrics**: Track processing times, success rates, and load distribution

## Best Practices

1. **Idempotency**: All workflows are designed to be idempotent
2. **Error Handling**: Comprehensive error handling with context
3. **Timeouts**: Appropriate timeouts for external service calls
4. **Concurrency Control**: Limited concurrent executions to prevent overload

## Troubleshooting

### Workflow Not Triggering
- Verify Inngest event key is correct
- Check Inngest dashboard for event reception
- Ensure webhook endpoint is accessible

### Processing Failures
- Check OpenAI API key validity
- Verify external service endpoints
- Review Inngest function logs

### Schedule Issues
- Confirm timezone settings (uses UTC)
- Check org creation dates and weekdays
- Verify cron expression in scheduled processor