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
# CI_TREEISH is set for the detached ci-pr merge and is already deterministic.
if [ -z "${CI_TREEISH:-}" ] && [ -n "$(git status --porcelain)" ]; then
  ci_die "working tree is dirty — commit first"
fi

git archive --format=tar "${TREEISH}" > "${OUT}"
ci_log "snapshot: ${TREEISH} -> ${OUT} ($(du -h "${OUT}" | cut -f1))"
