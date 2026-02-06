# infrastructure/terraform/eventbridge.tf
# EventBridge rules for triggering Step Functions workflows

# IAM role for EventBridge to invoke Step Functions
resource "aws_iam_role" "eventbridge_stepfunctions" {
  name = "${local.name_prefix}-eventbridge-stepfunctions-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Action = "sts:AssumeRole"
        Effect = "Allow"
        Principal = {
          Service = "events.amazonaws.com"
        }
      }
    ]
  })

  tags = local.common_tags
}

resource "aws_iam_role_policy" "eventbridge_stepfunctions" {
  name = "${local.name_prefix}-eventbridge-stepfunctions-policy"
  role = aws_iam_role.eventbridge_stepfunctions.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "states:StartExecution"
        ]
        Resource = aws_sfn_state_machine.org_evaluation.arn
      }
    ]
  })
}

# EventBridge rule for scheduled nightly runs
resource "aws_cloudwatch_event_rule" "nightly_org_evaluation" {
  count               = var.enable_eventbridge_trigger ? 1 : 0
  name                = "${local.name_prefix}-nightly-org-evaluation"
  description         = "Trigger nightly org evaluation workflows"
  schedule_expression = var.eventbridge_schedule

  tags = local.common_tags
}

# Dead Letter Queue for failed EventBridge invocations
resource "aws_sqs_queue" "eventbridge_dlq" {
  count                     = var.enable_eventbridge_trigger ? 1 : 0
  name                      = "${local.name_prefix}-eventbridge-dlq"
  message_retention_seconds = 1209600 # 14 days

  tags = local.common_tags
}

# EventBridge target - Step Functions with input transformer
resource "aws_cloudwatch_event_target" "org_evaluation" {
  count     = var.enable_eventbridge_trigger ? 1 : 0
  rule      = aws_cloudwatch_event_rule.nightly_org_evaluation[0].name
  target_id = "OrgEvaluationStepFunction"
  arn       = aws_sfn_state_machine.org_evaluation.arn
  role_arn  = aws_iam_role.eventbridge_stepfunctions.arn

  input_transformer {
    input_paths = {
      time = "$.time"
    }
    input_template = <<EOF
{
  "org_id": "<org_id>",
  "triggered_by": "eventbridge_schedule",
  "scheduled_time": <time>
}
EOF
  }

  dead_letter_config {
    arn = aws_sqs_queue.eventbridge_dlq[0].arn
  }

  retry_policy {
    maximum_event_age       = 3600
    maximum_retry_attempts  = 3
  }
}

# CloudWatch alarm for DLQ messages
resource "aws_cloudwatch_metric_alarm" "eventbridge_dlq_messages" {
  count               = var.enable_eventbridge_trigger ? 1 : 0
  alarm_name          = "${local.name_prefix}-eventbridge-dlq-messages"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 1
  metric_name         = "ApproximateNumberOfMessagesVisible"
  namespace           = "AWS/SQS"
  period              = 300
  statistic           = "Average"
  threshold           = 0
  alarm_description   = "Alert when messages appear in EventBridge DLQ"
  treat_missing_data  = "notBreaching"

  dimensions = {
    QueueName = aws_sqs_queue.eventbridge_dlq[0].name
  }

  # Uncomment to enable SNS notifications
  # alarm_actions = [aws_sns_topic.alerts.arn]

  tags = local.common_tags
}

# Lambda function for batch triggering (alternative to EventBridge for complex logic)
# This Lambda can query the database and trigger workflows for multiple orgs
resource "aws_lambda_function" "batch_trigger" {
  filename         = "${path.module}/../../lambda/batch-trigger.zip"
  function_name    = "${local.name_prefix}-batch-trigger"
  role             = aws_iam_role.lambda_batch_trigger.arn
  handler          = "main"
  source_code_hash = filebase64sha256("${path.module}/../../lambda/batch-trigger.zip")
  runtime          = "provided.al2"
  timeout          = 300
  memory_size      = 512

  environment {
    variables = {
      STATE_MACHINE_ARN = aws_sfn_state_machine.org_evaluation.arn
      DATABASE_URL      = var.database_url
      ENVIRONMENT       = var.environment
    }
  }

  vpc_config {
    subnet_ids         = data.aws_subnets.private.ids
    security_group_ids = [aws_security_group.lambda.id]
  }

  tags = local.common_tags

  # Only create if Lambda zip exists
  count = fileexists("${path.module}/../../lambda/batch-trigger.zip") ? 1 : 0
}

# IAM role for Lambda batch trigger
resource "aws_iam_role" "lambda_batch_trigger" {
  name = "${local.name_prefix}-lambda-batch-trigger-role"

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

resource "aws_iam_role_policy" "lambda_batch_trigger" {
  name = "${local.name_prefix}-lambda-batch-trigger-policy"
  role = aws_iam_role.lambda_batch_trigger.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "states:StartExecution"
        ]
        Resource = aws_sfn_state_machine.org_evaluation.arn
      },
      {
        Effect = "Allow"
        Action = [
          "logs:CreateLogGroup",
          "logs:CreateLogStream",
          "logs:PutLogEvents"
        ]
        Resource = "arn:aws:logs:${var.aws_region}:${local.account_id}:log-group:/aws/lambda/${local.name_prefix}-batch-trigger:*"
      },
      {
        Effect = "Allow"
        Action = [
          "ec2:CreateNetworkInterface",
          "ec2:DescribeNetworkInterfaces",
          "ec2:DeleteNetworkInterface"
        ]
        Resource = "*"
      }
    ]
  })
}

# Security group for Lambda
resource "aws_security_group" "lambda" {
  name        = "${local.name_prefix}-lambda"
  description = "Security group for Lambda batch trigger"
  vpc_id      = var.vpc_id

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = merge(
    local.common_tags,
    {
      Name = "${local.name_prefix}-lambda"
    }
  )
}

# CloudWatch Log Group for Lambda
resource "aws_cloudwatch_log_group" "lambda_batch_trigger" {
  name              = "/aws/lambda/${local.name_prefix}-batch-trigger"
  retention_in_days = 14

  tags = local.common_tags
}

# Output
output "eventbridge_rule_arn" {
  description = "ARN of the EventBridge rule"
  value       = try(aws_cloudwatch_event_rule.nightly_org_evaluation[0].arn, "")
}
