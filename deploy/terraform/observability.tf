# Logs, error alarms, traces and a spending tripwire for the backend Lambda
# (#46), within AWS's free monthly allowances even with the function at its
# concurrency cap for a whole month. deploy/README.md has the budget for
# every source.
#
# Never turn on Lambda Active tracing, Lambda Insights, CloudWatch
# Transaction Search, Application Signals, RUM, Synthetics, Contributor
# Insights or detailed monitoring: each one is billed outside these limits.

locals {
  # The worst case, as backend/observability/budget.go works it out. Keep the
  # two in step: the backend checks the same budget when it starts.
  minutes_per_month       = 31 * 24 * 60
  log_line_overhead_bytes = 128
  dropped_line_bytes      = 256
  monthly_log_bytes = local.lambda_concurrency_cap * local.minutes_per_month * (
    var.log_lines_per_minute * (var.log_line_bytes + local.log_line_overhead_bytes) + local.dropped_line_bytes + local.log_line_overhead_bytes
  )
  monthly_traces          = ceil(local.lambda_concurrency_cap * local.minutes_per_month * 60 / var.trace_interval_seconds)
  monthly_log_budget      = 1500000000
  monthly_trace_budget    = 80000
  concurrency_cap_setting = var.reserved_concurrency == null ? "account_concurrency_limit (reserved_concurrency is null)" : "reserved_concurrency"

  # The AWS Distro for OpenTelemetry collector layer. Its build has the
  # awsxray exporter, which X-Ray bills per trace recorded; the upstream
  # opentelemetry-lambda layer would need X-Ray's OTLP endpoint, which only
  # works with Transaction Search on.
  # An empty collector_layer_arn turns tracing off.
  collector_layer_arn = var.collector_layer_arn == null ? "arn:aws:lambda:${var.region}:901920570463:layer:aws-otel-collector-arm64-ver-0-156-0:4" : var.collector_layer_arn
  collector_layers    = local.collector_layer_arn == "" ? [] : [local.collector_layer_arn]

  # The collector's configuration, passed in the environment so it needs no
  # file in the function's zip. It takes OTLP over HTTP on localhost, sends
  # traces to X-Ray in the function's region, and logs only its errors. JSON
  # is YAML too, and fits on one line.
  collector_config = jsonencode({
    receivers = { otlp = { protocols = { http = { endpoint = "localhost:4318" } } } }
    exporters = { awsxray = {} }
    service = {
      telemetry = { logs = { level = "error" }, metrics = { level = "none" } }
      pipelines = { traces = { receivers = ["otlp"], exporters = ["awsxray"] } }
    }
  })
  collector_environment = local.collector_layer_arn == "" ? tomap({}) : tomap({
    OPENTELEMETRY_COLLECTOR_CONFIG_URI = "env:ISLAND_BATTLES_COLLECTOR_CONFIG"
    ISLAND_BATTLES_COLLECTOR_CONFIG    = local.collector_config
    # The extension's own logger, apart from the collector's.
    OPENTELEMETRY_EXTENSION_LOG_LEVEL = "error"
    # The backend sends its spans to the collector. Tracing is off without it.
    OTEL_EXPORTER_OTLP_ENDPOINT = "http://localhost:4318"
  })
  # The names, also when tracing is off, so lambda.env can't set them.
  collector_setting_names = ["OPENTELEMETRY_COLLECTOR_CONFIG_URI", "ISLAND_BATTLES_COLLECTOR_CONFIG", "OPENTELEMETRY_EXTENSION_LOG_LEVEL", "OTEL_EXPORTER_OTLP_ENDPOINT"]
}

