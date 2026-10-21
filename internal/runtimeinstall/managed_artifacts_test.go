package runtimeinstall

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func installManagedArtifactRuntime(t *testing.T, binDir, binary string, payload []byte) {
	t.Helper()
	runtime := regressionRuntime("v1.2.3", payload)
	runtime.Name = binary
	runtime.Binary = binary
	opts := regressionInstallOptions(binDir, runtime, payload)
	if result := Install(t.Context(), opts, runtime); result.Err != nil || !result.Installed {
		t.Fatalf("install %s: %+v", binary, result)
	}
}

func writeGenerationManifest(t *testing.T, binDir string, digests map[string]string) {
	t.Helper()
	body, err := json.Marshal(runtimeGenerationManifest{Version: 1, TransactionID: "t", Digests: digests})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, runtimeSetManifestName), body, 0o600); err != nil {
		t.Fatal(err)
	}
}

// Two separate install transactions must both stay provable: the second
// publish used to overwrite the generation manifest with only its own item,
// so uninstall would have lost the proof for the first runtime (#1289).
func TestManagedRuntimeArtifactsSurvivesSequentialInstalls(t *testing.T) {
	binDir := t.TempDir()
	installManagedArtifactRuntime(t, binDir, "alpha", []byte("alpha-runtime-v1.2.3"))
	installManagedArtifactRuntime(t, binDir, "beta", []byte("beta-runtime-v1.2.3"))
	got := ManagedRuntimeArtifacts(binDir)
	want := []string{
		filepath.Join(binDir, "alpha"),
		filepath.Join(binDir, "beta"),
		filepath.Join(binDir, runtimeSetManifestName),
		filepath.Join(binDir, runtimeSetLockName),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ManagedRuntimeArtifacts = %v, want %v", got, want)
	}
}

func TestManagedRuntimeArtifactsRequiresContentProof(t *testing.T) {
	binDir := t.TempDir()
	owned := []byte("veil-installed-hysteria")
	replaced := []byte("veil-installed-caddy")
	writeFile := func(name string, body []byte) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(binDir, name), body, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile("hysteria", owned)
	// The operator replaced caddy after Veil installed it: different bytes.
	writeFile("caddy", []byte("operator-built-caddy"))
	// A recorded name that is now a symlink to a matching file is not proof.
	writeFile("real-mita", owned)
	if err := os.Symlink(filepath.Join(binDir, "real-mita"), filepath.Join(binDir, "mita")); err != nil {
		t.Fatal(err)
	}
	writeFile("veil", owned)
	writeFile(".hidden", owned)
	writeGenerationManifest(t, binDir, map[string]string{
		"hysteria":    sha256Hex(owned),
		"caddy":       sha256Hex(replaced),
		"mita":        sha256Hex(owned),
		"veil":        sha256Hex(owned),
		".hidden":     sha256Hex(owned),
		"../escape":   sha256Hex(owned),
		"sub/dir":     sha256Hex(owned),
		"not-present": sha256Hex(owned),
	})
	got := ManagedRuntimeArtifacts(binDir)
	want := []string{filepath.Join(binDir, "hysteria"), filepath.Join(binDir, runtimeSetManifestName)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ManagedRuntimeArtifacts = %v, want %v", got, want)
	}
}

func TestManagedRuntimeArtifactsWithoutTrustworthyManifestIsEmpty(t *testing.T) {
	cases := map[string]func(t *testing.T, binDir string){
		"absent": func(*testing.T, string) {},
		"corrupt": func(t *testing.T, binDir string) {
			if err := os.WriteFile(filepath.Join(binDir, runtimeSetManifestName), []byte("{not json"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"wrong-version": func(t *testing.T, binDir string) {
			body := []byte(`{"version":2,"digests":{"hysteria":"` + sha256Hex([]byte("x")) + `"}}`)
			if err := os.WriteFile(filepath.Join(binDir, runtimeSetManifestName), body, 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"symlinked-manifest": func(t *testing.T, binDir string) {
			elsewhere := filepath.Join(t.TempDir(), "manifest.json")
			body := []byte(`{"version":1,"digests":{"hysteria":"` + sha256Hex([]byte("x")) + `"}}`)
			if err := os.WriteFile(elsewhere, body, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(elsewhere, filepath.Join(binDir, runtimeSetManifestName)); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			binDir := t.TempDir()
			if err := os.WriteFile(filepath.Join(binDir, "hysteria"), []byte("x"), 0o755); err != nil {
				t.Fatal(err)
			}
			setup(t, binDir)
			if got := ManagedRuntimeArtifacts(binDir); got != nil {
				t.Fatalf("ManagedRuntimeArtifacts = %v, want nil", got)
			}
		})
	}
	if got := ManagedRuntimeArtifacts(""); got != nil {
		t.Fatalf("ManagedRuntimeArtifacts(\"\") = %v, want nil", got)
	}
}
