package panel

import (
	"strings"
	"testing"
)

func TestPanelBackupRestorePollingRetriesUntilTerminalState(t *testing.T) {
	actions := BackupsActionsJS()
	marker := `const baseSetBackupControlsDisabled = setBackupControlsDisabled;`
	start := strings.Index(actions, marker)
	if start < 0 {
		t.Fatalf("backup restore polling reliability missing %q", marker)
	}
	reliability := actions[start:]
	for _, want := range []string{
		marker,
		`refreshButton.disabled = Boolean(disabled) || isViewerRole();`,
		`pollBackupRestore = async function(id, generation)`,
		`while (generation === backupRestorePollGeneration)`,
		`const delay = attempt < 120 ? 1000 : 5000;`,
		`response.status === 401 || response.status === 403 || response.status === 404`,
		`Restore status check failed; continuing to retry`,
		`continue;`,
		`Invalid backup restore status response.`,
		`clearStoredPanelIdentity();`,
		`window.location.reload();`,
		`job.status === 'failed' || job.status === 'degraded' || job.status === 'pending'`,
	} {
		if !strings.Contains(reliability, want) {
			t.Fatalf("backup restore polling reliability missing %q", want)
		}
	}
	// The stored panel identity and the reload that follows it must be gated
	// behind a terminal 'succeeded' job — clearing identity on any earlier
	// status would log the operator out of a still-broken restore (#848).
	succeeded := strings.Index(reliability, `job.status === 'succeeded'`)
	clearIdentity := strings.Index(reliability, `clearStoredPanelIdentity();`)
	reload := strings.Index(reliability, `window.location.reload();`)
	if succeeded < 0 {
		t.Fatal("backup restore polling missing the job.status === 'succeeded' branch")
	}
	if clearIdentity < succeeded || reload < succeeded {
		t.Fatal("clearStoredPanelIdentity/reload must run inside the succeeded branch, after the status check")
	}
	if strings.Contains(reliability, `for (let attempt = 0; attempt < 120; attempt += 1)`) {
		t.Fatal("backup restore polling override still releases the UI lock after a fixed timeout")
	}
}

func TestExportedBackupActionsMountReliabilityOnce(t *testing.T) {
	actions := BackupsActionsJS()
	if got := strings.Count(actions, `const baseSetBackupControlsDisabled = setBackupControlsDisabled;`); got != 1 {
		t.Fatalf("backup reliability runtime count = %d, want 1", got)
	}
}
