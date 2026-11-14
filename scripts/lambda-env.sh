#!/usr/bin/env bash
# Write deploy/local/lambda.env, the backend Lambda's settings, from what
# INTODAN/mpl publishes for Island Battles, its tenant, and this repository's
# own secrets. The VM is INTODAN/mpl's (#232); nothing here contacts it. See
# deploy/README.md, "The backend's settings".
set -euo pipefail
repository_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
ansible_directory="$repository_root/deploy/ansible"

usage() {
  cat >&2 <<'EOF_USAGE'
Pulls deploy/local/ from the deploy bucket first and pushes it back at the
end.

Usage:
  scripts/lambda-env.sh [ansible-playbook options]
      Copy INTODAN/mpl's settings for the tenant, the deploy bucket's
      mpl/tenants/island-battles/tenant.env, to deploy/local/tenant.env, and
      write deploy/local/lambda.env from them and deploy/local/secrets/. Run
      it once INTODAN/mpl's VM deploy has published them, and again whenever
      it changes them; then scripts/aws.sh deploy publishes the settings. A new
      deployment's first run needs -e app_generate_secrets=true, which creates
      this repository's own secrets.
EOF_USAGE
  exit 2
}

[[ "${1:-}" != -h && "${1:-}" != --help ]] || usage

deploy_files() {
  python3 -I "$repository_root/deploy/deploy_files.py" "$@"
}

# Pushes what the run changed, such as lambda.env or new secrets, even when it
# failed half way, and keeps the run's own exit status.
finish() {
  local status=$?
  if ! deploy_files push; then
    echo "deploy/local/ isn't in the deploy bucket yet. Run scripts/deploy-files.sh push." >&2
    [[ $status -ne 0 ]] || status=1
  fi
  exit "$status"
}

for command in aws python3 ansible-playbook; do
  if ! command -v "$command" >/dev/null; then
    echo "Install $command first, e.g. 'uv tool install ansible-core' for Ansible. See deploy/README.md." >&2
    exit 1
  fi
done
deploy_files pull
trap finish EXIT
deploy_files tenant
# ansible.cfg is only read from the current directory.
(cd "$ansible_directory" && ansible-playbook lambda-env.yml "$@")
echo "Wrote deploy/local/lambda.env. Publish it with scripts/aws.sh deploy."
