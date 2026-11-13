# Offline checks of the configuration's own logic. Both providers are mocked,
# so these plans never call AWS and need no credentials or built binary:
#
#   terraform -chdir=deploy/terraform test

mock_provider "aws" {}
mock_provider "archive" {}

variables {
  region                = "eu-central-1"
  lambda_settings_file  = "tests/fixtures/lambda.env"
  lambda_bootstrap_file = "tests/fixtures/bootstrap"
  vm_instances = {
    "island-battles-1" = {}
  }
  static_ip_instance = "island-battles-1"
  alert_email        = "alerts@example.org"
  game_hostname      = "island-battles.example.org"
}

run "passes_every_lambda_setting" {
  command = plan

  assert {
    condition = tomap({
      for key, value in nonsensitive(aws_lambda_function.backend.environment[0].variables) : key => value
      if !contains(keys(local.collector_environment), key)
      }) == tomap({
      ISLAND_BATTLES_DATABASE_URL                  = "postgres://island_battles:not-a-secret@vm.example.org:5432/island_battles?sslmode=require"
      ISLAND_BATTLES_CENTRIFUGO_PUBLIC_URL         = "wss://vm.example.org/connection/websocket"
      ISLAND_BATTLES_CENTRIFUGO_API_URL            = "https://vm.example.org/api"
      ISLAND_BATTLES_CENTRIFUGO_API_KEY            = "not-a-secret"
      ISLAND_BATTLES_CONNECTION_TOKEN_HMAC_SECRET  = "not-a-secret"
      ISLAND_BATTLES_LOBBY_PROXY_SECRET            = "not-a-secret"
      ISLAND_BATTLES_SETTING_TERRAFORM_DOESNT_KNOW = "a=b"
      ISLAND_BATTLES_DATABASE_MAX_OPEN_CONNECTIONS = "2"
      ISLAND_BATTLES_CONCURRENCY                   = "5"
      ISLAND_BATTLES_LOG_LINES_PER_MINUTE          = "5"
      ISLAND_BATTLES_LOG_LINE_BYTES                = "1024"
      ISLAND_BATTLES_TRACE_INTERVAL                = "180s"
      ISLAND_BATTLES_BOT_PACKAGES_BUCKET           = "island-battles-bot-packages"
    })
    error_message = "The function must get every lambda.env setting, the last value of a repeated key, the pool size, the log and trace limits with C from the reserved concurrency, and the bot package bucket."
  }

  assert {
    condition     = aws_lambda_function.backend.runtime == "provided.al2023" && aws_lambda_function.backend.architectures == tolist(["arm64"]) && aws_lambda_function.backend.handler == "bootstrap"
    error_message = "The function must run the arm64 bootstrap on provided.al2023."
  }

  assert {
    condition     = aws_lambda_function.backend.reserved_concurrent_executions == 5
    error_message = "The default reserved concurrency must be 5."
  }

  assert {
    condition     = aws_lambda_function_url.backend.authorization_type == "NONE" && aws_lambda_function_url.backend.invoke_mode == "RESPONSE_STREAM"
    error_message = "The Function URL must be public and streamed."
  }

  assert {
    condition     = aws_iam_role_policy_attachment.lambda_logs.policy_arn == "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole" && aws_iam_role_policy_attachment.lambda_traces.policy_arn == "arn:aws:iam::aws:policy/AWSXRayDaemonWriteAccess"
    error_message = "The role must have basic execution and X-Ray writes only."
  }

  assert {
    condition     = toset([for port in aws_lightsail_instance_public_ports.vm["island-battles-1"].port_info : port.from_port]) == toset([22, 443, 5432])
    error_message = "The firewall must open 22, 443 and Postgres, and nothing else."
  }

  assert {
    condition     = one([for add_on in aws_lightsail_instance.vm["island-battles-1"].add_on : add_on if add_on.type == "AutoSnapshot" && add_on.status == "Disabled"]) != null
    error_message = "The instance's automatic snapshots must be declared off: the database is backed up to S3 instead."
  }

  assert {
    condition     = aws_lightsail_static_ip_attachment.vm.instance_name == "island-battles-1"
    error_message = "The static IP must be attached to the default instance."
  }
}

run "takes_extra_settings" {
  command = plan

  variables {
    extra_lambda_environment = {
      ISLAND_BATTLES_DATABASE_CONNECTION_MAX_IDLE_TIME = "30s"
    }
  }

  assert {
    condition     = nonsensitive(aws_lambda_function.backend.environment[0].variables["ISLAND_BATTLES_DATABASE_CONNECTION_MAX_IDLE_TIME"]) == "30s"
    error_message = "Extra settings must reach the function."
  }
}

run "rejects_too_many_database_connections" {
  command = plan

  variables {
    # 5 × 19 = 95 leaves 5 of 100 free, fewer than the 10 spare connections.
    database_max_open_connections = 19
  }

  expect_failures = [aws_lambda_function.backend]
}

run "allows_the_largest_safe_pool" {
  command = plan

  variables {
    database_max_open_connections = 18
  }

  assert {
    condition     = nonsensitive(aws_lambda_function.backend.environment[0].variables["ISLAND_BATTLES_DATABASE_MAX_OPEN_CONNECTIONS"]) == "18"
    error_message = "5 × 18 = 90 leaves exactly the 10 spare connections."
  }
}

# A new account's limit of 10 can't be reserved. Unreserved, the limit is C,
# and the default log and trace limits are over the budget for 10 instances,
# so the documented ones for that case apply.
run "leaves_concurrency_unreserved_when_asked" {
  command = plan

  variables {
    reserved_concurrency   = null
    log_lines_per_minute   = 2
    trace_interval_seconds = 360
  }

  assert {
    condition     = aws_lambda_function.backend.reserved_concurrent_executions == -1
    error_message = "A null reserved_concurrency must leave the function unreserved."
  }

  assert {
    condition     = nonsensitive(aws_lambda_function.backend.environment[0].variables["ISLAND_BATTLES_CONCURRENCY"]) == "10"
    error_message = "Unreserved, the backend's concurrency must be the account limit."
  }
}

