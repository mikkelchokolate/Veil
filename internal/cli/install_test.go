package cli

import (
	"bytes"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
