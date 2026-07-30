package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDedicatedCrashAndFaultInjectionCIJobsAreWiredEndToEnd(t *testing.T) {
	root := filepath.Join("..", "..")
	read := func(path string) string {
		t.Helper()
		body, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatal(err)
		}
		return strings.ReplaceAll(string(body), "\r\n", "\n")
	}

	makefile := read("Makefile")
	dispatcher := read("scripts/ci/run-job.sh")
	workflow := read(".github/workflows/ci.yml")
	jobs := map[string][]string{
		"multi-process": {
			"TestApplyFencingAcrossOSProcesses",
			"TestIdempotencyReservationIsSharedAcrossOSProcesses",
			"TestPromotionLockCoversPreflightThroughPublicationAcrossProcesses",
		},
		"sigkill": {
			"TestSQLiteCommittedDesiredSnapshotSurvivesImmediateProcessKill",
			"TestPromotionRecoversSIGKILLAfterEveryArtifactPublication",
			"TestPromotionRollbackRecoversSIGKILLAfterEveryArtifactPublication",
			"TestFirewallTransactionRecoversExactStateAfterSIGKILL",
			"TestRestoreRecoversSIGKILLAfterEveryFilePublication",
		},
		"filesystem-faults": {
			"TestWriteCleansUpAndDoesNotCommitOnSyncFailure",
			"TestRestoreRecoveryRejectsUntrustedSafetyObjectsBeforeMutation",
			"TestRuntimeInstallRollsBackActiveTargetAfterPostActivationFailure",
			"TestRoutingSourceMultiFileReplacementIsTransactional",
			"TestMigrationHistoryChecksIteratorErrorBeforeSuccess",
		},
	}

	for job, tests := range jobs {
		scriptPath := "scripts/ci/" + job + ".sh"
		script := read(scriptPath)
		if !strings.Contains(makefile, job) {
			t.Errorf("Makefile does not advertise %q", job)
		}
		if !strings.Contains(dispatcher, "["+job+"]=base") {
			t.Errorf("run-job.sh does not dispatch %q in the base image", job)
		}
		if !strings.Contains(workflow, "  "+job+":") || !strings.Contains(workflow, "bash "+scriptPath) {
			t.Errorf("ci.yml does not invoke dedicated %q job", job)
		}
		for _, testName := range tests {
			if !strings.Contains(script, testName) {
				t.Errorf("%s does not invoke %s", scriptPath, testName)
			}
		}
	}
}
