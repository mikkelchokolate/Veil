package cli

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/runtimeinstall"
)

func TestRuntimeInstallCommandReportsPerRuntimeResults(t *testing.T) {
	old := runtimeInstallFunc
	runtimeInstallFunc = func(_ context.Context, opts runtimeinstall.Options, only []string) ([]runtimeinstall.Result, error) {
		if opts.BinDir != "/tmp/veilbin" {
			t.Fatalf("bin dir = %q", opts.BinDir)
		}
		if len(only) != 0 {
			t.Fatalf("only = %v, want empty", only)
		}
		return []runtimeinstall.Result{
			{Name: "mieru", Binary: "mita", Installed: true, Version: "v3.34.0", Path: "/tmp/veilbin/mita"},
			{Name: "olcrtc", Binary: "olcrtc", Installed: true, Path: "/tmp/veilbin/olcrtc"},
		}, nil
	}
	t.Cleanup(func() { runtimeInstallFunc = old })

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"runtime", "install", "--bin-dir", "/tmp/veilbin"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, out.String())
	}
	got := out.String()
	for _, want := range []string{
		"Installing protocol runtimes",
		"mieru (mita): installed v3.34.0 -> /tmp/veilbin/mita",
		"olcrtc (olcrtc): installed from source -> /tmp/veilbin/olcrtc",
		"All requested protocol runtimes are installed.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("runtime install output missing %q:\n%s", want, got)
		}
	}
}

func TestRuntimeInstallCommandFailsWhenRuntimeFails(t *testing.T) {
	old := runtimeInstallFunc
	runtimeInstallFunc = func(_ context.Context, _ runtimeinstall.Options, _ []string) ([]runtimeinstall.Result, error) {
		return []runtimeinstall.Result{
			{Name: "hysteria2", Binary: "hysteria", Err: context.DeadlineExceeded},
		}, nil
	}
	t.Cleanup(func() { runtimeInstallFunc = old })

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"runtime", "install"})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected error when a runtime fails to install:\n%s", out.String())
	}
	if !strings.Contains(err.Error(), "hysteria2") {
		t.Fatalf("error should name failed runtime: %v", err)
	}
}

func TestRuntimeInstallCommandOnlyFiltersByProtocolBeforeInstall(t *testing.T) {
	old := runtimeInstallFunc
	runtimeInstallFunc = func(_ context.Context, _ runtimeinstall.Options, only []string) ([]runtimeinstall.Result, error) {
		if !reflect.DeepEqual(only, []string{"mieru"}) {
			t.Fatalf("runtime installer received only = %v, want [mieru]", only)
		}
		return []runtimeinstall.Result{
			{Name: "mieru", Binary: "mita", Installed: true, Version: "v3", Path: "/usr/local/bin/mita"},
		}, nil
	}
	t.Cleanup(func() { runtimeInstallFunc = old })

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"runtime", "install", "--only", "mieru"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "mieru (mita)") {
		t.Fatalf("expected mieru in output:\n%s", got)
	}
	if strings.Contains(got, "hysteria") || strings.Contains(got, "sing-box") {
		t.Fatalf("--only mieru should not report other runtimes:\n%s", got)
	}
}

func TestRuntimeInstallCommandUnknownOnlyReportsSelection(t *testing.T) {
	old := runtimeInstallFunc
	called := false
	runtimeInstallFunc = func(_ context.Context, _ runtimeinstall.Options, _ []string) ([]runtimeinstall.Result, error) {
		called = true
		return nil, nil
	}
	t.Cleanup(func() { runtimeInstallFunc = old })

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"runtime", "install", "--only", "unknown"})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected unknown runtime selection to fail")
	}
	if called {
		t.Fatal("unknown --only names must be rejected before installing")
	}
	if !strings.Contains(err.Error(), "unknown runtime name(s) unknown") || !strings.Contains(err.Error(), "supported:") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRuntimeInstallCommandRejectsMixedUnknownOnlyWithoutInstalling(t *testing.T) {
	old := runtimeInstallFunc
	called := false
	runtimeInstallFunc = func(_ context.Context, _ runtimeinstall.Options, _ []string) ([]runtimeinstall.Result, error) {
		called = true
		return []runtimeinstall.Result{{Name: "mieru", Binary: "mita", Installed: true}}, nil
	}
	t.Cleanup(func() { runtimeInstallFunc = old })

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"runtime", "install", "--only", "mieru,hysetria2"})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected mixed unknown --only to fail, got:\n%s", out.String())
	}
	if called {
		t.Fatal("invalid --only selection must not install the matching subset")
	}
	if strings.Contains(out.String(), "All requested protocol runtimes are installed.") {
		t.Fatalf("must not report complete success:\n%s", out.String())
	}
	msg := err.Error()
	if !strings.Contains(msg, "hysetria2") || !strings.Contains(msg, "supported:") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRuntimeInstallCommandNoRuntimeResultsReportsPlatform(t *testing.T) {
	old := runtimeInstallFunc
	runtimeInstallFunc = func(_ context.Context, _ runtimeinstall.Options, only []string) ([]runtimeinstall.Result, error) {
		if len(only) != 0 {
			t.Fatalf("only = %v, want empty", only)
		}
		return nil, nil
	}
	t.Cleanup(func() { runtimeInstallFunc = old })

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"runtime", "install"})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected empty runtime catalog to fail")
	}
	if !strings.Contains(err.Error(), "no protocol runtimes are available for this platform") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRuntimeInstallCommandHelpUsesRuntimeDescriptions(t *testing.T) {
	cmd := newRuntimeInstallCommand()
	long := cmd.Long
	for _, want := range []string{
		"caddy is built from source with the naive forwardproxy fork",
		"hysteria is downloaded from its upstream GitHub release",
		"mita is downloaded from its upstream GitHub release",
		"olcrtc is built from source",
		"sing-box is downloaded from its pinned upstream GitHub release",
	} {
		if !strings.Contains(long, want) {
			t.Fatalf("runtime install help missing description %q:\n%s", want, long)
		}
	}
	// Help text must no longer be produced by a hardcoded switch statement.
	if strings.Contains(long, "switch") {
		t.Fatalf("runtime install help should not contain a hardcoded switch; got:\n%s", long)
	}
}
