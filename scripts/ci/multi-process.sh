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

# Each selected root must report `--- PASS:` by name — ci_assert_tests_ran
# accepts an all-SKIP suite as evidence it ran (issue #688).
ci_run multiprocess-apply \
  go test ./internal/apply -run '^TestApplyFencingAcrossOSProcesses$' -count=1 -v -timeout=60s
ci_assert_test_passed "${CI_ARTIFACT_DIR}/multiprocess-apply.log" TestApplyFencingAcrossOSProcesses
ci_run multiprocess-idempotency \
  go test ./internal/api -run '^TestIdempotencyReservationIsSharedAcrossOSProcesses$' -count=1 -v -timeout=60s
ci_assert_test_passed "${CI_ARTIFACT_DIR}/multiprocess-idempotency.log" TestIdempotencyReservationIsSharedAcrossOSProcesses
ci_run multiprocess-promotion-lock \
  go test ./internal/privileged -run '^TestPromotionLockCoversPreflightThroughPublicationAcrossProcesses$' -count=1 -v -timeout=60s
ci_assert_test_passed "${CI_ARTIFACT_DIR}/multiprocess-promotion-lock.log" TestPromotionLockCoversPreflightThroughPublicationAcrossProcesses

ci_log "multi-process job passed"
