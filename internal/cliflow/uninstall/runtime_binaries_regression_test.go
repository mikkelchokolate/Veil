package uninstall

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// seedRuntimeInstallDir lays out what `veil runtime install` leaves in the
// install dir: Veil's own hysteria (bytes recorded in the generation
// manifest), a caddy the operator replaced after install (recorded digest no
// longer matches) and the veil binary itself.
func seedRuntimeInstallDir(t *testing.T, dir string) {
	t.Helper()
	veilHysteria := []byte("veil-installed-hysteria")
	digest := func(body []byte) string {
		sum := sha256.Sum256(body)
		return hex.EncodeToString(sum[:])
	}
	for name, body := range map[string][]byte{
		"veil":     []byte("veil"),
		"hysteria": veilHysteria,
		"caddy":    []byte("operator-built-caddy"),
	} {
		if err := os.WriteFile(filepath.Join(dir, name), body, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	manifest, err := json.Marshal(map[string]any{
		"version": 1, "transactionId": "t",
		"digests": map[string]string{
			"hysteria": digest(veilHysteria),
			"caddy":    digest([]byte("veil-installed-caddy")),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".veil-runtime-generation.json"), manifest, 0o600); err != nil {
		t.Fatal(err)
	}
}

func runtimeTestDeps() Dependencies {
	return Dependencies{
		ServiceStopper:  func(string) error { return nil },
		FileRemover:     RemovePath,
		SystemdReloader: func() error { return nil },
	}
}

func pathExists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Lstat(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	return err == nil
}

// TestRunRemovesVeilInstalledRuntimeBinaries is the #1289 regression for the
// runtime half: a full uninstall used to delete only <install-dir>/veil and
// leave caddy/hysteria/sing-box/mita/olcrtc on PATH for the next install to
// adopt. Only binaries whose bytes still match Veil's install record go; an
// operator-replaced caddy at the same path survives.
func TestRunRemovesVeilInstalledRuntimeBinaries(t *testing.T) {
	opts := firewallTestOptions(t)
	seedRuntimeInstallDir(t, opts.InstallDir)
	var out, errOut bytes.Buffer
	if err := Run(opts, &out, &errOut, runtimeTestDeps()); err != nil {
		t.Fatalf("Run: %v\n%s", err, errOut.String())
	}
	hysteria := filepath.ToSlash(filepath.Join(opts.InstallDir, "hysteria"))
	if !strings.Contains(out.String(), "Remove runtime binaries installed by Veil:\n  - "+hysteria+"\n") {
		t.Fatalf("plan must list the Veil-installed runtime:\n%s", out.String())
	}
	if strings.Contains(out.String(), filepath.ToSlash(filepath.Join(opts.InstallDir, "caddy"))) {
		t.Fatalf("plan must not list the operator-replaced caddy:\n%s", out.String())
	}
	for _, gone := range []string{"veil", "hysteria", ".veil-runtime-generation.json"} {
		if pathExists(t, filepath.Join(opts.InstallDir, gone)) {
			t.Fatalf("%s survived a full uninstall", gone)
		}
	}
	if !pathExists(t, filepath.Join(opts.InstallDir, "caddy")) {
		t.Fatal("uninstall removed a caddy Veil no longer owns (digest mismatch)")
	}
}

// --keep-data is the in-place upgrade path and `veil install` does not fetch
// runtimes, so the protocol binaries must stay where the kept configuration
// expects them.
func TestRunKeepDataPreservesRuntimeBinaries(t *testing.T) {
	opts := firewallTestOptions(t)
	opts.KeepData = true
	seedRuntimeInstallDir(t, opts.InstallDir)
	var out, errOut bytes.Buffer
	if err := Run(opts, &out, &errOut, runtimeTestDeps()); err != nil {
		t.Fatalf("Run: %v\n%s", err, errOut.String())
	}
	if !strings.Contains(out.String(), "Preserved runtime binaries:") {
		t.Fatalf("plan must say runtimes are kept:\n%s", out.String())
	}
	for _, kept := range []string{"hysteria", "caddy", ".veil-runtime-generation.json"} {
		if !pathExists(t, filepath.Join(opts.InstallDir, kept)) {
			t.Fatalf("--keep-data removed %s", kept)
		}
	}
	if pathExists(t, filepath.Join(opts.InstallDir, "veil")) {
		t.Fatal("--keep-data must still remove the veil binary")
	}
}

// Without an install record nothing beyond the veil binary is touched, even
// when same-named binaries sit in the install dir.
func TestRunWithoutRuntimeProvenanceKeepsBinaries(t *testing.T) {
	opts := firewallTestOptions(t)
	if err := os.WriteFile(filepath.Join(opts.InstallDir, "caddy"), []byte("system caddy"), 0o755); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if err := Run(opts, &out, &errOut, runtimeTestDeps()); err != nil {
		t.Fatalf("Run: %v\n%s", err, errOut.String())
	}
	if !strings.Contains(out.String(), "  - none recorded (no Veil runtime provenance in ") {
		t.Fatalf("plan must say no runtime provenance was found:\n%s", out.String())
	}
	if !pathExists(t, filepath.Join(opts.InstallDir, "caddy")) {
		t.Fatal("uninstall removed a caddy without Veil provenance")
	}
}
