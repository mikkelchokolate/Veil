package installer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var _apply_backup_deps = []any{
	os.ReadFile, filepath.Join, strings.Contains, testing.T{},
}

func TestApplyWithBackupDirBacksUpExistingFilesBeforeOverwrite(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backups")

	profile, err := BuildRURecommendedProfile(RURecommendedInput{
		PanelAccess: "caddy",
		Domain:      "example.com",
		Email:       "admin@example.com",
		Secret:      func(label string) string { return "secret-" + label },
		PanelPort:   2096,
	})
	if err != nil {
		t.Fatalf("build profile: %v", err)
	}

	etcDir := filepath.Join(dir, "etc", "veil")
	varDir := filepath.Join(dir, "var", "lib", "veil")
	systemdDir := filepath.Join(dir, "etc", "systemd", "system")

	// Pre-create a file that will be overwritten
	caddyfileDir := filepath.Join(etcDir, "generated", "caddy")
	if err := os.MkdirAll(caddyfileDir, 0o755); err != nil {
		t.Fatalf("mkdir caddy dir: %v", err)
	}
	oldCaddyPath := filepath.Join(caddyfileDir, "Caddyfile")
	if err := os.WriteFile(oldCaddyPath, []byte("old caddy content"), 0o600); err != nil {
		t.Fatalf("write old caddy: %v", err)
	}

	result, err := ApplyRURecommendedProfile(profile, ApplyPaths{
		EtcDir:     etcDir,
		VarDir:     varDir,
		SystemdDir: systemdDir,
		BackupDir:  backupDir,
	})
	if err != nil {
		t.Fatalf("apply profile: %v", err)
	}

	if result.BackupID == "" {
		t.Fatalf("expected BackupID to be set when BackupDir is provided")
	}

	// Verify backup contains old Caddyfile
	backupPath := filepath.Join(backupDir, result.BackupID, "Caddyfile")
	body, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatalf("read backup caddyfile: %v", err)
	}
	if string(body) != "old caddy content" {
		t.Fatalf("backup has wrong content: %q", string(body))
	}

	// Verify current Caddyfile has new content
	assertFileContains(t, oldCaddyPath, "reverse_proxy")
}

func TestApplyWithoutBackupDirDoesNotBackup(t *testing.T) {
	dir := t.TempDir()

	profile, err := BuildRURecommendedProfile(RURecommendedInput{
		Domain: "example.com",
		Email:  "admin@example.com",
		Secret: func(label string) string { return "secret-" + label },
	})
	if err != nil {
		t.Fatalf("build profile: %v", err)
	}

	result, err := ApplyRURecommendedProfile(profile, ApplyPaths{
		EtcDir:     filepath.Join(dir, "etc", "veil"),
		VarDir:     filepath.Join(dir, "var", "lib", "veil"),
		SystemdDir: filepath.Join(dir, "etc", "systemd", "system"),
	})
	if err != nil {
		t.Fatalf("apply profile: %v", err)
	}

	if result.BackupID != "" {
		t.Fatalf("expected BackupID to be empty when BackupDir is not set, got %q", result.BackupID)
	}
}

