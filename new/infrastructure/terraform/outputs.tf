# infrastructure/terraform/outputs.tf

output "deployment_summary" {
  description = "Summary of deployed resources"
  value = {
    ecs_cluster_name        = aws_ecs_cluster.workflows.name
    ecs_cluster_arn         = aws_ecs_cluster.workflows.arn
    task_definition_family  = aws_ecs_task_definition.org_evaluation.family
    task_definition_arn     = aws_ecs_task_definition.org_evaluation.arn
    step_function_name      = aws_sfn_state_machine.org_evaluation.name
    step_function_arn       = aws_sfn_state_machine.org_evaluation.arn
    secrets_arn             = aws_secretsmanager_secret.workflow_secrets.arn
    log_group_ecs           = aws_cloudwatch_log_group.ecs_tasks.name
    log_group_stepfunctions = aws_cloudwatch_log_group.step_functions.name
    security_group_id       = aws_security_group.ecs_tasks.id
  }
}

output "trigger_workflow_command" {
  description = "AWS CLI command to trigger a workflow manually"
  value       = <<-EOT
    aws stepfunctions start-execution \
      --state-machine-arn ${aws_sfn_state_machine.org_evaluation.arn} \
      --input '{"org_id":"<ORG_ID>","triggered_by":"manual","user_id":"<USER_ID>"}'
  EOT
}

output "view_logs_command" {
  description = "AWS CLI command to view recent logs"
  value       = <<-EOT
    # View ECS task logs:
    aws logs tail ${aws_cloudwatch_log_group.ecs_tasks.name} --follow

    # View Step Functions logs:
    aws logs tail ${aws_cloudwatch_log_group.step_functions.name} --follow
  EOT
}

output "next_steps" {
  description = "Next steps after deployment"
  value       = <<-EOT
    ✅ Infrastructure deployed successfully!

    Next steps:
    1. Build and push Docker image to ECR
    2. Update task definition with new image URI
    3. Test workflow execution with a single org
    4. Monitor CloudWatch Logs and X-Ray traces
    5. Enable EventBridge trigger for scheduled runs

    Monitoring URLs:
    - Step Functions Console: https://console.aws.amazon.com/states/home?region=${var.aws_region}#/statemachines/view/${aws_sfn_state_machine.org_evaluation.arn}
    - ECS Cluster: https://console.aws.amazon.com/ecs/v2/clusters/${aws_ecs_cluster.workflows.name}
    - CloudWatch Logs: https://console.aws.amazon.com/cloudwatch/home?region=${var.aws_region}#logsV2:log-groups
  EOT
}
