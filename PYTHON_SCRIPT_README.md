# Senso Workflows - Python Trigger Script

This Python script provides a powerful way to trigger the "process org" workflow for multiple organizations with built-in concurrency control and comprehensive monitoring.

## Features

- 🚀 **Concurrency Control**: Limits concurrent workflow triggers (default: 5)
- 🔄 **Automatic Retries**: Configurable retry attempts with exponential backoff
- 📊 **Comprehensive Logging**: Both file and console logging with detailed summaries
- 🎯 **Multiple Input Methods**: Support for command-line, file input, and API-based org discovery
- ⚡ **Async Performance**: High-performance async HTTP requests
- 🛡️ **Error Handling**: Robust error handling with detailed failure reporting

## Installation

1. **Install Python dependencies**:
   ```bash
   pip install -r requirements.txt
   ```

2. **Make the script executable** (optional):
   ```bash
   chmod +x trigger_org_workflow.py
   ```

## Usage

### Basic Usage

**Trigger workflows for specific organizations**:
```bash
python trigger_org_workflow.py --orgs "org1,org2,org3"
```

**Trigger workflows from a file**:
```bash
python trigger_org_workflow.py --file orgs.txt
```

**Trigger workflows for all active organizations**:
```bash
python trigger_org_workflow.py --all-active
```

### Advanced Usage

**Custom concurrency limit**:
```bash
python trigger_org_workflow.py --orgs "org1,org2,org3" --max-concurrent 3
```

**Custom service URL**:
```bash
python trigger_org_workflow.py --orgs "org1" --base-url "http://localhost:8080"
```

**With user tracking**:
```bash
python trigger_org_workflow.py --orgs "org1,org2" --user-id "user123"
```

**Custom retry settings**:
```bash
python trigger_org_workflow.py --orgs "org1" --retry-attempts 5 --timeout 60
```

## Command Line Options

| Option | Description | Default |
|--------|-------------|---------|
| `--orgs` | Comma-separated list of organization IDs | - |
| `--file` | File containing organization IDs (one per line) | - |
| `--all-active` | Trigger workflows for all active organizations | - |
| `--base-url` | Base URL of the Senso Workflows service | `http://localhost:8080` |
| `--max-concurrent` | Maximum concurrent workflow triggers | `5` |
| `--user-id` | User ID who is triggering the workflows | `system` |
| `--retry-attempts` | Number of retry attempts per organization | `3` |
| `--timeout` | Request timeout in seconds | `30` |

## Input Methods

### 1. Command Line (--orgs)
```bash
python trigger_org_workflow.py --orgs "550e8400-e29b-41d4-a716-446655440000,550e8400-e29b-41d4-a716-446655440001"
```

### 2. File Input (--file)
Create a file with organization IDs (one per line):
```txt
550e8400-e29b-41d4-a716-446655440000
550e8400-e29b-41d4-a716-446655440001
550e8400-e29b-41d4-a716-446655440002
```

Then run:
```bash
python trigger_org_workflow.py --file orgs.txt
```

### 3. API Discovery (--all-active)
Automatically fetches all active organizations from the API:
```bash
python trigger_org_workflow.py --all-active
```

## Output and Logging

### Console Output
The script provides real-time feedback:
```
2024-01-15 10:30:00 - INFO - 🚀 Starting workflow triggers for 3 organizations (max concurrent: 5)
2024-01-15 10:30:01 - INFO - Triggering workflow for org 550e8400-e29b-41d4-a716-446655440000 via http://localhost:8000/api/inngest (attempt 1)
2024-01-15 10:30:02 - INFO - ✅ Successfully triggered workflow for org 550e8400-e29b-41d4-a716-446655440000 (event_id: abc123)
```

### Summary Report
After completion, a detailed summary is displayed:
```
============================================================
📊 WORKFLOW TRIGGER SUMMARY
============================================================
Total organizations: 3
✅ Successful: 3
❌ Failed: 0
⏱️  Average response time: 1.23s

✅ Successful organizations:
  - 550e8400-e29b-41d4-a716-446655440000 (event_id: abc123)
  - 550e8400-e29b-41d4-a716-446655440001 (event_id: def456)
  - 550e8400-e29b-41d4-a716-446655440002 (event_id: ghi789)
============================================================
```

