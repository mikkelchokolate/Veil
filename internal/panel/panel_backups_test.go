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
		`Backup access requires the admin role`,
	} {
		if !strings.Contains(card, want) {
			t.Fatalf("backup card missing %q", want)
		}
	}
	for _, id := range []string{"btn-create-backup", "btn-load-backups", "btn-prune-backups"} {
		needle := `id="` + id + `"`
		index := strings.Index(card, needle)
		if index < 0 {
			t.Fatalf("backup card missing %s", needle)
		}
		end := index + 180
		if end > len(card) {
			end = len(card)
		}
		if !strings.Contains(card[index:end], `data-admin-only="true"`) {
			t.Fatalf("backup control %s must be admin-only", id)
		}
	}
}

func TestPanelBackupsActionsUseServerSideSecretAndQueuedRestore(t *testing.T) {
	actions := panelBackupsActionsJS()
	for _, want := range []string{
		`async function loadBackups()`,
		`async function createBackup()`,
		`async function pruneBackups()`,
		`formatAPIError(text, response.status)`,
		`/api/backups/`,
		`/verify`,
		`/download`,
		`/restore`,
		`/api/backup-restore-jobs/`,
		`confirm: true`,
		`if (isViewerRole())`,
		`verify.dataset.adminOnly = 'true'`,
		`download.dataset.adminOnly = 'true'`,
		`restore.dataset.adminOnly = 'true'`,
		`catch (err)`,
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
