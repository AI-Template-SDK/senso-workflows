# Testing the ProcessOrg Workflow

This document explains how to test the ProcessOrg workflow with mock implementations.

## Mock Implementations

All service methods have been modified to return mock data for testing purposes. Each method prints what it's doing and returns test data without making actual API calls.

### Modified Services

1. **OrgService** (`services/org_service.go`)
   - `GetOrgDetails()` - Returns a mock organization with test data
   - `GetOrgQuestions()` - Returns 3 mock questions

2. **ChatGPTService** (`services/chatgpt_service.go`)
   - `RunQuestion()` - Returns mock ChatGPT responses without calling OpenAI API

3. **AnalyticsService** (`services/analytics_service.go`)
   - `RunExtract()` - Returns mock extracted insights
   - `CalculateAnalytics()` - Returns mock analytics metrics and insights
   - `PushAnalytics()` - Simulates pushing analytics and returns success

## Testing the Workflow

### Option 1: Using the Test Script

```bash
# Run with default settings (localhost:8080)
./test_workflow.sh

# Or specify custom host/port
HOST=myserver PORT=9000 ./test_workflow.sh
```

### Option 2: Manual cURL Command

```bash
# Trigger the workflow
curl -X POST http://localhost:8080/test/trigger-org
```

### Option 3: Direct Inngest Event

If you have the Inngest CLI, you can send an event directly:

```json
{
  "name": "org.process",
  "data": {
    "org_id": "test-org-123",
    "triggered_by": "manual_test",
    "user_id": "test-user"
  }
}
```

## Expected Output

When the workflow runs, you'll see console output from each step:

```
[GetOrgDetails] Fetching details for org: test-org-123
[GetOrgDetails] Returning mock org: Test Organization test-org
[GetOrgQuestions] Fetching questions for org: test-org-123
[GetOrgQuestions] Returning 3 mock questions
[RunQuestion] Processing question 'What is your organization's primary goal?' (ID: q1) for org: test-org-123
[RunQuestion] Generated mock response for question q1: 147 characters
[RunQuestion] Processing question 'How many employees does your organization have?' (ID: q2) for org: test-org-123
[RunQuestion] Generated mock response for question q2: 14 characters
[RunQuestion] Processing question 'What are your main challenges?' (ID: q3) for org: test-org-123
[RunQuestion] Generated mock response for question q3: 174 characters
[RunExtract] Extracting insights from question q1 for org: test-org-123
[RunExtract] Extracted 8 insights from question q1
[RunExtract] Extracting insights from question q2 for org: test-org-123
[RunExtract] Extracted 8 insights from question q2
[RunExtract] Extracting insights from question q3 for org: test-org-123
[RunExtract] Extracted 8 insights from question q3
[CalculateAnalytics] Calculating analytics for org: Test Organization test-org with 3 questions and 3 extracts
[CalculateAnalytics] Generated 7 metrics and 6 insights
[PushAnalytics] Pushing analytics for org: test-org-123
[PushAnalytics] Analytics contains 7 metrics and 6 insights
[PushAnalytics] Metrics summary:
  - engagement_score: 0.87
  - response_quality: 0.92
  - completion_rate: 1.00
  - average_response_time: 2.50
  - sentiment_score: 0.75
  - innovation_index: 0.83
  - readiness_score: 0.79
[PushAnalytics] Analytics push completed successfully
```

## Workflow Steps in Inngest Dashboard

In the Inngest dashboard, you should see the following steps:

1. **get-org-details** - Fetches organization information
2. **get-org-questions** - Retrieves questions for the organization
3. **run-questions-parallel** - Processes all questions in parallel
   - **question-q1** - Processes question 1
   - **question-q2** - Processes question 2
   - **question-q3** - Processes question 3
4. **run-extracts-parallel** - Extracts insights from all responses in parallel
   - **extract-q1** - Extracts insights from question 1 response
   - **extract-q2** - Extracts insights from question 2 response
   - **extract-q3** - Extracts insights from question 3 response
5. **calculate-analytics** - Aggregates all data into analytics
6. **push-analytics** - Pushes the final analytics results

## Restoring Production Functionality

To restore the actual API calls:

1. Revert the changes in the service files
2. Ensure all environment variables are properly set:
   - `OPENAI_API_KEY`
   - `APPLICATION_API_URL`
   - `API_TOKEN`
3. Rebuild and deploy the application

## Notes

- All mock data includes timestamps and realistic-looking values
- The workflow flow and step structure remain unchanged
- Each step has a small delay to simulate processing time
- Error handling is preserved but won't trigger with mock data 