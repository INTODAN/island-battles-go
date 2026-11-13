# The backend on Lambda behind its Function URL (#38). Terraform owns the code
# and the settings too: scripts/aws.sh deploy builds the binary, migrates the
# database and applies this configuration. See deploy/README.md for why.

locals {
  lambda_bootstrap_file = coalesce(var.lambda_bootstrap_file, "${path.module}/../local/lambda/bootstrap")
  lambda_settings_file  = coalesce(var.lambda_settings_file, "${path.module}/../local/lambda.env")

  # lambda.env holds KEY=value lines, as the VM deploy writes them. Values are
  # taken literally, without quotes or escapes. Blank lines and lines starting
  # with # are skipped. A repeated key keeps its last value.
  lambda_settings_found = fileexists(local.lambda_settings_file)
  lambda_settings_lines = [
    for line in split("\n", local.lambda_settings_found ? file(local.lambda_settings_file) : "") :
    trimspace(line) if trimspace(line) != "" && !startswith(trimspace(line), "#")
  ]
  lambda_settings_pattern = "^([A-Za-z_][A-Za-z0-9_]*)=(.*)$"
  lambda_settings_malformed = [
    for index, line in local.lambda_settings_lines : index + 1 if !can(regex(local.lambda_settings_pattern, line))
  ]
  lambda_settings_values = {
    for line in local.lambda_settings_lines :
    regex(local.lambda_settings_pattern, line)[0] => regex(local.lambda_settings_pattern, line)[1]...
    if can(regex(local.lambda_settings_pattern, line))
  }
  lambda_settings = { for key, values in local.lambda_settings_values : key => values[length(values) - 1] }

  # Without a reservation, the account's own limit caps the function.
  lambda_concurrency_cap      = var.reserved_concurrency == null ? var.account_concurrency_limit : var.reserved_concurrency
  lambda_database_connections = local.lambda_concurrency_cap * var.database_max_open_connections

  # Terraform sets these itself, so it can check them against the
  # concurrency cap: the pool size against Postgres's connections, and the
  # log and trace limits against the free tier (observability.tf). The
  # backend reads the same cap as its concurrency, C, and checks the budget
  # again when it starts.
  lambda_terraform_settings = {
    ISLAND_BATTLES_DATABASE_MAX_OPEN_CONNECTIONS = tostring(var.database_max_open_connections)
    ISLAND_BATTLES_CONCURRENCY                   = tostring(local.lambda_concurrency_cap)
    ISLAND_BATTLES_LOG_LINES_PER_MINUTE          = tostring(var.log_lines_per_minute)
    ISLAND_BATTLES_LOG_LINE_BYTES                = tostring(var.log_line_bytes)
    ISLAND_BATTLES_TRACE_INTERVAL                = "${var.trace_interval_seconds}s"
    # The bucket that this configuration creates (bot_packages.tf).
    ISLAND_BATTLES_BOT_PACKAGES_BUCKET = local.bot_packages_bucket
    # The bot runners that play hosted and uploaded bots' turns, which the
    # backend invokes (bot_runners.tf, #225). Naming the functions makes
    # Terraform create them before it updates the backend.
    ISLAND_BATTLES_BOT_RUNNER_PYTHON = aws_lambda_function.bot_runner["python"].function_name
    ISLAND_BATTLES_BOT_RUNNER_NATIVE = aws_lambda_function.bot_runner["native"].function_name
  }
  lambda_settings_overridden = sort([
    for key in concat(keys(local.lambda_settings), keys(var.extra_lambda_environment)) : key
    if contains(keys(local.lambda_terraform_settings), key) || contains(local.collector_setting_names, key)
  ])
  lambda_environment = merge(local.lambda_settings, var.extra_lambda_environment, local.lambda_terraform_settings, local.collector_environment)
  # Lambda refuses a function whose variables, names and values together,
  # come to more than 4 KB.
  lambda_environment_bytes = length(join("", [for key, value in local.lambda_environment : "${key}${value}"]))
}

# The provided.al2023 runtime runs the executable named bootstrap at the root
# of the zip. A fixed file mode keeps the zip, and so the code hash, the same
# for the same binary.
data "archive_file" "backend" {
  type             = "zip"
  source_file      = local.lambda_bootstrap_file
  output_path      = "${dirname(local.lambda_bootstrap_file)}/bootstrap.zip"
  output_file_mode = "0755"
}

# The function writes its logs, and its collector layer sends traces to X-Ray
# (observability.tf). In S3 it presigns uploads to, reads and deletes the zips
# of the bot package bucket, and only that bucket (bot_packages.tf). It
# invokes the two bot runner functions, and no other function, and reads
# their compute with cloudwatch:GetMetricStatistics (bot_runners.tf). It calls
# nothing else in AWS: it reaches Postgres and
# Centrifugo over the internet.
resource "aws_iam_role" "lambda" {
  name = "${var.name}-lambda"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "lambda.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })
}

