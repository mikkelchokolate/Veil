#!/usr/bin/env bash
# scripts/ci/frontend.sh — frontend CI job (shared by GitHub Actions and local VM).
# Mirrors the former `frontend` workflow job exactly:
#   frozen install -> API client generation drift -> typecheck -> Biome ->
#   i18n (catalog parity + leak scan) -> unit tests (jsdom) -> build.
# Browser-mode (Chromium) unit tests are NOT part of this job — they run in
# browser-e2e.sh which owns the browser image (#419).
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

frontend_dist_artifact_dir="${CI_ARTIFACT_DIR}/frontend-dist"
rm -rf "${frontend_dist_artifact_dir}"
mkdir -p "${frontend_dist_artifact_dir}"
cp -a dist "${frontend_dist_artifact_dir}/"
# Producer-side integrity: never publish an artifact that is not a real Vite
# build — non-empty index.html plus at least one JavaScript bundle (#442).
[ -s "${frontend_dist_artifact_dir}/dist/index.html" ] \
  || ci_die "frontend build produced no dist/index.html — refusing to publish"
find "${frontend_dist_artifact_dir}/dist" -type f -name '*.js' | grep -q . \
  || ci_die "frontend build produced no JavaScript bundle — refusing to publish"
# source.sha records provenance (which commit was built). dist.sha256 binds
# the artifact's CONTENT — same commit with different/corrupted dist must not
# reuse the identity (#413, #414).
git -C "${CI_ROOT}" rev-parse HEAD > "${frontend_dist_artifact_dir}/source.sha"
(cd "${frontend_dist_artifact_dir}/dist" && find . -type f -print0 | LC_ALL=C sort -z | xargs -0 sha256sum) \
  > "${frontend_dist_artifact_dir}/dist.sha256"

ci_log "frontend job passed"
