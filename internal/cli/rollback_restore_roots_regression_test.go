package cli

import (
	"bytes"
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
