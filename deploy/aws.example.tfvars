# Settings for scripts/aws.sh. Copy this file to deploy/local/aws.tfvars, which
# is gitignored, and replace the placeholders. Every variable is described in
# deploy/terraform/variables.tf. See deploy/README.md.

# The AWS region for the VM and the Lambda.
region = "<region, e.g. eu-central-1>"

# The OpenSSH public key that logs in to the instance as `ubuntu`.
ssh_public_key = "<contents of your public key, e.g. ~/.ssh/id_ed25519.pub>"

# The Lightsail instances, by name, and the one the static IP points at.
# Moving to a new VM adds a second instance here (deploy/README.md). Keep this
# file in step with what runs: Terraform replaces or deletes any instance that
# isn't listed as it is.
vm_instances = {
  "island-battles-1" = {}
}
static_ip_instance = "island-battles-1"

# Where players open the game. CloudFront serves it with an ACM certificate,
# and its DNS records are added by hand (deploy/README.md).
game_hostname = "island-battles.<your-domain>"

# Where the error alarms and the zero-spend budget send their emails.
alert_email = "<your email address>"

# Optional: where the $5 monthly cost budget emails at 80 % of actual costs.
# Unset, it emails alert_email.
# budget_email = "<your email address>"

# Optional: let the repository's Deploy workflow deploy through an IAM role
# that GitHub Actions assumes with OIDC (deploy/README.md).
# github_repository = "<owner>/<name>"

# Optional: how many Lambda instances may run at once. Every one keeps up to
# database_max_open_connections (2) connections to Postgres, and together they
# must leave postgres_spare_connections (10) of Postgres's 100 free. Every one
# also logs and traces up to its own limits, which must fit the free tier for
# all of them (deploy/README.md). Set it to null if the account's concurrency
# limit is too low to reserve any, and set account_concurrency_limit to that
# limit. Look the limit up again before each apply: AWS raises it on its own.
# reserved_concurrency      = 5
# account_concurrency_limit = 10

# Optional: each instance's log and trace limits. The defaults fit 5
# instances. For 10, such as an unreserved account limit of 10, use 2 lines
# a minute and a trace every 360 seconds.
# log_lines_per_minute   = 5
# log_line_bytes         = 1024
# trace_interval_seconds = 180

# Optional: further, non-secret environment variables for the Lambda.
# extra_lambda_environment = {
#   ISLAND_BATTLES_DATABASE_CONNECTION_MAX_IDLE_TIME = "1m"
# }
