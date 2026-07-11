package panel

import (
	"strings"
	"testing"
)

func TestPanelBackupRestorePollingRetriesTransientFailures(t *testing.T) {
	actions := BackupsActionsJS()
	for _, want := range []string{
		`const baseSetBackupControlsDisabled = setBackupControlsDisabled;`,
		`refreshButton.disabled = Boolean(disabled) || isViewerRole();`,
		`pollBackupRestore = async function(id, generation)`,
		`response.status === 401 || response.status === 403 || response.status === 404`,
		`Restore status check failed; retrying`,
		`continue;`,
		`Invalid backup restore status response.`,
		`clearStoredPanelIdentity();`,
		`Last polling error:`,
	} {
		if !strings.Contains(actions, want) {
			t.Fatalf("backup restore polling reliability missing %q", want)
		}
	}
}

func TestExportedBackupActionsMountReliabilityOnce(t *testing.T) {
	actions := BackupsActionsJS()
	if got := strings.Count(actions, `const baseSetBackupControlsDisabled = setBackupControlsDisabled;`); got != 1 {
		t.Fatalf("backup reliability runtime count = %d, want 1", got)
	}
}