### Log File
All activity is logged to `workflow_trigger.log` for debugging and audit purposes.

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | All workflows triggered successfully |
| `1` | Some workflows failed (partial success) |
| `2` | All workflows failed |
| `130` | Script interrupted by user (Ctrl+C) |

## Concurrency Control

The script respects the concurrency limit set in the Go workflow (5 concurrent executions). The Python script's `--max-concurrent` parameter controls how many HTTP requests are made simultaneously, but the actual workflow execution is limited by the Inngest concurrency settings.

### How It Works

1. **Python Level**: Limits concurrent HTTP requests to trigger workflows
2. **Inngest Level**: Queues workflows when the limit (5) is reached
3. **Per-Org Queuing**: Each organization gets its own virtual queue

## Error Handling

### Retry Logic
- **Exponential Backoff**: Retry delays increase with each attempt
- **Multiple Endpoints**: Tries both `/api/inngest` and `/test/trigger-org`
- **Timeout Protection**: Configurable request timeouts

### Failure Scenarios
- **Network Issues**: Automatic retry with backoff
- **Service Unavailable**: Tries alternative endpoints
- **Invalid Org IDs**: Detailed error reporting
- **Rate Limiting**: Respects service limits

## Examples

### Development Testing
```bash
# Test with a single org
python trigger_org_workflow.py --orgs "test-org-123"

# Test with multiple orgs
python trigger_org_workflow.py --file example_orgs.txt

# Test with custom settings
python trigger_org_workflow.py --orgs "org1,org2" --max-concurrent 2 --retry-attempts 5
```

### Production Usage
```bash
# Process all active organizations
python trigger_org_workflow.py --all-active --base-url "https://workflows.senso.ai"

# Process specific organizations with user tracking
python trigger_org_workflow.py --orgs "org1,org2,org3" --user-id "admin-user" --base-url "https://workflows.senso.ai"
```

### Batch Processing
```bash
# Process organizations in batches
python trigger_org_workflow.py --file batch1.txt --max-concurrent 3
python trigger_org_workflow.py --file batch2.txt --max-concurrent 3
```

## Monitoring

### Real-time Monitoring
- Watch the console output for real-time progress
- Monitor the log file for detailed information
- Check the summary report for overall results

### Integration with Monitoring Systems
The script can be integrated with monitoring systems by:
- Parsing the exit codes
- Monitoring the log file
- Using the summary output for dashboards

## Troubleshooting

### Common Issues

**Connection Refused**:
```bash
# Check if the service is running
curl http://localhost:8080/health

# Use the correct base URL
python trigger_org_workflow.py --orgs "org1" --base-url "https://correct-url.com"
```

**Timeout Errors**:
```bash
# Increase timeout
python trigger_org_workflow.py --orgs "org1" --timeout 60

# Reduce concurrency
python trigger_org_workflow.py --orgs "org1,org2,org3" --max-concurrent 2
```

**Invalid Organization IDs**:
- Verify the organization IDs exist in the database
- Check the format (should be valid UUIDs)
- Ensure the organizations are active

### Debug Mode
For detailed debugging, you can modify the logging level in the script:
```python
logging.basicConfig(level=logging.DEBUG, ...)
```

## Security Considerations

- **API Keys**: The script doesn't handle API keys directly (handled by the Go service)
- **User Tracking**: Use `--user-id` to track who triggered workflows
- **Network Security**: Use HTTPS in production environments
- **Log Security**: Be aware that logs may contain sensitive information

## Performance Tips

1. **Optimal Concurrency**: Start with the default (5) and adjust based on service capacity
2. **Batch Processing**: Use file input for large numbers of organizations
3. **Network Optimization**: Use the closest service endpoint
4. **Monitoring**: Watch response times and adjust timeouts accordingly

## Integration Examples

### CI/CD Pipeline
```yaml
# GitHub Actions example
- name: Trigger Workflows
  run: |
    python trigger_org_workflow.py --orgs "${{ env.ORG_IDS }}" --user-id "ci-system"
```

### Cron Job
```bash
# Daily processing of all active orgs
0 2 * * * cd /path/to/script && python trigger_org_workflow.py --all-active --base-url "https://workflows.senso.ai"
```

### Monitoring Script
```bash
#!/bin/bash
python trigger_org_workflow.py --orgs "monitor-org" --user-id "monitoring"
if [ $? -eq 0 ]; then
    echo "Workflow trigger successful"
else
    echo "Workflow trigger failed"
    # Send alert
fi
``` 