run "checks_the_account_limit_when_concurrency_is_unreserved" {
  command = plan

  variables {
    # Unreserved, the account limit caps the function: 5 × 19 = 95 leaves 5
    # of 100 free, fewer than the 10 spare connections.
    reserved_concurrency          = null
    account_concurrency_limit     = 5
    database_max_open_connections = 19
  }

  expect_failures = [aws_lambda_function.backend]
}

run "rejects_an_account_limit_of_1000_without_a_reservation" {
  command = plan

  variables {
    reserved_concurrency      = null
    account_concurrency_limit = 1000
  }

  # 1,000 instances are over the free-tier budget too. The function depends
  # on the log group, so the plan stops there.
  expect_failures = [aws_cloudwatch_log_group.backend]
}

run "allows_the_largest_safe_account_limit_without_a_reservation" {
  command = plan

  variables {
    reserved_concurrency          = null
    account_concurrency_limit     = 5
    database_max_open_connections = 18
  }

  assert {
    condition     = aws_lambda_function.backend.reserved_concurrent_executions == -1
    error_message = "An account limit of 5 × 18 = 90 leaves exactly the 10 spare connections."
  }
}

run "ignores_the_account_limit_when_concurrency_is_reserved" {
  command = plan

  variables {
    reserved_concurrency      = 5
    account_concurrency_limit = 1000
  }

  assert {
    condition     = aws_lambda_function.backend.reserved_concurrent_executions == 5 && nonsensitive(aws_lambda_function.backend.environment[0].variables["ISLAND_BATTLES_CONCURRENCY"]) == "5"
    error_message = "A reservation caps the function, whatever the account limit."
  }
}

# The game's name (#75) --------------------------------------------------------

run "serves_the_game_through_cloudfront" {
  command = plan

  # AWS assigns these on creation. Known during the plan, the distribution's
  # origin and certificate can be checked.
  override_resource {
    target          = aws_lambda_function_url.backend
    override_during = plan
    values = {
      function_url = "https://abcdefghijklmnopqrstuvwxyz012345.lambda-url.eu-central-1.on.aws/"
    }
  }

  override_resource {
    target          = aws_acm_certificate.game
    override_during = plan
    values = {
      arn = "arn:aws:acm:us-east-1:123456789012:certificate/00000000-0000-4000-8000-000000000001"
    }
  }

  assert {
    condition     = aws_acm_certificate.game.region == "us-east-1" && aws_acm_certificate.game.domain_name == "island-battles.example.org" && aws_acm_certificate.game.validation_method == "DNS" && aws_acm_certificate_validation.game.region == "us-east-1"
    error_message = "The certificate must be for game_hostname, validated by DNS, in us-east-1 where CloudFront takes it from."
  }

  assert {
    condition = (
      aws_cloudfront_distribution.game.aliases == toset(["island-battles.example.org"]) &&
      aws_cloudfront_distribution.game.viewer_certificate[0].acm_certificate_arn == "arn:aws:acm:us-east-1:123456789012:certificate/00000000-0000-4000-8000-000000000001" &&
      aws_cloudfront_distribution.game.viewer_certificate[0].ssl_support_method == "sni-only"
    )
    error_message = "The distribution must serve game_hostname with the validated certificate."
  }

  assert {
    condition = (
      one([for origin in aws_cloudfront_distribution.game.origin : origin if origin.origin_id == "backend"]).domain_name == "abcdefghijklmnopqrstuvwxyz012345.lambda-url.eu-central-1.on.aws" &&
      one([for origin in aws_cloudfront_distribution.game.origin : origin if origin.origin_id == "backend"]).custom_origin_config[0].origin_protocol_policy == "https-only" &&
      try(length(one([for origin in aws_cloudfront_distribution.game.origin : origin if origin.origin_id == "backend"]).origin_access_control_id), 0) == 0
    )
    error_message = "The distribution must call the Function URL's host over HTTPS, without origin access control."
  }

  assert {
    condition = (
      toset(aws_cloudfront_distribution.game.default_cache_behavior[0].allowed_methods) == toset(["DELETE", "GET", "HEAD", "OPTIONS", "PATCH", "POST", "PUT"]) &&
      aws_cloudfront_distribution.game.default_cache_behavior[0].viewer_protocol_policy == "redirect-to-https" &&
      data.aws_cloudfront_cache_policy.caching_disabled.name == "Managed-CachingDisabled" &&
      aws_cloudfront_distribution.game.default_cache_behavior[0].cache_policy_id == data.aws_cloudfront_cache_policy.caching_disabled.id &&
      data.aws_cloudfront_origin_request_policy.all_viewer_except_host_header.name == "Managed-AllViewerExceptHostHeader" &&
      aws_cloudfront_distribution.game.default_cache_behavior[0].origin_request_policy_id == data.aws_cloudfront_origin_request_policy.all_viewer_except_host_header.id &&
      aws_cloudfront_distribution.game.default_cache_behavior[0].target_origin_id == "backend"
    )
    error_message = "Every other request must reach the Lambda uncached over HTTPS, with every method and every viewer header but Host."
  }

  assert {
    condition     = length(aws_cloudfront_distribution.game.logging_config) == 0 && try(length(aws_cloudfront_distribution.game.default_cache_behavior[0].realtime_log_config_arn), 0) == 0
    error_message = "Standard and real-time logs must stay off."
  }

  assert {
    condition     = output.game_url == "https://island-battles.example.org"
    error_message = "The game's URL is game_hostname over HTTPS."
  }
}

