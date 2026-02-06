# Deployment Guide - Senso Workflows Step Functions

Complete step-by-step guide to deploy the org evaluation workflow to AWS production.

## Pre-Deployment Checklist

Before starting, ensure you have:

- [ ] AWS Account with admin access (or specific IAM permissions)
- [ ] AWS CLI v2 installed and configured
- [ ] Terraform >= 1.5.0 installed
- [ ] Docker >= 20.10 installed
- [ ] Go >= 1.24 installed (for local testing)
- [ ] Existing VPC with:
  - [ ] Private subnets (at least 2 for HA)
  - [ ] NAT Gateway configured
  - [ ] Internet Gateway configured
- [ ] RDS PostgreSQL database (from senso-api) with:
  - [ ] Connection string ready
  - [ ] Security group configured
  - [ ] Database migrations applied
- [ ] API keys ready:
  - [ ] OpenAI API key (or Azure OpenAI)
  - [ ] BrightData API key and dataset IDs
  - [ ] Anthropic API key (optional)

## Phase 1: Infrastructure Preparation (Day 1)

### 1.1 Create ECR Repository

```bash
# Set variables
export AWS_REGION="us-east-1"
export AWS_ACCOUNT_ID=$(aws sts get-caller-identity --query Account --output text)
export ECR_REPO_NAME="senso-workflows"

# Create repository
aws ecr create-repository \
  --repository-name $ECR_REPO_NAME \
  --image-scanning-configuration scanOnPush=true \
  --region $AWS_REGION \
  --tags Key=Project,Value=senso-workflows Key=Environment,Value=production

# Enable lifecycle policy to clean up old images
cat > ecr-lifecycle-policy.json <<EOF
{
  "rules": [
    {
      "rulePriority": 1,
      "description": "Keep last 10 images",
      "selection": {
        "tagStatus": "any",
        "countType": "imageCountMoreThan",
        "countNumber": 10
      },
      "action": {
        "type": "expire"
      }
    }
  ]
}
EOF

aws ecr put-lifecycle-policy \
  --repository-name $ECR_REPO_NAME \
  --lifecycle-policy-text file://ecr-lifecycle-policy.json \
  --region $AWS_REGION

echo "✅ ECR repository created: ${AWS_ACCOUNT_ID}.dkr.ecr.${AWS_REGION}.amazonaws.com/${ECR_REPO_NAME}"
```

### 1.2 Verify VPC Configuration

```bash
# List VPCs
aws ec2 describe-vpcs --query 'Vpcs[*].[VpcId,Tags[?Key==`Name`].Value|[0],CidrBlock]' --output table

# Export your VPC ID
export VPC_ID="vpc-xxxxxxxxxxxxxxxxx"

# Verify private subnets
aws ec2 describe-subnets \
  --filters "Name=vpc-id,Values=$VPC_ID" "Name=tag:Tier,Values=private" \
  --query 'Subnets[*].[SubnetId,AvailabilityZone,CidrBlock,Tags[?Key==`Name`].Value|[0]]' \
  --output table

# Verify NAT Gateway exists
aws ec2 describe-nat-gateways \
  --filter "Name=vpc-id,Values=$VPC_ID" "Name=state,Values=available" \
  --query 'NatGateways[*].[NatGatewayId,SubnetId,State]' \
  --output table

# If NAT Gateway missing, create one:
# (This is expensive - $32/month per NAT Gateway)
# PUBLIC_SUBNET_ID="subnet-xxxxxxxxx"
# aws ec2 allocate-address --domain vpc
# EIP_ALLOC_ID="eipalloc-xxxxxxxxx"
# aws ec2 create-nat-gateway --subnet-id $PUBLIC_SUBNET_ID --allocation-id $EIP_ALLOC_ID
```

### 1.3 Verify RDS Access

```bash
# Test database connection from your machine
# (Must be on VPN or bastion host if RDS is private)

export DATABASE_URL="postgres://username:password@your-rds-endpoint.us-east-1.rds.amazonaws.com:5432/senso2?sslmode=require"

# Test connection using psql
psql $DATABASE_URL -c "SELECT version();"

# Test connection using Go (from new/ directory)
go run -tags test ./cmd/test-db-connection/main.go

# If connection fails:
# 1. Check security group allows your IP
# 2. Check RDS is in correct VPC
# 3. Check subnet routing to NAT Gateway
```

## Phase 2: Build and Push Docker Image (Day 1)

### 2.1 Local Build and Test

