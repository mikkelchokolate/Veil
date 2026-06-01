package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
