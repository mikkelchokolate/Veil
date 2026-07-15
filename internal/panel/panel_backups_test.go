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
		`id="backup-daily" type="number" min="0" max="365" value="7" required`,
		`id="backup-weekly" type="number" min="0" max="104" value="4" required`,
		`id="backup-monthly" type="number" min="0" max="120" value="12" required`,
		`id="backups-table-body"`,
		`id="backup-output"`,
		`veil backup schedule enable`,
		`Backup access requires the admin role`,
		`btn-create-backup" data-admin-only="true`,
		`btn-load-backups" class="secondary" data-admin-only="true`,
		`btn-prune-backups" class="danger" data-admin-only="true`,
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

func TestPanelBackupsRejectBlankRetentionAndSerializeOperations(t *testing.T) {
	actions := panelBackupsActionsJS()
	for _, want := range []string{
		`function backupRetentionValue(id, label)`,
		`raw === '' || !input.checkValidity()`,
		`Number.isInteger(value)`,
		`let backupOperationInFlight = false;`,
		`if (backupOperationInFlight) return null;`,
		`return await action();`,
		`backupOperationInFlight = false;`,
		`button.disabled = Boolean(disabled) || isViewerRole();`,
	} {
		if !strings.Contains(actions, want) {
			t.Fatalf("backup reliability missing %q", want)
		}
	}
	if strings.Contains(actions, `.value || 0`) {
		t.Fatal("blank retention values must not silently become zero")
	}
}

func TestPanelBackupsGuardStaleLoadsAndResetAuthAfterRestore(t *testing.T) {
	actions := panelBackupsActionsJS()
	for _, want := range []string{
		`const generation = ++backupLoadGeneration;`,
		`if (generation !== backupLoadGeneration) return;`,
		`renderBackupTableMessage(message, 'var(--accent-danger)')`,
		`const generation = ++backupRestorePollGeneration;`,
		`if (generation !== backupRestorePollGeneration) return;`,
		`localStorage.removeItem('veil_api_token');`,
		`localStorage.removeItem('veil_username');`,
		`setTimeout(() => URL.revokeObjectURL(url), 1000);`,
	} {
		if !strings.Contains(actions, want) {
			t.Fatalf("backup stale-state handling missing %q", want)
		}
	}
}
