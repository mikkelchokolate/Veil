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

# A dirty worktree is fine for local runs — the parity contract targets
# committed state. When CI_TREEISH is set explicitly (e.g. the ci-pr merge
# commit), the snapshot content is already fully determined by that commit, so
# the check is unnecessary. Otherwise refuse dirty trees so "what CI checks"
# always equals "what is committed".
if [ "${CI_ALLOW_DIRTY:-0}" != "1" ] && [ -z "${CI_TREEISH:-}" ]; then
  if [ -n "$(git status --porcelain)" ]; then
    ci_die "working tree is dirty — commit first (or set CI_ALLOW_DIRTY=1 for a diagnostic run)"
  fi
fi

git archive --format=tar "${TREEISH}" > "${OUT}"
ci_log "snapshot: ${TREEISH} -> ${OUT} ($(du -h "${OUT}" | cut -f1))"