# The web client at /play/ (#104) ----------------------------------------------

run "serves_the_web_client_from_a_private_bucket" {
  command = plan

  # AWS assigns these on creation.
  override_resource {
    target          = aws_lambda_function_url.backend
    override_during = plan
    values = {
      function_url = "https://abcdefghijklmnopqrstuvwxyz012345.lambda-url.eu-central-1.on.aws/"
    }
  }

  override_resource {
    target          = aws_cloudfront_origin_access_control.web_client
    override_during = plan
    values = {
      id = "E2ABCDEFGHIJKL"
    }
  }

  override_resource {
    target          = aws_cloudfront_cache_policy.origin_cache_control
    override_during = plan
    values = {
      id = "11111111-2222-3333-4444-555555555555"
    }
  }

  override_resource {
    target          = aws_cloudfront_distribution.game
    override_during = plan
    values = {
      arn = "arn:aws:cloudfront::123456789012:distribution/E1ABCDEFGHIJKL"
    }
  }

  override_resource {
    target          = aws_s3_bucket.web_client
    override_during = plan
    values = {
      arn = "arn:aws:s3:::island-battles-web-client-123456789012"
    }
  }

  assert {
    condition     = startswith(aws_s3_bucket.web_client.bucket, "island-battles-web-client-") && startswith(aws_s3_bucket.web_client.bucket, "island-battles-")
    error_message = "The bucket must be named island-battles-*, which the apply role may change."
  }

  assert {
    condition = (
      aws_s3_bucket_public_access_block.web_client.block_public_acls &&
      aws_s3_bucket_public_access_block.web_client.block_public_policy &&
      aws_s3_bucket_public_access_block.web_client.ignore_public_acls &&
      aws_s3_bucket_public_access_block.web_client.restrict_public_buckets &&
      one(one(aws_s3_bucket_server_side_encryption_configuration.web_client.rule).apply_server_side_encryption_by_default).sse_algorithm == "AES256"
    )
    error_message = "The bucket must block all public access and encrypt objects."
  }

  assert {
    condition     = one(aws_s3_bucket_ownership_controls.web_client.rule).object_ownership == "BucketOwnerEnforced"
    error_message = "The bucket must have no ACLs."
  }

  assert {
    condition = (
      aws_cloudfront_origin_access_control.web_client.origin_access_control_origin_type == "s3" &&
      aws_cloudfront_origin_access_control.web_client.signing_behavior == "always" &&
      aws_cloudfront_origin_access_control.web_client.signing_protocol == "sigv4"
    )
    error_message = "The origin access control must sign S3 requests."
  }

  assert {
    condition = (
      one([for origin in aws_cloudfront_distribution.game.origin : origin if origin.origin_id == "web-client"]).origin_access_control_id == aws_cloudfront_origin_access_control.web_client.id &&
      length(aws_cloudfront_distribution.game.origin) == 2
    )
    error_message = "The distribution must have the bucket as a second origin, through the origin access control."
  }

  assert {
    condition = (
      toset([for behavior in aws_cloudfront_distribution.game.ordered_cache_behavior : behavior.path_pattern if behavior.target_origin_id == "web-client"]) == toset(["/play/*", "/play"]) &&
      alltrue([for behavior in aws_cloudfront_distribution.game.ordered_cache_behavior : (
        behavior.compress &&
        behavior.viewer_protocol_policy == "redirect-to-https" &&
        toset(behavior.allowed_methods) == toset(["GET", "HEAD", "OPTIONS"]) &&
        behavior.cache_policy_id == data.aws_cloudfront_cache_policy.caching_optimized.id &&
        one(behavior.function_association).event_type == "viewer-request"
      ) if behavior.target_origin_id == "web-client"]) &&
      data.aws_cloudfront_cache_policy.caching_optimized.name == "Managed-CachingOptimized"
    )
    error_message = "/play/* and /play must be cached and compressed from the bucket, with the function on viewer requests, ahead of the uncached default."
  }

  # The Lambda's own pages (#162): the handlers' Cache-Control decides what is
  # kept. No API path may match these patterns.
  assert {
    condition = (
      toset([for behavior in aws_cloudfront_distribution.game.ordered_cache_behavior : behavior.path_pattern if behavior.target_origin_id == "backend"]) == toset(["/lobby/ui/*", "/game/demo/*", "/game/demo-viewer/*", "/map/builder/*"]) &&
      alltrue([for behavior in aws_cloudfront_distribution.game.ordered_cache_behavior : (
        behavior.viewer_protocol_policy == "redirect-to-https" &&
        toset(behavior.allowed_methods) == toset(["GET", "HEAD", "OPTIONS"]) &&
        behavior.compress &&
        behavior.cache_policy_id == aws_cloudfront_cache_policy.origin_cache_control.id &&
        behavior.origin_request_policy_id == data.aws_cloudfront_origin_request_policy.all_viewer_except_host_header.id
      ) if behavior.target_origin_id == "backend"])
    )
    error_message = "The Lambda's static pages must follow the origin's Cache-Control, compressed, with GET, HEAD and OPTIONS only, and the default behaviour's origin request policy."
  }

  # A header in the cache key is sent to the origin, and the Function URL
  # refuses a Host that isn't its own. Cookies in the key would give every
  # player their own copy.
  assert {
    condition = (
      aws_cloudfront_cache_policy.origin_cache_control.min_ttl == 0 &&
      aws_cloudfront_cache_policy.origin_cache_control.default_ttl == 0 &&
      one(one(aws_cloudfront_cache_policy.origin_cache_control.parameters_in_cache_key_and_forwarded_to_origin).headers_config).header_behavior == "none" &&
      one(one(aws_cloudfront_cache_policy.origin_cache_control.parameters_in_cache_key_and_forwarded_to_origin).cookies_config).cookie_behavior == "none" &&
      one(one(aws_cloudfront_cache_policy.origin_cache_control.parameters_in_cache_key_and_forwarded_to_origin).query_strings_config).query_string_behavior == "none"
    )
    error_message = "The static pages' cache policy must cache only what the origin allows, and key on no header, cookie or query string."
  }

  assert {
    condition     = aws_cloudfront_function.web_client.runtime == "cloudfront-js-2.0" && strcontains(aws_cloudfront_function.web_client.code, "index.html") && strcontains(aws_cloudfront_function.web_client.code, "301")
    error_message = "The function must serve folders as index.html and redirect /play."
  }

  assert {
    condition     = strcontains(aws_s3_bucket_policy.web_client.policy, "cloudfront.amazonaws.com") && strcontains(aws_s3_bucket_policy.web_client.policy, "AWS:SourceArn") && strcontains(aws_s3_bucket_policy.web_client.policy, "aws:SecureTransport") && strcontains(aws_s3_bucket_policy.web_client.policy, "distribution/E1ABCDEFGHIJKL")
    error_message = "The bucket policy must allow only the distribution, over TLS."
  }

  assert {
    condition     = output.web_client_bucket == aws_s3_bucket.web_client.bucket
    error_message = "The upload script reads the bucket from an output."
  }
}

