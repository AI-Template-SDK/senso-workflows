# infrastructure/terraform/main.tf
# Terraform configuration for Senso Workflows Step Functions deployment

terraform {
  required_version = ">= 1.5.0"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }

  # Backend configuration for remote state
  # Uncomment and configure for production
  # backend "s3" {
  #   bucket         = "senso-terraform-state"
  #   key            = "workflows-stepfunctions/terraform.tfstate"
  #   region         = "us-east-1"
  #   encrypt        = true
  #   dynamodb_table = "terraform-state-lock"
  # }
}

provider "aws" {
  region = var.aws_region

  default_tags {
    tags = {
      Project     = "senso-workflows"
      Environment = var.environment
      ManagedBy   = "terraform"
    }
  }
}

# Data sources
data "aws_caller_identity" "current" {}

data "aws_vpc" "selected" {
  id = var.vpc_id
}

data "aws_subnets" "private" {
  filter {
    name   = "vpc-id"
    values = [var.vpc_id]
  }

  tags = {
    Tier = "private"
  }
}

# Locals
locals {
  account_id = data.aws_caller_identity.current.account_id
  name_prefix = "senso-workflows-${var.environment}"

  common_tags = {
    Project     = "senso-workflows"
    Environment = var.environment
    Component   = "step-functions"
  }
}
