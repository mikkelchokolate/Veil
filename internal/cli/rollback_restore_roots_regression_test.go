package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/backup"
)

// TestRollbackRestoreCoversTheSystemdDirByDefault locks #1446: install and
// repair snapshot the systemd units they overwrite, so the default restore
// allowlist has to include the unit directory. Before this the documented
// recovery command refused the whole backup with "outside the allowed roots"
// and restored nothing.
func TestRollbackRestoreCoversTheSystemdDirByDefault(t *testing.T) {
	root := t.TempDir()
	etcDir := filepath.Join(root, "etc")
	varDir := filepath.Join(root, "var")
	systemdDir := filepath.Join(root, "systemd")
	for _, dir := range []string{etcDir, varDir, systemdDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("VEIL_ETC_DIR", etcDir)
	t.Setenv("VEIL_VAR_DIR", varDir)
	originalSystemd := rollbackDefaultSystemdDir
	rollbackDefaultSystemdDir = systemdDir
	t.Cleanup(func() { rollbackDefaultSystemdDir = originalSystemd })

	envFile := filepath.Join(etcDir, "veil.env")
	unitFile := filepath.Join(systemdDir, "veil.service")
	originalEnv := "VEIL_LISTEN=127.0.0.1:2096\n"
	originalUnit := "[Unit]\nDescription=Veil panel\n"
	if err := os.WriteFile(envFile, []byte(originalEnv), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unitFile, []byte(originalUnit), 0o644); err != nil {
		t.Fatal(err)
	}

	backupDir := filepath.Join(varDir, "backups")
	backupID, err := backup.BackupBeforeApply([]string{envFile, unitFile}, backupDir)
	if err != nil {
		t.Fatalf("backup: %v", err)
	}
	// An install/repair overwrites both files.
	if err := os.WriteFile(envFile, []byte("NEW=1\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unitFile, []byte("[Unit]\nNEW\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := NewRootCommand("test")
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs([]string{"rollback", "restore", backupID, "--backup-dir", backupDir, "--yes"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("restore with the default roots: %v\n%s", err, out.String())
	}
	if body, err := os.ReadFile(envFile); err != nil || string(body) != originalEnv {
		t.Fatalf("veil.env not restored: %q err=%v", body, err)
	}
	if body, err := os.ReadFile(unitFile); err != nil || string(body) != originalUnit {
		t.Fatalf("unit file not restored: %q err=%v", body, err)
	}
	if !strings.Contains(out.String(), unitFile) {
		t.Fatalf("restore output does not list the unit file:\n%s", out.String())
	}
}

// TestRollbackRestoreStillRefusesUnmanagedDestinations keeps the #1219 guard:
// a manifest entry outside the managed roots is still refused, and the whole
// restore stays all-or-nothing.
func TestRollbackRestoreStillRefusesUnmanagedDestinations(t *testing.T) {
	root := t.TempDir()
	etcDir := filepath.Join(root, "etc")
	varDir := filepath.Join(root, "var")
	for _, dir := range []string{etcDir, varDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("VEIL_ETC_DIR", etcDir)
	t.Setenv("VEIL_VAR_DIR", varDir)
	originalSystemd := rollbackDefaultSystemdDir
	rollbackDefaultSystemdDir = filepath.Join(root, "systemd")
	t.Cleanup(func() { rollbackDefaultSystemdDir = originalSystemd })

	managed := filepath.Join(etcDir, "veil.env")
	unmanaged := filepath.Join(t.TempDir(), "outside.conf")
	if err := os.WriteFile(managed, []byte("original\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unmanaged, []byte("original\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	backupDir := filepath.Join(varDir, "backups")
	backupID, err := backup.BackupBeforeApply([]string{managed, unmanaged}, backupDir)
	if err != nil {
		t.Fatalf("backup: %v", err)
	}
	if err := os.WriteFile(managed, []byte("modified\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unmanaged, []byte("modified\n"), 0o640); err != nil {
		t.Fatal(err)
	}

	cmd := NewRootCommand("test")
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs([]string{"rollback", "restore", backupID, "--backup-dir", backupDir, "--yes"})
	err = cmd.Execute()
	if err == nil {
		t.Fatalf("restore of an unmanaged destination succeeded:\n%s", out.String())
	}
	if !strings.Contains(err.Error(), "outside the allowed roots") {
		t.Fatalf("error = %v, want the allowed-roots refusal", err)
	}
	if body, err := os.ReadFile(managed); err != nil || string(body) != "modified\n" {
		t.Fatalf("the all-or-nothing restore touched the managed file: %q err=%v", body, err)
	}
}

// TestRollbackRestoreRejectsForgedUnitNames locks the #1219 side of #1446: the
// unit directory is in the defaults, but a manifest may only write Veil's own
// units there. A forged `evil-payload.service` would be root-executed code
// (daemon-reload + enable/start, or the next boot).
func TestRollbackRestoreRejectsForgedUnitNames(t *testing.T) {
	root := t.TempDir()
	etcDir := filepath.Join(root, "etc")
	varDir := filepath.Join(root, "var")
	systemdDir := filepath.Join(root, "systemd")
	for _, dir := range []string{etcDir, varDir, systemdDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("VEIL_ETC_DIR", etcDir)
	t.Setenv("VEIL_VAR_DIR", varDir)
	originalSystemd := rollbackDefaultSystemdDir
	rollbackDefaultSystemdDir = systemdDir
	t.Cleanup(func() { rollbackDefaultSystemdDir = originalSystemd })

	unitFile := filepath.Join(systemdDir, "veil.service")
	if err := os.WriteFile(unitFile, []byte("[Unit]\nDescription=Veil panel\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	backupDir := filepath.Join(varDir, "backups")
	backupID, err := backup.BackupBeforeApply([]string{unitFile}, backupDir)
	if err != nil {
		t.Fatalf("backup: %v", err)
	}

	// Tamper with the manifest: keep the member, aim it at a unit Veil does not
	// manage.
	manifestPath := filepath.Join(backupDir, backupID, "manifest.json")
	body, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest backup.Manifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Entries) != 1 {
		t.Fatalf("manifest entries = %d", len(manifest.Entries))
	}
	manifest.Entries[0].OriginalPath = filepath.Join(systemdDir, "evil-payload.service")
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := NewRootCommand("test")
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs([]string{"rollback", "restore", backupID, "--backup-dir", backupDir, "--yes"})
	err = cmd.Execute()
	if err == nil {
		t.Fatalf("a forged manifest entry was restored:\n%s", out.String())
	}
	if !strings.Contains(err.Error(), "not a managed path") {
		t.Fatalf("error = %v, want the managed-path refusal", err)
	}
	if _, statErr := os.Stat(filepath.Join(systemdDir, "evil-payload.service")); !os.IsNotExist(statErr) {
		t.Fatalf("forged unit was written: %v", statErr)
	}

	// The managed name and its drop-in still restore.
	dropInDir := filepath.Join(systemdDir, "veil-helper.service.d")
	if err := os.MkdirAll(dropInDir, 0o755); err != nil {
		t.Fatal(err)
	}
	dropIn := filepath.Join(dropInDir, "10-veil-install.conf")
	originalDropIn := "[Service]\nEnvironmentFile=-/etc/veil/veil.env\n"
	if err := os.WriteFile(dropIn, []byte(originalDropIn), 0o644); err != nil {
		t.Fatal(err)
	}
	secondID, err := backup.BackupBeforeApply([]string{unitFile, dropIn}, backupDir)
	if err != nil {
		t.Fatalf("second backup: %v", err)
	}
	if err := os.WriteFile(unitFile, []byte("[Unit]\nNEW\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dropIn, []byte("[Service]\nNEW\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	restore := NewRootCommand("test")
	restoreOut := &bytes.Buffer{}
	restore.SetOut(restoreOut)
	restore.SetErr(restoreOut)
	restore.SetArgs([]string{"rollback", "restore", secondID, "--backup-dir", backupDir, "--yes"})
	if err := restore.Execute(); err != nil {
		t.Fatalf("managed unit and drop-in refused: %v\n%s", err, restoreOut.String())
	}
	if body, err := os.ReadFile(dropIn); err != nil || string(body) != originalDropIn {
		t.Fatalf("drop-in not restored: %q err=%v", body, err)
	}
}
