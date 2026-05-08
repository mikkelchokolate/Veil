package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestInstallPanelCaddyAccessPrintsPanelURLWithoutProxyStack(t *testing.T) {
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"install", "--panel-access", "caddy", "--domain", "panel.example.com", "--email", "admin@example.com", "--panel-port", "2096", "--dry-run"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("install dry-run: %v\n%s", err, out.String())
	}
	got := out.String()
	for _, want := range []string{"Install scope: Panel", "Panel URL: https://panel.example.com/", "Generated Caddyfile", "reverse_proxy 127.0.0.1:2096"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{"Stack:", "NaiveProxy TCP port:", "Hysteria2 UDP port:", "Mieru asset:", "forward_proxy", "Shared port:"} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("panel Caddy install should not include %q:\n%s", unwanted, got)
		}
	}
}

func TestInstallPanelCaddyAccessRequiresDomainAndEmail(t *testing.T) {
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"install", "--panel-access", "caddy", "--dry-run"})

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "--domain and --email are required for caddy Panel access") {
		t.Fatalf("err = %v\n%s", err, out.String())
	}
}
