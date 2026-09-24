package runtimeinstall

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRuntimeInstallPublishesVersionedTargetAndDurableManifest(t *testing.T) {
	binDir := t.TempDir()
	orphan := filepath.Join(binDir, ".veil-runtime-stage-orphan")
	if err := os.MkdirAll(orphan, 0o700); err != nil {
		t.Fatal(err)
	}
	payload := []byte("runtime-v1.2.3")
	runtime := regressionRuntime("v1.2.3", payload)
	result := Install(t.Context(), regressionInstallOptions(binDir, runtime, payload), runtime)
	if result.Err != nil || !result.Installed {
		t.Fatalf("install: %+v", result)
	}
	active := filepath.Join(binDir, runtime.Binary)
	info, err := os.Lstat(active)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("active runtime %s is not an atomically renamed regular file", active)
	}
	body, err := os.ReadFile(active)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(body)
	if got := hex.EncodeToString(digest[:]); got != result.SHA256 {
		t.Fatalf("active digest = %s, want %s", got, result.SHA256)
	}
	manifestPath := filepath.Join(binDir, runtimeSetManifestName)
	manifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("runtime generation manifest missing: %v", err)
	}
	for _, value := range []string{runtime.Binary, result.SHA256, "transactionId"} {
		if !strings.Contains(string(manifest), value) {
			t.Errorf("runtime generation manifest lacks %q: %s", value, manifest)
		}
	}
	if _, err := os.Stat(orphan); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("abandoned staging directory was not cleaned: %v", err)
	}
}

func TestRuntimeInstallRollsBackActiveTargetAfterPostActivationFailure(t *testing.T) {
	binDir := t.TempDir()
	oldPayload := []byte("runtime-old-v1.2.3")
	oldRuntime := regressionRuntime("v1.2.3", oldPayload)
	oldResult := Install(t.Context(), regressionInstallOptions(binDir, oldRuntime, oldPayload), oldRuntime)
	if oldResult.Err != nil {
		t.Fatalf("old install: %v", oldResult.Err)
	}
	active := filepath.Join(binDir, oldRuntime.Binary)
	// The live activation contract for the bin dir is a regular file — the
	// set-level journal renames a staged copy onto it. Record the object type
	// and digest so the rollback assert proves identity, not just content.
	oldInfo, err := os.Lstat(active)
	if err != nil {
		t.Fatal(err)
	}
	if !oldInfo.Mode().IsRegular() {
		t.Fatalf("pre-condition: active runtime %s is not a regular file (%s)", active, oldInfo.Mode())
	}
	oldBody, err := os.ReadFile(active)
	if err != nil {
		t.Fatal(err)
	}
	oldDigest := sha256.Sum256(oldBody)

	newPayload := []byte("runtime-new-v1.2.4")
	newRuntime := regressionRuntime("v1.2.4", newPayload)
	options := regressionInstallOptions(binDir, newRuntime, newPayload)
	hookRan := false
	options.AfterActivate = func(Runtime, string) error {
		hookRan = true
		return errors.New("injected post-activation failure")
	}
	failed := Install(t.Context(), options, newRuntime)
	if failed.Err == nil {
		t.Fatal("post-activation fault unexpectedly reported successful install")
	}
	if !hookRan {
		t.Fatal("AfterActivate fault hook never ran — the rollback contract was not exercised")
	}

	currentInfo, err := os.Lstat(active)
	if err != nil {
		t.Fatalf("active runtime missing after rollback: %v", err)
	}
	if !currentInfo.Mode().IsRegular() {
		t.Fatalf("rollback left active runtime %s as non-regular file (%s)", active, currentInfo.Mode())
	}
	currentBody, err := os.ReadFile(active)
	if err != nil {
		t.Fatal(err)
	}
	if currentDigest := sha256.Sum256(currentBody); currentDigest != oldDigest {
		t.Fatalf("failed activation did not restore prior payload: old=%x current=%x", oldDigest, currentDigest)
	}
	if string(currentBody) == string(newPayload) {
		t.Fatal("failed activation left the NEW payload live")
	}
	// The durable generation manifest must still name the old digest — a
	// manifest claiming the failed generation is a lie about what is live.
	manifestBody, err := os.ReadFile(filepath.Join(binDir, runtimeSetManifestName))
	if err != nil {
		t.Fatalf("generation manifest missing after rollback: %v", err)
	}
	var manifest runtimeGenerationManifest
	if err := json.Unmarshal(manifestBody, &manifest); err != nil {
		t.Fatalf("decode generation manifest: %v", err)
	}
	if got := manifest.Digests[oldRuntime.Binary]; got != hex.EncodeToString(oldDigest[:]) {
		t.Fatalf("generation manifest still records the failed digest: %q", got)
	}
	// No activation scratch (`.new.`/`.old.`) or staging dirs may survive a
	// rolled-back transaction.
	entries, err := os.ReadDir(binDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".veil-runtime-set-stage-") || runtimeSetScratchNamePattern.MatchString(name) {
			t.Fatalf("rolled-back activation left scratch artifact %s", name)
		}
	}
}

