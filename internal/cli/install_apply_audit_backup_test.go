package cli

import (
	"bytes"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/veil-panel/veil/internal/installer"
)

var _installTestDeps_apply_audit_backup = []any{
	bytes.Buffer{}, net.ParseIP, http.MethodGet, httptest.NewRecorder, os.ReadFile, filepath.Join, strings.Contains, testing.T{}, installer.RURecommendedProfile{},
}

func TestRepairApplyRequiresYes(t *testing.T) {
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"repair", "--profile", "ru-recommended", "--domain", "example.com", "--email", "admin@example.com", "--port", "31874"})

	if err := cmd.Execute(); err == nil {
		t.Fatalf("expected repair without --dry-run or --yes to fail")
	}
}

func TestInstallRURecommendedApplyWritesFilesWhenConfirmed(t *testing.T) {
	dir := t.TempDir()
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"install",
		"--profile", "ru-recommended",
		"--domain", "example.com",
		"--email", "admin@example.com", "--panel-access", "caddy",
		"--etc-dir", dir + "/etc/veil",
		"--var-dir", dir + "/var/lib/veil",
		"--systemd-dir", dir + "/etc/systemd/system",
		"--panel-port", "2096",
		"--yes",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, out.String())
	}
	got := out.String()
	for _, want := range []string{"Written files:", "Caddyfile", "index.html", "veil.service", "veil-naive.service", "Panel port: 2096 (user selected)", "Panel URL: https://example.com/"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{"server.yaml", "veil-hysteria2.service"} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("Panel Caddy install should not write %q:\n%s", unwanted, got)
		}
	}
	// Caddyfile must include reverse_proxy to panel port
	caddyPath := filepath.Join(dir, "etc/veil/generated/caddy/Caddyfile")
	body, err := os.ReadFile(caddyPath)
	if err != nil {
		t.Fatalf("read Caddyfile: %v", err)
	}
	if !strings.Contains(string(body), "reverse_proxy 127.0.0.1:2096") {
		t.Fatalf("Caddyfile missing reverse_proxy:\n%s", string(body))
	}
}

func TestInstallApplyWithAuditLogWritesSuccessEvent(t *testing.T) {
	dir := t.TempDir()
	auditPath := filepath.Join(dir, "audit.jsonl")

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"install",
		"--profile", "ru-recommended",
		"--domain", "example.com",
		"--email", "admin@example.com", "--port", "31874",
		"--etc-dir", dir + "/etc/veil",
		"--var-dir", dir + "/var/lib/veil",
		"--systemd-dir", dir + "/etc/systemd/system",
		"--panel-port", "2096",
		"--yes",
		"--audit-log", auditPath,
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
	if ev["action"] != "install.apply" {
		t.Fatalf("expected action 'install.apply', got %v", ev["action"])
	}
	if ev["success"] != true {
		t.Fatalf("expected success=true, got %v", ev["success"])
	}
	if ev["timestamp"] == nil || ev["timestamp"] == "" {
		t.Fatalf("expected non-empty timestamp")
	}
	wf, ok := ev["writtenFiles"].([]interface{})
	if !ok {
		t.Fatalf("expected writtenFiles array, got %T", ev["writtenFiles"])
	}
	if len(wf) == 0 {
		t.Fatalf("expected non-empty writtenFiles, got %v", wf)
	}
}

func TestInstallApplyNoAuditFlagBackwardCompatible(t *testing.T) {
	dir := t.TempDir()

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"install",
		"--profile", "ru-recommended",
		"--domain", "example.com",
		"--email", "admin@example.com", "--port", "31874",
		"--etc-dir", dir + "/etc/veil",
		"--var-dir", dir + "/var/lib/veil",
		"--systemd-dir", dir + "/etc/systemd/system",
		"--panel-port", "2096",
		"--yes",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error without --audit-log: %v\n%s", err, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "Written files:") {
		t.Fatalf("expected 'Written files:' in output, got:\n%s", got)
	}
}

func TestInstallDefaultsBackupDirWhenNotSet(t *testing.T) {
	var capturedPaths installer.ApplyPaths
	oldApply := installApplyFunc
	installApplyFunc = func(profile installer.RURecommendedProfile, paths installer.ApplyPaths) (installer.ApplyResult, error) {
		capturedPaths = paths
		return installer.ApplyResult{}, nil
	}
	t.Cleanup(func() { installApplyFunc = oldApply })

	dir := t.TempDir()
	varDir := filepath.Join(dir, "var", "lib", "veil")

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"install",
		"--profile", "ru-recommended",
		"--domain", "example.com",
		"--email", "admin@example.com", "--port", "31874",
		"--etc-dir", filepath.Join(dir, "etc", "veil"),
		"--var-dir", varDir,
		"--systemd-dir", filepath.Join(dir, "etc", "systemd", "system"),
		"--yes",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, out.String())
	}

	if capturedPaths.BackupDir == "" {
		t.Fatalf("expected non-empty BackupDir when --backup-dir is not set, got empty")
	}
	expectedPrefix := filepath.Join(varDir, "backups")
	if !strings.HasPrefix(capturedPaths.BackupDir, expectedPrefix) {
		t.Fatalf("expected BackupDir to start with %q, got %q", expectedPrefix, capturedPaths.BackupDir)
	}
}

func TestInstallExplicitEmptyBackupDirSkipsBackup(t *testing.T) {
	var capturedPaths installer.ApplyPaths
	oldApply := installApplyFunc
	installApplyFunc = func(profile installer.RURecommendedProfile, paths installer.ApplyPaths) (installer.ApplyResult, error) {
		capturedPaths = paths
		return installer.ApplyResult{}, nil
	}
	t.Cleanup(func() { installApplyFunc = oldApply })

	dir := t.TempDir()

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"install",
		"--profile", "ru-recommended",
		"--domain", "example.com",
		"--email", "admin@example.com", "--port", "31874",
		"--etc-dir", filepath.Join(dir, "etc", "veil"),
		"--var-dir", filepath.Join(dir, "var", "lib", "veil"),
		"--systemd-dir", filepath.Join(dir, "etc", "systemd", "system"),
		"--backup-dir", "",
		"--yes",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, out.String())
	}

	if capturedPaths.BackupDir != "" {
		t.Fatalf("expected empty BackupDir when --backup-dir is explicitly empty, got %q", capturedPaths.BackupDir)
	}
}
