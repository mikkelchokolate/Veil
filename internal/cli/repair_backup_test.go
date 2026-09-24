package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// backupIDFromOutput extracts the printed backup id so tests can verify the
// archive actually exists on disk instead of trusting stdout.
func backupIDFromOutput(t *testing.T, output string) string {
	t.Helper()
	for _, line := range strings.Split(output, "\n") {
		if id, ok := strings.CutPrefix(strings.TrimSpace(line), "Backup ID: "); ok {
			return strings.TrimSpace(id)
		}
	}
	t.Fatalf("output has no 'Backup ID:' line:\n%s", output)
	return ""
}

// assertBackupDirHasArchive locks the on-disk contract behind "Backup ID:":
// the id resolves to a directory containing a manifest.json.
func assertBackupDirHasArchive(t *testing.T, backupDir, backupID string) {
	t.Helper()
	if backupID == "" {
		t.Fatal("expected a backup id")
	}
	archive := filepath.Join(backupDir, backupID)
	info, err := os.Stat(archive)
	if err != nil || !info.IsDir() {
		t.Fatalf("backup archive %q missing: %v", archive, err)
	}
	if _, err := os.Stat(filepath.Join(archive, "manifest.json")); err != nil {
		t.Fatalf("backup archive %q has no manifest.json: %v", archive, err)
	}
}

// assertNoBackupArtifacts locks the negative side: neither an explicit backup
// dir nor the default <varDir>/backups may gain an archive.
func assertNoBackupArtifacts(t *testing.T, dirs ...string) {
	t.Helper()
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatalf("read backup dir %q: %v", dir, err)
		}
		if len(entries) != 0 {
			t.Fatalf("backup artifacts must not be created under %q, found %v", dir, entries)
		}
	}
}

func TestRepairWithBackupDirPrintsBackupID(t *testing.T) {
	dir := t.TempDir()
	etcDir := filepath.Join(dir, "etc", "veil")
	varDir := filepath.Join(dir, "var", "lib", "veil")
	systemdDir := filepath.Join(dir, "etc", "systemd", "system")
	backupDir := filepath.Join(dir, "backups")

	// Pre-create a file with wrong content so repair plan detects drift
	caddyfileDir := filepath.Join(etcDir, "generated", "caddy")
	if err := os.MkdirAll(caddyfileDir, 0o755); err != nil {
		t.Fatalf("mkdir caddy dir: %v", err)
	}
	caddyfilePath := filepath.Join(caddyfileDir, "Caddyfile")
	if err := os.WriteFile(caddyfilePath, []byte("old-drifting-content"), 0o600); err != nil {
		t.Fatalf("write caddyfile: %v", err)
	}

	// Also pre-create veil.env with old content to ensure drift detection
	veilEnvPath := filepath.Join(etcDir, "veil.env")
	if err := os.MkdirAll(filepath.Dir(veilEnvPath), 0o755); err != nil {
		t.Fatalf("mkdir veil env dir: %v", err)
	}
	if err := os.WriteFile(veilEnvPath, []byte("VEIL_API_TOKEN=old-token\n"), 0o600); err != nil {
		t.Fatalf("write veil.env: %v", err)
	}

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"repair",
		"--profile", "ru-recommended",
		"--yes",
		"--backup-dir", backupDir,
		"--etc-dir", etcDir,
		"--var-dir", varDir,
		"--systemd-dir", systemdDir,
	})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v\noutput: %s", err, out.String())
	}
	backupID := backupIDFromOutput(t, out.String())
	assertBackupDirHasArchive(t, backupDir, backupID)
	// The drifted managed file must be recoverable: the archive holds a
	// member with the pre-repair content plus a manifest entry mapping it
	// back to the live path. (The stray Caddyfile is NOT a managed file, so
	// it is deliberately absent — only managed drift is backed up.)
	archive := filepath.Join(backupDir, backupID)
	entries, err := os.ReadDir(archive)
	if err != nil {
		t.Fatalf("read backup archive: %v", err)
	}
	foundDrifted := false
	for _, entry := range entries {
		if entry.Name() == "manifest.json" {
			continue
		}
		body, err := os.ReadFile(filepath.Join(archive, entry.Name()))
		if err != nil {
			t.Fatalf("read backup member %q: %v", entry.Name(), err)
		}
		if string(body) == "VEIL_API_TOKEN=old-token\n" {
			foundDrifted = true
		}
	}
	if !foundDrifted {
		t.Fatalf("backup archive %q contains no member with the pre-repair veil.env content", archive)
	}
	manifestBody, err := os.ReadFile(filepath.Join(archive, "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if !strings.Contains(string(manifestBody), filepath.ToSlash(veilEnvPath)) && !strings.Contains(string(manifestBody), veilEnvPath) {
		t.Fatalf("manifest does not map a member back to %s:\n%s", veilEnvPath, manifestBody)
	}
}

