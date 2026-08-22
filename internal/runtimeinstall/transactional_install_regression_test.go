package runtimeinstall

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
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
	oldTarget, _ := os.Readlink(active)
	oldBody, err := os.ReadFile(active)
	if err != nil {
		t.Fatal(err)
	}

	newPayload := []byte("runtime-new-v1.2.4")
	newRuntime := regressionRuntime("v1.2.4", newPayload)
	options := regressionInstallOptions(binDir, newRuntime, newPayload)
	optionsValue := reflect.ValueOf(&options).Elem()
	hook := optionsValue.FieldByName("AfterActivate")
	if !hook.IsValid() || !hook.CanSet() || hook.Kind() != reflect.Func {
		t.Errorf("Options lacks required AfterActivate rollback fault seam")
	} else {
		hook.Set(reflect.MakeFunc(hook.Type(), func([]reflect.Value) []reflect.Value {
			outputs := make([]reflect.Value, hook.Type().NumOut())
			for index := range outputs {
				outputs[index] = reflect.Zero(hook.Type().Out(index))
			}
			if len(outputs) > 0 && hook.Type().Out(len(outputs)-1).Implements(reflect.TypeOf((*error)(nil)).Elem()) {
				outputs[len(outputs)-1] = reflect.ValueOf(errors.New("injected post-activation failure"))
			}
			return outputs
		}))
	}
	failed := Install(t.Context(), options, newRuntime)
	if failed.Err == nil {
		t.Fatal("post-activation fault unexpectedly reported successful install")
	}
	currentTarget, _ := os.Readlink(active)
	currentBody, readErr := os.ReadFile(active)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if currentTarget != oldTarget || string(currentBody) != string(oldBody) {
		t.Fatalf("failed activation did not restore prior target/body: old=%q/%q current=%q/%q", oldTarget, oldBody, currentTarget, currentBody)
	}
}

func TestRuntimeDescriptorsRequireImmutableSourceCommitAndReadonlySourceGraphs(t *testing.T) {
	for _, runtime := range Catalog("amd64") {
		value := reflect.ValueOf(runtime)
		field := value.FieldByName("SourceCommit")
		commitPinned := field.IsValid() && field.Kind() == reflect.String && len(field.String()) == 40
		digestPinned := len(runtime.PinnedSHA256) == 64
		signaturePinned := runtime.SignaturePolicy != ""
		if !commitPinned && !digestPinned && !signaturePinned {
			t.Errorf("runtime %s lacks immutable commit, digest, or signature provenance", runtime.Name)
		}
	}
	source, err := os.ReadFile("runtimeinstall.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"go mod verify", "-mod=readonly", "SourceCommit"} {
		if !strings.Contains(string(source), required) {
			t.Errorf("source runtime build lacks %q", required)
		}
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
