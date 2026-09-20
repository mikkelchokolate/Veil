#!/usr/bin/env bash
# scripts/ci/common.sh — shared helpers sourced by every CI script.
# shellcheck shell=bash

# Resolve repo root regardless of the caller's working directory.
ci_repo_root() {
  if [ -n "${CI_REPO_ROOT:-}" ] && [ -d "${CI_REPO_ROOT}" ]; then
    printf '%s\n' "${CI_REPO_ROOT}"
    return 0
  fi
  local dir
  dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  # scripts/ci/common.sh -> repo root is two levels up.
  (cd "${dir}/../.." && pwd)
}

CI_ROOT="$(ci_repo_root)"
CI_SCRIPTS_DIR="${CI_ROOT}/scripts/ci"

# shellcheck source=scripts/ci/versions.sh
. "${CI_SCRIPTS_DIR}/versions.sh"

# Keep Go module transport deterministic across local Docker/smolvm and GitHub.
# This is a repository pin, not a host-specific caller override.
GODEBUG="${CI_GO_GODEBUG}"
GOPROXY="${CI_GO_GOPROXY}"
export GODEBUG GOPROXY

# Artifacts --------------------------------------------------------------------
: "${CI_ARTIFACT_DIR:=${CI_ROOT}/.artifacts/ci}"
mkdir -p "${CI_ARTIFACT_DIR}"

ci_log()  { printf '[ci] %s %s\n' "$(date -u +%H:%M:%S)" "$*"; }
ci_step() { printf '\n[ci] === %s ===\n' "$*"; }
ci_warn() { printf '[ci] WARNING: %s\n' "$*" >&2; }
ci_die()  { printf '[ci] ERROR: %s\n' "$*" >&2; exit 1; }

# ci_assert_tests_ran <log-file>: fail when a verbose `go test` log shows that
# zero tests executed. `go test -run <pattern>` exits 0 when the pattern matches
# nothing, so a targeted CI gate must prove its tests actually ran.
ci_assert_tests_ran() {
  local log="$1"
  if grep -qE '^(=== RUN[[:space:]]|--- (PASS|FAIL|SKIP):)' "${log}"; then
    return 0
  fi
  ci_warn "zero tests executed (see ${log}) — refusing to pass on an empty selection"
  return 1
}

# ci_assert_tests_passed <log-file>: fail when a verbose `go test -v` log shows
# that no test actually passed. ci_assert_tests_ran only proves the -run
# selection was non-empty; a suite where every test reported --- SKIP still
# satisfies it, which silently greens an unprivileged-only suite that never
# executed on the CI host (issue #411).
ci_assert_tests_passed() {
  local log="$1"
  if grep -qE '^--- PASS:' "${log}"; then
    return 0
  fi
  ci_warn "no test passed (see ${log}) — refusing to pass on an all-skip suite"
  return 1
}

# ci_assert_test_passed <log-file> <TestName>: fail when a specific test did
# not report `--- PASS: <name>` in a verbose `go test -v` log. Used for
# security-contract tests that must RUN (not skip) on the CI host — a skipped
# assertion is not evidence (issues #428).
ci_assert_test_passed() {
  local log="$1" name="$2"
  if grep -qE "^--- PASS: ${name}( |$)" "${log}"; then
    return 0
  fi
  ci_warn "required test ${name} did not pass (see ${log}) — a skip is not evidence"
  return 1
}

# Run a command, tee its output to a job log, preserve the command's exit code
# (tee must never mask failures). The pipeline runs in a subshell so the
# caller's shell options — notably pipefail — are left untouched; toggling
# pipefail in the caller's shell would leak `set +o pipefail` into every
# subsequent pipeline and silently false-green them (issue #451).
ci_run() {
  local name="$1"; shift
  local log="${CI_ARTIFACT_DIR}/${name}.log"
  ci_step "${name}: $*"
  local rc
  ( set -o pipefail; "$@" 2>&1 | tee "${log}" )
  rc=$?
  if [ "${rc}" -ne 0 ]; then
    ci_warn "${name} failed with exit code ${rc} (log: ${log})"
  fi
  return "${rc}"
}

# Timings ----------------------------------------------------------------------
# shellcheck disable=SC2034  # consumed by timings tooling outside this file
CI_TIMINGS_FILE="${CI_ARTIFACT_DIR}/timings.json"
_CI_TIMER_START=""

ci_timer_start() { _CI_TIMER_START="$(date +%s%N)"; }

# ci_timer_stop <label> — appends one JSON line to timings.jsonl (later merged).
ci_timer_stop() {
  local label="$1"
  local end dur_ms
  end="$(date +%s%N)"
  dur_ms=$(( (end - _CI_TIMER_START) / 1000000 ))
  printf '{"step": "%s", "ms": %d}\n' "${label}" "${dur_ms}" >> "${CI_ARTIFACT_DIR}/timings.jsonl"
  ci_log "timing: ${label} took $(( dur_ms / 1000 )).$(( (dur_ms % 1000) / 100 ))s"
}

# Job wrapper -------------------------------------------------------------------
# ci_job_run <job-name> <script-args...>: records environment manifest (once per
# job), runs scripts/ci/<job>.sh with timing, and on failure prints the
# artifact location. Returns the job's exit code.
ci_job_run() {
  local job="$1"; shift || true
  local script="${CI_SCRIPTS_DIR}/${job}.sh"
  [ -x "${script}" ] || [ -f "${script}" ] || ci_die "unknown CI job '${job}' (no ${script})"
  "${CI_SCRIPTS_DIR}/environment.sh" "${job}" || ci_warn "environment manifest failed (non-fatal)"
  ci_timer_start
  bash "${script}" "$@"
  local rc=$?
  ci_timer_stop "job:${job}"
  return "${rc}"
}

ci_fail_banner() {
  printf '\nCI failed.\n\nArtifacts:\n%s\n\n' "${CI_ARTIFACT_DIR}" >&2
}
