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

func TestNewEnsurerCachesSuccessfulInstallWithoutSharingHelpers(t *testing.T) {
	payload := []byte("pinned runtime v1.2.3")
	runtime := regressionRuntime("v1.2.3", payload)
	opts := regressionInstallOptions(t.TempDir(), runtime, payload)
	probes, downloads := 0, 0
	probe, download := opts.RunVersion, opts.Download
	opts.RunVersion = func(ctx context.Context, path string, args []string) (string, error) {
		probes++
		return probe(ctx, path, args)
	}
	opts.Download = func(ctx context.Context, url string) ([]byte, error) { downloads++; return download(ctx, url) }
	ensure := NewEnsurer()
	for range 2 {
		if result := ensure(t.Context(), opts, runtime); result.Err != nil || !result.Installed {
			t.Fatalf("ensure: %+v", result)
		}
	}
	if probes != 1 || downloads != 1 {
		t.Fatalf("warm helper repeated acquisition: probes=%d downloads=%d", probes, downloads)
	}
	if result := NewEnsurer()(t.Context(), opts, runtime); result.Err != nil {
		t.Fatal(result.Err)
	}
	if probes != 2 || downloads != 1 {
		t.Fatalf("independent helper trusted another helper's probe: probes=%d downloads=%d", probes, downloads)
	}
	if result := Ensure(t.Context(), opts, runtime); result.Err != nil {
		t.Fatal(result.Err)
	}
	if probes != 3 {
		t.Fatalf("exported Ensure unexpectedly cached its probe: %d", probes)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if result := ensure(ctx, opts, runtime); !errors.Is(result.Err, context.Canceled) || probes != 3 {
		t.Fatalf("cached ensure ignored cancellation: %+v probes=%d", result, probes)
	}
}

func TestNewEnsurerInvalidatesChangedPinsAndProbePolicy(t *testing.T) {
	for _, change := range []string{"version", "arch", "path", "probe-args", "source-commit", "integrity-pin"} {
		t.Run(change, func(t *testing.T) {
			payload := []byte("regression-runtime v1.2.3")
			runtime := regressionRuntime("v1.2.3", payload)
			opts := regressionInstallOptions(t.TempDir(), runtime, payload)
			probes, downloads := 0, 0
			readVersion := func(_ context.Context, path string, _ []string) (string, error) {
				probes++
				body, err := os.ReadFile(path)
				return string(body), err
			}
			opts.RunVersion = readVersion
			ensure := NewEnsurer()
			if result := ensure(t.Context(), opts, runtime); result.Err != nil {
				t.Fatal(result.Err)
			}
			probes = 0
			switch change {
			case "version":
				payload = []byte("regression-runtime v1.2.4")
				runtime = regressionRuntime("v1.2.4", payload)
				binDir := opts.BinDir
				opts = regressionInstallOptions(binDir, runtime, payload)
				opts.RunVersion = readVersion
			case "arch":
				opts.Arch = "arm64"
			case "path":
				opts.BinDir = t.TempDir()
				if result := Install(t.Context(), opts, runtime); result.Err != nil {
					t.Fatal(result.Err)
				}
				probes = 0
			case "probe-args":
				runtime.VersionArgs = []string{"--version"}
			case "source-commit":
				runtime.SourceCommit = strings.Repeat("a", 40)
			case "integrity-pin":
				runtime.PinnedSHA256 = strings.Repeat("a", 64)
			}
			download := opts.Download
			opts.Download = func(ctx context.Context, url string) ([]byte, error) { downloads++; return download(ctx, url) }
			if result := ensure(t.Context(), opts, runtime); result.Err != nil {
				t.Fatal(result.Err)
			}
			wantProbes, wantDownloads := 1, 0
			if change == "version" {
				wantProbes, wantDownloads = 2, 1
			}
			if probes != wantProbes || downloads != wantDownloads {
				t.Fatalf("changed %s bypassed verification: probes=%d downloads=%d", change, probes, downloads)
			}
		})
	}
}

func TestNewEnsurerRejectsChangedBytesOrMissingProvenanceDespiteCache(t *testing.T) {
	for _, change := range []string{"bytes", "manifest", "manifest-digest"} {
		t.Run(change, func(t *testing.T) {
			payload := []byte("pinned runtime v1.2.3")
			runtime := regressionRuntime("v1.2.3", payload)
			opts := regressionInstallOptions(t.TempDir(), runtime, payload)
			ensure := NewEnsurer()
			if result := ensure(t.Context(), opts, runtime); result.Err != nil {
				t.Fatal(result.Err)
			}
			manifestPath := filepath.Join(opts.BinDir, runtimeSetManifestName)
			switch change {
			case "bytes":
				if err := os.WriteFile(filepath.Join(opts.BinDir, runtime.Binary), []byte("modified"), 0o755); err != nil {
					t.Fatal(err)
				}
			case "manifest":
				if err := os.Remove(manifestPath); err != nil {
					t.Fatal(err)
				}
			case "manifest-digest":
				manifest, ok := readRuntimeGenerationManifest(manifestPath)
				if !ok {
					t.Fatal("manifest missing")
				}
				manifest.Digests[runtime.Binary] = strings.Repeat("0", 64)
				if err := writeRuntimeJSONAtomic(manifestPath, manifest, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			downloads := 0
			opts.Download = func(context.Context, string) ([]byte, error) { downloads++; return nil, errors.New("offline") }
			if result := ensure(t.Context(), opts, runtime); result.Err == nil || downloads != 1 {
				t.Fatalf("cached verification bypassed changed %s: %+v downloads=%d", change, result, downloads)
			}
		})
	}
}

func TestRuntimeSetReportsOneInstallingEventWithoutRuntimeName(t *testing.T) {
	payload := []byte("pinned runtime v1.2.3")
	one := regressionRuntime("v1.2.3", payload)
	two := one
	one.Name, one.Binary = "one", "one"
	two.Name, two.Binary = "two", "two"
	opts := regressionInstallOptions(t.TempDir(), one, payload)
	var installing []string
	opts.Progress = func(stage, name string) {
		if stage == "installing" {
			installing = append(installing, name)
		}
	}
	for _, result := range installRuntimes(t.Context(), opts, []Runtime{one, two}) {
		if result.Err != nil {
			t.Fatal(result.Err)
		}
	}
	if !reflect.DeepEqual(installing, []string{""}) {
		t.Fatalf("installing events = %q, want one set-level event", installing)
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
