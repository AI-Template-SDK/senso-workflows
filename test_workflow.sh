#!/bin/bash

# Test script for ProcessOrg workflow
# This script triggers the workflow via the test endpoint

echo "🚀 Testing ProcessOrg Workflow"
echo "=============================="

# Default values
HOST="${HOST:-localhost}"
PORT="${PORT:-8080}"
ORG_ID="${ORG_ID:-test-org-123}"

echo "📍 Target: http://${HOST}:${PORT}"
echo "🏢 Organization ID: ${ORG_ID}"
echo ""

# Check if server is healthy
echo "🏥 Checking server health..."
HEALTH_RESPONSE=$(curl -s "http://${HOST}:${PORT}/health")
if [[ $? -ne 0 ]]; then
    echo "❌ Server is not responding. Make sure the service is running."
    exit 1
fi

echo "✅ Server is healthy: ${HEALTH_RESPONSE}"
echo ""

# Trigger the workflow
echo "🔄 Triggering ProcessOrg workflow..."
TRIGGER_RESPONSE=$(curl -s -X POST "http://${HOST}:${PORT}/test/trigger-org")

if [[ $? -eq 0 ]]; then
    echo "✅ Workflow triggered successfully!"
    echo "📋 Response: ${TRIGGER_RESPONSE}"
    echo ""
    echo "💡 Check your Inngest dashboard to monitor the workflow execution"
    echo "   You should see:"
    echo "   1. get-org-details"
    echo "   2. get-org-questions"
    echo "   3. run-questions-parallel (with question-* sub-steps)"
    echo "   4. run-extracts-parallel (with extract-* sub-steps)"
    echo "   5. calculate-analytics"
    echo "   6. push-analytics"
else
    echo "❌ Failed to trigger workflow"
    exit 1
fi 