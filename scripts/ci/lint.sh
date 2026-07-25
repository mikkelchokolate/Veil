#!/usr/bin/env bash
# scripts/ci/lint.sh — lint CI job (shared by GitHub Actions and local VM).
# staticcheck + govulncheck + shellcheck, versions from versions.sh.
set -euo pipefail

_script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/ci/common.sh
. "${_script_dir}/common.sh"

cd "${CI_ROOT}"

ci_run version-consistency python3 scripts/ci/verify_versions.py

ci_step "stub web/dist for analysis (go:embed must resolve)"
if [ ! -f web/dist/index.html ]; then
  mkdir -p web/dist
  echo '<!doctype html><title>analysis stub</title>' > web/dist/index.html
fi

GOBIN_PATH="$(go env GOPATH)/bin"
export PATH="${GOBIN_PATH}:${PATH}"

if ! command -v staticcheck >/dev/null 2>&1; then
  ci_run install-staticcheck go install "honnef.co/go/tools/cmd/staticcheck@${CI_STATICCHECK_VERSION}"
fi
ci_run staticcheck staticcheck ./...

if ! command -v govulncheck >/dev/null 2>&1; then
  ci_run install-govulncheck go install "golang.org/x/vuln/cmd/govulncheck@${CI_GOVULNCHECK_VERSION}"
fi
ci_run govulncheck govulncheck ./...

ci_run shellcheck shellcheck scripts/*.sh scripts/ci/*.sh packaging/scripts/*.sh

ci_step "install/uninstall script validation"
bash -n scripts/install.sh scripts/uninstall.sh
bash scripts/install.sh --help >/dev/null
bash scripts/uninstall.sh --help >/dev/null

ci_log "lint job passed"
