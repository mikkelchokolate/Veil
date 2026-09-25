#!/usr/bin/env bash
# scripts/ci/lint.sh — lint CI job (shared by GitHub Actions and local VM).
# staticcheck + govulncheck + shellcheck, versions from versions.sh.
set -euo pipefail

_script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/ci/common.sh
. "${_script_dir}/common.sh"

cd "${CI_ROOT}"

ci_run version-consistency python3 scripts/ci/verify_versions.py

# Named-root job scripts (e2e/sigkill/filesystem-faults/multi-process and the
# privilege-boundary named legs) select coverage by literal test name; the
# test-job inventory cannot see those roots, so reconcile the lists against
# package contents here or a new gated root silently bypasses CI (issue #1014).
ci_run named-roots python3 scripts/ci/verify-named-roots.py

ci_step "stub web/dist for analysis (go:embed must resolve)"
if [ ! -f web/dist/index.html ]; then
  mkdir -p web/dist
  echo '<!doctype html><title>analysis stub</title>' > web/dist/index.html
fi

GOBIN_PATH="$(go env GOPATH)/bin"
export PATH="${GOBIN_PATH}:${PATH}"

# A preinstalled tool is only acceptable when it reports the exact pinned
# version — an older staticcheck/govulncheck on PATH must not silently satisfy
# the gate (issue #403).
ensure_pinned_tool() { # <binary> <pinned-version> <go-install-package@version>
  local bin="$1" want="$2" pkg="$3"
  if command -v "${bin}" >/dev/null 2>&1 && "${bin}" -version 2>/dev/null | grep -qF "${want}"; then
    ci_log "${bin} ${want} already installed"
    return 0
  fi
  ci_run "install-${bin}" go install "${pkg}"
  "${bin}" -version 2>/dev/null | grep -qF "${want}" \
    || ci_die "${bin} does not report the pinned version ${want} after install"
}

ensure_pinned_tool staticcheck "${CI_STATICCHECK_VERSION}" \
  "honnef.co/go/tools/cmd/staticcheck@${CI_STATICCHECK_VERSION}"
ci_run staticcheck staticcheck ./...

ensure_pinned_tool govulncheck "${CI_GOVULNCHECK_VERSION}" \
  "golang.org/x/vuln/cmd/govulncheck@${CI_GOVULNCHECK_VERSION}"
ci_run govulncheck govulncheck ./...

# The shellcheck binary is pinned in versions.sh (CI_SHELLCHECK_VERSION); the
# GHA job and the CI image install the matching apt package. Fail closed on
# drift (issue #420) rather than linting with whatever happens to be on PATH.
# NB: keep the word "shellcheck" out of the comment lead — a `# shellcheck`
# prefix is parsed as a shellcheck directive (SC1072/SC1073).
command -v shellcheck >/dev/null 2>&1 \
  || ci_die "shellcheck ${CI_SHELLCHECK_VERSION} is required (runner job / CI image installs it)"
shellcheck --version | grep -qF "version: ${CI_SHELLCHECK_VERSION}" \
  || ci_die "shellcheck version mismatch: expected ${CI_SHELLCHECK_VERSION}, got $(shellcheck --version | awk '/version:/ {print $2}')"

ci_run shellcheck shellcheck scripts/*.sh scripts/ci/*.sh packaging/scripts/*.sh \
  ci/vm/*.sh ci/vm/systemd/*.sh packaging/docker/entrypoint.sh

ci_step "install/uninstall script validation"
sh -n scripts/install.sh
sh -n scripts/install-main.sh
bash -n scripts/install-privileged.sh scripts/uninstall.sh
# ci/vm guest helpers and the container entrypoint are bash/sh scripts too —
# keep them inside the syntax gate (issue #452).
bash -n ci/vm/*.sh ci/vm/systemd/*.sh packaging/docker/entrypoint.sh
bash scripts/install-privileged.sh --help >/dev/null
bash scripts/uninstall.sh --help >/dev/null
bash scripts/install-main.sh --help >/dev/null

ci_log "lint job passed"
