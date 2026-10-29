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

# Optional: limit SSH to your own address.
# ssh_allowed_cidrs = ["<your IP>/32"]

# Optional: how many Lambda instances may run at once. Every one keeps up to
# database_max_open_connections (2) connections to Postgres, and together they
# must leave postgres_spare_connections (10) of Postgres's 100 free. Set it to
# null if the account's concurrency limit is too low to reserve any, and set
# account_concurrency_limit to that limit. Look the limit up again before each
# apply: AWS raises it on its own.
# reserved_concurrency      = 20
# account_concurrency_limit = 10

# Optional: further, non-secret environment variables for the Lambda.
# extra_lambda_environment = {
#   ISLAND_BATTLES_DATABASE_CONNECTION_MAX_IDLE_TIME = "1m"
# }
