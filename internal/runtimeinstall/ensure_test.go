package runtimeinstall

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestEnsureReusesVerifiedRuntimeWithoutNetwork(t *testing.T) {
	payload := []byte("pinned runtime v1.2.3")
	runtime := regressionRuntime("v1.2.3", payload)
	opts := regressionInstallOptions(t.TempDir(), runtime, payload)
	if result := Install(t.Context(), opts, runtime); result.Err != nil {
		t.Fatal(result.Err)
	}
	opts.Download = func(context.Context, string) ([]byte, error) {
		t.Fatal("verified runtime was downloaded again")
		return nil, nil
	}
	opts.FetchReleaseVersion = func(context.Context, string, string) (*Release, error) {
		t.Fatal("verified runtime queried the network")
		return nil, nil
	}
	var stages []string
	opts.Progress = func(stage, name string) {
		if name != runtime.Name {
			t.Fatalf("progress runtime = %q", name)
		}
		stages = append(stages, stage)
	}
	result := Ensure(t.Context(), opts, runtime)
	if result.Err != nil || !result.Installed {
		t.Fatalf("ensure: %+v", result)
	}
	if want := []string{"checking", "verifying", "reused"}; !reflect.DeepEqual(stages, want) {
		t.Fatalf("stages = %v, want %v", stages, want)
	}
}

func TestEnsureRepairsMissingModifiedUntrackedAndOldRuntimes(t *testing.T) {
	for _, scenario := range []string{"missing", "modified", "untracked", "old-version"} {
		t.Run(scenario, func(t *testing.T) {
			payload := []byte("pinned runtime v1.2.3")
			runtime := regressionRuntime("v1.2.3", payload)
			opts := regressionInstallOptions(t.TempDir(), runtime, payload)
			if scenario != "missing" {
				if result := Install(t.Context(), opts, runtime); result.Err != nil {
					t.Fatal(result.Err)
				}
			}
			switch scenario {
			case "modified":
				if err := os.WriteFile(filepath.Join(opts.BinDir, runtime.Binary), []byte("modified"), 0o755); err != nil {
					t.Fatal(err)
				}
			case "untracked":
				if err := os.Remove(filepath.Join(opts.BinDir, runtimeSetManifestName)); err != nil {
					t.Fatal(err)
				}
			case "old-version":
				probe := opts.RunVersion
				opts.RunVersion = func(ctx context.Context, path string, args []string) (string, error) {
					if path == filepath.Join(opts.BinDir, runtime.Binary) {
						return "old v1.0.0", nil
					}
					return probe(ctx, path, args)
				}
			}
			downloads := 0
			opts.Download = func(context.Context, string) ([]byte, error) { downloads++; return payload, nil }
			var stages []string
			opts.Progress = func(stage, _ string) { stages = append(stages, stage) }
			result := Ensure(t.Context(), opts, runtime)
			if result.Err != nil || !result.Installed || downloads != 1 {
				t.Fatalf("ensure: %+v, downloads=%d", result, downloads)
			}
			for _, stage := range []string{"checking", "downloading", "verifying", "installing"} {
				if !strings.Contains(strings.Join(stages, ","), stage) {
					t.Errorf("missing stage %s in %v", stage, stages)
				}
			}
		})
	}
}

func TestEnsureRejectsUnprovenBinaryWhenDownloadFails(t *testing.T) {
	payload := []byte("pinned runtime v1.2.3")
	runtime := regressionRuntime("v1.2.3", payload)
	opts := regressionInstallOptions(t.TempDir(), runtime, payload)
	if err := os.WriteFile(filepath.Join(opts.BinDir, runtime.Binary), payload, 0o755); err != nil {
		t.Fatal(err)
	}
	opts.Download = func(context.Context, string) ([]byte, error) { return nil, errors.New("offline") }
	if result := Ensure(t.Context(), opts, runtime); result.Err == nil {
		t.Fatal("unproven binary was accepted")
	}
}

func TestHelperBuildHomeKeepsRootEnvironmentSanitized(t *testing.T) {
	asRoot(t)
	privateHome := t.TempDir()
	t.Setenv("HOME", "/attacker")
	t.Setenv("GOFLAGS", "-toolexec=/attacker/tool")
	t.Setenv("GOCACHE", "/attacker/cache")
	t.Setenv("GOSUMDB", "off")
	ctx := ContextWithBuildHome(t.Context(), privateHome)
	env := envMap(t, goBuildEnv(ctx, "/usr/local/go/bin/go"))
	for key, want := range map[string]string{
		"HOME": privateHome, "GOPATH": filepath.Join(privateHome, "go"),
		"GOMODCACHE": filepath.Join(privateHome, "go", "pkg", "mod"), "GOCACHE": filepath.Join(privateHome, ".cache", "go-build"),
		"GOFLAGS": "", "GOENV": "off", "GOSUMDB": "sum.golang.org", "GOTOOLCHAIN": "local",
	} {
		if env[key] != want {
			t.Errorf("%s = %q, want %q", key, env[key], want)
		}
	}
}
