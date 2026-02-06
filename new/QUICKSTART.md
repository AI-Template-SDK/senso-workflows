# Quick Start Checklist

Use this checklist for rapid deployment. For detailed explanations, see [DEPLOYMENT_GUIDE.md](DEPLOYMENT_GUIDE.md).

## Prerequisites Checklist

- [ ] AWS Account with admin access
- [ ] AWS CLI v2 configured (`aws configure`)
- [ ] Terraform >= 1.5.0 installed
- [ ] Docker >= 20.10 installed
- [ ] VPC with private subnets and NAT Gateway
- [ ] RDS PostgreSQL database connection string
- [ ] OpenAI API key
- [ ] BrightData API key and dataset IDs

## 5-Step Deployment

### Step 1: Setup (5 minutes)

```bash
# Navigate to new/ directory
cd new

# Copy and edit configuration
cp infrastructure/terraform/terraform.tfvars.example infrastructure/terraform/terraform.tfvars
vim infrastructure/terraform/terraform.tfvars

# Required values:
# - vpc_id
# - database_url
# - openai_api_key (or azure_openai_*)
# - brightdata_api_key
# - docker_image_uri (will get this in Step 2)
```

### Step 2: Build & Push Image (10 minutes)

```bash
# Create ECR repository
make ecr-create

# Build and push Docker image
make push

# Copy the image URI from output, add to terraform.tfvars:
# docker_image_uri = "123456789012.dkr.ecr.us-east-1.amazonaws.com/senso-workflows:latest"
```

### Step 3: Deploy Infrastructure (10 minutes)

```bash
# Initialize Terraform
make tf-init

# Review plan
make tf-plan

# Deploy (takes 5-10 minutes)
make tf-apply

# Save outputs
make status
```

### Step 4: Test Execution (15 minutes)

```bash
# Find a test org with 5-10 questions
# Then trigger test workflow:
make trigger-test ORG_ID=your-org-id-here

# Watch logs
make logs

# Check status in AWS Console (link in output)
```

### Step 5: Validate Results (5 minutes)

```sql
-- In PostgreSQL
SELECT * FROM org_evaluation_batches
WHERE org_id = 'YOUR_TEST_ORG_ID'
ORDER BY created_at DESC LIMIT 1;

-- Should see:
-- - status = 'completed'
-- - total_questions > 0
-- - completed_at IS NOT NULL
```

## Common Commands

```bash
# Show all available commands
make help

# Build Docker image
make build

# Push to ECR
make push

# Deploy everything
make deploy

# Trigger test workflow
make trigger-test ORG_ID=xxx

# Trigger batch workflows
make trigger-batch  # reads from org_ids.txt

# View logs
make logs      # ECS tasks
make logs-sf   # Step Functions

# Check status
make status

# Validate configuration
make validate

# Estimate costs
make cost-estimate
```

## Troubleshooting

### Image Push Fails

```bash
# Re-login to ECR
make ecr-login

# Retry push
make push
```

### Terraform Apply Fails

```bash
# Check VPC configuration
aws ec2 describe-vpcs --vpc-ids YOUR_VPC_ID

# Verify subnets
aws ec2 describe-subnets --filters "Name=vpc-id,Values=YOUR_VPC_ID"

# Check terraform errors
cd infrastructure/terraform && terraform plan
```

### Workflow Execution Fails

```bash
# Get detailed logs
make logs

# Check specific execution
aws stepfunctions describe-execution --execution-arn ARN

# Common issues:
# 1. Wrong org_id -> Use valid UUID from database
# 2. Balance check failed -> Add credits to partner account
# 3. Database timeout -> Check security groups
```

## Success Criteria

Deployment is successful when you see:

✅ Docker image in ECR
✅ Terraform apply completes without errors
✅ State machine shows "ACTIVE" status
✅ Test execution completes (status: SUCCEEDED)
✅ Batch record in database with status='completed'
✅ Question runs and evaluations created

## Next Steps

After successful test:

1. **Scale Test:** Trigger 10 orgs concurrently
2. **Monitor:** Review CloudWatch metrics for 24 hours
3. **Optimize:** Adjust task_cpu/memory if needed
4. **Schedule:** Enable EventBridge trigger
5. **Production:** Gradually shift traffic from Inngest

## Useful Links

- [Architecture Review](../ARCHITECTURE_REVIEW.md)
- [Deployment Guide](DEPLOYMENT_GUIDE.md)
- [README](README.md)
- [AWS Step Functions Console](https://console.aws.amazon.com/states/)
- [ECR Console](https://console.aws.amazon.com/ecr/)
- [CloudWatch Logs](https://console.aws.amazon.com/cloudwatch/home#logsV2:log-groups)

## Estimated Time

- First-time deployment: **45 minutes**
- Subsequent deploys: **15 minutes** (just image + tf apply)

## Cost

- Infrastructure: **~$193/month**
- LLM costs: **~$85,000/month** (OpenAI + BrightData)

---

**Need help?** Check [DEPLOYMENT_GUIDE.md](DEPLOYMENT_GUIDE.md) for detailed troubleshooting.
