# infrastructure/terraform/variables.tf

variable "aws_region" {
  description = "AWS region for deployment"
  type        = string
  default     = "us-east-1"
}

variable "environment" {
  description = "Environment name (dev, staging, production)"
  type        = string
  default     = "production"
}

variable "vpc_id" {
  description = "VPC ID where resources will be deployed"
  type        = string
}

variable "database_url" {
  description = "PostgreSQL database connection URL"
  type        = string
  sensitive   = true
}

variable "openai_api_key" {
  description = "OpenAI API key"
  type        = string
  sensitive   = true
}

variable "azure_openai_endpoint" {
  description = "Azure OpenAI endpoint URL"
  type        = string
  default     = ""
}

variable "azure_openai_key" {
  description = "Azure OpenAI API key"
  type        = string
  sensitive   = true
  default     = ""
}

variable "azure_openai_deployment_name" {
  description = "Azure OpenAI deployment name"
  type        = string
  default     = ""
}

variable "anthropic_api_key" {
  description = "Anthropic API key"
  type        = string
  sensitive   = true
  default     = ""
}

variable "brightdata_api_key" {
  description = "BrightData API key"
  type        = string
  sensitive   = true
}

variable "brightdata_dataset_id" {
  description = "BrightData dataset ID"
  type        = string
}

variable "perplexity_dataset_id" {
  description = "Perplexity dataset ID via BrightData"
  type        = string
}

variable "gemini_dataset_id" {
  description = "Gemini dataset ID via BrightData"
  type        = string
  default     = ""
}

variable "linkup_api_key" {
  description = "Linkup API key"
  type        = string
  sensitive   = true
  default     = ""
}

variable "application_api_url" {
  description = "Senso application API URL"
  type        = string
}

variable "api_token" {
  description = "API authentication token"
  type        = string
  sensitive   = true
}

variable "docker_image_uri" {
  description = "Docker image URI for ECS tasks (ECR repository URI)"
  type        = string
}

variable "task_cpu" {
  description = "CPU units for ECS task (1024 = 1 vCPU)"
  type        = number
  default     = 1024
}

variable "task_memory" {
  description = "Memory for ECS task in MB"
  type        = number
  default     = 2048
}

variable "max_concurrent_executions" {
  description = "Maximum concurrent Step Functions executions"
  type        = number
  default     = 100
}

variable "enable_eventbridge_trigger" {
  description = "Enable EventBridge scheduled trigger for nightly runs"
  type        = bool
  default     = false
}

variable "eventbridge_schedule" {
  description = "EventBridge cron schedule (UTC). Default: 2 AM UTC daily"
  type        = string
  default     = "cron(0 2 * * ? *)"
}
