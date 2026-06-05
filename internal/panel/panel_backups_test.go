package panel

import (
	"strings"
	"testing"
)

func TestPanelBackupsCardRendersRecoveryControls(t *testing.T) {
	card := panelBackupsCardHTML()
	for _, want := range []string{
		`id="btn-create-backup"`,
		`id="btn-load-backups"`,
		`id="btn-prune-backups"`,
		`id="backup-daily"`,
		`id="backup-weekly"`,
		`id="backup-monthly"`,
		`id="backups-table-body"`,
		`id="backup-output"`,
		`veil backup schedule enable`,
	} {
		if !strings.Contains(card, want) {
			t.Fatalf("backup card missing %q", want)
		}
	}
}

func TestPanelBackupsActionsUseServerSideSecretAndQueuedRestore(t *testing.T) {
	actions := panelBackupsActionsJS()
	for _, want := range []string{
		`async function loadBackups()`,
		`async function createBackup()`,
		`async function pruneBackups()`,
		`/api/backups/`,
		`/verify`,
		`/download`,
		`/restore`,
		`/api/backups/restore-jobs/`,
		`confirm: true`,
	} {
		if !strings.Contains(actions, want) {
			t.Fatalf("backup actions missing %q", want)
		}
	}
	for _, forbidden := range []string{"passphrase:", "backup.passphrase"} {
		if strings.Contains(actions, forbidden) {
			t.Fatalf("browser code must not handle server-side passphrase: %q", forbidden)
		}
	}
}
