#!/usr/bin/env bash
# scripts/ci/test.sh — backend test CI job (shared by GitHub Actions and local VM).
# This job keeps the complete race+coverage test scope, but runs package and API
# work through one bounded scheduler so the expensive phases overlap.
set -euo pipefail

_script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/ci/common.sh
# shellcheck disable=SC1091
. "${_script_dir}/common.sh"

cd "${CI_ROOT}"
: > "${CI_ARTIFACT_DIR}/timings.jsonl"

ci_test_stage() {
  local label="$1"; shift
  ci_timer_start
  ci_step "${label}"
  set +e
  "$@"
  local rc=$?
  set -e
  ci_timer_stop "test:${label}"
  return "${rc}"
}

frontend_stage() { bash "${CI_SCRIPTS_DIR}/prepare-frontend-dist.sh"; }
tidy_stage() { go mod tidy; git diff --exit-code -- go.mod go.sum; }
gofmt_stage() {
  local unformatted
  # Null-delimited file list: spaced paths must not be split, and -r keeps an
  # empty list from hanging gofmt on stdin (same pattern as release.yml, #444).
  unformatted="$(git ls-files -z '*.go' | xargs -0 -r gofmt -l)"
  if [ -n "${unformatted}" ]; then
    printf 'These files are not gofmt-clean:\n%s\n' "${unformatted}" >&2
    return 1
  fi
}
caddy_stage() {
  local runtime_dir="${VEIL_CI_RUNTIME_DIR:-/opt/veil-runtime}"
  if [ ! -x "${runtime_dir}/caddy" ]; then
    runtime_dir="${VEIL_CI_CADDY_CACHE_DIR:-${HOME}/.cache/veil-caddy-test}"
    # shellcheck source=scripts/ci/runtimes.sh
    # shellcheck disable=SC1091
    . "${CI_SCRIPTS_DIR}/runtimes.sh"
    install_pinned_caddy_for_tests "${runtime_dir}" "./bin/veil"
  elif ! "${runtime_dir}/caddy" list-modules 2>/dev/null | grep -Fx http.handlers.forward_proxy >/dev/null; then
    rm -f "${runtime_dir}/caddy"
    # shellcheck source=scripts/ci/runtimes.sh
    # shellcheck disable=SC1091
    . "${CI_SCRIPTS_DIR}/runtimes.sh"
    install_pinned_caddy_for_tests "${runtime_dir}" "./bin/veil"
  fi
  export PATH="${runtime_dir}:${PATH}"
  "${runtime_dir}/caddy" list-modules | grep -Fx http.handlers.forward_proxy
}

ci_test_stage "frontend artifact restore" frontend_stage
ci_test_stage "go.mod tidy drift" tidy_stage
ci_test_stage gofmt gofmt_stage
ci_test_stage go-vet ci_run go-vet go vet ./...
ci_test_stage "OpenAPI verification" ci_run verify-openapi make verify-openapi
ci_test_stage "SDK verification" ci_run verify-sdk make verify-sdk
ci_test_stage build ci_run build make build
ci_test_stage "Caddy preparation" caddy_stage

# The SDK suite writes a real coverprofile that is merged and measured like
# every other package — a run without -coverprofile would keep sdk/go
# invisible. It gates on CI_SDK_COVERAGE_FLOOR rather than the 70% product
# threshold, which always measured the product statement universe (#438).
rm -rf "${CI_ARTIFACT_DIR}/coverage-tasks"
mkdir -p "${CI_ARTIFACT_DIR}/coverage-tasks"
ci_test_stage "SDK tests" ci_run sdk-tests go test ./sdk/go -race -count=1 -json \
  "-coverprofile=${CI_ARTIFACT_DIR}/coverage-tasks/coverage-sdk.out"

# test-orchestrator.py is the live bounded scheduler for package + API shard
# tasks; verify-test-shards.py then proves every API root executed exactly
# once across the shard logs (the retired api-shards.sh did this inline).
packages="$(go list ./... | grep -v '/sdk/go$')"
api_package="$(go list ./internal/api)"
non_api_packages="$(printf '%s\n' "${packages}" | grep -v "^${api_package}$")"
printf '%s\n' "${non_api_packages}" > "${CI_ARTIFACT_DIR}/product-packages.txt"

ci_test_stage "test discovery" python3 "${CI_SCRIPTS_DIR}/test-inventory.py" \
  --repo "${CI_ROOT}" --artifact-dir "${CI_ARTIFACT_DIR}"

test_workers="${CI_TEST_WORKERS:-}"
if [ -z "${test_workers}" ]; then
  test_workers="$(getconf _NPROCESSORS_ONLN 2>/dev/null || nproc)"
  [ "${test_workers}" -le 4 ] || test_workers=4
