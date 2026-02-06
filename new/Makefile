# Makefile for Senso Workflows Step Functions
# Common commands for development and deployment

.PHONY: help build push deploy test clean logs

# Variables
APP_NAME = senso-workflows
AWS_REGION ?= us-east-1
AWS_ACCOUNT_ID ?= $(shell aws sts get-caller-identity --query Account --output text)
ECR_REPO = $(AWS_ACCOUNT_ID).dkr.ecr.$(AWS_REGION).amazonaws.com/$(APP_NAME)
GIT_SHA = $(shell git rev-parse --short HEAD)
IMAGE_TAG ?= latest

help: ## Show this help message
	@echo "Senso Workflows - Step Functions Deployment"
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-20s %s\n", $$1, $$2}'

deps: ## Install Go dependencies
	@echo "📦 Installing Go dependencies..."
	go mod download
	go mod verify

build: ## Build Docker image locally
	@echo "🔨 Building Docker image..."
	docker build -t $(APP_NAME):$(IMAGE_TAG) .
	docker tag $(APP_NAME):$(IMAGE_TAG) $(APP_NAME):$(GIT_SHA)
	@echo "✅ Image built: $(APP_NAME):$(IMAGE_TAG)"

test-local: build ## Test container locally
	@echo "🧪 Testing container locally..."
	docker run --rm \
		-e STEP_NAME=get-or-create-batch \
		-e DATABASE_URL=$${DATABASE_URL} \
		-e OPENAI_API_KEY=$${OPENAI_API_KEY} \
		$(APP_NAME):$(IMAGE_TAG) \
		echo '{"org_id":"test","triggered_by":"local_test"}'

ecr-login: ## Login to AWS ECR
	@echo "🔐 Logging in to ECR..."
	aws ecr get-login-password --region $(AWS_REGION) | \
		docker login --username AWS --password-stdin $(ECR_REPO)

ecr-create: ## Create ECR repository (one-time setup)
	@echo "📦 Creating ECR repository..."
	aws ecr create-repository \
		--repository-name $(APP_NAME) \
		--image-scanning-configuration scanOnPush=true \
		--region $(AWS_REGION) \
		--tags Key=Project,Value=$(APP_NAME) Key=ManagedBy,Value=terraform || true
	@echo "✅ ECR repository ready"

push: ecr-login build ## Push Docker image to ECR
	@echo "📤 Pushing image to ECR..."
	docker tag $(APP_NAME):$(IMAGE_TAG) $(ECR_REPO):$(IMAGE_TAG)
	docker tag $(APP_NAME):$(IMAGE_TAG) $(ECR_REPO):$(GIT_SHA)
	docker push $(ECR_REPO):$(IMAGE_TAG)
	docker push $(ECR_REPO):$(GIT_SHA)
	@echo "✅ Images pushed:"
	@echo "   $(ECR_REPO):$(IMAGE_TAG)"
	@echo "   $(ECR_REPO):$(GIT_SHA)"

tf-init: ## Initialize Terraform
	@echo "🏗️  Initializing Terraform..."
	cd infrastructure/terraform && terraform init

tf-plan: ## Plan Terraform changes
	@echo "📋 Planning Terraform changes..."
	cd infrastructure/terraform && terraform plan -out=tfplan

tf-apply: ## Apply Terraform changes
	@echo "🚀 Applying Terraform changes..."
	cd infrastructure/terraform && terraform apply tfplan
	@echo "✅ Infrastructure deployed!"

tf-destroy: ## Destroy Terraform infrastructure (DANGEROUS)
	@echo "⚠️  WARNING: This will destroy all infrastructure!"
	@echo "Press Ctrl+C to cancel, or wait 10 seconds to continue..."
	@sleep 10
	cd infrastructure/terraform && terraform destroy

deploy: push tf-apply ## Build, push, and deploy (full deployment)
	@echo "🎉 Deployment complete!"

trigger-test: ## Trigger a test workflow execution
	@echo "🚀 Triggering test workflow..."
	@if [ -z "$(ORG_ID)" ]; then \
		echo "Error: ORG_ID is required. Usage: make trigger-test ORG_ID=your-org-id"; \
		exit 1; \
	fi
	@STATE_MACHINE_ARN=$$(cd infrastructure/terraform && terraform output -raw step_function_arn 2>/dev/null); \
	if [ -z "$$STATE_MACHINE_ARN" ]; then \
		echo "Error: State machine ARN not found. Deploy infrastructure first."; \
		exit 1; \
	fi; \
	EXECUTION_ARN=$$(aws stepfunctions start-execution \
		--state-machine-arn $$STATE_MACHINE_ARN \
		--name "test-$$(date +%s)" \
		--input "{\"org_id\":\"$(ORG_ID)\",\"triggered_by\":\"makefile\",\"user_id\":\"admin\"}" \
		--query 'executionArn' \
		--output text); \
	echo "✅ Execution started: $$EXECUTION_ARN"; \
	echo "View in console: https://console.aws.amazon.com/states/home?region=$(AWS_REGION)#/executions/details/$$EXECUTION_ARN"

trigger-batch: ## Trigger batch workflow executions from file
	@echo "🚀 Triggering batch workflows..."
	@if [ ! -f "org_ids.txt" ]; then \
		echo "Error: org_ids.txt not found. Create a file with one org_id per line."; \
		exit 1; \
	fi
	@STATE_MACHINE_ARN=$$(cd infrastructure/terraform && terraform output -raw step_function_arn 2>/dev/null); \
	if [ -z "$$STATE_MACHINE_ARN" ]; then \
		echo "Error: State machine ARN not found. Deploy infrastructure first."; \
		exit 1; \
	fi; \
	while IFS= read -r ORG_ID; do \
		aws stepfunctions start-execution \
			--state-machine-arn $$STATE_MACHINE_ARN \
			--name "batch-$$(date +%s)-$$RANDOM" \
			--input "{\"org_id\":\"$$ORG_ID\",\"triggered_by\":\"batch_makefile\"}" \
			--output text; \
		echo "Triggered: $$ORG_ID"; \
	done < org_ids.txt