func TestRepairDryRunDoesNotCreateBackup(t *testing.T) {
	dir := t.TempDir()
	etcDir := filepath.Join(dir, "etc", "veil")
	varDir := filepath.Join(dir, "var", "lib", "veil")
	systemdDir := filepath.Join(dir, "etc", "systemd", "system")
	backupDir := filepath.Join(dir, "backups")

	// Pre-create a drifted file
	caddyfileDir := filepath.Join(etcDir, "generated", "caddy")
	if err := os.MkdirAll(caddyfileDir, 0o755); err != nil {
		t.Fatalf("mkdir caddy dir: %v", err)
	}
	caddyfilePath := filepath.Join(caddyfileDir, "Caddyfile")
	if err := os.WriteFile(caddyfilePath, []byte("old-drifting-content"), 0o600); err != nil {
		t.Fatalf("write caddyfile: %v", err)
	}

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"repair",
		"--profile", "ru-recommended",
		"--dry-run",
		"--backup-dir", backupDir,
		"--etc-dir", etcDir,
		"--var-dir", varDir,
		"--systemd-dir", systemdDir,
	})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v\noutput: %s", err, out.String())
	}
	if strings.Contains(out.String(), "Backup ID:") {
		t.Fatalf("expected output to NOT contain 'Backup ID:' in dry-run mode, got:\n%s", out.String())
	}
	// Stdout alone cannot prove no backup ran — a silent write would still
	// pass. Assert the backup dir stays absent/empty on disk.
	assertNoBackupArtifacts(t, backupDir)
}

func TestRepairDefaultsBackupDirWhenNotSet(t *testing.T) {
	dir := t.TempDir()
	etcDir := filepath.Join(dir, "etc", "veil")
	varDir := filepath.Join(dir, "var", "lib", "veil")
	systemdDir := filepath.Join(dir, "etc", "systemd", "system")

	// Pre-create a drifted file so repair has actions
	caddyfileDir := filepath.Join(etcDir, "generated", "caddy")
	if err := os.MkdirAll(caddyfileDir, 0o755); err != nil {
		t.Fatalf("mkdir caddy dir: %v", err)
	}
	caddyfilePath := filepath.Join(caddyfileDir, "Caddyfile")
	if err := os.WriteFile(caddyfilePath, []byte("old-drifting-content"), 0o600); err != nil {
		t.Fatalf("write caddyfile: %v", err)
	}

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"repair",
		"--profile", "ru-recommended",
		"--yes",
		"--etc-dir", etcDir,
		"--var-dir", varDir,
		"--systemd-dir", systemdDir,
	})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error without --backup-dir: %v\noutput: %s", err, out.String())
	}
	// Verify repair still works (files were written)
	if !strings.Contains(out.String(), "Repaired files:") {
		t.Fatalf("expected 'Repaired files:' in output, got:\n%s", out.String())
	}
	// Should contain backup ID since default backup-dir is var-dir/backups
	backupID := backupIDFromOutput(t, out.String())
	assertBackupDirHasArchive(t, filepath.Join(varDir, "backups"), backupID)
}

