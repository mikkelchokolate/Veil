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

	"github.com/mikkelchokolate/Veil/internal/installer"
)

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

// Domain+email on a local panel install keep the scope Panel-only — the
// profile records them, the panel URL advertises the domain, but no Caddy
// config, firewall openings, or protocol runtime plans appear.
func TestInstallDryRunWithDomainEmailStillInstallsPanelOnly(t *testing.T) {
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"install", "--profile", "ru-recommended", "--domain", "example.com", "--email", "admin@example.com", "--dry-run"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, out.String())
	}
	got := out.String()
	for _, want := range []string{
		"Veil ru-recommended dry run",
		"Domain: example.com",
		"Email: admin@example.com",
		"Install scope: Panel",
		"Panel port: 2096",
		"Panel URL: https://example.com/",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{"ufw allow 2096/tcp comment Veil panel", "Generated Caddy JSON", "NaiveProxy TCP port:", "Hysteria2 UDP port:", "NaiveProxy client URL:", "Hysteria2 client URI:", "Generated Hysteria2 server.yaml", "Shared port:"} {
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

	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected error with invalid public IP")
	}
	// Any error would pass an err-only check — including unrelated flag
	// errors. Lock that the refusal names the flag concept and the reason
	// (#773); the production message is "public IP must be a valid IPv4 or
	// IPv6 address, or auto".
	combined := err.Error() + "\n" + out.String()
	lower := strings.ToLower(combined)
	if !strings.Contains(lower, "public ip") && !strings.Contains(lower, "public-ip") {
		t.Fatalf("invalid public-ip refusal must name the flag, got err=%v out=%s", err, out.String())
	}
	if !strings.Contains(lower, "valid") && !strings.Contains(lower, "invalid") {
		t.Fatalf("invalid public-ip refusal must state the validation reason, got err=%v out=%s", err, out.String())
	}
}

func TestInstallDryRunPublicIPWithoutDomainDoesNotRequireDomain(t *testing.T) {
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"install", "--profile", "ru-recommended", "--public-ip", "203.0.113.10", "--dry-run"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("public-ip without domain should skip DNS check: %v\n%s", err, out.String())
	}
	if strings.Contains(out.String(), "DNS check") {
		t.Fatalf("no-domain install should not run DNS validation:\n%s", out.String())
	}
}

func TestInstallDryRunDirectPublicIPWithoutDomainPrintsPlan(t *testing.T) {
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"install",
		"--profile", "ru-recommended",
		"--panel-access", "direct",
		"--public-ip", "203.0.113.10",
		"--dry-run",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("direct public-ip without domain: %v\n%s", err, out.String())
	}
	got := out.String()
	// The header alone proves nothing — lock the plan body: the public panel
	// firewall rule, the LE IP-cert HTTP-01 opening, and the direct access URL.
	for _, want := range []string{
		"Install plan",
		"ufw allow 2096/tcp comment Veil panel",
		"ufw allow 80/tcp comment Veil ACME HTTP-01",
		"Panel access: https://0.0.0.0:2096/",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("direct dry-run output missing %q:\n%s", want, got)
		}
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
	got := out.String()
	// The plan must render a usable local panel: no domain requirement means
	// the panel URL still resolves on loopback.
	for _, want := range []string{"Install scope: Panel", "Panel port: 2096", "Panel access: https://127.0.0.1:2096/"} {
		if !strings.Contains(got, want) {
			t.Fatalf("domain-less local panel plan missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Domain: example") || strings.Contains(got, "domain is required") {
		t.Fatalf("local panel plan must not demand a domain:\n%s", got)
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
	got := out.String()
	if !strings.Contains(got, "Install scope: Panel") || !strings.Contains(got, "Install plan") {
		t.Fatalf("expected a complete Panel-only plan:\n%s", got)
	}
	for _, unwanted := range []string{"Shared port:", "NaiveProxy TCP port:", "Hysteria2 UDP port:"} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("Panel install should not require %q:\n%s", unwanted, got)
		}
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