fi
manifest_args=()
if [ -f "${CI_TEST_TIMING_MANIFEST:-${CI_ARTIFACT_DIR}/timing-manifest.json}" ]; then
  manifest_args=(--timing-manifest "${CI_TEST_TIMING_MANIFEST:-${CI_ARTIFACT_DIR}/timing-manifest.json}")
fi
set +e
python3 "${CI_SCRIPTS_DIR}/test-orchestrator.py" \
  --repo "${CI_ROOT}" \
  --api-package "${api_package}" \
  --packages-file "${CI_ARTIFACT_DIR}/product-packages.txt" \
  --artifact-dir "${CI_ARTIFACT_DIR}" \
  --coverage-dir "${CI_ARTIFACT_DIR}/coverage-tasks" \
  --workers "${test_workers}" \
  --api-shards "${CI_API_SHARDS:-4}" \
  --task-timeout "${CI_TEST_TASK_TIMEOUT:-1800}" \
  --roots-json "${CI_ARTIFACT_DIR}/test-roots.json" \
  "${manifest_args[@]}" \
  ${CI_API_SERIAL_ROOTS:+--serial-roots "${CI_API_SERIAL_ROOTS}"} \
  2>&1 | tee -a "${CI_ARTIFACT_DIR}/product-test.log"
orchestrator_rc=${PIPESTATUS[0]}
set -e
if [ "${orchestrator_rc}" -ne 0 ]; then
  ci_fail_banner
  exit "${orchestrator_rc}"
fi

# Shard verification (issue #429): every discovered API root must appear
# exactly once across the shard + serial JSON logs the orchestrator produced.
# A dropped lane or a -run regex that silently matched nothing is a gate
# failure, not a green run. Read the discovered inventory (test-roots.json,
# written by the discovery stage above) — expected-roots.txt only exists
# after the report stage below, so reading it here would be stale data.
python3 - "${CI_ARTIFACT_DIR}/test-roots.json" "${api_package}" \
  > "${CI_ARTIFACT_DIR}/api-expected-roots.txt" <<'PY'
import json, sys
rows = json.load(open(sys.argv[1], encoding="utf-8"))
print("\n".join(sorted(r["root"] for r in rows if r["package"] == sys.argv[2])))
PY
[ -s "${CI_ARTIFACT_DIR}/api-expected-roots.txt" ] \
  || ci_die "test discovery found no API roots for ${api_package}"
mapfile -t api_task_logs < <(grep -E '/api-(shard-[0-9]+|serial)\.json$' "${CI_ARTIFACT_DIR}/test-task-logs.txt")
[ "${#api_task_logs[@]}" -gt 0 ] || ci_die "orchestrator produced no API shard task logs"
ci_test_stage "API shard root verification" \
  python3 "${CI_SCRIPTS_DIR}/verify-test-shards.py" \
    "${CI_ARTIFACT_DIR}/api-expected-roots.txt" "${api_task_logs[@]}"

mapfile -t task_logs < "${CI_ARTIFACT_DIR}/test-task-logs.txt"
report_args=(--repo "${CI_ROOT}" --artifact-dir "${CI_ARTIFACT_DIR}"
  --skip-allowlist "${CI_SCRIPTS_DIR}/test-skip-allowlist.txt")
for log in "${CI_ARTIFACT_DIR}/sdk-tests.log" "${task_logs[@]}"; do
  [ -f "${log}" ] && report_args+=(--log "${log}")
