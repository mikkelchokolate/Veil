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
	// The default comparison is hostenv-derived (#661) — pin the install
	// root so a stray VEIL_ETC_DIR in the environment cannot move it.
	t.Setenv("VEIL_ETC_DIR", "/etc/veil")

	got, err := loadBackupPassphrase(ResolvedBackup{Action: BackupActionVerify, BackupPassphrasePath: "/etc/veil/backup.passphrase"})
	if err != nil {
		t.Fatalf("loadBackupPassphrase: %v", err)
	}
	if got != "custom-path-passphrase-value" {
		t.Fatalf("passphrase = %q, want custom drop-in secret", got)
	}
}

// Regression for #661: on a custom --etc-dir install the policy fallback is
// <etc>/backup.passphrase under that root — not /etc/veil/backup.passphrase —
// but it is still the *default* location for that install, so a
// schedule-relocated passphrase (drop-in --passphrase-file) must win exactly
// like on packaged installs.
func TestLoadBackupPassphraseUsesScheduledDropInForCustomEtcDefault(t *testing.T) {
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

	// The helper resolves its install root from the environment the unit
	// exports — VEIL_ETC_DIR here — so the custom-root default is
	// <etc>/backup.passphrase.
	customEtc := filepath.Join(t.TempDir(), "veil-etc")
	t.Setenv("VEIL_ETC_DIR", customEtc)

	got, err := loadBackupPassphrase(ResolvedBackup{Action: BackupActionVerify, BackupPassphrasePath: filepath.Join(customEtc, "backup.passphrase")})
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
