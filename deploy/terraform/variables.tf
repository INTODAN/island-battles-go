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

# The VM (#26, #32) ------------------------------------------------------------

variable "vm_instances" {
  description = <<-EOT
    The Lightsail instances, by name. Normally one. Moving to a new VM adds a
    second one next to it, moves the static IP and then removes the old one
    (deploy/README.md). Changing an existing instance's zone, image or plan
    replaces it and deletes its disk, so add a new instance instead.
  EOT
  type = map(object({
    # Defaults to the region's "a" zone.
    availability_zone = optional(string)
    # Ubuntu 24.04 LTS. The VM deploy needs 24.04 or newer.
    blueprint_id = optional(string, "ubuntu_24_04")
    # 1 GB of memory, 2 vCPUs, 40 GB of disk and a public IPv4 address.
    bundle_id = optional(string, "micro_3_0")
  }))
  default = {
    "island-battles-1" = {}
  }

  validation {
    condition     = length(var.vm_instances) > 0
    error_message = "List at least one instance in vm_instances."
  }
}

variable "static_ip_instance" {
  description = "The instance in vm_instances the static IP, and so vm.<domain>, points at."
  type        = string
  default     = "island-battles-1"

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
  description = "How long one request may run."
  type        = number
  default     = 30
}

variable "reserved_concurrency" {
  description = <<-EOT
    The most Lambda instances that run at once. Every instance keeps its own
    pool of up to database_max_open_connections, so the product must leave
    postgres_spare_connections free under postgres_max_connections. Set it to
    null only on an account whose concurrency limit is too low to reserve any
    (deploy/README.md); the account limit is then the cap.
  EOT
  type        = number
  default     = 20
  nullable    = true

  validation {
    condition     = var.reserved_concurrency == null || try(var.reserved_concurrency >= 1, false)
    error_message = "reserved_concurrency must be at least 1, or null. 0 would stop the function."
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