run "requires_a_game_hostname" {
  command = plan

  variables {
    game_hostname = "https://island-battles.example.org/"
  }

  expect_failures = [var.game_hostname]
}

# The free-tier budget (#46) ---------------------------------------------------

run "keeps_logs_and_traces_inside_the_free_tier" {
  command = plan

  assert {
    # 5 × 44,640 minutes × (5 × (1,024 + 128) + 256 + 128) bytes, and
    # 5 × 2,678,400 seconds / 180, as backend/observability works them out.
    condition     = local.monthly_log_bytes == 1371340800 && local.monthly_traces == 74400
    error_message = "The defaults' worst case must match the backend's: 1,371,340,800 bytes of logs and 74,400 traces a month."
  }

  assert {
    condition     = aws_cloudwatch_log_group.backend.name == "/aws/lambda/island-battles" && aws_cloudwatch_log_group.backend.retention_in_days == 7 && aws_lambda_function.backend.logging_config[0].log_group == "/aws/lambda/island-battles"
    error_message = "The function must log to its own log group, kept for 7 days."
  }

  assert {
    condition     = aws_lambda_function.backend.logging_config[0].log_format == "JSON" && aws_lambda_function.backend.logging_config[0].application_log_level == "WARN" && aws_lambda_function.backend.logging_config[0].system_log_level == "WARN"
    error_message = "Lambda must filter application and system logs at WARN."
  }

  assert {
    condition     = aws_lambda_function.backend.tracing_config[0].mode == "PassThrough"
    error_message = "Lambda's Active tracing must stay off."
  }

  assert {
    condition     = aws_lambda_function.backend.layers == tolist(["arn:aws:lambda:eu-central-1:901920570463:layer:aws-otel-collector-arm64-ver-0-156-0:4"])
    error_message = "The function must run the ADOT collector layer for arm64 in its region."
  }

  assert {
    condition = (
      nonsensitive(aws_lambda_function.backend.environment[0].variables["OTEL_EXPORTER_OTLP_ENDPOINT"]) == "http://localhost:4318" &&
      nonsensitive(aws_lambda_function.backend.environment[0].variables["OPENTELEMETRY_EXTENSION_LOG_LEVEL"]) == "error" &&
      nonsensitive(aws_lambda_function.backend.environment[0].variables["OPENTELEMETRY_COLLECTOR_CONFIG_URI"]) == "env:ISLAND_BATTLES_COLLECTOR_CONFIG" &&
      jsondecode(nonsensitive(aws_lambda_function.backend.environment[0].variables["ISLAND_BATTLES_COLLECTOR_CONFIG"])).service.telemetry.logs.level == "error" &&
      jsondecode(nonsensitive(aws_lambda_function.backend.environment[0].variables["ISLAND_BATTLES_COLLECTOR_CONFIG"])).service.pipelines.traces.exporters == ["awsxray"]
    )
    error_message = "The backend must send its spans to the collector on localhost, which sends them to X-Ray and logs only errors."
  }

  assert {
    condition     = local.lambda_environment_bytes <= 4096
    error_message = "The function's environment must fit Lambda's 4 KB."
  }
}

run "alerts_on_errors_within_the_free_tier" {
  command = plan

  assert {
    condition     = aws_cloudwatch_log_metric_filter.errors.pattern == "{ ($.level = \"ERROR\") || ($.level = \"error\") }" && try(length(aws_cloudwatch_log_metric_filter.errors.metric_transformation[0].dimensions), 0) == 0
    error_message = "One metric, without dimensions, must count the error lines."
  }

  assert {
    condition = alltrue([for alarm in [aws_cloudwatch_metric_alarm.lambda_errors, aws_cloudwatch_metric_alarm.error_lines] :
      alarm.period == 3600 && alarm.evaluation_periods == 1 && alarm.threshold == 0 && alarm.treat_missing_data == "notBreaching" &&
      length(alarm.alarm_actions) == 1 && try(length(alarm.ok_actions), 0) == 0 && try(length(alarm.insufficient_data_actions), 0) == 0
    ])
    error_message = "Each alarm must look at one hour and notify only on entering ALARM."
  }

  assert {
    condition     = aws_cloudwatch_metric_alarm.lambda_errors.namespace == "AWS/Lambda" && aws_cloudwatch_metric_alarm.lambda_errors.metric_name == "Errors" && aws_cloudwatch_metric_alarm.error_lines.metric_name == "ErrorLines"
    error_message = "One alarm must watch the function's Errors, the other the error lines."
  }

  assert {
    condition     = aws_sns_topic_subscription.alert_email.protocol == "email" && aws_sns_topic_subscription.alert_email.endpoint == "alerts@example.org"
    error_message = "The alarms must email alert_email."
  }

  assert {
    condition = (
      aws_budgets_budget.zero_spend.budget_type == "COST" && aws_budgets_budget.zero_spend.limit_amount == "0.01" && aws_budgets_budget.zero_spend.time_unit == "MONTHLY" &&
      one(aws_budgets_budget.zero_spend.notification).subscriber_email_addresses == toset(["alerts@example.org"])
    )
    error_message = "A zero-spend budget must email alert_email."
  }
}

