# infrastructure/terraform/secrets.tf
# AWS Secrets Manager for sensitive configuration

# KMS key for encrypting secrets
resource "aws_kms_key" "secrets" {
  description             = "KMS key for Senso Workflows secrets encryption"
  deletion_window_in_days = 30
  enable_key_rotation     = true

  tags = merge(
    local.common_tags,
    {
      Name = "${local.name_prefix}-secrets-key"
    }
  )
}

resource "aws_kms_alias" "secrets" {
  name          = "alias/${local.name_prefix}-secrets"
  target_key_id = aws_kms_key.secrets.key_id
}

# Secrets Manager secret
resource "aws_secretsmanager_secret" "workflow_secrets" {
  name        = "${local.name_prefix}-secrets"
  description = "Sensitive configuration for Senso Workflows Step Functions"
  kms_key_id  = aws_kms_key.secrets.id

  tags = local.common_tags
}

# Secret version with all sensitive values
resource "aws_secretsmanager_secret_version" "workflow_secrets" {
  secret_id = aws_secretsmanager_secret.workflow_secrets.id

  secret_string = jsonencode({
    DATABASE_URL           = var.database_url
    OPENAI_API_KEY         = var.openai_api_key
    AZURE_OPENAI_KEY       = var.azure_openai_key
    ANTHROPIC_API_KEY      = var.anthropic_api_key
    BRIGHTDATA_API_KEY     = var.brightdata_api_key
    LINKUP_API_KEY         = var.linkup_api_key
    API_TOKEN              = var.api_token
  })

  lifecycle {
    ignore_changes = [secret_string]
  }
}

# IAM policy for secret rotation Lambda (optional, for future use)
resource "aws_iam_role" "secrets_rotation" {
  name = "${local.name_prefix}-secrets-rotation-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Action = "sts:AssumeRole"
        Effect = "Allow"
        Principal = {
          Service = "lambda.amazonaws.com"
        }
      }
    ]
  })

  tags = local.common_tags
}

resource "aws_iam_role_policy" "secrets_rotation" {
  name = "${local.name_prefix}-secrets-rotation-policy"
  role = aws_iam_role.secrets_rotation.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "secretsmanager:DescribeSecret",
          "secretsmanager:GetSecretValue",
          "secretsmanager:PutSecretValue",
          "secretsmanager:UpdateSecretVersionStage"
        ]
        Resource = aws_secretsmanager_secret.workflow_secrets.arn
      },
      {
        Effect = "Allow"
        Action = [
          "kms:Decrypt",
          "kms:DescribeKey",
          "kms:GenerateDataKey"
        ]
        Resource = aws_kms_key.secrets.arn
      }
    ]
  })
}

# Output
output "secrets_arn" {
  description = "ARN of the Secrets Manager secret"
  value       = aws_secretsmanager_secret.workflow_secrets.arn
  sensitive   = true
}
