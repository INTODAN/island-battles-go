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
}

run "passes_every_lambda_setting" {
  command = plan

  assert {
    condition = nonsensitive(aws_lambda_function.backend.environment[0].variables) == tomap({
      ISLAND_BATTLES_DATABASE_URL                  = "postgres://island_battles:not-a-secret@vm.example.org:5432/island_battles?sslmode=require"
      ISLAND_BATTLES_CENTRIFUGO_PUBLIC_URL         = "wss://vm.example.org/connection/websocket"
      ISLAND_BATTLES_CENTRIFUGO_API_URL            = "https://vm.example.org/api"
      ISLAND_BATTLES_CENTRIFUGO_API_KEY            = "not-a-secret"
      ISLAND_BATTLES_CONNECTION_TOKEN_HMAC_SECRET  = "not-a-secret"
      ISLAND_BATTLES_LOBBY_PROXY_SECRET            = "not-a-secret"
      ISLAND_BATTLES_SETTING_TERRAFORM_DOESNT_KNOW = "a=b"
      ISLAND_BATTLES_DATABASE_MAX_OPEN_CONNECTIONS = "2"
    })
    error_message = "The function must get every lambda.env setting, the last value of a repeated key, and the pool size."
  }

  assert {
    condition     = aws_lambda_function.backend.runtime == "provided.al2023" && aws_lambda_function.backend.architectures == tolist(["arm64"]) && aws_lambda_function.backend.handler == "bootstrap"
    error_message = "The function must run the arm64 bootstrap on provided.al2023."
  }

  assert {
    condition     = aws_lambda_function.backend.reserved_concurrent_executions == 20
    error_message = "The default reserved concurrency must be 20."
  }

  assert {
    condition     = aws_lambda_function_url.backend.authorization_type == "NONE" && aws_lambda_function_url.backend.invoke_mode == "RESPONSE_STREAM"
    error_message = "The Function URL must be public and streamed."
  }

  assert {
    condition     = aws_iam_role_policy_attachment.lambda_logs.policy_arn == "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
    error_message = "The role must have basic execution only."
  }

  assert {
    condition     = toset([for port in aws_lightsail_instance_public_ports.vm["island-battles-1"].port_info : port.from_port]) == toset([22, 443, 5432])
    error_message = "The firewall must open 22, 443 and Postgres, and nothing else."
  }

  assert {
    condition     = one([for add_on in aws_lightsail_instance.vm["island-battles-1"].add_on : add_on if add_on.type == "AutoSnapshot" && add_on.status == "Enabled"]) != null
    error_message = "The instance must have automatic snapshots."
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
    # 46 × 2 = 92 leaves 8 of 100 free, fewer than the 10 spare connections.
    reserved_concurrency = 46
  }

  expect_failures = [aws_lambda_function.backend]
}

run "allows_the_largest_safe_concurrency" {
  command = plan

  variables {
    reserved_concurrency = 45
  }

  assert {
    condition     = aws_lambda_function.backend.reserved_concurrent_executions == 45
    error_message = "45 × 2 = 90 leaves exactly the 10 spare connections."
  }
}

run "leaves_concurrency_unreserved_when_asked" {
  command = plan

  variables {
    reserved_concurrency = null
  }

  assert {
    condition     = aws_lambda_function.backend.reserved_concurrent_executions == -1
    error_message = "A null reserved_concurrency must leave the function unreserved."
  }
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
    condition     = nonsensitive(aws_lambda_function.backend.environment[0].variables) == tomap({ ISLAND_BATTLES_DATABASE_MAX_OPEN_CONNECTIONS = "2" })
    error_message = "Without lambda.env, the function only gets the pool size."
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
