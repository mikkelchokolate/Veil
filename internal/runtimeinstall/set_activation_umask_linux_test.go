package runtimeinstall

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestRuntimeActivationPreservesModesWithPrivateUmask(t *testing.T) {
	const helperEnv = "VEIL_RUNTIME_ACTIVATION_UMASK_HELPER"
	if os.Getenv(helperEnv) != "1" {
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRuntimeActivationPreservesModesWithPrivateUmask$", "-test.v")
		command.Env = append(os.Environ(), helperEnv+"=1")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("activation under helper umask failed: %v\n%s", err, output)
		}
		return
	}
	previous := syscall.Umask(0o077)
	defer syscall.Umask(previous)
	assertMode := func(path string, want os.FileMode) {
		t.Helper()
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Fatalf("%s mode = %o, want %o", path, got, want)
		}
	}

	binDir := t.TempDir()
	for _, version := range []string{"v1.2.3", "v1.2.4"} {
		payload := []byte("runtime " + version)
		runtime := regressionRuntime(version, payload)
		result := Install(t.Context(), regressionInstallOptions(binDir, runtime, payload), runtime)
		if result.Err != nil {
			t.Fatal(result.Err)
		}
		assertMode(result.Path, 0o755)
		assertMode(filepath.Join(binDir, runtimeSetManifestName), 0o600)
		assertMode(filepath.Join(binDir, runtimeSetLockName), 0o600)
	}

	payload := []byte("runtime v1.2.5")
	runtime := regressionRuntime("v1.2.5", payload)
	opts := regressionInstallOptions(t.TempDir(), runtime, payload)
	staged := installOne(t.Context(), opts, runtime)
	if staged.Err != nil {
		t.Fatal(staged.Err)
	}
	journal, err := prepareRuntimeSetActivation(binDir, []Result{staged})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(journal.Items[0].Staged, journal.Items[0].Target); err != nil {
		t.Fatal(err)
	}
	cause := errors.New("injected activation failure")
	if err := rollbackRuntimeSet(binDir, &journal, cause); err != cause {
		t.Fatalf("rollback error = %v, want %v", err, cause)
	}
	assertMode(filepath.Join(binDir, runtime.Binary), 0o755)
	if restored, err := os.ReadFile(filepath.Join(binDir, runtime.Binary)); err != nil || string(restored) != "runtime v1.2.4" {
		t.Fatalf("rollback restored %q, error %v", restored, err)
	}

	privateCopy := filepath.Join(binDir, "private-backup")
	if err := copyRuntimeFile(filepath.Join(binDir, runtime.Binary), privateCopy, 0o600); err != nil {
		t.Fatal(err)
	}
	assertMode(privateCopy, 0o600)
}

func TestEnsureRepairsNonCanonicalPermissions(t *testing.T) {
	for _, cached := range []bool{false, true} {
		for _, test := range []struct {
			name string
			mode os.FileMode
		}{
			{"owner-only", 0o700},
			{"world-writable", 0o777},
			{"setuid", 0o755 | os.ModeSetuid},
			{"setgid", 0o755 | os.ModeSetgid},
		} {
			name := "uncached/" + test.name
			if cached {
				name = "cached/" + test.name
			}
			t.Run(name, func(t *testing.T) {
				payload := []byte("runtime v1.2.3")
				runtime := regressionRuntime("v1.2.3", payload)
				opts := regressionInstallOptions(t.TempDir(), runtime, payload)
				ensure := Ensure
				if cached {
					ensure = NewEnsurer()
				}
				if result := ensure(t.Context(), opts, runtime); result.Err != nil {
					t.Fatal(result.Err)
				}
				active := filepath.Join(opts.BinDir, runtime.Binary)
				if err := os.Chmod(active, test.mode); err != nil {
					t.Fatal(err)
				}
				downloads := 0
				opts.Download = func(context.Context, string) ([]byte, error) { downloads++; return payload, nil }
				result := ensure(t.Context(), opts, runtime)
				if result.Err != nil || downloads != 1 {
					t.Fatalf("noncanonical permissions were reused: %+v downloads=%d", result, downloads)
				}
				info, err := os.Stat(active)
				if err != nil || info.Mode() != 0o755 {
					t.Fatalf("repaired runtime permissions: info=%v error=%v", info, err)
				}
			})
		}
	}
}
