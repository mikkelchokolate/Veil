#!/usr/bin/env bash
# scripts/ci/test.sh — backend test CI job (shared by GitHub Actions and local VM).
# Mirrors the former `test` workflow job exactly, including the strict test
# invocation that plain `go test ./...` does NOT reproduce:
#   go mod tidy drift -> gofmt -> vet -> OpenAPI -> SDK drift -> build ->
#   runtime for unit tests -> SDK tests (-race) -> product tests
#   (-race -count=1 -coverprofile) -> coverage threshold -> shell hygiene.
set -euo pipefail

_script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/ci/common.sh
. "${_script_dir}/common.sh"

cd "${CI_ROOT}"

ci_step "web/dist (embedded into the binary)"
bash "${CI_SCRIPTS_DIR}/prepare-frontend-dist.sh"

ci_step "go.mod tidy drift"
go mod tidy
git diff --exit-code -- go.mod go.sum

ci_step "gofmt"
unformatted="$(git ls-files '*.go' | xargs gofmt -l)"
if [ -n "${unformatted}" ]; then
  printf 'These files are not gofmt-clean:\n%s\n' "${unformatted}" >&2
  exit 1
fi

ci_run go-vet go vet ./...
ci_run verify-openapi make verify-openapi
ci_run verify-sdk make verify-sdk
ci_run build make build

ci_step "install Caddy runtime for unit tests (pinned, checksum-verified)"
# Unit tests need a real caddy binary with the naive forward_proxy module.
# In CI images the runtime is pre-provisioned under /opt/veil-runtime by
# vm-build.sh from pinned artifacts (see versions.sh). Elsewhere (GitHub
# runners, diagnostic host runs) install the same pinned, checksum-verified
# artifacts — never /releases/latest.
runtime_dir="${VEIL_CI_RUNTIME_DIR:-/opt/veil-runtime}"
if [ ! -x "${runtime_dir}/caddy" ]; then
  runtime_dir="$(mktemp -d /tmp/veil-runtime.XXXXXX)"
  # shellcheck source=scripts/ci/runtimes.sh
  . "${CI_SCRIPTS_DIR}/runtimes.sh"
  install_pinned_runtimes "${runtime_dir}" "./bin/veil"
fi
export PATH="${runtime_dir}:${PATH}"
"${runtime_dir}/caddy" list-modules | grep -Fx http.handlers.forward_proxy

ci_run sdk-tests go test ./sdk/go -race -count=1

ci_step "product tests: -race -count=1 -coverprofile"
packages="$(go list ./... | grep -v '/sdk/go$')"
api_package="$(go list ./internal/api)"
non_api_packages="$(printf '%s\n' "${packages}" | grep -v "^${api_package}$")"
rm -f coverage.out coverage-non-api.out coverage-api.out
: > "${CI_ARTIFACT_DIR}/product-test.log"
set -o pipefail
# Keep the non-API packages in one process; the API package is intentionally
# run by api-shards.sh in isolated processes because its global test seams and
# SQLite state must never be shared between concurrent test functions.
# shellcheck disable=SC2086
# This is one bounded run, not a retry.
go test ${non_api_packages} -race -count=1 -timeout 30m -coverprofile=coverage-non-api.out 2>&1 \
  | tee -a "${CI_ARTIFACT_DIR}/product-test.log"
non_api_rc=${PIPESTATUS[0]}
if [ "${non_api_rc}" -ne 0 ]; then
  ci_fail_banner
  exit "${non_api_rc}"
fi

CI_API_SHARD_DIR="${CI_ARTIFACT_DIR}/api-shards" \
  bash "${CI_SCRIPTS_DIR}/api-shards.sh" ./internal/api coverage-api.out 2>&1 \
  | tee -a "${CI_ARTIFACT_DIR}/product-test.log"
api_rc=${PIPESTATUS[0]}
if [ "${api_rc}" -ne 0 ]; then
  ci_fail_banner
  exit "${api_rc}"
fi

python3 "${CI_SCRIPTS_DIR}/merge-coverprofiles.py" coverage.out \
  coverage-non-api.out coverage-api.out
set +o pipefail
cp -f coverage.out "${CI_ARTIFACT_DIR}/coverage.out" 2>/dev/null || true

ci_step "coverage threshold (min ${CI_COVERAGE_THRESHOLD}%)"
total_coverage="$(go tool cover -func=coverage.out | grep total | awk '{print $3}' | tr -d '%')"
echo "Total statement coverage is ${total_coverage}%" | tee "${CI_ARTIFACT_DIR}/coverage-summary.txt"
awk -v cov="${total_coverage}" -v min="${CI_COVERAGE_THRESHOLD}" \
  'BEGIN { if (cov+0 < min+0) { print "Error: coverage " cov "% is below threshold (" min "%)"; exit 1 } }'

ci_step "shell hygiene"
sh -n scripts/install.sh
bash -n scripts/install-privileged.sh scripts/uninstall.sh scripts/package-smoke.sh
sh -n packaging/scripts/*.sh
bash scripts/install-privileged.sh --help >/dev/null
bash scripts/uninstall.sh --help >/dev/null
git diff --check

ci_log "test job passed (coverage ${total_coverage}%)"
