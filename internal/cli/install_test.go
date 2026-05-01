package cli

import (
	"bytes"
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
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
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
		"--yes",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, out.String())
	}
	got := out.String()
	for _, want := range []string{"Written files:", "Caddyfile", "server.yaml", "index.html", "veil.service", "veil-naive.service", "veil-hysteria2.service"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
}