func TestApplyBackupThenRestoreRollback(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backups")

	profile, err := BuildRURecommendedProfile(RURecommendedInput{
		PanelAccess: "caddy",
		Domain:      "example.com",
		Email:       "admin@example.com",
		Secret:      func(label string) string { return "secret-" + label },
		PanelPort:   2096,
	})
	if err != nil {
		t.Fatalf("build profile: %v", err)
	}

	etcDir := filepath.Join(dir, "etc", "veil")
	varDir := filepath.Join(dir, "var", "lib", "veil")
	systemdDir := filepath.Join(dir, "etc", "systemd", "system")

	// Pre-create files that will be overwritten
	caddyfileDir := filepath.Join(etcDir, "generated", "caddy")
	if err := os.MkdirAll(caddyfileDir, 0o755); err != nil {
		t.Fatalf("mkdir caddy dir: %v", err)
	}
	oldCaddyPath := filepath.Join(caddyfileDir, "Caddyfile")
	if err := os.WriteFile(oldCaddyPath, []byte("pre-apply content"), 0o600); err != nil {
		t.Fatalf("write old caddy: %v", err)
	}

	veilEnvPath := filepath.Join(etcDir, "veil.env")
	if err := os.MkdirAll(filepath.Dir(veilEnvPath), 0o755); err != nil {
		t.Fatalf("mkdir veil env dir: %v", err)
	}
	if err := os.WriteFile(veilEnvPath, []byte("VEIL_API_TOKEN=old-token\n"), 0o600); err != nil {
		t.Fatalf("write old env: %v", err)
	}

	// Apply with backup
	result, err := ApplyRURecommendedProfile(profile, ApplyPaths{
		EtcDir:     etcDir,
		VarDir:     varDir,
		SystemdDir: systemdDir,
		BackupDir:  backupDir,
	})
	if err != nil {
		t.Fatalf("apply profile: %v", err)
	}

	// Verify files were overwritten
	body, _ := os.ReadFile(oldCaddyPath)
	if string(body) == "pre-apply content" {
		t.Fatalf("Caddyfile should have been overwritten")
	}
	assertFileContains(t, oldCaddyPath, "reverse_proxy")

	body, _ = os.ReadFile(veilEnvPath)
	if string(body) == "VEIL_API_TOKEN=old-token\n" {
		t.Fatalf("veil.env should have been overwritten")
	}
	assertFileContains(t, veilEnvPath, "VEIL_API_TOKEN=secret-panel")

	// Rollback: restore from backup
	restored, err := RestoreFromBackup(backupDir, result.BackupID)
	if err != nil {
		t.Fatalf("restore from backup: %v", err)
	}
	if len(restored) < 2 {
		t.Fatalf("expected at least 2 restored files, got %d: %v", len(restored), restored)
	}

	// Verify original content is back
	body, err = os.ReadFile(oldCaddyPath)
	if err != nil {
		t.Fatalf("read restored caddy: %v", err)
	}
	if string(body) != "pre-apply content" {
		t.Fatalf("restored Caddyfile has wrong content: %q", string(body))
	}

	body, err = os.ReadFile(veilEnvPath)
	if err != nil {
		t.Fatalf("read restored veil.env: %v", err)
	}
	if string(body) != "VEIL_API_TOKEN=old-token\n" {
		t.Fatalf("restored veil.env has wrong content: %q", string(body))
	}

	// Cleanup backup
	if err := CleanupBackup(backupDir, result.BackupID); err != nil {
		t.Fatalf("cleanup backup: %v", err)
	}

	// Verify backup is gone
	if _, err := os.Stat(filepath.Join(backupDir, result.BackupID)); !os.IsNotExist(err) {
		t.Fatalf("backup should be cleaned up")
	}
}

func TestApplyBackupSkipsNewFilesThatDidNotExist(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backups")

	profile, err := BuildRURecommendedProfile(RURecommendedInput{
		Domain: "example.com",
		Email:  "admin@example.com",
		Secret: func(label string) string { return "secret-" + label },
	})
	if err != nil {
		t.Fatalf("build profile: %v", err)
	}

	result, err := ApplyRURecommendedProfile(profile, ApplyPaths{
		EtcDir:     filepath.Join(dir, "etc", "veil"),
		VarDir:     filepath.Join(dir, "var", "lib", "veil"),
		SystemdDir: filepath.Join(dir, "etc", "systemd", "system"),
		BackupDir:  backupDir,
	})
	if err != nil {
		t.Fatalf("apply profile: %v", err)
	}

	if result.BackupID == "" {
		t.Fatalf("expected BackupID to be set")
	}

	// All files are new, so backup should be nearly empty
	backupPath := filepath.Join(backupDir, result.BackupID)
	entries, err := os.ReadDir(backupPath)
	if err != nil {
		t.Fatalf("read backup dir: %v", err)
	}
	fileCount := 0
	for _, e := range entries {
		if e.Name() != "manifest.json" {
			fileCount++
		}
	}
	if fileCount != 0 {
		t.Fatalf("expected no backed up files for fresh apply, got %d: %v", fileCount, entries)
	}
}
