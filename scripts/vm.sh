#!/usr/bin/env bash
# Deploy Postgres and Centrifugo to the VM with Ansible, move its database to a
# new VM, or smoke-test the deployment. See deploy/README.md.
set -euo pipefail
repository_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
ansible_directory="$repository_root/deploy/ansible"
local_directory="$repository_root/deploy/local"
settings_file="$local_directory/vm.yml"

usage() {
  cat >&2 <<'EOF'
Usage:
  scripts/vm.sh deploy <ip> [ansible-playbook options]
      Provision the VM, or update it. Safe to re-run. The first deploy needs
      -e app_generate_secrets=true to create deploy/local/secrets/.
  scripts/vm.sh dump <ip> [ansible-playbook options]
      Dump the VM's database to deploy/local/dumps/.
  scripts/vm.sh restore <ip> <dump file> [ansible-playbook options]
      Restore a dump into the VM's empty database.
  scripts/vm.sh move-data <old ip> <new ip> [ansible-playbook options]
      Stop the backend from writing to the old VM, dump its database and
      restore it into the new VM.
  scripts/vm.sh smoke
      Check CloudFront, the Lambda, Postgres and Centrifugo together, reusing
      the saved guest credential in deploy/local/smoke-session.json.
EOF
  exit 2
}

if [[ $# -lt 1 ]]; then
  usage
fi
command="$1"
shift

if [[ ! -f "$settings_file" ]]; then
  echo "Copy deploy/vm.example.yml to deploy/local/vm.yml and fill it in." >&2
  exit 1
fi

# Extra vars as JSON, so a path with spaces stays one value.
dump_file_variables() {
  python3 -c 'import json, sys; print(json.dumps({"app_dump_file": sys.argv[1]}))' "$1"
}

# ansible.cfg is only read from the current directory, so playbooks run from
# deploy/ansible. Paths on the command line are resolved before that.
run_playbook() {
  local host="$1" playbook="$2"
  shift 2
  if ! command -v ansible-playbook >/dev/null; then
    echo "Install Ansible first, e.g. 'uv tool install ansible-core'. See deploy/README.md." >&2
    exit 1
  fi
  local collections_directory="$ansible_directory/.collections/ansible_collections"
  if [[ ! -d "$collections_directory/community/docker" ]] || [[ ! -d "$collections_directory/community/crypto" ]]; then
    (cd "$ansible_directory" && ansible-galaxy collection install -r requirements.yml -p .collections)
  fi
  (cd "$ansible_directory" && ansible-playbook -i "$host," -e "@$settings_file" "$playbook" "$@")
}

case "$command" in
  deploy)
    [[ $# -ge 1 ]] || usage
    host="$1"
    shift
    run_playbook "$host" site.yml "$@"
    ;;
  dump)
    [[ $# -ge 1 ]] || usage
    host="$1"
    shift
    run_playbook "$host" dump.yml "$@"
    ;;
  restore)
    [[ $# -ge 2 ]] || usage
    host="$1"
    dump_file="$(realpath -- "$2")"
    shift 2
    run_playbook "$host" restore.yml -e "$(dump_file_variables "$dump_file")" "$@"
    ;;
  move-data)
    [[ $# -ge 2 ]] || usage
    old_host="$1"
    new_host="$2"
    shift 2
    dump_file="$local_directory/dumps/island_battles-$old_host-$(date -u +%Y%m%dT%H%M%SZ).dump"
    run_playbook "$old_host" dump.yml -e "$(dump_file_variables "$dump_file")" -e app_dump_lock_writes=true "$@"
    if ! run_playbook "$new_host" restore.yml -e "$(dump_file_variables "$dump_file")" "$@"; then
      cat >&2 <<EOF
The restore into $new_host failed, and the backend stays locked out of $old_host.
Fix the cause and run the same move-data command again, or let the backend use
$old_host again with: scripts/vm.sh deploy $old_host -e app_unlock_after_move=true
EOF
      exit 1
    fi
    cat <<EOF
Copied the database from $old_host to $new_host. The dump stays in $dump_file.
The backend can't log in to $old_host any more, even after a redeploy. Move the
static IP to the new VM next; deploy/README.md lists the remaining steps.
EOF
    ;;
  smoke)
    [[ $# -eq 0 ]] || usage
    game_url="$(sed -n -E "s/^game_url:[[:space:]]*[\"']?([^\"' #]+).*/\1/p" "$settings_file")"
    if [[ -z "$game_url" ]]; then
      echo "Set game_url in $settings_file. scripts/aws.sh output game_url prints it." >&2
      exit 1
    fi
    # The smoke test uses the lobby client's locked dependencies.
    uv run --project "$repository_root/lobby/clients/lobby-python" --locked \
      python "$repository_root/deploy/smoke.py" \
      --game-url "$game_url" --session-file "$local_directory/smoke-session.json"
    ;;
  *)
    usage
    ;;
esac
