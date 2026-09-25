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

// TestInstallFailsWhenRuntimeInstallFails is the #1029 regression: a failed
// protocol-runtime install must abort the install, not degrade to a warning —
// reporting success would leave protocol units that can never exec their
// missing binaries.
func TestInstallFailsWhenRuntimeInstallFails(t *testing.T) {
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
		"--panel-access", "local",
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

func TestInstallYesInstallsRuntimesOnce(t *testing.T) {
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
	if calls != 1 {
		t.Fatalf("installRuntimesFunc called %d times, want 1", calls)
	}
}