logs: ## Tail CloudWatch logs
	@echo "📜 Tailing ECS logs (Ctrl+C to exit)..."
	@LOG_GROUP=$$(cd infrastructure/terraform && terraform output -json | jq -r '.deployment_summary.value.log_group_ecs' 2>/dev/null); \
	if [ -z "$$LOG_GROUP" ]; then \
		LOG_GROUP="/ecs/senso-workflows-production"; \
	fi; \
	aws logs tail $$LOG_GROUP --follow --since 5m

logs-sf: ## Tail Step Functions logs
	@echo "📜 Tailing Step Functions logs (Ctrl+C to exit)..."
	@LOG_GROUP=$$(cd infrastructure/terraform && terraform output -json | jq -r '.deployment_summary.value.log_group_stepfunctions' 2>/dev/null); \
	if [ -z "$$LOG_GROUP" ]; then \
		LOG_GROUP="/aws/stepfunctions/senso-workflows-production"; \
	fi; \
	aws logs tail $$LOG_GROUP --follow --since 5m

status: ## Show deployment status
	@echo "📊 Deployment Status"
	@echo "===================="
	@cd infrastructure/terraform && \
	STATE_MACHINE_ARN=$$(terraform output -raw step_function_arn 2>/dev/null); \
	CLUSTER_ARN=$$(terraform output -raw ecs_cluster_arn 2>/dev/null); \
	if [ -n "$$STATE_MACHINE_ARN" ]; then \
		echo "State Machine: $$STATE_MACHINE_ARN"; \
		RUNNING=$$(aws stepfunctions list-executions \
			--state-machine-arn $$STATE_MACHINE_ARN \
			--status-filter RUNNING \
			--query 'length(executions)' \
			--output text); \
		echo "Running executions: $$RUNNING"; \
	fi; \
	if [ -n "$$CLUSTER_ARN" ]; then \
		echo "ECS Cluster: $$CLUSTER_ARN"; \
		TASKS=$$(aws ecs list-tasks --cluster $$CLUSTER_ARN --query 'length(taskArns)' --output text); \
		echo "Running tasks: $$TASKS"; \
	fi

validate: ## Validate all configuration files
	@echo "✅ Validating configuration..."
	@echo "Checking Go code..."
	go vet ./...
	@echo "Checking Terraform..."
	cd infrastructure/terraform && terraform fmt -check -recursive
	cd infrastructure/terraform && terraform validate
	@echo "Checking JSON..."
	jq empty infrastructure/state-machines/org-evaluation-workflow.json
	@echo "✅ All validations passed!"

format: ## Format all code files
	@echo "🎨 Formatting code..."
	go fmt ./...
	cd infrastructure/terraform && terraform fmt -recursive
	@echo "✅ Formatting complete!"

clean: ## Clean build artifacts
	@echo "🧹 Cleaning build artifacts..."
	docker rmi $(APP_NAME):$(IMAGE_TAG) 2>/dev/null || true
	docker rmi $(APP_NAME):$(GIT_SHA) 2>/dev/null || true
	docker rmi $(ECR_REPO):$(IMAGE_TAG) 2>/dev/null || true
	docker rmi $(ECR_REPO):$(GIT_SHA) 2>/dev/null || true
	rm -rf infrastructure/terraform/.terraform
	rm -f infrastructure/terraform/tfplan
	rm -f infrastructure/terraform/.terraform.lock.hcl
	@echo "✅ Cleanup complete!"

docs: ## Generate documentation
	@echo "📚 Documentation available at:"
	@echo "  README.md               - Overview and quick start"
	@echo "  DEPLOYMENT_GUIDE.md     - Complete deployment guide"
	@echo "  ../ARCHITECTURE_REVIEW.md - Architecture analysis"

cost-estimate: ## Estimate monthly costs
	@echo "💰 Monthly Cost Estimate (1,500 orgs/day)"
	@echo "=========================================="
	@echo "Step Functions:     $$6.75"
	@echo "ECS Fargate:        $$60"
	@echo "RDS (db.t3.medium): $$100"
	@echo "CloudWatch Logs:    $$25"
	@echo "Secrets Manager:    $$0.40"
	@echo "Data Transfer:      $$0.90"
	@echo "=========================================="
	@echo "Total Infrastructure: ~$$193/month"
	@echo ""
	@echo "LLM Costs (not infrastructure):"
	@echo "OpenAI:           ~$$25,000"
	@echo "BrightData:       ~$$60,000"
	@echo "=========================================="
	@echo "Grand Total:      ~$$85,193/month"

version: ## Show version information
	@echo "Senso Workflows Step Functions"
	@echo "=============================="
	@echo "Version:        1.0.0"
	@echo "Git SHA:        $(GIT_SHA)"
	@echo "Go Version:     $$(go version | awk '{print $$3}')"
	@echo "Docker:         $$(docker --version | awk '{print $$3}')"
	@echo "Terraform:      $$(cd infrastructure/terraform && terraform version -json 2>/dev/null | jq -r '.terraform_version')"
	@echo "AWS Region:     $(AWS_REGION)"
	@echo "AWS Account:    $(AWS_ACCOUNT_ID)"

.DEFAULT_GOAL := help
