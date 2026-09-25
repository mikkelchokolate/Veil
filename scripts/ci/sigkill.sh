#!/usr/bin/env bash
# Dedicated abrupt-process-death durability and recovery regressions.
set -euo pipefail

_script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/ci/common.sh
. "${_script_dir}/common.sh"

cd "${CI_ROOT}"

# Each selected root must report `--- PASS:` by name — ci_assert_tests_ran
# accepts an all-SKIP suite as evidence it ran (issue #688).
ci_run sigkill-sqlite-durability \
  go test ./internal/storage -run '^TestSQLiteCommittedDesiredSnapshotSurvivesImmediateProcessKill$' -count=1 -v -timeout=60s
ci_assert_test_passed "${CI_ARTIFACT_DIR}/sigkill-sqlite-durability.log" TestSQLiteCommittedDesiredSnapshotSurvivesImmediateProcessKill
ci_run sigkill-promotion \
  go test ./internal/privileged -run '^(TestPromotionRecoversSIGKILLAfterEveryArtifactPublication|TestPromotionRollbackRecoversSIGKILLAfterEveryArtifactPublication)$' -count=1 -v -timeout=2m
ci_assert_test_passed "${CI_ARTIFACT_DIR}/sigkill-promotion.log" TestPromotionRecoversSIGKILLAfterEveryArtifactPublication
ci_assert_test_passed "${CI_ARTIFACT_DIR}/sigkill-promotion.log" TestPromotionRollbackRecoversSIGKILLAfterEveryArtifactPublication
ci_run sigkill-firewall \
  go test ./internal/privileged -run '^TestFirewallTransactionRecoversExactStateAfterSIGKILL$' -count=1 -v -timeout=60s
ci_assert_test_passed "${CI_ARTIFACT_DIR}/sigkill-firewall.log" TestFirewallTransactionRecoversExactStateAfterSIGKILL
ci_run sigkill-restore \
  go test ./internal/backup -run '^TestRestoreRecoversSIGKILLAfterEveryFilePublication$' -count=1 -v -timeout=2m
ci_assert_test_passed "${CI_ARTIFACT_DIR}/sigkill-restore.log" TestRestoreRecoversSIGKILLAfterEveryFilePublication
# Every Test*SIGKILL*/*ProcessKill* root in the tree must be named here (or in
# scripts/ci/named-root-exemptions.txt) — verify-named-roots.py enforces the
# reconciliation so a new durability root cannot silently bypass this job
# (issue #1014).
ci_run sigkill-apply-publication \
  go test ./internal/apply -run '^TestApplyPublicationSIGKILLMatrix$' -count=1 -v -timeout=2m
ci_assert_test_passed "${CI_ARTIFACT_DIR}/sigkill-apply-publication.log" TestApplyPublicationSIGKILLMatrix
ci_run sigkill-runtime-activation \
  go test ./internal/runtimeinstall -run '^TestRuntimeActivationRecoversSIGKILLAtIrreversiblePhases$' -count=1 -v -timeout=60s
ci_assert_test_passed "${CI_ARTIFACT_DIR}/sigkill-runtime-activation.log" TestRuntimeActivationRecoversSIGKILLAtIrreversiblePhases

ci_log "sigkill job passed"
