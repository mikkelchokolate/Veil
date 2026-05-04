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

func TestInstallInteractivePromptsForDomainEmailPortAndRandomPanelPort(t *testing.T) {
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetIn(strings.NewReader("example.com\nadmin@example.com\n31874\nn\n"))
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"install", "--profile", "ru-recommended", "--interactive", "--dry-run"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, out.String())
	}
	got := out.String()
	for _, want := range []string{
		"Domain for Veil/ACME:",
		"ACME email:",
		"Shared proxy port:",
		"Customize panel port?",
		"Domain: example.com",
		"Email: admin@example.com",
		"Panel port:",
		"(random)",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
}

func TestInstallInteractiveAcceptsCustomPanelPort(t *testing.T) {
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetIn(strings.NewReader("example.com\nadmin@example.com\n31874\ny\n2096\n"))
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"install", "--profile", "ru-recommended", "--interactive", "--dry-run"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "Panel port: 2096 (user selected)") {
		t.Fatalf("expected custom panel port output:\n%s", got)
	}
}

func TestInstallDryRunRURecommendedPrintsConfigsAndLinks(t *testing.T) {
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"install", "--profile", "ru-recommended", "--domain", "example.com", "--email", "admin@example.com", "--port", "31874", "--dry-run"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, out.String())
	}
	got := out.String()
	for _, want := range []string{
		"Veil ru-recommended dry run",
		"NaiveProxy TCP port:",
		"Hysteria2 UDP port:",
		"NaiveProxy client URL:",
		"Hysteria2 client URI:",
		"Generated Caddyfile",
		"Generated Hysteria2 server.yaml",
		"Panel port:",
		"(random)",
		"ufw allow ",
		"/tcp comment Veil panel",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
}

func TestInstallDryRunHonorsStackSelection(t *testing.T) {
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"install", "--profile", "ru-recommended", "--domain", "example.com", "--email", "admin@example.com", "--port", "31874", "--stack", "hysteria2", "--dry-run"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, out.String())
	}
	got := out.String()
	for _, want := range []string{
		"Stack: hysteria2",
		"Hysteria2 UDP port:",
		"Hysteria2 client URI:",
		"Generated Hysteria2 server.yaml",
		"ufw allow ",
		"/udp comment Veil Hysteria2",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{
		"NaiveProxy TCP port:",
		"NaiveProxy client URL:",
		"Generated Caddyfile",
		"Caddy/NaiveProxy build:",
		"/tcp comment Veil NaiveProxy",
	} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("output should not contain %q:\n%s", unwanted, got)
		}
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
		"--email", "admin@example.com", "--port", "31874",
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
		"--email", "admin@example.com", "--port", "31874",
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
	cmd.SetArgs([]string{"install", "--profile", "ru-recommended", "--domain", "example.com", "--email", "admin@example.com", "--port", "31874", "--public-ip", "not-an-ip", "--dry-run"})

	if err := cmd.Execute(); err == nil {
		t.Fatalf("expected error with invalid public IP")
	}
}

func TestInstallRURecommendedRequiresDomain(t *testing.T) {
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"install", "--profile", "ru-recommended", "--email", "admin@example.com", "--port", "31874", "--dry-run"})

	if err := cmd.Execute(); err == nil {
		t.Fatalf("expected error without domain")
	}
}

func TestInstallRURecommendedRequiresPort(t *testing.T) {
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"install", "--profile", "ru-recommended", "--domain", "example.com", "--email", "admin@example.com", "--dry-run"})

	if err := cmd.Execute(); err == nil {
		t.Fatalf("expected error without explicit shared proxy port")
	}
}

