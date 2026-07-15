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
	} {
		if !strings.Contains(reliability, want) {
			t.Fatalf("backup restore polling reliability missing %q", want)
		}
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
