# infrastructure/terraform/ecs.tf
# ECS Cluster and Task Definition for Step Functions workers

# ECS Cluster
resource "aws_ecs_cluster" "workflows" {
  name = "${local.name_prefix}-cluster"

  setting {
    name  = "containerInsights"
    value = "enabled"
  }

  tags = local.common_tags
}

# ECS Cluster Capacity Providers
resource "aws_ecs_cluster_capacity_providers" "workflows" {
  cluster_name = aws_ecs_cluster.workflows.name

  capacity_providers = ["FARGATE", "FARGATE_SPOT"]

  default_capacity_provider_strategy {
    capacity_provider = "FARGATE"
    weight            = 1
    base              = 1
  }
}

# CloudWatch Log Group for ECS tasks
resource "aws_cloudwatch_log_group" "ecs_tasks" {
  name              = "/ecs/${local.name_prefix}"
  retention_in_days = 30

  tags = local.common_tags
}

# ECS Task Execution Role (pulls images, writes logs)
resource "aws_iam_role" "ecs_task_execution" {
  name = "${local.name_prefix}-ecs-execution-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Action = "sts:AssumeRole"
        Effect = "Allow"
        Principal = {
          Service = "ecs-tasks.amazonaws.com"
        }
      }
    ]
  })

  tags = local.common_tags
}

resource "aws_iam_role_policy_attachment" "ecs_task_execution" {
  role       = aws_iam_role.ecs_task_execution.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AmazonECSTaskExecutionRolePolicy"
}

# Additional policy for secrets access
resource "aws_iam_role_policy" "ecs_secrets_access" {
  name = "${local.name_prefix}-secrets-access"
  role = aws_iam_role.ecs_task_execution.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "secretsmanager:GetSecretValue",
          "kms:Decrypt"
        ]
        Resource = [
          aws_secretsmanager_secret.workflow_secrets.arn,
          aws_kms_key.secrets.arn
        ]
      }
    ]
  })
}

# ECS Task Role (permissions tasks need during execution)
resource "aws_iam_role" "ecs_task" {
  name = "${local.name_prefix}-ecs-task-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Action = "sts:AssumeRole"
        Effect = "Allow"
        Principal = {
          Service = "ecs-tasks.amazonaws.com"
        }
      }
    ]
  })

  tags = local.common_tags
}

# Task role policy for CloudWatch metrics and X-Ray
resource "aws_iam_role_policy" "ecs_task_permissions" {
  name = "${local.name_prefix}-task-permissions"
  role = aws_iam_role.ecs_task.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "cloudwatch:PutMetricData",
          "xray:PutTraceSegments",
          "xray:PutTelemetryRecords"
        ]
        Resource = "*"
      }
    ]
  })
}

# Security Group for ECS tasks
resource "aws_security_group" "ecs_tasks" {
  name        = "${local.name_prefix}-ecs-tasks"
  description = "Security group for Senso Workflows ECS tasks"
  vpc_id      = var.vpc_id

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
    description = "Allow all outbound traffic for API calls"
  }

  tags = merge(
    local.common_tags,
    {
      Name = "${local.name_prefix}-ecs-tasks"
    }
  )
}

# Allow inbound from RDS security group (if needed)
# Uncomment if you're using RDS with a separate security group
# resource "aws_security_group_rule" "ecs_to_rds" {
#   type                     = "ingress"
#   from_port                = 5432
#   to_port                  = 5432
#   protocol                 = "tcp"
#   source_security_group_id = aws_security_group.ecs_tasks.id
#   security_group_id        = var.rds_security_group_id
#   description              = "Allow ECS tasks to connect to RDS"
# }

# ECS Task Definition
resource "aws_ecs_task_definition" "org_evaluation" {
  family                   = "${local.name_prefix}-org-evaluation"
  network_mode             = "awsvpc"
  requires_compatibilities = ["FARGATE"]
  cpu                      = var.task_cpu
  memory                   = var.task_memory
  execution_role_arn       = aws_iam_role.ecs_task_execution.arn
  task_role_arn            = aws_iam_role.ecs_task.arn

  container_definitions = jsonencode([
    {
      name      = "org-evaluation-worker"
      image     = var.docker_image_uri
      essential = true

      logConfiguration = {
        logDriver = "awslogs"
        options = {
          "awslogs-group"         = aws_cloudwatch_log_group.ecs_tasks.name
          "awslogs-region"        = var.aws_region
          "awslogs-stream-prefix" = "org-evaluation"
        }
      }

      secrets = [
        {
          name      = "DATABASE_URL"
          valueFrom = "${aws_secretsmanager_secret.workflow_secrets.arn}:DATABASE_URL::"
        },
        {
          name      = "OPENAI_API_KEY"
          valueFrom = "${aws_secretsmanager_secret.workflow_secrets.arn}:OPENAI_API_KEY::"
        },
        {
          name      = "AZURE_OPENAI_KEY"
          valueFrom = "${aws_secretsmanager_secret.workflow_secrets.arn}:AZURE_OPENAI_KEY::"
        },
        {
          name      = "ANTHROPIC_API_KEY"
          valueFrom = "${aws_secretsmanager_secret.workflow_secrets.arn}:ANTHROPIC_API_KEY::"
        },
        {
          name      = "BRIGHTDATA_API_KEY"
          valueFrom = "${aws_secretsmanager_secret.workflow_secrets.arn}:BRIGHTDATA_API_KEY::"
        },
        {
          name      = "LINKUP_API_KEY"
          valueFrom = "${aws_secretsmanager_secret.workflow_secrets.arn}:LINKUP_API_KEY::"
        },
        {
          name      = "API_TOKEN"
          valueFrom = "${aws_secretsmanager_secret.workflow_secrets.arn}:API_TOKEN::"
        }
      ]

      environment = [
        {
          name  = "ENVIRONMENT"
          value = var.environment
        },
        {
          name  = "AWS_REGION"
          value = var.aws_region
        },
        {
          name  = "AZURE_OPENAI_ENDPOINT"
          value = var.azure_openai_endpoint
        },
        {
          name  = "AZURE_OPENAI_DEPLOYMENT_NAME"
          value = var.azure_openai_deployment_name
        },
        {
          name  = "BRIGHTDATA_DATASET_ID"
          value = var.brightdata_dataset_id
        },
        {
          name  = "PERPLEXITY_DATASET_ID"
          value = var.perplexity_dataset_id
        },
        {
          name  = "GEMINI_DATASET_ID"
          value = var.gemini_dataset_id
        },
        {
          name  = "APPLICATION_API_URL"
          value = var.application_api_url
        }
      ]

      healthCheck = {
        command = [
          "CMD-SHELL",
          "echo '{\"status\":\"healthy\"}' || exit 1"
        ]
        interval    = 30
        timeout     = 5
        retries     = 3
        startPeriod = 60
      }
    }
  ])

  tags = local.common_tags
}

# Output
output "ecs_cluster_arn" {
  description = "ARN of the ECS cluster"
  value       = aws_ecs_cluster.workflows.arn
}

output "task_definition_arn" {
  description = "ARN of the ECS task definition"
  value       = aws_ecs_task_definition.org_evaluation.arn
}

output "security_group_id" {
  description = "Security group ID for ECS tasks"
  value       = aws_security_group.ecs_tasks.id
}
