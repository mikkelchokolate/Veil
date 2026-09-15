package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestInstallDoesNotInstallRuntimesWithoutYes(t *testing.T) {
	calls := 0
	old := installRuntimesFunc
	installRuntimesFunc = func(*cobra.Command, ruRecommendedInstallOptions) { calls++ }
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
	installRuntimesFunc = func(*cobra.Command, ruRecommendedInstallOptions) { calls++ }
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

func TestInstallYesInstallsRuntimesOnce(t *testing.T) {
	withMockedInstallRuntimes(t)
	calls := 0
	installRuntimesFunc = func(*cobra.Command, ruRecommendedInstallOptions) { calls++ }

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