```bash
# Navigate to new/ directory
cd new

# Build Docker image locally
docker build -t senso-workflows:latest .

# Verify image
docker images | grep senso-workflows

# Test container locally (dry-run)
docker run --rm \
  -e STEP_NAME=get-or-create-batch \
  -e DATABASE_URL=$DATABASE_URL \
  -e OPENAI_API_KEY=$OPENAI_API_KEY \
  senso-workflows:latest \
  echo '{"org_id":"test","triggered_by":"local_test"}'

# Expected: Container starts successfully, logs configuration
```

### 2.2 Push to ECR

```bash
# Set ECR variables
export ECR_URI="${AWS_ACCOUNT_ID}.dkr.ecr.${AWS_REGION}.amazonaws.com/${ECR_REPO_NAME}"

# Login to ECR
aws ecr get-login-password --region $AWS_REGION | \
  docker login --username AWS --password-stdin $ECR_URI

# Tag image
docker tag senso-workflows:latest ${ECR_URI}:latest
docker tag senso-workflows:latest ${ECR_URI}:$(git rev-parse --short HEAD)

# Push both tags
docker push ${ECR_URI}:latest
docker push ${ECR_URI}:$(git rev-parse --short HEAD)

# Verify push
aws ecr describe-images \
  --repository-name $ECR_REPO_NAME \
  --region $AWS_REGION \
  --query 'imageDetails[*].[imageTags[0],imageSizeInBytes,imagePushedAt]' \
  --output table

echo "✅ Docker image pushed: ${ECR_URI}:latest"
```

## Phase 3: Configure Terraform (Day 1)

### 3.1 Create terraform.tfvars

```bash
cd infrastructure/terraform

# Copy example
cp terraform.tfvars.example terraform.tfvars

# Edit with your values
vim terraform.tfvars
```

**Fill in these REQUIRED values:**

```hcl
# terraform.tfvars
aws_region  = "us-east-1"
environment = "production"

# VPC (from Phase 1)
vpc_id = "vpc-xxxxxxxxxxxxxxxxx"

# Database
database_url = "postgres://username:password@your-rds.us-east-1.rds.amazonaws.com:5432/senso2?sslmode=require"

# OpenAI
openai_api_key = "sk-proj-xxxxxxxxxxxxxxxxxxxxxxxxxxxxx"

# Azure OpenAI (if using Azure instead of standard OpenAI)
azure_openai_endpoint        = "https://your-resource.openai.azure.com/"
azure_openai_key             = "xxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
azure_openai_deployment_name = "gpt-4-1"

# BrightData (REQUIRED)
brightdata_api_key    = "xxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
brightdata_dataset_id = "gd_xxxxxxxxxxxxxxxxxxxxx"
perplexity_dataset_id = "gd_xxxxxxxxxxxxxxxxxxxxx"

# Application
application_api_url = "https://api.senso.ai"
api_token           = "your-api-auth-token"

# Docker image (from Phase 2)
docker_image_uri = "123456789012.dkr.ecr.us-east-1.amazonaws.com/senso-workflows:latest"

# Scaling
task_cpu                   = 1024  # Start conservative
task_memory                = 2048
max_concurrent_executions  = 100

# Scheduling (disable initially for testing)
enable_eventbridge_trigger = false
```

### 3.2 Initialize Terraform

```bash
# Initialize (downloads providers)
terraform init

# Validate configuration
terraform validate

# Format files
terraform fmt

# Review plan (shows what will be created)
terraform plan -out=tfplan

# Expected output: ~30-40 resources to create
# - ECS cluster
# - Task definition
# - Step Functions state machine
# - IAM roles (4-5 roles)
# - Security groups
# - CloudWatch log groups
# - Secrets Manager secret
# - KMS key
```

## Phase 4: Deploy Infrastructure (Day 1-2)

### 4.1 Apply Terraform

```bash
# Deploy everything
terraform apply tfplan

# This takes 5-10 minutes
# Watch for errors, especially:
# - VPC/subnet configuration
# - IAM permission issues
# - Secrets Manager conflicts

# If successful, you'll see:
# Apply complete! Resources: 35 added, 0 changed, 0 destroyed.
```

### 4.2 Verify Deployment