# Without this, the function creates its own log group on first use and keeps
# the logs forever. Seven days keep storage to about a quarter of a month's
# logs.
resource "aws_cloudwatch_log_group" "backend" {
  name              = "/aws/lambda/${var.name}"
  retention_in_days = 7
  # Metric filters only work on the standard class.
  log_group_class = "STANDARD"

  lifecycle {
    precondition {
      condition     = local.monthly_log_bytes <= local.monthly_log_budget
      error_message = "${local.concurrency_cap_setting} (${local.lambda_concurrency_cap}) instances writing log_lines_per_minute (${var.log_lines_per_minute}) lines of log_line_bytes (${var.log_line_bytes}) a minute come to ${local.monthly_log_bytes} bytes of logs a month, over the free-tier budget of ${local.monthly_log_budget}. Lower one of them (deploy/README.md)."
    }
    precondition {
      condition     = local.monthly_traces <= local.monthly_trace_budget
      error_message = "${local.concurrency_cap_setting} (${local.lambda_concurrency_cap}) instances starting a trace every trace_interval_seconds (${var.trace_interval_seconds}) come to ${local.monthly_traces} traces a month, over the free-tier budget of ${local.monthly_trace_budget}. Lower the concurrency or raise the interval (deploy/README.md)."
    }
  }
}

# Counts the error lines: the backend's, and the collector's, which log in
# lower case if Lambda lets them through at all (deploy/README.md). It is 1
# of the 10 free custom metrics. It has no dimensions,
# because each dimension value would be another metric.
resource "aws_cloudwatch_log_metric_filter" "errors" {
  name           = "${var.name}-errors"
  log_group_name = aws_cloudwatch_log_group.backend.name
  pattern        = "{ ($.level = \"ERROR\") || ($.level = \"error\") }"

  metric_transformation {
    name      = "ErrorLines"
    namespace = "IslandBattles/Backend"
    value     = "1"
  }
}

resource "aws_sns_topic" "alerts" {
  name = "${var.name}-alerts"
}

# SNS emails a confirmation link first. Until it is followed, the alarms send
# nothing.
resource "aws_sns_topic_subscription" "alert_email" {
  topic_arn = aws_sns_topic.alerts.arn
  protocol  = "email"
  endpoint  = var.alert_email
}

# Two standard alarms of the 10 free. Each looks at one hour and notifies only
# on entering ALARM. It must pass an hour in OK first, so each sends at most
# 360 emails in a 30-day month and 372 in a 31-day one.
resource "aws_cloudwatch_metric_alarm" "lambda_errors" {
  alarm_name          = "${var.name}-lambda-errors"
  alarm_description   = "The backend Lambda failed an invocation: a crash, a timeout or a failed start. Read /aws/lambda/${var.name} (deploy/README.md)."
  namespace           = "AWS/Lambda"
  metric_name         = "Errors"
  dimensions          = { FunctionName = aws_lambda_function.backend.function_name }
  statistic           = "Sum"
  period              = 3600
  evaluation_periods  = 1
  threshold           = 0
  comparison_operator = "GreaterThanThreshold"
  treat_missing_data  = "notBreaching"
  alarm_actions       = [aws_sns_topic.alerts.arn]
}

resource "aws_cloudwatch_metric_alarm" "error_lines" {
  alarm_name          = "${var.name}-error-lines"
  alarm_description   = "The backend logged an error, such as a 5xx answer or a recovered panic. Read /aws/lambda/${var.name} (deploy/README.md)."
  namespace           = aws_cloudwatch_log_metric_filter.errors.metric_transformation[0].namespace
  metric_name         = aws_cloudwatch_log_metric_filter.errors.metric_transformation[0].name
  statistic           = "Sum"
  period              = 3600
  evaluation_periods  = 1
  threshold           = 0
  comparison_operator = "GreaterThanThreshold"
  treat_missing_data  = "notBreaching"
  alarm_actions       = [aws_sns_topic.alerts.arn]
}

# The tripwire: a "zero spend" budget that emails once anything costs money.
# Budgets updates about three times a day, so a charge is reported within a
# day. It stops nothing; the limits above do. A budget without actions is
# free.
resource "aws_budgets_budget" "zero_spend" {
  name         = "${var.name}-zero-spend"
  budget_type  = "COST"
  limit_amount = "0.01"
  limit_unit   = "USD"
  time_unit    = "MONTHLY"

  notification {
    comparison_operator        = "GREATER_THAN"
    threshold                  = 100
    threshold_type             = "PERCENTAGE"
    notification_type          = "ACTUAL"
    subscriber_email_addresses = [var.alert_email]
  }
}
