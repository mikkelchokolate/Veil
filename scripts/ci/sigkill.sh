#!/usr/bin/env bash
# Dedicated abrupt-process-death durability and recovery regressions.
set -euo pipefail

_script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/ci/common.sh
. "${_script_dir}/common.sh"

cd "${CI_ROOT}"

ci_run sigkill-sqlite-durability \
  go test ./internal/storage -run '^TestSQLiteCommittedDesiredSnapshotSurvivesImmediateProcessKill$' -count=1 -timeout=60s
ci_run sigkill-promotion \
  go test ./internal/privileged -run '^(TestPromotionRecoversSIGKILLAfterEveryArtifactPublication|TestPromotionRollbackRecoversSIGKILLAfterEveryArtifactPublication)$' -count=1 -timeout=2m
ci_run sigkill-firewall \
  go test ./internal/privileged -run '^TestFirewallTransactionRecoversExactStateAfterSIGKILL$' -count=1 -timeout=60s
ci_run sigkill-restore \
  go test ./internal/backup -run '^TestRestoreRecoversSIGKILLAfterEveryFilePublication$' -count=1 -timeout=2m

ci_log "sigkill job passed"