```bash
# Get outputs
terraform output

# Save important ARNs
export CLUSTER_ARN=$(terraform output -raw ecs_cluster_arn)
export TASK_DEF_ARN=$(terraform output -raw task_definition_arn)
export STATE_MACHINE_ARN=$(terraform output -raw step_function_arn)
export SECRETS_ARN=$(terraform output -raw secrets_arn)

echo "Cluster: $CLUSTER_ARN"
echo "Task Definition: $TASK_DEF_ARN"
echo "State Machine: $STATE_MACHINE_ARN"

# Verify state machine exists
aws stepfunctions describe-state-machine \
  --state-machine-arn $STATE_MACHINE_ARN \
  --query '[name,status,creationDate]' \
  --output table

# Verify secrets
aws secretsmanager describe-secret \
  --secret-id $SECRETS_ARN \
  --query '[Name,ARN,LastChangedDate]' \
  --output table

# Verify ECS cluster
aws ecs describe-clusters \
  --clusters $CLUSTER_ARN \
  --query 'clusters[0].[clusterName,status,registeredContainerInstancesCount]' \
  --output table
```

## Phase 5: Test Execution (Day 2)

### 5.1 Prepare Test Org

```sql
-- In your PostgreSQL database, find a test org
-- with a small number of questions (5-10)

SELECT
  org_id,
  name,
  (SELECT COUNT(*) FROM geo_questions WHERE org_id = orgs.org_id) AS question_count
FROM orgs
WHERE is_active = true
ORDER BY created_at DESC
LIMIT 10;

-- Choose an org with 5-10 questions for first test
```

### 5.2 Trigger Test Execution

```bash
# Export test org ID
export TEST_ORG_ID="01234567-89ab-cdef-0123-456789abcdef"

# Trigger workflow
EXECUTION_ARN=$(aws stepfunctions start-execution \
  --state-machine-arn $STATE_MACHINE_ARN \
  --name "test-$(date +%s)" \
  --input "{\"org_id\":\"$TEST_ORG_ID\",\"triggered_by\":\"deployment_test\",\"user_id\":\"admin\"}" \
  --query 'executionArn' \
  --output text)

echo "Execution started: $EXECUTION_ARN"
echo "View in console: https://console.aws.amazon.com/states/home?region=us-east-1#/executions/details/$EXECUTION_ARN"
```

### 5.3 Monitor Execution

```bash
# Watch status
watch -n 5 "aws stepfunctions describe-execution \
  --execution-arn $EXECUTION_ARN \
  --query '[status,startDate,stopDate]' \
  --output table"

# View logs in real-time
aws logs tail /ecs/senso-workflows-production --follow --since 5m

# Check specific step output
aws stepfunctions get-execution-history \
  --execution-arn $EXECUTION_ARN \
  --query 'events[?type==`TaskStateExited`].[name,output]' \
  --output table
```

### 5.4 Validate Results

```sql
-- Check if batch was created
SELECT * FROM org_evaluation_batches
WHERE org_id = 'YOUR_TEST_ORG_ID'
ORDER BY created_at DESC
LIMIT 1;

-- Check question runs
SELECT COUNT(*) FROM question_runs
WHERE batch_id = 'BATCH_ID_FROM_ABOVE';

-- Check org evaluations
SELECT COUNT(*) FROM org_evals
WHERE question_run_id IN (
  SELECT question_run_id FROM question_runs
  WHERE batch_id = 'BATCH_ID_FROM_ABOVE'
);

-- Check citations and competitors
SELECT
  (SELECT COUNT(*) FROM org_citations WHERE question_run_id IN (...)) AS citations,
  (SELECT COUNT(*) FROM org_competitors WHERE question_run_id IN (...)) AS competitors;
```

### 5.5 Troubleshooting Failed Test

If the test execution fails:

**1. Check CloudWatch Logs**
```bash
# Get failure details
aws stepfunctions describe-execution \
  --execution-arn $EXECUTION_ARN \
  --query 'cause' \
  --output text

# Get last 100 log entries
aws logs tail /ecs/senso-workflows-production --since 30m | tail -100
```

**2. Check Specific Step Failure**
```bash
# Get execution history
aws stepfunctions get-execution-history \
  --execution-arn $EXECUTION_ARN \
  --max-items 100 \
  --query 'events[?type==`TaskFailed` || type==`ExecutionFailed`]' \
  --output json > execution_history.json

# Review execution_history.json for error details
```

**3. Common Issues**

