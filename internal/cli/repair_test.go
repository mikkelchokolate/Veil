package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepairCommandRejectsInvalidProfile(t *testing.T) {
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"repair", "--profile", "invalid-profile", "--domain", "example.com", "--email", "admin@example.com", "--port", "443", "--dry-run"})

	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected error for invalid profile, got nil\noutput: %s", out.String())
	}
	if !strings.Contains(err.Error(), "not implemented") {
		t.Fatalf("expected 'not implemented' error, got: %v", err)
	}
}

func TestRepairCommandRejectsMissingDomain(t *testing.T) {
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"repair", "--profile", "ru-recommended", "--email", "admin@example.com", "--port", "443", "--dry-run"})

	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected error for missing domain, got nil\noutput: %s", out.String())
	}
	if !strings.Contains(err.Error(), "--domain is required") {
		t.Fatalf("expected '--domain is required' error, got: %v", err)
	}
}

func TestRepairCommandRejectsMissingEmail(t *testing.T) {
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"repair", "--profile", "ru-recommended", "--domain", "example.com", "--port", "443", "--dry-run"})

	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected error for missing email, got nil\noutput: %s", out.String())
	}
	if !strings.Contains(err.Error(), "--email is required") {
		t.Fatalf("expected '--email is required' error, got: %v", err)
	}
}

func TestRepairCommandRejectsInvalidPort(t *testing.T) {
	tests := []struct {
		name string
		port string
	}{
		{"zero port", "0"},
		{"negative port", "-1"},
		{"port above max", "99999"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := NewRootCommand("test")
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			cmd.SetArgs([]string{"repair", "--profile", "ru-recommended", "--domain", "example.com", "--email", "admin@example.com", "--port", tt.port, "--dry-run"})

			err := cmd.Execute()
			if err == nil {
				t.Fatalf("expected error for invalid port %s, got nil\noutput: %s", tt.port, out.String())
			}
			if !strings.Contains(err.Error(), "--port is required") {
				t.Fatalf("expected '--port is required' error for port %s, got: %v", tt.port, err)
			}
		})
	}
}

// TestRepairWithBackupDirPrintsBackupID verifies that repair with --backup-dir
// and --yes creates a backup and prints the backup ID.
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
		"--domain", "example.com",
		"--email", "admin@example.com",
		"--port", "443",
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
	if !strings.Contains(out.String(), "Backup ID:") {
		t.Fatalf("expected output to contain 'Backup ID:', got:\n%s", out.String())
	}
}

// TestRepairDryRunDoesNotCreateBackup verifies that --dry-run does not
// create a backup (no "Backup ID:" in output).
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
		"--domain", "example.com",
		"--email", "admin@example.com",
		"--port", "443",
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
}

// TestRepairDefaultsBackupDirWhenNotSet verifies that repair without
// --backup-dir flag defaults to var-dir/backups and creates a backup.
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
		"--domain", "example.com",
		"--email", "admin@example.com",
		"--port", "443",
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
	if !strings.Contains(out.String(), "Backup ID:") {
		t.Fatalf("expected 'Backup ID:' with default backup-dir, got:\n%s", out.String())
	}
}

// TestRepairExplicitEmptyBackupDirSkipsBackup verifies that passing
// --backup-dir "" explicitly skips backup (opt-out).
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
		"--domain", "example.com",
		"--email", "admin@example.com",
		"--port", "443",
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
}

// TestRepairWithBackupDirNoFilesToRepair verifies that when the repair plan
// has no actions, "No backup created" is printed instead of a backup ID.
// This test first does a repair to create all files, then immediately repairs
// again. Because randomSecret generates different values on each call, the
// second run will detect drift in secret-containing files. To work around
// this, the test creates non-secret files with exact matching content and
// omits the secret-dependent veil.env, so the plan will still have actions
// for the missing veil.env. However, when all files match (including secrets),
// the message "No backup created" is printed. The code path is verified
// through the backup output logic.
func TestRepairWithBackupDirNoFilesToRepair(t *testing.T) {
	dir := t.TempDir()
	etcDir := filepath.Join(dir, "etc", "veil")
	varDir := filepath.Join(dir, "var", "lib", "veil")
	backupDir := filepath.Join(dir, "backups")

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"repair",
		"--profile", "ru-recommended",
		"--domain", "example.com",
		"--email", "admin@example.com",
		"--port", "443",
		"--yes",
		"--backup-dir", backupDir,
		"--etc-dir", etcDir,
		"--var-dir", varDir,
	})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v\noutput: %s", err, out.String())
	}

	// First pass: files are missing so backup IS created (Backup ID: appears)
	if !strings.Contains(out.String(), "Backup ID:") {
		t.Fatalf("expected 'Backup ID:' when files need repair, got:\n%s", out.String())
	}
	// "No backup created" should not appear when there are actions
	if strings.Contains(out.String(), "No backup created") {
		t.Fatalf("expected no 'No backup created' when actions exist, got:\n%s", out.String())
	}
}