func TestRepairExplicitEmptyBackupDirSkipsBackup(t *testing.T) {
	dir := t.TempDir()
	etcDir := filepath.Join(dir, "etc", "veil")
	varDir := filepath.Join(dir, "var", "lib", "veil")
	systemdDir := filepath.Join(dir, "etc", "systemd", "system")

	// Pre-create a drifted file so repair has actions
	caddyfileDir := filepath.Join(etcDir, "generated", "caddy")
	if err := os.MkdirAll(caddyfileDir, 0o755); err != nil {
		t.Fatalf("mkdir caddy dir: %v", err)
	}
	caddyfilePath := filepath.Join(caddyfileDir, "Caddyfile")
	if err := os.WriteFile(caddyfilePath, []byte("old-drifting-content"), 0o600); err != nil {
		t.Fatalf("write caddyfile: %v", err)
	}

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"repair",
		"--profile", "ru-recommended",
		"--yes",
		"--backup-dir", "",
		"--etc-dir", etcDir,
		"--var-dir", varDir,
		"--systemd-dir", systemdDir,
	})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error with --backup-dir '': %v\noutput: %s", err, out.String())
	}
	// Verify repair still works
	if !strings.Contains(out.String(), "Repaired files:") {
		t.Fatalf("expected 'Repaired files:' in output, got:\n%s", out.String())
	}
	// Should NOT contain backup ID since --backup-dir "" explicitly disables backup
	if strings.Contains(out.String(), "Backup ID:") {
		t.Fatalf("expected no 'Backup ID:' with --backup-dir '', got:\n%s", out.String())
	}
	// Stdout-only cannot prove the skip: the default <varDir>/backups and any
	// stray dir under the temp root must stay without archives.
	assertNoBackupArtifacts(t, filepath.Join(varDir, "backups"))
}

func TestRepairWithBackupDirNoFilesToRepair(t *testing.T) {
	dir := t.TempDir()
	etcDir := filepath.Join(dir, "etc", "veil")
	varDir := filepath.Join(dir, "var", "lib", "veil")
	systemdDir := filepath.Join(dir, "etc", "systemd", "system")
	backupDir := filepath.Join(dir, "backups")

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"repair",
		"--profile", "ru-recommended",
		"--yes",
		"--backup-dir", backupDir,
		"--etc-dir", etcDir,
		"--var-dir", varDir,
		"--systemd-dir", systemdDir,
	})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v\noutput: %s", err, out.String())
	}

	// First pass: files are missing so backup IS created (Backup ID: appears)
	backupID := backupIDFromOutput(t, out.String())
	assertBackupDirHasArchive(t, backupDir, backupID)
	// "No backup created" should not appear when there are actions
	if strings.Contains(out.String(), "No backup created") {
		t.Fatalf("expected no 'No backup created' when actions exist, got:\n%s", out.String())
	}
}

