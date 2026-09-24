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
	// stripHashComments before every substring assert: a `# runs TestX`
	// comment, a dead dispatcher entry in a comment, or a commented-out
	// workflow step must not satisfy the contract (issue #916).
	makefile := read("Makefile")
	dispatcher := stripHashComments(t, read("scripts/ci/run-job.sh"))
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
			"TestMigrationHistoryIteratorErrorFailsMigrate",
		},
	}

	for job, tests := range jobs {
		scriptPath := "scripts/ci/" + job + ".sh"
		script := stripHashComments(t, read(scriptPath))
		// The ci-job recipe (not a .PHONY line or comment) must advertise the
		// job name so `make ci-job JOB=<name>` reaches it.
		recipe := strings.Join(makefileRecipe(t, makefile, "ci-job"), "\n")
		if !strings.Contains(recipe, job) {
			t.Errorf("Makefile ci-job recipe does not advertise %q", job)
		}
		if !strings.Contains(dispatcher, "["+job+"]=base") {
			t.Errorf("run-job.sh does not dispatch %q in the base image", job)
		}
		// The workflow must declare a literal job key whose own steps invoke
		// the script — a sibling job or a workflow comment cannot satisfy this.
		block := stripHashComments(t, workflowJobBlock(t, workflow, job))
		if !strings.Contains(block, "bash "+scriptPath) {
			t.Errorf("ci.yml %q job does not invoke %s", job, scriptPath)
		}
		for _, testName := range tests {
			// Each root must be both selected by a -run pattern AND asserted
			// PASS by name; a bare mention or comment does not prove execution.
			if !strings.Contains(script, "-run") || !strings.Contains(script, testName) {
				t.Errorf("%s does not select %s via -run", scriptPath, testName)
			}
			if !strings.Contains(script, "ci_assert_test_passed") || !assertsTestPassed(script, testName) {
				t.Errorf("%s does not assert %s passed", scriptPath, testName)
			}
		}
	}
}

// assertsTestPassed reports whether a ci_assert_test_passed invocation names
// the root — matching the assertion, not a -run pattern or comment.
func assertsTestPassed(script, testName string) bool {
	for _, line := range strings.Split(script, "\n") {
		if strings.Contains(line, "ci_assert_test_passed") && strings.Contains(line, testName) {
			return true
		}
	}
	return false
}
