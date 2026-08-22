#!/usr/bin/env bash
# Dedicated cross-process consistency and serialization regressions.
set -euo pipefail

_script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/ci/common.sh
. "${_script_dir}/common.sh"

cd "${CI_ROOT}"
if [ ! -f web/dist/index.html ]; then
  mkdir -p web/dist
  printf '%s\n' '<!doctype html><title>CI test stub</title>' > web/dist/index.html
fi

ci_run multiprocess-apply \
  go test ./internal/apply -run '^TestApplyFencingAcrossOSProcesses$' -count=1 -timeout=60s
ci_run multiprocess-idempotency \
  go test ./internal/api -run '^TestIdempotencyReservationIsSharedAcrossOSProcesses$' -count=1 -timeout=60s
ci_run multiprocess-promotion-lock \
  go test ./internal/privileged -run '^TestPromotionLockCoversPreflightThroughPublicationAcrossProcesses$' -count=1 -timeout=60s

ci_log "multi-process job passed"