run "rejects_logs_over_the_budget" {
  command = plan

  variables {
    # 5 × 44,640 × (6 × 1,152 + 384) = 1.63 GB.
    log_lines_per_minute = 6
  }

  expect_failures = [aws_cloudwatch_log_group.backend]
}

run "rejects_traces_over_the_budget" {
  command = plan

  variables {
    # 5 × 2,678,400 / 160 = 83,700 traces.
    trace_interval_seconds = 160
  }

  expect_failures = [aws_cloudwatch_log_group.backend]
}

run "rejects_a_concurrency_over_the_budget" {
  command = plan

  variables {
    reserved_concurrency = 20
  }

  expect_failures = [aws_cloudwatch_log_group.backend]
}

run "rejects_the_limits_in_lambda_settings" {
  command = plan

  variables {
    extra_lambda_environment = {
      ISLAND_BATTLES_LOG_LINES_PER_MINUTE = "100"
    }
  }

  expect_failures = [aws_lambda_function.backend]
}

run "turns_tracing_off_without_a_collector_layer" {
  command = plan

  variables {
    collector_layer_arn = ""
  }

  assert {
    condition     = length(aws_lambda_function.backend.layers) == 0 && !contains(keys(nonsensitive(aws_lambda_function.backend.environment[0].variables)), "OTEL_EXPORTER_OTLP_ENDPOINT")
    error_message = "Without a collector layer, the function must run without it and without tracing."
  }
}

run "requires_a_timeout_over_the_deadline_margin" {
  command = plan

  variables {
    lambda_timeout_seconds = 2
  }

  expect_failures = [var.lambda_timeout_seconds]
}

run "keeps_the_timeout_under_cloudfronts" {
  command = plan

  variables {
    lambda_timeout_seconds = 51
  }

  expect_failures = [var.lambda_timeout_seconds]
}

run "requires_an_alert_email" {
  command = plan

  variables {
    alert_email = "not an address"
  }

  expect_failures = [var.alert_email]
}


run "rejects_the_pool_size_in_lambda_settings" {
  command = plan

  variables {
    lambda_settings_file = "tests/fixtures/pool-setting.env"
  }

  expect_failures = [aws_lambda_function.backend]
}

run "rejects_malformed_lambda_settings" {
  command = plan

  variables {
    lambda_settings_file = "tests/fixtures/malformed.env"
  }

  expect_failures = [aws_lambda_function.backend]
}

run "requires_lambda_settings" {
  command = plan

  variables {
    lambda_settings_file = "tests/fixtures/missing.env"
  }

  expect_failures = [aws_lambda_function.backend]
}

run "creates_the_function_without_settings_on_the_first_apply" {
  command = plan

  variables {
    lambda_settings_file          = "tests/fixtures/missing.env"
    allow_missing_lambda_settings = true
  }

  assert {
    condition     = keys(nonsensitive(aws_lambda_function.backend.environment[0].variables)) == sort(concat(keys(local.lambda_terraform_settings), keys(local.collector_environment)))
    error_message = "Without lambda.env, the function only gets what Terraform sets."
  }
}

run "moves_the_static_ip_to_a_new_instance" {
  command = plan

  variables {
    vm_instances = {
      "island-battles-1" = {}
      "island-battles-2" = { bundle_id = "small_3_0" }
    }
    static_ip_instance = "island-battles-2"
  }

  assert {
    condition     = aws_lightsail_static_ip_attachment.vm.instance_name == "island-battles-2" && aws_lightsail_instance.vm["island-battles-2"].bundle_id == "small_3_0" && length(aws_lightsail_instance_public_ports.vm) == 2
    error_message = "A second instance must get its own plan and firewall, and can take the static IP."
  }
}

run "rejects_a_static_ip_instance_that_doesnt_exist" {
  command = plan

  variables {
    static_ip_instance = "island-battles-2"
  }

  expect_failures = [var.static_ip_instance]
}

# The GitHub deploy role ------------------------------------------------------

run "creates_no_github_role_by_default" {
  command = plan

  assert {
    condition     = length(aws_iam_role.github_deploy) == 0 && length(aws_iam_openid_connect_provider.github) == 0 && output.github_deploy_role_arn == null
    error_message = "Without github_repository, there must be no OIDC provider or deploy role."
  }
}

