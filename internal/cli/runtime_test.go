package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
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

func TestPanelInstallVerifiesCaddyProvenanceBeforeReuse(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("runtime activation requires Linux filesystem semantics")
	}
	for _, scenario := range []string{"missing", "untracked", "modified", "old-version", "verified"} {
		t.Run(scenario, func(t *testing.T) {
			oldLookPath, oldEnsure := commandLookPath, runtimeEnsureFunc
			commandLookPath = func(string) (string, error) { return "/usr/sbin/caddy", nil }
			t.Cleanup(func() { commandLookPath, runtimeEnsureFunc = oldLookPath, oldEnsure })
			binDir := t.TempDir()
			active := filepath.Join(binDir, "caddy")
			calls, builds := 0, 0
			runtimeEnsureFunc = func(ctx context.Context, opts runtimeinstall.Options, runtime runtimeinstall.Runtime) runtimeinstall.Result {
				calls++
				if opts.BinDir != runtimeinstall.DefaultBinDir() || runtime.Name != "naiveproxy" || runtime.Binary != "caddy" || runtime.Method != runtimeinstall.MethodCaddyNaive || runtime.Version == "" {
					t.Fatalf("panel install requested opts=%+v runtime=%+v", opts, runtime)
				}
				opts.BinDir = binDir
				opts.CaddyCacheDir = t.TempDir()
				opts.EnsureGo = func(context.Context) (string, error) { return "/usr/local/go/bin/go", nil }
				opts.BuildCaddy = func(_ context.Context, outPath string) error {
					builds++
					return os.WriteFile(outPath, []byte("pinned caddy with forwardproxy"), 0o755)
				}
				opts.RunVersion = func(_ context.Context, binary string, _ []string) (string, error) {
					if scenario == "old-version" && binary == active {
						return "caddy v2.0.0", nil
					}
					return "caddy " + runtime.Version, nil
				}
				if scenario != "missing" && scenario != "untracked" {
					if result := runtimeinstall.Install(ctx, opts, runtime); result.Err != nil {
						t.Fatal(result.Err)
					}
				}
				if scenario == "untracked" || scenario == "modified" {
					if err := os.WriteFile(active, []byte("stock caddy"), 0o755); err != nil {
						t.Fatal(err)
					}
				}
				builds = 0
				return runtimeinstall.Ensure(ctx, opts, runtime)
			}
			cmd := NewRootCommand("test")
			cmd.SetContext(t.Context())
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			if err := defaultInstallRuntimesDuringInstall(cmd, ruRecommendedInstallOptions{PanelAccess: "caddy"}); err != nil {
				t.Fatalf("install Caddy: %v\n%s", err, out.String())
			}
			wantBuilds := 1
			if scenario == "verified" {
				wantBuilds = 0
			}
			if calls != 1 || builds != wantBuilds {
				t.Fatalf("ensure calls=%d, builds=%d, want 1 and %d", calls, builds, wantBuilds)
			}
		})
	}
}

func TestPanelInstallRejectsUnverifiedCaddyWhenEnsureFailsOrSkips(t *testing.T) {
	for name, result := range map[string]runtimeinstall.Result{
		"failed":  {Err: errors.New("offline")},
		"skipped": {Skipped: true, SkipReason: "Go toolchain unavailable"},
	} {
		t.Run(name, func(t *testing.T) {
			oldLookPath, oldEnsure := commandLookPath, runtimeEnsureFunc
			commandLookPath = func(string) (string, error) { return "/usr/sbin/caddy", nil }
			runtimeEnsureFunc = func(context.Context, runtimeinstall.Options, runtimeinstall.Runtime) runtimeinstall.Result {
				return result
			}
			t.Cleanup(func() { commandLookPath, runtimeEnsureFunc = oldLookPath, oldEnsure })
			cmd := NewRootCommand("test")
			cmd.SetContext(t.Context())
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			if err := defaultInstallRuntimesDuringInstall(cmd, ruRecommendedInstallOptions{PanelAccess: "caddy"}); err == nil {
				t.Fatalf("unverified Caddy was accepted: %+v", result)
			}
			if !strings.Contains(out.String(), "veil runtime install --only naiveproxy") {
				t.Fatalf("missing repair guidance: %s", out.String())
			}
		})
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

func TestRuntimeInstallCommandRejectsExplicitEmptySelection(t *testing.T) {
	old := runtimeInstallFunc
	runtimeInstallFunc = func(context.Context, runtimeinstall.Options, []string) ([]runtimeinstall.Result, error) {
		t.Fatal("empty --only selection must not install all runtimes")
		return nil, nil
	}
	t.Cleanup(func() { runtimeInstallFunc = old })
	for _, selection := range []string{"", ",", " , , "} {
		t.Run(selection, func(t *testing.T) {
			cmd := NewRootCommand("test")
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			cmd.SetArgs([]string{"runtime", "install", "--only", selection})
			if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "at least one runtime") {
				t.Fatalf("empty selector %q was accepted: %v\n%s", selection, err, out.String())
			}
		})
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
