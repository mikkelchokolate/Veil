#!/usr/bin/env bash
# Dedicated filesystem and publication fault-injection regressions.
set -euo pipefail

_script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/ci/common.sh
. "${_script_dir}/common.sh"

cd "${CI_ROOT}"

ci_run filesystem-atomic-sync \
  go test ./internal/atomicfile -run '^TestWriteCleansUpAndDoesNotCommitOnSyncFailure$' -count=1 -timeout=30s
ci_run filesystem-restore-safety \
  go test ./internal/backup -run '^TestRestoreRecoveryRejectsUntrustedSafetyObjectsBeforeMutation$' -count=1 -timeout=60s
ci_run filesystem-runtime-publication \
  go test ./internal/runtimeinstall -run '^TestRuntimeInstallRollsBackActiveTargetAfterPostActivationFailure$' -count=1 -timeout=60s
ci_run filesystem-routing-publication \
  go test ./internal/generatedconfig -run '^TestRoutingSourceMultiFileReplacementIsTransactional$' -count=1 -timeout=60s
ci_run filesystem-iterator-error \
  go test ./internal/storage -run '^TestMigrationHistoryChecksIteratorErrorBeforeSuccess$' -count=1 -timeout=30s

ci_log "filesystem-faults job passed"
