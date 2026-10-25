#!/usr/bin/env bash
# Create the AWS resources with Terraform, or deploy a new backend version to
# Lambda. See deploy/README.md.
set -euo pipefail
repository_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
terraform_directory="$repository_root/deploy/terraform"
local_directory="$repository_root/deploy/local"
variables_file="$local_directory/aws.tfvars"
lambda_settings_file="$local_directory/lambda.env"
build_directory="$local_directory/lambda"
plan_file="$local_directory/terraform/scripts-aws.tfplan"

usage() {
  cat >&2 <<'EOF'
Usage:
  scripts/aws.sh apply [terraform plan options]
      Build the backend, then create or update every AWS resource: the
      Lightsail VM, its static IP and firewall, and the Lambda. Shows the plan
      and asks first. Runs the migrations before publishing new code, once the
      VM deploy has written deploy/local/lambda.env. The very first apply
      needs -var allow_missing_lambda_settings=true.
  scripts/aws.sh deploy
      Build the backend, run the migrations against Postgres and publish the
      new code and deploy/local/lambda.env's settings to the Lambda. Doesn't
      ask, and refuses a plan that changes Lightsail resources or deletes
      anything. Never touches the VM.
  scripts/aws.sh output [name]
      Print Terraform's outputs, or one of them, e.g. static_ip or
      function_url.
EOF
  exit 2
}

if [[ $# -lt 1 ]]; then
  usage
fi
command="$1"
shift

require_command() {
  if ! command -v "$1" >/dev/null; then
    echo "Install $1 first. See deploy/README.md." >&2
    exit 1
  fi
}

run_terraform() {
  terraform -chdir="$terraform_directory" "$@"
}

initialize_terraform() {
  require_command terraform
  mkdir -p "$local_directory/terraform"
  # .terraform/terraform.tfstate records the backend; `init -backend=false`
  # (for validating) leaves it out.
  if [[ ! -f "$terraform_directory/.terraform/terraform.tfstate" ]]; then
    run_terraform init -input=false
  fi
}

require_variables() {
  if [[ ! -f "$variables_file" ]]; then
    echo "Copy deploy/aws.example.tfvars to deploy/local/aws.tfvars and fill it in." >&2
    exit 1
  fi
}

# The build command from backend/README.md. Terraform zips the result.
build_backend() {
  require_command go
  mkdir -p "$build_directory"
  echo "Building the backend for Lambda..."
  (cd "$repository_root" && GOOS=linux GOARCH=arm64 CGO_ENABLED=0 \
    go build -tags lambda.norpc -trimpath -ldflags='-s -w' -o "$build_directory/bootstrap" ./backend/cmd/lambda)
}

# Reads one KEY=value line from lambda.env without evaluating it as shell.
lambda_setting() {
  sed -n "s/^$1=//p" "$lambda_settings_file" | tail -n 1
}

# Migrations run from this machine, straight against Postgres over TLS, before
# the new code is published. A cold start runs no DDL.
migrate_database() {
  local database_url
  database_url="$(lambda_setting ISLAND_BATTLES_DATABASE_URL)"
  if [[ -z "$database_url" ]]; then
    echo "$lambda_settings_file has no ISLAND_BATTLES_DATABASE_URL. Run the VM deploy again to rewrite it." >&2
    exit 1
  fi
  echo "Migrating the database..."
  (cd "$repository_root" && ISLAND_BATTLES_DATABASE_URL="$database_url" go run ./backend/cmd/migrate)
}

check_plan() {
  run_terraform show -json "$plan_file" | python3 "$repository_root/deploy/check_deploy_plan.py" "$1"
}

# Plans, checks the plan, migrates and applies exactly the plan that was
# checked. The plan file holds the Lambda's settings, so it's removed after.
plan_and_apply() {
  local mode="$1"
  shift
  initialize_terraform
  require_variables
  build_backend
  require_command python3
  trap 'rm -f "$plan_file"' EXIT
  run_terraform plan -input=false -var-file="$variables_file" -out="$plan_file" "$@"

  local migrate=yes
  if [[ "$mode" == deploy ]]; then
    check_plan deploy
  else
    local answer
    read -r -p "Apply this plan? Only 'yes' applies it: " answer
    if [[ "$answer" != yes ]]; then
      echo "Nothing applied." >&2
      exit 1
    fi
    # Only migrate when this apply publishes the backend. Moving the static IP
    # in the middle of a move must work while the old VM refuses the backend.
    migrate="$(check_plan publishes-backend)"
  fi

  if [[ "$migrate" == yes ]]; then
    if [[ -f "$lambda_settings_file" ]]; then
      migrate_database
    else
      echo "No $lambda_settings_file yet, so no migrations: the Lambda gets its settings on the first scripts/aws.sh deploy."
    fi
  fi
  run_terraform apply -input=false "$plan_file"
  echo "The backend is at $(run_terraform output -raw function_url)"
}

case "$command" in
  apply)
    plan_and_apply apply "$@"
    ;;
  deploy)
    [[ $# -eq 0 ]] || usage
    if [[ ! -f "$lambda_settings_file" ]]; then
      echo "$lambda_settings_file is missing. Copy deploy/local/ from where you keep it, or run scripts/vm.sh deploy, which writes it." >&2
      exit 1
    fi
    plan_and_apply deploy
    ;;
  output)
    [[ $# -le 1 ]] || usage
    initialize_terraform
    if [[ $# -eq 1 ]]; then
      run_terraform output -raw "$1"
    else
      run_terraform output
    fi
    ;;
  *)
    usage
    ;;
esac
