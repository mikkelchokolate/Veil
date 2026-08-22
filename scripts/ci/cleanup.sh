#!/usr/bin/env bash
# scripts/ci/cleanup.sh — `make ci-clean`: removes CI run residue.
# Image cache is NOT touched unless --images is passed.
set -euo pipefail

_script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/ci/common.sh
. "${_script_dir}/common.sh"

ci_step "artifacts and temporary state"
rm -rf "${CI_ROOT}/.artifacts/ci"
rm -rf "${CI_ROOT}/.artifacts/pr-worktree."* 2>/dev/null || true

ci_step "leftover worktrees"
git -C "${CI_ROOT}" worktree list --porcelain | \
  awk '/^worktree / && $2 ~ /pr-worktree/ {print $2}' | while read -r wt; do
    git -C "${CI_ROOT}" worktree remove --force "${wt}" || true
  done

ci_step "leftover CI containers / VMs"
if command -v docker >/dev/null 2>&1; then
  docker ps -aq --filter 'name=veil-ci-' | xargs -r docker rm -f >/dev/null 2>&1 || true
fi
if command -v smolvm >/dev/null 2>&1; then
  smolvm machine ls --json 2>/dev/null | jq -r '.[].name // empty' 2>/dev/null | \
    awk '/^veil-ci-/ {print}' | while read -r m; do smolvm machine delete --name "${m}" -f >/dev/null 2>&1 || true; done
fi

# Do not use broad host-process matches here. VM/container jobs are destroyed
# above, and host diagnostic scripts own their exact child PIDs through traps.
# A prefix match for a test port or socket can otherwise kill a live service.

if [ "${1:-}" = "--images" ]; then
  ci_step "CI images and exported archives"
  docker images --format '{{.Repository}}:{{.Tag}}' | grep '^veil-ci-' | xargs -r docker rmi >/dev/null 2>&1 || true
  rm -f "${HOME}/.cache/veil-ci"/veil-ci-*.tar
fi

ci_log "cleanup done"