run "lets_only_the_repositorys_production_jobs_deploy" {
  command = plan

  variables {
    github_repository = "INTODAN/island-battles-go"
    deploy_bucket     = "island-battles-deploy-123456789012"
  }

  override_resource {
    target          = aws_iam_openid_connect_provider.github
    override_during = plan
    values = {
      arn = "arn:aws:iam::123456789012:oidc-provider/token.actions.githubusercontent.com"
    }
  }

  override_resource {
    target          = aws_iam_role.lambda
    override_during = plan
    values = {
      arn = "arn:aws:iam::123456789012:role/island-battles-lambda"
    }
  }

  override_resource {
    target          = aws_s3_bucket.web_client
    override_during = plan
    values = {
      arn = "arn:aws:s3:::island-battles-web-client-123456789012"
    }
  }

  override_resource {
    target          = aws_cloudfront_distribution.game
    override_during = plan
    values = {
      arn = "arn:aws:cloudfront::123456789012:distribution/E1ABCDEFGHIJKL"
    }
  }

  override_data {
    target          = data.aws_caller_identity.current
    override_during = plan
    values = {
      account_id = "123456789012"
    }
  }

  assert {
    condition = jsondecode(aws_iam_role.github_deploy[0].assume_role_policy).Statement[0].Condition.StringEquals == {
      "token.actions.githubusercontent.com:aud" = "sts.amazonaws.com"
      "token.actions.githubusercontent.com:sub" = "repo:INTODAN/island-battles-go:environment:production"
    }
    error_message = "Only the repository's jobs in the production environment may assume the role."
  }

  assert {
    condition     = aws_iam_role_policy_attachment.github_deploy_read[0].policy_arn == "arn:aws:iam::aws:policy/ReadOnlyAccess"
    error_message = "The role must read the account, as every plan does."
  }

  assert {
    condition = toset(flatten([for statement in jsondecode(aws_iam_role_policy.github_deploy[0].policy).Statement : statement.Action])) == toset([
      "lambda:UpdateFunctionCode", "lambda:UpdateFunctionConfiguration", "lambda:TagResource", "lambda:UntagResource",
      "iam:PassRole", "s3:ListBucket", "s3:GetObject", "s3:PutObject", "s3:DeleteObject",
      "cloudfront:CreateInvalidation", "lightsail:GetInstanceAccessDetails",
    ])
    error_message = "The role may write only the function, the deploy bucket, the web client and a temporary VM key."
  }

  assert {
    condition     = one([for statement in jsondecode(aws_iam_role_policy.github_deploy[0].policy).Statement : statement.Resource if statement.Sid == "ListTheWebClientBucket"]) == "arn:aws:s3:::island-battles-web-client-123456789012"
    error_message = "The role may list the web client bucket."
  }

  assert {
    condition = (
      one([for statement in jsondecode(aws_iam_role_policy.github_deploy[0].policy).Statement : statement.Resource if statement.Sid == "UploadTheWebClient"]) == "arn:aws:s3:::island-battles-web-client-123456789012/play/*"
      && toset(one([for statement in jsondecode(aws_iam_role_policy.github_deploy[0].policy).Statement : statement.Action if statement.Sid == "UploadTheWebClient"])) == toset(["s3:PutObject"])
    )
    error_message = "The role may only put the web client's files under play/: nothing deletes them."
  }

  assert {
    condition     = one([for statement in jsondecode(aws_iam_role_policy.github_deploy[0].policy).Statement : statement.Resource if statement.Sid == "InvalidateTheWebClient"]) == "arn:aws:cloudfront::123456789012:distribution/E1ABCDEFGHIJKL"
    error_message = "The role may invalidate only the game's distribution."
  }

  assert {
    condition     = one([for statement in jsondecode(aws_iam_role_policy.github_deploy[0].policy).Statement : statement.Resource if statement.Sid == "PublishTheBackend"]) == "arn:aws:lambda:eu-central-1:123456789012:function:island-battles"
    error_message = "The role may only publish the backend function."
  }

  assert {
    condition = (
      one([for statement in jsondecode(aws_iam_role_policy.github_deploy[0].policy).Statement : statement.Resource if statement.Sid == "PublishTheBotRunners"]) == [
        "arn:aws:lambda:eu-central-1:123456789012:function:island-battles-bot-runner-native",
        "arn:aws:lambda:eu-central-1:123456789012:function:island-battles-bot-runner-python",
      ]
      && one([for statement in jsondecode(aws_iam_role_policy.github_deploy[0].policy).Statement : statement.Action if statement.Sid == "PublishTheBotRunners"]) == "lambda:UpdateFunctionCode"
    )
    error_message = "The role may update the bot runners' code, and nothing else of theirs."
  }

  assert {
    condition     = one([for statement in jsondecode(aws_iam_role_policy.github_deploy[0].policy).Statement : statement.Resource if statement.Sid == "ReleaseTheStateLock"]) == "arn:aws:s3:::island-battles-deploy-123456789012/terraform/terraform.tfstate.tflock"
    error_message = "The role may delete nothing but the state's lock."
  }
}

run "needs_the_deploy_bucket_for_the_github_role" {
  command = plan

  variables {
    github_repository = "INTODAN/island-battles-go"
  }

  expect_failures = [var.github_repository]
}

run "rejects_a_github_repository_that_isnt_owner_and_name" {
  command = plan

  variables {
    github_repository = "https://github.com/INTODAN/island-battles-go"
    deploy_bucket     = "island-battles-deploy-123456789012"
  }

  expect_failures = [var.github_repository]
}

# Database backups ---------------------------------------------------------------

