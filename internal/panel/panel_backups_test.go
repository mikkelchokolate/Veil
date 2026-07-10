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
		`<button type="button" id="btn-create-backup" data-admin-only="true">`,
		`<button type="button" id="btn-load-backups" class="secondary" data-admin-only="true">`,
		`<button type="button" id="btn-prune-backups" class="danger" data-admin-only="true">`,
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
