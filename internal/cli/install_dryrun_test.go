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

var _installTestDeps_dryrun = []any{
	bytes.Buffer{}, net.ParseIP, http.MethodGet, httptest.NewRecorder, os.ReadFile, filepath.Join, strings.Contains, testing.T{}, installer.RURecommendedProfile{},
}

func TestRURecommendedInstallWorkflowDryRunPrintsPanelURLWithoutApply(t *testing.T) {
	oldApply := installApplyFunc
	installApplyFunc = func(profile installer.RURecommendedProfile, paths installer.ApplyPaths) (installer.ApplyResult, error) {
		t.Fatalf("dry-run must not apply files")
		return installer.ApplyResult{}, nil
	}
	t.Cleanup(func() { installApplyFunc = oldApply })

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := runRURecommendedInstall(cmd, ruRecommendedInstallOptions{
		Profile:     "ru-recommended",
		PanelAccess: "caddy",
		Domain:      "example.com",
		Email:       "admin@example.com",
		DryRun:      true,
		EtcDir:      "/etc/veil",
		VarDir:      "/var/lib/veil",
	})
	if err != nil {
		t.Fatalf("runRURecommendedInstall: %v\n%s", err, out.String())
	}
	got := out.String()
	for _, want := range []string{
		"Veil ru-recommended dry run",
		"Panel URL: https://example.com/",
		"Install plan",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
}

func TestInstallDryRunWithDomainEmailStillInstallsPanelOnly(t *testing.T) {
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"install", "--profile", "ru-recommended", "--dry-run"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, out.String())
	}
	got := out.String()
	for _, want := range []string{
		"Veil ru-recommended dry run",
		"Install scope: Panel",
		"Panel port: 2096",
		"Panel access: https://127.0.0.1:2096/",
		"ufw allow 2096/tcp comment Veil panel",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{"NaiveProxy TCP port:", "Hysteria2 UDP port:", "NaiveProxy client URL:", "Hysteria2 client URI:", "Generated Hysteria2 server.yaml", "Shared port:"} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("Panel install should not contain %q:\n%s", unwanted, got)
		}
	}
}

func TestInstallDryRunRejectsStackFlag(t *testing.T) {
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"install", "--profile", "ru-recommended", "--stack", "hysteria2", "--dry-run"})

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "unknown flag: --stack") {
		t.Fatalf("expected --stack to be removed, got %v\n%s", err, out.String())
	}
}

func TestInstallDryRunPrintsDNSWarningWhenPublicIPDoesNotMatch(t *testing.T) {
	oldResolver := installDNSResolver
	installDNSResolver = staticDNSResolver{ips: []net.IP{net.ParseIP("203.0.113.10")}}
	t.Cleanup(func() { installDNSResolver = oldResolver })

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"install",
		"--profile", "ru-recommended",
		"--domain", "example.com",
		"--email", "admin@example.com",
		"--public-ip", "93.184.216.34",
		"--dry-run",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, out.String())
	}
	got := out.String()
	for _, want := range []string{
		"DNS check",
		"Public IP: 93.184.216.34",
		"Resolved IPs: 203.0.113.10",
		"Warning: domain example.com does not resolve to public IP 93.184.216.34",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
}

func TestInstallDryRunDetectsPublicIPWhenRequested(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("93.184.216.34\n"))
	}))
	defer server.Close()

	oldResolver := installDNSResolver
	oldClient := installPublicIPClient
	oldEndpoints := installPublicIPEndpoints
	installDNSResolver = staticDNSResolver{ips: []net.IP{net.ParseIP("93.184.216.34")}}
	installPublicIPClient = server.Client()
	installPublicIPEndpoints = []string{server.URL}
	t.Cleanup(func() {
		installDNSResolver = oldResolver
		installPublicIPClient = oldClient
		installPublicIPEndpoints = oldEndpoints
	})

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"install",
		"--profile", "ru-recommended",
		"--domain", "example.com",
		"--email", "admin@example.com",
		"--public-ip", "auto",
		"--dry-run",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, out.String())
	}
	got := out.String()
	for _, want := range []string{
		"Public IP: 93.184.216.34",
		"Resolved IPs: 93.184.216.34",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Warning:") {
		t.Fatalf("did not expect DNS warning:\n%s", got)
	}
}

func TestInstallRURecommendedRejectsInvalidPublicIP(t *testing.T) {
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"install", "--profile", "ru-recommended", "--public-ip", "not-an-ip", "--dry-run"})

	if err := cmd.Execute(); err == nil {
		t.Fatalf("expected error with invalid public IP")
	}
}

func TestInstallRURecommendedDoesNotRequireDomainForLocalPanel(t *testing.T) {
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"install", "--profile", "ru-recommended", "--dry-run"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("local Panel install should not require domain: %v\n%s", err, out.String())
	}
}

func TestInstallRURecommendedDoesNotRequireSharedProxyPort(t *testing.T) {
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"install", "--profile", "ru-recommended", "--dry-run"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Panel install should not require shared proxy port: %v\n%s", err, out.String())
	}
}

func TestRepairDryRunReportsMissingManagedFiles(t *testing.T) {
	dir := t.TempDir()
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"repair",
		"--profile", "ru-recommended",
		"--etc-dir", dir + "/etc/veil",
		"--var-dir", dir + "/var/lib/veil",
		"--systemd-dir", dir + "/etc/systemd/system",
		"--dry-run",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, out.String())
	}
	got := out.String()
	for _, want := range []string{"Veil repair plan", "repair missing", "panel/tls.crt", "veil.env", "veil.service"} {
		if !strings.Contains(filepath.ToSlash(got), filepath.ToSlash(want)) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
}

func TestInstallDryRunWithAuditLogDoesNotCreateLog(t *testing.T) {
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
		"--email", "admin@example.com",
		"--dry-run",
		"--audit-log", auditPath,
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, out.String())
	}

	// Audit log must NOT exist after dry-run
	if _, err := os.Stat(auditPath); !os.IsNotExist(err) {
		t.Fatalf("audit log should not exist after dry-run, but found: %s", auditPath)
	}
}