func TestInstallDryRunUsesHysteriaChecksumInBinaryPlan(t *testing.T) {
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"install",
		"--profile", "ru-recommended",
		"--domain", "example.com",
		"--email", "admin@example.com", "--port", "31874",
		"--hysteria-sha256", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"--dry-run",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "Hysteria2 sha256: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa") {
		t.Fatalf("expected supplied checksum in install plan:\n%s", got)
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
		"--domain", "example.com",
		"--email", "admin@example.com", "--port", "31874",
		"--etc-dir", dir + "/etc/veil",
		"--var-dir", dir + "/var/lib/veil",
		"--systemd-dir", dir + "/etc/systemd/system",
		"--dry-run",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, out.String())
	}
	got := out.String()
	for _, want := range []string{"Veil repair plan", "repair missing", "Caddyfile", "server.yaml", "veil.service"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
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
		"--email", "admin@example.com", "--port", "31874",
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
	for _, want := range []string{"Written files:", "Caddyfile", "server.yaml", "index.html", "veil.service", "veil-naive.service", "veil-hysteria2.service", "Panel port: 2096 (user selected)"} {
		if !strings.Contains(got, want) {
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
		"--email", "admin@example.com", "--port", "31874",
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

func TestInstallInteractiveRejectsInvalidDomainAndReprompts(t *testing.T) {
	// Pass an invalid domain (no dot), then a valid one. Verify the command succeeds.
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	// Sequence: invalid-domain-without-dot, valid-domain, email, port, n (no custom panel port)
	cmd.SetIn(strings.NewReader("not-a-domain\nvalid.example.com\nadmin@example.com\n31874\nn\n"))
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"install", "--profile", "ru-recommended", "--interactive", "--dry-run"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, out.String())
	}
	got := out.String()
	// Should have reprompted and eventually used the valid domain.
	if !strings.Contains(got, "Domain: valid.example.com") {
		t.Fatalf("expected valid domain in output:\n%s", got)
	}
	// The invalid domain should NOT appear as the final domain.
	if strings.Contains(got, "Domain: not-a-domain") {
		t.Fatalf("expected invalid domain to be rejected, got:\n%s", got)
	}
}

func TestInstallInteractiveRejectsInvalidSharedPortAndReprompts(t *testing.T) {
	// Pass invalid ports (0, out-of-range, non-numeric), then a valid one.
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetIn(strings.NewReader("example.com\nadmin@example.com\n0\n99999\nabc\n31874\nn\n"))
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"install", "--profile", "ru-recommended", "--interactive", "--dry-run"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, out.String())
	}
	got := out.String()
	// Should succeed with the valid port (31874 appears in output as NaiveProxy TCP port etc.)
	if !strings.Contains(got, "NaiveProxy TCP port: 31874") {
		t.Fatalf("expected valid shared port 31874 in output:\n%s", got)
	}
}

func TestInstallInteractiveRejectsInvalidPanelPortAndReprompts(t *testing.T) {
	// User chooses to customize panel port but enters invalid values, then valid.
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetIn(strings.NewReader("example.com\nadmin@example.com\n31874\ny\n0\n99999\nxyz\n2096\n"))
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"install", "--profile", "ru-recommended", "--interactive", "--dry-run"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "Panel port: 2096 (user selected)") {
		t.Fatalf("expected custom panel port 2096 in output:\n%s", got)
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

func TestStackName(t *testing.T) {
	tests := []struct {
		name    string
		profile installer.RURecommendedProfile
		want    string
	}{
		{
			name:    "both naive and hysteria2",
			profile: installer.RURecommendedProfile{InstallNaive: true, InstallHysteria2: true},
			want:    "both",
		},
		{
			name:    "naive only",
			profile: installer.RURecommendedProfile{InstallNaive: true, InstallHysteria2: false},
			want:    "naive",
		},
		{
			name:    "hysteria2 only",
			profile: installer.RURecommendedProfile{InstallNaive: false, InstallHysteria2: true},
			want:    "hysteria2",
		},
		{
			name:    "neither",
			profile: installer.RURecommendedProfile{InstallNaive: false, InstallHysteria2: false},
			want:    "none",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stackName(tt.profile)
			if got != tt.want {
				t.Errorf("stackName() = %q, want %q", got, tt.want)
			}
		})
	}
}
