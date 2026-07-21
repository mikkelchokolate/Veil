#!/usr/bin/env bash
# scripts/ci/frontend.sh — frontend CI job (shared by GitHub Actions and local VM).
# Mirrors the former `frontend` workflow job exactly:
#   frozen install -> API client generation drift -> typecheck -> Biome ->
#   i18n (catalog parity + leak scan) -> unit tests -> browser-mode tests -> build.
set -euo pipefail

_script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/ci/common.sh
. "${_script_dir}/common.sh"

cd "${CI_ROOT}/web"

ci_run frontend-install pnpm install --frozen-lockfile

ci_step "generated API client drift"
pnpm gen
git diff --exit-code -- src/api/generated

ci_run frontend-typecheck pnpm typecheck
ci_run frontend-biome pnpm check
ci_run frontend-i18n pnpm i18n:check
ci_run frontend-unit pnpm test

# Browser-mode unit tests need a real Chromium and run in the browser image —
# see browser-e2e.sh (spec: no runtime provisioning inside base jobs).

ci_run frontend-build pnpm build

ci_log "frontend job passed"
