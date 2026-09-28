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

# `git archive` honors .gitattributes export-ignore/export-subst: a marked
# file is silently dropped (or its $Format:$ content rewritten), so the VM
# would test a tree that differs from the commit while appearing green
# (#1149). Prove parity — every tracked blob in the tree must appear in the
# archive; a miss means export-ignore started excluding paths.
missing="$(comm -23 \
  <(git ls-tree -r --name-only "${TREEISH}" | LC_ALL=C sort) \
  <(tar -tf "${OUT}" | grep -v '/$' | LC_ALL=C sort) || true)"
if [ -n "${missing}" ]; then
  printf 'snapshot is missing tracked files (export-ignore?):\n%s\n' "${missing}" >&2
  ci_die "git archive snapshot dropped tracked files — VM/test parity broken"
fi
# export-subst keeps the file but rewrites content; flag it loudly too.
if git ls-files -z | git check-attr -z --stdin export-subst 2>/dev/null | tr '\0' '\n' | grep -qx 'set'; then
  ci_warn "tracked files carry export-subst — archived content may differ from the worktree (#1149)"
fi
ci_log "snapshot: ${TREEISH} -> ${OUT} ($(du -h "${OUT}" | cut -f1))"
