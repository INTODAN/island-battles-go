# The bot runners (#223): two Lambda functions that each play one turn of one
# uploaded bot package per invocation, and submit it with the turn's submit
# token. The Python runner plays python packages on the python3.14 runtime;
# the native runner plays native packages, such as Go bots, and also posts a
# hosted bot's delivery to its server. The backend invokes them
# asynchronously (#225). See deploy/README.md, "Bot runners".
#
# Like the backend, Terraform publishes their code: scripts/aws.sh builds the
# native runner's bootstrap next to the backend's, and the Python runner is
# one file of the standard library, zipped from the repository.

locals {
  build_directory                  = dirname(local.lambda_bootstrap_file)
  bot_runner_native_bootstrap_file = "${local.build_directory}/bot-runner-native/bootstrap"
  bot_runner_python_source_file    = "${path.module}/../../backend/botrunner/python/runner.py"

  # The owner's budget (#112): 1 GB and 2 seconds of play per turn, plus up
  # to 10 seconds to load a package. The timeout covers both with room to
  # submit. The ephemeral storage holds a 250 MiB zip while it unpacks to at
  # most 1 GiB.
  bot_runners = {
    python = {
      runtime  = "python3.14"
      handler  = "runner.handler"
      zip      = data.archive_file.bot_runner_python
      describe = "Plays one turn of an uploaded Python bot package."
    }
    native = {
      runtime  = "provided.al2023"
      handler  = "bootstrap"
      zip      = data.archive_file.bot_runner_native
      describe = "Plays one turn of an uploaded native bot package, or delivers a hosted bot's turn."
    }
  }
  bot_runner_memory_mb            = 1024
  bot_runner_timeout_seconds      = 20
  bot_runner_ephemeral_storage_mb = 2048
  bot_runner_reserved_concurrency = 10
  bot_runner_maximum_event_age    = 60
  bot_runner_function_names       = { for kind, _ in local.bot_runners : kind => "${var.name}-bot-runner-${kind}" }
  bot_runner_log_group_names      = { for kind, name in local.bot_runner_function_names : kind => "/aws/lambda/${name}" }
}

data "archive_file" "bot_runner_python" {
  type             = "zip"
  source_file      = local.bot_runner_python_source_file
  output_path      = "${local.build_directory}/bot-runner-python.zip"
  output_file_mode = "0644"
}

# The provided.al2023 runtime runs the executable named bootstrap at the root
# of the zip, as for the backend (lambda.tf).
data "archive_file" "bot_runner_native" {
  type             = "zip"
  source_file      = local.bot_runner_native_bootstrap_file
  output_path      = "${local.build_directory}/bot-runner-native/bootstrap.zip"
  output_file_mode = "0755"
}

# The runners hold no secrets and no permissions: they write their logs, and
# nothing else in AWS. A package arrives as a presigned GET in the event, so
# they can't read the bot package bucket either.
resource "aws_iam_role" "bot_runner" {
  name = "${var.name}-bot-runner"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "lambda.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })
}

resource "aws_iam_role_policy" "bot_runner_logs" {
  name = "logs"
  role = aws_iam_role.bot_runner.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Sid      = "WriteTheirLogs"
      Effect   = "Allow"
      Action   = ["logs:CreateLogStream", "logs:PutLogEvents"]
      Resource = [for group in aws_cloudwatch_log_group.bot_runner : "${group.arn}:*"]
    }]
  })
}

# Created before the functions, so Lambda never creates a group that keeps
# its logs forever.
resource "aws_cloudwatch_log_group" "bot_runner" {
  for_each          = local.bot_runner_log_group_names
  name              = each.value
  retention_in_days = 7
}

resource "aws_lambda_function" "bot_runner" {
  for_each      = local.bot_runners
  function_name = local.bot_runner_function_names[each.key]
  description   = each.value.describe
  role          = aws_iam_role.bot_runner.arn
  runtime       = each.value.runtime
  handler       = each.value.handler
  architectures = ["arm64"]

  filename         = each.value.zip.output_path
  source_code_hash = each.value.zip.output_base64sha256

  memory_size = local.bot_runner_memory_mb
  timeout     = local.bot_runner_timeout_seconds
  ephemeral_storage {
    size = local.bot_runner_ephemeral_storage_mb
  }
  # Ten turns at once each at most, so a runaway can't take the account's
  # concurrency that the backend needs (#106).
  reserved_concurrent_executions = local.bot_runner_reserved_concurrency

  environment {
    variables = {
      # The only host a delivery's submit URL may name.
      GAME_SUBMIT_HOST = var.game_hostname
    }
  }

  # Warnings and errors only, as for the backend: one line for each turn a
  # bot passes, and none of the start and report lines of every invocation.
  logging_config {
    log_format            = "JSON"
    log_group             = aws_cloudwatch_log_group.bot_runner[each.key].name
    application_log_level = "WARN"
    system_log_level      = "WARN"
  }

  depends_on = [aws_iam_role_policy.bot_runner_logs, aws_cloudwatch_log_group.bot_runner]
}

# A turn is played once or not at all: a retry would submit a turn twice, or
# after its deadline.
resource "aws_lambda_function_event_invoke_config" "bot_runner" {
  for_each                     = aws_lambda_function.bot_runner
  function_name                = each.value.function_name
  maximum_retry_attempts       = 0
  maximum_event_age_in_seconds = local.bot_runner_maximum_event_age
}
