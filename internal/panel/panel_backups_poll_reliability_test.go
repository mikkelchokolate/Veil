package panel

import (
	"strings"
	"testing"
)

func TestPanelBackupRestorePollingRetriesUntilTerminalState(t *testing.T) {
	actions := BackupsActionsJS()
	for _, want := range []string{
		`const baseSetBackupControlsDisabled = setBackupControlsDisabled;`,
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
		if !strings.Contains(actions, want) {
			t.Fatalf("backup restore polling reliability missing %q", want)
		}
	}
	if strings.Contains(actions, `for (let attempt = 0; attempt < 120; attempt += 1)`) {
		t.Fatal("backup restore polling still releases the UI lock after a fixed timeout")
	}
}

func TestExportedBackupActionsMountReliabilityOnce(t *testing.T) {
	actions := BackupsActionsJS()
	if got := strings.Count(actions, `const baseSetBackupControlsDisabled = setBackupControlsDisabled;`); got != 1 {
		t.Fatalf("backup reliability runtime count = %d, want 1", got)
	}
}