func TestRepairApplyWithAuditLogWritesSuccessEventWithBackupID(t *testing.T) {
	dir := t.TempDir()
	etcDir := filepath.Join(dir, "etc", "veil")
	varDir := filepath.Join(dir, "var", "lib", "veil")
	systemdDir := filepath.Join(dir, "etc", "systemd", "system")
	backupDir := filepath.Join(dir, "backups")
	auditPath := filepath.Join(dir, "audit.jsonl")

	// Pre-create a drifted file so repair has actions
	caddyfileDir := filepath.Join(etcDir, "generated", "caddy")
	if err := os.MkdirAll(caddyfileDir, 0o755); err != nil {
		t.Fatalf("mkdir caddy dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(caddyfileDir, "Caddyfile"), []byte("old-drifting-content"), 0o600); err != nil {
		t.Fatalf("write caddyfile: %v", err)
	}
	// Also pre-create veil.env with old content
	veilEnvPath := filepath.Join(etcDir, "veil.env")
	if err := os.MkdirAll(filepath.Dir(veilEnvPath), 0o755); err != nil {
		t.Fatalf("mkdir veil env dir: %v", err)
	}
	if err := os.WriteFile(veilEnvPath, []byte("VEIL_API_TOKEN=old-token\n"), 0o600); err != nil {
		t.Fatalf("write veil.env: %v", err)
	}

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"repair",
		"--profile", "ru-recommended",
		"--yes",
		"--backup-dir", backupDir,
		"--audit-log", auditPath,
		"--etc-dir", etcDir,
		"--var-dir", varDir,
		"--systemd-dir", systemdDir,
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, out.String())
	}

	// Verify audit log exists with success event
	events := readAuditLog(t, auditPath)
	if len(events) != 1 {
		t.Fatalf("expected 1 audit event, got %d", len(events))
	}
	ev := events[0]
	if ev["action"] != "repair.apply" {
		t.Fatalf("expected action 'repair.apply', got %v", ev["action"])
	}
	if ev["success"] != true {
		t.Fatalf("expected success=true, got %v", ev["success"])
	}
	if ev["timestamp"] == nil || ev["timestamp"] == "" {
		t.Fatalf("expected non-empty timestamp")
	}
	// backupID must be set since --backup-dir was provided
	if ev["backupID"] == nil || ev["backupID"] == "" {
		t.Fatalf("expected non-empty backupID, got %v", ev["backupID"])
	}
	wf, ok := ev["writtenFiles"].([]interface{})
	if !ok {
		t.Fatalf("expected writtenFiles array, got %T", ev["writtenFiles"])
	}
	if len(wf) == 0 {
		t.Fatalf("expected non-empty writtenFiles, got %v", wf)
	}
}

func TestRepairApplyBackupFailureWithAuditLog(t *testing.T) {
	dir := t.TempDir()
	etcDir := filepath.Join(dir, "etc", "veil")
	varDir := filepath.Join(dir, "var", "lib", "veil")
	systemdDir := filepath.Join(dir, "etc", "systemd", "system")

	// Pre-create a drifted file so repair has actions (needed for backup path)
	caddyfileDir := filepath.Join(etcDir, "generated", "caddy")
	if err := os.MkdirAll(caddyfileDir, 0o755); err != nil {
		t.Fatalf("mkdir caddy dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(caddyfileDir, "Caddyfile"), []byte("old-drifting-content"), 0o600); err != nil {
		t.Fatalf("write caddyfile: %v", err)
	}

	// Write a regular file at backupDir so MkdirAll inside BackupBeforeApply fails with ENOTDIR.
	// (chmod 0o555 does not block root due to CAP_DAC_OVERRIDE.)
	backupDir := filepath.Join(dir, "backups")
	if err := os.WriteFile(backupDir, []byte("block"), 0o644); err != nil {
		t.Fatalf("write blocker file at backups: %v", err)
	}

	auditPath := filepath.Join(dir, "audit.jsonl")

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"repair",
		"--profile", "ru-recommended",
		"--yes",
		"--backup-dir", backupDir,
		"--audit-log", auditPath,
		"--etc-dir", etcDir,
		"--var-dir", varDir,
		"--systemd-dir", systemdDir,
	})

	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected error from backup failure, got nil\noutput: %s", out.String())
	}

	// Audit log must exist with a failure event
	events := readAuditLog(t, auditPath)
	if len(events) != 1 {
		t.Fatalf("expected 1 audit event for backup failure, got %d", len(events))
	}
	ev := events[0]
	if ev["action"] != "repair.apply" {
		t.Fatalf("expected action 'repair.apply', got %v", ev["action"])
	}
	if ev["success"] != false {
		t.Fatalf("expected success=false, got %v", ev["success"])
	}
	if ev["error"] == nil || ev["error"] == "" {
		t.Fatalf("expected non-empty error field, got %v", ev["error"])
	}
}
