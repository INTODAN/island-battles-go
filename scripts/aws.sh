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
local_state_file="$local_directory/terraform/terraform.tfstate"
backup_settings_file="$local_directory/secrets/database_backups.json"

usage() {
  cat >&2 <<'EOF'
Terraform's state is in the deploy bucket. apply and deploy pull deploy/local/
from the bucket first and push it back at the end.

Usage:
  scripts/aws.sh apply [terraform plan options]
      Build the backend, then create or update every AWS resource: the
      Lightsail VM, its static IP and firewall, the Lambda, the game's
      certificate and CloudFront distribution, and the database backups'
      bucket and key. Shows the plan and asks first. Writes the backups' key
      to deploy/local/secrets/database_backups.json for the VM deploy.
      A plan that deletes or replaces an instance, the static IP, a disk or
      the distribution also needs each one's name typed, or listed in
      ISLAND_BATTLES_ALLOW_DESTROY, comma-separated. Runs the migrations before
      publishing new code, once the VM deploy has written
      deploy/local/lambda.env. The first apply with game_hostname set needs
      its certificate alone, -target=aws_acm_certificate.game, so its
      validation record can be added before the full apply waits for it. The
      very first full apply needs -var allow_missing_lambda_settings=true.
  scripts/aws.sh deploy
      Build the backend, run the migrations against Postgres and publish the
      new code and deploy/local/lambda.env's settings to the Lambda. Doesn't
      ask, and refuses a plan that changes Lightsail resources or the game's
      certificate or distribution, or deletes anything. Never touches the VM.
  scripts/aws.sh output [name]
      Print Terraform's outputs, or one of them, e.g. static_ip,
      function_url, game_url, certificate_validation_record or
      distribution_domain.
  scripts/aws.sh migrate-state
      Move Terraform's old local state, deploy/local/terraform/terraform.tfstate,
      into the deploy bucket. Once, on the machine that has it.
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

deploy_files() {
  python3 -I "$repository_root/deploy/deploy_files.py" "$@"
}

# The bucket and its region come from the AWS credentials, so a new machine
# needs nothing else. Terraform's GitHub deploy role is scoped to the bucket.
initialize_terraform() {
  require_command terraform
  require_command aws
  require_command python3
  mkdir -p "$local_directory/terraform"
  local bucket_and_region bucket region
  bucket_and_region="$(deploy_files bucket)"
  read -r bucket region <<<"$bucket_and_region"
  export TF_VAR_deploy_bucket="$bucket"
  # .terraform/terraform.tfstate records the backend and its bucket; `init
  # -backend=false` (for validating) leaves it out.
  if ! python3 -I -c 'import json, sys
backend = json.load(open(sys.argv[1])).get("backend") or {}
sys.exit(0 if backend.get("type") == "s3" and backend["config"].get("bucket") == sys.argv[2] else 1)' \
    "$terraform_directory/.terraform/terraform.tfstate" "$bucket" 2>/dev/null; then
    # To stderr, so "$(scripts/aws.sh output static_ip)" captures only the IP.
    run_terraform init -input=false -reconfigure \
      -backend-config="bucket=$bucket" -backend-config="region=$region" >&2
  fi
}

# A machine that still has the old local state must move it first, or it would
# plan against the bucket's state while its own copy goes stale.
require_migrated_state() {
  if [[ -s "$local_state_file" ]]; then
    echo "$local_state_file is Terraform's old local state. Run scripts/aws.sh migrate-state to move it into the deploy bucket." >&2
    exit 1
  fi
}

# Removes the plan, which holds the Lambda's settings, and pushes what changed
# in deploy/local/, keeping the run's own exit status.
finish() {
  local status=$?
  rm -f "$plan_file"
  if ! deploy_files push; then
    echo "deploy/local/ isn't in the deploy bucket yet. Run scripts/deploy-files.sh push." >&2
    [[ $status -ne 0 ]] || status=1
  fi
  exit "$status"
}

sync_deploy_files() {
  require_command aws
  require_command python3
  deploy_files pull
  trap finish EXIT
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

# Reads one KEY=value line from lambda.env without evaluating it as shell. Like
# Terraform (lambda.tf), it ignores spaces and a carriage return around the
# line, and keeps the last value of a repeated key.
lambda_setting() {
  sed -n -E "s/^[[:space:]]*$1=(.*[^[:space:]])?[[:space:]]*\$/\1/p" "$lambda_settings_file" | tail -n 1
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
  run_terraform show -json "$plan_file" | python3 -I "$repository_root/deploy/check_deploy_plan.py" "$1"
}

# Deleting or replacing an instance deletes its disk and everything written
# since the last backup, releasing the static IP gives vm.<domain>'s address away, and a
# new distribution has a new domain for the game's CNAME. A generic yes isn't
# enough for that: each one's name must be typed, or listed in
# ISLAND_BATTLES_ALLOW_DESTROY.
confirm_destruction() {
  local destroyed name answer
  local names=()
  destroyed="$(check_plan apply)"
  [[ -n "$destroyed" ]] || return 0
  while IFS= read -r name; do
    names+=("$name")
  done <<<"$destroyed"
  local allowed=",${ISLAND_BATTLES_ALLOW_DESTROY:-},"
  for name in "${names[@]}"; do
    if [[ "$allowed" == *",$name,"* ]]; then
      echo "ISLAND_BATTLES_ALLOW_DESTROY allows destroying $name." >&2
      continue
    fi
    read -r -p "Type $name to destroy it: " answer
    if [[ "$answer" != "$name" ]]; then
      echo "Nothing applied. Check deploy/local/aws.tfvars against the resources that run." >&2
      exit 1
    fi
  done
}

# The VM deploy installs the daily backup's bucket and upload-only key on the
# VM (deploy/README.md, "Backups"). Only Terraform's state has the key, so
# every apply copies it to deploy/local/secrets/, which the deploy bucket
# shares. An apply of the certificate alone has no such output yet.
save_backup_settings() {
  local settings
  if ! settings="$(run_terraform output -json database_backups 2>/dev/null)" || [[ -z "$settings" ]] || [[ "$settings" == null ]]; then
    return 0
  fi
  mkdir -p "$(dirname -- "$backup_settings_file")"
  (umask 077 && printf '%s\n' "$settings" >"$backup_settings_file")
}

# Plans, checks the plan, migrates and applies exactly the plan that was
# checked. The plan file holds the Lambda's settings, so finish removes it.
plan_and_apply() {
  local mode="$1"
  shift
  require_migrated_state
  initialize_terraform
  require_variables
  build_backend
  run_terraform plan -input=false -var-file="$variables_file" -out="$plan_file" "$@"

  local migrate=yes
  if [[ "$mode" == deploy ]]; then
    check_plan deploy
  else
    confirm_destruction
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
  save_backup_settings
  # The certificate's apply on its own leaves the rest for later.
  local function_url
  if function_url="$(run_terraform output -raw function_url 2>/dev/null)"; then
    echo "The backend is at $function_url"
  fi
  if run_terraform output -raw distribution_domain >/dev/null 2>&1; then
    echo "Players open the game at $(run_terraform output -raw game_url)"
  fi
}

case "$command" in
  apply)
    sync_deploy_files
    plan_and_apply apply "$@"
    ;;
  deploy)
    [[ $# -eq 0 ]] || usage
    sync_deploy_files
    if [[ ! -f "$lambda_settings_file" ]]; then
      echo "$lambda_settings_file is missing. Push deploy/local/ from the machine that has it, or run scripts/vm.sh deploy, which writes it." >&2
      exit 1
    fi
    plan_and_apply deploy
    ;;
  output)
    [[ $# -le 1 ]] || usage
    require_migrated_state
    initialize_terraform
    if [[ $# -eq 1 ]]; then
      run_terraform output -raw "$1"
    else
      run_terraform output
    fi
    ;;
  migrate-state)
    [[ $# -eq 0 ]] || usage
    if [[ ! -s "$local_state_file" ]]; then
      echo "There's no local state at $local_state_file to move." >&2
      exit 1
    fi
    initialize_terraform
    if [[ "$(deploy_files has-state)" == yes ]]; then
      echo "The deploy bucket already has Terraform's state. Compare it with $local_state_file before replacing anything." >&2
      exit 1
    fi
    run_terraform state push "$local_state_file"
    mv "$local_state_file" "$local_state_file.migrated"
    echo "Moved the state into the deploy bucket, and kept the old file as $local_state_file.migrated."
    ;;
  *)
    usage
    ;;
esac
