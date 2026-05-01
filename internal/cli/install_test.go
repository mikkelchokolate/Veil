package cli

import (
	"bytes"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInstallDryRunRURecommendedPrintsConfigsAndLinks(t *testing.T) {
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
		"--public-ip", "198.51.100.25",
		"--dry-run",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, out.String())
	}
	got := out.String()
	for _, want := range []string{
		"DNS check",
		"Public IP: 198.51.100.25",
		"Resolved IPs: 203.0.113.10",
		"Warning: domain example.com does not resolve to public IP 198.51.100.25",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
}

func TestInstallDryRunDetectsPublicIPWhenRequested(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("198.51.100.25\n"))
	}))
	defer server.Close()

	oldResolver := installDNSResolver
	oldClient := installPublicIPClient
	oldEndpoints := installPublicIPEndpoints
	installDNSResolver = staticDNSResolver{ips: []net.IP{net.ParseIP("198.51.100.25")}}
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
		"Public IP: 198.51.100.25",
		"Resolved IPs: 198.51.100.25",
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
	cmd.SetArgs([]string{"install", "--profile", "ru-recommended", "--domain", "example.com", "--email", "admin@example.com", "--public-ip", "not-an-ip", "--dry-run"})

	if err := cmd.Execute(); err == nil {
		t.Fatalf("expected error with invalid public IP")
	}
}

func TestInstallRURecommendedRequiresDomain(t *testing.T) {
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"install", "--profile", "ru-recommended", "--email", "admin@example.com", "--dry-run"})

	if err := cmd.Execute(); err == nil {
		t.Fatalf("expected error without domain")
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
		"--email", "admin@example.com",
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
