package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/installer"
	"github.com/mikkelchokolate/Veil/internal/service"
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
	for _, want := range []string{"Install scope: Panel", "Panel URL: https://panel.example.com/", "Generated Caddy JSON", `"dial": "127.0.0.1:2096"`} {
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

func TestInstallPanelCaddyAccessDryRunShowsResolvedCaddyBinary(t *testing.T) {
	oldLookPath := commandLookPath
	commandLookPath = func(name string) (string, error) {
		if name == "caddy" {
			return "/usr/sbin/caddy", nil
		}
		return "/usr/bin/" + name, nil
	}
	t.Cleanup(func() { commandLookPath = oldLookPath })

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"install", "--panel-access", "caddy", "--domain", "panel.example.com", "--email", "admin@example.com", "--panel-port", "2096", "--dry-run"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("install dry-run: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "Caddy/Panel reverse proxy: /usr/sbin/caddy") {
		t.Fatalf("dry-run should show resolved Caddy binary path:\n%s", out.String())
	}
}

func TestInstallPanelCaddyAccessUsesResolvedCaddyBinaryInSystemdUnit(t *testing.T) {
	oldApply := installApplyFunc
	oldLookPath := commandLookPath
	oldSystemd := installSystemdRunFunc
	oldExecutable := installExecutableFunc
	var gotPaths installer.ApplyPaths
	installApplyFunc = func(profile installer.RURecommendedProfile, paths installer.ApplyPaths) (installer.ApplyResult, error) {
		gotPaths = paths
		return installer.ApplyResult{WrittenFiles: []string{"/etc/systemd/system/veil-caddy.service"}}, nil
	}
	commandLookPath = func(name string) (string, error) {
		if name == "caddy" {
			return "/usr/sbin/caddy", nil
		}
		return "/usr/bin/" + name, nil
	}
	installSystemdRunFunc = func([]service.SystemdAction) error { return nil }
	installExecutableFunc = func() (string, error) { return "/usr/local/bin/veil", nil }
	defer func() {
		installApplyFunc = oldApply
		commandLookPath = oldLookPath
		installSystemdRunFunc = oldSystemd
		installExecutableFunc = oldExecutable
	}()

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	etcDir := t.TempDir()
	varDir := t.TempDir()
	cmd.SetArgs([]string{
		"install",
		"--panel-access", "caddy",
		"--domain", "panel.example.com",
		"--email", "admin@example.com",
		"--panel-port", "2096",
		"--etc-dir", etcDir,
		"--var-dir", varDir,
		"--yes",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("install panel caddy: %v\n%s", err, out.String())
	}
	if gotPaths.CaddyBinary != "/usr/sbin/caddy" {
		t.Fatalf("CaddyBinary = %q, want resolved Caddy path", gotPaths.CaddyBinary)
	}
}

func TestInstallPanelCaddyAccessRequiresCaddyBinaryForApply(t *testing.T) {
	oldLookPath := commandLookPath
	commandLookPath = func(name string) (string, error) {
		if name == "caddy" {
			return "", errors.New("missing caddy")
		}
		return "/usr/bin/" + name, nil
	}
	t.Cleanup(func() { commandLookPath = oldLookPath })

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"install", "--panel-access", "caddy", "--domain", "panel.example.com", "--email", "admin@example.com", "--panel-port", "2096", "--yes"})

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "caddy is required for caddy Panel access") {
		t.Fatalf("expected caddy prerequisite error, got %v\n%s", err, out.String())
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
