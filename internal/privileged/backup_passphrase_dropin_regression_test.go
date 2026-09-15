package privileged

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/backup"
)

func TestLoadBackupPassphraseUsesScheduledDropInForDefaultPath(t *testing.T) {
	systemdDir := t.TempDir()
	customPath := filepath.Join(systemdDir, "custom.passphrase")
	if err := os.WriteFile(customPath, []byte("custom-path-passphrase-value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dropInDir := filepath.Join(systemdDir, backup.ScheduleDropInDir)
	if err := os.MkdirAll(dropInDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "[Service]\nExecStart=/usr/local/bin/veil backup create --passphrase-file " + filepath.ToSlash(customPath) + " --prune\n"
	if err := os.WriteFile(filepath.Join(dropInDir, backup.ScheduleDropInName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	oldDir := backupSystemdDir
	backupSystemdDir = systemdDir
	t.Cleanup(func() { backupSystemdDir = oldDir })

	got, err := loadBackupPassphrase(ResolvedBackup{Action: BackupActionVerify, BackupPassphrasePath: "/etc/veil/backup.passphrase"})
	if err != nil {
		t.Fatalf("loadBackupPassphrase: %v", err)
	}
	if got != "custom-path-passphrase-value" {
		t.Fatalf("passphrase = %q, want custom drop-in secret", got)
	}
}

func TestLoadBackupPassphraseKeepsExplicitNonDefaultPath(t *testing.T) {
	systemdDir := t.TempDir()
	customPath := filepath.Join(systemdDir, "custom.passphrase")
	explicitPath := filepath.Join(systemdDir, "explicit.passphrase")
	if err := os.WriteFile(customPath, []byte("drop-in-passphrase-value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(explicitPath, []byte("explicit-passphrase-value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dropInDir := filepath.Join(systemdDir, backup.ScheduleDropInDir)
	if err := os.MkdirAll(dropInDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "[Service]\nExecStart=/usr/local/bin/veil backup create --passphrase-file " + filepath.ToSlash(customPath) + " --prune\n"
	if err := os.WriteFile(filepath.Join(dropInDir, backup.ScheduleDropInName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	oldDir := backupSystemdDir
	backupSystemdDir = systemdDir
	t.Cleanup(func() { backupSystemdDir = oldDir })

	got, err := loadBackupPassphrase(ResolvedBackup{Action: BackupActionVerify, BackupPassphrasePath: explicitPath})
	if err != nil {
		t.Fatalf("loadBackupPassphrase: %v", err)
	}
	if got != "explicit-passphrase-value" {
		t.Fatalf("passphrase = %q, want explicit path secret", got)
	}
}