done
coverage_merge_stage() {
  local profiles=("${CI_ARTIFACT_DIR}/coverage-tasks/"*.out)
  [ -f "${profiles[0]}" ]
  # Every scheduled task (packages + API shards + optional serial lane) must
  # have left a coverprofile, plus the SDK profile written above — merging a
  # subset inflates the coverage figure (#439).
  local expected
  expected=$(( $(wc -l < "${CI_ARTIFACT_DIR}/test-task-logs.txt") + 1 ))
  if [ "${#profiles[@]}" -ne "${expected}" ]; then
    printf 'expected %s coverage profiles, found %s\n' "${expected}" "${#profiles[@]}" >&2
    return 1
  fi
  python3 "${CI_SCRIPTS_DIR}/merge-coverprofiles.py" coverage.out "${profiles[@]}"
}
coverage_check_stage() {
  local total_coverage product_coverage sdk_coverage
  [ -n "${CI_COVERAGE_THRESHOLD:-}" ] || { echo 'CI_COVERAGE_THRESHOLD is unset' >&2; return 1; }
  [ -n "${CI_SDK_COVERAGE_FLOOR:-}" ] || { echo 'CI_SDK_COVERAGE_FLOOR is unset' >&2; return 1; }
  # `go tool cover -func` prints one summary row "total:"; a bare substring
  # grep can match a *function* named e.g. totalUpload and read the wrong
  # percentage, false-greening the threshold (#415).
  total_coverage="$(go tool cover -func=coverage.out | awk '$1 == "total:" {print $NF}' | tr -d '%')"
  [ -n "${total_coverage}" ] || { echo 'no total coverage row in coverage.out' >&2; return 1; }
  # The 70% gate has always measured the PRODUCT statement universe — sdk/go
  # sat outside the coverprofile merge entirely (#438). sdk/go is merged now
  # so it is measured and reported instead of invisible, but it is a thin
  # client-library surface that was never part of the 70% contract: it gates
  # on its own floor (the suite must produce real assertions, not just run)
  # while the product threshold keeps the statement set it always covered.
  # Both figures are reported so an SDK coverage collapse stays visible
  # rather than being averaged into the total.
  product_coverage="$(awk '$1 ~ /^github\.com\/mikkelchokolate\/Veil\// && $1 !~ /^github\.com\/mikkelchokolate\/Veil\/sdk\// {
      stmts += $2; if ($3+0 > 0) cov += $2;
    } END { if (stmts == 0) exit 1; printf "%.1f", 100*cov/stmts }' coverage.out)"
  sdk_coverage="$(awk '$1 ~ /^github\.com\/mikkelchokolate\/Veil\/sdk\// {
      stmts += $2; if ($3+0 > 0) cov += $2;
    } END { if (stmts == 0) exit 1; printf "%.1f", 100*cov/stmts }' coverage.out)"
  [ -n "${product_coverage}" ] || { echo 'no product statements in coverage.out' >&2; return 1; }
  [ -n "${sdk_coverage}" ] || { echo 'no sdk statements in coverage.out - merge dropped the sdk profile' >&2; return 1; }
  {
    printf 'Product statement coverage is %s%% (threshold %s%%)\n' "${product_coverage}" "${CI_COVERAGE_THRESHOLD}"
    printf 'SDK statement coverage is %s%% (floor %s%%)\n' "${sdk_coverage}" "${CI_SDK_COVERAGE_FLOOR}"
    printf 'Total statement coverage is %s%%\n' "${total_coverage}"
  } | tee "${CI_ARTIFACT_DIR}/coverage-summary.txt"
  awk -v cov="${product_coverage}" -v min="${CI_COVERAGE_THRESHOLD}" \
    'BEGIN { if (cov+0 < min+0) { print "Error: product coverage " cov "% is below threshold (" min "%)"; exit 1 } }' || return 1
  awk -v cov="${sdk_coverage}" -v min="${CI_SDK_COVERAGE_FLOOR}" \
    'BEGIN { if (cov+0 < min+0) { print "Error: sdk coverage " cov "% is below floor (" min "%)"; exit 1 } }'
}
ci_test_stage "coverage merge" coverage_merge_stage
cp -f coverage.out "${CI_ARTIFACT_DIR}/coverage.out"
ci_test_stage "test inventory report" python3 "${CI_SCRIPTS_DIR}/test-inventory.py" "${report_args[@]}"

ci_test_stage "coverage check" coverage_check_stage

ci_test_stage "shell hygiene" bash -c '
sh -n scripts/install.sh
sh -n scripts/install-main.sh
bash -n scripts/install-privileged.sh scripts/uninstall.sh scripts/package-smoke.sh
sh -n packaging/scripts/*.sh
bash scripts/install-privileged.sh --help >/dev/null
bash scripts/uninstall.sh --help >/dev/null
bash scripts/install-main.sh --help >/dev/null
git diff --check
'

# Keep both JSONL (append-friendly) and a JSON array for artifact consumers.
python3 - "${CI_ARTIFACT_DIR}/timings.jsonl" "${CI_ARTIFACT_DIR}/timings.json" <<'PY'
import json, sys
src, dst = sys.argv[1:]
rows = []
for line in open(src, encoding="utf-8"):
    if line.strip():
        rows.append(json.loads(line))
with open(dst, "w", encoding="utf-8") as stream:
    json.dump(rows, stream, indent=2)
    stream.write("\n")
summary = "\n## test job timings\n\n| step | ms |\n| --- | ---: |\n"
summary += "".join(f"| {r['step']} | {r['ms']} |\n" for r in rows)
if os_path := __import__("os").environ.get("GITHUB_STEP_SUMMARY"):
    with open(os_path, "a", encoding="utf-8") as stream:
        stream.write(summary)
PY

total_coverage="$(go tool cover -func=coverage.out | awk '$1 == "total:" {print $NF}' | tr -d '%')"
ci_log "test job passed (coverage ${total_coverage}%, workers ${CI_TEST_WORKERS:-auto})"