// TestRuntimeDescriptorsRequireImmutableSourceCommitAndReadonlySourceGraphs
// locks the provenance contract for catalog runtimes and verifies the
// source-commit field is really propagated into the manifest (not merely
// declared in a comment — issue #913).
func TestRuntimeDescriptorsRequireImmutableSourceCommitAndReadonlySourceGraphs(t *testing.T) {
	for _, runtime := range Catalog("amd64") {
		value := runtime.SourceCommit
		commitPinned := len(value) == 40 && isHex40(value)
		digestPinned := len(runtime.PinnedSHA256) == 64
		signaturePinned := runtime.SignaturePolicy != ""
		if !commitPinned && !digestPinned && !signaturePinned {
			t.Errorf("runtime %s lacks immutable commit, digest, or signature provenance", runtime.Name)
		}
	}
	// Behavioral propagation check: a source-built runtime's pinned commit
	// must land in the manifest the installer persists.
	storeRoot := filepath.Join(t.TempDir(), ".veil-runtimes")
	sourceCommit := strings.Repeat("ab", 20)
	runtime := regressionRuntime("v9.9.9", []byte("payload"))
	runtime.SourceCommit = sourceCommit
	if err := updateRuntimeManifest(storeRoot, runtime, "/store/target", "v9.9.9", "digest", time.Now()); err != nil {
		t.Fatalf("updateRuntimeManifest: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(storeRoot, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var decoded runtimeManifest
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Runtimes[runtime.Name].SourceCommit != sourceCommit {
		t.Fatalf("manifest did not persist sourceCommit: %s", body)
	}
}

func isHex40(value string) bool {
	for _, r := range value {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') && (r < 'A' || r > 'F') {
			return false
		}
	}
	return len(value) == 40
}

// TestCaddyNaiveBuildVerifiesModuleGraphBeforeReadonlyBuild executes the real
// command sequence of runCaddyNaiveBuild through a recording seam and asserts
// the argv contract: `go mod verify` runs as a standalone command AFTER the
// module graph is pinned and BEFORE the build, and the build itself is
// -mod=readonly + -trimpath (issue #913 — the previous substring gate could be
// satisfied by comment text alone).
func TestCaddyNaiveBuildVerifiesModuleGraphBeforeReadonlyBuild(t *testing.T) {
	cacheDir := t.TempDir()
	outPath := filepath.Join(t.TempDir(), "caddy")
	var calls [][]string
	orig := execBuildCommand
	t.Cleanup(func() { execBuildCommand = orig })
	execBuildCommand = func(_ context.Context, dir string, _ []string, args ...string) ([]byte, error) {
		calls = append(calls, append([]string(nil), args...))
		return nil, nil
	}
	if err := runCaddyNaiveBuild(t.Context(), "veil-test-go-stub", cacheDir, outPath); err != nil {
		t.Fatalf("runCaddyNaiveBuild: %v", err)
	}
	if len(calls) < 2 {
		t.Fatalf("expected module setup + build commands, got %d calls", len(calls))
	}
	indexOf := func(match func([]string) bool) int {
		for i, args := range calls {
			if match(args) {
				return i
			}
		}
		return -1
	}
	isSub := func(args []string, subs ...string) bool {
		if len(args) < 1+len(subs) {
			return false
		}
		for i, want := range subs {
			if args[1+i] != want {
				return false
			}
		}
		return true
	}
	verifyIdx := indexOf(func(args []string) bool { return isSub(args, "mod", "verify") })
	tidyIdx := indexOf(func(args []string) bool { return isSub(args, "mod", "tidy") })
	requireIdx := indexOf(func(args []string) bool {
		return isSub(args, "mod", "edit", "-require", "github.com/caddyserver/caddy/v2@v2.11.4")
	})
	replaceIdx := indexOf(func(args []string) bool {
		return isSub(args, "mod", "edit", "-replace", "github.com/caddyserver/forwardproxy=github.com/klzgrad/forwardproxy@d62c80d3dd2c706b6b87579844d2397bddd18317")
	})
	buildIdx := indexOf(func(args []string) bool { return isSub(args, "build") })
	for name, idx := range map[string]int{
		"go mod edit -require caddy pin": requireIdx,
		"go mod edit -replace fork pin":  replaceIdx,
		"go mod tidy":                    tidyIdx,
		"go mod verify":                  verifyIdx,
		"go build":                       buildIdx,
	} {
		if idx < 0 {
			t.Fatalf("caddy naive build never invoked %s (calls=%v)", name, calls)
		}
	}
	if !(requireIdx < verifyIdx && replaceIdx < verifyIdx && tidyIdx < verifyIdx && verifyIdx < buildIdx) {
		t.Fatalf("module graph must be pinned+tidied+verified before the build (verify=%d build=%d): %v", verifyIdx, buildIdx, calls)
	}
	build := calls[buildIdx]
	for _, want := range []string{"-mod=readonly", "-trimpath", "-o"} {
		found := false
		for _, arg := range build {
			if arg == want || strings.HasPrefix(arg, want+"=") {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("caddy build args lack %q: %v", want, build)
		}
	}
	if build[len(build)-1] != "." {
		t.Fatalf("caddy build must compile the local module (trailing \".\"): %v", build)
	}
}

func regressionRuntime(version string, payload []byte) Runtime {
	digest := sha256.Sum256(payload)
	return Runtime{
		Name:           "regression-runtime",
		Binary:         "regression-runtime",
		Method:         MethodRawBinary,
		Repo:           "owner/runtime",
		Version:        version,
		Integrity:      "pinned-sha256",
		PinnedSHA256:   hex.EncodeToString(digest[:]),
		VersionArgs:    []string{"version"},
		VersionCommand: "regression-runtime version",
		VersionPattern: strings.TrimPrefix(version, "v"),
		AssetMatch:     func(name string) bool { return name == "runtime" },
	}
}

func regressionInstallOptions(binDir string, runtime Runtime, payload []byte) Options {
	return Options{
		BinDir: binDir,
		Arch:   "amd64",
		FetchReleaseVersion: func(context.Context, string, string) (*Release, error) {
			return &Release{TagName: runtime.Version, Assets: []Asset{{Name: "runtime", BrowserDownloadURL: "https://example.test/runtime"}}}, nil
		},
		Download: func(context.Context, string) ([]byte, error) { return append([]byte(nil), payload...), nil },
		RunVersion: func(context.Context, string, []string) (string, error) {
			return "regression-runtime " + runtime.Version, nil
		},
	}
}
