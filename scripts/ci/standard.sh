#!/usr/bin/env bash
# scripts/ci/standard.sh — job set for `make ci` (pre-push authoritative gate).
# Runs INSIDE a VM via run-job.sh; this script executes inside the guest.
set -euo pipefail

_script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/ci/common.sh
. "${_script_dir}/common.sh"

ci_job_run frontend
ci_job_run test
ci_job_run lint
# The heavy Playwright E2E and the protocol/system matrix belong to full.sh.
# OCI build validation runs in the system image (needs the OCI daemon socket);
# run-job.sh dispatches it after this base phase.

ci_log "standard (make ci) base phase passed"
