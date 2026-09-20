#!/usr/bin/env bash
# scripts/ci/snapshot.sh — create a clean repository snapshot for transfer into
# a CI VM. Uses `git archive` so uncommitted/untracked files, node_modules,
# build outputs, credentials and runtime state can never leak into the VM.
#
# Usage: snapshot.sh <tree-ish> <output.tar>
set -euo pipefail

TREEISH="${1:?usage: snapshot.sh <tree-ish> <output.tar>}"
OUT="${2:?usage: snapshot.sh <tree-ish> <output.tar>}"

_script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/ci/common.sh
. "${_script_dir}/common.sh"

cd "${CI_ROOT}"

# Refuse dirty trees so the archived tree and the tested tree are identical.
# A non-HEAD tree-ish (the detached ci-pr merge SHA) is deterministic and
# independent of the worktree, so it may be archived from a dirty checkout.
# But CI_TREEISH=HEAD — or any tree-ish resolving to HEAD — still archives the
# checked-out commit while the user's actual tree is dirty: refuse (#446).
if [ -n "$(git status --porcelain)" ]; then
  treeish_sha="$(git rev-parse --verify "${TREEISH}^{commit}" 2>/dev/null || true)"
  if [ -z "${treeish_sha}" ] || [ "${treeish_sha}" = "$(git rev-parse HEAD)" ]; then
    ci_die "working tree is dirty — commit first"
  fi
fi

git archive --format=tar "${TREEISH}" > "${OUT}"
ci_log "snapshot: ${TREEISH} -> ${OUT} ($(du -h "${OUT}" | cut -f1))"