run "backs_the_database_up_to_s3" {
  command = plan

  variables {
    github_repository = "INTODAN/island-battles-go"
    deploy_bucket     = "island-battles-deploy-123456789012"
  }

  override_data {
    target          = data.aws_caller_identity.current
    override_during = plan
    values = {
      account_id = "123456789012"
    }
  }

  override_resource {
    target          = aws_s3_bucket.backups
    override_during = plan
    values = {
      arn = "arn:aws:s3:::island-battles-backups-123456789012"
    }
  }

  override_resource {
    target          = aws_iam_role.github_deploy
    override_during = plan
    values = {
      arn = "arn:aws:iam::123456789012:role/island-battles-github-deploy"
    }
  }

  assert {
    condition     = aws_s3_bucket.backups.bucket == "island-battles-backups-123456789012" && nonsensitive(output.database_backups.bucket) == aws_s3_bucket.backups.bucket && nonsensitive(output.database_backups.region) == "eu-central-1"
    error_message = "The backups bucket must be named after the account, in the deployment's region."
  }

  assert {
    condition = (
      aws_s3_bucket_public_access_block.backups.block_public_acls && aws_s3_bucket_public_access_block.backups.block_public_policy &&
      aws_s3_bucket_public_access_block.backups.ignore_public_acls && aws_s3_bucket_public_access_block.backups.restrict_public_buckets
    )
    error_message = "The backups bucket must never be public."
  }

  assert {
    condition = (
      aws_s3_bucket_versioning.backups.versioning_configuration[0].status == "Enabled" &&
      aws_s3_bucket_lifecycle_configuration.backups.rule[0].expiration[0].days == 7 &&
      aws_s3_bucket_lifecycle_configuration.backups.rule[0].noncurrent_version_expiration[0].noncurrent_days == 7
    )
    error_message = "Each backup, and each overwritten one, must be kept for 7 days by default."
  }

  assert {
    condition = jsondecode(aws_iam_user_policy.backups.policy).Statement == [
      {
        Sid      = "UploadDumps"
        Effect   = "Allow"
        Action   = "s3:PutObject"
        Resource = "arn:aws:s3:::island-battles-backups-123456789012/postgres/*"
      },
      {
        Sid       = "CountBackups"
        Effect    = "Allow"
        Action    = "cloudwatch:PutMetricData"
        Resource  = "*"
        Condition = { StringEquals = { "cloudwatch:namespace" = "IslandBattles/VM" } }
      },
    ]
    error_message = "The VM's key may only upload dumps and publish the backup metric: no reading, listing or deleting backups."
  }

  assert {
    condition = (
      one([for statement in jsondecode(aws_s3_bucket_policy.backups.policy).Statement : statement.Condition if statement.Sid == "RequireTls"]) == { Bool = { "aws:SecureTransport" = "false" } } &&
      one([for statement in jsondecode(aws_s3_bucket_policy.backups.policy).Statement : statement.Condition.ArnEquals["aws:PrincipalArn"] if statement.Sid == "KeepDumpsFromGitHubRoles" && statement.Effect == "Deny"]) == [
        "arn:aws:iam::123456789012:role/island-battles-github-deploy",
        "arn:aws:iam::123456789012:role/island-battles-github-apply",
      ]
    )
    error_message = "The bucket must refuse plain HTTP, and keep the dumps from both GitHub roles."
  }

  assert {
    condition = (
      aws_cloudwatch_metric_alarm.database_backups.namespace == nonsensitive(output.database_backups.metric_namespace) &&
      aws_cloudwatch_metric_alarm.database_backups.metric_name == nonsensitive(output.database_backups.metric_name) &&
      aws_cloudwatch_metric_alarm.database_backups.period * aws_cloudwatch_metric_alarm.database_backups.evaluation_periods == 26 * 3600 &&
      aws_cloudwatch_metric_alarm.database_backups.datapoints_to_alarm == aws_cloudwatch_metric_alarm.database_backups.evaluation_periods &&
      aws_cloudwatch_metric_alarm.database_backups.threshold == 1 && aws_cloudwatch_metric_alarm.database_backups.comparison_operator == "LessThanThreshold" &&
      aws_cloudwatch_metric_alarm.database_backups.treat_missing_data == "breaching" && length(aws_cloudwatch_metric_alarm.database_backups.alarm_actions) == 1
    )
    error_message = "The backup alarm must email when the metric the VM publishes saw no backup for 26 hours."
  }
}

run "keeps_the_backups_without_a_github_role" {
  command = plan

  override_resource {
    target          = aws_s3_bucket.backups
    override_during = plan
    values = {
      arn = "arn:aws:s3:::island-battles-backups-123456789012"
    }
  }

  assert {
    condition     = [for statement in jsondecode(aws_s3_bucket_policy.backups.policy).Statement : statement.Sid] == ["RequireTls"]
    error_message = "Without the GitHub role, the bucket policy must only require TLS."
  }
}

run "rejects_backups_kept_under_a_day" {
  command = plan

  variables {
    database_backup_retention_days = 0
  }

  expect_failures = [var.database_backup_retention_days]
}

