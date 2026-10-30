# Settings come from deploy/local/aws.tfvars, which scripts/aws.sh passes with
# -var-file. See deploy/aws.example.tfvars and deploy/README.md.

variable "region" {
  description = "The AWS region for the VM and the Lambda."
  type        = string

  validation {
    condition     = can(regex("^[a-z]{2}(-[a-z]+)+-[0-9]+$", var.region))
    error_message = "region must be an AWS region, such as eu-central-1."
  }
}

variable "name" {
  description = "Prefix for the names of the Lambda, its role, the static IP and the key pair."
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

# The VM (#26, #32) ------------------------------------------------------------

variable "vm_instances" {
  description = <<-EOT
    The Lightsail instances, by name. Normally one. Moving to a new VM adds a
    second one next to it, moves the static IP and then removes the old one
    (deploy/README.md). Changing an existing instance's zone, image or plan
    replaces it and deletes its disk, so add a new instance instead. There's
    no default, so a lost or stale aws.tfvars can't plan to replace the live
    instance with a default one.
  EOT
  type = map(object({
    # Defaults to the region's "a" zone.
    availability_zone = optional(string)
    # Ubuntu 24.04 LTS. The VM deploy needs 24.04 or newer.
    blueprint_id = optional(string, "ubuntu_24_04")
    # 1 GB of memory, 2 vCPUs, 40 GB of disk and a public IPv4 address.
    bundle_id = optional(string, "micro_3_0")
  }))

  validation {
    condition     = length(var.vm_instances) > 0
    error_message = "List at least one instance in vm_instances."
  }
}

variable "static_ip_instance" {
  description = "The instance in vm_instances the static IP, and so vm.<domain>, points at."
  type        = string

  validation {
    condition     = contains(keys(var.vm_instances), var.static_ip_instance)
    error_message = "static_ip_instance must name an instance in vm_instances."
  }
}

variable "ssh_public_key" {
  description = <<-EOT
    An OpenSSH public key for the instances' `ubuntu` user, e.g. the contents of
    ~/.ssh/id_ed25519.pub. Unset, Lightsail uses the region's default key pair,
    whose private key you download from the Lightsail console. Only new
    instances pick up a change.
  EOT
  type        = string
  default     = null

  validation {
    condition     = var.ssh_public_key == null || can(regex("^(ssh-|ecdsa-)", var.ssh_public_key))
    error_message = "ssh_public_key must be an OpenSSH public key, starting with ssh-ed25519, ssh-rsa or ecdsa-."
  }
}

variable "ssh_allowed_cidrs" {
  description = "IPv4 ranges that may reach SSH (22). Limit it to your own address if you can, e.g. [\"198.51.100.7/32\"]."
  type        = list(string)
  default     = ["0.0.0.0/0"]
}

variable "postgres_port" {
  description = "Postgres's public port. It must match the VM deploy's postgres_port."
  type        = number
  default     = 5432
}

variable "snapshot_time" {
  description = "When Lightsail takes the daily automatic snapshot (#33), in UTC, on the hour."
  type        = string
  default     = "01:00"

  validation {
    condition     = can(regex("^([01][0-9]|2[0-3]):00$", var.snapshot_time))
    error_message = "snapshot_time must be an hour in UTC, such as \"01:00\"."
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
    so the product must leave postgres_spare_connections free under
    postgres_max_connections. Every instance also writes and traces up to its
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
    reserved_concurrency is null, so Terraform checks it against Postgres's
    connections and the free-tier budget instead. AWS raises new accounts'
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

variable "postgres_max_connections" {
  description = "Postgres's max_connections on the VM. The VM deploy keeps Postgres's default of 100."
  type        = number
  default     = 100
}

variable "postgres_spare_connections" {
  description = <<-EOT
    Connections the Lambda must leave free: 3 that Postgres reserves for
    superusers, 2 for the migrate command, and some for administration.
  EOT
  type        = number
  default     = 10

  validation {
    condition     = var.postgres_spare_connections >= 5
    error_message = "postgres_spare_connections must be at least 5: 3 for superusers and 2 for the migrate command."
  }
}

variable "lambda_settings_file" {
  description = <<-EOT
    The Lambda's settings, which the VM deploy writes. Every KEY=value line is
    passed to the function as an environment variable. Defaults to
    deploy/local/lambda.env.
  EOT
  type        = string
  default     = null
}

variable "allow_missing_lambda_settings" {
  description = <<-EOT
    Create the function without lambda_settings_file. Only for the very first
    apply, before the VM deploy has written it: the function then fails every
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
