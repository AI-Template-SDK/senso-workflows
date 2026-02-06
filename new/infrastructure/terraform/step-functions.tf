# infrastructure/terraform/step-functions.tf
# AWS Step Functions State Machine for Org Evaluation Workflow

# IAM role for Step Functions
resource "aws_iam_role" "step_functions" {
  name = "${local.name_prefix}-stepfunctions-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Action = "sts:AssumeRole"
        Effect = "Allow"
        Principal = {
          Service = "states.amazonaws.com"
        }
      }
    ]
  })

  tags = local.common_tags
}

# IAM policy for Step Functions to run ECS tasks
resource "aws_iam_role_policy" "step_functions_ecs" {
  name = "${local.name_prefix}-stepfunctions-ecs-policy"
  role = aws_iam_role.step_functions.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "ecs:RunTask",
          "ecs:StopTask",
          "ecs:DescribeTasks"
        ]
        Resource = [
          aws_ecs_task_definition.org_evaluation.arn,
          "arn:aws:ecs:${var.aws_region}:${local.account_id}:task/${aws_ecs_cluster.workflows.name}/*"
        ]
      },
      {
        Effect = "Allow"
        Action = [
          "iam:PassRole"
        ]
        Resource = [
          aws_iam_role.ecs_task_execution.arn,
          aws_iam_role.ecs_task.arn
        ]
      },
      {
        Effect = "Allow"
        Action = [
          "events:PutTargets",
          "events:PutRule",
          "events:DescribeRule"
        ]
        Resource = "*"
      },
      {
        Effect = "Allow"
        Action = [
          "logs:CreateLogDelivery",
          "logs:GetLogDelivery",
          "logs:UpdateLogDelivery",
          "logs:DeleteLogDelivery",
          "logs:ListLogDeliveries",
          "logs:PutResourcePolicy",
          "logs:DescribeResourcePolicies",
          "logs:DescribeLogGroups"
        ]
        Resource = "*"
      }
    ]
  })
}

# CloudWatch Log Group for Step Functions
resource "aws_cloudwatch_log_group" "step_functions" {
  name              = "/aws/stepfunctions/${local.name_prefix}"
  retention_in_days = 30

  tags = local.common_tags
}

# Step Functions State Machine
resource "aws_sfn_state_machine" "org_evaluation" {
  name     = "${local.name_prefix}-org-evaluation"
  role_arn = aws_iam_role.step_functions.arn

  definition = templatefile("${path.module}/../state-machines/org-evaluation-workflow.json", {
    ECS_CLUSTER_ARN       = aws_ecs_cluster.workflows.arn
    TASK_DEFINITION_ARN   = aws_ecs_task_definition.org_evaluation.arn
    PRIVATE_SUBNETS       = jsonencode(data.aws_subnets.private.ids)
    SECURITY_GROUP_ID     = aws_security_group.ecs_tasks.id
    TASK_ROLE_ARN         = aws_iam_role.ecs_task.arn
  })

  logging_configuration {
    log_destination        = "${aws_cloudwatch_log_group.step_functions.arn}:*"
    include_execution_data = true
    level                  = "ALL"
  }

  tracing_configuration {
    enabled = true
  }

  tags = local.common_tags
}

# CloudWatch Alarms for Step Functions monitoring
resource "aws_cloudwatch_metric_alarm" "step_functions_failed_executions" {
  alarm_name          = "${local.name_prefix}-stepfunctions-failures"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 1
  metric_name         = "ExecutionsFailed"
  namespace           = "AWS/States"
  period              = 300
  statistic           = "Sum"
  threshold           = 5
  alarm_description   = "Alert when more than 5 Step Functions executions fail in 5 minutes"
  treat_missing_data  = "notBreaching"

  dimensions = {
    StateMachineArn = aws_sfn_state_machine.org_evaluation.arn
  }

  # Uncomment to enable SNS notifications
  # alarm_actions = [aws_sns_topic.alerts.arn]

  tags = local.common_tags
}

resource "aws_cloudwatch_metric_alarm" "step_functions_throttled_executions" {
  alarm_name          = "${local.name_prefix}-stepfunctions-throttled"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 1
  metric_name         = "ExecutionThrottled"
  namespace           = "AWS/States"
  period              = 300
  statistic           = "Sum"
  threshold           = 10
  alarm_description   = "Alert when Step Functions executions are being throttled"
  treat_missing_data  = "notBreaching"

  dimensions = {
    StateMachineArn = aws_sfn_state_machine.org_evaluation.arn
  }

  # Uncomment to enable SNS notifications
  # alarm_actions = [aws_sns_topic.alerts.arn]

  tags = local.common_tags
}

# Output
output "step_function_arn" {
  description = "ARN of the Step Functions state machine"
  value       = aws_sfn_state_machine.org_evaluation.arn
}

output "step_function_name" {
  description = "Name of the Step Functions state machine"
  value       = aws_sfn_state_machine.org_evaluation.name
}