resource "aws_iam_role_policy_attachment" "lambda_logs" {
  role       = aws_iam_role.lambda.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

# xray:PutTraceSegments and xray:PutTelemetryRecords, for the collector.
resource "aws_iam_role_policy_attachment" "lambda_traces" {
  role       = aws_iam_role.lambda.name
  policy_arn = "arn:aws:iam::aws:policy/AWSXRayDaemonWriteAccess"
}

resource "aws_lambda_function" "backend" {
  function_name = var.name
  role          = aws_iam_role.lambda.arn
  runtime       = "provided.al2023"
  handler       = "bootstrap"
  architectures = ["arm64"]

  filename         = data.archive_file.backend.output_path
  source_code_hash = data.archive_file.backend.output_base64sha256

  memory_size                    = var.lambda_memory_mb
  timeout                        = var.lambda_timeout_seconds
  reserved_concurrent_executions = var.reserved_concurrency == null ? -1 : var.reserved_concurrency

  environment {
    # The settings hold the database password and Centrifugo's keys, so plans
    # don't print them. They are still in the state.
    variables = sensitive(local.lambda_environment)
  }

  # The OpenTelemetry collector, which takes the backend's spans on localhost
  # and sends them to X-Ray (observability.tf).
  layers = local.collector_layers

  # The backend logs JSON lines with a "level" and a "timestamp", which
  # Lambda's level filtering reads. Only warnings and errors reach CloudWatch,
  # from the backend and from Lambda itself: no request lines, and none of
  # the start and report lines of every invocation (deploy/README.md).
  logging_config {
    log_format            = "JSON"
    log_group             = aws_cloudwatch_log_group.backend.name
    application_log_level = "WARN"
    system_log_level      = "WARN"
  }

  # Lambda's own tracing stays off. Active tracing would sample about one
  # request a second, some 2.6 million traces a month, over X-Ray's free
  # 100,000. The backend samples its own traces instead.
  tracing_config {
    mode = "PassThrough"
  }

  # The log group must exist before the first invocation, or Lambda creates
  # one that keeps its logs forever.
  depends_on = [aws_iam_role_policy_attachment.lambda_logs, aws_iam_role_policy_attachment.lambda_traces, aws_cloudwatch_log_group.backend]

  lifecycle {
    precondition {
      condition     = local.lambda_settings_found || var.allow_missing_lambda_settings
      error_message = "${local.lambda_settings_file} is missing. Push deploy/local/ to the deploy bucket from the machine that has it, or run the VM deploy, which writes it. Only for the very first apply, pass -var allow_missing_lambda_settings=true."
    }
    precondition {
      condition     = length(local.lambda_settings_malformed) == 0
      error_message = "${local.lambda_settings_file} has lines that aren't KEY=value: ${join(", ", [for line in local.lambda_settings_malformed : tostring(line)])} (counting only lines that aren't blank or comments)."
    }
    precondition {
      condition     = length(local.lambda_settings_overridden) == 0
      error_message = "Terraform sets ${join(", ", local.lambda_settings_overridden)} itself, so they're checked against the concurrency cap and the free tier. Remove them from lambda_settings_file and extra_lambda_environment, and set the pool size with database_max_open_connections and the log and trace limits with log_lines_per_minute, log_line_bytes and trace_interval_seconds (deploy/README.md)."
    }
    precondition {
      condition     = local.lambda_environment_bytes <= 4096
      error_message = "The function's environment comes to ${local.lambda_environment_bytes} bytes, over Lambda's 4 KB."
    }
    precondition {
      condition     = local.lambda_database_connections <= var.postgres_max_connections - var.postgres_spare_connections
      error_message = "${var.reserved_concurrency == null ? "account_concurrency_limit (reserved_concurrency is null)" : "reserved_concurrency"} × database_max_open_connections is ${local.lambda_database_connections}, which leaves fewer than postgres_spare_connections (${var.postgres_spare_connections}) of Postgres's ${var.postgres_max_connections} connections free. Lower one of them."
    }
  }
}

# CloudFront serves the game from here (cloudfront.tf), and Centrifugo calls
# the subscribe proxy here. For auth type NONE, the provider adds the public lambda:InvokeFunctionUrl
# and lambda:InvokeFunction permissions on creation. The backend serves through
# lambdaurl, which needs RESPONSE_STREAM.
resource "aws_lambda_function_url" "backend" {
  function_name      = aws_lambda_function.backend.function_name
  authorization_type = "NONE"
  invoke_mode        = "RESPONSE_STREAM"
}
