#!/usr/bin/env bash
# scripts/ci/pr-merge.sh — `make ci-pr`: build the temporary merge of the
# current HEAD with origin/main (exactly what GitHub Actions checks out for a
# pull_request) and run the full CI on that merge tree.
#
# Guarantees:
#   - never modifies the user's branch or working copy;
#   - never pushes anything;
#   - prints conflicting files and exits non-zero on merge conflicts;
#   - removes the temporary worktree afterwards.
set -euo pipefail

_script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/ci/common.sh
. "${_script_dir}/common.sh"

cd "${CI_ROOT}"

head_sha="$(git rev-parse HEAD)"
branch="$(git rev-parse --abbrev-ref HEAD)"

ci_step "fetch origin main"
git fetch origin main

base_sha="$(git rev-parse origin/main)"
if [ "$(git merge-base origin/main HEAD)" = "${base_sha}" ]; then
  ci_log "HEAD is already up to date with origin/main — testing HEAD directly"
  merge_ref="${head_sha}"
else
  ci_step "temporary merge: ${branch} (${head_sha:0:8}) into origin/main (${base_sha:0:8})"
fi

WORKTREE="$(mktemp -d "${CI_ARTIFACT_DIR}/pr-worktree.XXXXXX")"
rmdir "${WORKTREE}"  # git worktree add wants to create the dir itself

# shellcheck disable=SC2317  # invoked indirectly via trap
cleanup() {
  rc=$?
  git worktree remove --force "${WORKTREE}" >/dev/null 2>&1 || true
  exit "${rc}"
}
trap cleanup EXIT

git worktree add --detach "${WORKTREE}" origin/main >/dev/null 2>&1

if [ "${merge_ref:-}" = "${head_sha}" ]; then
  git -C "${WORKTREE}" checkout --detach "${head_sha}" >/dev/null 2>&1
else
  if ! git -C "${WORKTREE}" merge --no-ff --no-edit "${head_sha}" >/dev/null 2>&1; then
    echo "Merge conflicts with origin/main:" >&2
    git -C "${WORKTREE}" diff --name-only --diff-filter=U >&2
    git -C "${WORKTREE}" merge --abort >/dev/null 2>&1 || true
    exit 1
  fi
  ci_log "merge tree: $(git -C "${WORKTREE}" rev-parse HEAD)"
fi

# Run the full CI on the merge tree. The snapshot is taken from the worktree's
# HEAD, so exactly the merge result is tested — the user's checkout is untouched.
CI_TREEISH="$(git -C "${WORKTREE}" rev-parse HEAD)"
export CI_TREEISH

rc=0
"${CI_SCRIPTS_DIR}/run-job.sh" full || rc=$?

exit "${rc}"
