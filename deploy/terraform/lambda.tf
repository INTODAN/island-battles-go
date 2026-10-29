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

  # Terraform sets the pool size itself, so it can check it against the
  # reserved concurrency below.
  lambda_pool_setting = "ISLAND_BATTLES_DATABASE_MAX_OPEN_CONNECTIONS"
  lambda_environment = merge(local.lambda_settings, var.extra_lambda_environment, {
    (local.lambda_pool_setting) = tostring(var.database_max_open_connections)
  })

  # Without a reservation, the account's own limit caps the function.
  lambda_concurrency_cap      = var.reserved_concurrency == null ? var.account_concurrency_limit : var.reserved_concurrency
  lambda_database_connections = local.lambda_concurrency_cap * var.database_max_open_connections
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

# Basic execution only: the function writes its logs and calls nothing else in
# AWS. It reaches Postgres and Centrifugo over the internet.
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

  # The backend logs JSON lines with slog's "level" key, which Lambda's level
  # filtering reads (backend/README.md).
  logging_config {
    log_format            = "JSON"
    application_log_level = "INFO"
    system_log_level      = "WARN"
  }

  depends_on = [aws_iam_role_policy_attachment.lambda_logs]

  lifecycle {
    precondition {
      condition     = local.lambda_settings_found || var.allow_missing_lambda_settings
      error_message = "${local.lambda_settings_file} is missing. Copy deploy/local/ from where you keep it, or run the VM deploy, which writes it. Only for the very first apply, pass -var allow_missing_lambda_settings=true."
    }
    precondition {
      condition     = length(local.lambda_settings_malformed) == 0
      error_message = "${local.lambda_settings_file} has lines that aren't KEY=value: ${join(", ", [for line in local.lambda_settings_malformed : tostring(line)])} (counting only lines that aren't blank or comments)."
    }
    precondition {
      condition     = !contains(concat(keys(local.lambda_settings), keys(var.extra_lambda_environment)), local.lambda_pool_setting)
      error_message = "Set the pool size with the database_max_open_connections variable, not ${local.lambda_pool_setting}, so it is checked against reserved_concurrency."
    }
    precondition {
      condition     = local.lambda_database_connections <= var.postgres_max_connections - var.postgres_spare_connections
      error_message = "${var.reserved_concurrency == null ? "account_concurrency_limit (reserved_concurrency is null)" : "reserved_concurrency"} × database_max_open_connections is ${local.lambda_database_connections}, which leaves fewer than postgres_spare_connections (${var.postgres_spare_connections}) of Postgres's ${var.postgres_max_connections} connections free. Lower one of them."
    }
  }
}

# Players open the lobby UI here, and Centrifugo calls the subscribe proxy
# here. For auth type NONE, the provider adds the public lambda:InvokeFunctionUrl
# and lambda:InvokeFunction permissions on creation. The backend serves through
# lambdaurl, which needs RESPONSE_STREAM.
resource "aws_lambda_function_url" "backend" {
  function_name      = aws_lambda_function.backend.function_name
  authorization_type = "NONE"
  invoke_mode        = "RESPONSE_STREAM"
}
