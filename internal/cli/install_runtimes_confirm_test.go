package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/mikkelchokolate/Veil/internal/installer"
)

func TestInstallDoesNotInstallRuntimesWithoutYes(t *testing.T) {
	calls := 0
	old := installRuntimesFunc
	installRuntimesFunc = func(*cobra.Command, ruRecommendedInstallOptions) error { calls++; return nil }
	t.Cleanup(func() { installRuntimesFunc = old })

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"install", "--profile", "ru-recommended"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "apply mode requires --yes") {
		t.Fatalf("expected --yes error, got %v\n%s", err, out.String())
	}
	if calls != 0 {
		t.Fatalf("installRuntimesFunc called %d times before confirm", calls)
	}
}

func TestInstallInteractiveCancelDoesNotInstallRuntimes(t *testing.T) {
	calls := 0
	old := installRuntimesFunc
	installRuntimesFunc = func(*cobra.Command, ruRecommendedInstallOptions) error { calls++; return nil }
	t.Cleanup(func() { installRuntimesFunc = old })

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetIn(strings.NewReader("n\n"))
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"install", "--profile", "ru-recommended", "--panel-access", "local", "--panel-port", "2096", "--interactive"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "install cancelled") {
		t.Fatalf("expected cancelled install, got %v\n%s", err, out.String())
	}
	if calls != 0 {
		t.Fatalf("installRuntimesFunc called %d times after rejected confirm", calls)
	}
}

// Caddy is required immediately when it exposes the panel itself.
func TestInstallFailsWhenPanelCaddyRuntimeInstallFails(t *testing.T) {
	withMockedInstallRuntimes(t)
	sentinel := errors.New("release download failed")
	installRuntimesFunc = func(*cobra.Command, ruRecommendedInstallOptions) error { return sentinel }
	applyRan := false
	oldApply := installApplyFunc
	installApplyFunc = func(installer.RURecommendedProfile, installer.ApplyPaths) (installer.ApplyResult, error) {
		applyRan = true
		return installer.ApplyResult{}, nil
	}
	t.Cleanup(func() { installApplyFunc = oldApply })

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"install",
		"--profile", "ru-recommended",
		"--panel-access", "caddy",
		"--domain", "panel.example.com",
		"--email", "admin@example.com",
		"--etc-dir", t.TempDir(),
		"--var-dir", t.TempDir(),
		"--systemd-dir", t.TempDir(),
		"--yes",
	})
	if err := cmd.Execute(); !errors.Is(err, sentinel) {
		t.Fatalf("install --yes must fail closed on runtime failure, got %v\n%s", err, out.String())
	}
	if applyRan {
		t.Fatal("install apply must not run after a failed runtime install")
	}
}

func TestLocalPanelInstallDoesNotInstallProtocolRuntimes(t *testing.T) {
	withMockedInstallRuntimes(t)
	calls := 0
	installRuntimesFunc = func(*cobra.Command, ruRecommendedInstallOptions) error { calls++; return nil }

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"install",
		"--profile", "ru-recommended",
		"--panel-access", "local",
		"--etc-dir", t.TempDir(),
		"--var-dir", t.TempDir(),
		"--systemd-dir", t.TempDir(),
		"--yes",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("install --yes: %v\n%s", err, out.String())
	}
	if calls != 0 {
		t.Fatalf("panel-only install downloaded runtimes %d times", calls)
	}
}

func TestDirectPanelInstallDoesNotInstallProtocolRuntimes(t *testing.T) {
	withMockedInstallRuntimes(t)
	installRuntimesFunc = func(*cobra.Command, ruRecommendedInstallOptions) error {
		t.Fatal("direct panel install must defer protocol downloads")
		return nil
	}
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"install", "--panel-access", "direct", "--public-ip", "203.0.113.4", "--le-ip-cert=false",
		"--etc-dir", t.TempDir(), "--var-dir", t.TempDir(), "--systemd-dir", t.TempDir(), "--yes"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("direct panel install: %v\n%s", err, out.String())
	}
}
