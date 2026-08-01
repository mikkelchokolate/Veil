package runtimeinstall

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

const runtimeActivationCrashEnv = "VEIL_RUNTIME_ACTIVATION_CRASH"

func TestRuntimeActivationRecoversSIGKILLAtIrreversiblePhases(t *testing.T) {
	for _, phase := range []string{"after-preserve", "after-active"} {
		t.Run(phase, func(t *testing.T) {
			binDir := t.TempDir()
			active := filepath.Join(binDir, "regression-runtime")
			old := []byte("legacy-runtime-old")
			if err := os.WriteFile(active, old, 0o755); err != nil {
				t.Fatal(err)
			}
			command := exec.Command(os.Args[0], "-test.run=^TestRuntimeActivationCrashHelper$")
			command.Env = append(os.Environ(), runtimeActivationCrashEnv+"="+phase, "VEIL_RUNTIME_BIN_DIR="+binDir)
			if err := command.Run(); err == nil {
				t.Fatal("crash helper exited successfully")
			}
			storeRoot := filepath.Join(binDir, ".veil-runtimes")
			if err := recoverRuntimeActivation(storeRoot); err != nil {
				t.Fatalf("recover activation: %v", err)
			}
			body, err := os.ReadFile(active)
			if err != nil {
				t.Fatal(err)
			}
			if string(body) != string(old) {
				t.Fatalf("recovery selected wrong runtime: %q", body)
			}
			info, err := os.Lstat(active)
			if err != nil {
				t.Fatal(err)
			}
			if !info.Mode().IsRegular() {
				t.Fatalf("legacy active runtime type changed after recovery: %s", info.Mode())
			}
		})
	}
}

func TestRuntimeActivationCrashHelper(t *testing.T) {
	phase := os.Getenv(runtimeActivationCrashEnv)
	if phase == "" {
		return
	}
	binDir := os.Getenv("VEIL_RUNTIME_BIN_DIR")
	payload := []byte("runtime-new-v1.2.4")
	runtime := regressionRuntime("v1.2.4", payload)
	options := regressionInstallOptions(binDir, runtime, payload)
	options.AfterPreserve = func(Runtime, string) error {
		if phase == "after-preserve" {
			os.Exit(91)
		}
		return nil
	}
	options.AfterActivate = func(Runtime, string) error {
		if phase == "after-active" {
			os.Exit(92)
		}
		return nil
	}
	_ = Install(t.Context(), options, runtime)
	os.Exit(93)
}