| Error | Cause | Solution |
|-------|-------|----------|
| `CannotPullContainerError` | Image not in ECR | Re-push image (Phase 2.2) |
| `Database connection timeout` | Security group issue | Check RDS security group allows ECS SG |
| `Invalid org ID` | Wrong test org | Use valid org ID from database |
| `Partner balance check failed` | Insufficient balance | Add credits to partner account |
| `Task timed out` | Step took > 30 min | Increase timeout in state machine |

## Phase 6: Scale Testing (Day 2-3)

### 6.1 Test 10 Orgs Concurrently

```bash
# Create list of 10 test org IDs
cat > test_orgs.txt <<EOF
org_id_1
org_id_2
org_id_3
org_id_4
org_id_5
org_id_6
org_id_7
org_id_8
org_id_9
org_id_10
EOF

# Trigger all
while IFS= read -r ORG_ID; do
  aws stepfunctions start-execution \
    --state-machine-arn $STATE_MACHINE_ARN \
    --name "scale-test-$(date +%s)-$RANDOM" \
    --input "{\"org_id\":\"$ORG_ID\",\"triggered_by\":\"scale_test\"}" \
    --output text
  echo "Triggered: $ORG_ID"
done < test_orgs.txt

# Monitor concurrent executions
watch -n 5 "aws stepfunctions list-executions \
  --state-machine-arn $STATE_MACHINE_ARN \
  --status-filter RUNNING \
  --max-items 20 \
  --query 'length(executions)'"
```

### 6.2 Monitor Resource Utilization

```bash
# Check ECS task count
aws ecs list-tasks --cluster $CLUSTER_ARN | jq '.taskArns | length'

# Check CPU/Memory utilization
aws cloudwatch get-metric-statistics \
  --namespace AWS/ECS \
  --metric-name CPUUtilization \
  --dimensions Name=ClusterName,Value=senso-workflows-production-cluster \
  --start-time $(date -u -d '1 hour ago' +%Y-%m-%dT%H:%M:%S) \
  --end-time $(date -u +%Y-%m-%dT%H:%M:%S) \
  --period 300 \
  --statistics Average \
  --query 'Datapoints[*].[Timestamp,Average]' \
  --output table
```

### 6.3 Performance Benchmarks

Expected performance:
- **Step 1 (Get Batch):** 5-10 seconds
- **Step 2 (Check Balance):** 2-5 seconds
- **Step 3 (Start Batch):** 2-5 seconds
- **Step 4 (Run Questions):** 5-15 minutes (depends on question count)
- **Step 5 (Track Usage):** 5-10 seconds
- **Step 6 (Complete Batch):** 2-5 seconds

**Total:** 6-17 minutes per org

If Step 4 takes > 20 minutes, consider:
1. Increasing task CPU (1024 → 2048)
2. Increasing task memory (2048 → 4096)
3. Checking BrightData API latency

## Phase 7: Production Rollout (Day 3-7)

### 7.1 Gradual Traffic Shift

**Day 3: 10% Traffic**
```bash
# Manually trigger 150 orgs (10% of 1,500)
# Compare results with Inngest runs

# Query for discrepancies
psql $DATABASE_URL <<EOF
SELECT
  sf.org_id,
  sf.total_processed AS stepfunctions_processed,
  i.total_processed AS inngest_processed,
  ABS(sf.total_cost - i.total_cost) AS cost_diff
FROM stepfunctions_batches sf
LEFT JOIN inngest_batches i ON sf.org_id = i.org_id
  AND DATE(sf.created_at) = DATE(i.created_at)
WHERE sf.created_at > NOW() - INTERVAL '1 day'
  AND (sf.total_processed != i.total_processed OR ABS(sf.total_cost - i.total_cost) > 0.01);
EOF
```

**Day 4-5: 50% Traffic**
- Increase to 750 orgs/day
- Monitor error rates
- Compare costs

**Day 6-7: 100% Traffic**
- All 1,500 orgs through Step Functions
- Disable Inngest triggers
- Monitor for 48 hours

### 7.2 Enable Scheduled Runs

After successful 100% rollout:

```hcl
# infrastructure/terraform/terraform.tfvars
enable_eventbridge_trigger = true
eventbridge_schedule       = "cron(0 2 * * ? *)"  # 2 AM UTC
```

```bash
terraform apply

# Verify EventBridge rule
aws events list-rules --name-prefix senso-workflows --output table

# Test scheduled trigger (dry-run)
aws events put-events --entries '[{
  "Source": "aws.events",
  "DetailType": "Scheduled Event",
  "Detail": "{\"org_id\":\"test\"}"
}]'
```

## Phase 8: Post-Deployment (Ongoing)

