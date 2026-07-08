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
	runtimeInstallFunc = func(_ context.Context, opts runtimeinstall.Options, only []string) []runtimeinstall.Result {
		if opts.BinDir != "/tmp/veilbin" {
			t.Fatalf("bin dir = %q", opts.BinDir)
		}
		if len(only) != 0 {
			t.Fatalf("only = %v, want empty", only)
		}
		return []runtimeinstall.Result{
			{Name: "mieru", Binary: "mita", Installed: true, Version: "v3.34.0", Path: "/tmp/veilbin/mita"},
			{Name: "olcrtc", Binary: "olcrtc", Installed: true, Path: "/tmp/veilbin/olcrtc"},
		}
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
	runtimeInstallFunc = func(_ context.Context, _ runtimeinstall.Options, _ []string) []runtimeinstall.Result {
		return []runtimeinstall.Result{
			{Name: "hysteria2", Binary: "hysteria", Err: context.DeadlineExceeded},
		}
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
	runtimeInstallFunc = func(_ context.Context, _ runtimeinstall.Options, only []string) []runtimeinstall.Result {
		if !reflect.DeepEqual(only, []string{"mieru"}) {
			t.Fatalf("runtime installer received only = %v, want [mieru]", only)
		}
		return []runtimeinstall.Result{
			{Name: "mieru", Binary: "mita", Installed: true, Version: "v3", Path: "/usr/local/bin/mita"},
		}
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