run "keeps_bot_packages_in_a_private_bucket_for_8_days" {
  command = plan

  # AWS assigns these on creation.
  override_resource {
    target          = aws_s3_bucket.bot_packages
    override_during = plan
    values = {
      arn = "arn:aws:s3:::island-battles-bot-packages"
    }
  }

  override_resource {
    target          = aws_iam_role.lambda
    override_during = plan
    values = {
      id  = "island-battles-lambda"
      arn = "arn:aws:iam::123456789012:role/island-battles-lambda"
    }
  }

  assert {
    condition     = aws_s3_bucket.bot_packages.bucket == "island-battles-bot-packages"
    error_message = "The bucket must be <name>-bot-packages, which the apply role may change."
  }

  assert {
    condition = (
      aws_s3_bucket_public_access_block.bot_packages.block_public_acls &&
      aws_s3_bucket_public_access_block.bot_packages.block_public_policy &&
      aws_s3_bucket_public_access_block.bot_packages.ignore_public_acls &&
      aws_s3_bucket_public_access_block.bot_packages.restrict_public_buckets &&
      one(one(aws_s3_bucket_server_side_encryption_configuration.bot_packages.rule).apply_server_side_encryption_by_default).sse_algorithm == "AES256" &&
      one(aws_s3_bucket_ownership_controls.bot_packages.rule).object_ownership == "BucketOwnerEnforced"
    )
    error_message = "The bucket must block all public access, encrypt objects with SSE-S3 and have no ACLs."
  }

  assert {
    condition = (
      one(aws_s3_bucket_lifecycle_configuration.bot_packages.rule).status == "Enabled" &&
      one(one(aws_s3_bucket_lifecycle_configuration.bot_packages.rule).expiration).days == 8
    )
    error_message = "Every package must be deleted 8 days after its upload."
  }

  assert {
    condition = (
      toset(one(aws_s3_bucket_cors_configuration.bot_packages.cors_rule).allowed_methods) == toset(["POST"]) &&
      toset(one(aws_s3_bucket_cors_configuration.bot_packages.cors_rule).allowed_origins) == toset(["https://island-battles.example.org"])
    )
    error_message = "Browsers must upload only with POST, and only from the game's origin, where the lobby page is too."
  }

  assert {
    condition = (
      [for statement in jsondecode(aws_iam_role_policy.lambda_bot_packages.policy).Statement : statement.Resource] == ["arn:aws:s3:::island-battles-bot-packages/*"] &&
      toset(one(jsondecode(aws_iam_role_policy.lambda_bot_packages.policy).Statement).Action) == toset(["s3:PutObject", "s3:GetObject", "s3:DeleteObject"]) &&
      aws_iam_role_policy.lambda_bot_packages.role == aws_iam_role.lambda.id
    )
    error_message = "The backend's role must reach this bucket's objects only."
  }

  assert {
    condition     = [for statement in jsondecode(aws_s3_bucket_policy.bot_packages.policy).Statement : statement.Sid] == ["RequireTls"]
    error_message = "The bucket policy must require TLS."
  }
}

# Bot runners ------------------------------------------------------------------

run "runs_uploaded_bots_in_two_runner_functions" {
  command = plan

  override_resource {
    target          = aws_iam_role.bot_runner
    override_during = plan
    values = {
      id  = "island-battles-bot-runner"
      arn = "arn:aws:iam::123456789012:role/island-battles-bot-runner"
    }
  }

  override_resource {
    target          = aws_cloudwatch_log_group.bot_runner
    override_during = plan
    values = {
      arn = "arn:aws:logs:eu-central-1:123456789012:log-group:/aws/lambda/island-battles-bot-runner"
    }
  }

  assert {
    condition = (
      aws_lambda_function.bot_runner["python"].function_name == "island-battles-bot-runner-python" &&
      aws_lambda_function.bot_runner["python"].runtime == "python3.14" &&
      aws_lambda_function.bot_runner["python"].handler == "runner.handler" &&
      aws_lambda_function.bot_runner["native"].function_name == "island-battles-bot-runner-native" &&
      aws_lambda_function.bot_runner["native"].runtime == "provided.al2023" &&
      aws_lambda_function.bot_runner["native"].handler == "bootstrap"
    )
    error_message = "The Python runner must run runner.handler on python3.14, and the native runner a bootstrap on provided.al2023."
  }

  assert {
    condition = alltrue([for runner in aws_lambda_function.bot_runner : (
      runner.architectures == tolist(["arm64"]) &&
      runner.memory_size == 1024 &&
      runner.timeout == 20 &&
      one(runner.ephemeral_storage).size == 2048 &&
      runner.reserved_concurrent_executions == 10
    )])
    error_message = "Each runner must be arm64 with 1024 MB, a 20-second timeout, 2048 MB of /tmp and a reserved concurrency of 10."
  }

  assert {
    condition     = alltrue([for runner in aws_lambda_function.bot_runner : runner.environment[0].variables == tomap({ GAME_SUBMIT_HOST = "island-battles.example.org" })])
    error_message = "The runners must get the game's host to submit to, and no other setting."
  }

  assert {
    condition = alltrue([for kind, runner in aws_lambda_function.bot_runner : (
      one(runner.logging_config).log_group == "/aws/lambda/island-battles-bot-runner-${kind}" &&
      one(runner.logging_config).log_format == "JSON" &&
      one(runner.logging_config).application_log_level == "WARN" &&
      one(runner.logging_config).system_log_level == "WARN"
    )])
    error_message = "Each runner must log warnings and errors only, to its own log group."
  }

  assert {
    condition     = length(aws_cloudwatch_log_group.bot_runner) == 2 && alltrue([for group in aws_cloudwatch_log_group.bot_runner : group.retention_in_days == 7])
    error_message = "Each runner's log group must keep its logs for 7 days."
  }

  assert {
    condition = length(aws_lambda_function_event_invoke_config.bot_runner) == 2 && alltrue([for config in aws_lambda_function_event_invoke_config.bot_runner : (
      config.maximum_retry_attempts == 0 && config.maximum_event_age_in_seconds == 60
    )])
    error_message = "An asynchronous turn must never be retried, and must be dropped after a minute."
  }

  assert {
    condition = (
      aws_lambda_function.bot_runner["python"].role == "arn:aws:iam::123456789012:role/island-battles-bot-runner" &&
      aws_iam_role_policy.bot_runner_logs.role == "island-battles-bot-runner" &&
      [for statement in jsondecode(aws_iam_role_policy.bot_runner_logs.policy).Statement : statement.Action] == [["logs:CreateLogStream", "logs:PutLogEvents"]] &&
      length(one(jsondecode(aws_iam_role_policy.bot_runner_logs.policy).Statement).Resource) == 2
    )
    error_message = "The runners' role may write their own logs and nothing else: no S3, no other AWS access."
  }

  assert {
    condition     = jsondecode(aws_iam_role.bot_runner.assume_role_policy).Statement[0].Principal == { Service = "lambda.amazonaws.com" }
    error_message = "Only Lambda may assume the runners' role."
  }
}