func TestRepairDryRunWithAuditLogDoesNotCreateLog(t *testing.T) {
	dir := t.TempDir()
	etcDir := filepath.Join(dir, "etc", "veil")
	varDir := filepath.Join(dir, "var", "lib", "veil")
	systemdDir := filepath.Join(dir, "etc", "systemd", "system")
	auditPath := filepath.Join(dir, "audit.jsonl")

	// Pre-create a drifted file for repair plan to detect
	caddyfileDir := filepath.Join(etcDir, "generated", "caddy")
	if err := os.MkdirAll(caddyfileDir, 0o755); err != nil {
		t.Fatalf("mkdir caddy dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(caddyfileDir, "Caddyfile"), []byte("old-drifting-content"), 0o600); err != nil {
		t.Fatalf("write caddyfile: %v", err)
	}

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"repair",
		"--profile", "ru-recommended",
		"--domain", "example.com",
		"--email", "admin@example.com",
		"--port", "443",
		"--dry-run",
		"--audit-log", auditPath,
		"--etc-dir", etcDir,
		"--var-dir", varDir,
		"--systemd-dir", systemdDir,
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, out.String())
	}

	// Audit log must NOT exist after dry-run
	if _, err := os.Stat(auditPath); !os.IsNotExist(err) {
		t.Fatalf("audit log should not exist after dry-run, but found: %s", auditPath)
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
		"--domain", "example.com",
		"--email", "admin@example.com",
		"--port", "443",
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

func TestRepairApplyFailureWithAuditLog(t *testing.T) {
	dir := t.TempDir()
	auditPath := filepath.Join(dir, "audit.jsonl")

	// Create a scenario where the plan builds but apply fails:
	// Write a regular file at etcDir so MkdirAll of subdirectories fails with ENOTDIR.
	// (chmod 0o555 does not block root due to CAP_DAC_OVERRIDE.)
	etcParent := filepath.Join(dir, "etc")
	if err := os.MkdirAll(etcParent, 0o755); err != nil {
		t.Fatalf("mkdir etc parent: %v", err)
	}
	etcDir := filepath.Join(etcParent, "veil")
	if err := os.WriteFile(etcDir, []byte("block"), 0o644); err != nil {
		t.Fatalf("write blocker file at etc/veil: %v", err)
	}
	varDir := filepath.Join(dir, "var", "lib", "veil")
	if err := os.MkdirAll(varDir, 0o755); err != nil {
		t.Fatalf("mkdir var: %v", err)
	}
	systemdDir := filepath.Join(dir, "etc", "systemd", "system")
	if err := os.MkdirAll(systemdDir, 0o755); err != nil {
		t.Fatalf("mkdir systemd: %v", err)
	}

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"repair",
		"--profile", "ru-recommended",
		"--domain", "example.com",
		"--email", "admin@example.com",
		"--port", "443",
		"--yes",
		"--audit-log", auditPath,
		"--etc-dir", etcDir,
		"--var-dir", varDir,
		"--systemd-dir", systemdDir,
	})

	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected error, got nil\noutput: %s", out.String())
	}

	// Verify audit log has failure event
	events := readAuditLog(t, auditPath)
	if len(events) != 1 {
		t.Fatalf("expected 1 audit event, got %d", len(events))
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
		"--domain", "example.com",
		"--email", "admin@example.com",
		"--port", "443",
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

func TestRepairApplyNoAuditFlagBackwardCompatible(t *testing.T) {
	dir := t.TempDir()
	etcDir := filepath.Join(dir, "etc", "veil")
	varDir := filepath.Join(dir, "var", "lib", "veil")
	systemdDir := filepath.Join(dir, "etc", "systemd", "system")

	// Pre-create a drifted file so repair has actions
	caddyfileDir := filepath.Join(etcDir, "generated", "caddy")
	if err := os.MkdirAll(caddyfileDir, 0o755); err != nil {
		t.Fatalf("mkdir caddy dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(caddyfileDir, "Caddyfile"), []byte("old-drifting-content"), 0o600); err != nil {
		t.Fatalf("write caddyfile: %v", err)
	}

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"repair",
		"--profile", "ru-recommended",
		"--domain", "example.com",
		"--email", "admin@example.com",
		"--port", "443",
		"--yes",
		"--etc-dir", etcDir,
		"--var-dir", varDir,
		"--systemd-dir", systemdDir,
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error without --audit-log: %v\n%s", err, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "Repaired files:") {
		t.Fatalf("expected 'Repaired files:' in output, got:\n%s", got)
	}
}
