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
bash "${CI_SCRIPTS_DIR}/prepare-frontend-dist.sh"

ci_step "build package payload"
mkdir -p dist
go build -trimpath -ldflags "-s -w -X main.version=package-smoke" -o dist/veil ./cmd/veil

GOBIN_PATH="$(go env GOPATH)/bin"
export PATH="${GOBIN_PATH}:${PATH}"
# Always install the pinned nfpm — an arbitrary nfpm already on PATH (stale CI
# image, host tool) must never satisfy the pin silently (issue #391).
ci_run install-nfpm go install "github.com/goreleaser/nfpm/v2/cmd/nfpm@${CI_NFPM_VERSION}"
# `nfpm --version` prints a goreleaser-ldflags version that is "dev" for
# `go install` builds; the pin is only trustworthy in the Go module build info.
nfpm_bin="$(command -v nfpm)" \
  || ci_die "nfpm not on PATH after pinned install"
go version -m "${nfpm_bin}" | grep -F "mod	github.com/goreleaser/nfpm/v2	${CI_NFPM_VERSION}" \
  || ci_die "nfpm on PATH is not the pinned ${CI_NFPM_VERSION}: $(go version -m "${nfpm_bin}" 2>&1 | grep -F 'nfpm' | head -1)"

# ci_run already tees the smoke output to ${CI_ARTIFACT_DIR}/package-smoke.log;
# there is no second log file to copy (issue #392).
ci_run package-smoke bash scripts/package-smoke.sh

ci_log "package-smoke job passed"
