#!/usr/bin/env bash
# Work out the next release for the release workflow (.github/workflows/release.yml):
# its version, and whether the VM must be deployed too. Prints version=,
# previous=, bump=, vm= and vm_owner= lines (for $GITHUB_OUTPUT) on stdout, and
# explains itself in Markdown on stderr (for the run's summary).
#
# Usage: scripts/release-plan.sh <patch|minor|major> [commit]
#
# Run it in a clone that has all tags. The commit defaults to HEAD.
#
# ISLAND_BATTLES_VM_OWNER is the repository that the deploy bucket's marker
# names as the VM's owner (`deploy/deploy_files.py vm-owner`), empty while this
# repository deploys the VM. Once INTODAN/mpl has taken the VM over, it is
# `mpl`: the release then never deploys the VM, and its inputs don't make the
# bump major.
set -euo pipefail

vm_owner="${ISLAND_BATTLES_VM_OWNER:-}"

# Everything scripts/vm.sh deploy reads from the repository: the playbooks and
# roles, the settings template they document, the script itself and the deploy
# bucket helper it calls. Any change here makes the release redeploy the VM.
vm_inputs=(
  deploy/ansible
  deploy/vm.example.yml
  scripts/vm.sh
  deploy/deploy_files.py
)

[[ $# -ge 1 && $# -le 2 ]] || { echo "Usage: $0 <patch|minor|major> [commit]" >&2; exit 2; }
requested="$1"
commit="$(git rev-parse --verify "${2:-HEAD}^{commit}")"
case "$requested" in
  patch | minor | major) ;;
  *) echo "The bump must be patch, minor or major, not '$requested'." >&2; exit 2 ;;
esac
if [[ -n "$vm_owner" && ! "$vm_owner" =~ ^[a-z0-9][a-z0-9._-]{0,63}$ ]]; then
  echo "ISLAND_BATTLES_VM_OWNER must name a repository such as mpl, not '$vm_owner'." >&2
  exit 2
fi
if [[ "$vm_owner" == mpl ]]; then
  owner_repository="INTODAN/mpl"
else
  owner_repository="the repository \`$vm_owner\`"
fi
skipped_vm="**The VM isn't deployed:** it is deployed from $owner_repository now (the deploy bucket's \`mpl/vm-owner\` says \`$vm_owner\`), whose Ansible owns its configuration."

previous="$(git tag --list 'v[0-9]*' --sort=-v:refname | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | head -n 1 || true)"

if [[ -z "$previous" ]]; then
  # Nothing to compare with, and nobody can tell what the VM runs, so the
  # first release deploys it too. The owner decided the first version.
  version="v0.1.0"
  bump="first"
  if [[ -n "$vm_owner" ]]; then
    vm=false
    reason="There is no earlier release, so this is the first one: $version. $skipped_vm"
  else
    vm=true
    reason="There is no earlier release, so this is the first one: $version. It deploys the VM too, because there's nothing to compare the VM's inputs with."
  fi
else
  if [[ "$(git rev-parse "$previous^{commit}")" == "$commit" ]]; then
    echo "$previous is already this commit (${commit:0:7}). Merge something first." >&2
    exit 1
  fi
  changed="$(git diff --name-only "$previous" "$commit" -- "${vm_inputs[@]}")"
  bump="$requested"
  if [[ -n "$vm_owner" ]]; then
    # The VM's inputs here no longer reach the VM, so they don't make the
    # release major either.
    vm=false
    if [[ -n "$changed" ]]; then
      reason="$skipped_vm The VM deploy's inputs here changed since $previous, but they no longer reach the VM, so the bump is $bump, as you asked. Changed:"$'\n'"$(sed 's/^/- `/; s/$/`/' <<<"$changed")"
    else
      reason="$skipped_vm The bump is $bump."
    fi
  elif [[ -n "$changed" ]]; then
    vm=true
    bump=major
    reason="Since $previous, the VM deploy's inputs changed, so the release deploys the VM and the bump is major (you asked for $requested). Changed:"$'\n'"$(sed 's/^/- `/; s/$/`/' <<<"$changed")"
  else
    vm=false
    reason="Since $previous, none of the VM deploy's inputs changed ($(printf '`%s` ' "${vm_inputs[@]}")), so the VM stays as it is and the bump is $bump."
  fi
  IFS=. read -r major minor patch <<<"${previous#v}"
  case "$bump" in
    major) version="v$((major + 1)).0.0" ;;
    minor) version="v$major.$((minor + 1)).0" ;;
    patch) version="v$major.$minor.$((patch + 1))" ;;
  esac
fi

# The release tags these two; the Go SDK module, in a subdirectory, needs its
# directory as the tag's prefix. The lobby is released from INTODAN/mpl.
for tag in "$version" "clients/island-battles-go/$version"; do
  if git rev-parse -q --verify "refs/tags/$tag" >/dev/null; then
    echo "The tag $tag already exists." >&2
    exit 1
  fi
done

{
  echo "## Release $version"
  echo
  echo "Commit \`${commit:0:7}\`, previous release: ${previous:-none}."
  echo
  echo "$reason"
} >&2

echo "version=$version"
echo "previous=$previous"
echo "bump=$bump"
echo "vm=$vm"
echo "vm_owner=$vm_owner"
