#!/usr/bin/env bash
# scripts/ci/full.sh — job set for `make ci-full` / `make ci-pr`.
# Runs INSIDE VMs via run-job.sh; this script executes inside a guest with all
# three image roles available (the VM layer starts the right image per phase —
# see vm-run.sh). This top-level script is used by the single-VM docker backend;
# the smolvm backend runs the same per-image phases as separate ephemeral VMs.
set -euo pipefail

_script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/ci/common.sh
. "${_script_dir}/common.sh"

phase="${CI_FULL_PHASE:-all}"

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
    ci_job_run package-smoke
    ci_job_run image-build
    ;;
  all)
    ci_job_run frontend
    ci_job_run test
    ci_job_run lint
    ci_job_run browser-e2e
    ci_job_run privilege-boundary
    ci_job_run e2e
    ci_job_run package-smoke
    ci_job_run image-build
    ;;
  *) ci_die "unknown CI_FULL_PHASE '${phase}'" ;;
esac

ci_log "full (ci-full) phase '${phase}' passed"
