#!/usr/bin/env bash
# scripts/ci/full.sh — job set for `make ci-full` / `make ci-pr`.
# Runs only as a phase selected by run-job.sh. Each phase is executed in the
# image/backend that actually provides its requirements; there is intentionally
# no mixed single-guest fallback.
set -euo pipefail

_script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/ci/common.sh
. "${_script_dir}/common.sh"

phase="${CI_FULL_PHASE:-}"
[ -n "${phase}" ] || ci_die "CI_FULL_PHASE is required; invoke full through scripts/ci/run-job.sh"

case "${phase}" in
  base)
    ci_job_run frontend
    ci_job_run test
    ci_job_run lint
    ;;
  browser)
    ci_job_run browser-e2e
    ;;
  system)
    ci_job_run privilege-boundary
    ci_job_run e2e
    ;;
  docker)
    ci_job_run package-smoke
    ci_job_run image-build
    ;;
  *) ci_die "unknown CI_FULL_PHASE '${phase}'" ;;
esac

ci_log "full (ci-full) phase '${phase}' passed"
