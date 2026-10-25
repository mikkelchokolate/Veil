package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/veilenv"
)

// TestBackupCreateUsesInstalledVeilEnvKeyPath locks the backup half of the
// installed-layout resolver: after `rotate-key --new-key-path` the documented
// "create a fresh encrypted backup after every rotation" step has to archive
// the new state together with the key the Panel reads. Before the resolver,
// create fell back to the packaged default, paired new state with the old key,
// failed its own verify and deleted the archive.
func TestBackupCreateUsesInstalledVeilEnvKeyPath(t *testing.T) {
	etcDir, _, keyPath, statePath := restoreEnvFixture(t)
	rotatedKeyPath := filepath.Join(etcDir, "rotated-state.key")
	if out, err := runRotateKey(t, "--state", statePath, "--key-path", keyPath, "--new-key-path", rotatedKeyPath); err != nil {
		t.Fatalf("rotate-key: %v\n%s", err, out)
	}

	archive := filepath.Join(t.TempDir(), "post-rotation.tar.gz")
	create := NewRootCommand("test")
	out := &bytes.Buffer{}
	create.SetOut(out)
	create.SetErr(out)
	create.SetArgs([]string{"backup", "create", "--output", archive, "--allow-unencrypted"})
	if err := create.Execute(); err != nil {
		t.Fatalf("backup create after rotation: %v\n%s", err, out.String())
	}
	if _, err := os.Stat(archive); err != nil {
		t.Fatalf("backup create did not keep its archive: %v", err)
	}

	// The archive has to carry the environment's key: restore it into a clean
	// tree and load the state the way the Panel would.
	restoreState := filepath.Join(t.TempDir(), "state.json")
	restoreKey := filepath.Join(t.TempDir(), "state.key")
	restore := NewRootCommand("test")
	restoreOut := &bytes.Buffer{}
	restore.SetOut(restoreOut)
	restore.SetErr(restoreOut)
	restore.SetArgs([]string{
		"backup", "restore", archive,
		"--state", restoreState,
		"--key-path", restoreKey,
		"--allow-unencrypted", "--yes",
	})
	if err := restore.Execute(); err != nil {
		t.Fatalf("restore of the post-rotation archive: %v\n%s", err, restoreOut.String())
	}
	for _, pair := range [][2]string{{restoreKey, restoreState}, {rotatedKeyPath, statePath}} {
		key, err := os.ReadFile(pair[0])
		if err != nil {
			t.Fatalf("read key %s: %v", pair[0], err)
		}
		cipher, err := cipherFromRawKey(key)
		if err != nil {
			t.Fatalf("cipher from %s: %v", pair[0], err)
		}
		snapshot, ok, err := managementstate.NewStore(pair[1], cipher).Load()
		if err != nil || !ok || len(snapshot.Users) != 1 {
			t.Fatalf("state %s does not decrypt with key %s: ok=%t users=%d err=%v", pair[1], pair[0], ok, len(snapshot.Users), err)
		}
	}
}

// TestAdminShowUsesInstalledVeilEnvKeyPath covers the other half: the admin
// commands read the key from the installed environment, so `veil admin show`
// keeps working after the key moved.
func TestAdminShowUsesInstalledVeilEnvKeyPath(t *testing.T) {
	etcDir, _, keyPath, statePath := restoreEnvFixture(t)
	if out, err := runRotateKey(t, "--state", statePath, "--key-path", keyPath, "--new-key-path", filepath.Join(etcDir, "rotated-state.key")); err != nil {
		t.Fatalf("rotate-key: %v\n%s", err, out)
	}
	show := NewRootCommand("test")
	out := &bytes.Buffer{}
	show.SetOut(out)
	show.SetErr(out)
	show.SetArgs([]string{"admin", "show"})
	if err := show.Execute(); err != nil {
		t.Fatalf("admin show after rotation: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "admin") {
		t.Fatalf("admin show did not list the restored account:\n%s", out.String())
	}
}

// TestInstalledLayoutResolverFallsBackWithoutEnvironmentFile keeps the previous
// behaviour when there is no installed veil.env: the process environment (and
// then the packaged default) still resolves the paths.
func TestInstalledLayoutResolverFallsBackWithoutEnvironmentFile(t *testing.T) {
	etcDir, _, keyPath, statePath := restoreEnvFixture(t)
	// Point discovery at an empty tree, then provide the legacy override.
	emptyEtc := t.TempDir()
	t.Setenv("VEIL_ETC_DIR", emptyEtc)
	t.Setenv("VEIL_STATE_PATH", statePath)
	t.Setenv("VEIL_KEY_PATH", keyPath)

	if body, err := os.ReadFile(filepath.Join(etcDir, "veil.env")); err == nil {
		// The fixture file must not be discovered any more.
		if got := veilenv.Value(body, veilenv.KeyPathKey); got == "" {
			t.Fatal("fixture environment is empty")
		}
	}
	show := NewRootCommand("test")
	out := &bytes.Buffer{}
	show.SetOut(out)
	show.SetErr(out)
	show.SetArgs([]string{"admin", "show"})
	if err := show.Execute(); err != nil {
		t.Fatalf("admin show with a legacy environment override: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "admin") {
		t.Fatalf("admin show did not list the account through the process environment:\n%s", out.String())
	}
}

// TestRotateKeySecondRotationUsesInstalledVeilEnvKeyPath locks the missing half
// of #1477: after a path-moving rotation the state is encrypted with the key
// veil.env names, so a second `rotate-key` (with or without --new-key-path)
// has to read from there. Otherwise it decrypts with the packaged default and
// fails with "decrypt current state: … GCM decrypt …".
func TestRotateKeySecondRotationUsesInstalledVeilEnvKeyPath(t *testing.T) {
	etcDir, envPath, keyPath, statePath := restoreEnvFixture(t)
	stubAdminSystemctl(t, envPath)

	first := filepath.Join(etcDir, "rotated-state.key")
	if out, err := runRotateKey(t, "--state", statePath, "--key-path", keyPath, "--new-key-path", first); err != nil {
		t.Fatalf("first rotation: %v\n%s", err, out)
	}
	second := filepath.Join(etcDir, "second-state.key")
	if out, err := runRotateKey(t, "--new-key-path", second); err != nil {
		t.Fatalf("second rotation must read the key from veil.env: %v\n%s", err, out)
	}
	body, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := veilenv.Value(body, veilenv.KeyPathKey); got != second {
		t.Fatalf("VEIL_KEY_PATH = %q, want %q", got, second)
	}
	keyBytes, err := os.ReadFile(second)
	if err != nil {
		t.Fatal(err)
	}
	cipher, err := cipherFromRawKey(keyBytes)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, ok, err := managementstate.NewStore(statePath, cipher).Load()
	if err != nil || !ok || len(snapshot.Users) != 1 {
		t.Fatalf("state does not load with the key veil.env names: ok=%t users=%d err=%v", ok, len(snapshot.Users), err)
	}

	// An in-place rotation after the path moved must also resolve from the
	// environment rather than the packaged default.
	if out, err := runRotateKey(t, "--state", statePath); err != nil {
		t.Fatalf("in-place rotation after a path-moving one: %v\n%s", err, out)
	}
}