### 8.1 Set Up Monitoring

**CloudWatch Dashboard**
```bash
# Create dashboard (using AWS CLI or Console)
aws cloudwatch put-dashboard \
  --dashboard-name senso-workflows-production \
  --dashboard-body file://cloudwatch-dashboard.json
```

**Alarms Already Configured:**
- ✅ ExecutionsFailed > 5 in 5 min
- ✅ ExecutionThrottled > 10 in 5 min
- ✅ EventBridge DLQ messages > 0

**Add SNS Topic for Alerts (optional):**
```bash
# Create SNS topic
aws sns create-topic --name senso-workflows-alerts

# Subscribe your email
aws sns subscribe \
  --topic-arn arn:aws:sns:us-east-1:123456789:senso-workflows-alerts \
  --protocol email \
  --notification-endpoint your-email@company.com

# Update alarms to use SNS
# (Edit ecs.tf, step-functions.tf, uncomment alarm_actions)
terraform apply
```

### 8.2 Set Up Backup and DR

**1. Enable Point-in-Time Recovery (RDS)**
```bash
aws rds modify-db-instance \
  --db-instance-identifier senso-production \
  --backup-retention-period 30 \
  --preferred-backup-window "03:00-04:00" \
  --enable-iam-database-authentication
```

**2. Cross-Region Replication (ECR)**
```bash
# Create replication rule
aws ecr put-replication-configuration \
  --replication-configuration file://ecr-replication.json
```

**3. Terraform State Backup**
```bash
# Enable S3 backend (recommended)
# Edit infrastructure/terraform/main.tf, uncomment backend "s3" block

terraform init -migrate-state
```

### 8.3 Regular Maintenance

**Weekly:**
- [ ] Review CloudWatch metrics
- [ ] Check ECS task failure logs
- [ ] Review Step Functions error rates
- [ ] Monitor costs in Cost Explorer

**Monthly:**
- [ ] Rotate secrets in Secrets Manager
- [ ] Update Docker base images
- [ ] Review and optimize ECS task sizing
- [ ] Clean up old ECR images (lifecycle policy handles this)

**Quarterly:**
- [ ] Review IAM permissions (least privilege)
- [ ] Update dependencies (go mod, Terraform providers)
- [ ] Load testing with increased concurrency
- [ ] Review and update documentation

## Cost Monitoring

```bash
# Get last 30 days costs
aws ce get-cost-and-usage \
  --time-period Start=$(date -d '30 days ago' +%Y-%m-%d),End=$(date +%Y-%m-%d) \
  --granularity DAILY \
  --metrics BlendedCost \
  --filter '{
    "And": [{
      "Tags": {
        "Key": "Project",
        "Values": ["senso-workflows"]
      }
    }]
  }' \
  --query 'ResultsByTime[*].[TimePeriod.Start,Total.BlendedCost.Amount]' \
  --output table

# Expected: $6-10/day for infrastructure
```

## Rollback Plan

If critical issues arise:

**Immediate Rollback to Inngest:**
```bash
# 1. Stop all Step Functions executions
aws stepfunctions list-executions \
  --state-machine-arn $STATE_MACHINE_ARN \
  --status-filter RUNNING \
  --query 'executions[*].executionArn' \
  --output text | xargs -I {} aws stepfunctions stop-execution --execution-arn {}

# 2. Disable EventBridge rule
aws events disable-rule --name senso-workflows-production-nightly-org-evaluation

# 3. Re-enable Inngest triggers
# (Use old trigger scripts)

# 4. Monitor for 24 hours
```

**Destroy Infrastructure (if needed):**
```bash
cd infrastructure/terraform
terraform destroy

# Confirm: yes
# This will delete all resources except:
# - ECR images (delete manually if needed)
# - S3 terraform state (if using remote backend)
```

## Success Criteria

Deployment is successful when:
- ✅ Single org test execution completes successfully
- ✅ 10 concurrent executions complete without errors
- ✅ Results match Inngest outputs (within 1% variance)
- ✅ Average execution time < 20 minutes
- ✅ No step timeouts or OOM errors
- ✅ CloudWatch metrics show healthy state
- ✅ Daily costs align with projections ($6-10/day)

## Next Steps

After successful deployment:
1. Implement network workflows (future)
2. Add Grafana dashboards
3. Implement custom metrics
4. Set up automated load testing
5. Configure multi-region failover

---

**Congratulations! Your Step Functions deployment is complete.** 🎉
