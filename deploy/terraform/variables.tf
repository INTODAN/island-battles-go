# Settings come from deploy/local/aws.tfvars, which scripts/aws.sh passes with
# -var-file. See deploy/aws.example.tfvars and deploy/README.md.

variable "region" {
  description = "The AWS region for the Lambda."
  type        = string

  validation {
    condition     = can(regex("^[a-z]{2}(-[a-z]+)+-[0-9]+$", var.region))
    error_message = "region must be an AWS region, such as eu-central-1."
  }
}

variable "name" {
  description = "Prefix for the names of the Lambda, its role and the other resources."
  type        = string
  default     = "island-battles"
}

variable "game_hostname" {
  description = <<-EOT
    The name players open the game at, such as island-battles.example.org.
    CloudFront serves it from the Function URL, with an ACM certificate. Its
    CNAME and the certificate's validation record are added to the domain's
    DNS by hand (deploy/README.md).
  EOT
  type        = string

  validation {
    condition     = can(regex("^([a-z0-9]([a-z0-9-]*[a-z0-9])?\\.)+[a-z]{2,}$", var.game_hostname))
    error_message = "game_hostname must be a lowercase hostname, such as island-battles.example.org."
  }
}

variable "lobby_hostname" {
  description = <<-EOT
    The lobby's name, such as lobby.example.org, where INTODAN/mpl deploys the
    lobby (#232). Old links to the lobby's page on game_hostname redirect
    there (cloudfront.tf), the bot packages' bucket admits uploads from the
    lobby page there (bot_packages.tf), and the backend answers its origin
    with CORS (lambda.tf). The lobby's URL in lambda.env must be the same.
  EOT
  type        = string

  validation {
    condition     = can(regex("^([a-z0-9]([a-z0-9-]*[a-z0-9])?\\.)+[a-z]{2,}$", var.lobby_hostname))
    error_message = "lobby_hostname must be a lowercase hostname, such as lobby.example.org."
  }
}

# Database backups ---------------------------------------------------------------

variable "database_backup_retention_days" {
  description = "How many days S3 keeps each daily database backup (backups.tf), and each overwritten one."
  type        = number
  default     = 7

  validation {
    condition     = var.database_backup_retention_days >= 1 && var.database_backup_retention_days == floor(var.database_backup_retention_days)
    error_message = "database_backup_retention_days must be a whole number of days, at least 1."
  }
}

variable "backup_denied_roles" {
  description = <<-EOT
    Names of further IAM roles that may never download a database backup
    (backups.tf), whose ReadOnlyAccess would otherwise let them read the dumps.
    By default INTODAN/mpl's GitHub roles, which share the account (#256).
  EOT
  type        = list(string)
  default     = ["mpl-github-deploy", "mpl-github-apply"]

  validation {
    condition     = alltrue([for role in var.backup_denied_roles : can(regex("^[A-Za-z0-9+=,.@_-]{1,64}$", role))])
    error_message = "backup_denied_roles must hold IAM role names, such as mpl-github-deploy, not ARNs."
  }
}

# The Lambda (#38) -------------------------------------------------------------

variable "lambda_memory_mb" {
  description = "The Lambda's memory. CPU scales with it."
  type        = number
  default     = 512
}

variable "lambda_timeout_seconds" {
  description = "How long one request may run. The backend ends a request's work 2 seconds before it (backend/cmd/lambda), so a timeout doesn't reset the instance. At most 50, so a request and a cold start fit in CloudFront's 60-second origin timeout (cloudfront.tf)."
  type        = number
  default     = 30

  validation {
    condition     = var.lambda_timeout_seconds > 2 && var.lambda_timeout_seconds <= 50
    error_message = "lambda_timeout_seconds must be more than the backend's 2-second deadline margin, and at most 50, so CloudFront's 60-second origin timeout doesn't cut a request short and retry it (cloudfront.tf)."
  }
}

variable "reserved_concurrency" {
  description = <<-EOT
    The most Lambda instances that run at once, C in the free-tier budget.
    Every instance keeps its own pool of up to database_max_open_connections,
    so the product must fit tenant_database_connections. Every instance also
    writes and traces up to its
    own limits, so C also bounds the month's logs and traces (deploy/README.md).
    Set it to null only on an account whose concurrency limit is too low to
    reserve any. The account limit, account_concurrency_limit, is then the cap.
  EOT
  type        = number
  default     = 5
  nullable    = true

  validation {
    condition     = var.reserved_concurrency == null || try(var.reserved_concurrency >= 1, false)
    error_message = "reserved_concurrency must be at least 1, or null. 0 would stop the function."
  }
}

variable "account_concurrency_limit" {
  description = <<-EOT
    The account's Lambda concurrency limit in this region: "Concurrent
    executions" in Service Quotas. It caps the function when
    reserved_concurrency is null, so Terraform checks it against the
    database connections and the free-tier budget instead. AWS raises new accounts'
    limit of 10 on its own, so look it up again before every apply while
    reserved_concurrency is null.
  EOT
  type        = number
  default     = 10

  validation {
    condition     = var.account_concurrency_limit >= 1
    error_message = "account_concurrency_limit must be at least 1."
  }
}

variable "database_max_open_connections" {
  description = "ISLAND_BATTLES_DATABASE_MAX_OPEN_CONNECTIONS for every Lambda instance (backend/README.md)."
  type        = number
  default     = 2

  validation {
    condition     = var.database_max_open_connections >= 1
    error_message = "database_max_open_connections must be at least 1."
  }
}

variable "tenant_database_connections" {
  description = <<-EOT
    The most connections the function's instances may open together on the
    VM's Postgres, which INTODAN/mpl runs and shares between the lobby and its
    tenants (#232). It must not exceed what INTODAN/mpl's Terraform reserves
    for Island Battles, its own tenant_database_connections (20).
  EOT
  type        = number
  default     = 20

  validation {
    condition     = var.tenant_database_connections >= 1
    error_message = "tenant_database_connections must be at least 1."
  }
}

variable "lambda_settings_file" {
  description = <<-EOT
    The Lambda's settings, which scripts/lambda-env.sh writes from
    INTODAN/mpl's settings for the tenant (deploy/README.md, "The backend's
    settings"). Every KEY=value line is
    passed to the function as an environment variable. Defaults to
    deploy/local/lambda.env.
  EOT
  type        = string
  default     = null
}

variable "allow_missing_lambda_settings" {
  description = <<-EOT
    Create the function without lambda_settings_file. Only for the very first
    apply, before scripts/lambda-env.sh has written it: the function then fails every
    request until scripts/aws.sh deploy publishes its settings.
  EOT
  type        = bool
  default     = false
}

variable "extra_lambda_environment" {
  description = "Further environment variables for the function, such as ISLAND_BATTLES_DATABASE_CONNECTION_MAX_IDLE_TIME. Keep secrets in lambda_settings_file instead."
  type        = map(string)
  default     = {}
}

variable "lambda_bootstrap_file" {
  description = "The built Lambda executable. Defaults to deploy/local/lambda/bootstrap, where scripts/aws.sh builds it."
  type        = string
  default     = null
}

# Observability within the free tier (#46) -------------------------------------

variable "alert_email" {
  description = <<-EOT
    Where the error alarms and the zero-spend budget send their emails. Set it
    in deploy/local/aws.tfvars and never commit it. Confirm the subscription
    email that SNS sends after the first apply, or the alarms send nothing.
  EOT
  type        = string

  validation {
    condition     = can(regex("^[^@[:space:]]+@[^@[:space:]]+\\.[^@[:space:]]+$", var.alert_email))
    error_message = "alert_email must be an email address."
  }
}

variable "budget_email" {
  description = <<-EOT
    Where the $5 monthly cost budget emails once actual costs pass 80 % of it
    (observability.tf, #226). Unset, it emails alert_email. Set it in
    deploy/local/aws.tfvars and never commit it. Budgets emails directly,
    without a confirmation.
  EOT
  type        = string
  default     = null
  nullable    = true

  validation {
    condition     = var.budget_email == null || can(regex("^[^@[:space:]]+@[^@[:space:]]+\\.[^@[:space:]]+$", var.budget_email))
    error_message = "budget_email must be an email address, or unset for alert_email."
  }
}

variable "log_lines_per_minute" {
  description = <<-EOT
    N: the most warning and error lines one Lambda instance writes a minute.
    Warnings may use N-1 of them. The lines over the limit become one line
    that counts them. Terraform checks it, with the concurrency cap, against
    the 1.5 GB a month that logs may take (deploy/README.md).
  EOT
  type        = number
  default     = 5

  validation {
    condition     = var.log_lines_per_minute >= 2 && floor(var.log_lines_per_minute) == var.log_lines_per_minute
    error_message = "log_lines_per_minute must be a whole number of at least 2, so an error always has room."
  }
}

variable "log_line_bytes" {
  description = "L: the most bytes of one log line. Longer lines are cut."
  type        = number
  default     = 1024

  validation {
    condition     = var.log_line_bytes >= 512 && floor(var.log_line_bytes) == var.log_line_bytes
    error_message = "log_line_bytes must be a whole number of at least 512."
  }
}

variable "trace_interval_seconds" {
  description = <<-EOT
    T: one Lambda instance starts at most one new trace every T seconds, and
    its first one T seconds after it starts. Terraform checks it, with the
    concurrency cap, against the 80,000 traces a month that X-Ray may record.
  EOT
  type        = number
  default     = 180

  validation {
    condition     = var.trace_interval_seconds >= 1 && floor(var.trace_interval_seconds) == var.trace_interval_seconds
    error_message = "trace_interval_seconds must be a whole number of at least 1."
  }
}

variable "collector_layer_arn" {
  description = <<-EOT
    The OpenTelemetry collector layer that sends the backend's spans to X-Ray.
    Defaults to the AWS Distro for OpenTelemetry collector layer for arm64,
    ver-0-156-0 (ADOT Collector v0.49.0), in var.region (deploy/README.md).
    An empty string runs the function without the layer, and so without
    tracing.
  EOT
  type        = string
  default     = null
}

variable "deploy_bucket" {
  description = "The deploy bucket, which holds the state and deploy/local/'s files. scripts/aws.sh sets it from the account (deploy/deploy_files.py)."
  type        = string
  default     = null
}

variable "github_repository" {
  description = <<-EOT
    The GitHub repository, as owner/name, whose Deploy workflow may deploy
    through an IAM role it assumes with OIDC (github.tf). Null creates no role.
  EOT
  type        = string
  default     = null

  validation {
    condition     = var.github_repository == null || can(regex("^[A-Za-z0-9-]+/[A-Za-z0-9._-]+$", var.github_repository))
    error_message = "github_repository must be owner/name, such as INTODAN/island-battles-go."
  }

  validation {
    condition     = var.github_repository == null || var.deploy_bucket != null
    error_message = "The GitHub deploy role needs deploy_bucket, which scripts/aws.sh sets."
  }
}

variable "github_environment" {
  description = "The GitHub environment whose jobs may assume the deploy role. Limit it to the main branch in the repository's settings."
  type        = string
  default     = "production"
}
