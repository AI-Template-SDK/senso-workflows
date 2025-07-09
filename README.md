# Senso Workflows

![Version](https://img.shields.io/badge/version-0.1.0-blue)
![Go](https://img.shields.io/badge/go-1.24-00ADD8?logo=go&logoColor=white)
![Lint](https://github.com/AI-Template-SDK/senso/actions/workflows/golangci-lint.yml/badge.svg)
![Build](https://github.com/AI-Template-SDK/senso/actions/workflows/build.yml/badge.svg)
![Production](https://github.com/AI-Template-SDK/senso/actions/workflows/production.yml/badge.svg)
![Coverage](https://img.shields.io/badge/coverage-0%25-red)
![API Status](https://img.shields.io/badge/status-early%20access-purple)
![Last Updated](https://img.shields.io/badge/updated-July%202025-lightgrey)

A workflow orchestration service built with Go and Inngest that automates question/extract processing for organizations in the Senso platform. It supports both manual triggering and automated weekly scheduling.

## Overview

Senso Workflows is a microservice that:
- Processes organizations through a series of questions using ChatGPT
- Extracts insights from the responses
- Calculates analytics based on the results
- Pushes the analytics back to the main application
- Supports both manual (event-driven) and scheduled (weekly cycle) execution

## Environment Configuration

This project uses environment variables for configuration. Create a `.env` file in the project root with your configuration values.

### Required Environment Variables

```bash
# Inngest Configuration
INNGEST_EVENT_KEY=your-inngest-event-key
INNGEST_SIGNING_KEY=your-inngest-signing-key  # Production only

# API Keys
OPENAI_API_KEY=your-openai-api-key
ANTHROPIC_API_KEY=your-anthropic-api-key

# Application
APPLICATION_API_URL=https://your-app-api.com
DATABASE_URL=your-database-connection-string
API_TOKEN=your-api-authentication-token

# GitHub (for Docker builds)
GITHUB_PAT=your-github-personal-access-token

# Optional
PORT=8080                    # Default: 8080
ENVIRONMENT=production       # Default: development
```

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



## Private Repository Setup

This project depends on private GitHub repositories. You need to configure your Go environment to access the private `senso-api` repository.

### 1. Configure Go to recognize private repositories

Tell Go to treat AI-Template-SDK repositories as private:

```bash
go env -w GOPRIVATE=github.com/AI-Template-SDK/*
```

This prevents Go from trying to fetch these modules from the public proxy.

### 2. Configure Git to use SSH for GitHub

Set up Git to automatically use SSH instead of HTTPS for GitHub repositories:

```bash
git config --global url."git@github.com:".insteadOf "https://github.com/"
```

This ensures that Go can authenticate when fetching private dependencies.

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

### Running with Docker Compose

For local development with Docker:

```bash
# Create a .env file with your GitHub PAT and API keys
echo "GITHUB_PAT=your_github_token_here" > .env
echo "OPENAI_API_KEY=your_openai_key" >> .env
echo "ANTHROPIC_API_KEY=your_anthropic_key" >> .env

# Start the services
docker-compose up --build

# The application will be available at http://localhost:8080
# Inngest dev server will be available at http://localhost:8288
# PostgreSQL will be available at localhost:5432
```

The Docker Compose setup includes:
- **worker**: The senso-workflows application
- **inngest**: Inngest dev server for workflow orchestration
- **migrate**: Database migrations from senso-api package
- **postgres**: PostgreSQL database with automatic initialization
- **Networking**: Services can communicate with each other
- **Volume persistence**: Database data persists between restarts

### Building Docker Image Manually

```bash
docker build -f docker/worker.Dockerfile -t senso-workflows . --build-arg GITHUB_PAT=your_github_token
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
├── docker/                   # Docker configurations
│   ├── worker.Dockerfile     # Main application container
│   └── migrate.Dockerfile    # Database migration container
├── docker-compose.yml        # Docker Compose configuration
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

## Service Endpoints

When running with Docker Compose, the following services are available:

| Service | URL | Description |
|---------|-----|-------------|
| **Application** | http://localhost:8080 | Main senso-workflows API |
| **Inngest Dashboard** | http://localhost:8288 | Workflow monitoring and debugging |
| **Health Check** | http://localhost:8080/health | Service health status |
| **Database** | localhost:5432 | PostgreSQL database |

The Inngest Dashboard provides real-time workflow monitoring, execution logs, and debugging capabilities.

## Database Setup

The Docker Compose setup automatically handles database migrations:

### Migrations
- **Source**: Migrations are pulled from the `senso-api` dependency (`github.com/AI-Template-SDK/senso-api`)
- **Process**: The migrate service downloads the senso-api module and extracts migrations
- **Tool**: Uses `migrate/migrate` to run database schema migrations
- **Order**: Runs after PostgreSQL is healthy, before the main application starts

This ensures your database schema is always up-to-date with the senso-api package version specified in `go.mod`.

## GitHub Authentication Setup

Since this project depends on the private `senso-api` repository, you need a GitHub Personal Access Token (PAT) to build the Docker images.

### Creating a GitHub PAT

1. Go to [GitHub Settings > Developer settings > Personal access tokens](https://github.com/settings/tokens)
2. Click "Generate new token (classic)"
3. Give it a descriptive name like "Senso Workflows Docker"
4. Select the following scopes:
   - `repo` (Full control of private repositories)
5. Click "Generate token"
6. Copy the token (you won't be able to see it again!)

### Using the PAT

Add your token to your environment file:

```bash
# In docker.env or .env
GITHUB_PAT=ghp_your_actual_token_here
```

**Security Note**: Never commit your actual PAT to version control. The token should remain in your local environment files only.

