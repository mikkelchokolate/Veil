#!/usr/bin/env bash
# scripts/ci/package-smoke.sh — package smoke CI job.
# Builds .deb/.rpm/.apk with pinned nfpm and exercises install/upgrade/uninstall
# in clean distributions via scripts/package-smoke.sh (which uses docker).
set -euo pipefail

_script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/ci/common.sh
. "${_script_dir}/common.sh"

cd "${CI_ROOT}"

ci_step "web/dist (embedded into the binary)"
(cd web && pnpm install --frozen-lockfile && pnpm build)

ci_step "build package payload"
mkdir -p dist
go build -trimpath -ldflags "-s -w -X main.version=package-smoke" -o dist/veil ./cmd/veil

GOBIN_PATH="$(go env GOPATH)/bin"
export PATH="${GOBIN_PATH}:${PATH}"
if ! command -v nfpm >/dev/null 2>&1; then
  ci_run install-nfpm go install "github.com/goreleaser/nfpm/v2/cmd/nfpm@${CI_NFPM_VERSION}"
fi

ci_run package-smoke bash scripts/package-smoke.sh

cp -f package-smoke.log "${CI_ARTIFACT_DIR}/" 2>/dev/null || true

ci_log "package-smoke job passed"
